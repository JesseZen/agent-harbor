package targets

import (
	"strings"
	"testing"

	"github.com/asheshgoplani/agent-deck/internal/configdraft"
	"github.com/asheshgoplani/agent-deck/internal/coreclient/generated"
	"github.com/charmbracelet/x/ansi"
)

func TestPreserveEditKeepsSecretAction(t *testing.T) {
	page, draft, _ := newTestPage(t, &fakeStageHTTP{createFn: okCreate("unused")})
	page.SelectID("cred-1")
	page.BeginEdit()
	if page.EditorSecretMode() != SecretActionPreserve {
		t.Fatalf("default edit mode=%q", page.EditorSecretMode())
	}
	page.SetEditorValues(map[string]string{"name": "Primary-renamed"})
	if err := page.SaveEditor(); err != nil {
		t.Fatalf("save: %v", err)
	}
	cmd := draft.LocalCommand()
	var found bool
	for _, c := range cmd.Credentials {
		if c.Id == "cred-1" {
			found = true
			if c.Name != "Primary-renamed" {
				t.Fatalf("name=%q", c.Name)
			}
			action, err := c.SecretAction.AsCredentialSecretAction0()
			if err != nil || action.Mode != generated.CredentialSecretActionPreserve {
				t.Fatalf("secret action not preserve: %#v err=%v", c.SecretAction, err)
			}
		}
	}
	if !found {
		t.Fatal("cred-1 missing")
	}
}

func TestReplaceTokenSetsCredentialReplace(t *testing.T) {
	fake := &fakeStageHTTP{createFn: okCreate("stage-ok")}
	page, draft, _ := newTestPage(t, fake)
	page.SelectID("cred-1")
	page.BeginEdit()
	page.SetSecretMode(SecretActionReplace)
	if err := page.PasteTokenBytes(plantedToken()); err != nil {
		t.Fatalf("paste: %v", err)
	}
	if err := page.SaveEditor(); err != nil {
		t.Fatalf("save: %v", err)
	}
	action, err := draft.LocalCommand().Credentials[0].SecretAction.AsCredentialSecretAction1()
	if err != nil {
		t.Fatalf("replace action: %v", err)
	}
	if action.Mode != generated.CredentialSecretActionReplace || string(action.StageId) != "stage-ok" {
		t.Fatalf("action=%#v", action)
	}
	if page.TokenBufferLen() != 0 {
		t.Fatal("buffer should be zeroed after successful stage+save")
	}
	if !draft.DomainDirty(configdraft.DomainTargets) {
		t.Fatal("targets domain dirty expected")
	}
}

func TestExternalRefSave(t *testing.T) {
	page, draft, _ := newTestPage(t, &fakeStageHTTP{createFn: okCreate("x")})
	page.SelectID("cred-1")
	page.BeginEdit()
	page.SetSecretMode(SecretActionExternal)
	page.SetEditorValues(map[string]string{
		"external_kind":       "file",
		"external_file_path":  "/run/secrets/token",
		"external_exportable": "false",
	})
	if err := page.SaveEditor(); err != nil {
		t.Fatalf("save: %v", err)
	}
	action, err := draft.LocalCommand().Credentials[0].SecretAction.AsCredentialSecretAction2()
	if err != nil {
		t.Fatalf("external: %v", err)
	}
	if action.Mode != generated.CredentialSecretActionExternalRef || action.Ref.File == nil {
		t.Fatalf("action=%#v", action)
	}
	if action.Ref.File.Path != "/run/secrets/token" {
		t.Fatalf("path=%q", action.Ref.File.Path)
	}
	table := ansi.Strip(page.View())
	if !strings.Contains(table, "file") {
		t.Fatalf("SECRET cell missing file summary:\n%s", table)
	}
	if strings.Contains(table, "/run/secrets/token") {
		t.Fatal("locator path must not appear in SECRET column")
	}
}

func TestCreateRequiresReplaceOrExternal(t *testing.T) {
	page, draft, _ := newTestPage(t, &fakeStageHTTP{createFn: okCreate("stage-create")})
	page.BeginCreate()
	if page.EditorSecretMode() == SecretActionPreserve {
		t.Fatal("preserve unavailable on create")
	}
	page.SetEditorValues(map[string]string{
		"id":       "cred-new",
		"name":     "NewCred",
		"provider": "openai",
	})
	page.SetSecretMode(SecretActionPreserve)
	if err := page.SaveEditor(); err == nil {
		t.Fatal("preserve on create must fail")
	}
	page.SetSecretMode(SecretActionReplace)
	_ = page.PasteTokenBytes(plantedToken())
	if err := page.SaveEditor(); err != nil {
		t.Fatalf("create replace: %v", err)
	}
	found := false
	for _, c := range draft.LocalCommand().Credentials {
		if c.Id == "cred-new" {
			found = true
			action, err := c.SecretAction.AsCredentialSecretAction1()
			if err != nil || string(action.StageId) != "stage-create" {
				t.Fatalf("create action=%#v err=%v", c.SecretAction, err)
			}
		}
	}
	if !found {
		t.Fatal("cred-new not created")
	}
}

func TestDeleteCredentialBlockedByInboundRefs(t *testing.T) {
	page, draft, _ := newTestPage(t, &fakeStageHTTP{createFn: okCreate("s")})
	page.SelectID("cred-1")
	blocked, paths := page.TryDelete()
	if !blocked {
		t.Fatal("expected inbound ref block")
	}
	joined := strings.Join(paths, " ")
	if !strings.Contains(joined, "credential_id") {
		t.Fatalf("paths=%v", paths)
	}
	view := page.View()
	if !strings.Contains(view, "inbound") && !strings.Contains(view, "credential_id") {
		t.Fatalf("deps dialog missing:\n%s", view)
	}
	for _, c := range draft.LocalCommand().Credentials {
		if c.Id == "cred-1" {
			return
		}
	}
	t.Fatal("credential deleted despite inbound refs")
}

func TestDeleteCredentialUnblocked(t *testing.T) {
	page, draft, _ := newTestPage(t, &fakeStageHTTP{createFn: okCreate("s")})
	draft.Mutate(func(cmd *generated.MutableConfigCommand) {
		cmd.Targets = nil
	})
	page.Refresh()
	page.SelectID("cred-ext")
	page = confirmTryDelete(t, page)
	for _, c := range draft.LocalCommand().Credentials {
		if c.Id == "cred-ext" {
			t.Fatal("cred-ext still present")
		}
	}
}

func TestColumnsAndSecretSummary(t *testing.T) {
	page, _, _ := newTestPage(t, &fakeStageHTTP{createFn: okCreate("s")})
	view := ansi.Strip(page.View())
	for _, col := range []string{"ID", "NAME", "PROVIDER", "GENERATION", "SECRET"} {
		if !strings.Contains(view, col) {
			t.Fatalf("missing column %s:\n%s", col, view)
		}
	}
	if !strings.Contains(view, "Configured") {
		t.Fatalf("managed SECRET summary missing:\n%s", view)
	}
	if !strings.Contains(view, "env") {
		t.Fatalf("external SECRET summary missing:\n%s", view)
	}
	if strings.Contains(view, "OPENAI_API_KEY") {
		t.Fatal("env locator leaked")
	}
	if !strings.Contains(view, "3") {
		t.Fatalf("generation display missing:\n%s", view)
	}
}

func TestEditForcesIDAndProviderReadonly(t *testing.T) {
	page, _, _ := newTestPage(t, &fakeStageHTTP{createFn: okCreate("s")})
	page.SelectID("cred-1")
	page.BeginEdit()
	if page.EditorIDEditable() {
		t.Fatal("id must be read-only on edit")
	}
	if page.EditorProviderEditable() {
		t.Fatal("provider must be read-only on edit")
	}
	for i, name := range page.EditorFieldNames() {
		if name == "id" {
			page.editor.cursor = i
			break
		}
	}
	view := page.View()
	if !strings.Contains(view, "read-only") {
		t.Fatalf("expected read-only markers:\n%s", view)
	}
	if !strings.Contains(strings.ToLower(view), "generation") {
		t.Fatalf("generation display missing in editor:\n%s", view)
	}
}
