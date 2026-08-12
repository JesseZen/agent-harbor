package configdraft

import (
	"fmt"

	"github.com/asheshgoplani/agent-deck/internal/coreclient/generated"
)

type credentialSecretState struct {
	mode           SecretMode
	stageID        generated.SecretStageID
	baseGeneration int
}

type replaceRequiredInfo struct {
	name     string
	provider generated.MutableCredentialCommandProvider
}

func secretModeFromAction(action generated.CredentialSecretAction) (SecretMode, generated.SecretStageID, generated.ExternalSecretRef, error) {
	if preserve, err := action.AsCredentialSecretAction0(); err == nil && preserve.Mode == generated.CredentialSecretActionPreserve {
		return SecretPreserve, "", generated.ExternalSecretRef{}, nil
	}
	if replace, err := action.AsCredentialSecretAction1(); err == nil && replace.Mode == generated.CredentialSecretActionReplace {
		return SecretReplace, replace.StageId, generated.ExternalSecretRef{}, nil
	}
	if external, err := action.AsCredentialSecretAction2(); err == nil && external.Mode == generated.CredentialSecretActionExternalRef {
		return SecretExternalRef, "", external.Ref, nil
	}
	return "", "", generated.ExternalSecretRef{}, fmt.Errorf("unknown secret action")
}

func actionFromMode(mode SecretMode, stageID generated.SecretStageID, ref generated.ExternalSecretRef) (generated.CredentialSecretAction, error) {
	var action generated.CredentialSecretAction
	switch mode {
	case SecretPreserve:
		err := action.FromCredentialSecretAction0(generated.CredentialSecretAction0{
			Mode: generated.CredentialSecretActionPreserve,
		})
		return action, err
	case SecretReplace:
		err := action.FromCredentialSecretAction1(generated.CredentialSecretAction1{
			Mode:    generated.CredentialSecretActionReplace,
			StageId: stageID,
		})
		return action, err
	case SecretExternalRef:
		err := action.FromCredentialSecretAction2(generated.CredentialSecretAction2{
			Mode: generated.CredentialSecretActionExternalRef,
			Ref:  ref,
		})
		return action, err
	default:
		return action, fmt.Errorf("cannot convert local-only mode %q to wire action", mode)
	}
}

func normalizeCredentialBinding(view generated.MutableCredentialView, baseGeneration int) normalizedSecret {
	if managed, err := view.SecretBinding.AsCredentialSecretBinding0(); err == nil && managed.Mode == generated.CredentialSecretBindingManaged {
		return normalizedSecret{mode: SecretPreserve, baseGeneration: baseGeneration}
	}
	if external, err := view.SecretBinding.AsCredentialSecretBinding1(); err == nil && external.Mode == generated.CredentialSecretBindingExternalRef {
		return normalizedSecret{mode: SecretExternalRef, externalRef: external.Ref}
	}
	return normalizedSecret{mode: SecretPreserve, baseGeneration: baseGeneration}
}

type normalizedSecret struct {
	mode           SecretMode
	baseGeneration int
	externalRef    generated.ExternalSecretRef
	stageID        generated.SecretStageID
}

func (n normalizedSecret) equal(o normalizedSecret) bool {
	if n.mode != o.mode {
		return false
	}
	switch n.mode {
	case SecretPreserve:
		return n.baseGeneration == o.baseGeneration
	case SecretExternalRef:
		return jsonEqual(n.externalRef, o.externalRef)
	case SecretReplace:
		return n.stageID == o.stageID && n.baseGeneration == o.baseGeneration
	default:
		return false
	}
}

func findCredentialView(view generated.MutableConfigView, id generated.ConfigID) (generated.MutableCredentialView, bool) {
	for _, c := range view.Credentials {
		if c.Id == id {
			return c, true
		}
	}
	return generated.MutableCredentialView{}, false
}

func mergeCredentialSecret(
	path string,
	baseView, currentView generated.MutableCredentialView,
	draftCmd generated.MutableCredentialCommand,
	localState *credentialSecretState,
) (generated.MutableCredentialCommand, *Conflict, *credentialSecretState) {
	baseGen := 0
	if baseView.Generation != nil {
		baseGen = *baseView.Generation
	}
	currentGen := 0
	if currentView.Generation != nil {
		currentGen = *currentView.Generation
	}

	baseNorm := normalizeCredentialBinding(baseView, baseGen)
	currentNorm := normalizeCredentialBinding(currentView, currentGen)

	draftMode, draftStage, draftRef, err := secretModeFromAction(draftCmd.SecretAction)
	if err != nil {
		return draftCmd, &Conflict{Path: path + ".secret_action", Reason: "invalid secret action"}, localState
	}

	baseCmd := credentialViewToCommand(baseView)
	nonSecretDraft := credentialWithoutGeneration(draftCmd)
	nonSecretBase := credentialWithoutGeneration(baseCmd)

	if jsonEqual(nonSecretDraft, nonSecretBase) && draftMode == SecretPreserve {
		// unchanged local credential: take fresh current binding as preserve
		out := draftCmd
		out.SecretAction = credentialViewToCommand(currentView).SecretAction
		return out, nil, nil
	}

	switch draftMode {
	case SecretPreserve:
		out := draftCmd
		out.SecretAction = credentialViewToCommand(currentView).SecretAction
		return out, nil, nil

	case SecretExternalRef:
		draftNorm := normalizedSecret{mode: SecretExternalRef, externalRef: draftRef}
		if baseNorm.mode == SecretExternalRef && jsonEqual(baseNorm.externalRef, draftRef) {
			if jsonEqual(baseNorm.externalRef, currentNorm.externalRef) {
				return draftCmd, nil, nil
			}
			if currentNorm.mode == SecretExternalRef && jsonEqual(currentNorm.externalRef, draftRef) {
				return draftCmd, nil, nil
			}
		}
		if currentNorm.mode == SecretExternalRef && jsonEqual(currentNorm.externalRef, draftRef) && !jsonEqual(baseNorm.externalRef, draftRef) {
			return draftCmd, nil, nil
		}
		if draftNorm.equal(currentNorm) || (baseNorm.equal(draftNorm) && draftNorm.equal(currentNorm)) {
			return draftCmd, nil, nil
		}
		if baseNorm.equal(draftNorm) {
			out := draftCmd
			out.SecretAction = credentialViewToCommand(currentView).SecretAction
			return out, nil, nil
		}
		if jsonEqual(currentNorm.externalRef, draftRef) && draftNorm.equal(currentNorm) {
			return draftCmd, nil, nil
		}
		if jsonEqual(baseNorm.externalRef, draftRef) && jsonEqual(currentNorm.externalRef, draftRef) {
			return draftCmd, nil, nil
		}
		if !jsonEqual(draftRef, currentNorm.externalRef) && !baseNorm.equal(draftNorm) && !currentNorm.equal(draftNorm) {
			return draftCmd, &Conflict{Path: path + ".secret_action", Reason: "external secret ref diverged"}, localState
		}
		return draftCmd, nil, nil

	case SecretReplace:
		recordedGen := baseGen
		stageID := draftStage
		if localState != nil {
			recordedGen = localState.baseGeneration
			stageID = localState.stageID
		}
		baseBindingMatch := baseNorm.equal(normalizeCredentialBinding(baseView, recordedGen))
		currentBindingMatch := currentNorm.equal(normalizeCredentialBinding(currentView, recordedGen))
		if baseBindingMatch && currentBindingMatch {
			var action generated.CredentialSecretAction
			_ = action.FromCredentialSecretAction1(generated.CredentialSecretAction1{
				Mode:    generated.CredentialSecretActionReplace,
				StageId: stageID,
			})
			out := draftCmd
			out.SecretAction = action
			state := &credentialSecretState{
				mode:           SecretReplace,
				stageID:        stageID,
				baseGeneration: recordedGen,
			}
			return out, nil, state
		}
		return draftCmd, &Conflict{Path: path + ".secret_action", Reason: "credential generation changed under local replace"}, &credentialSecretState{
			mode:           SecretReplace,
			stageID:        stageID,
			baseGeneration: recordedGen,
		}
	}

	return draftCmd, nil, localState
}

func applyReplaceRequired(
	cmd generated.MutableCredentialCommand,
	info replaceRequiredInfo,
) generated.MutableCredentialCommand {
	cmd.Name = info.name
	cmd.Provider = info.provider
	return cmd
}
