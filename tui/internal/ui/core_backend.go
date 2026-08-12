package ui

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/asheshgoplani/agent-deck/internal/backend"
	"github.com/asheshgoplani/agent-deck/internal/session"
	tea "github.com/charmbracelet/bubbletea"
)

func coreBackendInstances(snapshot backend.Snapshot) []*session.Instance {
	instances := make([]*session.Instance, 0, len(snapshot.Sessions))
	for _, source := range snapshot.Sessions {
		instances = append(instances, coreBackendInstance(source))
	}
	return instances
}

// coreAttachExecCmd obtains the one-time grant inside Run and passes it to the
// public Core helper through inherited descriptor 3. The secret never enters a
// Bubble Tea message, model field, argv, environment variable, or cache.
type coreAttachExecCmd struct {
	ctx    context.Context
	source backend.Backend
	ref    backend.SessionRef
	stdin  io.Reader
	stdout io.Writer
	stderr io.Writer
}

func (command *coreAttachExecCmd) Run() error {
	grantReader, grantWriter, err := os.Pipe()
	if err != nil {
		return fmt.Errorf("create attach grant pipe: %w", err)
	}
	defer grantReader.Close()

	invocation, prepareErr := command.source.PrepareAttach(command.ctx, command.ref, grantWriter)
	closeErr := grantWriter.Close()
	if prepareErr != nil {
		return prepareErr
	}
	if closeErr != nil {
		return fmt.Errorf("close attach grant pipe: %w", closeErr)
	}
	if invocation.Executable == "" || invocation.InstanceID == "" || invocation.SessionID != command.ref.ID || !invocation.ExpectedUpdatedAt.Equal(command.ref.ExpectedUpdatedAt) || !filepath.IsAbs(invocation.AdminSocket) || filepath.Clean(invocation.AdminSocket) != invocation.AdminSocket {
		return fmt.Errorf("Core returned invalid attach invocation metadata")
	}

	process := exec.CommandContext(command.ctx, invocation.Executable,
		"attach",
		"--instance-id", invocation.InstanceID,
		"--admin-socket", invocation.AdminSocket,
		"--session-id", invocation.SessionID,
		"--expected-updated-at", invocation.ExpectedUpdatedAt.Format(time.RFC3339Nano),
		"--grant-fd", "3",
	)
	process.ExtraFiles = []*os.File{grantReader}
	process.Stdin = command.stdin
	process.Stdout = command.stdout
	process.Stderr = command.stderr
	if err := process.Run(); err != nil {
		return fmt.Errorf("Core attach helper: %w", err)
	}
	return nil
}

func (command *coreAttachExecCmd) SetStdin(reader io.Reader)  { command.stdin = reader }
func (command *coreAttachExecCmd) SetStdout(writer io.Writer) { command.stdout = writer }
func (command *coreAttachExecCmd) SetStderr(writer io.Writer) { command.stderr = writer }

func coreBackendInstance(source backend.Session) *session.Instance {
	instance := &session.Instance{
		ID:             source.ID,
		Title:          source.Title,
		ProjectPath:    source.ProjectPath,
		GroupPath:      source.GroupPath,
		Tool:           source.Tool,
		Status:         coreBackendDisplayStatus(source),
		CreatedAt:      source.CreatedAt,
		LastAccessedAt: source.LastAccessedAt,
	}
	detectedAt := source.ActivityObservedAt
	if detectedAt.IsZero() {
		detectedAt = source.UpdatedAt
	}
	switch source.NativeProvider {
	case backend.NativeProviderClaude:
		instance.ClaudeSessionID = source.NativeSessionID
		instance.ClaudeDetectedAt = detectedAt
	case backend.NativeProviderCodex:
		instance.CodexSessionID = source.NativeSessionID
		instance.CodexDetectedAt = detectedAt
	}
	return instance
}

func coreBackendDisplayStatus(source backend.Session) session.Status {
	if source.Status != backend.StatusRunning {
		return coreBackendStatus(source.Status)
	}
	switch source.NativeActivity {
	case backend.NativeActivityRunning:
		return session.StatusRunning
	case backend.NativeActivityWaiting:
		return session.StatusWaiting
	case backend.NativeActivityIdle, backend.NativeActivityUnknown:
		return session.StatusIdle
	case backend.NativeActivityFailed:
		return session.StatusError
	default:
		return coreBackendStatus(source.Status)
	}
}

func coreBackendStatus(status backend.Status) session.Status {
	switch status {
	case backend.StatusLaunching:
		return session.StatusStarting
	case backend.StatusRunning:
		return session.StatusRunning
	case backend.StatusEnding:
		return session.StatusWaiting
	case backend.StatusFailed:
		return session.StatusError
	case backend.StatusEnded:
		return session.StatusStopped
	default:
		return session.StatusIdle
	}
}

func (h *Home) runtimeBackend() (backend.Backend, bool) {
	source, ok := h.backend.(backend.Backend)
	return source, ok
}

func (h *Home) rejectCoreLocalAction(action string) bool {
	if h.backend == nil {
		return false
	}
	h.setError(fmt.Errorf("%s is unavailable for Core-owned sessions", action))
	return true
}

func (h *Home) activateCoreSession(inst *session.Instance) tea.Cmd {
	source, ok := h.backendSessions[inst.ID]
	if !ok {
		return func() tea.Msg {
			return statusUpdateMsg{attachedSessionID: inst.ID, err: fmt.Errorf("Core session %q is not in the current snapshot", inst.ID)}
		}
	}
	switch source.Status {
	case backend.StatusCreated:
		return h.restartSessionFresh(inst)
	case backend.StatusFailed, backend.StatusEnded:
		return h.restartSession(inst)
	case backend.StatusLaunching, backend.StatusEnding:
		return func() tea.Msg {
			return statusUpdateMsg{attachedSessionID: inst.ID, err: fmt.Errorf("Core session is currently %s", source.Status)}
		}
	default:
		return h.attachSession(inst)
	}
}

func (h *Home) showCoreNewSessionDialog() {
	paths := make([]string, 0, len(h.instances))
	seen := make(map[string]struct{}, len(h.instances))
	for _, inst := range h.instances {
		if inst != nil && inst.ProjectPath != "" {
			if _, ok := seen[inst.ProjectPath]; !ok {
				seen[inst.ProjectPath] = struct{}{}
				paths = append(paths, inst.ProjectPath)
			}
		}
	}
	sort.Strings(paths)
	h.newDialog.SetPathSuggestions(paths)
	h.newDialog.SetRecentSessions(nil)

	groupPath := ""
	groupName := ""
	defaultPath := "."
	if selected := h.getSelectedSession(); selected != nil {
		groupPath = selected.GroupPath
		defaultPath = selected.ProjectPath
		h.newDialog.SetDefaultTool(selected.Tool)
	}
	if h.cursor >= 0 && h.cursor < len(h.flatItems) {
		item := h.flatItems[h.cursor]
		if item.Type == session.ItemTypeGroup && item.Group != nil {
			groupPath = item.Group.Path
			groupName = item.Group.Name
		}
	}
	if groupPath == "" {
		routeIDs := make([]string, 0, len(h.backendRoutes))
		for id := range h.backendRoutes {
			routeIDs = append(routeIDs, id)
		}
		sort.Strings(routeIDs)
		if len(routeIDs) > 0 {
			route := h.backendRoutes[routeIDs[0]]
			groupPath = route.Name + "/" + route.ID
			groupName = route.Name
		}
	}
	if groupName == "" {
		groupName = groupPath
		if slash := strings.LastIndex(groupName, "/"); slash >= 0 {
			groupName = groupName[:slash]
		}
	}
	profileIDs := make([]string, 0, len(h.backendProfiles))
	for id := range h.backendProfiles {
		profileIDs = append(profileIDs, id)
	}
	sort.Strings(profileIDs)
	profiles := make([]backend.ClientProfile, 0, len(profileIDs))
	for _, id := range profileIDs {
		profiles = append(profiles, h.backendProfiles[id])
	}
	selectedProfile := h.backendProfileForCommand(h.newDialog.GetSelectedCommand())
	if selected := h.getSelectedSession(); selected != nil {
		if source, ok := h.backendSessions[selected.ID]; ok {
			selectedProfile = source.ClientProfileID
		}
	}
	if selectedProfile == "" && len(profileIDs) > 0 {
		selectedProfile = profileIDs[0]
	}
	h.newDialog.ShowCoreInGroup(groupPath, groupName, defaultPath, profiles, selectedProfile)
}

func (h *Home) backendProfileForCommand(command string) string {
	for _, profile := range h.backendProfiles {
		if profile.ID == command || profile.Launcher == command {
			return profile.ID
		}
	}
	return ""
}

func (h *Home) backendRouteForGroup(groupPath string) string {
	if _, ok := h.backendRoutes[groupPath]; ok {
		return groupPath
	}
	if slash := strings.LastIndex(groupPath, "/"); slash >= 0 {
		candidate := groupPath[slash+1:]
		if _, ok := h.backendRoutes[candidate]; ok {
			return candidate
		}
	}
	ids := make([]string, 0, len(h.backendRoutes))
	for id := range h.backendRoutes {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		route := h.backendRoutes[id]
		if route.Name == groupPath || route.Name+"/"+route.ID == groupPath {
			return id
		}
	}
	return ""
}

func (h *Home) quickCreateCoreSession() tea.Cmd {
	groupPath := ""
	projectPath := "."
	command := ""
	if selected := h.getSelectedSession(); selected != nil {
		groupPath = selected.GroupPath
		projectPath = selected.ProjectPath
		if source, ok := h.backendSessions[selected.ID]; ok {
			command = source.ClientProfileID
		}
	}
	if h.cursor >= 0 && h.cursor < len(h.flatItems) {
		item := h.flatItems[h.cursor]
		if item.Type == session.ItemTypeGroup {
			groupPath = item.Path
			for _, inst := range h.instances {
				if inst != nil && (inst.GroupPath == groupPath || strings.HasPrefix(inst.GroupPath, groupPath+"/")) {
					projectPath = inst.ProjectPath
					if source, ok := h.backendSessions[inst.ID]; ok {
						command = source.ClientProfileID
					}
					break
				}
			}
		}
	}
	if command == "" {
		profileIDs := make([]string, 0, len(h.backendProfiles))
		for id := range h.backendProfiles {
			profileIDs = append(profileIDs, id)
		}
		sort.Strings(profileIDs)
		if len(profileIDs) > 0 {
			command = profileIDs[0]
		}
	}
	name := session.GenerateUniqueSessionName(h.instances, groupPath)
	source, ok := h.runtimeBackend()
	if !ok {
		return func() tea.Msg { return sessionCreatedMsg{err: backend.ErrUnsupported} }
	}
	return h.createCoreSession(source, name, projectPath, command, groupPath, "")
}

func (h *Home) backendSessionRef(id string) (backend.SessionRef, error) {
	source, ok := h.backendSessions[id]
	if !ok {
		return backend.SessionRef{}, fmt.Errorf("Core session %q is not in the current snapshot", id)
	}
	return backend.SessionRef{
		ID:                   source.ID,
		ExpectedUpdatedAt:    source.UpdatedAt,
		CredentialGeneration: source.CredentialGeneration,
	}, nil
}

func (h *Home) createCoreSession(source backend.Backend, name, path, command, groupPath, tempID string) tea.Cmd {
	profileID := h.backendProfileForCommand(command)
	if profileID == "" {
		return func() tea.Msg {
			return sessionCreatedMsg{err: fmt.Errorf("Core client profile for %q is unavailable", command), tempID: tempID}
		}
	}
	profile := h.backendProfiles[profileID]
	routeID := profile.DefaultRouteID
	if routeID == "" {
		routeID = h.backendRouteForGroup(groupPath)
	}
	if routeID == "" {
		return func() tea.Msg {
			return sessionCreatedMsg{err: fmt.Errorf("Core route %q is unavailable", groupPath), tempID: tempID}
		}
	}
	if _, ok := h.backendRoutes[routeID]; !ok {
		return func() tea.Msg {
			return sessionCreatedMsg{err: fmt.Errorf("Core route %q is unavailable", routeID), tempID: tempID}
		}
	}
	generation := h.backendGeneration
	profiles := make(map[string]backend.ClientProfile, len(h.backendProfiles))
	for id, profile := range h.backendProfiles {
		profiles[id] = profile
	}
	routes := make(map[string]backend.Route, len(h.backendRoutes))
	for id, route := range h.backendRoutes {
		routes[id] = route
	}
	return func() tea.Msg {
		created, err := source.CreateSession(h.ctx, backend.CreateSessionRequest{
			ExpectedSnapshotGeneration: generation,
			Label:                      name,
			Workspace:                  path,
			ClientProfileID:            profileID,
			RouteID:                    routeID,
		})
		if err != nil {
			return sessionCreatedMsg{err: err, tempID: tempID}
		}
		launched, err := source.LaunchSession(h.ctx, backend.SessionRef{
			ID:                   created.ID,
			ExpectedUpdatedAt:    created.UpdatedAt,
			CredentialGeneration: created.CredentialGeneration,
		}, 30*time.Second)
		if err != nil {
			return sessionCreatedMsg{err: fmt.Errorf("session created but launch failed: %w", err), tempID: tempID}
		}
		launched = decorateBackendSessionWith(launched, profiles, routes)
		return sessionCreatedMsg{instance: coreBackendInstance(launched), backendSession: &launched, tempID: tempID}
	}
}

func (h *Home) decorateBackendSession(source backend.Session) backend.Session {
	return decorateBackendSessionWith(source, h.backendProfiles, h.backendRoutes)
}

func decorateBackendSessionWith(source backend.Session, profiles map[string]backend.ClientProfile, routes map[string]backend.Route) backend.Session {
	if profile, ok := profiles[source.ClientProfileID]; ok && source.Tool == source.ClientProfileID {
		source.Tool = profile.Launcher
	}
	if route, ok := routes[source.RouteID]; ok && (source.GroupPath == "" || source.GroupPath == source.RouteID) {
		source.GroupPath = route.Name + "/" + route.ID
	}
	return source
}

func (h *Home) restartCoreSession(source backend.Backend, inst *session.Instance, fresh bool) tea.Cmd {
	id := inst.ID
	ref, refErr := h.backendSessionRef(id)
	return func() tea.Msg {
		if refErr != nil {
			return sessionRestartedMsg{sessionID: id, err: refErr, fresh: fresh}
		}
		var updated backend.Session
		var err error
		if fresh {
			updated, err = source.LaunchSession(h.ctx, ref, 30*time.Second)
		} else {
			updated, err = source.ResumeSession(h.ctx, ref, 30*time.Second)
		}
		if err != nil {
			return sessionRestartedMsg{sessionID: id, err: err, fresh: fresh}
		}
		return sessionRestartedMsg{sessionID: id, fresh: fresh, backendSession: &updated}
	}
}

func (h *Home) endCoreSession(source backend.Backend, inst *session.Instance, mode backend.EndMode) tea.Cmd {
	id := inst.ID
	ref, refErr := h.backendSessionRef(id)
	return func() tea.Msg {
		err := refErr
		if err == nil {
			err = source.EndSession(h.ctx, backend.EndSessionRequest{
				Session: ref,
				Mode:    mode,
				Timeout: 30 * time.Second,
			})
		}
		return sessionDeletedMsg{deletedID: id, killErr: err}
	}
}
