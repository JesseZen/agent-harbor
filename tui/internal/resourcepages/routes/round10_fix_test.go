package routes

import (
	"testing"

	"github.com/asheshgoplani/agent-deck/internal/coreclient/generated"
)

func TestEditClearsOptionalContentPolicyMaxInspectionBytes(t *testing.T) {
	page, draft := newTestPage(t)
	draft.Mutate(func(cmd *generated.MutableConfigCommand) {
		cmd.ContentPolicies = append(cmd.ContentPolicies, sampleContentPolicy("cp-opt"))
	})
	page.Refresh()
	page.SetKind(KindContentPolicies)
	page.SelectID("cp-opt")
	page.BeginEdit()
	page.editor.values["max_inspection_bytes"] = ""
	if err := page.SaveEditor(); err != nil {
		t.Fatalf("SaveEditor: %v", err)
	}
	cp, ok := findContentPolicy(draft.LocalCommand(), "cp-opt")
	if !ok {
		t.Fatal("policy missing after save")
	}
	if cp.MaxInspectionBytes != nil {
		t.Fatalf("cleared optional max_inspection_bytes must be nil, got %v", *cp.MaxInspectionBytes)
	}
}

func TestBuildContentPolicyEditClearsOptionalPointers(t *testing.T) {
	base := sampleContentPolicy("cp-x")
	got, err := buildContentPolicy(map[string]string{
		"id":                   "cp-x",
		"mode":                 "",
		"max_inspection_bytes": "",
	}, base, false)
	if err != nil {
		t.Fatal(err)
	}
	if got.Mode != nil {
		t.Fatalf("empty mode on edit must clear, got %v", *got.Mode)
	}
	if got.MaxInspectionBytes != nil {
		t.Fatalf("empty max_inspection_bytes on edit must clear, got %v", *got.MaxInspectionBytes)
	}
}
