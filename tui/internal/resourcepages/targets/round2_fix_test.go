package targets

import (
	"context"
	"strings"
	"testing"

	"github.com/asheshgoplani/agent-deck/internal/coreclient/generated"
	"github.com/asheshgoplani/agent-deck/internal/resourcepage"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func TestValidationFailThenSaveClearsStatusAndBannerOffset(t *testing.T) {
	fake := &fakeStageHTTP{createFn: okCreate("stage-ok")}
	page, _, _ := newTestPage(t, fake)
	page.SelectID("cred-1")
	page.BeginEdit()
	page.SetSecretMode(SecretActionReplace)
	page.SetEditorValues(map[string]string{"name": ""})
	if err := page.SaveEditor(); err == nil {
		t.Fatal("expected validation failure")
	}
	if page.statusExtra == "" {
		t.Fatal("validation must write statusExtra via SetStatus")
	}
	if page.BannerOffset() < 1 {
		t.Fatalf("BannerOffset must count validation status, got %d", page.BannerOffset())
	}

	page.SetEditorValues(map[string]string{"name": "Primary"})
	_ = page.PasteTokenBytes(plantedToken())
	if err := page.SaveEditor(); err != nil {
		t.Fatalf("retry save: %v", err)
	}
	if page.statusExtra != "" {
		t.Fatalf("successful save must clear status, got %q", page.statusExtra)
	}
	if page.tablePage != nil && page.BannerOffset() != 0 {
		// Success state, no status → offset 0; table status must also be clear.
		t.Fatalf("BannerOffset after success=%d", page.BannerOffset())
	}
	view := ansi.Strip(page.View())
	if strings.Contains(view, "$.name: required") {
		t.Fatalf("stale validation status still visible:\n%s", view)
	}
}

func TestCancelAfterValidationClearsStatusAndBannerOffset(t *testing.T) {
	page, _, _ := newTestPage(t, &fakeStageHTTP{createFn: okCreate("s")})
	page.BeginCreate()
	page.SetEditorValues(map[string]string{"id": "", "name": "", "provider": "openai"})
	page.SetSecretMode(SecretActionReplace)
	if err := page.SaveEditor(); err == nil {
		t.Fatal("expected validation error")
	}
	model, _ := page.Update(tea.KeyMsg{Type: tea.KeyEsc})
	page = model.(*Page)
	if page.statusExtra != "" {
		t.Fatalf("cancel must clear statusExtra, got %q", page.statusExtra)
	}
	if page.BannerOffset() != 0 && page.State() == resourcepage.StateSuccess {
		t.Fatalf("BannerOffset after cancel=%d state=%q", page.BannerOffset(), page.State())
	}
	view := ansi.Strip(page.View())
	if strings.Contains(view, "$.id: required") {
		t.Fatalf("stale validation after cancel:\n%s", view)
	}
}

func TestReplaceOverwriteDeletesPreviousDraftStage(t *testing.T) {
	seq := 0
	fake := &fakeStageHTTP{
		createFn: func(context.Context, string, []byte) (*generated.CreateProviderSecretStageResponse, error) {
			seq++
			id := "stage-s1"
			if seq > 1 {
				id = "stage-s2"
			}
			return okCreate(id)(context.Background(), "", nil)
		},
	}
	page, draft, _ := newTestPage(t, fake)
	page.SelectID("cred-1")
	page.BeginEdit()
	page.SetSecretMode(SecretActionReplace)
	_ = page.PasteTokenBytes(plantedToken())
	if err := page.SaveEditor(); err != nil {
		t.Fatalf("save S1: %v", err)
	}
	action, _ := draft.LocalCommand().Credentials[0].SecretAction.AsCredentialSecretAction1()
	if string(action.StageId) != "stage-s1" {
		t.Fatalf("draft stage=%q", action.StageId)
	}

	page.SelectID("cred-1")
	page.BeginEdit()
	page.SetSecretMode(SecretActionReplace)
	_ = page.PasteTokenBytes(plantedToken())
	if err := page.SaveEditor(); err != nil {
		t.Fatalf("save S2: %v", err)
	}
	if !containsString(fake.deletedIDs, "stage-s1") {
		t.Fatalf("overwrite must DELETE S1, deleted=%v", fake.deletedIDs)
	}
	action, _ = draft.LocalCommand().Credentials[0].SecretAction.AsCredentialSecretAction1()
	if string(action.StageId) != "stage-s2" {
		t.Fatalf("draft stage after overwrite=%q", action.StageId)
	}

	// Orphan S1 must not remain live without tracking: DiscardOwnedStages only sees S2.
	fake.deletedIDs = nil
	if err := page.DiscardOwnedStages(context.Background()); err != nil {
		t.Fatalf("DiscardOwnedStages: %v", err)
	}
	if containsString(fake.deletedIDs, "stage-s1") {
		t.Fatal("S1 should already have been deleted on overwrite; Discard must not need to find orphan S1")
	}
	if !containsString(fake.deletedIDs, "stage-s2") {
		t.Fatalf("DiscardOwnedStages must DELETE current draft S2, got %v", fake.deletedIDs)
	}
}

func TestPreserveDiscardFailureDoesNotMutateDraft(t *testing.T) {
	fake := &fakeStageHTTP{
		createFn: okCreate("stage-s1"),
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
	// Re-open; draft holds replace(S1). Switch to Preserve with DELETE failing.
	page.SelectID("cred-1")
	page.BeginEdit()
	page.SetSecretMode(SecretActionPreserve)
	page.SetEditorValues(map[string]string{"name": "Primary-mutated"})
	err := page.SaveEditor()
	if err == nil {
		t.Fatal("Preserve save must fail when Discard of leftover replace fails")
	}
	action, aerr := draft.LocalCommand().Credentials[0].SecretAction.AsCredentialSecretAction1()
	if aerr != nil || action.Mode != generated.CredentialSecretActionReplace || string(action.StageId) != "stage-s1" {
		t.Fatalf("draft must remain replace(S1), action=%#v err=%v", draft.LocalCommand().Credentials[0].SecretAction, aerr)
	}
	if draft.LocalCommand().Credentials[0].Name == "Primary-mutated" {
		t.Fatal("name must not mutate when Discard fails")
	}
	if page.overlay != overlayEditor {
		t.Fatal("editor must stay open")
	}
}

func TestDiscardOwnedStagesSurfacesDeleteFailure(t *testing.T) {
	fake := &fakeStageHTTP{
		createFn: okCreate("stage-draft"),
		deleteFn: func(context.Context, generated.SecretStageID) (*generated.DeleteProviderSecretStageResponse, error) {
			return nil, context.DeadlineExceeded
		},
	}
	page, _, _ := newTestPage(t, fake)
	page.SelectID("cred-1")
	page.BeginEdit()
	page.SetSecretMode(SecretActionReplace)
	_ = page.PasteTokenBytes(plantedToken())
	if err := page.SaveEditor(); err != nil {
		t.Fatalf("save: %v", err)
	}
	err := page.DiscardOwnedStages(context.Background())
	if err == nil {
		t.Fatal("DiscardOwnedStages must return error on DELETE transport fail")
	}
	if !strings.Contains(page.statusExtra, "cleanup") && !strings.Contains(err.Error(), "transport") {
		t.Fatalf("expected observable cleanup/transport, status=%q err=%v", page.statusExtra, err)
	}
}

func TestCancelDiscardFailureKeepsTrackingAndStatus(t *testing.T) {
	fake := &fakeStageHTTP{
		createFn: okCreate("stage-cancel-fail"),
		deleteFn: func(context.Context, generated.SecretStageID) (*generated.DeleteProviderSecretStageResponse, error) {
			return nil, context.DeadlineExceeded
		},
	}
	page, _, _ := newTestPage(t, fake)
	page.SelectID("cred-1")
	page.BeginEdit()
	page.SetSecretMode(SecretActionReplace)
	_ = page.PasteTokenBytes(plantedToken())
	if err := page.StageToken(context.Background()); err != nil {
		t.Fatalf("stage: %v", err)
	}
	model, _ := page.Update(tea.KeyMsg{Type: tea.KeyEsc})
	page = model.(*Page)
	if page.OwnedStageID() != "stage-cancel-fail" {
		t.Fatalf("ownership must remain for retry, owned=%q", page.OwnedStageID())
	}
	if page.statusExtra == "" {
		t.Fatal("cancel Discard failure must set cleanup status")
	}
}

func TestEnumValuesMatchGeneratedValid(t *testing.T) {
	providers := credentialProviderEnumValues()
	if len(providers) == 0 {
		t.Fatal("provider enums empty")
	}
	for _, p := range providers {
		if !generated.MutableCredentialCommandProvider(p).Valid() {
			t.Fatalf("provider %q not Valid()", p)
		}
	}
	adapters := targetAdapterEnumValues()
	if len(adapters) == 0 {
		t.Fatal("adapter enums empty")
	}
	for _, a := range adapters {
		if !generated.MutableTargetCommandAdapter(a).Valid() {
			t.Fatalf("adapter %q not Valid()", a)
		}
	}
	bridges := targetBridgeEnumValues()
	if len(bridges) == 0 {
		t.Fatal("bridge enums empty")
	}
	for _, b := range bridges {
		if !generated.MutableTargetCommandBridge(b).Valid() {
			t.Fatalf("bridge %q not Valid()", b)
		}
	}
	// Drift guard: counts must match generated closed sets.
	if len(providers) != 4 {
		t.Fatalf("provider count drift: got %d", len(providers))
	}
	if len(adapters) != 4 {
		t.Fatalf("adapter count drift: got %d", len(adapters))
	}
	if len(bridges) != 5 {
		t.Fatalf("bridge count drift: got %d", len(bridges))
	}
}

func TestKeyboardSaveSurfacesValidationError(t *testing.T) {
	page, _, _ := newTestPage(t, &fakeStageHTTP{createFn: okCreate("s")})
	page.BeginCreate()
	page.SetEditorValues(map[string]string{"id": "", "name": "", "provider": "openai"})
	page.SetSecretMode(SecretActionReplace)
	model, _ := page.Update(tea.KeyMsg{Type: tea.KeyCtrlS})
	page = model.(*Page)
	if page.overlay != overlayEditor {
		t.Fatal("overlay must stay open on keyboard save validation failure")
	}
	if page.statusExtra == "" && page.editor.err == "" {
		t.Fatal("keyboard SaveEditor error must surface via status/editor.err")
	}
}

func TestSmallViewportKeepsEscHint(t *testing.T) {
	page, _, _ := newTestPage(t, &fakeStageHTTP{createFn: okCreate("s")})
	page.SetSize(70, 8)
	page.SelectID("cred-1")
	page.OpenDetails()
	view := ansi.Strip(page.View())
	if !strings.Contains(view, "esc") {
		t.Fatalf("details height-clamp must keep esc hint:\n%s", view)
	}
	page.CancelOverlay()
	page.BeginEdit()
	page.SetSecretMode(SecretActionReplace)
	view = ansi.Strip(page.View())
	if !strings.Contains(view, "esc") && !strings.Contains(view, "cancel") {
		t.Fatalf("editor height-clamp must keep esc/cancel hint:\n%s", view)
	}
}

func TestInvalidProviderRejectedViaValid(t *testing.T) {
	err := validateCredentialValues(map[string]string{
		"id": "x", "name": "n", "provider": "not-a-provider",
	}, true, SecretActionReplace)
	if err == nil || !strings.Contains(err.Error(), "provider") {
		t.Fatalf("expected provider invalid, got %v", err)
	}
}
