package main

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

type appRefreshTick struct{}
type appSessionInspected struct {
	expected, current sessionView
	nativeConfig      string
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
	id    string
	route observedRoute
	err   error
}

// harborModel owns interaction only. All product effects cross the service seam.
type harborModel struct {
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
	directoryChoices    []string
	directoryCursor     int
	scroll              int
	generation          uint64
	listGeneration      uint64
	routeSessionID      string
	service             applicationService
	width, height       int
	sessions            []sessionView
	cursor              int
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
	reviewedSelection   *codexSelection
	selectionReviewErr  error
	selectionFallback   bool
	formActive          bool
	formFocus           int
	formSaved           sessionDraft
	creationConnections map[profileBinding]string
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
func (m harborModel) refreshTimer() tea.Cmd {
	return func() tea.Msg {
		return tea.BatchMsg{tea.Tick(2*time.Second, func(time.Time) tea.Msg { return appRefreshTick{} })}
	}
}
func (m harborModel) visible() []sessionView {
	out := []sessionView{}
	texts := make([]string, len(m.sessions))
	for i, s := range m.sessions {
		texts[i] = s.Name + " " + s.WorkingDirectory + " " + s.MagpieModel
	}
	for _, hit := range searchMatches(m.filter, texts) {
		out = append(out, m.sessions[hit.Index])
	}
	return out
}
func (m harborModel) selected() (sessionView, bool) {
	rows := m.visible()
	if m.selectionMissing || len(rows) == 0 {
		return sessionView{}, false
	}
	return rows[min(m.cursor, len(rows)-1)], true
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
					return m.Update(uiKey("a"))
				}
				m.screen = settings.nav
				if m.screen == "profiles" {
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
			next, cmd := m.Update(msg)
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
			m.cursor = min(m.cursor, max(0, len(m.visible())-1))
		}
		if m.discoveryNotice != "" {
			m.notice = m.discoveryNotice
		} else if m.notice == previousDiscovery {
			m.notice = ""
		}
		if !m.pollScheduled {
			m.pollScheduled = true
			return m, tea.Batch(m.refreshTimer(), accounts)
		}
		return m, accounts
	case appSessionInspected:
		if m.refreshContext.Err() != nil || m.screen != "busy" || m.reviewed.ID != msg.expected.ID {
			return m, nil
		}
		found := false
		for _, row := range m.sessions {
			if row.ID == msg.expected.ID {
				found = true
				if !sameRuntimeTarget(row.Runtime, msg.current.Runtime) || !sameSession(row.session, msg.current.session) {
					msg.err = errors.New("target changed during inspection; refresh and review")
				}
			}
		}
		if !found {
			msg.err = errors.New("target disappeared during inspection; refresh and select again")
		}

		if msg.err != nil || !sameSession(msg.expected.session, msg.current.session) || !sameRuntimeTarget(msg.expected.Runtime, msg.current.Runtime) {
			m.screen = "sessions"
			m.notice = "Target changed or is unavailable; refresh and review again."
			if msg.err != nil {
				m.notice = msg.err.Error()
			}
			return m, nil
		}
		s := msg.current
		if s.Runtime.Presence == "observed" {
			return m, func() tea.Msg { return appOpened{m.service.FocusObserved(s)} }
		}
		m.screen = "sessions"
		if s.Runtime.Presence != "not_started" && s.Runtime.Presence != "historical_exit" && s.Runtime.Presence != "confirmed_exit" {
			m.notice = "Runtime unknown or not confirmed ended; use c to inspect a restored shell, then review recovery."
			return m, nil
		}
		if s.Started && s.Conversation == nil {
			m.notice = missingConversationRecovery
			return m, nil
		}
		m.reviewed = s.session
		m.nativeReview = msg.nativeConfig
		m.reviewedSelection = msg.selection
		m.selectionReviewErr = msg.selectionErr
		m.selectionFallback = false
		observation := s.Runtime
		m.reviewedObservation = &observation
		m.action = "resume"
		if s.Conversation != nil {
			m.action = "query-resume"
		}
		if !s.Started {
			m.action = "start"
		}
		m.scroll = 0
		m.screen = "confirm"
		return m, nil
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
		}
		return m.requestRefresh()
	case appNewInstances:
		if msg.generation != m.generation || m.screen != "new-instance-loading" {
			return m, nil
		}
		if msg.err != nil {
			m.notice = msg.err.Error()
			m.screen = "new-connection"
			return m, nil
		}
		m.newInstances = msg.instances
		m.instanceFilter = ""
		m.instanceCursor = 0
		for i, r := range m.newInstances {
			if r.ID == m.environment.DefaultInstance {
				m.instanceCursor = i
				break
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
				m.screen = "new-form"
				m.formFocus = 3
				return m, nil
			}
			m.screen = "new-instance"
			return m, nil
		}
		m.models = msg.models
		m.catalogCursor = 0
		m.catalogFilter = ""
		m.screen = "new-catalog"
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
		m.draft = sessionDraft{Profile: msg.environment.Profile, Connection: connection, Directory: msg.environment.Directory}
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
		m.formFocus = min(4, msg.focus)
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
			if msg.err != nil {
				m.route = observedRoute{Reason: msg.err.Error()}
			}
		}
		return m, nil
	case tea.KeyMsg:
		key := msg.String()
		if key == "ctrl+c" {
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
			m.screen = "sessions"
			m.notice = ""
			return m, nil
		}
		if m.screen == "confirm" {
			if (m.action == "query-resume" || m.action == "resume") && m.reviewed.Mode == "magpie" && m.reviewed.CLI == "codex" && m.selectionReviewErr != nil && key == "b" {
				if !errors.Is(m.selectionReviewErr, errCodexProfileMissing) {
					m.notice = "Session profile is unavailable or conflicts; resolve it before recovery."
					return m, nil
				}
				id, err := codexReference(m.reviewed)
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
			if (m.action == "query-resume" || m.action == "resume") && m.reviewed.Conversation == nil {
				m.notice = missingConversationRecovery
				return m, nil
			}
			a := sessionApproval{Session: m.reviewed, MagpieSelection: m.reviewedSelection, Observation: m.reviewedObservation, Confirmed: true, DevelopmentQuery: m.action == "query-resume", QueryUncertaintyAccepted: m.action == "query-resume", SavedReferenceAccepted: m.action == "query-resume"}
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
			m.cursor = min(m.cursor+1, max(0, len(m.visible())-1))
			m.route = observedRoute{}
		case "k", "up":
			m.cursor = max(0, m.cursor-1)
			m.route = observedRoute{}
		case "/":
			m.screen = "filter"
			m.input.SetValue(m.filter)
			m.input.Focus()
		case ",":
			settings := newSettingsModel()
			settings.width = m.width
			settings.height = m.height
			m.settings = &settings
			return m, nil
		case "m":
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
			m.screen = "profile-list"
			m.notice = ""
			return m, m.loadProfiles()
		case "a":
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
			m.generation++
			generation := m.generation
			m.screen = "new-project-loading"
			return m, func() tea.Msg {
				var env appEnvironment
				var e error
				if m.activeProfile != nil {
					env, e = m.service.ProfileEnvironment(*m.activeProfile)
				} else {
					env, e = m.service.Environment()
				}
				return appNewReady{environment: env, err: e, generation: generation}
			}
		case "r":
			return m.requestRefresh()
		case "enter", "s", "x", "e", "i", "c", "d", "v":
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
				id := s.ID
				return m, func() tea.Msg { r, e := m.service.ObservedRoute(id); return appRoute{id, r, e} }
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
				m.reviewed = s.session
				m.screen = "busy"
				return m, func() tea.Msg {
					v, e := m.service.GetContext(m.refreshContext, s.ID)
					summary := ""
					var selection *codexSelection
					var selectionErr error
					if e == nil && v.Mode == "local" && v.CLI == "codex" && v.Runtime.Presence != "observed" {
						summary = m.sessionNativeSummary(s.session)
					}
					if e == nil && v.Mode == "magpie" && v.CLI == "codex" && !joinedCodex(v.session) && v.Started && v.Runtime.Presence != "observed" {
						value, selectionError := m.service.CodexSelection(m.refreshContext, v.session)
						selectionErr = selectionError
						if selectionError == nil {
							selection = &value
						}
					}
					return appSessionInspected{expected: s, current: v, nativeConfig: summary, selection: selection, selectionErr: selectionErr, err: e}
				}

			}
		}
	}
	return m, nil
}
func runHarborApp() error {
	m := newHarborModel(appService)
	defer m.cancelRefresh()
	_, err := tea.NewProgram(m, tea.WithAltScreen(), tea.WithMouseAllMotion()).Run()
	return err
}

func runMagpieApp() error {
	m := newMagpieModel(appService)
	_, err := tea.NewProgram(m, tea.WithAltScreen(), tea.WithMouseAllMotion()).Run()
	return err
}
