package targets

import (
	"context"
	"testing"

	"github.com/asheshgoplani/agent-deck/internal/coreclient/generated"
	tea "github.com/charmbracelet/bubbletea"
)

func TestDeleteCredentialDeletesDraftReplaceStage(t *testing.T) {
	fake := &fakeStageHTTP{createFn: okCreate("stage-del")}
	page, draft, _ := newTestPage(t, fake)
	page.SelectID("cred-1")
	page.BeginEdit()
	page.SetSecretMode(SecretActionReplace)
	_ = page.PasteTokenBytes(plantedToken())
	if err := page.SaveEditor(); err != nil {
		t.Fatalf("replace save: %v", err)
	}
	if stage := replaceStageIDFromDraft(draft, "cred-1"); stage != "stage-del" {
		t.Fatalf("draft stage=%q", stage)
	}

	// Clear inbound refs so confirm-delete path is reachable.
	draft.Mutate(func(cmd *generated.MutableConfigCommand) {
		cmd.Targets = nil
	})
	page.Refresh()
	page.SelectID("cred-1")
	fake.deletedIDs = nil
	page = confirmTryDelete(t, page)

	if !containsString(fake.deletedIDs, "stage-del") {
		t.Fatalf("delete credential must DELETE draft replace stage, deleted=%v", fake.deletedIDs)
	}
	for _, c := range draft.LocalCommand().Credentials {
		if c.Id == "cred-1" {
			t.Fatal("cred-1 still present after confirm delete")
		}
	}

	// DiscardOwnedStages must not need to rediscover the orphan.
	fake.deletedIDs = nil
	if err := page.DiscardOwnedStages(context.Background()); err != nil {
		t.Fatalf("DiscardOwnedStages: %v", err)
	}
	if containsString(fake.deletedIDs, "stage-del") {
		t.Fatal("stage-del should already have been deleted on credential delete")
	}
}

func TestDeleteCredentialDiscardFailureKeepsCredential(t *testing.T) {
	fake := &fakeStageHTTP{
		createFn: okCreate("stage-keep"),
		deleteFn: func(context.Context, generated.SecretStageID) (*generated.DeleteProviderSecretStageResponse, error) {
			return nil, context.DeadlineExceeded
		},
	}
	page, draft, _ := newTestPage(t, fake)
	page.SelectID("cred-1")
	page.BeginEdit()
	page.SetSecretMode(SecretActionReplace)
	_ = page.PasteTokenBytes(plantedToken())
	if err := page.SaveEditor(); err != nil {
		t.Fatalf("replace save: %v", err)
	}
	draft.Mutate(func(cmd *generated.MutableConfigCommand) {
		cmd.Targets = nil
	})
	page.Refresh()
	page.SelectID("cred-1")
	blocked, _ := page.TryDelete()
	if blocked {
		t.Fatal("unexpected inbound block")
	}
	if page.overlay != overlayConfirmDelete {
		t.Fatalf("overlay=%v", page.overlay)
	}
	model, _ := page.Update(tea.KeyMsg{Type: tea.KeyEnter})
	page = model.(*Page)
	found := false
	for _, c := range draft.LocalCommand().Credentials {
		if c.Id == "cred-1" {
			found = true
			action, err := c.SecretAction.AsCredentialSecretAction1()
			if err != nil || string(action.StageId) != "stage-keep" {
				t.Fatalf("draft must remain replace(stage-keep), action=%#v err=%v", c.SecretAction, err)
			}
		}
	}
	if !found {
		t.Fatal("fail-closed: credential must remain when stage DELETE fails")
	}
	if page.overlay != overlayConfirmDelete {
		t.Fatalf("confirm overlay must stay open on discard failure, got %v", page.overlay)
	}
	if page.statusExtra == "" {
		t.Fatal("discard failure must set cleanup status")
	}
}

func TestBeginEditRestoresReplaceFromDraft(t *testing.T) {
	fake := &fakeStageHTTP{createFn: okCreate("stage-s1")}
	page, draft, _ := newTestPage(t, fake)
	page.SelectID("cred-1")
	page.BeginEdit()
	page.SetSecretMode(SecretActionReplace)
	_ = page.PasteTokenBytes(plantedToken())
	if err := page.SaveEditor(); err != nil {
		t.Fatalf("replace save: %v", err)
	}

	page.SelectID("cred-1")
	page.BeginEdit()
	if page.EditorSecretMode() != SecretActionReplace {
		t.Fatalf("BeginEdit must restore Replace from draft, mode=%v", page.EditorSecretMode())
	}

	// Meta rename while staying in Replace must keep stage without DELETE.
	fake.deletedIDs = nil
	page.SetEditorValues(map[string]string{"name": "Primary-renamed"})
	if err := page.SaveEditor(); err != nil {
		t.Fatalf("meta rename save: %v", err)
	}
	if containsString(fake.deletedIDs, "stage-s1") {
		t.Fatalf("meta rename must not DELETE pending replace stage, deleted=%v", fake.deletedIDs)
	}
	action, err := draft.LocalCommand().Credentials[0].SecretAction.AsCredentialSecretAction1()
	if err != nil || action.Mode != generated.CredentialSecretActionReplace || string(action.StageId) != "stage-s1" {
		t.Fatalf("draft must remain replace(stage-s1), action=%#v err=%v", draft.LocalCommand().Credentials[0].SecretAction, err)
	}
	if draft.LocalCommand().Credentials[0].Name != "Primary-renamed" {
		t.Fatalf("name=%q", draft.LocalCommand().Credentials[0].Name)
	}
}

func TestCapabilitiesEnumValuesMatchGeneratedValid(t *testing.T) {
	caps := targetCapabilitiesEnumValues()
	if len(caps) == 0 {
		t.Fatal("capabilities enums empty")
	}
	for _, c := range caps {
		if !generated.MutableTargetCommandCapabilities(c).Valid() {
			t.Fatalf("capability %q not Valid()", c)
		}
	}
	if len(caps) != 8 {
		t.Fatalf("capabilities count drift: got %d", len(caps))
	}
}
