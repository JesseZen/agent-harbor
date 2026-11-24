package main

import (
	"context"
	"errors"
	"os/exec"
)

// UI tests can seed results and inspect calls without starting any process.
// This fake is intentionally independent of disk, HTTP and the production store.
type fakeApplicationService struct {
	ProfileRecords   []profile
	SelectedProfile  *profileBinding
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
	Extensions       map[profileBinding]extensionSnapshot
	SkillSearch      map[profileBinding][]skillCatalogItem
	ExecCommands     []string
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
		if _, err := f.ResolveProfileClient(profileClientRequest{*d.Profile}); err != nil {
			return d, err
		}
	}
	return d, f.call("draft")
}
func (f *fakeApplicationService) Create(d sessionDraft) (session, error) {
	if d.Profile != nil {
		if _, err := f.ResolveProfileClient(profileClientRequest{*d.Profile}); err != nil {
			return session{}, err
		}
	}
	if err := f.call("create"); err != nil {
		return session{}, err
	}
	s := session{Profile: d.Profile, CLI: d.Client, ID: "0123456789abcdef0123456789abcdef", Name: d.Name, WorkingDirectory: d.Directory, Mode: d.Connection, MagpieModel: d.Model, InstanceID: d.Instance}
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
func (f *fakeApplicationService) ViewPreviousPane(p previousPane) error {
	return f.call("view-previous-pane " + p.Session.ID)
}
func (f *fakeApplicationService) ClosePreviousPane(p previousPane) error {
	return f.call("close-previous-pane " + p.Session.ID)
}

func (*fakeApplicationService) RecoveryMetadata(session) (map[string]string, string, error) {
	return nil, "", nil
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

// Profile metadata for UI tests is in memory; no disk/process lookup is used.
func (f *fakeApplicationService) Profiles() ([]profile, error) {
	return append([]profile{}, f.ProfileRecords...), f.call("profiles")
}
func (f *fakeApplicationService) CreateProfile(req profileCreateRequest) (profile, error) {
	if err := f.call("profile-create"); err != nil {
		return profile{}, err
	}
	id, _ := opaqueProfileID()
	envID, _ := opaqueProfileID()
	p := profile{ID: id, WorkspaceID: "11111111111111111111111111111111", Name: req.Name, Kind: req.Kind, Clients: []profileClientHome{{ID: envID, Client: "codex", Home: "/profiles/" + id}}}
	f.ProfileRecords = append(f.ProfileRecords, p)
	return p, nil
}
func (f *fakeApplicationService) EnsureProfiles() (profile, error) {
	if len(f.ProfileRecords) == 0 {
		return profile{}, ErrProfileNotReady
	}
	for _, p := range f.ProfileRecords {
		if f.SelectedProfile != nil && p.ID == f.SelectedProfile.ProfileID {
			return p, nil
		}
	}
	return f.ProfileRecords[0], nil
}
func (f *fakeApplicationService) RenameProfile(b profileBinding, name string) error {
	if err := f.call("profile-rename"); err != nil {
		return err
	}
	for i, p := range f.ProfileRecords {
		if p.ID == b.ProfileID && p.WorkspaceID == b.WorkspaceID {
			f.ProfileRecords[i].Name = name
			return nil
		}
	}
	return ErrProfileNotFound
}
func (f *fakeApplicationService) SelectProfile(b profileBinding) error {
	if _, err := f.ResolveProfileClient(profileClientRequest{b}); err != nil {
		return err
	}
	if err := f.call("profile-select"); err != nil {
		return err
	}
	copy := b
	f.SelectedProfile = &copy
	return nil
}
func (f *fakeApplicationService) ResolveProfileClient(req profileClientRequest) (profileClientEnvironment, error) {
	if f.Err != nil {
		return profileClientEnvironment{}, f.Err
	}
	for _, p := range f.ProfileRecords {
		if p.ID == req.Binding.ProfileID && p.WorkspaceID == req.Binding.WorkspaceID {
			for _, c := range p.Clients {
				if c.ID == req.Binding.EnvironmentID && (c.Client == req.Binding.Client || p.Kind == profileExisting) {
					return profileClientEnvironment{Binding: req.Binding, ProfileName: p.Name, Kind: p.Kind, Home: c.Home, Executable: true}, nil
				}
			}
		}
	}
	return profileClientEnvironment{}, ErrProfileIdentity
}
func (f *fakeApplicationService) ProfileSessions(req profileClientRequest) ([]session, error) {
	if _, err := f.ResolveProfileClient(req); err != nil {
		return nil, err
	}
	result := []session{}
	for _, v := range f.Sessions {
		if v.Profile != nil && *v.Profile == req.Binding {
			result = append(result, v.session)
		}
	}
	return result, nil
}
func (f *fakeApplicationService) WithProfileClient(_ context.Context, req profileClientRequest, _ profileClientOperation, fn func(profileClientScope) error) error {
	e, err := f.ResolveProfileClient(req)
	if err != nil {
		return err
	}
	sessions, err := f.ProfileSessions(req)
	if err != nil {
		return err
	}
	return fn(profileClientScope{Environment: e, Sessions: sessions})
}
func (f *fakeApplicationService) ValidateProfileResult(captured, visible profileBinding) error {
	if captured != visible {
		return ErrProfileIdentity
	}
	_, err := f.ResolveProfileClient(profileClientRequest{captured})
	return err
}
func (f *fakeApplicationService) ProfileEnvironment(b profileBinding) (appEnvironment, error) {
	e, err := f.ResolveProfileClient(profileClientRequest{b})
	if err != nil {
		return appEnvironment{}, err
	}
	base, err := f.Environment()
	base.Profile = &b
	base.LocalHome = e.Home
	for _, p := range f.ProfileRecords {
		if p.ID == b.ProfileID && p.WorkspaceID == b.WorkspaceID {
			for _, client := range p.Clients {
				if client.ID == b.EnvironmentID {
					base.LastMagpieInstance = client.LastMagpieInstanceID
					base.LastMagpieModel = client.LastMagpieModelID
				}
			}
		}
	}
	return base, err
}
func (f *fakeApplicationService) SetProfileDefaultInstance(b profileBinding, id string) error {
	if err := f.call("profile-default"); err != nil {
		return err
	}
	for i, p := range f.ProfileRecords {
		if p.ID == b.ProfileID && p.WorkspaceID == b.WorkspaceID {
			f.ProfileRecords[i].DefaultInstanceID = id
			return nil
		}
	}
	return ErrProfileIdentity
}
func (f *fakeApplicationService) RememberMagpieSelection(b profileBinding, instanceID, modelID string) error {
	if err := f.call("remember-magpie-selection"); err != nil {
		return err
	}
	for i := range f.ProfileRecords {
		if f.ProfileRecords[i].ID != b.ProfileID || f.ProfileRecords[i].WorkspaceID != b.WorkspaceID {
			continue
		}
		for j := range f.ProfileRecords[i].Clients {
			client := &f.ProfileRecords[i].Clients[j]
			if client.ID == b.EnvironmentID {
				client.LastMagpieInstanceID = instanceID
				client.LastMagpieModelID = modelID
				return nil
			}
		}
	}
	return ErrProfileIdentity
}
func (f *fakeApplicationService) ReviewSessionProfile(a sessionApproval, b profileBinding) error {
	if !a.Confirmed {
		return errors.New("review cancelled")
	}
	e, err := f.ResolveProfileClient(profileClientRequest{b})
	if err != nil {
		return err
	}
	if e.Kind != profileExisting {
		return ErrProfileIdentity
	}
	for i, v := range f.Sessions {
		if v.ID == a.Session.ID && sameSession(v.session, a.Session) {
			copy := b
			f.Sessions[i].Profile = &copy
			return f.call("profile-review")
		}
	}
	return ErrProfileIdentity
}
func (f *fakeApplicationService) ProfileCatalog(b profileBinding, id string) ([]catalogModel, error) {
	if _, err := f.ResolveProfileClient(profileClientRequest{b}); err != nil {
		return nil, err
	}
	return f.Catalog(id)
}

func (f *fakeApplicationService) InspectExtensionsLocal(_ context.Context, binding profileBinding) (extensionSnapshot, error) {
	if err := f.call("extensions-local-inspect"); err != nil {
		return extensionSnapshot{}, err
	}
	if snap, ok := f.Extensions[binding]; ok {
		return snap, nil
	}
	return extensionSnapshot{ProfileName: "Default", SkillsCLI: "path"}, nil
}

func (f *fakeApplicationService) InspectCodexSkills(_ context.Context, _ profileBinding) ([]extensionItem, error) {
	return nil, f.call("extensions-codex-skills")
}

func (f *fakeApplicationService) SearchSkills(_ context.Context, binding profileBinding, _ string) ([]skillCatalogItem, error) {
	if err := f.call("skills-search"); err != nil {
		return nil, err
	}
	return append([]skillCatalogItem(nil), f.SkillSearch[binding]...), nil
}

func (f *fakeApplicationService) ToggleExtension(_ context.Context, b profileBinding, item extensionItem, enabled bool) error {
	if err := f.call("extensions-toggle"); err != nil {
		return err
	}
	if f.Extensions != nil {
		if snap, ok := f.Extensions[b]; ok {
			snap.Skills = append([]extensionItem(nil), snap.Skills...)
			for i := range snap.Skills {
				if snap.Skills[i].ID == item.ID {
					snap.Skills[i].Enabled = enabled
				}
			}
			f.Extensions[b] = snap
		}
	}
	return nil
}

func (f *fakeApplicationService) ExtensionExecCommand(_ context.Context, _ profileBinding, action, target string) (*exec.Cmd, error) {
	f.ExecCommands = append(f.ExecCommands, action+":"+target)
	if f.Err != nil {
		return nil, f.Err
	}
	return exec.Command("true"), nil
}
