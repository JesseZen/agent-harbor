package main

import (
	"context"
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
type sessionDraft struct {
	Directory, Name, Connection, Instance, Model, Client string
	Profile                                              *profileBinding
	RecoveryChoice                                       string
	CodexConnection                                      string
}
type sessionApproval struct {
	Observation              *runtimeObservation
	Session                  session
	MagpieSelection          *codexSelection
	DevelopmentQuery         bool
	QueryUncertaintyAccepted bool
	SavedReferenceAccepted   bool
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
	EnsureProfiles() (profile, error)
	SelectProfile(profileBinding) error
	SetProfileDefaultInstance(profileBinding, string) error
	ReviewSessionProfile(sessionApproval, profileBinding) error
	ProfileEnvironment(profileBinding) (appEnvironment, error)
	ProfileCatalog(profileBinding, string) ([]catalogModel, error)
	Profiles() ([]profile, error)
	CreateProfile(profileCreateRequest) (profile, error)
	RenameProfile(profileBinding, string) error
	ResolveProfileClient(profileClientRequest) (profileClientEnvironment, error)
	ProfileSessions(profileClientRequest) ([]session, error)
	WithProfileClient(context.Context, profileClientRequest, profileClientOperation, func(profileClientScope) error) error
	ValidateProfileResult(profileBinding, profileBinding) error
	Reassociate(sessionApproval, herdrIdentity, string) error
	PreviousPane(session) (previousPane, error)
	ClosePreviousPane(previousPane) error
	ManagementContext() (string, error)
	AuxiliaryAcknowledged(string) (bool, error)
	AcknowledgeAuxiliary(string) error
	InstanceReports() ([]instanceReport, error)
	Magpie(string, string) error
	Environment() (appEnvironment, error)
	NativeCodexConfig() string
	CodexSelection(context.Context, session) (codexSelection, error)
	CompleteDirectory(string) ([]string, error)
	ValidateDirectory(string) error
	Open(sessionApproval) error
	List() ([]sessionView, error)
	Get(string) (sessionView, error)
	GetContext(context.Context, string) (sessionView, error)
	Draft(sessionDraft) (sessionDraft, error)
	Create(sessionDraft) (session, error)
	Start(sessionApproval) error
	Resume(sessionApproval) error
	Stop(sessionApproval) error
	Rename(string, string) error
	Remove(sessionApproval) error
	DeleteHistory(sessionApproval, string) error
	Focus(string) error
	FocusObserved(sessionView) error
	RenameObserved(sessionView, string) error
	ObservedRoute(string) (observedRoute, error)
	Instances() ([]magpieInstance, error)
	Catalog(string) ([]catalogModel, error)
	Bind(string, string) error
	Verify(session, string, string) error
}

type harborService struct{}

var appService applicationService = harborService{}

func deriveSession(s session) sessionView {
	v := sessionView{session: s, WriterLockCheck: "not inspected; explicitly review external writer uncertainty"}
	if s.Conversation == nil && (s.Started || s.CodexSessionID != "" || len(s.Reassociations) > 0) {
		if s.CodexSessionID == "" && s.RunToken != "" && processGroupActive(s.ProcessGroup) {
			v.Reason = "Conversation identity pending; approve the client identity hook if prompted."
		} else {
			v.Reason = missingConversationRecovery
		}
		return v
	}
	inspect := s
	inspect.ProcessGroup = 0
	kind := newConversation
	if s.Started {
		kind = resumeConversation
	}
	if s.Started && s.Mode == "magpie" && s.CLI == "codex" && !joinedCodex(s) {
		if selection, err := readCodexSelection(context.Background(), s); err == nil {
			inspect.MagpieModel = selection.Model
		}
	}
	if err := checkSession(inspect, kind, ""); err != nil {
		v.Reason = err.Error()
	}
	if v.Reason == "" && s.Started && s.Recovery != "ready" {
		v.Reason = "session identity and directory need verification"
	}
	return v
}

func (h harborService) List() ([]sessionView, error) { return h.list(context.Background(), false) }
func (h harborService) ListContext(ctx context.Context) ([]sessionView, error) {
	return h.list(ctx, true)
}
func (harborService) list(ctx context.Context, backgroundHealth bool) ([]sessionView, error) {
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
		v := sessionView{session: s}
		if backgroundHealth {
			v = cachedSessionHealth(s)
		} else {
			v = deriveSession(s)
		}
		if !backgroundHealth {
			v.Health = "not_checked"
		}
		if v.Reason != "" {
			v.Health = "needs_review"
		}
		views = append(views, v)
	}
	views, discoveryErr := mergeHerdrAgents(ctx, views)
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
func (h harborService) Get(id string) (sessionView, error) {
	return h.get(context.Background(), id, false)
}
func (h harborService) GetContext(ctx context.Context, id string) (sessionView, error) {
	return h.get(ctx, id, true)
}
func (h harborService) get(ctx context.Context, id string, cached bool) (sessionView, error) {
	views, err := h.list(ctx, cached)
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
	if d.Profile != nil {
		if d.Client == "" {
			d.Client = d.Profile.Client
		}
		if d.Client != d.Profile.Client {
			return d, ErrProfileIdentity
		}
		if _, err := h.ResolveProfileClient(profileClientRequest{Binding: *d.Profile}); err != nil {
			return d, err
		}
		if d.Connection == "magpie" {
			form, err := h.ProfileCodexConnection(context.Background(), profileClientRequest{Binding: *d.Profile})
			if err != nil {
				return d, err
			}
			d.CodexConnection = form
		}
	}
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
		if d.Instance == "" && d.Profile != nil {
			p, _, err := resolveProfileRecord(*d.Profile)
			if err != nil {
				return d, err
			}
			d.Instance = p.DefaultInstanceID
			if d.Instance == "" {
				r, err := loadMagpieRegistry()
				if err != nil {
					return d, err
				}
				d.Instance = r.Default
			}
		}
		var models []catalogModel
		var err error
		if d.Profile != nil {
			models, err = h.ProfileMagpieCatalog(context.Background(), profileClientRequest{Binding: *d.Profile}, d.Instance)
		} else {
			models, err = h.Catalog(d.Instance)
		}
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
	if d.Client != "" && d.Client != "codex" && d.Connection == "magpie" {
		return session{}, errors.New("additional client Magpie isolation unproven; choose native configuration")
	}
	if d.Profile == nil {
		p, err := h.EnsureProfiles()
		if err != nil {
			return session{}, err
		}
		b, err := bindingOf(p, d.Client)
		if d.Client == "" {
			b, err = bindingOf(p, "codex")
		}
		if err != nil {
			return session{}, err
		}
		d.Profile = &b
	}
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
	if err := validateApprovalRuntime(a); err != nil {
		return err
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
	if err := validateApprovalRuntime(a); err != nil {
		return err
	}
	if a.DevelopmentQuery || a.Session.Conversation != nil && a.Session.Conversation.Provenance == "reviewed-compatibility" {
		return launchQuery(a)
	}
	return launchReviewed(a.Session, resumeConversation, a.OverrideReason)
}
func (h harborService) Rename(id, name string) error {
	v, _, err := currentRuntime(id)
	if err != nil {
		return err
	}
	return h.RenameObserved(v, name)
}
func (h harborService) RenameObserved(expected sessionView, name string) error {
	if err := validateApprovalRuntime(sessionApproval{Session: expected.session, Observation: &expected.Runtime}); err != nil {
		return err
	}
	if err := renameSessionReviewed(expected.session, name); err != nil {
		return err
	}
	if expected.Runtime.Presence != "observed" || (!expected.Runtime.Associated && !expected.Runtime.RunVerified) {
		return nil
	}
	if expected.Runtime.RunVerified && conversationAssociationWarning(expected.session, expected.Runtime.Reference) != "" {
		return nil
	}
	// The public agent target accepts pane IDs; the terminal identity was checked above.
	var result map[string]any
	if err := agentRPC(expected.HerdrSocket, "agent.rename", map[string]any{"target": expected.HerdrPane, "name": agentName(name, expected.ID)}, &result); err != nil {
		return err
	}
	if err := agentRPC(expected.HerdrSocket, "pane.rename", map[string]any{"pane_id": expected.HerdrPane, "label": strings.TrimSpace(name)}, &result); err != nil {
		return err
	}
	return agentRPC(expected.HerdrSocket, "tab.rename", map[string]any{"tab_id": expected.HerdrTab, "label": name}, &result)
}

func (harborService) Remove(a sessionApproval) error {
	if !a.Confirmed {
		return errors.New("removal cancelled")
	}
	if err := validateApprovalRuntime(a); err != nil {
		return err
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
	if err := validateApprovalRuntime(a); err != nil {
		return err
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
	if facts.Variables["AH_SESSION_RUN_TOKEN"] != s.RunToken || !profileProcessMatches(s, facts) {
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
		current, err := getSession(s.ID)
		if err != nil {
			return err
		}
		h := harborService{}
		review, err := h.PreviousPane(current)
		if err != nil {
			if closeErr := closeFinishedRunner(current); closeErr == nil {
				return nil
			}
			return fmt.Errorf("client stopped; pane retained for review: %w", err)
		}
		return h.ClosePreviousPane(review)
	}
	return nil
}
func (h harborService) Focus(id string) error {
	v, _, err := currentRuntime(id)
	if err != nil {
		return err
	}
	return h.FocusObserved(v)
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
