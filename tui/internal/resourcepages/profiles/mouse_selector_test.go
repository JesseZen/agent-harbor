package profiles

import (
	"testing"

	"github.com/asheshgoplani/agent-deck/internal/configdraft"
	"github.com/asheshgoplani/agent-deck/internal/coreclient/generated"
	tea "github.com/charmbracelet/bubbletea"
)

func TestMouseOpensProfileSelectorAndChoosesOption(t *testing.T) {
	snapshot := configdraft.FixtureSnapshot()
	snapshot.MutableConfig.Routes = []generated.RouteConfig{{Id: "route-a"}, {Id: "route-b"}}
	page := NewPage(configdraft.Load(snapshot), Options{})
	page.SetSize(120, 30)
	page.openEditor(true)
	for index, name := range page.fieldOrder {
		if name == "default_route_id" {
			page.fieldIndex = index
			break
		}
	}

	layout := page.editorLayout()
	fieldY, ok := layout.FieldLines["default_route_id"]
	if !ok {
		t.Fatal("default_route_id missing from field hit map")
	}
	_, _ = page.updateEditorMouse(tea.MouseMsg{X: 40, Y: fieldY, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	if page.mode != modeRefSelect {
		t.Fatal("clicking the profile selector should open it")
	}

	_, _ = page.updateRefSelect(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("rb")})
	if len(page.refChoices) != 1 || page.refChoices[0] != "route-b" {
		t.Fatalf("fuzzy profile choices=%v", page.refChoices)
	}
	layout = page.editorLayout()
	optionY := -1
	for line, option := range layout.OptionLines {
		if option == 0 {
			optionY = line
			break
		}
	}
	if optionY < 0 {
		t.Fatal("open profile selector has no clickable option")
	}
	_, _ = page.updateEditorMouse(tea.MouseMsg{X: 40, Y: optionY, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	if page.fields["default_route_id"] != "route-b" || page.mode != modeEdit {
		t.Fatalf("mouse selection route=%q mode=%v", page.fields["default_route_id"], page.mode)
	}
}

func TestTypingInProfileLauncherStartsSearch(t *testing.T) {
	draft := configdraft.Load(configdraft.FixtureSnapshot())
	page := NewPage(draft, Options{})
	page.openEditor(true)
	page.fieldIndex = indexOf(page.fieldOrder, "launcher")
	page.fields["launcher"] = "codex"

	_, _ = page.updateEditor(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("opn")})
	if page.mode != modeRefSelect || page.refField != "launcher" || page.refFilter != "opn" {
		t.Fatalf("launcher search mode=%v field=%q filter=%q", page.mode, page.refField, page.refFilter)
	}
	if page.fields["launcher"] != "codex" {
		t.Fatalf("typing in launcher mutated value to %q", page.fields["launcher"])
	}
	if len(page.refChoices) != 1 || page.refChoices[0] != "opencode" {
		t.Fatalf("launcher fuzzy choices=%v", page.refChoices)
	}
}
