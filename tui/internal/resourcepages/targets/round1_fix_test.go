package targets

import (
	"context"
	"strings"
	"testing"

	"github.com/asheshgoplani/agent-deck/internal/coreclient/generated"
	"github.com/asheshgoplani/agent-deck/internal/resourcepage"
	"github.com/asheshgoplani/agent-deck/internal/secretinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func TestNoteStageLossZerosPriorEditorBufferAndDeletesOwnedStage(t *testing.T) {
	fake := &fakeStageHTTP{createFn: okCreate("stage-a")}
	page, _, _ := newTestPage(t, fake)
	page.SelectID("cred-1")
	page.BeginEdit()
	page.SetSecretMode(SecretActionReplace)
	token := plantedToken()
	if err := page.PasteTokenBytes(token); err != nil {
		t.Fatalf("paste: %v", err)
	}
	if err := page.StageToken(context.Background()); err != nil {
		t.Fatalf("stage: %v", err)
	}
	oldBuf := page.editor.tokenBuf
	if oldBuf == nil || page.OwnedStageID() != "stage-a" {
		t.Fatalf("precondition failed owned=%q buf=%v", page.OwnedStageID(), oldBuf != nil)
	}

	page.NoteStageLoss("cred-ext", secretinput.CodeExpired)

	for i := 0; i < oldBuf.Cap(); i++ {
		if oldBuf.ByteAt(i) != 0 {
			t.Fatalf("old buffer byte %d not zeroed", i)
		}
	}
	if fake.deleteCalls < 1 || !containsString(fake.deletedIDs, "stage-a") {
		t.Fatalf("expected DELETE for owned stage-a, deletes=%v", fake.deletedIDs)
	}
	if page.editor.id != "cred-ext" {
		t.Fatalf("editor should switch to cred-ext, got %q", page.editor.id)
	}
}

func TestReplaceRequiredForcesReplaceOnly(t *testing.T) {
	fake := &fakeStageHTTP{createFn: okCreate("stage-recover")}
	page, draft, _ := newTestPage(t, fake)
	page.SelectID("cred-1")
	page.BeginEdit()
	page.SetSecretMode(SecretActionReplace)
	_ = page.PasteTokenBytes(plantedToken())
	if err := page.SaveEditor(); err != nil {
		t.Fatalf("initial replace save: %v", err)
	}
	page.NoteStageLoss("cred-1", secretinput.CodeExpired)
	if !containsString(draft.ReplaceRequiredIDs(), "cred-1") {
		t.Fatal("expected replace_required")
	}
	if draft.CanPublish() {
		t.Fatal("publish blocked while replace_required")
	}

	page.SetSecretMode(SecretActionPreserve)
	if page.EditorSecretMode() != SecretActionReplace {
		t.Fatalf("Preserve must be rejected while replace_required, mode=%v", page.EditorSecretMode())
	}
	page.SetSecretMode(SecretActionExternal)
	if page.EditorSecretMode() != SecretActionReplace {
		t.Fatalf("External must be rejected while replace_required, mode=%v", page.EditorSecretMode())
	}
	page.SetEditorValues(map[string]string{"name": "Primary"})
	page.editor.secretMode = SecretActionPreserve // force invalid mode past UI
	if err := page.SaveEditor(); err == nil {
		t.Fatal("Preserve save must fail while replace_required")
	}
	if draft.CanPublish() {
		t.Fatal("publish still blocked after rejected Preserve")
	}

	page.SetSecretMode(SecretActionReplace)
	_ = page.PasteTokenBytes(plantedToken())
	if err := page.SaveEditor(); err != nil {
		t.Fatalf("Replace+stage save: %v", err)
	}
	if containsString(draft.ReplaceRequiredIDs(), "cred-1") {
		t.Fatal("replace_required must clear after SetCredentialReplace")
	}
	if !draft.CanPublish() {
		t.Fatal("CanPublish must be true after Replace recovery")
	}
}

func TestNoteStageLossKeepsOrDeletesMismatchedOwnedStage(t *testing.T) {
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
	// Restage to own S2 without saving over draft S1.
	page.SelectID("cred-1")
	page.BeginEdit()
	page.SetSecretMode(SecretActionReplace)
	_ = page.PasteTokenBytes(plantedToken())
	if err := page.StageToken(context.Background()); err != nil {
		t.Fatalf("stage S2: %v", err)
	}
	if page.OwnedStageID() != "stage-s2" {
		t.Fatalf("owned=%q", page.OwnedStageID())
	}
	action, _ := draft.LocalCommand().Credentials[0].SecretAction.AsCredentialSecretAction1()
	if string(action.StageId) != "stage-s1" {
		t.Fatalf("draft stage=%q", action.StageId)
	}

	page.NoteStageLoss("cred-1", secretinput.CodeExpired)
	owned := page.OwnedStageID()
	deletedS2 := containsString(fake.deletedIDs, "stage-s2")
	if owned != "stage-s2" && !deletedS2 {
		t.Fatalf("S2 must stay tracked or be DELETE'd; owned=%q deleted=%v", owned, fake.deletedIDs)
	}
	if owned == "" && !deletedS2 {
		t.Fatal("S2 must not be silently orphaned")
	}
}

func TestPreserveSaveDiscardFailureKeepsOwnershipAndErrors(t *testing.T) {
	fake := &fakeStageHTTP{
		createFn: okCreate("stage-keep"),
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
	page.SetSecretMode(SecretActionPreserve)
	page.SetEditorValues(map[string]string{"name": "Primary-kept"})
	err := page.SaveEditor()
	if err == nil {
		t.Fatal("Preserve save must surface Discard failure")
	}
	if page.OwnedStageID() != "stage-keep" {
		t.Fatalf("ownership must remain after Discard fail: %q", page.OwnedStageID())
	}
	if page.overlay != overlayEditor {
		t.Fatal("editor must stay open on Discard failure")
	}
	if !strings.Contains(page.statusExtra, "cleanup") && !strings.Contains(err.Error(), "transport") {
		t.Fatalf("expected cleanup/transport status, status=%q err=%v", page.statusExtra, err)
	}
}

func TestHasUnstagedBlocksPublishIntent(t *testing.T) {
	page, _, _ := newTestPage(t, &fakeStageHTTP{createFn: okCreate("s")})
	page.SelectID("cred-1")
	page.BeginEdit()
	page.SetSecretMode(SecretActionReplace)
	_ = page.PasteTokenBytes(plantedToken())
	if !page.HasUnstaged() {
		t.Fatal("HasUnstaged must be true after paste")
	}
	page.lastIntent = resourcepage.IntentPublish
	model, _ := page.handleIntent(resourcepage.IntentPublish)
	page = model.(*Page)
	if page.LastIntent() != resourcepage.IntentNone {
		t.Fatalf("publish intent must be swallowed, got %q", page.LastIntent())
	}
	if page.statusExtra == "" {
		t.Fatal("blocked publish must set status")
	}
}

func TestDiscardOwnedStagesDeletesDraftReplaceStage(t *testing.T) {
	fake := &fakeStageHTTP{createFn: okCreate("stage-draft")}
	page, _, _ := newTestPage(t, fake)
	page.SelectID("cred-1")
	page.BeginEdit()
	page.SetSecretMode(SecretActionReplace)
	_ = page.PasteTokenBytes(plantedToken())
	if err := page.SaveEditor(); err != nil {
		t.Fatalf("save: %v", err)
	}
	if page.OwnedStageID() != "" {
		t.Fatal("ownership cleared after replace save")
	}
	page.DiscardOwnedStages(context.Background())
	if !containsString(fake.deletedIDs, "stage-draft") {
		t.Fatalf("DiscardOwnedStages must DELETE draft replace stage, got %v", fake.deletedIDs)
	}
}

func TestDetailsShowsReplaceRequiredNotConfigured(t *testing.T) {
	page, draft, _ := newTestPage(t, &fakeStageHTTP{createFn: okCreate("s")})
	draft.MarkReplaceRequired("cred-1")
	page.SelectID("cred-1")
	page.OpenDetails()
	view := ansi.Strip(page.View())
	if !strings.Contains(view, "Replace required") && !strings.Contains(view, "replace_required") {
		t.Fatalf("details must show replace required:\n%s", view)
	}
	if strings.Contains(view, "Configured") {
		t.Fatalf("details must not show Configured while replace_required:\n%s", view)
	}
}

func TestBannerOffsetIncludesStatusLine(t *testing.T) {
	page, _, _ := newTestPage(t, &fakeStageHTTP{createFn: okCreate("s")})
	page.SetState(resourcepage.StateSuccess)
	page.SetStatus("")
	if page.BannerOffset() != 0 {
		t.Fatalf("success without status => offset=%d", page.BannerOffset())
	}
	page.SetStatus("replace_required: stage_expired")
	if page.BannerOffset() < 1 {
		t.Fatalf("status line must count in BannerOffset, got %d", page.BannerOffset())
	}
	page.SetState(resourcepage.StatePublicationError)
	page.SetStatus("generation conflict")
	if page.BannerOffset() < 2 {
		t.Fatalf("banner+status must count both, got %d", page.BannerOffset())
	}
}

func TestTokenBackspaceDeletesInEditor(t *testing.T) {
	page, _, _ := newTestPage(t, &fakeStageHTTP{createFn: okCreate("s")})
	page.SelectID("cred-1")
	page.BeginEdit()
	page.SetSecretMode(SecretActionReplace)
	_ = page.PasteTokenBytes([]byte{'A', 'B', 'C'})
	before := page.TokenBufferLen()
	model, _ := page.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	page = model.(*Page)
	if page.TokenBufferLen() != before-1 {
		t.Fatalf("token backspace len=%d want %d", page.TokenBufferLen(), before-1)
	}
	if strings.Contains(page.View(), "ABC") {
		t.Fatal("raw token leaked after backspace")
	}
}

func TestTryDeleteRoutesThroughConfirmOverlay(t *testing.T) {
	page, draft, _ := newTestPage(t, &fakeStageHTTP{createFn: okCreate("s")})
	draft.Mutate(func(cmd *generated.MutableConfigCommand) {
		cmd.Targets = nil
	})
	page.Refresh()
	page.SelectID("cred-ext")
	blocked, _ := page.TryDelete()
	if blocked {
		t.Fatal("unexpected inbound block")
	}
	if page.overlay != overlayConfirmDelete {
		t.Fatalf("TryDelete must open confirm overlay, got %v", page.overlay)
	}
	stillPresent := false
	for _, c := range draft.LocalCommand().Credentials {
		if c.Id == "cred-ext" {
			stillPresent = true
		}
	}
	if !stillPresent {
		t.Fatal("credential must remain until confirm")
	}
	model, _ := page.Update(tea.KeyMsg{Type: tea.KeyEnter})
	page = model.(*Page)
	for _, c := range draft.LocalCommand().Credentials {
		if c.Id == "cred-ext" {
			t.Fatal("cred-ext still present after confirm")
		}
	}
}

func TestTargetCreateSeedsHealthThrottleDefaults(t *testing.T) {
	page, _, _ := newTestPage(t, &fakeStageHTTP{createFn: okCreate("s")})
	page.SetKind(KindTargets)
	page.BeginCreate()
	for _, key := range []string{
		"failure_threshold", "initial_backoff_ms", "jitter_percent", "max_backoff_ms",
		"probe_timeout_ms", "recovery_success_threshold", "stable_probe_interval_ms",
		"default_cooling_ms", "max_cooling_ms",
	} {
		if strings.TrimSpace(page.editor.values[key]) == "" {
			t.Fatalf("create editor missing seeded default for %s", key)
		}
	}
}

func TestTargetsNilStatusBlanksHealthEligible(t *testing.T) {
	draft := seedDraft(t)
	rows := rowsFor(draft, KindTargets, nil)
	if len(rows) == 0 {
		t.Fatal("expected target rows")
	}
	for _, row := range rows {
		if len(row.Cells) < 4 {
			t.Fatalf("cells=%v", row.Cells)
		}
		if row.Cells[2] != "" || row.Cells[3] != "" {
			t.Fatalf("nil status must blank HEALTH/ELIGIBLE, got %q/%q", row.Cells[2], row.Cells[3])
		}
	}
}

func TestApplyCredentialExternalPreserveFailClosed(t *testing.T) {
	draft := seedDraft(t)
	err := applyCredentialExternal(draft, "missing-id", map[string]string{
		"name": "x", "external_kind": "env", "external_env_name": "FOO", "external_exportable": "false",
	})
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("external missing id: %v", err)
	}
	err = applyCredentialPreserve(draft, "missing-id", map[string]string{"name": "x"})
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("preserve missing id: %v", err)
	}
}

func confirmTryDelete(t *testing.T, page *Page) *Page {
	t.Helper()
	blocked, _ := page.TryDelete()
	if blocked {
		t.Fatal("unexpected inbound block in confirmTryDelete")
	}
	if page.overlay != overlayConfirmDelete {
		t.Fatalf("expected confirm overlay, got %v", page.overlay)
	}
	model, _ := page.Update(tea.KeyMsg{Type: tea.KeyEnter})
	return model.(*Page)
}
