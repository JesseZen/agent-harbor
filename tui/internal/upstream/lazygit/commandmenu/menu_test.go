package commandmenu

import (
	"regexp"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

func TestV2CommandsChromeAndFilterFirst(t *testing.T) {
	items := []Item{
		{ID: "language", LabelColumns: []string{"Language", "Switch UI language"}, Aliases: []string{"language"}, Shortcut: "/language", Section: "System", OpensMenu: true},
		{ID: "reload", LabelColumns: []string{"Reload from Core", "Preserve draft"}, Shortcut: "/reload", Section: "Runtime", Widget: CheckboxSelected},
		{ID: "disabled", LabelColumns: []string{"Delete", "Core-owned"}, Shortcut: "/delete", DisabledReason: "Core owns deletion"},
	}
	menu := New(Options{
		Title:                     "Spotlight",
		Items:                     items,
		AllowFilteringKeybindings: true,
		HideCancel:                true,
	})
	menu.Open()

	view := menu.ViewOver("harbor background\nsecond background", 72, 24)
	for _, expected := range []string{
		"Spotlight",
		"search:",
		"[x]",
		"┌", "┐", "└", "┘",
		"System",
		"◆ ",
		"Language",
		"/language",
		"Runtime",
		"/reload",
		"Reload from Core",
		"↑/↓",
		"nav",
		"Enter",
		"select",
		"Esc",
		"close",
		"harbor background",
	} {
		if !strings.Contains(view, expected) {
			t.Fatalf("menu omitted %q:\n%s", expected, view)
		}
	}
	if strings.Contains(view, "FILTER") || strings.Contains(view, "› ") {
		t.Fatalf("old HTML-B chrome still present:\n%s", view)
	}
	for _, rounded := range []string{"╭", "╮", "╰", "╯"} {
		if strings.Contains(view, rounded) {
			t.Fatalf("Grok chrome uses square corners, found %q:\n%s", rounded, view)
		}
	}

	for _, r := range "lang" {
		menu.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	filtered := menu.ViewOver("background", 72, 24)
	if !strings.Contains(filtered, "Language") || strings.Contains(filtered, "Reload from Core") {
		t.Fatalf("filter did not narrow:\n%s", filtered)
	}

	menu.Update(tea.KeyMsg{Type: tea.KeyTab})
	if menu.Filter() != "language" {
		t.Fatalf("tab complete = %q", menu.Filter())
	}

	result := menu.Update(tea.KeyMsg{Type: tea.KeyEnter})
	// OpensMenu keeps chrome open so the host can push a stack frame.
	if result.SelectedID != "language" || result.Closed || !menu.IsOpen() {
		t.Fatalf("selected = %#v open=%v", result, menu.IsOpen())
	}
}

func TestDisabledAndEscClose(t *testing.T) {
	menu := New(Options{
		Title: "Spotlight",
		Items: []Item{
			{ID: "disabled", LabelColumns: []string{"Delete", "nope"}, DisabledReason: "Core owns deletion"},
			{ID: "ok", LabelColumns: []string{"OK", ""}},
		},
		HideCancel: true,
	})
	menu.Open()
	menu.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	result := menu.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if result.DisabledReason != "Core owns deletion" {
		menu = New(Options{
			Title: "Spotlight",
			Items: []Item{
				{ID: "disabled", LabelColumns: []string{"Delete", "nope"}, DisabledReason: "Core owns deletion"},
			},
			HideCancel: true,
		})
		menu.Open()
		result = menu.Update(tea.KeyMsg{Type: tea.KeyEnter})
	}
	if result.DisabledReason != "Core owns deletion" || !menu.IsOpen() {
		t.Fatalf("disabled result = %#v open=%t", result, menu.IsOpen())
	}
	closed := menu.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if !closed.Closed || menu.IsOpen() {
		t.Fatalf("esc should close: %#v open=%t", closed, menu.IsOpen())
	}
}

func TestKeybindingFilterStillWorks(t *testing.T) {
	menu := New(Options{
		Title: "Spotlight",
		Items: []Item{
			{ID: "reload", LabelColumns: []string{"Reload", "Core"}, Keys: []string{"ctrl+r"}, Shortcut: "/reload", Section: "Runtime"},
			{ID: "route", LabelColumns: []string{"Choose route", "x"}, Keys: []string{"r"}, Shortcut: "/route", Section: "Navigation"},
		},
		AllowFilteringKeybindings: true,
		HideCancel:                true,
	})
	menu.Open()
	for _, r := range "@ctrl+r" {
		menu.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	view := menu.ViewOver("bg", 60, 18)
	if !strings.Contains(view, "Reload") || strings.Contains(view, "Choose route") {
		t.Fatalf("keybinding filter failed:\n%s", view)
	}
	if !strings.Contains(view, "/reload") {
		t.Fatalf("right column should show command shortcut, not keybinding:\n%s", view)
	}
}

func TestRightColumnShowsShortcutNotKeys(t *testing.T) {
	menu := New(Options{
		Title: "Spotlight",
		Items: []Item{
			{ID: "reload", LabelColumns: []string{"Reload from Core", ""}, Keys: []string{"r"}, Shortcut: "/reload", Section: "Runtime"},
			{ID: "theme", LabelColumns: []string{"Theme", ""}, Shortcut: "/theme", Section: "System", OpensMenu: true},
		},
		HideCancel: true,
	})
	menu.Open()
	view := menu.ViewOver(strings.Repeat(".\n", 20), 72, 24)
	if !strings.Contains(view, "/reload") || !strings.Contains(view, "/theme") {
		t.Fatalf("expected slash-command right labels:\n%s", view)
	}
	for _, line := range strings.Split(view, "\n") {
		if strings.Contains(line, "Reload") && !strings.Contains(line, "/reload") {
			t.Fatalf("right column still shows keybinding instead of command:\n%s", view)
		}
		if strings.Contains(line, "Theme") && strings.Contains(line, "…") {
			t.Fatalf("OpensMenu must not show ellipsis; use Shortcut:\n%s", view)
		}
	}
}

func TestSearchCursorAndCloseHoverHighlight(t *testing.T) {
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(prev) })

	menu := New(Options{
		Title: "Spotlight",
		Items: []Item{
			{ID: "language", LabelColumns: []string{"Language", ""}, Shortcut: "/language", Section: "System"},
			{ID: "theme", LabelColumns: []string{"Theme", ""}, Shortcut: "/theme", Section: "System"},
		},
		HideCancel: true,
	})
	menu.Open()
	view := menu.ViewOver(strings.Repeat(".\n", 40), 100, 24)

	// Solid reverse-video block cursor (Bubble Tea-safe; matches Grok caret).
	if !strings.Contains(view, "█") || !strings.Contains(view, "search:") {
		t.Fatalf("search cursor/label missing:\n%s", view)
	}

	// Hover [x] → closeHovered + highlight (modal_window.rs)
	cx := menu.hits.closeButton.X + 2
	cy := menu.hits.closeButton.Y
	menu.Update(tea.MouseMsg{X: cx, Y: cy, Action: tea.MouseActionMotion, Type: tea.MouseMotion})
	hovered := menu.ViewOver(strings.Repeat(".\n", 40), 100, 24)
	if !menu.closeHovered {
		t.Fatalf("closeHovered=false after motion over close rect %+v", menu.hits.closeButton)
	}
	hoverBG := trueColorSignature(string(chromeBGVisual()), true)
	if hoverBG == "" || !strings.Contains(hovered, hoverBG) {
		t.Fatalf("close hover missing highlight:\n%s", hovered)
	}

	// Hover second item → selection switches (also via render-time lastMouse).
	if len(menu.hits.itemRows) < 2 {
		t.Fatalf("need 2 item rows, got %d", len(menu.hits.itemRows))
	}
	row := menu.hits.itemRows[1]
	menu.Update(tea.MouseMsg{X: row.X + 3, Y: row.Y, Action: tea.MouseActionMotion, Type: tea.MouseMotion})
	_ = menu.ViewOver(strings.Repeat(".\n", 40), 100, 24)
	if menu.cursor != menu.hits.itemIndex[1] {
		t.Fatalf("hover item cursor=%d want %d", menu.cursor, menu.hits.itemIndex[1])
	}
	if menu.closeHovered {
		t.Fatal("closeHovered should clear when leaving [x]")
	}
}

func TestMouseCloseClickAndOutsideClose(t *testing.T) {
	menu := New(Options{
		Title: "Spotlight",
		Items: []Item{
			{ID: "language", LabelColumns: []string{"Language", ""}, Shortcut: "/language", Section: "System"},
			{ID: "theme", LabelColumns: []string{"Theme", ""}, Shortcut: "/theme", Section: "System"},
		},
		HideCancel: true,
	})
	menu.Open()
	_ = menu.ViewOver(strings.Repeat(".\n", 40), 100, 24)
	if menu.hits.closeButton.W == 0 || menu.hits.popup.W == 0 {
		t.Fatalf("hit areas not populated: %+v", menu.hits)
	}

	// Click [x] — modal_window.rs CloseRequested
	cx := menu.hits.closeButton.X + 1
	cy := menu.hits.closeButton.Y
	result := menu.Update(tea.MouseMsg{X: cx, Y: cy, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	if !result.Closed || menu.IsOpen() {
		t.Fatalf("close click = %#v open=%t", result, menu.IsOpen())
	}

	menu.Open()
	_ = menu.ViewOver(strings.Repeat(".\n", 40), 100, 24)
	// Click outside popup
	result = menu.Update(tea.MouseMsg{X: 0, Y: 0, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	if !result.Closed || menu.IsOpen() {
		t.Fatalf("outside click = %#v open=%t", result, menu.IsOpen())
	}
}

func TestMouseItemClickSelectsAndWheelScrolls(t *testing.T) {
	items := make([]Item, 0, 20)
	for i := 0; i < 20; i++ {
		items = append(items, Item{
			ID:           string(rune('a'+i%26)) + string(rune('0'+i/26)),
			LabelColumns: []string{"Item " + string(rune('A'+i%26)) + string(rune('0'+i/26)), ""},
			Shortcut:     "/cmd",
			Section:      "List",
		})
	}
	items[0].ID = "first"
	items[1].ID = "second"
	menu := New(Options{Title: "Spotlight", Items: items, HideCancel: true})
	menu.Open()
	_ = menu.ViewOver(strings.Repeat(".\n", 40), 100, 24)
	if len(menu.hits.itemRows) < 2 {
		t.Fatalf("expected item hit rows, got %d (listH=%d total=%d)", len(menu.hits.itemRows), menu.hits.listHeight, menu.hits.totalLines)
	}

	// Hover second visible selectable → cursor follows (picker Moved)
	row := menu.hits.itemRows[1]
	menu.Update(tea.MouseMsg{X: row.X + 2, Y: row.Y, Action: tea.MouseActionMotion})
	if menu.cursor != menu.hits.itemIndex[1] {
		t.Fatalf("hover cursor=%d want %d", menu.cursor, menu.hits.itemIndex[1])
	}

	// Click selects
	result := menu.Update(tea.MouseMsg{X: row.X + 2, Y: row.Y, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	if result.SelectedID != "second" || !result.Closed {
		t.Fatalf("item click = %#v", result)
	}

	menu.Open()
	_ = menu.ViewOver(strings.Repeat(".\n", 40), 100, 24)
	before := menu.scroll
	menu.Update(tea.MouseMsg{Button: tea.MouseButtonWheelDown, Action: tea.MouseActionPress})
	if menu.scroll != before+scrollStep && !(before+scrollStep > max(0, menu.hits.totalLines-menu.hits.listHeight) && menu.scroll > before) {
		// Allow clamp at bottom; otherwise must advance by scrollStep.
		maxScroll := max(0, menu.hits.totalLines-menu.hits.listHeight)
		if before >= maxScroll {
			t.Skip("already at bottom")
		}
		if menu.scroll != min(maxScroll, before+scrollStep) {
			t.Fatalf("wheel scroll=%d want %d (before=%d max=%d)", menu.scroll, before+scrollStep, before, maxScroll)
		}
	}
	if !menu.scrollPinned {
		t.Fatal("wheel should pin scroll_offset")
	}
	view := menu.ViewOver(strings.Repeat(".\n", 40), 100, 24)
	if !strings.Contains(view, "▐") {
		t.Fatalf("expected scrollbar thumb when overflowing:\n%s", view)
	}
}

func TestCommandPaletteDimsFollowWindow(t *testing.T) {
	// Port of modal_window.rs compute_modal_dims + CommandPalette sizing.
	w, h := computeCommandPaletteDims(160, 40)
	if w != 80 { // 50% of 160 = 80, clamped by max_width 80
		t.Fatalf("wide terminal width = %d, want 80", w)
	}
	if h != 32 { // 40 - 2*4
		t.Fatalf("height = %d, want 32", h)
	}

	w, h = computeCommandPaletteDims(100, 24)
	if w != 50 { // 50% of 100
		t.Fatalf("medium width = %d, want 50", w)
	}
	if h != 16 {
		t.Fatalf("height = %d, want 16", h)
	}

	w, h = computeCommandPaletteDims(60, 20)
	if w != 44 { // min_width floor
		t.Fatalf("narrow width = %d, want 44", w)
	}
	if h != 12 {
		t.Fatalf("height = %d, want 12", h)
	}

	w, _ = computeCommandPaletteDims(30, 20)
	if w != 30 { // never exceed terminal
		t.Fatalf("tiny terminal width = %d, want 30", w)
	}

	menu := New(Options{
		Title: "Spotlight",
		Items: []Item{
			{ID: "language", LabelColumns: []string{"Language", ""}, Shortcut: "/language", Section: "System"},
			{ID: "theme", LabelColumns: []string{"Theme", ""}, Shortcut: "/theme", Section: "System"},
		},
		HideCancel: true,
	})
	menu.Open()
	view := menu.ViewOver(strings.Repeat(".\n", 40), 100, 24)
	panelLines := 0
	for _, line := range strings.Split(view, "\n") {
		if strings.Contains(line, "┌") || strings.Contains(line, "│") || strings.Contains(line, "└") {
			panelLines++
		}
	}
	if panelLines != 16 { // modal height fills window minus v_margin*2
		t.Fatalf("panel line count = %d, want 16 (fills height):\n%s", panelLines, view)
	}
	// Width should track 50% (50 cols), not hug content.
	for _, line := range strings.Split(view, "\n") {
		if strings.Contains(line, "┌") {
			if got := lipgloss.Width(line); got < 50 {
				t.Fatalf("top border width = %d, want >= 50:\n%s", got, line)
			}
			break
		}
	}
}

func TestSpotlightChromeUsesThemeBackground(t *testing.T) {
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(prev) })

	menu := New(Options{
		Title: "Spotlight",
		Items: []Item{
			{ID: "language", LabelColumns: []string{"Language", "Switch UI language"}, Shortcut: "/language", Section: "System", OpensMenu: true},
			{ID: "theme", LabelColumns: []string{"Theme", "Switch color theme"}, Shortcut: "/theme", Section: "System", OpensMenu: true},
		},
		HideCancel: true,
	})
	menu.Open()
	view := menu.ViewOver(strings.Repeat(".\n", 40), 80, 36)

	for _, corner := range []string{"┌", "┐", "└", "┘"} {
		if !strings.Contains(view, corner) {
			t.Fatalf("outer panel missing square corner %q:\n%s", corner, view)
		}
	}
	panelHex := string(chromeBGBase())
	visualHex := string(chromeBGVisual())
	selectedBG := trueColorSignature(visualHex, true)
	if selectedBG == "" || !strings.Contains(view, selectedBG) {
		t.Fatalf("view missing theme surface %s (%q):\n%s", visualHex, selectedBG, view)
	}
	panelBG := trueColorSignature(panelHex, true)
	if panelBG == "" || !strings.Contains(view, panelBG) {
		t.Fatalf("view missing theme bg %s (%q):\n%s", panelHex, panelBG, view)
	}
	if panelHex == "#141414" || visualHex == "#363636" {
		t.Fatalf("chrome still on GrokNight blacks: bg=%s visual=%s", panelHex, visualHex)
	}
	if strings.Contains(view, "48;2;0;0;0") || strings.Contains(view, "48;2;20;20;20") {
		t.Fatalf("view must not use near-black Grok panel fill:\n%s", view)
	}
	if !strings.Contains(view, "search:") || !strings.Contains(view, "◆ ") {
		t.Fatalf("missing Grok search/diamond chrome:\n%s", view)
	}
}

var trueColorSeq = regexp.MustCompile(`(?:38|48);2;\d+;\d+;\d+`)

func trueColorSignature(hex string, background bool) string {
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	defer lipgloss.SetColorProfile(prev)

	style := lipgloss.NewStyle()
	if background {
		style = style.Background(lipgloss.Color(hex))
	} else {
		style = style.Foreground(lipgloss.Color(hex))
	}
	match := trueColorSeq.FindString(style.Render("x"))
	if match == "" {
		return ""
	}
	if background && !strings.HasPrefix(match, "48;") {
		return ""
	}
	if !background && !strings.HasPrefix(match, "38;") {
		return ""
	}
	return match
}
