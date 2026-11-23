package main

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
)

type appEnvironment struct {
	Profile                             *profileBinding
	DefaultInstance, LastConnection     string
	LastMagpieInstance, LastMagpieModel string
	CodexVersion, LocalHome, Directory  string
	NativeConfig                        string
	Projects                            []string
	Clients                             []clientCapability
	ClientDiscovery                     string
	CodexOptions                        codexOptions
}

func (h harborService) Environment() (appEnvironment, error) {
	selected, err := h.EnsureProfiles()
	if err != nil {
		return appEnvironment{}, err
	}
	binding, err := bindingOf(selected, "codex")
	if err != nil {
		return appEnvironment{}, err
	}
	return h.ProfileEnvironment(binding)
}

func (h harborService) ProfileEnvironment(binding profileBinding) (appEnvironment, error) {
	cwd, _ := os.Getwd()
	if launch := os.Getenv("AGENT_HARBOR_BRIDGE_LAUNCH_CWD"); launch != "" {
		cwd = launch
	}
	if active := os.Getenv("HERDR_ACTIVE_PANE_CWD"); active != "" && filepath.Clean(active) != filepath.Clean(os.Getenv("HOME")) {
		cwd = active
	}
	selected, client, err := resolveProfileRecord(binding)
	if err != nil {
		return appEnvironment{}, err
	}
	clientEnv, err := h.ResolveProfileClient(profileClientRequest{Binding: binding})
	if err != nil {
		return appEnvironment{}, err
	}
	result := appEnvironment{Profile: &binding, CodexVersion: codexVersion(clientEnv.Environment), LocalHome: clientEnv.Home, Directory: cwd}
	if binding.Client == "codex" && client.LastCodexOptions != nil {
		result.CodexOptions = *client.LastCodexOptions
	}
	result.NativeConfig = nativeCodexConfigSummary(clientEnv.Environment)
	result.Clients, result.ClientDiscovery = discoverClientSubset()
	registry, err := loadMagpieRegistry()
	if err != nil {
		return result, err
	}
	result.DefaultInstance = registry.Default
	if selected.DefaultInstanceID != "" {
		result.DefaultInstance = selected.DefaultInstanceID
	}
	var sessions []session
	err = withSessions(func(all []session) ([]session, error) { sessions = append([]session{}, all...); return nil, nil })
	if err != nil {
		return result, err
	}
	seen := map[string]bool{}
	var latest session
	var latestMagpie session
	for _, s := range sessions {
		if s.Profile != nil && *s.Profile == binding && (s.Mode == "local" || s.Mode == "magpie") && (latest.Mode == "" || !s.CreatedAt.Before(latest.CreatedAt)) {
			latest = s
		}
		if s.Profile != nil && *s.Profile == binding && s.Mode == "magpie" && (latestMagpie.Mode == "" || !s.CreatedAt.Before(latestMagpie.CreatedAt)) {
			latestMagpie = s
		}
		if !seen[s.WorkingDirectory] {
			result.Projects = append(result.Projects, s.WorkingDirectory)
			seen[s.WorkingDirectory] = true
		}
	}
	result.LastConnection = latest.Mode
	result.LastMagpieInstance = latestMagpie.InstanceID
	result.LastMagpieModel = latestMagpie.MagpieModel
	if client.LastMagpieInstanceID != "" && client.LastMagpieModelID != "" {
		result.LastMagpieInstance = client.LastMagpieInstanceID
		result.LastMagpieModel = client.LastMagpieModelID
	}
	return result, nil
}
func (harborService) CompleteDirectory(path string) ([]string, error) {
	matches, err := filepath.Glob(path + "*")
	out := []string{}
	for _, match := range matches {
		if info, e := os.Stat(match); e == nil && info.IsDir() {
			out = append(out, match+string(os.PathSeparator))
		}
	}
	sort.Strings(out)
	return out, err
}
func (harborService) ValidateDirectory(path string) error {
	resolved, err := checkDirectory(path)
	if err != nil {
		return err
	}
	directory, err := os.Open(resolved)
	if err != nil {
		return errors.New("working directory is unreadable; choose another directory or fix its permissions")
	}
	defer directory.Close()
	if _, err = directory.Readdirnames(1); err != nil && !errors.Is(err, io.EOF) {
		return errors.New("working directory is unreadable; choose another directory or fix its permissions")
	}
	return nil
}

type recoveryOpenError struct {
	err      error
	approval sessionApproval
}

func (e *recoveryOpenError) Error() string { return e.err.Error() }
func (e *recoveryOpenError) Unwrap() error { return e.err }

func (h harborService) Open(a sessionApproval) (err error) {
	reviewed := false
	defer func() {
		if err != nil && reviewed {
			err = &recoveryOpenError{err: err, approval: a}
		}
	}()

	if !a.Confirmed {
		return errors.New("launch cancelled")
	}
	if a.DevelopmentQuery && !a.QueryUncertaintyAccepted {
		return errors.New("query proves syntax only; explicitly accept conversation, login and external writer uncertainty")
	}
	if a.Session.Profile == nil {
		return errors.New("legacy session requires explicit review of its original Profile environment before launch")
	}
	if a.Recovery != nil {
		updated, err := h.approveEnterRecovery(a)
		if err != nil {
			return err
		}
		reviewed = true
		a.Session = updated
		if a.Observation.Presence == "confirmed_exit" && updated.Ended != nil && updated.Ended.Source == idleRecoverySource {
			observation := *a.Observation
			observation.Presence = "not_observed"
			a.Observation = &observation
		}
	} else if err := validateApprovalRuntime(a); err != nil {
		return err
	}
	if err := waitForLaunchState(a.Session); err != nil {
		return err
	}
	if a.DevelopmentQuery && a.SavedReferenceAccepted && queryReferenceCheck(a.Session) != nil {
		ref, checkErr := savedReferenceForReview(a.Session)
		if checkErr != nil {
			return checkErr
		}
		updated, reviewErr := reassociateRecord(a, ref, "Confirmed the saved conversation and connection in the recovery action")
		if reviewErr != nil {
			return reviewErr
		}
		a.Session = updated
		reviewed = true
		if checkErr := validateApprovalRuntime(a); checkErr != nil {
			return checkErr
		}
		if checkErr := waitForLaunchState(a.Session); checkErr != nil {
			return checkErr
		}
	}
	if a.DevelopmentQuery {
		if !a.QueryUncertaintyAccepted {
			return errors.New("query proves syntax only; explicitly accept conversation, login and external writer uncertainty")
		}
		plan, err := queryResume(a.Session)
		if err != nil {
			return err
		}
		if a.Recovery != nil {
			if err := checkEnterRecoveryPlan(a.Session, plan); err != nil {
				return err
			}
		}
		if a.Session.Mode == "magpie" && a.Session.CLI == "codex" {
			if err := verifyCodexSelection(context.Background(), a.Session, a.MagpieSelection); err != nil {
				return err
			}
		}
		if err := checkSessionSelection(a.Session, resumeConversation, a.OverrideReason, a.MagpieSelection); err != nil {
			return err
		}
		if a.Recovery != nil {
			return openPaneOptions(a.Session, launchKind("query"), a.OverrideReason, a.MagpieSelection, a.Recovery)
		}
		return openQueryPane(a.Session, a.OverrideReason, a.MagpieSelection)
	}
	kind := newConversation
	if a.Session.Started {
		kind = resumeConversation
	}
	if kind == resumeConversation && a.Session.Mode == "magpie" && a.Session.CLI == "codex" {
		if err := verifyCodexSelection(context.Background(), a.Session, a.MagpieSelection); err != nil {
			return err
		}
	}
	if err := checkSessionSelection(a.Session, kind, a.OverrideReason, a.MagpieSelection); err != nil {
		return err
	}
	// The pane rechecks the reviewed record and all launch checks atomically.
	return openSessionPane(a.Session, kind)
}

// Reports contain no management environment or private web key.
func (h harborService) InstanceReports() ([]instanceReport, error) {
	instances, err := h.Instances()
	if err != nil {
		return nil, err
	}
	reports := []instanceReport{}
	for _, i := range instances {
		p := probeGateway(i.Address)
		health := i.health(p)
		reports = append(reports, instanceReport{magpieInstance: i, Health: health.State, Detail: health.Detail, Warnings: health.Warnings, Probe: p})
	}
	return reports, nil
}
func (harborService) Magpie(action, id string) error {
	switch action {
	case "start":
		return startManagedMagpie(managedStartOptions{})
	case "stop":
		return stopManagedMagpie()
	case "use":
		_, e := useMagpieInstance(id)
		return e
	case "bind":
		p := probeGateway(id)
		_, e := bindExternalMagpie(p)
		return e
	case "manage":
		return magpieManageCommand([]string{"--instance", id})
	case "web":
		if id != managedInstanceID {
			return errors.New("web management is only available for the managed instance")
		}
		return openManagedWebPage()
	}
	return errors.New("unknown Magpie action")
}

func (harborService) AuxiliaryAcknowledged(id string) (bool, error) {
	r, err := loadMagpieRegistry()
	if err != nil {
		return false, err
	}
	i, ok := r.find(id)
	if !ok {
		return false, errors.New("Magpie instance is no longer registered; choose it again")
	}
	return i.AuxiliaryAcknowledged, nil
}
func (harborService) AcknowledgeAuxiliary(id string) error {
	return withMagpieRegistry(func(r *magpieRegistry) error {
		for n := range r.Instances {
			if r.Instances[n].ID == id {
				r.Instances[n].AuxiliaryAcknowledged = true
				return nil
			}
		}
		return errors.New("Magpie instance is no longer registered; choose it again")
	})
}

// The pinned Herdr custom-command API supplies the focused pane before the
// popup opens. Pane IDs are scoped to the exact recorded workspace socket.
func (harborService) ManagementContext() (string, error) {
	pane, socket := os.Getenv("HERDR_ACTIVE_PANE_ID"), os.Getenv("HERDR_SOCKET_PATH")
	if pane == "" {
		return "", nil
	}
	id := ""
	err := withSessions(func(all []session) ([]session, error) {
		for _, s := range all {
			if s.HerdrPane != pane {
				continue
			}
			if s.HerdrSocket != socket || s.HerdrSocketIdentity == "" || socketIdentity(socket) != s.HerdrSocketIdentity {
				return nil, errors.New("Current pane workspace identity changed; explicitly choose a management instance")
			}
			if s.Mode == "magpie" {
				id = s.InstanceID
			}
			return nil, nil
		}
		return nil, nil
	})
	return id, err
}
