package commandmenu

import (
	"strings"
	"testing"

	"github.com/asheshgoplani/agent-deck/internal/ui"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

func TestSearchCursorUsesBrightBlockNotReversedInvisible(t *testing.T) {
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(prev) })
	ui.InitTheme("tokyonight")

	menu := New(Options{
		Title:      "Spotlight",
		Items:      []Item{{ID: "a", LabelColumns: []string{"A", ""}, Shortcut: "/a"}},
		HideCancel: true,
		Prompt:     " search: ",
	})
	menu.Open()
	view := menu.ViewOver(strings.Repeat(".\n", 40), 80, 24)

	var searchLine string
	for _, line := range strings.Split(view, "\n") {
		if strings.Contains(line, "search:") {
			searchLine = line
			break
		}
	}
	if searchLine == "" {
		t.Fatal("search row missing")
	}
	if !strings.Contains(searchLine, "█") {
		t.Fatalf("search row missing block caret:\n%q", searchLine)
	}
	if strings.Contains(searchLine, ";7;") || strings.Contains(searchLine, "[7;") {
		t.Fatalf("caret must not use Reverse (makes █ paint panel-colored):\n%q", searchLine)
	}
	textFG := trueColorSignature(string(chromeText()), false)
	if textFG == "" || !strings.Contains(searchLine, textFG) {
		t.Fatalf("caret missing bright text foreground %q:\n%q", textFG, searchLine)
	}
}
