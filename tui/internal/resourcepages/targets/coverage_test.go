package targets

import (
	"context"
	"strings"
	"testing"

	"github.com/asheshgoplani/agent-deck/internal/coreclient/generated"
	"github.com/asheshgoplani/agent-deck/internal/resourcepage"
	tea "github.com/charmbracelet/bubbletea"
)

func TestEditorKeyboardPathsAndSelectors(t *testing.T) {
	page, _, _ := newTestPage(t, &fakeStageHTTP{createFn: okCreate("s")})
	page.BeginCreate()
	_ = page.View()
	model, _ := page.Update(tea.KeyMsg{Type: tea.KeyTab})
	page = model.(*Page)
	model, _ = page.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x")})
	page = model.(*Page)
	model, _ = page.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	page = model.(*Page)
	model, _ = page.Update(tea.KeyMsg{Type: tea.KeyDown})
	page = model.(*Page)
	model, _ = page.Update(tea.KeyMsg{Type: tea.KeyUp})
	page = model.(*Page)
	// Focus secret_mode, open the selector, then choose the next option.
	page.editor.cursor = 4 // secret_mode
	model, _ = page.Update(tea.KeyMsg{Type: tea.KeyEnter})
	page = model.(*Page)
	model, _ = page.Update(tea.KeyMsg{Type: tea.KeyDown})
	page = model.(*Page)
	model, _ = page.Update(tea.KeyMsg{Type: tea.KeyEnter})
	page = model.(*Page)
	if page.EditorSecretMode() == SecretActionPreserve {
		t.Fatal("create should not land on preserve via cycle")
	}
	page.SetSecretMode(SecretActionExternal)
	page.editor.cursor = 0
	for i, name := range page.editor.fieldNames() {
		if name == "external_kind" {
			page.editor.cursor = i
			break
		}
	}
	model, _ = page.Update(tea.KeyMsg{Type: tea.KeyEnter})
	page = model.(*Page)
	model, _ = page.Update(tea.KeyMsg{Type: tea.KeyDown})
	page = model.(*Page)
	model, _ = page.Update(tea.KeyMsg{Type: tea.KeyEnter})
	page = model.(*Page)
	if page.editor.values["external_kind"] == "env" {
		t.Fatal("expected external kind cycle")
	}
	for i, name := range page.editor.fieldNames() {
		if name == "external_exportable" {
			page.editor.cursor = i
			break
		}
	}
	model, _ = page.Update(tea.KeyMsg{Type: tea.KeySpace})
	page = model.(*Page)
	_ = page.View()
	model, _ = page.Update(tea.KeyMsg{Type: tea.KeyShiftTab})
	page = model.(*Page)
	model, _ = page.Update(tea.KeyMsg{Type: tea.KeyEsc})
	page = model.(*Page)
	if page.overlay != overlayNone {
		t.Fatal("esc should close editor")
	}
}

func TestEditExternalLoadsValuesAndDetails(t *testing.T) {
	page, _, _ := newTestPage(t, &fakeStageHTTP{createFn: okCreate("s")})
	page.SelectID("cred-ext")
	page.BeginEdit()
	if page.EditorSecretMode() != SecretActionExternal {
		t.Fatalf("mode=%q", page.EditorSecretMode())
	}
	if page.editor.values["external_env_name"] != "OPENAI_API_KEY" {
		t.Fatalf("env name=%q", page.editor.values["external_env_name"])
	}
	_ = page.View()
	page.CancelOverlay()
	page.SelectID("cred-ext")
	page.OpenDetails()
	view := page.View()
	if !strings.Contains(view, "external_ref") && !strings.Contains(view, "env") {
		t.Fatalf("details missing external summary:\n%s", view)
	}
	if !page.ShowingDetails() {
		t.Fatal("expected details overlay")
	}
	model, _ := page.Update(tea.KeyMsg{Type: tea.KeyEsc})
	page = model.(*Page)
	_ = page.VisibleIDs()
}

func TestConfirmDeleteAndKeychainExternalCreate(t *testing.T) {
	page, draft, _ := newTestPage(t, &fakeStageHTTP{createFn: okCreate("stage-kc")})
	draft.Mutate(func(cmd *generated.MutableConfigCommand) {
		cmd.Targets = nil
	})
	page.Refresh()
	page.SelectID("cred-ext")
	model, _ := page.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	page = model.(*Page)
	view := page.View()
	if !strings.Contains(view, "Delete") {
		t.Fatalf("confirm missing:\n%s", view)
	}
	model, _ = page.Update(tea.KeyMsg{Type: tea.KeyEsc})
	page = model.(*Page)

	page.BeginCreate()
	page.SetEditorValues(map[string]string{
		"id":                        "cred-kc",
		"name":                      "KC",
		"provider":                  "anthropic",
		"external_kind":             "keychain",
		"external_keychain_service": "svc",
		"external_keychain_account": "acct",
		"external_exportable":       "true",
	})
	page.SetSecretMode(SecretActionExternal)
	if err := page.SaveEditor(); err != nil {
		t.Fatalf("create keychain: %v", err)
	}
	found := false
	for _, c := range draft.LocalCommand().Credentials {
		if c.Id == "cred-kc" {
			found = true
			action, err := c.SecretAction.AsCredentialSecretAction2()
			if err != nil || action.Ref.Keychain == nil {
				t.Fatalf("action=%#v err=%v", c.SecretAction, err)
			}
		}
	}
	if !found {
		t.Fatal("cred-kc missing")
	}
}

func TestTargetsEndpointsStripAndRows(t *testing.T) {
	page, _, _ := newTestPage(t, &fakeStageHTTP{createFn: okCreate("s")})
	page.SetKind(KindTargets)
	view := page.View()
	for _, col := range []string{"ID", "NAME", "ADAPTER", "ENDPOINT", "CREDENTIAL"} {
		if !strings.Contains(view, col) {
			t.Fatalf("targets missing %s:\n%s", col, view)
		}
	}
	page.SelectID("tgt-1")
	page.OpenDetails()
	if !strings.Contains(page.View(), "credential_id") {
		t.Fatalf("target details:\n%s", page.View())
	}
	page.CancelOverlay()
	page.SetKind(KindEndpoints)
	view = page.View()
	if !strings.Contains(view, "BASE_URL") {
		t.Fatalf("endpoints columns:\n%s", view)
	}
	page.SelectID("ep-1")
	page.OpenDetails()
	if !strings.Contains(page.View(), "base_url") {
		t.Fatalf("endpoint details:\n%s", page.View())
	}
	_ = KindTargets.DescriptorKind()
	_ = KindEndpoints.DescriptorKind()
	_ = KindCredentials.DescriptorKind()
}

func TestUIStateBannersAndPublishGuards(t *testing.T) {
	page, draft, _ := newTestPage(t, &fakeStageHTTP{createFn: okCreate("s")})
	page.SetState(resourcepage.StateLoading)
	if !strings.Contains(page.View(), "Loading") {
		t.Fatal("loading banner")
	}
	page.forceState = false
	draft.Mutate(func(cmd *generated.MutableConfigCommand) { cmd.Credentials = nil })
	page.Refresh()
	if page.State() != resourcepage.StateEmpty {
		t.Fatalf("state=%q", page.State())
	}
	page.SetState(resourcepage.StateStale)
	_ = page.BannerOffset()
	page.HandleOperationUnknown(OperationUnknownSuccess, nil)
	page.HandleOperationUnknown(OperationUnknownConflict, nil)
	page.DiscardOwnedStages(context.Background())
}

func TestReplaceTokenTypingInEditor(t *testing.T) {
	page, _, _ := newTestPage(t, &fakeStageHTTP{createFn: okCreate("stage-type")})
	page.SelectID("cred-1")
	page.BeginEdit()
	page.SetSecretMode(SecretActionReplace)
	// move to token field
	for i, name := range page.editor.fieldNames() {
		if name == "token" {
			page.editor.cursor = i
			break
		}
	}
	model, _ := page.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a', 'b'}})
	page = model.(*Page)
	if page.TokenBufferLen() == 0 {
		t.Fatal("expected token bytes from typing")
	}
	view := page.View()
	if strings.Contains(view, "ab") && !strings.Contains(view, "•") {
		t.Fatal("raw token visible")
	}
	model, _ = page.Update(tea.KeyMsg{Type: tea.KeyCtrlS})
	page = model.(*Page)
}

func TestMouseStripHit(t *testing.T) {
	page, _, _ := newTestPage(t, &fakeStageHTTP{createFn: okCreate("s")})
	page.SetKind(KindCredentials)
	_ = page.View()
	// First strip segment is the default Simple Upstreams view.
	model, _ := page.Update(tea.MouseMsg{X: 1, Y: 0, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	page = model.(*Page)
	if page.Kind() != KindUpstreams {
		t.Fatalf("strip click kind=%q", page.Kind())
	}
}

func TestValidationErrorsAndEndpointRefs(t *testing.T) {
	page, draft, _ := newTestPage(t, &fakeStageHTTP{createFn: okCreate("s")})
	page.BeginCreate()
	page.SetEditorValues(map[string]string{"id": "", "name": "", "provider": "nope"})
	page.SetSecretMode(SecretActionReplace)
	if err := page.SaveEditor(); err == nil {
		t.Fatal("expected validation error")
	}
	_ = page.EditorFieldNames()
	page.CancelOverlay()
	page.SetKind(KindEndpoints)
	page.SelectID("ep-1")
	blocked, paths := page.TryDelete()
	if !blocked || len(paths) == 0 {
		t.Fatalf("endpoint inbound block=%v paths=%v", blocked, paths)
	}
	_ = page.View()
	page.CancelOverlay()
	// provider cycle on create
	page.SetKind(KindCredentials)
	page.BeginCreate()
	for i, name := range page.editor.fieldNames() {
		if name == "provider" {
			page.editor.cursor = i
			break
		}
	}
	model, _ := page.Update(tea.KeyMsg{Type: tea.KeyEnter})
	page = model.(*Page)
	_ = page.editor.values["provider"]
	// replace_required opens focused token editor
	draft.MarkReplaceRequired("cred-1")
	page.SelectID("cred-1")
	page.BeginEdit()
	if !page.EditorFocusesToken() {
		t.Fatal("replace_required should focus token")
	}
}

func TestCreateExternalEnvAndFile(t *testing.T) {
	page, draft, _ := newTestPage(t, &fakeStageHTTP{createFn: okCreate("s")})
	page.BeginCreate()
	page.SetEditorValues(map[string]string{
		"id": "cred-env", "name": "E", "provider": "openai",
		"external_kind": "env", "external_env_name": "FOO_TOKEN", "external_exportable": "false",
	})
	page.SetSecretMode(SecretActionExternal)
	if err := page.SaveEditor(); err != nil {
		t.Fatal(err)
	}
	page.BeginCreate()
	page.SetEditorValues(map[string]string{
		"id": "cred-file", "name": "F", "provider": "openai",
		"external_kind": "file", "external_file_path": "/tmp/x", "external_exportable": "true",
	})
	page.SetSecretMode(SecretActionExternal)
	if err := page.SaveEditor(); err != nil {
		t.Fatal(err)
	}
	ids := map[string]bool{}
	for _, c := range draft.LocalCommand().Credentials {
		ids[string(c.Id)] = true
	}
	if !ids["cred-env"] || !ids["cred-file"] {
		t.Fatalf("ids=%v", ids)
	}
}
