package overview_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/asheshgoplani/agent-deck/internal/configdraft"
	"github.com/asheshgoplani/agent-deck/internal/coreclient/generated"
	"github.com/asheshgoplani/agent-deck/internal/resourcepage"
	"github.com/asheshgoplani/agent-deck/internal/resourcepages/overview"
	"github.com/asheshgoplani/agent-deck/internal/upstream/k9s/resourceview"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func newDraft() *configdraft.Draft {
	return configdraft.Load(configdraft.FixtureSnapshot())
}

func TestNewPageFactoryExposesEditOnlyInstance(t *testing.T) {
	page := overview.NewPage(newDraft())
	page.SetSize(120, 30)
	page.Refresh()

	view := ansi.Strip(page.View())
	if !strings.Contains(view, "Instance") {
		t.Fatalf("missing Instance title:\n%s", view)
	}
	if !strings.Contains(view, "FIELD") || !strings.Contains(view, "VALUE") || !strings.Contains(view, "MUTABILITY") {
		t.Fatalf("missing singleton columns:\n%s", view)
	}
	if !strings.Contains(view, "log_level") {
		t.Fatalf("missing log_level row:\n%s", view)
	}
	if !strings.Contains(view, "mutable") {
		t.Fatalf("missing mutability cell:\n%s", view)
	}

	// Create/Delete must not fire.
	for _, key := range []tea.KeyMsg{
		{Type: tea.KeyRunes, Runes: []rune{'n'}},
		{Type: tea.KeyRunes, Runes: []rune{'d'}},
	} {
		intent, consumed := page.Update(key)
		if intent == resourcepage.IntentCreate || intent == resourcepage.IntentDelete {
			t.Fatalf("edit-only page returned mutation intent %q for %q (consumed=%v)", intent, key, consumed)
		}
	}
}

func TestOverviewEditMutatesSharedDraft(t *testing.T) {
	draft := newDraft()
	page := overview.NewPage(draft)
	page.SetSize(120, 30)
	page.Refresh()

	if draft.LocalCommand().Instance.LogLevel != generated.MutableInstanceConfigLogLevelSimple {
		t.Fatalf("fixture log_level = %q", draft.LocalCommand().Instance.LogLevel)
	}

	intent, _ := page.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	if intent != resourcepage.IntentEdit {
		t.Fatalf("expected IntentEdit, got %q", intent)
	}
	if !page.Editing() {
		t.Fatal("editor should be open after edit")
	}

	// Cycle enum to detail and save.
	page.Update(tea.KeyMsg{Type: tea.KeyRight})
	page.Update(tea.KeyMsg{Type: tea.KeyEnter})

	if page.Editing() {
		t.Fatal("editor should close after save")
	}
	if draft.LocalCommand().Instance.LogLevel != generated.MutableInstanceConfigLogLevelDetail {
		t.Fatalf("draft log_level = %q, want detail", draft.LocalCommand().Instance.LogLevel)
	}
	if !draft.DomainDirty(configdraft.DomainInstance) {
		t.Fatal("instance domain should be dirty after edit")
	}
	view := ansi.Strip(page.View())
	if !strings.Contains(view, "detail") {
		t.Fatalf("view not refreshed with new value:\n%s", view)
	}
}

func TestOverviewRejectsCreateDeleteAndHidesActions(t *testing.T) {
	page := overview.NewPage(newDraft())
	page.SetSize(120, 30)
	page.Refresh()
	_ = page.View()

	view := ansi.Strip(page.View())
	if strings.Contains(view, "n new") {
		t.Fatalf("create action must be absent from footer:\n%s", view)
	}
	if strings.Contains(view, "d del") {
		t.Fatalf("delete action must be absent from footer:\n%s", view)
	}

	for _, action := range []resourceview.Action{resourceview.ActionCreate, resourceview.ActionDelete} {
		hit := page.Inner().Table().FooterActionHit(action)
		if hit.Kind != resourceview.HitNone {
			t.Fatalf("unsupported %s retained a mouse hit region: %#v", action, hit)
		}
	}
}

func TestOverviewDetailsShowsDescriptorFields(t *testing.T) {
	page := overview.NewPage(newDraft())
	page.SetSize(120, 30)
	page.Refresh()

	page.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if !page.ShowingDetails() {
		t.Fatal("expected details view")
	}
	view := ansi.Strip(page.View())
	if !strings.Contains(view, "log_level") || !strings.Contains(view, "simple") {
		t.Fatalf("details missing field values:\n%s", view)
	}
	desc, ok := resourcepage.Lookup("instance")
	if !ok || len(desc.Fields) != 1 || desc.Fields[0].Name != "log_level" {
		t.Fatalf("descriptor drift: %#v", desc)
	}
}

func TestOverviewKeyboardEditSavesOnlyValidEnum(t *testing.T) {
	draft := newDraft()
	page := overview.NewPage(draft)
	page.SetSize(120, 30)
	page.Refresh()
	page.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	if !strings.Contains(ansi.Strip(page.View()), "value: <simple>") {
		t.Fatalf("editor missing current value:\n%s", page.View())
	}
	page.Update(tea.KeyMsg{Type: tea.KeyRight})
	page.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if draft.LocalCommand().Instance.LogLevel != generated.MutableInstanceConfigLogLevelDetail {
		t.Fatalf("keyboard save failed: %q", draft.LocalCommand().Instance.LogLevel)
	}

	// Guard API still rejects invalid values without mutating.
	page.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	if err := page.ApplyEditorValue("bogus"); err == nil {
		t.Fatal("expected validation error for bogus log_level")
	}
	if draft.LocalCommand().Instance.LogLevel != generated.MutableInstanceConfigLogLevelDetail {
		t.Fatal("invalid edit must not mutate draft")
	}
}

func TestOverviewConflictAndDisconnectedStates(t *testing.T) {
	draft := newDraft()
	draft.Mutate(func(cmd *generated.MutableConfigCommand) {
		cmd.Instance.LogLevel = generated.MutableInstanceConfigLogLevelDetail
	})
	current := configdraft.FixtureSnapshot()
	current.MutableConfig.Instance.LogLevel = generated.MutableInstanceConfigLogLevelSimple
	current.ActiveGeneration = 2
	current.ConfigRevision = "rev-2"
	draft.BeginConflict(current)

	page := overview.NewPage(draft)
	page.SetSize(120, 30)
	page.Refresh()
	view := ansi.Strip(page.View())
	if !strings.Contains(strings.ToLower(view), "conflict") {
		t.Fatalf("missing conflict presentation:\n%s", view)
	}

	draft2 := newDraft()
	draft2.SetDisconnected(true)
	page2 := overview.NewPage(draft2)
	page2.SetSize(120, 30)
	page2.Refresh()
	view2 := ansi.Strip(page2.View())
	if !strings.Contains(view2, "Disconnected") {
		t.Fatalf("missing disconnected banner:\n%s", view2)
	}
	intent, consumed := page2.Update(tea.KeyMsg{Type: tea.KeyCtrlS})
	if intent == resourcepage.IntentPublish {
		t.Fatalf("publish must be unavailable while disconnected (intent=%q consumed=%v)", intent, consumed)
	}

	draft3 := newDraft()
	draft3.Mutate(func(cmd *generated.MutableConfigCommand) {
		cmd.Instance.LogLevel = generated.MutableInstanceConfigLogLevelDetail
	})
	page3 := overview.NewPage(draft3)
	page3.SetSize(120, 30)
	page3.Refresh()
	cur := configdraft.FixtureSnapshot()
	cur.MutableConfig = draft3.BaseView()
	cur.ActiveGeneration = 7
	cur.ConfigRevision = "rev-7"
	draft3.BeginConflict(cur)
	page3.Refresh()
	if draft3.CanPublish() {
		t.Fatal("precondition: conflict disables CanPublish")
	}
	intent, _ = page3.Update(tea.KeyMsg{Type: tea.KeyCtrlS})
	if intent == resourcepage.IntentPublish {
		t.Fatal("list ctrl+s must not publish while CanPublish is false")
	}
}

func TestOverviewEditorCtrlSSavesAndApplies(t *testing.T) {
	draft := newDraft()
	page := overview.NewPage(draft)
	page.SetSize(120, 30)
	page.Refresh()
	page.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	page.Update(tea.KeyMsg{Type: tea.KeyRight}) // stage unsaved detail
	intent, consumed := page.Update(tea.KeyMsg{Type: tea.KeyCtrlS})
	if !consumed || intent != resourcepage.IntentPublish {
		t.Fatalf("editor ctrl+s => intent=%q consumed=%v", intent, consumed)
	}
	if draft.LocalCommand().Instance.LogLevel != generated.MutableInstanceConfigLogLevelDetail {
		t.Fatal("ctrl+s must save the selected enum before applying")
	}
	if page.Editing() {
		t.Fatal("successful ctrl+s should close the editor")
	}

	current := configdraft.FixtureSnapshot()
	current.ActiveGeneration = 2
	current.ConfigRevision = "rev-2"
	draft.BeginConflict(current)
	page.Refresh()
	if page.OverlayLines() < 2 {
		t.Fatalf("conflict overlay=%d, want >=2", page.OverlayLines())
	}
	draft.AcceptCurrent()
	page.Refresh()
	if page.OverlayLines() != 0 {
		t.Fatalf("after conflict clear overlay=%d", page.OverlayLines())
	}
}

func TestOverviewEditorRebasesAfterAcceptCurrent(t *testing.T) {
	draft := newDraft()
	page := overview.NewPage(draft)
	page.SetSize(120, 30)
	page.Refresh()
	page.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	page.Update(tea.KeyMsg{Type: tea.KeyRight}) // stage detail over simple
	if !strings.Contains(ansi.Strip(page.View()), "detail") {
		t.Fatalf("precondition: staged detail missing:\n%s", page.View())
	}

	cur := configdraft.FixtureSnapshot()
	cur.MutableConfig = draft.BaseView()
	cur.MutableConfig.Instance.LogLevel = generated.MutableInstanceConfigLogLevelSimple
	cur.ActiveGeneration = 12
	cur.ConfigRevision = "rev-12"
	draft.BeginConflict(cur)
	draft.AcceptCurrent()
	page.Refresh()
	if !page.Editing() {
		t.Fatal("editor should remain open after rebase")
	}
	if !strings.Contains(ansi.Strip(page.View()), "simple") {
		t.Fatalf("editor enumIdx not rebased to accepted remote:\n%s", page.View())
	}
	page.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if draft.LocalCommand().Instance.LogLevel != generated.MutableInstanceConfigLogLevelSimple {
		t.Fatalf("Enter after rebase wrote stale log_level %q", draft.LocalCommand().Instance.LogLevel)
	}
}

func TestOverviewEditorPublishGatedOnConflict(t *testing.T) {
	draft := newDraft()
	draft.Mutate(func(cmd *generated.MutableConfigCommand) {
		cmd.Instance.LogLevel = generated.MutableInstanceConfigLogLevelDetail
	})
	page := overview.NewPage(draft)
	page.SetSize(120, 30)
	page.Refresh()
	page.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	if !page.Editing() {
		t.Fatal("precondition: must be in editor for this gate")
	}
	cur := configdraft.FixtureSnapshot()
	cur.MutableConfig = draft.BaseView()
	cur.ActiveGeneration = 13
	cur.ConfigRevision = "rev-13"
	draft.BeginConflict(cur)
	page.Refresh()
	if !page.Editing() {
		t.Fatal("editor must stay open through conflict refresh")
	}
	if draft.CanPublish() {
		t.Fatal("precondition: conflict disables CanPublish")
	}
	intent, _ := page.Update(tea.KeyMsg{Type: tea.KeyCtrlS})
	if intent == resourcepage.IntentPublish {
		t.Fatal("editor ctrl+s must not publish while CanPublish is false")
	}
	if !page.Editing() {
		t.Fatal("gated ctrl+s must leave editor open")
	}
}

func TestOverviewOpenEditorKeepsConflictBanner(t *testing.T) {
	draft := newDraft()
	page := overview.NewPage(draft)
	page.SetSize(120, 30)
	page.Refresh()
	cur := configdraft.FixtureSnapshot()
	cur.MutableConfig = draft.BaseView()
	cur.ActiveGeneration = 15
	cur.ConfigRevision = "rev-15"
	draft.BeginConflict(cur)
	page.Refresh()
	page.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	if !page.Editing() {
		t.Fatal("expected editor open during conflict")
	}
	view := strings.ToLower(ansi.Strip(page.View()))
	if !strings.Contains(view, "conflict") {
		t.Fatalf("opening editor must retain conflict banner:\n%s", page.View())
	}
}

func TestOverviewConflictRefreshKeepsValidation(t *testing.T) {
	draft := newDraft()
	page := overview.NewPage(draft)
	page.SetSize(120, 30)
	page.Refresh()
	page.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	if err := page.ApplyEditorValue("not-a-level"); err == nil {
		t.Fatal("precondition: invalid enum should fail")
	}
	if page.State() != resourcepage.StateValidationError {
		t.Fatalf("precondition: validation state=%q", page.State())
	}
	cur := configdraft.FixtureSnapshot()
	cur.MutableConfig = draft.BaseView()
	cur.ActiveGeneration = 19
	cur.ConfigRevision = "rev-19"
	draft.BeginConflict(cur)
	page.Refresh()
	view := strings.ToLower(ansi.Strip(page.View()))
	if !strings.Contains(view, "conflict") {
		t.Fatalf("conflict missing after refresh:\n%s", page.View())
	}
	if !strings.Contains(view, "invalid") {
		t.Fatalf("validation must survive conflict refresh:\n%s", page.View())
	}
}

func TestOverviewKeyboardAndMouseEditPaths(t *testing.T) {
	draft := newDraft()
	page := overview.NewPage(draft)
	page.SetSize(120, 30)
	page.Refresh()
	_ = page.View()

	// Keyboard edit
	page.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	if !page.Editing() {
		t.Fatal("keyboard e should open editor")
	}
	page.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if page.Editing() {
		t.Fatal("esc should cancel editor")
	}

	// Mouse footer edit
	hit := page.Inner().Table().FooterActionHit(resourceview.ActionEdit)
	if hit.Kind != resourceview.HitFooterAction {
		t.Fatalf("edit footer hit missing: %#v", hit)
	}
	page.Update(tea.MouseMsg{X: hit.X, Y: hit.Y + page.OverlayLines(), Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	if !page.Editing() {
		t.Fatal("mouse edit should open editor")
	}
}

func TestOverviewEditorViewAndCancelPaths(t *testing.T) {
	page := overview.NewPage(newDraft())
	page.SetSize(80, 20)
	page.Refresh()
	page.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	view := ansi.Strip(page.View())
	if !strings.Contains(view, "log_level") || !strings.Contains(view, "simple") {
		t.Fatalf("editor view missing content:\n%s", view)
	}
	before := newDraft().LocalCommand().Instance.LogLevel
	page.Update(tea.KeyMsg{Type: tea.KeyLeft})
	page.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if page.Editing() {
		t.Fatal("esc should close editor")
	}
	if page.Inner() == nil {
		t.Fatal("inner missing")
	}
	// Cancel after cycling must not persist until Enter.
	draft := newDraft()
	page2 := overview.NewPage(draft)
	page2.SetSize(80, 20)
	page2.Refresh()
	page2.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	page2.Update(tea.KeyMsg{Type: tea.KeyRight})
	page2.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if draft.LocalCommand().Instance.LogLevel != before {
		t.Fatalf("cancel mutated draft to %q", draft.LocalCommand().Instance.LogLevel)
	}
}

func TestOverviewResponsiveFourSizesGolden(t *testing.T) {
	page := overview.NewPage(newDraft())
	sizes := []struct{ w, h int }{{160, 45}, {120, 30}, {90, 30}, {70, 30}}
	for _, size := range sizes {
		page.SetSize(size.w, size.h)
		page.Refresh()
		got := ansi.Strip(page.View())
		if !strings.Contains(got, "FIELD") || !strings.Contains(got, "log_level") {
			t.Fatalf("%dx%d lost identity columns:\n%s", size.w, size.h, got)
		}
		path := filepath.Join("testdata", "golden", "overview_"+itoa(size.w)+"x"+itoa(size.h)+".ansi")
		assertOrUpdateGolden(t, path, got)
	}
}

func assertOrUpdateGolden(t *testing.T, path, got string) {
	t.Helper()
	if os.Getenv("UPDATE_GOLDEN") == "1" {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("missing golden %s (run UPDATE_GOLDEN=1): %v\n--- got ---\n%s", path, err, got)
	}
	if strings.TrimSpace(string(want)) != strings.TrimSpace(got) {
		t.Fatalf("golden mismatch %s\n--- got ---\n%s\n--- want ---\n%s", path, got, want)
	}
}

func itoa(v int) string {
	if v == 0 {
		return "0"
	}
	var b [16]byte
	i := len(b)
	for v > 0 {
		i--
		b[i] = byte('0' + v%10)
		v /= 10
	}
	return string(b[i:])
}
