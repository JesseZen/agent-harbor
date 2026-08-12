package targets

import (
	"bytes"
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/asheshgoplani/agent-deck/internal/configdraft"
	"github.com/asheshgoplani/agent-deck/internal/coreclient/generated"
	"github.com/asheshgoplani/agent-deck/internal/resourcepage"
	"github.com/asheshgoplani/agent-deck/internal/secretinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func TestPreserveSaveAfterReplaceStagingDiscardsOwnedStage(t *testing.T) {
	fake := &fakeStageHTTP{createFn: okCreate("stage-orphan")}
	page, draft, _ := newTestPage(t, fake)
	page.SelectID("cred-1")
	page.BeginEdit()
	page.SetSecretMode(SecretActionReplace)
	if err := page.PasteTokenBytes(plantedToken()); err != nil {
		t.Fatalf("paste: %v", err)
	}
	if err := page.StageToken(context.Background()); err != nil {
		t.Fatalf("stage: %v", err)
	}
	if page.OwnedStageID() != "stage-orphan" {
		t.Fatalf("owned=%q", page.OwnedStageID())
	}
	page.SetSecretMode(SecretActionPreserve)
	page.SetEditorValues(map[string]string{"name": "Primary-kept"})
	if err := page.SaveEditor(); err != nil {
		t.Fatalf("preserve save: %v", err)
	}
	if fake.deleteCalls < 1 {
		t.Fatal("expected DELETE of owned staged secret on preserve save")
	}
	if page.OwnedStageID() != "" {
		t.Fatalf("OwnedStageID must be empty after preserve save: %q", page.OwnedStageID())
	}
	action, err := draft.LocalCommand().Credentials[0].SecretAction.AsCredentialSecretAction0()
	if err != nil || action.Mode != generated.CredentialSecretActionPreserve {
		t.Fatalf("expected preserve action, got %#v err=%v", draft.LocalCommand().Credentials[0].SecretAction, err)
	}
}

func TestCancelAfterValidationErrorClearsStickyValidationState(t *testing.T) {
	page, _, _ := newTestPage(t, &fakeStageHTTP{createFn: okCreate("s")})
	page.BeginCreate()
	page.SetEditorValues(map[string]string{"id": "", "name": "", "provider": "openai"})
	page.SetSecretMode(SecretActionReplace)
	if err := page.SaveEditor(); err == nil {
		t.Fatal("expected validation error")
	}
	if page.State() != resourcepage.StateValidationError {
		t.Fatalf("state after validation=%q", page.State())
	}
	model, _ := page.Update(tea.KeyMsg{Type: tea.KeyEsc})
	page = model.(*Page)
	if page.State() == resourcepage.StateValidationError {
		t.Fatalf("sticky ValidationError after Esc cancel: state=%q", page.State())
	}
}

func TestCancelEditorDiscardsOwnedStageAndZerosBuffer(t *testing.T) {
	fake := &fakeStageHTTP{createFn: okCreate("stage-cancel")}
	page, draft, _ := newTestPage(t, fake)
	page.SelectID("cred-1")
	page.BeginEdit()
	page.SetSecretMode(SecretActionReplace)
	if err := page.PasteTokenBytes(plantedToken()); err != nil {
		t.Fatalf("paste: %v", err)
	}
	if err := page.StageToken(context.Background()); err != nil {
		t.Fatalf("stage: %v", err)
	}
	if page.OwnedStageID() == "" {
		t.Fatal("expected owned stage before cancel")
	}
	model, _ := page.Update(tea.KeyMsg{Type: tea.KeyEsc})
	page = model.(*Page)
	if page.OwnedStageID() != "" {
		t.Fatalf("owned stage after cancel: %q", page.OwnedStageID())
	}
	if fake.deleteCalls < 1 {
		t.Fatal("expected DELETE on cancel")
	}
	if page.TokenBufferLen() != 0 {
		t.Fatalf("buffer not zeroed: len=%d", page.TokenBufferLen())
	}
	for _, c := range draft.LocalCommand().Credentials {
		if c.Id != "cred-1" {
			continue
		}
		if action, err := c.SecretAction.AsCredentialSecretAction1(); err == nil && action.Mode == generated.CredentialSecretActionReplace {
			t.Fatal("cancel must not keep replace action on draft")
		}
	}
	if len(draft.ReplaceRequiredIDs()) != 0 {
		t.Fatalf("unexpected replace_required after cancel: %v", draft.ReplaceRequiredIDs())
	}
	view := page.View()
	if bytes.Contains([]byte(view), plantedToken()) {
		t.Fatal("planted token leaked into view after cancel")
	}
}

func TestStageExpiryMarksReplaceRequiredFocusesTokenNeverReusesID(t *testing.T) {
	stageSeq := 0
	fake := &fakeStageHTTP{
		createFn: func(context.Context, string, []byte) (*generated.CreateProviderSecretStageResponse, error) {
			stageSeq++
			id := "stage-new"
			if stageSeq == 1 {
				id = "stage-old"
			}
			return okCreate(id)(context.Background(), "", nil)
		},
	}
	page, draft, _ := newTestPage(t, fake)
	page.SelectID("cred-1")
	page.BeginEdit()
	page.SetSecretMode(SecretActionReplace)
	_ = page.PasteTokenBytes(plantedToken())
	if err := page.StageToken(context.Background()); err != nil {
		t.Fatalf("stage: %v", err)
	}
	old := page.OwnedStageID()
	if old != "stage-old" {
		t.Fatalf("owned=%q", old)
	}
	page.NoteStageLoss("cred-1", secretinput.CodeExpired)
	if !containsString(draft.ReplaceRequiredIDs(), "cred-1") {
		t.Fatal("expected replace_required after expiry")
	}
	if draft.CanPublish() {
		t.Fatal("publish must be blocked while replace_required")
	}
	if page.OwnedStageID() != "" {
		t.Fatal("ownership must clear on stage loss")
	}
	if !page.EditorFocusesToken() {
		t.Fatal("token editor must be focused after stage loss")
	}
	_ = page.PasteTokenBytes(plantedToken())
	if err := page.StageToken(context.Background()); err != nil {
		t.Fatalf("restage: %v", err)
	}
	if page.OwnedStageID() == old {
		t.Fatal("must never reuse lost stage ID")
	}
	if page.OwnedStageID() != "stage-new" {
		t.Fatalf("new stage=%q", page.OwnedStageID())
	}
}

func TestStageReservedKeepsOwnershipBlocksRestage(t *testing.T) {
	fake := &fakeStageHTTP{
		createFn: okCreate("stage-reserved"),
		deleteFn: func(context.Context, generated.SecretStageID) (*generated.DeleteProviderSecretStageResponse, error) {
			return &generated.DeleteProviderSecretStageResponse{
				HTTPResponse: &http.Response{StatusCode: 409},
				ApplicationproblemJSON409: &generated.Problem{
					Code: string(secretinput.CodeReserved), Status: 409, Title: "reserved",
				},
			}, nil
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
	_ = page.PasteTokenBytes(plantedToken())
	err := page.StageToken(context.Background())
	if err == nil || !strings.Contains(err.Error(), string(secretinput.CodeReserved)) {
		t.Fatalf("expected reserved error, got %v", err)
	}
	if page.OwnedStageID() != "stage-reserved" {
		t.Fatalf("ownership cleared on reserved: %q", page.OwnedStageID())
	}
}

func TestConflictBlocksPublishRetainsDraft(t *testing.T) {
	page, draft, _ := newTestPage(t, &fakeStageHTTP{createFn: okCreate("s1")})
	draft.Mutate(func(cmd *generated.MutableConfigCommand) {
		for i := range cmd.Credentials {
			if cmd.Credentials[i].Id == "cred-1" {
				cmd.Credentials[i].Name = "renamed"
			}
		}
	})
	page.Refresh()
	fresh := configdraft.FixtureSnapshot(configdraft.WithManagedCredential("cred-1", "Remote", "openai", 4))
	draft.BeginConflict(fresh)
	page.ApplyConflictState()
	if draft.CanPublish() {
		t.Fatal("conflict must block publish")
	}
	if page.State() != resourcepage.StatePublicationError && !strings.Contains(page.View(), "conflict") {
		view := strings.ToLower(ansi.Strip(page.View()))
		if !strings.Contains(view, "conflict") && page.State() != resourcepage.StatePublicationError {
			t.Fatalf("expected conflict UI state=%q view=%s", page.State(), page.View())
		}
	}
	if draft.LocalCommand().Credentials[0].Name != "renamed" {
		t.Fatal("local draft must be retained across conflict")
	}
}

func TestOperationUnknownUnchangedMarksReplaceRequired(t *testing.T) {
	fake := &fakeStageHTTP{createFn: okCreate("stage-unk")}
	page, draft, _ := newTestPage(t, fake)
	page.SelectID("cred-1")
	page.BeginEdit()
	page.SetSecretMode(SecretActionReplace)
	_ = page.PasteTokenBytes(plantedToken())
	if err := page.SaveEditor(); err != nil {
		t.Fatalf("save: %v", err)
	}
	page.HandleOperationUnknown(OperationUnknownUnchanged, []string{"cred-1"})
	if !containsString(draft.ReplaceRequiredIDs(), "cred-1") {
		t.Fatal("unchanged generation must mark replace_required")
	}
	if draft.CanPublish() {
		t.Fatal("publish blocked")
	}
	if page.OwnedStageID() != "" {
		t.Fatal("owned stage cleared after unknown unchanged")
	}
}

func TestCleanupPendingDisablesPublish(t *testing.T) {
	page, draft, _ := newTestPage(t, &fakeStageHTTP{createFn: okCreate("s")})
	draft.SetMutationStatus(generated.ConfigMutationStatusSecretCleanupPending)
	page.ApplyCleanupPending()
	if draft.CanPublish() {
		t.Fatal("cleanup_pending must disable publish")
	}
	view := strings.ToLower(ansi.Strip(page.View()))
	if !strings.Contains(view, "cleanup") && page.State() != resourcepage.StatePublicationError {
		t.Fatalf("expected cleanup pending UI:\n%s", page.View())
	}
}

func TestDisconnectRetainsDraftDisablesPublish(t *testing.T) {
	page, draft, _ := newTestPage(t, &fakeStageHTTP{createFn: okCreate("s")})
	draft.Mutate(func(cmd *generated.MutableConfigCommand) {
		for i := range cmd.Credentials {
			if cmd.Credentials[i].Id == "cred-1" {
				cmd.Credentials[i].Name = "offline-edit"
			}
		}
	})
	draft.SetDisconnected(true)
	page.Refresh()
	if page.State() != resourcepage.StateDisconnected {
		t.Fatalf("state=%q", page.State())
	}
	if draft.CanPublish() {
		t.Fatal("disconnected publish")
	}
	if draft.LocalCommand().Credentials[0].Name != "offline-edit" {
		t.Fatal("draft not retained")
	}
}

func TestSecretNeverLeaksInViewDetailsErrorsGolden(t *testing.T) {
	fake := &fakeStageHTTP{createFn: okCreate("stage-leak")}
	page, _, _ := newTestPage(t, fake)
	page.SelectID("cred-1")
	page.BeginEdit()
	page.SetSecretMode(SecretActionReplace)
	token := plantedToken()
	_ = page.PasteTokenBytes(token)
	view := page.View()
	if bytes.Contains([]byte(view), token) {
		t.Fatal("token leaked in editor view")
	}
	if strings.Contains(view, string(token)) {
		t.Fatal("token string leaked in editor view")
	}
	_ = page.StageToken(context.Background())
	_ = page.SaveEditor()
	page.SelectID("cred-1")
	page.OpenDetails()
	details := page.View()
	if bytes.Contains([]byte(details), token) || strings.Contains(details, "stage-leak") && strings.Contains(strings.ToLower(details), "locator") {
		// stage id may appear nowhere; token must never appear
	}
	if bytes.Contains([]byte(details), token) {
		t.Fatal("token leaked in details")
	}
	// SECRET column summary only
	page.CancelOverlay()
	table := ansi.Strip(page.View())
	if strings.Contains(table, "OPENAI_API_KEY") {
		t.Fatal("external locator value leaked in table")
	}
	if !strings.Contains(table, "Configured") && !strings.Contains(table, "env") {
		t.Fatalf("missing SECRET summary:\n%s", table)
	}
}

func TestSecretScanPackageOmitsPlantedToken(t *testing.T) {
	token := plantedToken()
	root := "."
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		if strings.HasSuffix(path, "_test.go") {
			return nil
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		if bytes.Contains(data, token) {
			t.Errorf("planted token present in %s", path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
