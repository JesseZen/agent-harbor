package app

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/asheshgoplani/agent-deck/internal/backend"
	"github.com/asheshgoplani/agent-deck/internal/configdraft"
	"github.com/asheshgoplani/agent-deck/internal/coreclient"
	"github.com/asheshgoplani/agent-deck/internal/coreclient/generated"
	"github.com/asheshgoplani/agent-deck/internal/managedconfig"
	"github.com/asheshgoplani/agent-deck/internal/resourcepages/targets"
	"github.com/asheshgoplani/agent-deck/internal/secretinput"
	tea "github.com/charmbracelet/bubbletea"
)

type publishResultMsg struct {
	identity     generated.SnapshotIdentity
	err          error
	load         loadResultMsg
	hasLoad      bool
	baseGen      int64
	baseRev      string
	candidateRev string
	replaceIDs   []string
}

type targetProbeRef struct {
	id         string
	generation int
}

type targetProbeQueuedMsg struct {
	targetIDs []string
	err       error
}

type targetProbeCheckMsg struct {
	targetIDs []string
	load      loadResultMsg
}

func (model *Model) publish() tea.Cmd {
	switch {
	case model.publishing:
		model.status = "Save is already in progress"
		return nil
	case model.configSource == nil:
		model.status = "Configuration client unavailable"
		return nil
	case model.draft == nil || model.draft.Disconnected():
		model.status = "Save unavailable while disconnected"
		return nil
	case model.targets != nil && model.targets.HasUnstaged():
		model.status = "Paste the API key again before saving"
		return nil
	case !model.draft.CanPublish():
		model.status = "Save blocked by the current configuration state"
		return nil
	case !model.draft.IsDirty():
		model.status = "No changes to save"
		return nil
	}

	mutable, err := model.draft.Command()
	if err != nil {
		model.status = "Save blocked: " + err.Error()
		return nil
	}
	revision, err := newConfigRevision()
	if err != nil {
		model.status = "Save blocked: could not allocate a configuration revision"
		return nil
	}
	validate := generated.ValidateConfigCommand{
		InstanceId:         generated.InstanceID(model.draft.InstanceID()),
		ExpectedGeneration: model.draft.Generation(),
		MutableConfig:      mutable,
	}
	patch := generated.PatchConfigCommand{
		InstanceId:         generated.InstanceID(model.draft.InstanceID()),
		ExpectedGeneration: model.draft.Generation(),
		ConfigRevision:     revision,
		MutableConfig:      mutable,
	}
	baseRevision := model.draft.Revision()
	replaceIDs := submittedReplaceIDs(mutable)
	model.publishing = true
	model.status = "Saving..."
	model.resizePages()

	return func() tea.Msg {
		result := publishResultMsg{
			baseGen:      patch.ExpectedGeneration,
			baseRev:      baseRevision,
			candidateRev: patch.ConfigRevision,
			replaceIDs:   replaceIDs,
		}
		validation, err := model.configSource.ValidateConfig(model.ctx, validate)
		if err != nil {
			result.err = err
			return result
		}
		if !validation.Valid {
			result.err = fmt.Errorf("configuration validation failed")
			return result
		}
		result.identity, result.err = model.configSource.PatchConfig(model.ctx, patch)
		if result.err == nil || publicationNeedsConvergence(result.err) {
			result.load = model.fetchAll(model.ctx, result.err == nil)
			result.hasLoad = true
		}
		return result
	}
}

func newConfigRevision() (string, error) {
	var suffix [8]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		return "", err
	}
	return "tui-" + time.Now().UTC().Format("20060102T150405.000000000Z") + "-" + hex.EncodeToString(suffix[:]), nil
}

func (model *Model) applyPublishResult(result publishResultMsg) tea.Cmd {
	model.publishing = false
	if result.err == nil {
		cleanupErr := model.targets.DiscardOwnedStages(model.ctx)
		if result.hasLoad {
			model.applyLoadResult(result.load)
		}
		if cleanupErr != nil {
			model.targets.ApplyCleanupPending()
			model.status = "Saved; secure cleanup is still pending"
		} else {
			model.status = "Saved"
		}
		model.resizePages()
		return model.probeManagedTargets(result.load)
	}

	var apiErr *coreclient.APIError
	if !errors.As(result.err, &apiErr) {
		model.status = "Save failed: " + result.err.Error()
		model.resizePages()
		return nil
	}

	switch {
	case apiErr.Code == string(secretinput.CodeExpired) || apiErr.Code == string(secretinput.CodeNotFound):
		code := secretinput.Code(apiErr.Code)
		for _, credentialID := range result.replaceIDs {
			model.targets.NoteStageLoss(credentialID, code)
		}
		model.status = "Save blocked: paste the API key again (" + apiErr.Code + ")"
	case apiErr.Code == string(generated.ConfigMutationStatusSecretCleanupPending):
		model.targets.ApplyCleanupPending()
		model.status = "Save blocked: secure cleanup pending"
	case apiErr.Code == string(generated.OperationUnknown):
		model.applyOperationUnknown(result)
	case apiErr.StatusCode == http.StatusConflict:
		model.applyGenerationConflict(result)
	case apiErr.Code == "launcher_identity":
		model.status = "Save blocked: CLI installation is not trusted · run agent-harbor doctor"
	default:
		model.status = "Save failed: " + apiErr.Error()
	}
	model.refreshPages(false)
	model.resizePages()
	return nil
}

func (model *Model) probeManagedTargets(load loadResultMsg) tea.Cmd {
	if model.probeSource == nil || load.configErr != nil || load.runtimeErr != nil {
		return nil
	}
	refs := managedTargetProbeRefs(load.config, load.runtime)
	if len(refs) == 0 {
		return nil
	}
	return func() tea.Msg {
		ids := make([]string, 0, len(refs))
		for _, ref := range refs {
			ids = append(ids, ref.id)
			if err := model.probeSource.ProbeTarget(model.ctx, ref.id, ref.generation, 5*time.Second); err != nil {
				return targetProbeQueuedMsg{targetIDs: ids, err: err}
			}
		}
		return targetProbeQueuedMsg{targetIDs: ids}
	}
}

func managedTargetProbeRefs(snapshot generated.MutableConfigSnapshot, runtime backend.Snapshot) []targetProbeRef {
	command := configdraft.ViewToCommand(snapshot.MutableConfig)
	managedIDs := map[string]bool{}
	for _, object := range managedconfig.ObjectsByKind(command, generated.ManagedObjectKindUpstream) {
		for _, id := range managedconfig.Members(object, generated.ManagedResourceRefKindTarget) {
			managedIDs[id] = true
		}
	}
	refs := make([]targetProbeRef, 0, len(managedIDs))
	for _, target := range runtime.Targets {
		if managedIDs[target.ID] {
			refs = append(refs, targetProbeRef{id: target.ID, generation: target.TargetGeneration})
		}
	}
	return refs
}

func (model *Model) checkTargetProbes(ids []string) tea.Cmd {
	return tea.Tick(1200*time.Millisecond, func(time.Time) tea.Msg {
		return targetProbeCheckMsg{targetIDs: append([]string(nil), ids...), load: model.fetchAll(model.ctx, false)}
	})
}

func (model *Model) applyGenerationConflict(result publishResultMsg) {
	if !result.hasLoad || result.load.configErr != nil {
		model.status = "Generation conflict; reload failed"
		return
	}
	model.draft.BeginConflict(result.load.config)
	model.targets.ApplyConflictState()
	model.status = "Configuration was updated in another terminal; review it and save again"
}

func (model *Model) applyOperationUnknown(result publishResultMsg) {
	if !result.hasLoad || result.load.configErr != nil {
		model.targets.HandleOperationUnknown(targets.OperationUnknownConflict, result.replaceIDs)
		model.status = "Apply outcome unknown; reload failed"
		return
	}
	current := result.load.config
	switch {
	case current.ActiveGeneration > result.baseGen && current.ConfigRevision == result.candidateRev:
		model.targets.HandleOperationUnknown(targets.OperationUnknownSuccess, result.replaceIDs)
		result.load.forceReplace = true
		model.applyLoadResult(result.load)
		model.status = "Saved"
	case current.ActiveGeneration == result.baseGen && current.ConfigRevision == result.baseRev:
		model.targets.HandleOperationUnknown(targets.OperationUnknownUnchanged, result.replaceIDs)
		model.status = "Configuration unchanged: credential replace required"
	default:
		model.draft.BeginConflict(current)
		model.targets.HandleOperationUnknown(targets.OperationUnknownConflict, result.replaceIDs)
		model.status = "Publication outcome conflicts with current generation"
	}
}

func publicationNeedsConvergence(err error) bool {
	var apiErr *coreclient.APIError
	if !errors.As(err, &apiErr) {
		return false
	}
	return apiErr.StatusCode == http.StatusConflict || apiErr.Code == string(generated.OperationUnknown)
}

func submittedReplaceIDs(command generated.MutableConfigCommand) []string {
	var ids []string
	for _, credential := range command.Credentials {
		replace, err := credential.SecretAction.AsCredentialSecretAction1()
		if err == nil && replace.Mode == generated.CredentialSecretActionReplace {
			ids = append(ids, string(credential.Id))
		}
	}
	return ids
}
