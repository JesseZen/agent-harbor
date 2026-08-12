package app

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/asheshgoplani/agent-deck/internal/backend"
	"github.com/asheshgoplani/agent-deck/internal/configdraft"
	"github.com/asheshgoplani/agent-deck/internal/i18n"
	"github.com/asheshgoplani/agent-deck/internal/preferences"
	"github.com/asheshgoplani/agent-deck/internal/resourcepage"
	"github.com/asheshgoplani/agent-deck/internal/resourcepages/observations"
	"github.com/asheshgoplani/agent-deck/internal/resourcepages/overview"
	"github.com/asheshgoplani/agent-deck/internal/resourcepages/profiles"
	"github.com/asheshgoplani/agent-deck/internal/resourcepages/quotas"
	"github.com/asheshgoplani/agent-deck/internal/resourcepages/routes"
	"github.com/asheshgoplani/agent-deck/internal/resourcepages/targets"
	sessionpage "github.com/asheshgoplani/agent-deck/internal/session/page"
	"github.com/asheshgoplani/agent-deck/internal/spotlight"
	"github.com/asheshgoplani/agent-deck/internal/ui"
	"github.com/asheshgoplani/agent-deck/internal/upstream/lazygit/commandmenu"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

type tab int

const (
	tabOverview tab = iota
	tabSessions
	tabRoutes
	tabTargets
	tabQuotas
	tabObservations
)

var tabOrder = []tab{tabOverview, tabSessions, tabTargets, tabRoutes, tabObservations}

var tabLabelKeys = map[tab]string{
	tabOverview: "tab.overview", tabSessions: "tab.sessions", tabTargets: "tab.upstreams",
	tabRoutes: "tab.traffic_rules", tabQuotas: "tab.quotas", tabObservations: "tab.observations",
}

type sessionsSection int

const (
	sessionsSectionSessions sessionsSection = iota
	sessionsSectionProfiles
)

var sessionsSectionLabels = [...]string{"Sessions", "Profiles"}

const eventStreamDisconnectedStatus = "Event stream disconnected"

// Transient toast lifetime for Spotlight confirmations (language/theme, etc.).
const transientStatusTTL = 3 * time.Second

type clearStatusMsg struct {
	generation int
	text       string
}

// Model is the composition-only TUI root. Domain pages own their behavior;
// this host owns global navigation, one ConfigDraft, publication and reload.
type Model struct {
	source backend.SessionLoader

	runtimeSource  backend.Backend
	configSource   configSource
	statusSource   configMutationStatusSource
	sessionSource  agentSessionSource
	stageSource    stageSource
	probeSource    targetProbeSource
	ctx            context.Context
	cancel         context.CancelFunc
	watch          bool
	eventCursor    string
	active         tab
	sessionSection sessionsSection
	width          int
	height         int

	sessionHome    *ui.Home
	sessionBackend *snapshotBackend
	draft          *configdraft.Draft
	runtime        backend.Snapshot
	overview       *overview.Page
	sessions       *sessionpage.Page
	profiles       *profiles.Page
	routes         *routes.Page
	targets        *targets.Page
	quotas         *quotas.Page
	observations   *observations.Page
	status         string
	statusGen      int // bumps on each set; timeout clears only matching gen
	publishing     bool
	pageFocus      bool
	commands       *commandmenu.Model
	commandKind    spotlight.Kind
	commandStack   []commandFrame // Grok PaletteSnapshot stack; Esc pops
	help           *ui.HelpOverlay
	advancedPage   string
	lastLoadError  error
}

// commandFrame is one Spotlight level (root → language/theme), restored on Esc.
type commandFrame struct {
	kind   spotlight.Kind
	filter string
	cursor int
	scroll int
}

func New(source backend.SessionLoader) *Model {
	prefs, _ := preferences.Load()
	if prefs.Theme != "" {
		ui.InitTheme(prefs.Theme)
	}
	i18n.SelectInitialLocale(prefs.Locale)

	ctx, cancel := context.WithCancel(context.Background())
	sessionBackend := &snapshotBackend{source: source}
	model := &Model{
		source:         source,
		sessionHome:    ui.NewHomeWithBackend(sessionBackend),
		sessionBackend: sessionBackend,
		ctx:            ctx,
		cancel:         cancel,
		active:         tabSessions,
		commandKind:    spotlight.KindRoot,
	}
	model.runtimeSource, _ = source.(backend.Backend)
	model.configSource, _ = source.(configSource)
	model.statusSource, _ = source.(configMutationStatusSource)
	model.sessionSource, _ = source.(agentSessionSource)
	model.stageSource, _ = source.(stageSource)
	model.probeSource, _ = source.(targetProbeSource)
	_, model.watch = source.(interface {
		WatchInvalidations(context.Context, string) (<-chan backend.Invalidation, error)
	})
	return model
}

func (model *Model) Init() tea.Cmd {
	commands := []tea.Cmd{model.sessionHome.Init(), model.loadAll(false)}
	if model.watch {
		commands = append(commands, model.startInvalidationWatch())
	}
	return tea.Batch(commands...)
}

func (model *Model) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	switch typed := message.(type) {
	case clearStatusMsg:
		// Only clear if this toast is still showing (sticky statuses overwrite text).
		if typed.generation == model.statusGen && model.status == typed.text {
			model.status = ""
			model.resizePages()
		}
		return model, nil
	case tea.WindowSizeMsg:
		model.width = typed.Width
		model.height = typed.Height
		model.resizePages()
		if model.help != nil {
			model.help.SetSize(typed.Width, max(1, typed.Height-1))
		}
		return model, nil
	case loadResultMsg:
		model.applyLoadResult(typed)
		return model, nil
	case publishResultMsg:
		return model, model.applyPublishResult(typed)
	case targetProbeQueuedMsg:
		if typed.err != nil {
			model.status = "Saved, but connection test failed: " + typed.err.Error()
			model.resizePages()
			return model, nil
		}
		model.status = "Saved · testing connection..."
		model.resizePages()
		return model, model.checkTargetProbes(typed.targetIDs)
	case targetProbeCheckMsg:
		model.applyLoadResult(typed.load)
		if typed.load.runtimeErr != nil {
			model.status = "Saved · connection test pending"
			model.resizePages()
			return model, nil
		}
		failed, pending := false, false
		seen := 0
		wanted := make(map[string]bool, len(typed.targetIDs))
		for _, id := range typed.targetIDs {
			wanted[id] = true
		}
		for _, target := range typed.load.runtime.Targets {
			if !wanted[target.ID] {
				continue
			}
			seen++
			switch strings.ToLower(target.Health) {
			case "healthy":
			case "unhealthy":
				failed = true
			default:
				pending = true
			}
		}
		if seen < len(wanted) {
			pending = true
		}
		if failed {
			model.status = "Saved, but connection test failed"
		} else if pending {
			model.status = "Saved · connection test pending"
		} else {
			model.status = "Saved · connection test passed"
		}
		model.resizePages()
		return model, nil
	case sessionIntentResultMsg:
		if typed.err != nil {
			model.status = "Session action failed: " + typed.err.Error()
		} else {
			model.status = ""
		}
		return model, nil
	case invalidationWatchStartedMsg:
		if typed.err != nil {
			if !errors.Is(typed.err, backend.ErrUnsupported) {
				model.status = eventStreamDisconnectedStatus
				return model, retryInvalidationWatch()
			}
			return model, nil
		}
		model.clearEventStreamDisconnectedStatus()
		return model, listenForInvalidation(typed.events)
	case invalidationMsg:
		if typed.event.Err != nil {
			model.status = eventStreamDisconnectedStatus
			return model, retryInvalidationWatch()
		}
		model.clearEventStreamDisconnectedStatus()
		if typed.event.EventID != "" {
			model.eventCursor = typed.event.EventID
		}
		if model.sessions != nil && typed.event.Type == "session_changed" {
			model.sessions.InvalidatePreview(model.sessions.SelectedID())
		}
		_, sessionReload := model.updateSessionHome(tea.KeyMsg{Type: tea.KeyCtrlR})
		return model, tea.Batch(model.loadAll(false), sessionReload, listenForInvalidation(typed.events))
	case invalidationStreamClosedMsg:
		model.status = eventStreamDisconnectedStatus
		return model, retryInvalidationWatch()
	case invalidationWatchRetryMsg:
		return model, model.startInvalidationWatch()
	}

	if model.commands != nil && model.commands.IsOpen() {
		// Main tab row stays clickable under Spotlight (switch tab, keep overlay).
		if mouse, ok := message.(tea.MouseMsg); ok && mouse.Y == 0 {
			if mouse.Button == tea.MouseButtonLeft && mouse.Action == tea.MouseActionPress {
				if selected, hit := model.tabAtX(mouse.X); hit {
					model.active = selected
					model.advancedPage = ""
					model.pageFocus = false
				}
			}
			return model, nil
		}
		switch message.(type) {
		case tea.KeyMsg, tea.MouseMsg:
			return model.updateCommandOverlay(message)
		}
		return model, nil
	}
	if model.help != nil && model.help.IsVisible() {
		if key, ok := message.(tea.KeyMsg); ok {
			updated, command := model.help.Update(key)
			model.help = updated
			return model, command
		}
		return model, nil
	}

	if mouse, ok := message.(tea.MouseMsg); ok {
		if mouse.Y == 0 {
			if mouse.Button == tea.MouseButtonLeft && mouse.Action == tea.MouseActionPress {
				if selected, hit := model.tabAtX(mouse.X); hit {
					model.active = selected
					model.advancedPage = ""
					model.pageFocus = false
				}
			}
			return model, nil
		}
		mouse.Y--
		// Layout: main tabs → optional resource strip → optional status → page.
		switch {
		case model.active == tabSessions:
			if model.showStatusLine() {
				if mouse.Y == 0 {
					return model, nil
				}
				mouse.Y--
			}
			return model.updateSessionHome(mouse)
		case model.active == tabRoutes || model.active == tabTargets:
			// Page owns the strip at relative Y=0; status sits between strip and table.
			if model.showStatusLine() {
				if mouse.Y == 1 {
					return model, nil
				}
				if mouse.Y >= 2 {
					mouse.Y--
				}
			}
			message = mouse
		default:
			if model.showStatusLine() {
				if mouse.Y == 0 {
					return model, nil
				}
				mouse.Y--
			}
			message = mouse
		}
	}

	if key, ok := message.(tea.KeyMsg); ok {
		if model.active == tabSessions && model.sessionSection == sessionsSectionSessions {
			if !model.sessionHome.CanOpenGlobalCommandMenu() {
				return model.updateSessionHome(key)
			}
			switch key.String() {
			case "ctrl+right", "ctrl+left", "alt+right", "alt+left", ":":
				// These keys belong to the composition host in normal mode.
			case "ctrl+r":
				_, sessionReload := model.updateSessionHome(key)
				return model, tea.Batch(sessionReload, model.loadAll(false))
			default:
				return model.updateSessionHome(key)
			}
		}
		switch key.String() {
		case "ctrl+right":
			model.shiftTab(1)
			model.pageFocus = false
			return model, nil
		case "ctrl+left":
			model.shiftTab(-1)
			model.pageFocus = false
			return model, nil
		case "ctrl+r":
			return model, model.loadAll(false)
		case "ctrl+c":
			model.cancel()
			return model, tea.Quit
		case "q":
			if !model.pageFocus && !model.activeFiltering() {
				model.cancel()
				return model, tea.Quit
			}
		case "?":
			if !model.pageFocus && !model.activeFiltering() {
				model.help = ui.NewHelpOverlayWithContent(helpContentForTab(model.active))
				model.help.SetSize(model.width, max(1, model.height-1))
				model.help.Show()
				return model, nil
			}
		}
	}

	intent, consumed, command := model.updateActivePage(message)
	switch intent {
	case resourcepage.IntentCommands:
		model.openCommandOverlay()
		return model, command
	case resourcepage.IntentPublish:
		model.pageFocus = false
		return model, tea.Batch(command, model.publish())
	case resourcepage.IntentCreate, resourcepage.IntentEdit, resourcepage.IntentDelete, resourcepage.IntentDetails:
		model.pageFocus = true
	}
	if key, ok := message.(tea.KeyMsg); ok {
		switch key.String() {
		case "esc", "f2":
			model.pageFocus = false
		}
	}
	if consumed || command != nil {
		return model, command
	}
	if _, isKey := message.(tea.KeyMsg); !isKey && model.sessionHome != nil {
		_, sessionCommand := model.updateSessionHome(message)
		return model, tea.Batch(command, sessionCommand)
	}
	return model, nil
}

func (model *Model) clearEventStreamDisconnectedStatus() {
	if model.status == eventStreamDisconnectedStatus {
		model.status = ""
	}
}

func (model *Model) updateSessionHome(message tea.Msg) (tea.Model, tea.Cmd) {
	updated, command := model.sessionHome.Update(message)
	model.sessionHome = updated.(*ui.Home)
	return model, command
}

func (model *Model) updateActivePage(message tea.Msg) (resourcepage.Intent, bool, tea.Cmd) {
	if model.draft == nil {
		return resourcepage.IntentNone, false, nil
	}
	if model.advancedPage == "profiles" {
		intent, consumed := model.profiles.Update(message)
		return intent, consumed, nil
	}
	if model.advancedPage == "quotas" {
		command := model.quotas.Update(message)
		intent := model.quotas.LastIntent()
		return intent, model.pageMessageConsumed(message, intent), command
	}
	switch model.active {
	case tabOverview:
		intent, consumed := model.overview.Update(message)
		return intent, consumed, nil
	case tabSessions:
		intent, consumed := model.sessions.Update(message)
		switch intent {
		case sessionpage.IntentAttach, sessionpage.IntentLaunch, sessionpage.IntentResume,
			sessionpage.IntentEnd, sessionpage.IntentCreate:
			return intent, true, model.executeSessionIntent(intent)
		}
		return intent, consumed, nil
	case tabRoutes:
		updated, command := model.routes.Update(message)
		model.routes = updated.(*routes.Page)
		intent := model.routes.LastIntent()
		return intent, model.pageMessageConsumed(message, intent), command
	case tabTargets:
		updated, command := model.targets.Update(message)
		model.targets = updated.(*targets.Page)
		intent := model.targets.LastIntent()
		return intent, model.pageMessageConsumed(message, intent), command
	case tabQuotas:
		command := model.quotas.Update(message)
		intent := model.quotas.LastIntent()
		return intent, model.pageMessageConsumed(message, intent), command
	case tabObservations:
		command := model.observations.Update(message)
		intent := model.observations.LastIntent()
		return intent, model.pageMessageConsumed(message, intent), command
	default:
		return resourcepage.IntentNone, false, nil
	}
}

func (model *Model) pageMessageConsumed(message tea.Msg, intent resourcepage.Intent) bool {
	if intent != resourcepage.IntentNone || model.pageFocus || model.activeFiltering() {
		return true
	}
	_, isMouse := message.(tea.MouseMsg)
	if isMouse {
		return true
	}
	if key, ok := message.(tea.KeyMsg); ok {
		switch key.String() {
		case "?", "q":
			return false
		default:
			return true
		}
	}
	return false
}

func (model *Model) executeSessionIntent(intent sessionpage.Intent) tea.Cmd {
	page := model.sessions
	return func() tea.Msg {
		return sessionIntentResultMsg{intent: intent, err: page.ExecuteIntent(model.ctx, intent)}
	}
}

func (model *Model) View() string {
	row := model.renderTabRow()
	if model.help != nil && model.help.IsVisible() {
		return model.fitFrame(row + "\n" + model.help.View())
	}

	content := model.renderActivePage()
	view := row + "\n" + content
	if model.commands != nil && model.commands.IsOpen() {
		view = model.commands.ViewOver(view, model.width, model.height)
	}
	return model.fitFrame(view)
}

func (model *Model) statusLine() string {
	if !model.showStatusLine() {
		return ""
	}
	statusStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#FFB454")).Bold(true)
	return statusStyle.Render(ansi.Truncate(model.status, max(1, model.width), ""))
}

// setTransientStatus shows a toast that auto-clears (Spotlight language/theme, etc.).
// Sticky statuses (publish/disconnect) should assign model.status directly.
func (model *Model) setTransientStatus(text string) tea.Cmd {
	model.status = text
	model.statusGen++
	generation := model.statusGen
	model.resizePages()
	return tea.Tick(transientStatusTTL, func(time.Time) tea.Msg {
		return clearStatusMsg{generation: generation, text: text}
	})
}

// secondaryChromeLines is the number of page header rows that must stay
// directly under the main tabs (Traffic Rules and Upstreams resource strips).
func (model *Model) secondaryChromeLines() int {
	if model.advancedPage != "" {
		return 1
	}
	switch model.active {
	case tabRoutes, tabTargets:
		return 1
	default:
		return 0
	}
}

// placeStatusAfterChrome inserts the orange status below secondary tabs/strips.
func placeStatusAfterChrome(pageView, status string, chromeLines int) string {
	if status == "" {
		return pageView
	}
	if chromeLines <= 0 {
		return status + "\n" + pageView
	}
	lines := strings.Split(pageView, "\n")
	if len(lines) < chromeLines {
		return pageView + "\n" + status
	}
	head := strings.Join(lines[:chromeLines], "\n")
	tail := strings.Join(lines[chromeLines:], "\n")
	if tail == "" {
		return head + "\n" + status
	}
	return head + "\n" + status + "\n" + tail
}

func (model *Model) renderActivePage() string {
	if model.draft == nil {
		return "Loading Agent Harbor Core..."
	}
	status := model.statusLine()
	chrome := model.secondaryChromeLines()
	if model.advancedPage == "profiles" {
		return placeStatusAfterChrome(model.advancedHeader("Profiles")+"\n"+model.profiles.View(), status, chrome)
	}
	if model.advancedPage == "quotas" {
		return placeStatusAfterChrome(model.advancedHeader("Quota Groups")+"\n"+model.quotas.View(), status, chrome)
	}
	switch model.active {
	case tabOverview:
		return placeStatusAfterChrome(model.overview.View(), status, chrome)
	case tabSessions:
		return placeStatusAfterChrome(model.sessionHome.View(), status, chrome)
	case tabRoutes:
		return placeStatusAfterChrome(model.routes.View(), status, chrome)
	case tabTargets:
		return placeStatusAfterChrome(model.targets.View(), status, chrome)
	case tabQuotas:
		return placeStatusAfterChrome(model.quotas.View(), status, chrome)
	case tabObservations:
		return placeStatusAfterChrome(model.observations.View(), status, chrome)
	default:
		return ""
	}
}

func (model *Model) advancedHeader(resource string) string {
	return lipgloss.NewStyle().Foreground(ui.ColorTextDim).Render(" Advanced Resources / " + resource)
}

func (model *Model) renderTabRow() string {
	return ansi.Truncate(strings.Join(model.renderedTabParts(), " "), max(1, model.width), "")
}

func (model *Model) renderedTabParts() []string {
	activeStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(ui.ColorBg).
		Background(ui.ColorAccent).
		Padding(0, 1)
	inactiveStyle := lipgloss.NewStyle().
		Foreground(ui.ColorTextDim).
		Padding(0, 1)
	parts := make([]string, 0, len(tabOrder))
	for _, item := range tabOrder {
		label := i18n.T(tabLabelKeys[item])
		if item == model.active {
			parts = append(parts, activeStyle.Render(label))
		} else {
			parts = append(parts, inactiveStyle.Render(label))
		}
	}
	return parts
}

func (model *Model) tabAtX(x int) (tab, bool) {
	selected, ok := hitStyledRow(x, model.renderedTabParts())
	if !ok || selected < 0 || selected >= len(tabOrder) {
		return 0, false
	}
	return tabOrder[selected], true
}

func (model *Model) shiftTab(delta int) {
	index := 0
	for i, item := range tabOrder {
		if item == model.active {
			index = i
			break
		}
	}
	index = (index + delta + len(tabOrder)) % len(tabOrder)
	model.active = tabOrder[index]
	model.advancedPage = ""
}

func (model *Model) renderSessionsSectionRow() string {
	return ansi.Truncate(strings.Join(model.renderedSessionsSectionParts(), " "), max(1, model.width), "")
}

func (model *Model) renderedSessionsSectionParts() []string {
	activeStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(ui.ColorBg).
		Background(ui.ColorCyan).
		Padding(0, 1)
	inactiveStyle := lipgloss.NewStyle().
		Foreground(ui.ColorTextDim).
		Padding(0, 1)
	parts := make([]string, 0, len(sessionsSectionLabels))
	for index, label := range sessionsSectionLabels {
		if sessionsSection(index) == model.sessionSection {
			parts = append(parts, activeStyle.Render(label))
		} else {
			parts = append(parts, inactiveStyle.Render(label))
		}
	}
	return parts
}

func (model *Model) sessionSectionAtX(x int) (sessionsSection, bool) {
	selected, ok := hitStyledRow(x, model.renderedSessionsSectionParts())
	return sessionsSection(selected), ok
}

func hitStyledRow(x int, parts []string) (int, bool) {
	if x < 0 {
		return 0, false
	}
	position := 0
	for index, part := range parts {
		width := ansi.StringWidth(part)
		if x >= position && x < position+width {
			return index, true
		}
		position += width
		if index < len(parts)-1 {
			if x == position {
				return 0, false
			}
			position++
		}
	}
	return 0, false
}

func (model *Model) activeFiltering() bool {
	if model.draft == nil {
		return false
	}
	if model.advancedPage == "profiles" {
		return model.profiles.Inner().Table().Filtering()
	}
	if model.advancedPage == "quotas" {
		return model.quotas.Host().Table().Filtering()
	}
	switch model.active {
	case tabOverview:
		return model.overview.Inner().Table().Filtering()
	case tabSessions:
		return !model.sessionHome.CanOpenGlobalCommandMenu()
	case tabRoutes:
		return model.routes.Table().Filtering()
	case tabTargets:
		return model.targets.Table().Filtering()
	case tabQuotas:
		return model.quotas.Host().Table().Filtering()
	case tabObservations:
		return model.observations.Host().Table().Filtering()
	default:
		return false
	}
}

func (model *Model) resizePages() {
	if model.sessionHome != nil {
		_, _ = model.updateSessionHome(tea.WindowSizeMsg{
			Width: model.width, Height: model.sessionHomeHeight(),
		})
	}
	if model.draft == nil {
		return
	}
	contentHeight := max(8, model.height-1)
	if model.showStatusLine() {
		contentHeight = max(8, contentHeight-1)
	}
	model.overview.SetSize(model.width, contentHeight)
	model.sessions.SetSize(model.width, contentHeight)
	model.profiles.SetSize(model.width, max(8, contentHeight-1))
	model.routes.SetSize(model.width, contentHeight)
	model.targets.SetSize(model.width, contentHeight)
	model.quotas.SetSize(model.width, contentHeight)
	model.observations.SetSize(model.width, contentHeight)
}

func (model *Model) showStatusLine() bool {
	return model.status != "" &&
		!(model.active == tabSessions && model.sessionSection == sessionsSectionSessions)
}

func (model *Model) sessionHomeHeight() int {
	height := model.height - 1
	if height < 1 {
		return 1
	}
	return height
}

func (model *Model) fitFrame(view string) string {
	if model.width <= 0 || model.height <= 0 {
		return view
	}
	lines := strings.Split(strings.TrimSuffix(view, "\n"), "\n")
	if len(lines) > model.height {
		lines = lines[:model.height]
	}
	for len(lines) < model.height {
		lines = append(lines, "")
	}
	for index, line := range lines {
		lines[index] = ansi.Truncate(line, model.width, "")
	}
	return strings.Join(lines, "\n")
}

type sessionIntentResultMsg struct {
	intent sessionpage.Intent
	err    error
}
