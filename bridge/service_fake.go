package main

import (
	"context"
	"errors"
)

// UI tests can seed results and inspect calls without starting any process.
// This fake is intentionally independent of disk, HTTP and the production store.
type fakeApplicationService struct {
	ContextInstance  string
	DirectoryMatches []string
	Acknowledged     map[string]bool
	Reports          []instanceReport
	Sessions         []sessionView
	KnownInstances   []magpieInstance
	Models           []catalogModel
	Route            observedRoute
	Selection        *codexSelection
	SelectionErr     error
	LastApproval     *sessionApproval
	Calls            []string
	Err              error
}

func (f *fakeApplicationService) call(action string) error {
	f.Calls = append(f.Calls, action)
	return f.Err
}
func (f *fakeApplicationService) List() ([]sessionView, error) {
	return append([]sessionView{}, f.Sessions...), f.call("list")
}
func (f *fakeApplicationService) Get(id string) (sessionView, error) {
	if err := f.call("get " + id); err != nil {
		return sessionView{}, err
	}
	for _, s := range f.Sessions {
		if s.ID == id {
			return s, nil
		}
	}
	return sessionView{}, errors.New("session not found")
}
func (f *fakeApplicationService) Draft(d sessionDraft) (sessionDraft, error) {
	if d.Profile != nil {
		return d, ErrProfileNotReady
	}
	return d, f.call("draft")
}
func (f *fakeApplicationService) Create(d sessionDraft) (session, error) {
	if d.Profile != nil {
		return session{}, ErrProfileNotReady
	}
	if err := f.call("create"); err != nil {
		return session{}, err
	}
	s := session{ID: "0123456789abcdef0123456789abcdef", Name: d.Name, WorkingDirectory: d.Directory, Mode: d.Connection, MagpieModel: d.Model, InstanceID: d.Instance}
	f.Sessions = append(f.Sessions, sessionView{session: s, Status: "Not started", Runtime: runtimeObservation{Presence: "not_started", Activity: "unknown"}})
	return s, nil
}
func (f *fakeApplicationService) Start(a sessionApproval) error {
	return f.call("start " + a.Session.ID)
}
func (f *fakeApplicationService) Resume(a sessionApproval) error {
	return f.call("resume " + a.Session.ID)
}
func (f *fakeApplicationService) Stop(a sessionApproval) error { return f.call("stop " + a.Session.ID) }
func (f *fakeApplicationService) Rename(id, name string) error {
	if err := f.call("rename " + id); err != nil {
		return err
	}
	for i := range f.Sessions {
		if f.Sessions[i].ID == id {
			f.Sessions[i].Name = name
			return nil
		}
	}
	return errors.New("session not found")
}
func (f *fakeApplicationService) Remove(a sessionApproval) error {
	if err := f.call("remove " + a.Session.ID); err != nil {
		return err
	}
	for i, s := range f.Sessions {
		if s.ID == a.Session.ID {
			f.Sessions = append(f.Sessions[:i], f.Sessions[i+1:]...)
			return nil
		}
	}
	return errors.New("session not found")
}
func (f *fakeApplicationService) DeleteHistory(a sessionApproval, name string) error {
	return f.call("delete-history " + a.Session.ID + " " + name)
}
func (f *fakeApplicationService) Focus(id string) error { return f.call("focus " + id) }
func (f *fakeApplicationService) ObservedRoute(id string) (observedRoute, error) {
	return f.Route, f.call("route " + id)
}
func (f *fakeApplicationService) Instances() ([]magpieInstance, error) {
	return append([]magpieInstance{}, f.KnownInstances...), f.call("instances")
}
func (f *fakeApplicationService) Catalog(id string) ([]catalogModel, error) {
	return append([]catalogModel{}, f.Models...), f.call("catalog " + id)
}
func (f *fakeApplicationService) Bind(id, conversation string) error { return f.call("bind " + id) }
func (f *fakeApplicationService) Verify(s session, id, dir string) error {
	return f.call("verify " + s.ID)
}

var _ applicationService = (*fakeApplicationService)(nil)

func (f *fakeApplicationService) Environment() (appEnvironment, error) {
	return appEnvironment{CodexVersion: "codex-cli 0.167.0", LocalHome: "/home/test/.codex"}, f.call("environment")
}
func (f *fakeApplicationService) NativeCodexConfig() string {
	return "Codex home model test; provider fixture. Project or profile may override these; verify the actual route."
}
func (f *fakeApplicationService) CodexSelection(_ context.Context, s session) (codexSelection, error) {
	if f.SelectionErr != nil {
		return codexSelection{}, f.SelectionErr
	}
	if f.Selection != nil {
		return *f.Selection, nil
	}
	return codexSelection{ThreadID: s.CodexSessionID, Model: s.MagpieModel}, nil
}
func (f *fakeApplicationService) CompleteDirectory(path string) ([]string, error) {
	return f.DirectoryMatches, f.call("complete " + path)
}
func (f *fakeApplicationService) ValidateDirectory(path string) error {
	return f.call("validate " + path)
}
func (f *fakeApplicationService) Open(a sessionApproval) error {
	f.LastApproval = &a
	return f.call("open " + a.Session.ID)
}

func (f *fakeApplicationService) InstanceReports() ([]instanceReport, error) {
	reports := append([]instanceReport{}, f.Reports...)
	if f.Reports == nil {
		for _, i := range f.KnownInstances {
			reports = append(reports, instanceReport{magpieInstance: i, Health: "unknown"})
		}
	}
	return reports, f.call("reports")
}
func (f *fakeApplicationService) Magpie(action, id string) error {
	return f.call("magpie " + action + " " + id)
}

func (f *fakeApplicationService) AuxiliaryAcknowledged(id string) (bool, error) {
	return f.Acknowledged[id], f.call("auxiliary-check " + id)
}
func (f *fakeApplicationService) AcknowledgeAuxiliary(id string) error {
	if err := f.call("auxiliary-ack " + id); err != nil {
		return err
	}
	if f.Acknowledged == nil {
		f.Acknowledged = map[string]bool{}
	}
	f.Acknowledged[id] = true
	return nil
}

func (f *fakeApplicationService) ManagementContext() (string, error) {
	return f.ContextInstance, f.call("management-context")
}

func (f *fakeApplicationService) PreviousPane(s session) (previousPane, error) {
	return previousPane{Session: s, PaneID: s.HerdrPane, Label: s.Name}, f.call("previous-pane " + s.ID)
}
func (f *fakeApplicationService) ClosePreviousPane(p previousPane) error {
	return f.call("close-previous-pane " + p.Session.ID)
}

func (f *fakeApplicationService) Reassociate(a sessionApproval, r herdrIdentity, reason string) error {
	return f.call("reassociate")
}

func (f *fakeApplicationService) FocusObserved(v sessionView) error { return f.Focus(v.ID) }
func (f *fakeApplicationService) RenameObserved(v sessionView, name string) error {
	return f.Rename(v.ID, name)
}

func (f *fakeApplicationService) GetContext(ctx context.Context, id string) (sessionView, error) {
	return f.Get(id)
}

// Profile behavior is opt-in in this UI fake; unseeded operations fail closed.
func (f *fakeApplicationService) Profiles() ([]profile, error) { return nil, ErrProfileNotReady }
func (f *fakeApplicationService) CreateProfile(profileCreateRequest) (profile, error) {
	return profile{}, ErrProfileNotReady
}
func (f *fakeApplicationService) RenameProfile(profileBinding, string) error {
	return ErrProfileNotReady
}
func (f *fakeApplicationService) ResolveProfileClient(profileClientRequest) (profileClientEnvironment, error) {
	return profileClientEnvironment{}, ErrProfileNotReady
}
func (f *fakeApplicationService) ProfileSessions(profileClientRequest) ([]session, error) {
	return nil, ErrProfileNotReady
}
func (f *fakeApplicationService) WithProfileClient(context.Context, profileClientRequest, profileClientOperation, func(profileClientScope) error) error {
	return ErrProfileNotReady
}
func (f *fakeApplicationService) ValidateProfileResult(profileBinding, profileBinding) error {
	return ErrProfileNotReady
}
