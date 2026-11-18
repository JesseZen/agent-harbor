package main

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

type recoveryFormService struct {
	*fakeApplicationService
	choices []string
}

func (f *recoveryFormService) PrepareRecovery(d sessionDraft) error {
	f.choices = append(f.choices, d.RecoveryChoice)
	if d.RecoveryChoice == "" {
		return errRecoveryChoice
	}
	return nil
}
func TestCreateRecoveryDecisionPrecedesRecordAndCancelKeepsDraft(t *testing.T) {
	m, fake := formFixture()
	m.draft.Name = "Keep this"
	f := &recoveryFormService{fakeApplicationService: fake}
	m.service = f
	n, cmd := m.Update(uiKey("enter"))
	n, _ = n.Update(cmd())
	got := n.(harborModel)
	if got.screen != "new-recovery" || len(fake.Sessions) != 0 {
		t.Fatal(got.screen, fake.Calls)
	}
	// Safe default: Enter cancels, preserving the original creation intent.
	n, cmd = got.Update(uiKey("enter"))
	got = n.(harborModel)
	if cmd != nil || got.screen != "new-form" || got.draft.Name != "Keep this" || len(fake.Sessions) != 0 {
		t.Fatal(got.screen, fake.Calls)
	}
	for _, choice := range []string{"y", "s"} {
		n, cmd = got.Update(uiKey("ctrl+s"))
		n, _ = n.Update(cmd())
		n, cmd = n.Update(uiKey(choice))
		if cmd == nil {
			t.Fatal("missing submit")
		}
		n, _ = n.Update(cmd())
		if len(fake.Sessions) == 0 {
			t.Fatal("choice did not create")
		}
		if f.choices[len(f.choices)-1] == "" {
			t.Fatal(f.choices)
		}
		got = m
	}
}
func TestSettingsHelpPreservesDraftAndError(t *testing.T) {
	m := settingsModel{width: 60, height: 24, values: defaultShortcuts(), saved: defaultShortcuts(), notice: "save failed"}
	m.values.Harbor = "prefix+t"
	n, _ := m.Update(uiKey("?"))
	got := n.(settingsModel)
	if !got.help || !strings.Contains(got.View(), "Navigation") {
		t.Fatal(got.View())
	}
	n, _ = got.Update(uiKey("esc"))
	got = n.(settingsModel)
	if got.help || got.closed || got.notice != "save failed" || got.values.Harbor != "prefix+t" {
		t.Fatal(got)
	}
}
func TestMagpieConfirmationUsesDirectKeysWithoutFooterSelection(t *testing.T) {
	m := newHarborModel(&fakeApplicationService{})
	m.screen = "magpie-confirm"
	m.uiFocus = "conn:esc"
	m.action = "stop"
	n, _ := m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = n.(harborModel)
	if m.screen != "magpie-confirm" || m.uiFocus != "conn:esc" {
		t.Fatal("Tab changed confirmation state")
	}
	m.reviewOpenedAt = m.reviewOpenedAt.Add(-1000000000)
	n, c := m.Update(uiKey("enter"))
	if n.(harborModel).screen != "magpie-busy" || c == nil {
		t.Fatal("Enter did not directly confirm")
	}
	m.screen = "magpie-confirm"
	n, c = m.Update(uiKey("esc"))
	if n.(harborModel).screen != "magpie" || c != nil {
		t.Fatal("Esc did not cancel")
	}
}

func TestSessionCompactRowsKeepWarningAndMouseTarget(t *testing.T) {
	m := newHarborModel(&fakeApplicationService{})
	m.width = 60
	m.height = 24
	for _, name := range []string{"one", "two", "three", "four", "five", "six", "seven", "eight"} {
		m.sessions = append(m.sessions, sessionView{session: session{ID: name, Name: name}, Runtime: runtimeObservation{Presence: "not_started"}})
	}
	m.sessions[2].DiscoveryWarning = "Discovery failed"
	m.cursor = 5
	p := m.sessionsLayout()
	if !strings.Contains(p.view(), "six") {
		t.Fatal(p.view())
	}
	for _, r := range p.Hits {
		if strings.HasPrefix(r.ID, "session:") && r.H != 2 && r.ID != "session:three" {
			t.Fatal(r)
		}
	}
	m.cursor = 2
	if !strings.Contains(m.sessionsLayout().view(), "Discovery failed") {
		t.Fatal("warning hidden")
	}
}

func TestTerminalColorCapabilityAndASCIIGeometry(t *testing.T) {
	old := lipgloss.ColorProfile()
	defer lipgloss.SetColorProfile(old)
	for _, profile := range []termenv.Profile{termenv.ANSI, termenv.ANSI256, termenv.Ascii} {
		lipgloss.SetColorProfile(profile)
		p := newUIPage(60, 5)
		p.line(highlightSearch("Harbor", "Har"))
		view := p.view()
		if strings.Contains(view, ";2;") {
			t.Fatal("forced truecolor", profile, view)
		}
		if profile == termenv.Ascii && strings.Contains(view, "\x1b[") {
			t.Fatal("monochrome emitted color")
		}
	}
	t.Setenv("AGENT_HARBOR_ASCII", "1")
	for _, text := range []string{"⚓ Harbor", "🤖 Codex", "│⌕ test╭─╮", "› selected", "🧠 Model", "🗂 Profiles", "⚙ Settings"} {
		if ansi.StringWidth(text) != ansi.StringWidth(chromeText(text)) {
			t.Fatal(text, chromeText(text))
		}
	}
	if keyLabel("esc") != "esc" {
		t.Fatal("ASCII keys unavailable")
	}
}

// Drain immediate Bubble Tea effects while leaving wall-clock timers to tests
// that explicitly inject their messages. This includes nested service batches.
func settleApp(m tea.Model, cmd tea.Cmd) tea.Model {
	if cmd == nil {
		return m
	}
	name := runtime.FuncForPC(reflect.ValueOf(cmd).Pointer()).Name()
	if strings.Contains(name, "bubbletea.Tick.") || strings.Contains(name, "bubbletea.Every.") {
		return m
	}
	msg := cmd()
	if batch, ok := msg.(tea.BatchMsg); ok {
		for _, c := range batch {
			m = settleApp(m, c)
		}
		return m
	}
	next, c := m.Update(msg)
	return settleApp(next, c)
}

func TestServiceMutationRunsInCommandAndRejectsDuplicateCompletion(t *testing.T) {
	m := newHarborModel(&fakeApplicationService{})
	ran, applied := false, 0
	n, cmd, _ := m.serviceTask(func() error { ran = true; return nil }, func(n *harborModel, err error) tea.Cmd { applied++; n.notice = "Profile saved"; return nil })
	if ran {
		t.Fatal("work ran on the event thread")
	}
	waiting := n.(harborModel)
	resized, _ := waiting.Update(tea.WindowSizeMsg{Width: 60, Height: 24})
	waiting = resized.(harborModel)
	if waiting.width != 60 || !waiting.servicePending {
		t.Fatal("resize blocked")
	}
	ignored, _ := waiting.Update(uiKey("n"))
	if ignored.(harborModel).screen != waiting.screen {
		t.Fatal("duplicate action accepted")
	}
	result := cmd()
	n, _ = waiting.Update(result)
	if !ran || applied != 1 || n.(harborModel).servicePending {
		t.Fatal("completion not applied")
	}
	n, _ = n.Update(result)
	if applied != 1 {
		t.Fatal("completion replay applied twice")
	}
}

func TestSettingsHelpClampsScroll(t *testing.T) {
	m := settingsModel{width: 40, height: 10, values: defaultShortcuts()}
	n, _ := m.Update(uiKey("?"))
	m = n.(settingsModel)
	if !m.help {
		t.Fatal("navigation swallowed help")
	}
	for range 100 {
		n, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
		m = n.(settingsModel)
	}
	end := m.scroll
	if end == 0 {
		t.Fatal("fixture did not overflow")
	}
	n, _ = m.Update(tea.KeyMsg{Type: tea.KeyUp})
	m = n.(settingsModel)
	if m.scroll != end-1 {
		t.Fatal("overscroll swallowed Up", end, m.scroll)
	}
}
func TestHelpRespectsSearchInputAndRecoveryReturn(t *testing.T) {
	m, _ := formFixture()
	m.screen = "new-recovery"
	m.uiFocus = "recovery:esc"
	n, _ := m.Update(uiKey("?"))
	m = n.(harborModel)
	if m.screen != "help" {
		t.Fatal("recovery help unavailable")
	}
	n, _ = m.Update(uiKey("esc"))
	m = n.(harborModel)
	if m.screen != "new-recovery" || m.uiFocus != "recovery:esc" {
		t.Fatal("lost recovery choice")
	}
	m.screen = "new-cli"
	m.uiFocus = "new-client-search"
	m.input.SetValue("")
	m.input.Focus()
	n, _ = m.Update(uiKey("?"))
	m = n.(harborModel)
	if m.screen != "new-cli" || m.input.Value() != "?" {
		t.Fatal("help took search text")
	}
}

func TestErrorDetailsRoundTripPreservesEvidence(t *testing.T) {
	m, _ := formFixture()
	m.notice = "integration installation failed"
	m.uiFocus = ""
	n, _ := m.Update(uiKey("!"))
	m = n.(harborModel)
	if m.screen != "error" {
		t.Fatal("details unavailable", m.screen)
	}
	n, _ = m.Update(uiKey("?"))
	n, _ = n.Update(uiKey("esc"))
	n, _ = n.Update(uiKey("esc"))
	m = n.(harborModel)
	if m.screen != "new-form" || m.notice != "integration installation failed" {
		t.Fatal("lost error evidence", m.screen, m.notice)
	}
	n, _ = m.Update(uiKey("esc"))
	m = n.(harborModel)
	if m.notice != "" {
		t.Fatal("cancelled task leaked error", m.notice)
	}
}

func TestASCIIFallbackCoversSharedPages(t *testing.T) {
	t.Setenv("AGENT_HARBOR_ASCII", "1")
	t.Setenv("NO_COLOR", "1")
	m, _ := formFixture()
	m.width = 60
	m.height = 24
	for _, screen := range []string{"new-form", "new-cli", "new-name", "new-connection", "new-recovery", "new-instance", "new-catalog"} {
		m.screen = screen
		for _, r := range m.View() {
			if r > 127 {
				t.Fatalf("%s has unsupported chrome %q", screen, r)
			}
		}
	}
}

func TestProfileDefaultTaskClearsCallingFocusAndRestoresSavedChoice(t *testing.T) {
	f := &fakeApplicationService{KnownInstances: []magpieInstance{{ID: "saved", Name: "Saved"}}}
	p, _ := f.CreateProfile(profileCreateRequest{Name: "A", Kind: profileIsolated})
	p.DefaultInstanceID = "saved"
	f.ProfileRecords[0] = p
	m := newHarborModel(f)
	m.profiles = []profile{p}
	m.screen = "profile-list"
	n, c := m.Update(uiKey("d"))
	m = settleApp(n, c).(harborModel)
	if m.screen != "profile-default" || m.uiFocus != "" || m.profileDefaultCursor != 1 {
		t.Fatal("lost visible target", m.screen, m.uiFocus, m.profileDefaultCursor)
	}
	m = appKey(m, "enter").(harborModel)
	if m.screen != "profile-list" || f.ProfileRecords[0].DefaultInstanceID != "saved" {
		t.Fatal("first Enter failed")
	}
	m.uiFocus = "profile:r"
	m = appKey(m, "down").(harborModel)
	if m.uiFocus != "" {
		t.Fatal("list move kept an action as Enter target")
	}
}

func TestProfileRowMouseSelectsTheClickedProfile(t *testing.T) {
	for _, size := range [][2]int{{104, 32}, {58, 21}, {40, 17}} {
		f := &fakeApplicationService{}
		a, _ := f.CreateProfile(profileCreateRequest{Name: "A", Kind: profileIsolated})
		b, _ := f.CreateProfile(profileCreateRequest{Name: "B", Kind: profileIsolated})
		m := newHarborModel(f)
		m.profiles = []profile{a, b}
		m.screen = "profile-list"
		m.width, m.height = size[0], size[1]
		m.uiFocus = "profile:r"
		p, _ := m.profilePage()
		found := false
		for _, hit := range p.Hits {
			if hit.ID == "profile-row:"+b.ID {
				if p.target(hit.X, hit.Y) != hit.ID {
					t.Fatal("row hidden by another hit")
				}
				n, c := m.Update(tea.MouseMsg{X: hit.X, Y: hit.Y, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
				m = settleApp(n, c).(harborModel)
				found = true
				break
			}
		}
		if !found || m.profileCursor != 1 || m.uiFocus != "" {
			t.Fatal("mouse did not select B", size, m.profileCursor, m.uiFocus)
		}
		m = appKey(m, "enter").(harborModel)
		if m.activeProfile == nil || m.activeProfile.ProfileID != b.ID || f.SelectedProfile.ProfileID != b.ID {
			t.Fatal("Enter selected another Profile")
		}
	}
}

func TestSuspendUsesFrameworkWithoutDiscardingDraft(t *testing.T) {
	m, _ := formFixture()
	m.draft.Name = "unsaved"
	m.servicePending = true
	m.refreshing = false
	n, c := m.Update(tea.KeyMsg{Type: tea.KeyCtrlZ})
	got := n.(harborModel)
	if c == nil || got.draft.Name != "unsaved" || !got.servicePending {
		t.Fatal("suspend lost pending task or draft")
	}
	if reflect.TypeOf(c()) != reflect.TypeOf(tea.Suspend()) {
		t.Fatal("not framework suspend")
	}
	n, c = got.Update(tea.ResumeMsg{})
	if c == nil || n.(harborModel).draft.Name != "unsaved" {
		t.Fatal("resume did not refresh runtime")
	}
	settings := settingsModel{editing: true}
	_, c = settings.Update(tea.KeyMsg{Type: tea.KeyCtrlZ})
	if c == nil {
		t.Fatal("editing intercepted suspend")
	}
	_, c = (onboardingModel{busy: true}).Update(tea.KeyMsg{Type: tea.KeyCtrlZ})
	if c == nil {
		t.Fatal("onboarding intercepted suspend")
	}
}

func TestRecoveryReviewShowsReviewedModelInsteadOfInitialChoice(t *testing.T) {
	m := newHarborModel(&fakeApplicationService{})
	m.width = 100
	m.height = 30
	m.screen = "confirm"
	m.action = "query-resume"
	m.reviewed = session{Mode: "magpie", CLI: "codex", InstanceID: "managed", MagpieModel: "group/original"}
	m.reviewedSelection = &codexSelection{Model: "provider/current", Effort: "high"}
	view := cleanUI(m.View())
	if !strings.Contains(view, "provider/current · high") || strings.Contains(view, "group/original") {
		t.Fatal(view)
	}
}
