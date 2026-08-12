package observations

import (
	"errors"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/asheshgoplani/agent-deck/internal/backend"
	"github.com/asheshgoplani/agent-deck/internal/resourcepage"
	"github.com/asheshgoplani/agent-deck/internal/upstream/k9s/resourceview"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func TestReadOnlyColumnsAndNoMutationIntents(t *testing.T) {
	page := newTestPage(t)
	view := ansi.Strip(page.View())
	for _, col := range []string{"TIME", "KIND", "OUTCOME", "SESSION", "ROUTE", "TARGET", "DURATION"} {
		if !strings.Contains(view, col) {
			t.Fatalf("missing column %s:\n%s", col, view)
		}
	}
	if !strings.Contains(view, "request") || !strings.Contains(view, "ses-aaa") {
		t.Fatalf("missing observation row:\n%s", view)
	}
	durationIdx := durationColumnIndex(t)
	for _, row := range rowsFromObservations(sampleObservations()) {
		if row.ID == "obs-1" {
			if row.Cells[durationIdx] != "-" {
				t.Fatalf("obs-1 DURATION cell=%q want '-'", row.Cells[durationIdx])
			}
		}
	}

	host := page.Host()
	if host == nil {
		t.Fatal("Host() nil")
	}

	mutationKeys := []tea.KeyMsg{
		{Type: tea.KeyRunes, Runes: []rune{'n'}},
		{Type: tea.KeyRunes, Runes: []rune{'e'}},
		{Type: tea.KeyRunes, Runes: []rune{'d'}},
		{Type: tea.KeyCtrlS},
	}
	for _, key := range mutationKeys {
		page.Update(key)
		if page.LastIntent() == resourcepage.IntentCreate ||
			page.LastIntent() == resourcepage.IntentEdit ||
			page.LastIntent() == resourcepage.IntentDelete ||
			page.LastIntent() == resourcepage.IntentPublish {
			t.Fatalf("mutation key %v produced intent %q", key, page.LastIntent())
		}
	}

	_ = page.View()
	newHit := page.Host().Table().FooterActionHit(resourceview.ActionCreate)
	if newHit.Kind != 0 {
		page.Update(tea.MouseMsg{X: newHit.X, Y: newHit.Y + page.OverlayLines(), Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
		if page.LastIntent() == resourcepage.IntentCreate {
			t.Fatal("footer create must not fire on read-only page")
		}
	}
}

func TestEmptyDisconnectedStates(t *testing.T) {
	page := NewPage()
	page.SetSize(120, 30)
	page.SetObservations(nil)
	if page.State() != resourcepage.StateEmpty {
		t.Fatalf("empty state=%q", page.State())
	}
	if !strings.Contains(ansi.Strip(page.View()), "No resources") {
		t.Fatalf("missing empty banner:\n%s", page.View())
	}

	page.SetObservations(sampleObservations())
	page.SetDisconnected(true)
	if page.State() != resourcepage.StateDisconnected {
		t.Fatalf("disconnected state=%q", page.State())
	}
	if !strings.Contains(ansi.Strip(page.View()), "Disconnected") {
		t.Fatalf("missing disconnected banner:\n%s", page.View())
	}
}

func TestFilterDetailsCopySelection(t *testing.T) {
	page := newTestPage(t)
	page.SelectID("obs-1")

	page.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	for _, r := range []rune("request") {
		page.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	page.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if len(page.VisibleIDs()) != 1 || page.VisibleIDs()[0] != "obs-1" {
		t.Fatalf("filter visible=%v", page.VisibleIDs())
	}

	page.Update(tea.KeyMsg{Type: tea.KeyEsc})
	page.SelectID("obs-1")
	page.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if !page.ShowingDetails() {
		t.Fatal("details should open")
	}
	view := page.View()
	if !strings.Contains(strings.ToLower(view), "obs-1") {
		t.Fatalf("details missing id:\n%s", view)
	}

	var copied string
	page.copyFn = func(text string) error {
		copied = text
		return nil
	}
	text, err := page.CopySelected()
	if err != nil {
		t.Fatalf("copy: %v", err)
	}
	if text == "" || copied == "" {
		t.Fatal("copy payload empty")
	}
	for _, field := range []string{"id: obs-1", "session_id: ses-aaa", "decision_reason:", "snapshot_generation: 42"} {
		if !strings.Contains(text, field) {
			t.Fatalf("copy missing %q in:\n%s", field, text)
		}
	}
	if copied != text {
		t.Fatal("copyFn payload must match returned text")
	}

	page.Update(tea.KeyMsg{Type: tea.KeyEsc})
	page.SelectID("obs-1")
	copied = ""
	page.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	if copied == "" {
		t.Fatal("keyboard y must invoke copyFn")
	}
	if !strings.Contains(copied, "snapshot_generation: 42") {
		t.Fatalf("keyboard copy missing snapshot_generation:\n%s", copied)
	}
	if !strings.Contains(ansi.Strip(page.View()), "copied observation details") {
		t.Fatal("keyboard copy should set status banner")
	}
}

func TestCopyBoundsPayload(t *testing.T) {
	page := NewPage()
	page.SetSize(80, 20)
	big := strings.Repeat("x", MaxCopyBytes+1024)
	page.SetObservations([]backend.Observation{{
		ID:             "obs-big",
		Type:           "request",
		DecisionReason: big,
	}})
	page.SelectID("obs-big")
	text, err := page.CopySelected()
	if err != nil {
		t.Fatalf("copy: %v", err)
	}
	if len(text) > MaxCopyBytes {
		t.Fatalf("copy len=%d want <= %d", len(text), MaxCopyBytes)
	}
	if !strings.Contains(text, "truncated") {
		t.Fatalf("expected truncation marker in %q", text[:min(80, len(text))])
	}
}

func TestBoundCopyTextUTF8Safe(t *testing.T) {
	// Each 日 is 3 bytes; truncate near boundary must stay valid UTF-8.
	payload := strings.Repeat("日", MaxCopyBytes/3+100)
	text := boundCopyText("id: obs\n" + payload)
	if len(text) > MaxCopyBytes {
		t.Fatalf("len=%d want <= %d", len(text), MaxCopyBytes)
	}
	if !utf8.ValidString(text) {
		t.Fatal("truncated copy must be valid UTF-8")
	}
}

func TestSortChangesVisibleOrder(t *testing.T) {
	page := newTestPage(t)
	before := append([]string(nil), page.VisibleIDs()...)
	page.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	after := page.VisibleIDs()
	changed := false
	for i := range before {
		if before[i] != after[i] {
			changed = true
			break
		}
	}
	if !changed {
		page.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
		after = page.VisibleIDs()
		for i := range before {
			if before[i] != after[i] {
				changed = true
				break
			}
		}
	}
	if !changed {
		t.Fatalf("sort did not change order: before=%v after=%v", before, after)
	}
}

func TestSetStateLoadingAndError(t *testing.T) {
	page := newTestPage(t)
	page.SetState(resourcepage.StateLoading)
	if page.State() != resourcepage.StateLoading {
		t.Fatalf("loading state=%q", page.State())
	}
	if !strings.Contains(ansi.Strip(page.View()), "Loading") {
		t.Fatalf("missing loading banner:\n%s", page.View())
	}
	page.SetState(resourcepage.StatePublicationError)
	if page.State() != resourcepage.StatePublicationError {
		t.Fatalf("error state=%q", page.State())
	}
	if !strings.Contains(ansi.Strip(page.View()), "Publication error") {
		t.Fatalf("missing publication error banner:\n%s", page.View())
	}
	page.ClearForcedState()
	page.refresh()
	if page.State() != resourcepage.StateSuccess {
		t.Fatalf("after clear forced state=%q", page.State())
	}
}

func TestMouseDoubleClickDetails(t *testing.T) {
	page := newTestPage(t)
	page.SetSize(120, 30)
	page.SelectID("obs-1")
	rowY := 1 + page.OverlayLines() + 1
	click := tea.MouseMsg{X: 2, Y: rowY, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress}
	page.Update(click)
	page.Update(click)
	if page.LastIntent() != resourcepage.IntentDetails || !page.ShowingDetails() {
		t.Fatalf("double-click details failed; intent=%q showing=%v", page.LastIntent(), page.ShowingDetails())
	}
}

func TestCopySelectedErrorSetsStatus(t *testing.T) {
	page := newTestPage(t)
	page.SelectID("obs-1")
	page.copyFn = func(text string) error {
		return errors.New("clipboard unavailable")
	}
	_, err := page.CopySelected()
	if err == nil {
		t.Fatal("expected CopySelected error")
	}
	page.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	if !strings.Contains(ansi.Strip(page.View()), "copy failed: clipboard unavailable") {
		t.Fatalf("keyboard copy error should set status banner:\n%s", page.View())
	}
}

func durationColumnIndex(t *testing.T) int {
	t.Helper()
	for i, col := range observationColumns() {
		if col.Title == "DURATION" {
			return i
		}
	}
	t.Fatal("DURATION column not found")
	return -1
}

func TestKeyboardMouseResize(t *testing.T) {
	page := newTestPage(t)
	page.SetSize(120, 30)
	page.Update(tea.KeyMsg{Type: tea.KeyDown})
	selected := page.SelectedID()

	page.SetSize(70, 30)
	_ = page.View()
	if page.SelectedID() != selected {
		t.Fatalf("selection lost: got %q want %q", page.SelectedID(), selected)
	}

	page.Update(tea.KeyMsg{Type: tea.KeyEsc})
	_ = page.View()
	filterHit := page.Host().Table().FooterFilterHit()
	page.Update(tea.MouseMsg{X: filterHit.X, Y: filterHit.Y + page.OverlayLines(), Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	if page.LastIntent() != resourcepage.IntentFilter {
		t.Fatalf("filter intent=%q", page.LastIntent())
	}
}

func TestMouseUnderDisconnectedBanner(t *testing.T) {
	page := newTestPage(t)
	page.SetSize(120, 30)
	page.SetDisconnected(true)
	_ = page.View()
	if page.OverlayLines() == 0 {
		t.Fatal("expected disconnected banner overlay")
	}

	filterHit := page.Host().Table().FooterFilterHit()
	page.Update(tea.MouseMsg{
		X:      filterHit.X,
		Y:      filterHit.Y + page.OverlayLines(),
		Button: tea.MouseButtonLeft,
		Action: tea.MouseActionPress,
	})
	if page.LastIntent() != resourcepage.IntentFilter {
		t.Fatalf("footer filter under banner => intent=%q", page.LastIntent())
	}

	page.Update(tea.KeyMsg{Type: tea.KeyEsc})
	page.SelectID("obs-1")
	rowY := 1 + page.OverlayLines() + 1
	click := tea.MouseMsg{X: 2, Y: rowY, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress}
	page.Update(click)
	page.Update(click)
	if page.LastIntent() != resourcepage.IntentDetails || !page.ShowingDetails() {
		t.Fatalf("double-click under banner => intent=%q details=%v", page.LastIntent(), page.ShowingDetails())
	}
}

func TestFilterModeYDoesNotCopy(t *testing.T) {
	page := newTestPage(t)
	page.SelectID("obs-1")
	copied := false
	page.copyFn = func(string) error {
		copied = true
		return nil
	}
	page.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	page.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	if copied {
		t.Fatal("y while filtering must not copy")
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
