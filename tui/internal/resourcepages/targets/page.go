package targets

import (
	"context"
	"fmt"
	"strings"

	"github.com/asheshgoplani/agent-deck/internal/configdraft"
	"github.com/asheshgoplani/agent-deck/internal/coreclient/generated"
	"github.com/asheshgoplani/agent-deck/internal/formui"
	"github.com/asheshgoplani/agent-deck/internal/managedconfig"
	"github.com/asheshgoplani/agent-deck/internal/resourcepage"
	"github.com/asheshgoplani/agent-deck/internal/secretinput"
	"github.com/asheshgoplani/agent-deck/internal/upstream/k9s/resourceview"
	tea "github.com/charmbracelet/bubbletea"
)

type overlayKind int

const (
	overlayNone overlayKind = iota
	overlayEditor
	overlayDetails
	overlayDeps
	overlayConfirmDelete
	overlayUpstreamDelete
	overlayMigrationConfirm
	overlayRestoreUpstream
)

// Page is a Bubble Tea model for the Targets owner tab (Targets/Endpoints/Credentials).
type Page struct {
	draft            *configdraft.Draft
	client           secretinput.StageHTTPClient
	status           TargetStatusProvider
	scope            string
	stager           *secretinput.Stager
	kind             Kind
	tablePage        *resourcepage.Page
	width            int
	height           int
	overlay          overlayKind
	editor           editorState
	depsPaths        []string
	detailsID        string
	deleteID         string
	upstreamDelete   upstreamDeletePlan
	deleteCursor     int
	migrationImpact  []string
	migrationOptions []upstreamDeleteOption
	migrationCursor  int
	migrationBlocked string
	migrationConfirm bool
	restoreID        string
	lastIntent       resourcepage.Intent
	forcedState      resourcepage.State
	forceState       bool
	statusExtra      string
	advancedUnlocked bool
}

func (p *Page) Init() tea.Cmd { return nil }

func (p *Page) Kind() Kind { return p.kind }

func (p *Page) SetKind(kind Kind) {
	p.kind = kind
	p.advancedUnlocked = false
	p.CancelOverlay()
	p.rebuildTable()
	p.Refresh()
	if p.kind == KindTargets || p.kind == KindEndpoints || p.kind == KindCredentials {
		p.SetStatus("Managed members are read-only · u unlock")
	}
}

func (p *Page) managedMemberLocked(id string) bool {
	if p.advancedUnlocked || p.draft == nil || id == "" {
		return false
	}
	var kind generated.ManagedResourceRefKind
	switch p.kind {
	case KindTargets:
		kind = generated.ManagedResourceRefKindTarget
	case KindEndpoints:
		kind = generated.ManagedResourceRefKindEndpoint
	case KindCredentials:
		kind = generated.ManagedResourceRefKindCredential
	default:
		return false
	}
	_, owned := managedconfig.OwnerOf(p.draft.LocalCommand(), kind, id)
	return owned
}

func (p *Page) SetSize(width, height int) {
	p.width = max(1, width)
	p.height = max(8, height)
	if p.tablePage != nil {
		p.tablePage.SetSize(p.width, p.tableHeight())
	}
}

func (p *Page) State() resourcepage.State {
	if p.forceState {
		return p.forcedState
	}
	if p.tablePage == nil {
		return resourcepage.StateLoading
	}
	return p.tablePage.State()
}

func (p *Page) SetState(state resourcepage.State) {
	p.forceState = true
	p.forcedState = state
	if p.tablePage != nil {
		p.tablePage.SetState(state)
	}
}

func (p *Page) SetStatus(status string) {
	p.statusExtra = status
	if p.tablePage != nil {
		p.tablePage.SetStatus(status)
	}
}

func (p *Page) LastIntent() resourcepage.Intent { return p.lastIntent }

func (p *Page) Table() *resourceview.Model {
	if p.tablePage == nil {
		return nil
	}
	return p.tablePage.Table()
}

func (p *Page) StripHeight() int { return stripHeight() }

func (p *Page) BannerOffset() int {
	if p.tablePage == nil {
		return 0
	}
	lines := 0
	switch p.tablePage.State() {
	case resourcepage.StateDisconnected, resourcepage.StateEmpty, resourcepage.StateLoading,
		resourcepage.StateValidationError, resourcepage.StatePublicationError, resourcepage.StateStale:
		lines++
	}
	// Match resourcepage.overlayLines: status line is an additional overlay row.
	if p.statusExtra != "" {
		lines++
	}
	return lines
}

func (p *Page) TableHeaderY() int { return 1 }

func (p *Page) VisibleIDs() []string {
	if p.tablePage == nil {
		return nil
	}
	return p.tablePage.Table().VisibleRowIDs()
}

func (p *Page) SelectedID() string {
	if p.tablePage == nil {
		return ""
	}
	return p.tablePage.SelectedID()
}

func (p *Page) SelectID(id string) {
	if p.tablePage == nil {
		return
	}
	ids := p.tablePage.Table().VisibleRowIDs()
	for i, candidate := range ids {
		if candidate == id {
			p.tablePage.Update(tea.KeyMsg{Type: tea.KeyHome})
			for j := 0; j < i; j++ {
				p.tablePage.Update(tea.KeyMsg{Type: tea.KeyDown})
			}
			return
		}
	}
}

func (p *Page) ShowingDetails() bool { return p.overlay == overlayDetails }

func (p *Page) ensureStager() *secretinput.Stager {
	if p.stager == nil {
		p.stager = secretinput.NewStager(p.client)
	}
	return p.stager
}

func (p *Page) OwnedStageID() string {
	return p.ensureStager().OwnedStageID()
}

func (p *Page) TokenBufferLen() int {
	if p.editor.tokenBuf == nil {
		return 0
	}
	return p.editor.tokenBuf.Len()
}

func (p *Page) EditorSecretMode() SecretActionMode { return p.editor.secretMode }

func (p *Page) EditorFocusesToken() bool {
	name := p.editor.focusedName()
	return p.editor.focusToken || name == "token" || name == "api_key"
}

func (p *Page) EditorIDEditable() bool       { return p.editor.idEditable() }
func (p *Page) EditorProviderEditable() bool { return p.editor.providerEditable() }
func (p *Page) EditorFieldNames() []string   { return p.editor.fieldNames() }

func (p *Page) SetSecretMode(mode SecretActionMode) {
	if p.replaceRequiredLocked() && mode != SecretActionReplace {
		p.editor.err = "$.secret_action: replace required"
		p.editor.secretMode = SecretActionReplace
		p.editor.focusToken = true
		return
	}
	p.editor.setSecretMode(mode)
}

// HasUnstaged reports whether the open secret editor holds unstaged token bytes.
// Hosts must query this before global Publish (resource-ui.md).
func (p *Page) HasUnstaged() bool {
	return p.editor.tokenBuf != nil && p.editor.tokenBuf.HasUnstaged()
}

func (p *Page) replaceRequiredLocked() bool {
	if p.kind != KindCredentials || p.editor.mode != editorEdit {
		return false
	}
	id := p.editor.id
	if id == "" {
		id = strings.TrimSpace(p.editor.values["id"])
	}
	return containsString(p.draft.ReplaceRequiredIDs(), id)
}

// releaseEditorSecrets Discards owned stages and zeros the token buffer before
// the editor state is replaced. Safe when no editor is open.
// DELETE failures keep ownership and set cleanup status for host retry.
func (p *Page) releaseEditorSecrets() {
	if p.editor.tokenBuf == nil && p.OwnedStageID() == "" {
		return
	}
	res := p.ensureStager().Discard(context.Background(), p.editor.tokenBuf)
	if p.editor.tokenBuf != nil {
		p.editor.tokenBuf.Zero()
	}
	if !res.OK() {
		p.SetStatus("stage_cleanup_pending: " + string(res.Code))
	}
}

func (p *Page) PasteTokenBytes(raw []byte) error {
	if p.editor.tokenBuf == nil {
		p.editor.tokenBuf = secretinput.New()
	}
	p.editor.secretMode = SecretActionReplace
	p.editor.focusToken = true
	return p.editor.tokenBuf.PasteBytes(raw)
}

func (p *Page) StageToken(ctx context.Context) error {
	stager := p.ensureStager()
	if p.editor.tokenBuf == nil {
		return fmt.Errorf("%s", secretinput.CodeEmpty)
	}
	res := stager.Stage(ctx, p.editor.tokenBuf)
	if !res.OK() {
		return fmt.Errorf("%s", res.Code)
	}
	return nil
}

func (p *Page) CancelOverlay() {
	cleanupPending := ""
	if p.overlay == overlayEditor {
		res := p.ensureStager().Discard(context.Background(), p.editor.tokenBuf)
		if !res.OK() {
			cleanupPending = "stage_cleanup_pending: " + string(res.Code)
		}
	} else if p.overlay == overlayConfirmDelete && strings.Contains(p.statusExtra, "stage_cleanup_pending") {
		// Mirror editor cancel: retain cleanup status written by discard failure.
		cleanupPending = p.statusExtra
	}
	p.overlay = overlayNone
	p.depsPaths = nil
	p.detailsID = ""
	p.deleteID = ""
	p.upstreamDelete = upstreamDeletePlan{}
	p.deleteCursor = 0
	p.migrationImpact = nil
	p.migrationOptions = nil
	p.migrationCursor = 0
	p.migrationBlocked = ""
	p.migrationConfirm = false
	p.restoreID = ""
	p.editor = editorState{}
	p.forceState = false
	p.forcedState = ""
	if cleanupPending != "" {
		p.SetStatus(cleanupPending)
	} else {
		p.SetStatus("")
	}
	p.Refresh()
}

func (p *Page) BeginCreate() {
	if p.draft.Disconnected() {
		p.SetStatus("Disconnected: editing is unavailable")
		return
	}
	p.releaseEditorSecrets()
	switch p.kind {
	case KindUpstreams:
		p.overlay = overlayEditor
		p.editor = newUpstreamEditor(editorCreate, "", p.draft, p.status)
		p.lastIntent = resourcepage.IntentCreate
	case KindLimitPolicies:
		p.overlay = overlayEditor
		p.editor = newLimitPolicyEditor(editorCreate, "", p.draft)
		p.lastIntent = resourcepage.IntentCreate
	case KindCredentials:
		p.overlay = overlayEditor
		p.editor = newCredentialEditor(editorCreate, "", p.draft)
		p.lastIntent = resourcepage.IntentCreate
	case KindEndpoints:
		p.overlay = overlayEditor
		p.editor = newEndpointEditor(editorCreate, "", p.draft)
		p.lastIntent = resourcepage.IntentCreate
	case KindTargets:
		p.overlay = overlayEditor
		p.editor = newTargetEditor(editorCreate, "", p.draft)
		p.lastIntent = resourcepage.IntentCreate
	}
}

func (p *Page) BeginEdit() {
	if p.draft.Disconnected() {
		p.SetStatus("Disconnected: editing is unavailable")
		return
	}
	id := p.SelectedID()
	if id == "" {
		return
	}
	if p.managedMemberLocked(id) {
		p.SetStatus("This resource belongs to a managed object · press u to unlock advanced editing")
		return
	}
	p.releaseEditorSecrets()
	switch p.kind {
	case KindUpstreams:
		upstream, ok := findSimpleUpstream(p.draft, id, p.status)
		if !ok || !upstream.Editable {
			p.SetStatus("Custom internal configuration · press R to preview restoring the simple structure")
			return
		}
		p.overlay = overlayEditor
		p.editor = newUpstreamEditor(editorEdit, id, p.draft, p.status)
		p.lastIntent = resourcepage.IntentEdit
	case KindLimitPolicies:
		p.overlay = overlayEditor
		p.editor = newLimitPolicyEditor(editorEdit, id, p.draft)
		p.lastIntent = resourcepage.IntentEdit
	case KindCredentials:
		p.overlay = overlayEditor
		p.editor = newCredentialEditor(editorEdit, id, p.draft)
		if containsString(p.draft.ReplaceRequiredIDs(), id) {
			p.editor.secretMode = SecretActionReplace
			p.editor.focusToken = true
			p.editor.replaceRequired = true
		}
		p.lastIntent = resourcepage.IntentEdit
	case KindEndpoints:
		p.overlay = overlayEditor
		p.editor = newEndpointEditor(editorEdit, id, p.draft)
		p.lastIntent = resourcepage.IntentEdit
	case KindTargets:
		p.overlay = overlayEditor
		p.editor = newTargetEditor(editorEdit, id, p.draft)
		p.lastIntent = resourcepage.IntentEdit
	}
}

func (p *Page) OpenDetails() {
	id := p.SelectedID()
	if id == "" {
		return
	}
	p.overlay = overlayDetails
	p.detailsID = id
	p.lastIntent = resourcepage.IntentDetails
}

func (p *Page) SetEditorValues(values map[string]string) {
	p.editor.setValues(values)
}

func (p *Page) SaveEditor() error {
	if p.overlay != overlayEditor {
		return fmt.Errorf("editor not open")
	}
	if p.editor.mode == editorCreate && p.kind != KindUpstreams && p.kind != KindLimitPolicies && strings.TrimSpace(p.editor.values["id"]) == "" {
		p.editor.values["id"] = formui.UniqueID(p.editor.values["name"], targetIDPrefix(p.kind), formui.UsedConfigIDs(p.draft.LocalCommand()))
	}
	if p.kind == KindUpstreams && p.beginMigrationConfirmation() {
		return nil
	}
	var err error
	switch p.kind {
	case KindUpstreams:
		err = p.saveUpstreamEditor()
	case KindLimitPolicies:
		err = saveLimitPolicy(p.draft, p.editor.mode, p.editor.id, p.editor.values)
		if err != nil {
			p.setEditorValidation(err)
		} else {
			p.closeEditorSuccess()
		}
	case KindCredentials:
		err = p.saveCredentialEditor()
	case KindEndpoints:
		err = p.saveEndpointEditor()
	case KindTargets:
		err = p.saveTargetEditor()
	default:
		return fmt.Errorf("editor not open")
	}
	if err == nil {
		p.lastIntent = resourcepage.IntentPublish
	}
	return err
}

func targetIDPrefix(kind Kind) string {
	switch kind {
	case KindCredentials:
		return "credential"
	case KindEndpoints:
		return "endpoint"
	default:
		return "target"
	}
}

func (p *Page) saveUpstreamEditor() error {
	creating := p.editor.mode == editorCreate
	hasToken := p.editor.tokenBuf != nil && p.editor.tokenBuf.HasUnstaged()
	if err := validateUpstreamValues(p.editor.values, creating, hasToken); err != nil {
		p.setEditorValidation(err)
		return err
	}

	var current simpleUpstream
	protocolChanged := false
	if !creating {
		var ok bool
		current, ok = findSimpleUpstream(p.draft, p.editor.id, p.status)
		if !ok || !current.Editable {
			err := fmt.Errorf("$.upstream: advanced configuration is read-only")
			p.setEditorValidation(err)
			return err
		}
		loaded := newUpstreamEditor(editorEdit, p.editor.id, p.draft, p.status)
		protocolChanged = loaded.values["api_formats"] != p.editor.values["api_formats"]
	}

	if creating || current.ObjectID != "" {
		stages := map[generated.MutableCredentialCommandProvider]string{}
		stagers := []*secretinput.Stager{}
		if hasToken {
			var err error
			stages, stagers, err = p.stageUpstreamSecrets(context.Background(), parseUpstreamFormats(p.editor.values))
			if err != nil {
				p.setEditorValidation(err)
				return err
			}
		}
		cleanupStages := func() {
			for _, stager := range stagers {
				_ = stager.Discard(context.Background(), nil)
			}
		}
		if creating {
			if !hasToken {
				err := fmt.Errorf("$.api_key: required")
				p.setEditorValidation(err)
				return err
			}
			if _, _, err := createManagedUpstream(p.draft, p.editor.values, stages); err != nil {
				cleanupStages()
				p.setEditorValidation(err)
				return err
			}
		} else if protocolChanged {
			if err := migrateManagedUpstream(p.draft, current, p.editor.values, stages); err != nil {
				cleanupStages()
				p.setEditorValidation(err)
				return err
			}
		} else {
			if err := editSimpleUpstream(p.draft, current, p.editor.values); err != nil {
				cleanupStages()
				p.setEditorValidation(err)
				return err
			}
			if hasToken {
				for _, credentialID := range current.CredentialIDs {
					credential, ok := findCredential(p.draft.LocalCommand(), credentialID)
					if !ok {
						continue
					}
					stageID := stages[credential.Provider]
					if stageID == "" {
						continue
					}
					if err := p.discardPreviousDraftReplace(credentialID, stageID); err != nil {
						cleanupStages()
						return err
					}
					p.draft.SetCredentialReplace(credentialID, stageID)
				}
			}
		}
		for _, stager := range stagers {
			stager.ClearOwnership()
		}
		if p.editor.tokenBuf != nil {
			p.editor.tokenBuf.Zero()
		}
		p.closeEditorSuccess()
		return nil
	}

	stageID := ""
	if hasToken {
		if err := p.StageToken(context.Background()); err != nil {
			p.setEditorValidation(err)
			return err
		}
		stageID = p.OwnedStageID()
	}
	if creating {
		if stageID == "" {
			err := fmt.Errorf("$.api_key: required")
			p.setEditorValidation(err)
			return err
		}
		if err := createSimpleUpstream(p.draft, p.editor.values, stageID); err != nil {
			p.setEditorValidation(err)
			return err
		}
		p.ensureStager().ClearOwnership()
	} else if protocolChanged {
		if _, err := createSimpleProtocolBinding(p.draft, current, p.editor.values, stageID); err != nil {
			p.setEditorValidation(err)
			return err
		}
		if stageID != "" {
			p.ensureStager().ClearOwnership()
		}
	} else {
		if stageID != "" {
			if err := p.discardPreviousDraftReplace(current.CredentialID, stageID); err != nil {
				return err
			}
		}
		if err := editSimpleUpstream(p.draft, current, p.editor.values); err != nil {
			p.setEditorValidation(err)
			return err
		}
		if stageID != "" {
			p.draft.SetCredentialReplace(current.CredentialID, stageID)
			p.ensureStager().ClearOwnership()
		}
	}
	if p.editor.tokenBuf != nil {
		p.editor.tokenBuf.Zero()
	}
	p.closeEditorSuccess()
	return nil
}

func (p *Page) stageUpstreamSecrets(
	ctx context.Context,
	formats []managedconfig.Format,
) (map[generated.MutableCredentialCommandProvider]string, []*secretinput.Stager, error) {
	if p.editor.tokenBuf == nil || !p.editor.tokenBuf.HasUnstaged() {
		return nil, nil, fmt.Errorf("$.api_key: required")
	}
	providers := []generated.MutableCredentialCommandProvider{}
	seen := map[generated.MutableCredentialCommandProvider]bool{}
	for _, format := range formats {
		provider, err := providerForFormat(format)
		if err != nil {
			return nil, nil, err
		}
		if !seen[provider] {
			seen[provider] = true
			providers = append(providers, provider)
		}
	}
	raw := make([]byte, p.editor.tokenBuf.Len())
	p.editor.tokenBuf.CopyBytes(raw)
	defer func() {
		for i := range raw {
			raw[i] = 0
		}
	}()
	stages := make(map[generated.MutableCredentialCommandProvider]string, len(providers))
	stagers := make([]*secretinput.Stager, 0, len(providers))
	for _, provider := range providers {
		buf := secretinput.New()
		if err := buf.PasteBytes(raw); err != nil {
			for _, stager := range stagers {
				_ = stager.Discard(ctx, nil)
			}
			return nil, nil, err
		}
		stager := secretinput.NewStager(p.client)
		result := stager.Stage(ctx, buf)
		if !result.OK() {
			for _, staged := range stagers {
				_ = staged.Discard(ctx, nil)
			}
			return nil, nil, fmt.Errorf("$.api_key: %s", result.Code)
		}
		stages[provider] = result.StageID
		stagers = append(stagers, stager)
	}
	return stages, stagers, nil
}

func (p *Page) saveTargetEditor() error {
	creating := p.editor.mode == editorCreate
	if err := validateTargetValues(p.editor.values, creating, p.draft); err != nil {
		p.setEditorValidation(err)
		return err
	}
	var err error
	if creating {
		err = applyTargetCreate(p.draft, p.editor.values)
	} else {
		err = applyTargetEdit(p.draft, p.editor.id, p.editor.values)
	}
	if err != nil {
		p.setEditorValidation(err)
		return err
	}
	p.closeEditorSuccess()
	return nil
}

func (p *Page) saveEndpointEditor() error {
	creating := p.editor.mode == editorCreate
	if err := validateEndpointValues(p.editor.values, creating); err != nil {
		p.setEditorValidation(err)
		return err
	}
	var err error
	if creating {
		err = applyEndpointCreate(p.draft, p.editor.values)
	} else {
		err = applyEndpointEdit(p.draft, p.editor.id, p.editor.values)
	}
	if err != nil {
		p.setEditorValidation(err)
		return err
	}
	p.closeEditorSuccess()
	return nil
}

func (p *Page) saveCredentialEditor() error {
	creating := p.editor.mode == editorCreate
	mode := p.editor.secretMode
	if err := validateCredentialValues(p.editor.values, creating, mode); err != nil {
		p.setEditorValidation(err)
		return err
	}
	id := strings.TrimSpace(p.editor.values["id"])
	if !creating {
		id = p.editor.id
	}
	if !creating && containsString(p.draft.ReplaceRequiredIDs(), id) && mode != SecretActionReplace {
		err := fmt.Errorf("$.secret_action: replace required")
		p.setEditorValidation(err)
		return err
	}

	switch mode {
	case SecretActionReplace:
		stageID := p.OwnedStageID()
		if p.editor.tokenBuf != nil && p.editor.tokenBuf.HasUnstaged() {
			if err := p.StageToken(context.Background()); err != nil {
				p.setEditorValidation(err)
				return err
			}
			stageID = p.OwnedStageID()
		}
		// Meta-only Save while draft already holds replace(stage_id): reuse it
		// so Rename does not require a re-paste and does not DELETE the pending stage.
		// Never reuse under replace_required — draft may still hold a dead stage_id,
		// and SetCredentialReplace would clear the publish gate (N-09).
		if stageID == "" && !creating && !containsString(p.draft.ReplaceRequiredIDs(), id) && !p.replaceRequiredLocked() {
			stageID = replaceStageIDFromDraft(p.draft, id)
		}
		if stageID == "" {
			err := fmt.Errorf("$.secret_action.stage_id: required")
			p.setEditorValidation(err)
			return err
		}
		// Superseding a draft replace(stage_id) must DELETE the previous stage
		// so DiscardOwnedStages cannot miss orphans.
		if err := p.discardPreviousDraftReplace(id, stageID); err != nil {
			return err
		}
		if creating {
			action, err := buildReplaceAction(stageID)
			if err != nil {
				p.setEditorValidation(err)
				return err
			}
			if err := applyCredentialCreate(p.draft, p.editor.values, action); err != nil {
				p.setEditorValidation(err)
				return err
			}
			p.draft.SetCredentialReplace(id, stageID)
		} else {
			if err := applyCredentialEditMeta(p.draft, id, p.editor.values); err != nil {
				p.setEditorValidation(err)
				return err
			}
			p.draft.SetCredentialReplace(id, stageID)
		}
		p.ensureStager().ClearOwnership()
	case SecretActionExternal:
		ref, err := buildExternalRef(p.editor.values)
		if err != nil {
			p.setEditorValidation(err)
			return err
		}
		var action generated.CredentialSecretAction
		if err := action.FromCredentialSecretAction2(generated.CredentialSecretAction2{
			Mode: generated.CredentialSecretActionExternalRef,
			Ref:  ref,
		}); err != nil {
			p.setEditorValidation(err)
			return err
		}
		// Discard leftovers before mutate so Discard failure cannot leave a
		// partial Preserve/External draft commit.
		if err := p.discardLeftoverStagesBeforeSecretMutate(id); err != nil {
			return err
		}
		if creating {
			if err := applyCredentialCreate(p.draft, p.editor.values, action); err != nil {
				p.setEditorValidation(err)
				return err
			}
		} else {
			if err := applyCredentialExternal(p.draft, id, p.editor.values); err != nil {
				p.setEditorValidation(err)
				return err
			}
		}
	case SecretActionPreserve:
		if creating {
			err := fmt.Errorf("$.secret_action: preserve unavailable on create")
			p.setEditorValidation(err)
			return err
		}
		if err := p.discardLeftoverStagesBeforeSecretMutate(id); err != nil {
			return err
		}
		if err := applyCredentialPreserve(p.draft, id, p.editor.values); err != nil {
			p.setEditorValidation(err)
			return err
		}
	default:
		err := fmt.Errorf("$.secret_action.mode: required")
		p.setEditorValidation(err)
		return err
	}

	if p.editor.tokenBuf != nil {
		p.editor.tokenBuf.Zero()
	}
	p.closeEditorSuccess()
	return nil
}

func (p *Page) closeEditorSuccess() {
	p.overlay = overlayNone
	p.editor = editorState{}
	p.migrationImpact = nil
	p.migrationOptions = nil
	p.migrationCursor = 0
	p.migrationBlocked = ""
	p.migrationConfirm = false
	p.forceState = false
	p.forcedState = ""
	p.SetStatus("")
	p.Refresh()
}

func (p *Page) setEditorValidation(err error) {
	p.editor.err = err.Error()
	p.forceState = true
	p.forcedState = resourcepage.StateValidationError
	if p.tablePage != nil {
		p.tablePage.SetState(resourcepage.StateValidationError)
	}
	p.SetStatus(err.Error())
}

func (p *Page) TryDelete() (bool, []string) {
	id := p.SelectedID()
	if id == "" {
		return false, nil
	}
	if p.managedMemberLocked(id) {
		p.SetStatus("This resource belongs to a managed object · press u to unlock advanced editing")
		return true, []string{"managed object ownership"}
	}
	p.beginDeleteIntent()
	paths := InboundRefs(p.draft, p.kind, id)
	if p.kind == KindUpstreams && p.overlay == overlayUpstreamDelete {
		return false, nil
	}
	if len(paths) > 0 {
		return true, paths
	}
	// Unblocked deletes require the same confirm overlay as keyboard `d`.
	return false, nil
}

func (p *Page) discardEditorStageOrFail() error {
	res := p.ensureStager().Discard(context.Background(), p.editor.tokenBuf)
	if res.OK() {
		return nil
	}
	msg := "stage_cleanup_pending: " + string(res.Code)
	p.SetStatus(msg)
	p.setEditorValidation(fmt.Errorf("%s", msg))
	return fmt.Errorf("%s", res.Code)
}

// discardPreviousDraftReplace DELETE's a draft-held replace stage when a new
// replace stageID will supersede it. No-op when absent or identical.
func (p *Page) discardPreviousDraftReplace(credentialID, newStageID string) error {
	prev := replaceStageIDFromDraft(p.draft, credentialID)
	if prev == "" || prev == newStageID {
		return nil
	}
	res := p.ensureStager().DiscardStageID(context.Background(), prev)
	if res.OK() {
		return nil
	}
	msg := "stage_cleanup_pending: " + string(res.Code)
	p.SetStatus(msg)
	p.setEditorValidation(fmt.Errorf("%s", msg))
	return fmt.Errorf("%s", res.Code)
}

// discardDraftReplaceBeforeDelete DELETE's a draft-held replace stage before
// the credential row is removed. Fail-closed: callers must not applyDelete
// when this returns a non-nil error (stage-loss codes are OK via DiscardStageID).
func (p *Page) discardDraftReplaceBeforeDelete(credentialID string) error {
	if p.kind == KindUpstreams {
		object, ok := managedconfig.FindObject(p.draft.LocalCommand(), credentialID)
		if !ok {
			return nil
		}
		for _, id := range managedconfig.Members(object, generated.ManagedResourceRefKindCredential) {
			if err := p.discardDraftReplaceIDBeforeDelete(id); err != nil {
				return err
			}
		}
		return nil
	}
	if p.kind != KindCredentials {
		return nil
	}
	return p.discardDraftReplaceIDBeforeDelete(credentialID)
}

func (p *Page) discardDraftReplaceIDBeforeDelete(credentialID string) error {
	prev := replaceStageIDFromDraft(p.draft, credentialID)
	if prev == "" {
		return nil
	}
	res := p.ensureStager().DiscardStageID(context.Background(), prev)
	if res.OK() {
		return nil
	}
	msg := "stage_cleanup_pending: " + string(res.Code)
	p.SetStatus(msg)
	return fmt.Errorf("%s", res.Code)
}

// discardLeftoverStagesBeforeSecretMutate removes owned and draft-held replace
// stages before Preserve/External mutation. Failure keeps draft unchanged.
func (p *Page) discardLeftoverStagesBeforeSecretMutate(credentialID string) error {
	prev := replaceStageIDFromDraft(p.draft, credentialID)
	if err := p.discardEditorStageOrFail(); err != nil {
		return err
	}
	if prev == "" {
		return nil
	}
	// Owned discard may have already DELETE'd prev when owned == prev.
	res := p.ensureStager().DiscardStageID(context.Background(), prev)
	if res.OK() {
		return nil
	}
	msg := "stage_cleanup_pending: " + string(res.Code)
	p.SetStatus(msg)
	p.setEditorValidation(fmt.Errorf("%s", msg))
	return fmt.Errorf("%s", res.Code)
}

func (p *Page) beginDeleteIntent() {
	id := p.SelectedID()
	if id == "" {
		return
	}
	if p.managedMemberLocked(id) {
		p.SetStatus("This resource belongs to a managed object · press u to unlock advanced editing")
		return
	}
	p.deleteID = id
	p.lastIntent = resourcepage.IntentDelete
	if p.kind == KindUpstreams {
		plan := planUpstreamDelete(p.draft.LocalCommand(), id)
		p.upstreamDelete = plan
		p.deleteCursor = 0
		if len(plan.CustomBlockers) > 0 {
			p.overlay = overlayDeps
			p.depsPaths = append([]string(nil), plan.CustomBlockers...)
			return
		}
		if len(plan.Uses) > 0 {
			if plan.needsReplacement() && len(plan.Options) == 0 {
				p.overlay = overlayDeps
				p.depsPaths = []string{"Add another compatible upstream before deleting this primary upstream"}
				return
			}
			p.overlay = overlayUpstreamDelete
			p.depsPaths = nil
			return
		}
	}
	paths := InboundRefs(p.draft, p.kind, id)
	if len(paths) > 0 {
		p.overlay = overlayDeps
		p.depsPaths = paths
		return
	}
	p.overlay = overlayConfirmDelete
	p.depsPaths = nil
}

func (p *Page) NoteStageLoss(credentialID string, code secretinput.Code) {
	lostStageID := replaceStageIDFromDraft(p.draft, credentialID)
	p.draft.MarkReplaceRequired(credentialID)
	sameEditor := p.overlay == overlayEditor && p.editor.id == credentialID
	if lostStageID != "" {
		// Clear only when owned matches the lost draft stage; keep a newer restage.
		p.ensureStager().NoteLoss(lostStageID, code)
	} else if sameEditor && p.OwnedStageID() != "" {
		// Pre-save editor stage loss for this credential: Discard (idempotent DELETE).
		_ = p.ensureStager().Discard(context.Background(), p.editor.tokenBuf)
	}
	if !sameEditor {
		p.SelectID(credentialID)
		p.BeginEdit() // releaseEditorSecrets Discards any leftover owned stage
	} else if p.editor.tokenBuf != nil {
		p.editor.tokenBuf.Zero()
	}
	p.editor.secretMode = SecretActionReplace
	p.editor.focusToken = true
	p.editor.replaceRequired = true
	p.editor.err = string(code)
	p.SetStatus("replace_required: " + string(code))
	p.Refresh()
}

func (p *Page) HandleOperationUnknown(outcome OperationUnknownOutcome, submittedReplaceIDs []string) {
	switch outcome {
	case OperationUnknownSuccess:
		p.forceState = false
		p.Refresh()
	case OperationUnknownUnchanged:
		for _, id := range submittedReplaceIDs {
			lost := replaceStageIDFromDraft(p.draft, id)
			p.draft.MarkReplaceRequired(id)
			if lost != "" {
				p.ensureStager().NoteLoss(lost, secretinput.CodeNotFound)
			}
		}
		// Do not ClearOwnership unconditionally — a restaged live ID must stay tracked.
		p.SetState(resourcepage.StatePublicationError)
		p.SetStatus("operation_unknown: replace_required")
		p.Refresh()
	case OperationUnknownConflict:
		p.SetState(resourcepage.StatePublicationError)
		p.SetStatus("operation_unknown: conflict")
	}
}

func (p *Page) ApplyCleanupPending() {
	p.draft.SetMutationStatus(generated.ConfigMutationStatusSecretCleanupPending)
	p.SetState(resourcepage.StatePublicationError)
	p.SetStatus("secret_cleanup_pending")
	p.Refresh()
}

func (p *Page) ApplyConflictState() {
	p.SetState(resourcepage.StatePublicationError)
	p.SetStatus("generation conflict")
	p.Refresh()
}

// DiscardOwnedStages deletes stager-owned and draft-held replace stages.
// DELETE failures are returned and surfaced via status; failed IDs remain
// tracked (owned) or still present on the draft for host retry.
func (p *Page) DiscardOwnedStages(ctx context.Context) error {
	var first error
	res := p.ensureStager().Discard(ctx, p.editor.tokenBuf)
	if !res.OK() && first == nil {
		first = fmt.Errorf("%s", res.Code)
	}
	// Also destroy draft-held replace(stage_id) stages (Cancel/Discard contract).
	seen := map[string]struct{}{}
	for _, c := range p.draft.LocalCommand().Credentials {
		replace, err := c.SecretAction.AsCredentialSecretAction1()
		if err != nil || replace.Mode != generated.CredentialSecretActionReplace {
			continue
		}
		id := string(replace.StageId)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		del := p.ensureStager().DiscardStageID(ctx, id)
		if !del.OK() && first == nil {
			first = fmt.Errorf("%s", del.Code)
		}
	}
	if first != nil {
		p.SetStatus("stage_cleanup_pending: " + first.Error())
	}
	return first
}

func (p *Page) Refresh() {
	if p.tablePage == nil {
		p.rebuildTable()
	}
	rows := rowsFor(p.draft, p.kind, p.status)
	p.tablePage.SetRows(rows)
	p.tablePage.SetDirty(p.draft.DomainDirty(configdraft.DomainTargets))

	if p.forceState {
		p.tablePage.SetState(p.forcedState)
		if p.statusExtra != "" {
			p.tablePage.SetStatus(p.statusExtra)
		}
		return
	}
	if p.draft.Disconnected() {
		p.tablePage.SetState(resourcepage.StateDisconnected)
		return
	}
	if len(rows) == 0 {
		p.tablePage.SetState(resourcepage.StateEmpty)
		return
	}
	p.tablePage.SetState(resourcepage.StateSuccess)
}

func (p *Page) rebuildTable() {
	scope := p.scope
	if scope == "" {
		scope = "all"
	}
	actions := resourcepage.ActionSet{
		Create: true, Edit: true, Delete: true, Publish: false, Details: true, Filter: true, Mark: true,
	}
	if p.kind == KindUpstreams {
		actions.Details = true
	}
	p.tablePage = resourcepage.New(resourcepage.Spec{
		Title:   p.kind.Title(),
		Scope:   scope,
		Columns: columnsFor(p.kind),
		Actions: actions,
		Domain:  string(configdraft.DomainTargets),
	})
	p.tablePage.SetSize(p.width, p.tableHeight())
}

func (p *Page) tableHeight() int {
	return max(6, p.height-stripHeight())
}

func (p *Page) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	p.lastIntent = resourcepage.IntentNone
	if msg == nil {
		return p, nil
	}

	if key, ok := msg.(tea.KeyMsg); ok {
		if p.overlay != overlayNone {
			return p.updateOverlayKey(key)
		}
		if (p.kind == KindTargets || p.kind == KindEndpoints || p.kind == KindCredentials) && key.String() == "u" &&
			(p.tablePage == nil || !p.tablePage.Table().Filtering()) {
			p.advancedUnlocked = !p.advancedUnlocked
			if p.advancedUnlocked {
				p.SetStatus("Advanced editing unlocked for this page")
			} else {
				p.SetStatus("Managed members are read-only · u unlock")
			}
			return p, nil
		}
		if p.kind == KindUpstreams && key.String() == "R" && (p.tablePage == nil || !p.tablePage.Table().Filtering()) {
			id := p.SelectedID()
			if upstream, ok := findSimpleUpstream(p.draft, id, p.status); ok && !upstream.Editable {
				if _, err := previewRestoreUpstream(p.draft.LocalCommand(), upstream.ObjectID); err != nil {
					p.SetStatus("Cannot restore simple structure: " + err.Error())
					return p, nil
				}
				p.restoreID = upstream.ObjectID
				p.overlay = overlayRestoreUpstream
				return p, nil
			}
		}
		switch {
		case key.Alt && key.Type == tea.KeyRight:
			p.shiftKind(1)
			return p, nil
		case key.Alt && key.Type == tea.KeyLeft:
			p.shiftKind(-1)
			return p, nil
		}
	}

	if mouse, ok := msg.(tea.MouseMsg); ok {
		if p.overlay != overlayNone {
			if mouse.Action == tea.MouseActionPress && mouse.Button == tea.MouseButtonLeft {
				y := mouse.Y - stripHeight()
				footerY := p.height - stripHeight() - 1
				switch p.overlay {
				case overlayRestoreUpstream:
					if y == footerY {
						if mouse.X < p.width/2 {
							return p.updateOverlayKey(tea.KeyMsg{Type: tea.KeyEnter})
						}
						p.CancelOverlay()
					}
					return p, nil
				case overlayUpstreamDelete:
					optionStart := 2 + len(p.upstreamDelete.Uses)
					if option := y - optionStart; option >= 0 && option < len(p.upstreamDelete.Options) {
						p.deleteCursor = option
						return p, nil
					}
					if y == footerY {
						if mouse.X < p.width/2 {
							return p.updateOverlayKey(tea.KeyMsg{Type: tea.KeyEnter})
						}
						p.CancelOverlay()
					}
					return p, nil
				case overlayMigrationConfirm:
					optionStart := 3 + len(p.migrationImpact)
					if option := y - optionStart; option >= 0 && option < len(p.migrationOptions) {
						p.migrationCursor = option
						return p, nil
					}
					if y == footerY {
						if mouse.X < p.width/2 {
							return p.updateOverlayKey(tea.KeyMsg{Type: tea.KeyEnter})
						}
						p.overlay = overlayEditor
					}
					return p, nil
				case overlayEditor:
					layout := p.editor.formLayout(p.width, p.height-stripHeight())
					if y == layout.FooterLine {
						if mouse.X < p.width/2 {
							_ = p.SaveEditor()
						} else {
							p.CancelOverlay()
						}
						return p, nil
					}
					for name, line := range layout.FieldLines {
						if line == y {
							next := indexOfEditorField(p.editor.fieldNames(), name)
							if next != p.editor.cursor {
								p.editor.closeSelector()
							}
							p.editor.cursor = next
							if p.editor.focusedIsToggle() {
								p.editor.toggleBool(p.editor.focusedName())
								return p, nil
							}
							if p.editor.focusedIsSelector() {
								p.editor.openSelector()
							}
							return p, nil
						}
					}
					if option, ok := layout.OptionLines[y]; ok {
						p.editor.chooseOption(option)
					}
				}
			}
			return p, nil
		}
		if kind, hit := hitTestStrip(mouse.X, mouse.Y, p.width); hit && mouse.Action == tea.MouseActionPress && mouse.Button == tea.MouseButtonLeft {
			p.SetKind(kind)
			return p, nil
		}
		adjusted := mouse
		adjusted.Y -= stripHeight()
		intent, _ := p.tablePage.Update(adjusted)
		return p.handleIntent(intent)
	}

	intent, consumed := p.tablePage.Update(msg)
	if consumed {
		return p.handleIntent(intent)
	}
	return p, nil
}

func indexOfEditorField(fields []string, name string) int {
	for index, field := range fields {
		if field == name {
			return index
		}
	}
	return 0
}

func (p *Page) handleIntent(intent resourcepage.Intent) (tea.Model, tea.Cmd) {
	p.lastIntent = intent
	switch intent {
	case resourcepage.IntentCreate:
		p.BeginCreate()
	case resourcepage.IntentEdit:
		p.BeginEdit()
	case resourcepage.IntentDelete:
		p.beginDeleteIntent()
	case resourcepage.IntentDetails:
		p.OpenDetails()
	case resourcepage.IntentPublish:
		if p.draft.Disconnected() || !p.draft.CanPublish() {
			p.lastIntent = resourcepage.IntentNone
		}
		if p.HasUnstaged() {
			p.lastIntent = resourcepage.IntentNone
			p.SetStatus("publish blocked: unstaged secret")
		}
	}
	return p, nil
}

func (p *Page) updateOverlayKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch p.overlay {
	case overlayEditor:
		p.handleEditorKey(key)
	case overlayDetails, overlayDeps:
		if key.String() == "esc" {
			p.CancelOverlay()
		}
	case overlayConfirmDelete:
		switch key.String() {
		case "esc":
			p.CancelOverlay()
		case "enter":
			deleteID := p.deleteID
			deletedCredentials := []string{}
			if p.kind == KindUpstreams {
				if object, ok := managedconfig.FindObject(p.draft.LocalCommand(), deleteID); ok {
					deletedCredentials = managedconfig.Members(object, generated.ManagedResourceRefKindCredential)
				}
			}
			if err := p.discardDraftReplaceBeforeDelete(deleteID); err != nil {
				// Fail-closed: keep credential and confirm overlay.
				return p, nil
			}
			applyDelete(p.draft, p.kind, deleteID)
			if p.kind == KindCredentials {
				p.draft.ClearCredentialSecretState(deleteID)
			}
			for _, credentialID := range deletedCredentials {
				p.draft.ClearCredentialSecretState(credentialID)
			}
			p.CancelOverlay()
			p.Refresh()
			p.lastIntent = resourcepage.IntentPublish
		}
	case overlayUpstreamDelete:
		switch key.String() {
		case "esc":
			p.CancelOverlay()
		case "up", "left", "k":
			if len(p.upstreamDelete.Options) > 0 {
				p.deleteCursor = (p.deleteCursor + len(p.upstreamDelete.Options) - 1) % len(p.upstreamDelete.Options)
			}
		case "down", "right", "j":
			if len(p.upstreamDelete.Options) > 0 {
				p.deleteCursor = (p.deleteCursor + 1) % len(p.upstreamDelete.Options)
			}
		case "enter":
			deleteID := p.deleteID
			replacementID := ""
			if p.upstreamDelete.needsReplacement() {
				if len(p.upstreamDelete.Options) == 0 {
					return p, nil
				}
				replacementID = p.upstreamDelete.Options[p.deleteCursor].ObjectID
			}
			object, _ := managedconfig.FindObject(p.draft.LocalCommand(), deleteID)
			deletedCredentials := managedconfig.Members(object, generated.ManagedResourceRefKindCredential)
			if err := p.discardDraftReplaceBeforeDelete(deleteID); err != nil {
				return p, nil
			}
			if err := applyUpstreamDelete(p.draft, deleteID, replacementID); err != nil {
				p.SetStatus(err.Error())
				return p, nil
			}
			for _, credentialID := range deletedCredentials {
				p.draft.ClearCredentialSecretState(credentialID)
			}
			p.CancelOverlay()
			p.Refresh()
			p.lastIntent = resourcepage.IntentPublish
		}
	case overlayMigrationConfirm:
		switch key.String() {
		case "esc":
			p.overlay = overlayEditor
		case "up", "left", "k":
			if len(p.migrationOptions) > 0 {
				p.migrationCursor = (p.migrationCursor + len(p.migrationOptions) - 1) % len(p.migrationOptions)
			}
		case "down", "right", "j":
			if len(p.migrationOptions) > 0 {
				p.migrationCursor = (p.migrationCursor + 1) % len(p.migrationOptions)
			}
		case "enter":
			if p.migrationBlocked != "" {
				return p, nil
			}
			if len(p.migrationOptions) > 0 {
				p.editor.values["migration_replacement_id"] = p.migrationOptions[p.migrationCursor].ObjectID
			}
			p.migrationConfirm = true
			p.overlay = overlayEditor
			_ = p.SaveEditor()
		}
	case overlayRestoreUpstream:
		switch key.String() {
		case "esc":
			p.CancelOverlay()
		case "enter":
			if err := restoreUpstreamSimple(p.draft, p.restoreID); err != nil {
				p.SetStatus("Restore failed: " + err.Error())
				return p, nil
			}
			p.CancelOverlay()
			p.Refresh()
			p.lastIntent = resourcepage.IntentPublish
		}
	}
	return p, nil
}

func (p *Page) handleEditorKey(key tea.KeyMsg) {
	switch key.Type {
	case tea.KeyEsc:
		if p.editor.selectorOpen {
			if p.editor.selectorFilter != "" {
				p.editor.selectorFilter = ""
				p.editor.selectorIndex = 0
			} else {
				p.editor.closeSelector()
			}
			return
		}
		p.CancelOverlay()
		return
	case tea.KeyCtrlS:
		if err := p.SaveEditor(); err != nil {
			// SaveEditor already sets validation/status; keep overlay open.
			return
		}
		return
	case tea.KeyEnter:
		if p.editor.applyFocusedSelector(p.draft) {
			return
		}
		p.editor.moveField(1)
		return
	case tea.KeySpace:
		if p.editor.toggleFocusedSelector() {
			return
		}
		if p.editor.focusedIsToggle() {
			p.editor.toggleBool(p.editor.focusedName())
			return
		}
	case tea.KeyTab:
		p.editor.moveField(1)
		return
	case tea.KeyShiftTab:
		p.editor.moveField(-1)
		return
	case tea.KeyCtrlA:
		if (p.editor.focusedName() == "token" || p.editor.focusedName() == "api_key") && p.editor.tokenBuf != nil {
			p.editor.tokenBuf.SelectAll()
			return
		}
	case tea.KeyBackspace:
		if (p.editor.focusedName() == "token" || p.editor.focusedName() == "api_key") && p.editor.tokenBuf != nil {
			p.editor.tokenBuf.Backspace()
			p.editor.focusToken = true
			return
		}
		p.editor.backspace()
		return
	case tea.KeyDelete:
		if (p.editor.focusedName() == "token" || p.editor.focusedName() == "api_key") && p.editor.tokenBuf != nil {
			p.editor.tokenBuf.DeleteRune()
			p.editor.focusToken = true
			return
		}
		return
	case tea.KeyUp:
		if p.editor.moveSelector(-1) {
			return
		}
		p.editor.moveField(-1)
		return
	case tea.KeyDown:
		if p.editor.moveSelector(1) {
			return
		}
		p.editor.moveField(1)
		return
	case tea.KeyLeft:
		if !key.Alt {
			_ = p.editor.cycleFocusedSelect(-1)
		}
		return
	case tea.KeyRight:
		if !key.Alt {
			_ = p.editor.cycleFocusedSelect(1)
		}
		return
	case tea.KeyRunes:
		if key.Alt {
			return
		}
		if (p.editor.focusedName() == "token" || p.editor.focusedName() == "api_key") && p.editor.tokenBuf != nil {
			for _, r := range key.Runes {
				_ = p.editor.tokenBuf.InsertRune(r)
			}
			p.editor.focusToken = true
			return
		}
		p.editor.appendRunes(key.Runes)
		return
	}
	switch key.String() {
	case "ctrl+p":
		if p.editor.moveSelector(-1) {
			return
		}
	case "ctrl+n":
		if p.editor.moveSelector(1) {
			return
		}
	case "esc":
		p.CancelOverlay()
	case "ctrl+s":
		if err := p.SaveEditor(); err != nil {
			return
		}
	}
}

func (p *Page) shiftKind(delta int) {
	idx := 0
	for i, kind := range userKindOrder {
		if kind == p.kind {
			idx = i
			break
		}
	}
	idx = (idx + delta + len(userKindOrder)) % len(userKindOrder)
	p.SetKind(userKindOrder[idx])
}

func (p *Page) View() string {
	if p.overlay == overlayEditor {
		return strings.Join([]string{
			renderStrip(p.kind, p.width),
			p.editor.render(p.width, p.height-stripHeight()),
		}, "\n")
	}
	if p.overlay == overlayDetails {
		return strings.Join([]string{
			renderStrip(p.kind, p.width),
			renderDetails(p.draft, p.kind, p.detailsID, p.width, p.height-stripHeight()),
		}, "\n")
	}
	if p.overlay == overlayDeps {
		return strings.Join([]string{
			renderStrip(p.kind, p.width),
			renderDependencyDialog(p.kind, p.deleteID, p.depsPaths, p.width, p.height-stripHeight()),
		}, "\n")
	}
	if p.overlay == overlayConfirmDelete {
		return strings.Join([]string{
			renderStrip(p.kind, p.width),
			renderConfirmDelete(p.kind, p.deleteID, max(0, countFor(p.draft, p.kind)-1), p.width, p.height-stripHeight(), p.statusExtra),
		}, "\n")
	}
	if p.overlay == overlayUpstreamDelete {
		return strings.Join([]string{
			renderStrip(p.kind, p.width),
			renderUpstreamDelete(p.upstreamDelete, p.deleteCursor, p.width, p.height-stripHeight()),
		}, "\n")
	}
	if p.overlay == overlayMigrationConfirm {
		return strings.Join([]string{
			renderStrip(p.kind, p.width),
			renderMigrationImpact(p.editor.values["name"], p.migrationImpact, p.migrationOptions, p.migrationCursor, p.migrationBlocked, p.width, p.height-stripHeight()),
		}, "\n")
	}
	if p.overlay == overlayRestoreUpstream {
		return strings.Join([]string{
			renderStrip(p.kind, p.width),
			renderRestoreUpstream(p.draft.LocalCommand(), p.restoreID, p.width, p.height-stripHeight()),
		}, "\n")
	}

	p.tablePage.SetSize(p.width, p.tableHeight())
	return strings.Join([]string{
		renderStrip(p.kind, p.width),
		p.tablePage.View(),
	}, "\n")
}

func containsString(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}
