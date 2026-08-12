package formui

import (
	"strings"
	"testing"

	"github.com/asheshgoplani/agent-deck/internal/i18n"
	"github.com/charmbracelet/lipgloss"
)

func TestRenderResponsiveDialogAndInlineError(t *testing.T) {
	for _, size := range []struct{ width, height int }{{70, 30}, {90, 30}, {120, 30}, {160, 45}} {
		layout := Render(Spec{
			Title: "Create profile", Context: "Agent CLI configuration", Width: size.width, Height: size.height,
			Fields: []Field{
				{ID: "name", Label: "Name", Value: "coding", Required: true, Section: "Basics"},
				{ID: "launcher", Label: "CLI", Value: "codex", Kind: Select, Error: "Choose a supported CLI"},
				{ID: "id", Label: "Resource ID", Value: "coding", Kind: ReadOnly, Advanced: true},
			},
		})
		if lipgloss.Width(layout.View) != size.width || lipgloss.Height(layout.View) != size.height {
			t.Fatalf("size %dx%d rendered %dx%d", size.width, size.height, lipgloss.Width(layout.View), lipgloss.Height(layout.View))
		}
		if !strings.Contains(layout.View, "Choose a supported CLI") || strings.Contains(layout.View, "Resource ID") {
			t.Fatalf("unexpected collapsed view:\n%s", layout.View)
		}
	}
}

func TestFriendlyLabelsAndErrorsFollowLocale(t *testing.T) {
	i18n.SetLocale("zh-CN")
	t.Cleanup(func() { i18n.SetLocale("en") })
	if got := FriendlyLabel("default_route_id"); got != "流量规则" {
		t.Fatalf("label=%q", got)
	}
	field, detail := CleanError("$.name: required")
	if field != "name" || detail != "此项必填" {
		t.Fatalf("localized error=%q, %q", field, detail)
	}
	view := Render(Spec{Title: "创建", Width: 70, Height: 30, Fields: []Field{{ID: "name", Label: FriendlyLabel("name"), Section: "Basics"}}}).View
	if !strings.Contains(view, "基本信息") || !strings.Contains(view, "名称") {
		t.Fatalf("localized form missing labels:\n%s", view)
	}
}

func TestSelectUsesPlaceholderArrowAndOnlyExpandsOnDemand(t *testing.T) {
	field := Field{
		ID: "launcher", Label: "CLI", Kind: Select, Section: "Basics",
		Options: []Option{{Label: "codex", Value: "codex", Selected: true}, {Label: "claude", Value: "claude"}},
	}
	collapsed := Render(Spec{Title: "Create", Width: 90, Height: 30, Fields: []Field{field}}).View
	if !strings.Contains(collapsed, "[ Choose CLI...") || !strings.Contains(collapsed, "▾ ]") {
		t.Fatalf("select affordance missing:\n%s", collapsed)
	}
	if strings.Contains(collapsed, "› codex") || strings.Contains(collapsed, " v") {
		t.Fatalf("select rendered legacy or prematurely expanded UI:\n%s", collapsed)
	}

	field.Expanded = true
	expanded := Render(Spec{Title: "Create", Width: 90, Height: 30, Fields: []Field{field}}).View
	if !strings.Contains(expanded, "╭") || !strings.Contains(expanded, "› codex") || !strings.Contains(expanded, "claude") || !strings.Contains(expanded, "╰") {
		t.Fatalf("expanded choices missing:\n%s", expanded)
	}
	if strings.Contains(expanded, "[x]") {
		t.Fatalf("single-select rendered a multi-select marker:\n%s", expanded)
	}

	field.Kind = MultiSelect
	multi := Render(Spec{Title: "Create", Width: 90, Height: 30, Fields: []Field{field}}).View
	if !strings.Contains(multi, "codex [x]") {
		t.Fatalf("multi-select marker missing:\n%s", multi)
	}
}

func TestCycleValueWrapsFixedChoices(t *testing.T) {
	options := []string{"codex", "claude", "opencode"}
	for _, test := range []struct {
		current string
		delta   int
		want    string
	}{
		{current: "codex", delta: 1, want: "claude"},
		{current: "opencode", delta: 1, want: "codex"},
		{current: "codex", delta: -1, want: "opencode"},
		{current: "", delta: 1, want: "codex"},
		{current: "", delta: -1, want: "opencode"},
	} {
		got, ok := CycleValue(test.current, options, test.delta)
		if !ok || got != test.want {
			t.Fatalf("CycleValue(%q, %d)=(%q, %v), want (%q, true)", test.current, test.delta, got, ok, test.want)
		}
	}
}

func TestFocusedTextFieldUsesTextInputPrompt(t *testing.T) {
	view := Render(Spec{
		Title: "Create", Width: 90, Height: 30,
		Fields: []Field{{ID: "name", Label: "Name", Value: "coding", Kind: Text, Section: "Basics"}},
	}).View
	if !strings.Contains(view, "> coding") || strings.Contains(view, "┃") || strings.Contains(view, "[ coding") {
		t.Fatalf("focused text field does not look editable:\n%s", view)
	}
}

func TestTextInputControlFillsAssignedWidth(t *testing.T) {
	for _, active := range []bool{false, true} {
		control := renderInputControl(Field{ID: "name", Label: "Name", Kind: Text}, 48, active)
		if got := lipgloss.Width(control); got != 48 {
			t.Fatalf("active=%v width=%d want 48", active, got)
		}
	}
}

func TestToggleUsesCheckboxInsteadOfTextInput(t *testing.T) {
	for _, test := range []struct {
		value string
		want  string
	}{
		{value: "true", want: "[x] On"},
		{value: "false", want: "[ ] Off"},
	} {
		view := Render(Spec{
			Title: "Edit", Width: 90, Height: 30,
			Fields: []Field{{ID: "private", Label: "Private network", Value: test.value, Kind: Toggle, Section: "Advanced"}},
		}).View
		if !strings.Contains(view, test.want) || strings.Contains(view, "> "+test.value) {
			t.Fatalf("toggle %q rendered as an input:\n%s", test.value, view)
		}
	}
}

func TestLongInlineLabelDoesNotCreateSecondControlRow(t *testing.T) {
	view := Render(Spec{
		Title: "Edit", Width: 90, Height: 30,
		Fields: []Field{{ID: "private", Label: "Allow private network access with a very long label", Value: "true", Kind: Toggle}},
	}).View
	if strings.Contains(view, "true") || strings.Count(view, "[x] On") != 1 {
		t.Fatalf("long toggle label produced an unstable control:\n%s", view)
	}
}

func TestCleanErrorAndUniqueID(t *testing.T) {
	field, detail := CleanError("$.models[0]: at least one model is required")
	if field != "models" || detail != "at least one model is required" {
		t.Fatalf("CleanError = %q, %q", field, detail)
	}
	used := map[string]bool{"my-route": true}
	if got := UniqueID("My Route", "route", used); got != "my-route-2" {
		t.Fatalf("UniqueID = %q", got)
	}
}

func TestLayoutHitMapScrollAdvancedAndSecretMasking(t *testing.T) {
	fields := []Field{{ID: "name", Label: "Name", Section: "Basics"}, {ID: "secret", Label: "API key", Kind: Secret, Value: "must-not-leak"}}
	for i := 0; i < 12; i++ {
		fields = append(fields, Field{ID: "advanced-" + string(rune('a'+i)), Label: "Advanced", Value: "value", Advanced: true, Section: "Advanced"})
	}
	layout := Render(Spec{Title: "Create", Fields: fields, Focus: len(fields) - 1, AdvancedExpanded: true, Width: 70, Height: 30})
	if strings.Contains(layout.View, "must-not-leak") {
		t.Fatal("secret leaked into rendered form")
	}
	if layout.Scroll == 0 {
		t.Fatal("last advanced field should scroll into view")
	}
	if _, ok := layout.FieldLines[fields[len(fields)-1].ID]; !ok {
		t.Fatalf("focused field missing from hit map: %#v", layout.FieldLines)
	}
	for _, line := range layout.FieldLines {
		if line < 0 || line >= 30 {
			t.Fatalf("field hit outside viewport: %d", line)
		}
	}
}
