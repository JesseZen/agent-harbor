package targets

import (
	"context"
	"strings"
	"testing"

	"github.com/asheshgoplani/agent-deck/internal/configdraft"
	"github.com/asheshgoplani/agent-deck/internal/coreclient/generated"
	"github.com/asheshgoplani/agent-deck/internal/secretinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func TestMetaOnlySaveForbiddenWhileReplaceRequired(t *testing.T) {
	fake := &fakeStageHTTP{createFn: okCreate("stage-dead")}
	page, draft, _ := newTestPage(t, fake)
	page.SelectID("cred-1")
	page.BeginEdit()
	page.SetSecretMode(SecretActionReplace)
	_ = page.PasteTokenBytes(plantedToken())
	if err := page.SaveEditor(); err != nil {
		t.Fatalf("replace save: %v", err)
	}
	if stage := replaceStageIDFromDraft(draft, "cred-1"); stage != "stage-dead" {
		t.Fatalf("draft stage=%q", stage)
	}

	page.NoteStageLoss("cred-1", secretinput.CodeExpired)
	if !containsString(draft.ReplaceRequiredIDs(), "cred-1") {
		t.Fatal("expected replace_required after NoteStageLoss")
	}
	if draft.CanPublish() {
		t.Fatal("CanPublish must be false while replace_required")
	}

	// Meta-only Save (Replace mode, no new stage / unstaged token) must not
	// reuse the dead draft stage_id or clear replace_required.
	page.SetEditorValues(map[string]string{"name": "Primary-meta"})
	if err := page.SaveEditor(); err == nil {
		t.Fatal("meta-only Save must fail while replace_required")
	}
	if !containsString(draft.ReplaceRequiredIDs(), "cred-1") {
		t.Fatal("replace_required must remain after rejected meta Save")
	}
	if draft.CanPublish() {
		t.Fatal("CanPublish must stay false until a fresh StageToken")
	}
}

func TestDeleteCredentialClearsReplaceRequired(t *testing.T) {
	fake := &fakeStageHTTP{createFn: okCreate("stage-rr")}
	page, draft, _ := newTestPage(t, fake)
	page.SelectID("cred-1")
	page.BeginEdit()
	page.SetSecretMode(SecretActionReplace)
	_ = page.PasteTokenBytes(plantedToken())
	if err := page.SaveEditor(); err != nil {
		t.Fatalf("replace save: %v", err)
	}
	page.NoteStageLoss("cred-1", secretinput.CodeExpired)
	if !containsString(draft.ReplaceRequiredIDs(), "cred-1") {
		t.Fatal("expected replace_required")
	}
	if draft.CanPublish() {
		t.Fatal("CanPublish blocked by replace_required")
	}

	draft.Mutate(func(cmd *generated.MutableConfigCommand) {
		cmd.Targets = nil
	})
	page.Refresh()
	page.SelectID("cred-1")
	page = confirmTryDelete(t, page)

	for _, c := range draft.LocalCommand().Credentials {
		if c.Id == "cred-1" {
			t.Fatal("cred-1 still present after delete")
		}
	}
	if containsString(draft.ReplaceRequiredIDs(), "cred-1") {
		t.Fatal("delete must clear replaceReq for deleted credential")
	}
	if !draft.CanPublish() {
		t.Fatal("CanPublish must be true after deleting replace_required credential (otherwise clean)")
	}
}

func TestDeleteCreatedCredentialClearsSecretMapsDirty(t *testing.T) {
	fake := &fakeStageHTTP{createFn: okCreate("stage-new")}
	page, draft, _ := newTestPage(t, fake)
	page.BeginCreate()
	page.SetEditorValues(map[string]string{
		"id":       "cred-new",
		"name":     "NewCred",
		"provider": "openai",
	})
	page.SetSecretMode(SecretActionReplace)
	_ = page.PasteTokenBytes(plantedToken())
	if err := page.SaveEditor(); err != nil {
		t.Fatalf("create replace: %v", err)
	}
	if !draft.DomainDirty(configdraft.DomainTargets) {
		t.Fatal("create+replace must dirty DomainTargets")
	}

	page.SelectID("cred-new")
	page = confirmTryDelete(t, page)
	for _, c := range draft.LocalCommand().Credentials {
		if c.Id == "cred-new" {
			t.Fatal("cred-new still present after delete")
		}
	}
	// seedDraft mutates Targets/Endpoints; restore them so DomainTargets reflects
	// credential/secret-map dirty only.
	baseCmd := configdraft.ViewToCommand(draft.BaseView())
	draft.Mutate(func(cmd *generated.MutableConfigCommand) {
		cmd.Targets = baseCmd.Targets
		cmd.Endpoints = baseCmd.Endpoints
		cmd.QuotaGroups = baseCmd.QuotaGroups
	})
	if draft.DomainDirty(configdraft.DomainTargets) {
		t.Fatal("DomainTargets must not stay sticky-dirty from secret maps alone after delete")
	}
}

func TestConfirmDeleteDiscardFailureShowsStatusAndEscRetains(t *testing.T) {
	fake := &fakeStageHTTP{
		createFn: okCreate("stage-vis"),
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
	model, _ := page.Update(tea.KeyMsg{Type: tea.KeyEnter})
	page = model.(*Page)
	if page.overlay != overlayConfirmDelete {
		t.Fatalf("overlay=%v", page.overlay)
	}
	if page.statusExtra == "" || !strings.Contains(page.statusExtra, "cleanup") {
		t.Fatalf("expected cleanup statusExtra, got %q", page.statusExtra)
	}
	view := ansi.Strip(page.View())
	if !strings.Contains(view, "stage_cleanup_pending") && !strings.Contains(view, "cleanup") {
		t.Fatalf("confirm-delete View must show cleanup status:\n%s", view)
	}

	statusBeforeEsc := page.statusExtra
	model, _ = page.Update(tea.KeyMsg{Type: tea.KeyEsc})
	page = model.(*Page)
	if page.overlay != overlayNone {
		t.Fatalf("esc must close overlay, got %v", page.overlay)
	}
	if page.statusExtra != statusBeforeEsc {
		t.Fatalf("esc from confirm must retain cleanup status, before=%q after=%q", statusBeforeEsc, page.statusExtra)
	}
}
