package main

import (
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"syscall"
	"time"
)

type sessionView struct {
	Runtime          runtimeObservation `json:"runtime"`
	Health           string             `json:"health"`
	External         bool               `json:"external,omitempty"`
	DiscoveryWarning string             `json:"discovery_warning,omitempty"`
	session
	Running         bool   `json:"running"`
	WriterLockCheck string `json:"writer_lock_check"`
	Status          string `json:"status"`
	Reason          string `json:"reason,omitempty"`
}
type sessionDraft struct{ Directory, Name, Connection, Instance, Model, Client string }
type sessionApproval struct {
	Session                  session
	DevelopmentQuery         bool
	QueryUncertaintyAccepted bool
	Confirmed                bool
	OverrideReason           string
}
type observedRoute struct {
	Observed  bool   `json:"observed"`
	Main      any    `json:"main,omitempty"`
	Auxiliary []any  `json:"auxiliary,omitempty"`
	Reason    string `json:"reason,omitempty"`
}

// The CLI and Harbor app share this boundary. Approval carries the exact record
// reviewed by the user; changes invalidate it at the atomic launch boundary.
type applicationService interface {
	Reassociate(sessionApproval, herdrIdentity, string) error
	PreviousPane(session) (previousPane, error)
	ClosePreviousPane(previousPane) error
	ManagementContext() (string, error)
	AuxiliaryAcknowledged(string) (bool, error)
	AcknowledgeAuxiliary(string) error
	InstanceReports() ([]instanceReport, error)
	Magpie(string, string) error
	Environment() (appEnvironment, error)
	CompleteDirectory(string) ([]string, error)
	ValidateDirectory(string) error
	Open(sessionApproval) error
	List() ([]sessionView, error)
	Get(string) (sessionView, error)
	Draft(sessionDraft) (sessionDraft, error)
	Create(sessionDraft) (session, error)
	Start(sessionApproval) error
	Resume(sessionApproval) error
	Stop(sessionApproval) error
	Rename(string, string) error
	Remove(sessionApproval) error
	DeleteHistory(sessionApproval, string) error
	Focus(string) error
	ObservedRoute(string) (observedRoute, error)
	Instances() ([]magpieInstance, error)
	Catalog(string) ([]catalogModel, error)
	Bind(string, string) error
	Verify(session, string, string) error
}

type harborService struct{}

var appService applicationService = harborService{}

func deriveSession(s session) sessionView {
	v := sessionView{session: s, WriterLockCheck: "unavailable for this recorded CLI version"}
	v.WriterLockCheck = "not inspected; explicitly review external writer uncertainty"
	if !processGroupActive(s.ProcessGroup) && s.Conversation == nil && (s.Started || s.CodexSessionID != "" || len(s.Reassociations) > 0) {
		v.Status = "Needs attention"
		v.Reason = "Legacy conversation needs review: unassociated; inspect Herdr identity and use reassociate SESSION_ID REFERENCE_JSON REVIEW_REASON, then explicitly approve query recovery."
		return v
	}
	if processGroupActive(s.ProcessGroup) {
		v.Running = true
		v.Status = "Running"
		// Inspect connection/directory health without treating this session's
		// own live group as an attempted concurrent resume.
		inspect := s
		inspect.ProcessGroup = 0
		if err := checkSession(inspect, newConversation, ""); err != nil {
			v.Status = "Needs attention"
			v.Reason = err.Error()
		}
		return v
	}
	if sessionLaunchPending(s) {
		v.Status = "Starting"
		return v
	}
	kind := newConversation
	if s.Started || s.CodexSessionID != "" {
		kind = resumeConversation
	}
	if err := checkSession(s, kind, ""); err != nil {
		v.Status = "Needs attention"
		v.Reason = err.Error()
		return v
	}
	if s.Started && s.Recovery != "ready" {
		v.Status = "Needs attention"
		v.Reason = "session identity and directory need verification"
		return v
	}
	if !s.Started {
		v.Status = "Not started"
	} else if s.LastStartedAt == nil && s.LastExitAt == nil && s.RunToken != "" {
		v.Status = "Starting"
	} else {
		v.Status = "Exited"
	}
	return v
}
func (harborService) List() ([]sessionView, error) {
	var all []session
	err := withSessions(func(records []session) ([]session, error) { all = append([]session{}, records...); return nil, nil })
	if err != nil {
		return nil, err
	}
	sort.SliceStable(all, func(i, j int) bool {
		a, b := all[i].CreatedAt, all[j].CreatedAt
		if all[i].LastActivityAt != nil {
			a = *all[i].LastActivityAt
		}
		if all[j].LastActivityAt != nil {
			b = *all[j].LastActivityAt
		}
		return a.After(b)
	})
	views := make([]sessionView, 0, len(all))
	for _, s := range all {
		v := deriveSession(s)
		v.Health = "not_checked"
		if v.Reason != "" {
			v.Health = "needs_review"
		}
		views = append(views, v)
	}
	views, discoveryErr := mergeHerdrAgents(views)
	if discoveryErr != nil {
		if len(views) == 0 {
			return views, discoveryErr
		}
		for i := range views {
			views[i].DiscoveryWarning = discoveryErr.Error()
		}
	}
	return views, nil
}
func (harborService) Get(id string) (sessionView, error) {
	views, err := (harborService{}).List()
	if err != nil {
		return sessionView{}, err
	}
	for _, v := range views {
		if v.ID == id {
			return v, nil
		}
	}
	return sessionView{}, errors.New("session no longer available; refresh")
}
func (h harborService) Draft(d sessionDraft) (sessionDraft, error) {
	if d.Client == "" {
		d.Client = "codex"
	}
	if d.Client != "codex" {

		if d.Connection != "local" {
			return d, errors.New("additional client Magpie isolation unproven; choose native configuration")
		}
		if _, err := nativeClientProgram(d.Client); err != nil {
			return d, err
		}
	}
	if d.Name != "" {
		if err := validateSessionName(d.Name); err != nil {
			return d, err
		}
	}
	dir, err := checkDirectory(d.Directory)
	if err != nil {
		return d, err
	}
	d.Directory = dir
	if d.Connection != "local" && d.Connection != "magpie" {
		return d, errors.New("choose local or magpie")
	}
	if d.Connection == "magpie" {
		models, err := h.Catalog(d.Instance)
		if err != nil {
			return d, err
		}
		found := false
		for _, m := range models {
			if m.ID == d.Model {
				found = true
			}
		}
		if !found {
			return d, errors.New("selected Magpie model or routing group is unavailable")
		}
	}
	return d, nil
}
func (h harborService) Create(d sessionDraft) (session, error) {
	d, err := h.Draft(d)
	if err != nil {
		return session{}, err
	}
	address := ""
	var selected *magpieInstance
	if d.Connection == "magpie" {
		registry, err := loadMagpieRegistry()
		if err != nil {
			return session{}, err
		}
		instance, ok := registry.find(d.Instance)
		if !ok {
			return session{}, errors.New("Magpie instance not found")
		}
		address = instance.Address
		selected = &instance
	}
	return createSessionIntent(d, address, selected)
}
func (harborService) Start(a sessionApproval) error {
	if !a.Confirmed {
		return errors.New("start cancelled")
	}
	if a.Session.CodexSessionID != "" || a.Session.Conversation != nil || len(a.Session.Reassociations) > 0 {
		return errors.New("existing conversation intent; use reassociate to review legacy identity, then resume")
	}
	return launchReviewed(a.Session, newConversation, a.OverrideReason)
}
func (harborService) Resume(a sessionApproval) error {
	if !a.Confirmed {
		return errors.New("recovery cancelled")
	}
	if a.DevelopmentQuery || a.Session.Conversation != nil && a.Session.Conversation.Provenance == "reviewed-compatibility" {
		return launchQuery(a)
	}
	return launchReviewed(a.Session, resumeConversation, a.OverrideReason)
}
func (harborService) Rename(id, name string) error {
	if err := renameSession(id, name); err != nil {
		return err
	}
	s, err := getSession(id)
	if err != nil {
		return err
	}
	if s.HerdrTab != "" {
		if err := sessionHerdr(s, "tab", "rename", s.HerdrTab, name); err != nil {
			return fmt.Errorf("name saved; Herdr tab was not renamed: %w", err)
		}
	}
	if s.HerdrPane != "" && processGroupActive(s.ProcessGroup) {
		if err := sessionHerdr(s, "agent", "rename", s.HerdrPane, s.HerdrAgentName); err != nil {
			return fmt.Errorf("name saved; Herdr agent name could not be updated: %w", err)
		}
	}
	return nil
}
func (harborService) Remove(a sessionApproval) error {
	if !a.Confirmed {
		return errors.New("removal cancelled")
	}
	return removeSession(a.Session)
}
func (harborService) Bind(id, conversation string) error     { return bindSession(id, conversation) }
func (harborService) Verify(s session, id, dir string) error { return verifySession(s, id, dir) }
func (harborService) Instances() ([]magpieInstance, error) {
	r, err := loadMagpieRegistry()
	return r.Instances, err
}
func (harborService) Catalog(id string) ([]catalogModel, error) {
	if strings.HasPrefix(id, "http://") {
		return fetchCatalog(id)
	}
	r, err := loadMagpieRegistry()
	if err != nil {
		return nil, err
	}
	instance, ok := r.find(id)
	if !ok {
		return nil, errors.New("Magpie instance not found")
	}
	return fetchCatalog(instance.Address)
}
func (harborService) Stop(a sessionApproval) error {
	if !a.Confirmed {
		return errors.New("stop cancelled; the conversation stays resumable")
	}
	s, err := getSession(a.Session.ID)
	if err != nil {
		return err
	}
	if !sameSession(s, a.Session) {
		return errors.New("session changed; review it again")
	}
	if s.ProcessGroup <= 1 || !processGroupActive(s.ProcessGroup) {
		return errors.New("session is not running")
	}
	facts, err := readProcessFacts(s.ProcessGroup)
	if err != nil {
		return errors.New("session process evidence unavailable; nothing was stopped")
	}
	if s.RunToken == "" {
		return errors.New("session has no start token; nothing was stopped")
	}
	if facts.Variables["AH_SESSION_RUN_TOKEN"] == "" {
		return errors.New("session process has no start token; nothing was stopped")
	}
	if facts.Variables["AH_SESSION_RUN_TOKEN"] != s.RunToken {
		return errors.New("cannot verify ownership of the session process group; nothing was stopped")
	}
	if err := syscall.Kill(-s.ProcessGroup, syscall.SIGTERM); err != nil {
		return err
	}
	deadline := time.Now().Add(5 * time.Second)
	for processGroupActive(s.ProcessGroup) && time.Now().Before(deadline) {
		time.Sleep(25 * time.Millisecond)
	}
	if processGroupActive(s.ProcessGroup) {
		return errors.New("session is still closing; no unrelated process was signalled")
	}
	if s.HerdrPane != "" {
		// The runner still owns final identity observation after the client exits.
		// Closing its terminal here would kill that owner before it records exit.
		deadline := time.Now().Add(12 * time.Second)
		for {
			current, err := getSession(s.ID)
			if err != nil {
				return err
			}
			if current.RunToken != s.RunToken {
				return errors.New("session changed while stopping; review the pane before closing it")
			}
			if current.ProcessGroup == 0 {
				break
			}
			if time.Now().After(deadline) {
				return errors.New("client exited but final session observation is pending; inspect the session before closing its pane")
			}
			time.Sleep(25 * time.Millisecond)
		}
		return sessionHerdr(s, "pane", "close", s.HerdrPane)
	}
	return nil
}
func (harborService) Focus(id string) error {
	if strings.HasPrefix(id, "external:") {
		v, err := (harborService{}).Get(id)
		if err != nil {
			return err
		}
		if socketIdentity(v.HerdrSocket) != v.HerdrSocketIdentity {
			return errors.New("workspace changed; refresh")
		}
		var result map[string]any
		return agentRPC(v.HerdrSocket, "agent.focus", map[string]any{"target": v.HerdrPane}, &result)
	}
	s, err := getSession(id)
	if err != nil {
		return err
	}
	if !processGroupActive(s.ProcessGroup) {
		return errors.New("session is not running; choose resume instead")
	}
	if s.HerdrTab == "" {
		return errors.New("this session has no recorded Herdr tab")
	}
	return sessionHerdr(s, "tab", "focus", s.HerdrTab)
}

// Commands require the managed workspace paths, never an arbitrary ambient
// HERDR_SOCKET_PATH that may identify the user's separate workspace.
func sessionHerdr(s session, args ...string) error {
	root, err := bridgeHome()
	if err != nil {
		return err
	}
	if !managedSocket(root, s.HerdrSocket) || s.HerdrSocketIdentity == "" || socketIdentity(s.HerdrSocket) != s.HerdrSocketIdentity {
		return errors.New("Herdr workspace identity changed; no tab or pane was targeted")
	}
	if err != nil {
		return err
	}
	if err := ensureHome(root); err != nil {
		return err
	}
	env, _, err := managedEnvironment(root)
	if err != nil {
		return err
	}
	running, err := herdrRunning(env)
	if err != nil {
		return err
	}
	if !running {
		return errors.New("managed Herdr workspace is not running")
	}
	cmd := herdrCommand(env, append([]string{"--session", herdrSession}, args...)...)
	if err := cmd.Run(); err != nil {
		return errors.New("managed Herdr command failed; check the pane and retry")
	}
	return nil
}

func socketIdentity(path string) string {
	info, err := os.Lstat(path)
	if err != nil || info.Mode()&os.ModeSocket == 0 {
		return ""
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return ""
	}
	return fmt.Sprintf("%d:%d", stat.Dev, stat.Ino)
}
