package main

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

type appRefreshTick struct{}
type appSessionInspected struct {
	expected, current sessionView
	recheck           bool
	nativeConfig      string
	fingerprint       string
	candidates        []recoveryCandidate
	pane              previousPane
	blocked           error
	selection         *codexSelection
	selectionErr      error
	err               error
}

type appContextReady struct {
	id  string
	err error
}

type appLoaded struct {
	profiles       []profile
	initialProfile *profileBinding
	generation     uint64
	sessions       []sessionView
	err            error
}
type appNewReady struct {
	generation  uint64
	environment appEnvironment
	err         error
}
type appCreated struct {
	record session
	err    error
}
type appPreviousPane struct {
	pane previousPane
	err  error
}
type appOpened struct{ err error }
type appResult struct{ err error }
type appRoute struct {
	id     string
	route  observedRoute
	err    error
	window codexWindowObservation
}

// harborModel owns interaction only. All product effects cross the service seam.
type harborModel struct {
	preview              previewState
	previewExpanded      bool
	projectBranch        sessionBranchState
	motion               sessionMotion
	suspended            bool
	servicePending       bool
	serviceTaskID        uint64
	profileReturn        string
	profileReturnScroll  int
	profileReview        *profileReviewState
	activeProfile        *profileBinding
	profiles             []profile
	profileCursor        int
	profileDefaultCursor int
	profileInstances     []magpieInstance
	secondaryHelpScroll  int
	secondaryHelpFocus   string
	reviewOpenedAt       time.Time

	refreshContext   context.Context
	cancelRefresh    context.CancelFunc
	refreshing       bool
	pollScheduled    bool
	selectionMissing bool
	discoveryNotice  string

	uiFocus             string
	uiHover             string
	rowHover            string
	mousePosition       *tea.MouseMsg
	settings            *settingsModel
	mouseListScreen     string
	mouseListStart      int
	detailScroll        int
	picker              directoryPicker
	previousPane        previousPane
	recoveryCandidates  []recoveryCandidate
	recoveryCursor      int
	recoveryBlocked     string
	recoveryRetry       bool
	recoveryFingerprint string
	directoryChoices    []string
	directoryCursor     int
	scroll              int
	generation          uint64
	listGeneration      uint64
	routeSessionID      string
	codexWindow         codexWindowObservation
	service             applicationService
	grouper             sessionGrouper
	width, height       int
	sessions            []sessionView
	cursor              int
	archivedView        bool
	archivedFilter      string
	expandedGroups      map[string]bool
	keepSelectedID      string
	sessionsReturnID    string
	searchFocus         string
	filter              string
	input               textinput.Model
	screen              string
	previous            string
	action              string
	reviewed            session
	reviewedObservation *runtimeObservation
	notice              string
	noticeID            uint64
	noticeTransient     bool
	errorReturn         string
	noticeCanStart      bool
	route               observedRoute
	environment         appEnvironment
	nativeReview        string
	accountResult       string
	accountWaiting      *accountExec
	accountWaitReason   string
	accountProfile      *profileBinding
	accountGeneration   uint64
	accountSnapshot     *profileAccountResult
	accountCache        map[profileBinding]profileAccountResult
	accountUsagePending bool
	accountUsageError   string
	accountRow          string
	accountListScroll   int
	accountResetReview  string
	accountResetNotice  string
	ext                 extensionState
	reviewedSelection   *codexSelection
	selectionReviewErr  error
	selectionFallback   bool
	formActive          bool
	formFocus           int
	formSaved           sessionDraft
	creationConnections map[profileBinding]string
	codexEditor         codexOptionEditor
	creating            bool
	draft               sessionDraft
	reports             []instanceReport
	instanceCursor      int
	instanceID          string
	contextInstance     string
	newInstances        []instanceReport
	instanceFilter      string
	models              []catalogModel
	catalogFilter       string
	catalogCursor       int
	connectionCursor    int
	clientCursor        int
	referenceAccepted   bool
	newOnOpen           bool
}

func newHarborModel(s applicationService) harborModel {
	input := textinput.New()
	input.CharLimit = 1024
	ctx, cancel := context.WithCancel(context.Background())
	return harborModel{refreshContext: ctx, cancelRefresh: cancel, refreshing: true, service: s, width: 80, height: 24, input: input, screen: "sessions"}
}
func newMagpieModel(s applicationService) harborModel {
	m := newHarborModel(s)
	m.refreshing = false
	m.screen = "magpie-context-loading"
	return m
}
func (m harborModel) Init() tea.Cmd {
	if m.screen == "magpie-context-loading" {
		return func() tea.Msg { id, err := m.service.ManagementContext(); return appContextReady{id, err} }
	}

	if m.screen == "magpie" {
		return m.loadInstances()
	}
	return m.load()
}
func (m harborModel) load() tea.Cmd {
	return func() tea.Msg {
		var s []sessionView
		var e error
		if h, ok := m.service.(interface {
			ListContext(context.Context) ([]sessionView, error)
		}); ok {
			parent := m.refreshContext
			if parent == nil {
				parent = context.Background()
			}
			ctx, cancel := context.WithTimeout(parent, 2500*time.Millisecond)
			defer cancel()
			s, e = h.ListContext(ctx)
		} else {
			s, e = m.service.List()
		}
		var profiles []profile
		var initial *profileBinding
		if p, err := m.service.EnsureProfiles(); err == nil {
			profiles, _ = m.service.Profiles()
			if b, err := bindingOf(p, "codex"); err == nil {
				initial = &b
			}
		}
		return appLoaded{profiles: profiles, initialProfile: initial, sessions: s, err: e, generation: m.listGeneration}
	}
}
func (m harborModel) requestRefresh() (tea.Model, tea.Cmd) {
	if m.refreshing || m.refreshContext != nil && m.refreshContext.Err() != nil {
		return m, nil
	}
	m.refreshing = true
	m.listGeneration++
	return m, m.load()
}
func (m harborModel) forceRefresh() (tea.Model, tea.Cmd) {
	if m.refreshContext != nil && m.refreshContext.Err() != nil {
		return m, nil
	}
	m.refreshing = true
	m.listGeneration++
	return m, m.load()
}
func (m harborModel) refreshTimer() tea.Cmd {
	return func() tea.Msg {
		return tea.BatchMsg{tea.Tick(2*time.Second, func(time.Time) tea.Msg { return appRefreshTick{} })}
	}
}
func (m harborModel) sessionGrouper() sessionGrouper {
	if m.grouper != nil {
		return m.grouper
	}
	return statusGrouper{}
}
func (m harborModel) archivedCount() int {
	count := 0
	for _, s := range m.sessions {
		if !s.External && s.Archived {
			count++
		}
	}
	return count
}
func (m harborModel) sessionGroups() []sessionGroup {
	scoped := make([]sessionView, 0, len(m.sessions))
	for _, s := range m.sessions {
		if m.archivedView {
			if !s.External && s.Archived {
				scoped = append(scoped, s)
			}
		} else if !s.Archived {
			scoped = append(scoped, s)
		}
	}
	filtered := make([]sessionView, 0, len(scoped))
	texts := make([]string, len(scoped))
	for i, s := range scoped {
		texts[i] = s.Name + " " + s.WorkingDirectory + " " + s.MagpieModel
	}
	query := m.filter
	if m.archivedView {
		query = m.archivedFilter
	}
	for _, hit := range searchMatches(query, texts) {
		filtered = append(filtered, scoped[hit.Index])
	}
	if m.archivedView {
		if len(filtered) == 0 {
			return nil
		}
		return []sessionGroup{{
			ID:       "archived",
			Title:    "Archived",
			Meta:     strconv.Itoa(len(filtered)),
			Sessions: pinFirst(filtered),
		}}
	}
	return m.sessionGrouper().Group(filtered)
}
func (m harborModel) sessionDisplayGroups() []sessionDisplayGroup {
	groups := m.sessionGroups()
	if len(groups) == 0 {
		return nil
	}
	filtering := m.filter != ""
	if m.archivedView {
		filtering = m.archivedFilter != ""
	}
	out := make([]sessionDisplayGroup, 0, len(groups))
	for _, g := range groups {
		expanded := !m.archivedView && m.expandedGroups[g.ID]
		out = append(out, foldSessionGroup(g, expanded, filtering, m.keepSelectedID))
	}
	return out
}
func (m harborModel) visibleRows() []sessionRow {
	groups := m.sessionDisplayGroups()
	out := make([]sessionRow, 0, len(m.sessions))
	for _, g := range groups {
		out = append(out, g.Rows...)
	}
	return out
}
func (m harborModel) visible() []sessionView {
	rows := m.visibleRows()
	out := make([]sessionView, 0, len(rows))
	for _, r := range rows {
		if r.Kind == sessionRowItem {
			out = append(out, r.View)
		}
	}
	return out
}
func (m harborModel) selectedRow() (sessionRow, bool) {
	rows := m.visibleRows()
	if m.selectionMissing || len(rows) == 0 {
		return sessionRow{}, false
	}
	return rows[min(max(0, m.cursor), len(rows)-1)], true
}
func (m harborModel) selected() (sessionView, bool) {
	row, ok := m.selectedRow()
	if !ok || row.Kind != sessionRowItem {
		return sessionView{}, false
	}
	return row.View, true
}
func (m harborModel) currentGroupExpanded() (string, bool) {
	if m.archivedView || m.filter != "" {
		return "", false
	}
	row, ok := m.selectedRow()
	if !ok || !m.expandedGroups[row.GroupID] {
		return "", false
	}
	for _, g := range m.sessionDisplayGroups() {
		if g.ID == row.GroupID && g.Expanded {
			return row.GroupID, true
		}
	}
	return "", false
}
func (m *harborModel) focusSessionRowID(id string) {
	if id != "" && !strings.HasPrefix(id, "toggle:") {
		m.keepSelectedID = id
	} else {
		m.keepSelectedID = ""
	}
	rows := m.visibleRows()
	if len(rows) == 0 {
		m.cursor = 0
		return
	}
	for i, r := range rows {
		if r.RowID() == id {
			m.cursor = i
			return
		}
	}
	m.cursor = min(max(0, m.cursor), len(rows)-1)
}
func (m *harborModel) moveSessionCursor(delta int) {
	rows := m.visibleRows()
	if len(rows) == 0 {
		m.cursor = 0
		m.keepSelectedID = ""
		m.route = observedRoute{}
		return
	}
	cur := min(max(0, m.cursor), len(rows)-1)
	nextIdx := min(max(0, cur+delta), len(rows)-1)
	target := rows[nextIdx]
	m.focusSessionRowID(target.RowID())
	m.selectionMissing = false
	m.route = observedRoute{}
}
func (m *harborModel) toggleSessionGroup(groupID string) {
	if m.expandedGroups == nil {
		m.expandedGroups = map[string]bool{}
	}
	if m.expandedGroups[groupID] {
		delete(m.expandedGroups, groupID)
	} else {
		m.expandedGroups[groupID] = true
	}
	m.mouseListScreen = ""
	m.selectionMissing = false
	m.route = observedRoute{}
	m.focusSessionRowID("toggle:" + groupID)
}
func (m *harborModel) collapseCurrentGroup() bool {
	groupID, ok := m.currentGroupExpanded()
	if !ok {
		return false
	}
	delete(m.expandedGroups, groupID)
	m.mouseListScreen = ""
	m.selectionMissing = false
	m.route = observedRoute{}
	m.focusSessionRowID("toggle:" + groupID)
	return true
}
func (m harborModel) neighborRowIDAfterRemoval(targetID string) string {
	rows := m.visibleRows()
	for i, r := range rows {
		if r.RowID() == targetID {
			if i+1 < len(rows) {
				return rows[i+1].RowID()
			}
			if i-1 >= 0 {
				return rows[i-1].RowID()
			}
			return ""
		}
	}
	return ""
}
func (m harborModel) openSettingsModel() settingsModel {
	settings := newSettingsModel()
	settings.width, settings.height = m.width, m.height
	switch len(m.profiles) {
	case 0:
		settings.profileSummary = "Default"
	case 1:
		settings.profileSummary = m.profiles[0].Name
	default:
		active := m.profileShortLabel(m.activeProfile)
		settings.profileSummary = fmt.Sprintf("%d profiles · %s", len(m.profiles), active)
	}
	return settings
}

func (m harborModel) update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m.settings != nil && !isRuntimeRefresh(msg) {
		next, cmd := m.settings.Update(msg)
		settings := next.(settingsModel)
		if settings.closed {
			m.settings = nil
			if settings.nav != "" {
				if settings.nav == "accounts" {
					m.screen = "sessions"
					return m.updatePage(uiKey("a"))
				}
				if settings.nav == "extensions" {
					m.screen = "extensions"
					m.scroll = 0
					return m, m.beginExtensionsInspect()
				}
				m.screen = settings.nav
				if m.screen == "profiles" {
					m.profileReturn = "settings"
					m.screen = "profile-list"
					return m, m.loadProfiles()
				}
				if m.screen == "magpie" {
					return m, m.loadInstances()
				}
			}
			return m, nil
		}
		m.settings = &settings
		return m, cmd
	}
	// Help overlays the underlying model; async responses still advance it.
	if m.screen == "help" {
		_, key := msg.(tea.KeyMsg)
		_, resize := msg.(tea.WindowSizeMsg)
		if !key && !resize {
			overlayScroll := m.scroll
			m.screen = m.previous
			m.scroll = m.secondaryHelpScroll
			m.uiFocus = m.secondaryHelpFocus
			next, cmd := m.updatePage(msg)
			underlying := next.(harborModel)
			underlying.previous = underlying.screen
			underlying.secondaryHelpScroll = underlying.scroll
			underlying.scroll = overlayScroll
			underlying.screen = "help"
			return underlying, cmd
		}
	}
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = max(20, msg.Width)
		m.height = max(10, msg.Height)
		return m, nil
	case directoryPreviewResult:
		p := &m.picker
		if (m.screen != "new-directory" && !(m.screen == "help" && m.previous == "new-directory")) || msg.generation != p.previewGeneration || msg.path != p.previewPath {
			return m, nil
		}
		rows := p.matches()
		if len(rows) == 0 || rows[min(p.cursor, len(rows)-1)].path != msg.path {
			return m, nil
		}
		p.preview = msg.text
		return m, nil
	case directoryResult:
		if (m.screen != "new-directory" && !(m.screen == "help" && m.previous == "new-directory")) || msg.generation != m.picker.generation {
			return m, nil
		}
		m.picker.entries = msg.entries
		m.picker.searching = false
		m.picker.err = msg.err
		m.picker.truncated = msg.truncated
		return m, m.requestDirectoryPreview()
	case appDirectoryValidated:
		if m.screen != "new-project" || msg.generation != m.generation || msg.path != m.input.Value() {
			return m, nil
		}
		m.notice = "Directory ready. Enter continues."
		if msg.err != nil {
			m.notice = msg.err.Error()
		}
		return m, nil
	case appContextReady:
		if m.screen != "magpie-context-loading" {
			return m, nil
		}
		m.screen = "magpie"
		m.contextInstance = msg.id
		if msg.err != nil {
			m.screen = "magpie-context"
			m.notice = msg.err.Error() + ". Enter to list instances; Esc returns."
			return m, nil
		}
		return m, m.loadInstances()
	case appRefreshTick:
		if m.refreshContext != nil && m.refreshContext.Err() != nil {
			return m, nil
		}
		next, cmd := m.requestRefresh()
		n := next.(harborModel)
		n.pollScheduled = true
		return n, tea.Batch(cmd, n.refreshTimer())
	case appLoaded:
		if msg.profiles != nil {
			m.profiles = msg.profiles
		}
		if m.activeProfile == nil && msg.initialProfile != nil {
			m.activeProfile = msg.initialProfile
		}
		if msg.generation != m.listGeneration {
			return m, nil
		}
		var accounts tea.Cmd
		if m.screen == "accounts" && m.accountProfile == nil && m.activeProfile != nil {
			m.accountProfile = m.activeProfile
			accounts = m.beginAccountInspect()
		}
		var extensions tea.Cmd
		if m.screen == "extensions" && m.ext.profile == nil && m.activeProfile != nil {
			b := *m.activeProfile
			m.ext.profile = &b
			extensions = m.beginExtensionsInspect()
		}
		m.refreshing = false
		previousDiscovery := m.discoveryNotice
		m.discoveryNotice = ""
		if msg.err != nil {
			m.discoveryNotice = msg.err.Error()
			for i := range m.sessions {
				v := &m.sessions[i]
				v.Running = false
				v.Status = "Unknown"
				v.Runtime.Activity = "unknown"
				v.Runtime.Presence = "stale"
				v.DiscoveryWarning = msg.err.Error()
			}
		} else {
			m.sessions = msg.sessions
			for _, s := range msg.sessions {
				if s.DiscoveryWarning != "" {
					m.discoveryNotice = s.DiscoveryWarning
					break
				}
			}
			m.cursor = min(m.cursor, max(0, len(m.visibleRows())-1))
		}
		if m.discoveryNotice != "" {
			m.notice = m.discoveryNotice
		} else if m.notice == previousDiscovery {
			m.notice = ""
		}
		var newSession tea.Cmd
		if m.newOnOpen {
			m.newOnOpen = false
			if m.screen == "new-project-loading" {
				newSession = m.prepareNewSession()
			}
		}
		if !m.pollScheduled {
			m.pollScheduled = true
			return m, tea.Batch(m.refreshTimer(), accounts, extensions, newSession)
		}
		return m, tea.Batch(accounts, extensions, newSession)
	case appSessionInspected:
		if m.refreshContext.Err() != nil || m.screen != "busy" || m.reviewed.ID != msg.expected.ID {
			return m, nil
		}
		return m.finishEnterInspection(msg)
	case appReviewedResult:
		if m.screen != "busy" || msg.generation != m.generation || msg.target != m.reviewed.ID {
			return m, nil
		}
		var err error
		switch v := msg.result.(type) {
		case appOpened:
			err = v.err
		case appResult:
			err = v.err
		}
		if err != nil {
			m.screen = "confirm"
			var recoveryErr *recoveryOpenError
			if errors.As(err, &recoveryErr) {
				m.reviewed = recoveryErr.approval.Session
				m.reviewedObservation = recoveryErr.approval.Observation
			}
			if m.action == "query-resume" && len(m.recoveryCandidates) > 0 {
				if errors.Is(err, errRecoveryStale) {
					m.recoveryBlocked = err.Error()
				} else {
					m.recoveryRetry = true
				}
				m.scroll = 0
			}
			m.notice = err.Error()
			m.reviewOpenedAt = time.Now()
			return m, nil
		}
		return m.update(msg.result)
	case appResult:
		m.screen = "sessions"
		m.notice = "Action completed"
		if msg.err != nil {
			m.notice = msg.err.Error()
			m.screen = "error"
		} else if m.action == "remove" {
			nextID := m.neighborRowIDAfterRemoval(m.reviewed.ID)
			for i := range m.sessions {
				if m.sessions[i].ID == m.reviewed.ID {
					m.sessions = append(m.sessions[:i], m.sessions[i+1:]...)
					break
				}
			}
			m.selectionMissing = false
			m.focusSessionRowID(nextID)
		}
		return m.forceRefresh()
	case appNewInstances:
		if msg.generation != m.generation || m.screen != "new-instance-loading" {
			return m, nil
		}
		if msg.err != nil {
			m.notice = msg.err.Error()
			m.screen = "new-connection"
			if m.formActive {
				m.draft = m.formSaved
				m.screen = "new-form"
			}
			return m, nil
		}
		m.newInstances = msg.instances
		m.instanceFilter = ""
		m.instanceCursor = 0
		preferred := m.environment.DefaultInstance
		if m.formActive && m.draft.Instance != "" {
			preferred = m.draft.Instance
		}
		for i, r := range m.newInstances {
			if r.ID == preferred {
				m.instanceCursor = i
				break
			}
		}
		if m.formActive && !msg.chooseInstance && len(m.newInstances) > 0 {
			candidate := m.newInstances[m.instanceCursor]
			if candidate.Health == instanceVerified && (candidate.ID == preferred || preferred == "" && len(m.newInstances) == 1) {
				cmd := m.formCatalog(candidate.ID)
				return m, cmd
			}
		}
		m.screen = "new-instance"
		return m, nil
	case appCatalog:
		if msg.generation != m.generation || msg.instance != m.draft.Instance || m.screen != "new-catalog-loading" {
			return m, nil
		}
		if msg.err != nil {
			m.notice = msg.err.Error()
			if m.formActive {
				m.draft = m.formSaved
				if len(m.newInstances) == 0 {
					cmd := m.formInstances(true)
					return m, cmd
				}
				m.screen = "new-instance"
				return m, nil
			}
			m.screen = "new-instance"
			return m, nil
		}
		m.models = msg.models
		m.catalogCursor = 0
		m.catalogFilter = ""
		m.screen = "new-catalog"
		if m.formActive {
			for i, model := range m.models {
				if model.ID == m.draft.Model {
					m.catalogCursor = i
					break
				}
			}
			m.uiFocus = "selector-search"
			m.input.SetValue("")
			cmd := m.input.Focus()
			return m, cmd
		}
		return m, nil
	case appPreviousPane:
		if m.screen != "busy" {
			return m, nil
		}
		if msg.err != nil {
			m.screen = "error"
			m.notice = msg.err.Error()
			return m, nil
		}
		m.previousPane = msg.pane
		m.reviewed = msg.pane.Session
		m.action = "close-previous"
		m.scroll = 0
		m.screen = "confirm"
		return m, nil
	case appOpened:
		if msg.err == nil {
			if m.cancelRefresh != nil {
				m.cancelRefresh()
			}
			return m, tea.Quit
		}
		m.notice = msg.err.Error()
		m.screen = "error"
		return m.requestRefresh()
	case appInstances:
		if msg.generation != m.generation || m.screen != "magpie" {
			return m, nil
		}
		if msg.err != nil {
			m.notice = msg.err.Error()
		} else {
			m.reports = msg.reports
			if m.contextInstance != "" {
				id := m.contextInstance
				m.contextInstance = ""
				found := false
				for i, r := range m.reports {
					if r.ID == id {
						m.instanceCursor = i
						found = true
						break
					}
				}
				if !found {
					m.notice = "Session instance " + id + " is no longer registered. Enter to choose another instance for management; Esc returns. The session connection stays unchanged."
					m.screen = "magpie-context"
					return m, nil
				}
			}
			m.instanceCursor = min(m.instanceCursor, max(0, len(m.instanceRows())-1))
		}
		m.screen = "magpie"
		return m, nil
	case appMagpieDone:
		m.screen = "magpie"
		m.generation++
		if msg.err != nil {
			m.notice = msg.err.Error()
		} else {
			m.notice = "Magpie action completed"
		}
		return m, tea.Batch(m.loadInstances(), tea.EnableMouseAllMotion)
	case appAccountDone:
		m.screen = "accounts-result"
		m.accountResult = msg.result
		if msg.err != nil {
			m.accountResult = msg.err.Error()
		}
		return m, nil
	case appNewReady:
		if msg.generation != m.generation || m.screen != "new-project-loading" {
			return m, nil
		}
		if msg.err != nil {
			m.notice = msg.err.Error()
			m.screen = "sessions"
			return m, nil
		}
		m.environment = msg.environment
		if m.activeProfile == nil {
			m.activeProfile = msg.environment.Profile
		}
		connection := msg.environment.LastConnection
		if msg.environment.Profile != nil {
			if choice := m.creationConnections[*msg.environment.Profile]; choice != "" {
				connection = choice
			}
		}
		if connection != "magpie" {
			connection = "local"
		}
		m.draft = sessionDraft{
			Profile: msg.environment.Profile, Connection: connection, Directory: msg.environment.Directory,
			Instance: msg.environment.LastMagpieInstance, Model: msg.environment.LastMagpieModel,
			CodexOptions: msg.environment.CodexOptions.saved(),
		}
		m.newInstances = nil
		m.formActive = true
		m.formFocus = 0
		m.creating = false
		m.screen = "new-form"
		m.input.SetValue(msg.environment.Directory)
		m.input.Focus()
		return m, nil
	case appFormRejected:
		if !m.formActive || !m.creating || msg.generation != m.generation || m.screen != "busy" {
			return m, nil
		}
		m.creating = false
		m.formFocus = min(formName, msg.focus)
		m.screen = msg.page
		m.notice = ""
		if msg.err != nil {
			m.notice = msg.err.Error()
		}
		return m, nil
	case appPreparedDraft:
		if !m.formActive || !m.creating || msg.generation != m.generation || m.screen != "busy" {
			return m, nil
		}
		m.draft = msg.draft
		return m.update(appFormCreated{msg.generation, msg.result})
	case appFormCreated:
		if !m.formActive || !m.creating || msg.generation != m.generation || m.screen != "busy" {
			return m, nil
		}
		return m.update(msg.result)
	case appCreated:
		if m.formActive {
			if !m.creating || m.screen != "busy" {
				return m, nil
			}
			m.creating = false
			if errors.Is(msg.err, errRecoveryChoice) {
				m.screen = "new-recovery"
				m.uiFocus = "recovery:esc"
				m.scroll = 0
				return m, nil
			}
			if msg.err != nil {
				m.screen = "new-form"
				m.formFocus = 0
				m.notice = msg.err.Error()
				return m, nil
			}
		}
		if msg.err != nil {
			m.notice = msg.err.Error()
			m.screen = "sessions"
			return m.requestRefresh()
		}
		m.reviewed = msg.record
		return m, func() tea.Msg {
			return appOpened{m.service.Open(sessionApproval{Session: msg.record, Confirmed: true})}
		}
	case appRoute:
		s, ok := m.selected()
		if ok && s.ID == msg.id {
			m.routeSessionID = msg.id
			m.route = msg.route
			m.codexWindow = msg.window
			if msg.err != nil {
				m.route = observedRoute{Reason: msg.err.Error()}
			}
		}
		return m, nil
	case tea.KeyMsg:
		key := msg.String()
		if key == "ctrl+c" {
			if m.accountWaiting != nil {
				m.accountWaiting.login.close()
			}
			if m.cancelRefresh != nil {
				m.cancelRefresh()
			}
			return m, tea.Quit
		}
		if m.screen == "busy" || m.screen == "magpie-busy" || m.screen == "accounts-busy" {
			return m, nil
		}
		if (m.screen == "accounts" || m.screen == "accounts-result") && key != "?" {
			return m.updateAccounts(key)
		}
		if key == "ctrl+g" {
			m.notice = ""
			m.noticeID++
			if m.screen == "error" {
				m.screen = m.errorReturn
				if m.screen == "" {
					m.screen = "sessions"
				}
				m.scroll = 0
			}
			return m, nil
		}
		if !m.editingText() && key == "!" && m.notice != "" && m.screen != "error" {
			m.errorReturn = m.screen
			m.screen = "error"
			m.scroll = 0
			return m, nil
		}
		if m.screen == "error" || m.screen == "detail" || m.screen == "magpie-detail" || m.screen == "new-instance-detail" {
			if key == "esc" {
				switch m.screen {
				case "error":
					m.screen = m.errorReturn
					if m.screen == "" {
						m.screen = "sessions"
					}
				case "magpie-detail":
					m.screen = "magpie"
				case "new-instance-detail":
					m.screen = "new-instance"
				default:
					m.screen = "sessions"
				}
				m.scroll = 0
				return m, nil
			}
			if m.screen == "error" && key == "r" {
				m.screen = m.errorReturn
				m.notice = ""
				if strings.HasPrefix(m.screen, "magpie") {
					m.screen = "magpie"
					m.generation++
					return m, m.loadInstances()
				}
				m.screen = "sessions"
				return m.requestRefresh()
			}
			if m.screen == "error" && key == "s" && m.errorReturn == "magpie" && m.noticeCanStart {
				m.screen = "magpie"
				return m.updateMagpie(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("s")})
			}
			if key == "j" || key == "down" {
				m.scroll = min(m.scroll+1, m.scrollLimit())
				return m, nil
			}
			if key == "k" || key == "up" {
				m.scroll = max(0, min(m.scroll, m.scrollLimit())-1)
				return m, nil
			}
			if key != "?" && key != "q" {
				return m, nil
			}
		}
		if m.screen == "help" {
			if key == "down" || key == "j" {
				m.scroll = min(m.scroll+1, m.scrollLimit())
				return m, nil
			}
			if key == "up" || key == "k" {
				m.scroll = max(0, min(m.scroll, m.scrollLimit())-1)
				return m, nil
			}
			if key == "esc" || key == "?" {
				m.screen = m.previous
				m.scroll = m.secondaryHelpScroll
				m.uiFocus = m.secondaryHelpFocus
			}
			if key == "q" {
				if m.cancelRefresh != nil {
					m.cancelRefresh()
				}
				return m, tea.Quit
			}
			return m, nil
		}
		if m.formActive && (key != "?" || m.screen == "new-name") && (m.screen == "new-form" || m.screen == "new-name" || m.screen == "new-cli" || m.screen == "new-connection" || m.screen == "new-notice") {
			return m.updateNew(msg)
		}
		if m.screen == "new-directory" {
			return m.updateDirectory(msg)
		}
		if key == "?" {
			m.secondaryHelpScroll = m.scroll
			m.secondaryHelpFocus = m.uiFocus
			m.scroll = 0
			m.previous = m.screen
			m.screen = "help"
			return m, nil
		}
		if key == "q" && m.screen != "magpie-bind" && m.screen != "magpie-filter" && (!strings.HasPrefix(m.screen, "new-") || (m.screen != "new-project" && m.screen != "new-name" && m.screen != "new-instance-filter" && m.screen != "new-catalog-filter")) && m.screen != "filter" && m.screen != "rename" && m.screen != "delete-history" {
			if m.cancelRefresh != nil {
				m.cancelRefresh()
			}
			return m, tea.Quit
		}
		if m.screen == "magpie-confirm" || m.screen == "confirm" || m.screen == "new-review" || m.screen == "new-notice" {
			if key == "down" || key == "j" {
				m.scroll = min(m.scroll+1, m.scrollLimit())
				return m, nil
			}
			if key == "up" || key == "k" {
				m.scroll = max(0, min(m.scroll, m.scrollLimit())-1)
				return m, nil
			}
		}
		if m.screen == "magpie-filter" || m.screen == "new-instance-filter" {
			if key == "esc" || key == "enter" {
				if key == "enter" {
					m.instanceFilter = m.input.Value()
					m.instanceCursor = 0
				}
				if m.screen == "magpie-filter" {
					m.screen = "magpie"
				} else {
					m.screen = "new-instance"
				}
				return m, nil
			}
			var cmd tea.Cmd
			m.input, cmd = m.input.Update(msg)
			return m, cmd
		}
		if m.screen == "magpie" || m.screen == "new-instance" {
			if key == "/" {
				m.screen += "-filter"
				m.input.SetValue(m.instanceFilter)
				m.input.Focus()
				return m, nil
			}
			if key == "i" && len(m.instanceRows()) > 0 {
				m.screen += "-detail"
				m.scroll = 0
				return m, nil
			}
		}
		if strings.HasPrefix(m.screen, "magpie") {
			return m.updateMagpie(msg)
		}
		if strings.HasPrefix(m.screen, "new-") {
			return m.updateNew(msg)
		}
		if m.screen == "delete-history" {
			if key == "esc" || key == "enter" {
				m.screen = "sessions"
			}
			return m, nil
		}
		if m.screen == "filter" || m.screen == "rename" {
			if key == "esc" {
				m.screen = "sessions"
				return m, nil
			}
			if key == "enter" {
				if m.screen == "filter" {
					m.filter = m.input.Value()
					m.cursor = 0
					m.screen = "sessions"
					return m, nil
				}
				name := m.input.Value()
				reviewed := sessionView{session: m.reviewed}
				if m.reviewedObservation != nil {
					reviewed.Runtime = *m.reviewedObservation
				}
				return m, func() tea.Msg { return appResult{m.service.RenameObserved(reviewed, name)} }
			}
			var cmd tea.Cmd
			m.input, cmd = m.input.Update(msg)
			return m, cmd
		}
		if key == "q" {
			if m.cancelRefresh != nil {
				m.cancelRefresh()
			}
			return m, tea.Quit
		}
		if key == "esc" {
			if m.screen == "sessions" && m.archivedView {
				m.archivedView = false
				m.archivedFilter = ""
				m.selectionMissing = false
				m.mouseListScreen = ""
				m.restoreSearchSelection(m.sessionsReturnID)
				m.notice = ""
				return m, nil
			}
			m.screen = "sessions"
			if m.isEnterRecovery() {
				m.restoreSearchSelection(m.reviewed.ID)
			}
			m.notice = ""
			return m, nil
		}
		if m.screen == "confirm" {
			if m.isEnterRecovery() {
				switch key {
				case "r":
					if m.recoveryBlocked != "" {
						return m.inspectEnter(sessionView{session: m.reviewed}, true)
					}
				case "o":
					if m.recoveryBlocked != "" && m.previousPane.PaneID != "" {
						pane := m.previousPane
						return m, func() tea.Msg { return appOpened{m.service.ViewPreviousPane(pane)} }
					}
				case "enter":
					if m.recoveryBlocked != "" || m.recoveryCursor < 0 || m.recoveryCursor >= len(m.recoveryCandidates) {
						return m, nil
					}
				}
			}
			if (m.action == "query-resume" || m.action == "resume") && m.reviewed.Mode == "magpie" && m.reviewed.CLI == "codex" && m.selectionReviewErr != nil && key == "b" {
				if !errors.Is(m.selectionReviewErr, errCodexProfileMissing) {
					m.notice = "Session profile is unavailable or conflicts; resolve it before recovery."
					return m, nil
				}
				id, err := codexReference(m.selectedRecoveryTarget())
				if err != nil {
					m.notice = err.Error()
					return m, nil
				}
				m.reviewedSelection = &codexSelection{ThreadID: id, Model: m.reviewed.MagpieModel, Fallback: true}
				m.selectionFallback = true
				return m, nil
			}
			if m.action == "review-reference" && key == "a" {
				m.referenceAccepted = true
				return m, nil
			}
			if m.action == "review-reference" && key == "enter" && !m.referenceAccepted {
				m.notice = "Press a to explicitly accept the saved identity review, or Esc to cancel."
				return m, nil
			}
			if (m.action == "query-resume" || m.action == "resume") && key == "enter" && m.reviewed.Mode == "magpie" && m.reviewed.CLI == "codex" && !joinedCodex(m.reviewed) && m.reviewedSelection == nil {
				m.notice = "Session model unavailable; review the uncertainty and choose the recorded model explicitly, or cancel."
				return m, nil
			}
			if key != "enter" {
				return m, nil
			}
			if !m.isEnterRecovery() && (m.action == "query-resume" || m.action == "resume") && m.reviewed.Conversation == nil {
				m.notice = missingConversationRecovery
				return m, nil
			}
			a := sessionApproval{Session: m.reviewed, MagpieSelection: m.reviewedSelection, Observation: m.reviewedObservation, Confirmed: true, DevelopmentQuery: m.action == "query-resume", QueryUncertaintyAccepted: m.action == "query-resume", SavedReferenceAccepted: m.action == "query-resume"}
			if m.isEnterRecovery() {
				a.Recovery = &recoveryApproval{Target: m.recoveryCandidates[m.recoveryCursor].Reference, Shell: m.previousPane, Fingerprint: m.recoveryFingerprint}
				a.SavedReferenceAccepted = false
			}
			action := m.action
			m.generation++
			generation := m.generation
			target := m.reviewed.ID
			m.screen = "busy"
			return m, func() tea.Msg {
				var e error
				switch action {
				case "stop":
					e = m.service.Stop(a)
				case "remove":
					e = m.service.Remove(a)
				case "close-previous":
					e = m.service.ClosePreviousPane(m.previousPane)
				case "review-reference":
					ref, err := savedReferenceForReview(a.Session)
					if err != nil {
						e = err
					} else {
						e = m.service.Reassociate(a, ref, "Explicitly reviewed the saved conversation reference and connection in Harbor")
					}
				case "resume", "start", "query-resume":
					e = m.service.Open(a)
				}
				if action == "resume" || action == "start" || action == "query-resume" {
					return appReviewedResult{generation, target, appOpened{e}}
				}
				return appReviewedResult{generation, target, appResult{e}}
			}
		}
		if m.screen == "busy" {
			return m, nil
		}
		switch key {
		case "j", "down":
			m.moveSessionCursor(1)
		case "k", "up":
			m.moveSessionCursor(-1)
		case "/":
			m.screen = "filter"
			m.input.SetValue(m.searchQuery())
			m.input.Focus()
		case ",":
			if m.archivedView {
				return m, nil
			}
			settings := newSettingsModel()
			settings.width = m.width
			settings.height = m.height
			m.settings = &settings
			return m, nil
		case "m":
			if m.archivedView {
				return m, nil
			}
			m.contextInstance = ""
			if s, ok := m.selected(); ok && s.Mode == "magpie" {
				m.contextInstance = s.InstanceID
			}
			m.generation++
			m.instanceFilter = ""
			m.instanceCursor = 0
			m.screen = "magpie"
			return m, m.loadInstances()
		case "p":
			if m.archivedView {
				return m, nil
			}
			m.screen = "profile-list"
			m.notice = ""
			return m, m.loadProfiles()
		case "a":
			if m.archivedView {
				return m, nil
			}
			m.screen = "accounts"
			m.accountRow = ""
			m.scroll = 0
			if m.activeProfile != nil {
				b := *m.activeProfile
				m.accountProfile = &b
				m.accountSnapshot = nil
			}
			if m.formActive && m.draft.Profile != nil {
				b := *m.draft.Profile
				m.accountProfile = &b
				m.accountSnapshot = nil
			}
			if m.accountProfile != nil {
				cmd := m.beginAccountInspect()
				return m, cmd
			}
		case "n":
			if m.archivedView {
				return m, nil
			}
			cmd := m.prepareNewSession()
			return m, cmd
		case "r":
			return m.requestRefresh()
		case "enter", "s", "x", "e", "i", "c", "d", "v":
			if key == "enter" {
				row, ok := m.selectedRow()
				if !ok {
					return m, nil
				}
				if row.Kind == sessionRowToggle {
					m.toggleSessionGroup(row.GroupID)
					return m, nil
				}
				if m.archivedView {
					key = "i"
				}
			}
			if m.archivedView && key != "i" && key != "x" {
				return m, nil
			}
			s, ok := m.selected()
			if !ok {
				return m, nil
			}
			if s.External && key != "enter" && key != "i" {
				m.notice = "External / unmanaged agent: manage it in its terminal. Harbor has no recorded connection intent."
				return m, nil
			}
			if s.External && key == "i" {
				m.screen = "detail"
				return m, nil
			}
			switch key {
			case "v":
				if s.DiscoveryWarning != "" {
					m.notice = s.DiscoveryWarning
					return m, nil
				}
				if s.Runtime.Presence == "observed" || s.Running || processGroupActive(s.ProcessGroup) || sessionLaunchPending(s.session) {
					m.notice = "Stop the live session or wait for launch before reviewing its saved identity."
					return m, nil
				}
				if _, err := savedReferenceForReview(s.session); err != nil {
					m.notice = err.Error()
					return m, nil
				}
				m.reviewed = s.session
				observation := s.Runtime
				m.reviewedObservation = &observation
				m.nativeReview = ""
				var summary string
				n, c, _ := m.serviceTask(func() error {
					if s.Mode == "local" && s.CLI == "codex" {
						summary = m.sessionNativeSummary(s.session)
					}
					return nil
				}, func(n *harborModel, err error) tea.Cmd {
					n.nativeReview = summary
					n.action, n.screen = "review-reference", "confirm"
					n.referenceAccepted, n.scroll, n.notice = false, 0, ""
					n.uiFocus = "review:esc"
					n.reviewOpenedAt = time.Now()
					return nil
				})
				return n, c
			case "d":
				m.reviewed = s.session
				observation := s.Runtime
				m.reviewedObservation = &observation
				m.screen = "delete-history"
				m.scroll = 0
				m.notice = ""
				m.input.SetValue("")
				m.input.Focus()
				return m, nil
			case "c":
				m.screen = "busy"
				return m, func() tea.Msg { pane, err := m.service.PreviousPane(s.session); return appPreviousPane{pane, err} }
			case "i":
				m.screen = "detail"
				return m, func() tea.Msg {
					route, err := m.service.ObservedRoute(s.ID)
					var window codexWindowObservation
					if reader, ok := m.service.(interface {
						ObservedCodexWindow(session) codexWindowObservation
					}); ok && !s.External {
						window = reader.ObservedCodexWindow(s.session)
					}
					return appRoute{s.ID, route, err, window}
				}
			case "e":
				m.reviewed = s.session
				observation := s.Runtime
				m.reviewedObservation = &observation
				m.screen = "rename"
				m.input.SetValue(s.Name)
				m.input.CursorEnd()
				m.input.Focus()
			case "s", "x":
				m.reviewed = s.session
				observation := s.Runtime
				m.reviewedObservation = &observation
				m.action = "stop"
				if key == "x" {
					m.action = "remove"
				}
				m.scroll = 0
				m.screen = "confirm"
			case "enter":
				return m.inspectEnter(s, false)

			}
		}
	}
	return m, nil
}
func runHarborApp(newSession bool) error {
	m := newHarborModel(appService)
	if newSession {
		m.newOnOpen = true
		m.screen = "new-project-loading"
	}
	defer m.cancelRefresh()
	_, err := tea.NewProgram(m, tea.WithAltScreen(), tea.WithMouseAllMotion()).Run()
	return err
}

func runMagpieApp() error {
	m := newMagpieModel(appService)
	_, err := tea.NewProgram(m, tea.WithAltScreen(), tea.WithMouseAllMotion()).Run()
	return err
}
