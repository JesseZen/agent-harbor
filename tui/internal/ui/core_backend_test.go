package ui

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/asheshgoplani/agent-deck/internal/backend"
	"github.com/asheshgoplani/agent-deck/internal/session"
	tea "github.com/charmbracelet/bubbletea"
)

type fakeCoreBackend struct {
	snapshot backend.Snapshot
	loads    int
}

func (fake *fakeCoreBackend) LoadSessions(context.Context) (backend.Snapshot, error) {
	fake.loads++
	return fake.snapshot, nil
}

type fakeCoreActionBackend struct {
	backend.UnimplementedBackend
	snapshot       backend.Snapshot
	previewRefs    []backend.SessionRef
	createRequests []backend.CreateSessionRequest
	launchRefs     []backend.SessionRef
	resumeRefs     []backend.SessionRef
	endRequests    []backend.EndSessionRequest
	attachRefs     []backend.SessionRef
	attachCommand  backend.AttachCommand
}

func (fake *fakeCoreActionBackend) LoadSessions(context.Context) (backend.Snapshot, error) {
	return fake.snapshot, nil
}

func (fake *fakeCoreActionBackend) Preview(_ context.Context, ref backend.SessionRef) (backend.Preview, error) {
	fake.previewRefs = append(fake.previewRefs, ref)
	return backend.Preview{SessionID: ref.ID, Lines: []string{"core preview", "second line"}}, nil
}

func (fake *fakeCoreActionBackend) CreateSession(_ context.Context, request backend.CreateSessionRequest) (backend.Session, error) {
	fake.createRequests = append(fake.createRequests, request)
	return backend.Session{
		ID: "ses_core_created", Title: request.Label, ProjectPath: request.Workspace, GroupPath: "Main client/route_main",
		Tool: "codex", Status: backend.StatusCreated, CreatedAt: time.Unix(1_700_000_200, 0).UTC(),
		UpdatedAt: time.Unix(1_700_000_300, 0).UTC(), ClientProfileID: request.ClientProfileID, RouteID: request.RouteID,
	}, nil
}

func (fake *fakeCoreActionBackend) LaunchSession(_ context.Context, ref backend.SessionRef, _ time.Duration) (backend.Session, error) {
	fake.launchRefs = append(fake.launchRefs, ref)
	return backend.Session{
		ID: ref.ID, Title: "Created through Core", ProjectPath: "/workspace/created", GroupPath: "Main client/route_main",
		Tool: "codex", Status: backend.StatusRunning, CreatedAt: time.Unix(1_700_000_200, 0).UTC(),
		UpdatedAt: time.Unix(1_700_000_400, 0).UTC(), ClientProfileID: "profile_codex", RouteID: "route_main",
	}, nil
}

func (fake *fakeCoreActionBackend) ResumeSession(_ context.Context, ref backend.SessionRef, _ time.Duration) (backend.Session, error) {
	fake.resumeRefs = append(fake.resumeRefs, ref)
	return backend.Session{ID: ref.ID, Title: "Resumed", ProjectPath: "/workspace/resume", GroupPath: "Main/route_main", Tool: "codex", Status: backend.StatusRunning, UpdatedAt: ref.ExpectedUpdatedAt.Add(time.Second)}, nil
}

func (fake *fakeCoreActionBackend) EndSession(_ context.Context, request backend.EndSessionRequest) error {
	fake.endRequests = append(fake.endRequests, request)
	return nil
}

func (fake *fakeCoreActionBackend) PrepareAttach(_ context.Context, ref backend.SessionRef, destination io.Writer) (backend.AttachCommand, error) {
	fake.attachRefs = append(fake.attachRefs, ref)
	if _, err := io.WriteString(destination, "abcdefghijklmnopqrstuvwxyzABCDEFGH123456789"); err != nil {
		return backend.AttachCommand{}, err
	}
	return fake.attachCommand, nil
}

func TestCoreBackendUsesAgentDeckHomeWithoutLocalRuntimeOwners(t *testing.T) {
	fake := &fakeCoreBackend{snapshot: backend.Snapshot{Sessions: []backend.Session{
		{
			ID:          "ses_core_alpha",
			Title:       "Alpha planning",
			ProjectPath: "/workspace/alpha",
			GroupPath:   "Main client/route_main",
			Tool:        "codex",
			Status:      backend.StatusRunning,
			CreatedAt:   time.Unix(1_700_000_000, 0),
		},
	}}}

	home := NewHomeWithBackend(fake)
	if home.storage != nil || home.storageWatcher != nil || home.hookWatcher != nil || home.liveSet != nil {
		t.Fatalf("Core-backed Home started Agent Deck runtime owners")
	}

	model, _ := home.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	home = model.(*Home)
	message := home.Init()()
	model, _ = home.Update(message)
	home = model.(*Home)

	view := home.View()
	for _, expected := range []string{"Alpha planning", "Main client", "route_main"} {
		if !strings.Contains(view, expected) {
			t.Fatalf("Agent Deck Home did not render backend value %q:\n%s", expected, view)
		}
	}
	if fake.loads != 1 {
		t.Fatalf("backend loads = %d, want 1", fake.loads)
	}
}

func TestCoreBackendProjectsAuthoritativeNativeActivityIntoHomeStatus(t *testing.T) {
	tests := []struct {
		name     string
		activity backend.NativeActivity
		want     session.Status
	}{
		{name: "running", activity: backend.NativeActivityRunning, want: session.StatusRunning},
		{name: "waiting", activity: backend.NativeActivityWaiting, want: session.StatusWaiting},
		{name: "idle", activity: backend.NativeActivityIdle, want: session.StatusIdle},
		{name: "failed", activity: backend.NativeActivityFailed, want: session.StatusError},
		{name: "unknown", activity: backend.NativeActivityUnknown, want: session.StatusIdle},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			instance := coreBackendInstance(backend.Session{
				ID:             "ses_activity",
				Status:         backend.StatusRunning,
				NativeActivity: test.activity,
				ActivitySource: backend.ActivitySourceHook,
				HookHealth:     backend.HookHealthActive,
			})
			if instance.Status != test.want {
				t.Fatalf("status = %q, want %q", instance.Status, test.want)
			}
		})
	}
}

func TestCoreBackendPreviewShowsAuthoritativeActivityDetails(t *testing.T) {
	observedAt := time.Unix(1_700_000_000, 0).UTC()
	fake := &fakeCoreBackend{snapshot: backend.Snapshot{Sessions: []backend.Session{{
		ID: "ses_activity_details", Title: "Activity details", ProjectPath: "/workspace/details",
		GroupPath: "Main/route_main", Tool: "codex", Status: backend.StatusRunning,
		NativeActivity: backend.NativeActivityWaiting, ActivitySource: backend.ActivitySourceHook,
		ActivityObservedAt: observedAt, HookHealth: backend.HookHealthActive,
		NativeProvider: backend.NativeProviderCodex, NativeSessionID: "native-session-123",
	}}}}
	home := NewHomeWithBackend(fake)
	model, _ := home.Update(home.Init()())
	home = model.(*Home)
	home.width = 120
	home.height = 30
	home.moveCursorToSession("ses_activity_details")

	rendered := home.renderPreviewPane(80, 24)
	for _, expected := range []string{
		"waiting via hook",
		"Hook: active",
		"Native: codex / native-session-123",
	} {
		if !strings.Contains(rendered, expected) {
			t.Fatalf("authoritative Session preview omitted %q:\n%s", expected, rendered)
		}
	}
}

func TestCoreLoaderWithoutActionContractNeverFallsBackToLocalRuntime(t *testing.T) {
	fake := &fakeCoreBackend{snapshot: backend.Snapshot{Sessions: []backend.Session{{
		ID: "ses_core_loader", Title: "Loader only", ProjectPath: "/workspace/loader", GroupPath: "Main/route_main",
		Tool: "codex", Status: backend.StatusFailed, UpdatedAt: time.Unix(1_700_000_050, 0).UTC(),
	}}}}
	home := NewHomeWithBackend(fake)
	message := home.Init()()
	model, _ := home.Update(message)
	home = model.(*Home)
	inst := home.getInstanceByID("ses_core_loader")
	if inst == nil {
		t.Fatal("Core session was not loaded")
	}
	t.Setenv("PATH", t.TempDir())
	if restarted := home.restartSession(inst)().(sessionRestartedMsg); !errors.Is(restarted.err, backend.ErrUnsupported) {
		t.Fatalf("restart = %#v", restarted)
	}
	if attached := home.attachSession(inst)().(statusUpdateMsg); !errors.Is(attached.err, backend.ErrUnsupported) {
		t.Fatalf("attach = %#v", attached)
	}
}

func TestCoreBackendShutdownDoesNotWaitForUnstartedAgentDeckWorkers(t *testing.T) {
	home := NewHomeWithBackend(&fakeCoreBackend{})
	started := time.Now()
	message := home.performFinalShutdown(false)()
	if _, ok := message.(tea.QuitMsg); !ok {
		t.Fatalf("shutdown message = %T", message)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("Core shutdown waited for unstarted Agent Deck workers: %s", elapsed)
	}
}

func TestCoreBackendPreviewUsesBackendInsteadOfLocalTmux(t *testing.T) {
	updatedAt := time.Unix(1_700_000_100, 0).UTC()
	fake := &fakeCoreActionBackend{snapshot: backend.Snapshot{Generation: 8, Sessions: []backend.Session{{
		ID: "ses_core_preview", Title: "Core preview", ProjectPath: "/workspace/preview", GroupPath: "Main/route_main",
		Tool: "codex", Status: backend.StatusRunning, CreatedAt: time.Unix(1_700_000_000, 0), UpdatedAt: updatedAt,
	}}}}
	home := NewHomeWithBackend(fake)
	message := home.Init()()
	model, _ := home.Update(message)
	home = model.(*Home)

	inst := home.getInstanceByID("ses_core_preview")
	if inst == nil {
		t.Fatal("Core session was not loaded")
	}
	message = home.fetchPreview(inst, inst.ID, -1)()
	preview, ok := message.(previewFetchedMsg)
	if !ok {
		t.Fatalf("preview message = %T", message)
	}
	if preview.err != nil || preview.content != "core preview\nsecond line" {
		t.Fatalf("preview = %#v", preview)
	}
	if len(fake.previewRefs) != 1 || fake.previewRefs[0].ID != inst.ID || !fake.previewRefs[0].ExpectedUpdatedAt.Equal(updatedAt) {
		t.Fatalf("preview refs = %#v", fake.previewRefs)
	}
}

func TestCoreBackendCreateUsesBackendBeforeAnyLocalRuntimeCheck(t *testing.T) {
	fake := &fakeCoreActionBackend{snapshot: backend.Snapshot{
		Generation:     12,
		ClientProfiles: []backend.ClientProfile{{ID: "profile_codex", Name: "Codex", Launcher: "codex"}},
		Routes:         []backend.Route{{ID: "route_main", Name: "Main client"}},
	}}
	home := NewHomeWithBackend(fake)
	message := home.Init()()
	model, _ := home.Update(message)
	home = model.(*Home)

	t.Setenv("PATH", t.TempDir())
	message = home.createSessionInGroupWithWorktreeAndOptions(
		"Created through Core", "/workspace/created", "codex", "Main client/route_main", "", "", "",
		false, false, nil, nil, "", "", false, nil, "", "", "temp-core", false,
	)()
	created, ok := message.(sessionCreatedMsg)
	if !ok {
		t.Fatalf("create message = %T", message)
	}
	if created.err != nil || created.instance == nil || created.instance.ID != "ses_core_created" {
		t.Fatalf("created message = %#v", created)
	}
	if len(fake.createRequests) != 1 {
		t.Fatalf("create requests = %#v", fake.createRequests)
	}
	request := fake.createRequests[0]
	if request.ExpectedSnapshotGeneration != 12 || request.ClientProfileID != "profile_codex" || request.RouteID != "route_main" {
		t.Fatalf("create request = %#v", request)
	}
	if len(fake.launchRefs) != 1 || fake.launchRefs[0].ID != "ses_core_created" || fake.launchRefs[0].ExpectedUpdatedAt.IsZero() {
		t.Fatalf("launch refs = %#v", fake.launchRefs)
	}
}

func TestCoreBackendCreateUsesSelectedProfileDefaultRoute(t *testing.T) {
	fake := &fakeCoreActionBackend{snapshot: backend.Snapshot{
		Generation: 12,
		ClientProfiles: []backend.ClientProfile{{
			ID: "client_codex", Name: "Codex", Launcher: "codex",
			DefaultRouteID: "route_codex", DefaultRouteName: "Codex route",
		}},
		Routes: []backend.Route{
			{ID: "route_claude", Name: "Claude route"},
			{ID: "route_codex", Name: "Codex route"},
		},
	}}
	home := NewHomeWithBackend(fake)
	model, _ := home.Update(home.Init()())
	home = model.(*Home)

	message := home.createCoreSession(
		fake, "Codex through Core", "/workspace/codex", "client_codex",
		"Claude route/route_claude", "",
	)()
	created, ok := message.(sessionCreatedMsg)
	if !ok || created.err != nil {
		t.Fatalf("create message = %#v", message)
	}
	if len(fake.createRequests) != 1 || fake.createRequests[0].RouteID != "route_codex" {
		t.Fatalf("create request = %#v", fake.createRequests)
	}
}

func TestCoreBackendNewSessionDialogShowsOnlyCoreInputs(t *testing.T) {
	fake := &fakeCoreActionBackend{snapshot: backend.Snapshot{
		ClientProfiles: []backend.ClientProfile{
			{
				ID: "client_claude", Name: "Claude Code", Launcher: "claude",
				DefaultRouteID: "route_claude", DefaultRouteName: "Claude route",
			},
			{
				ID: "client_codex", Name: "Codex", Launcher: "codex",
				DefaultRouteID: "route_codex", DefaultRouteName: "Codex route",
			},
		},
		Routes: []backend.Route{
			{ID: "route_claude", Name: "Claude route"},
			{ID: "route_codex", Name: "Codex route"},
		},
	}}
	home := NewHomeWithBackend(fake)
	model, _ := home.Update(home.Init()())
	home = model.(*Home)
	home.showCoreNewSessionDialog()
	home.newDialog.SetSize(120, 40)

	view := home.newDialog.View()
	for _, expected := range []string{
		"New Session", "Name:", "Traffic Rule:",
		"Claude Code", "Workspace:",
	} {
		if !strings.Contains(view, expected) {
			t.Fatalf("Core new-session dialog missing %q:\n%s", expected, view)
		}
	}
	for _, forbidden := range []string{
		"Route:", "Client Profile:",
		"Model ID:", "Claude Options", "Create in worktree", "Docker sandbox",
		"Multi-repo mode", "Continue", "Resume", "gemini", "opencode", "codex",
	} {
		if strings.Contains(view, forbidden) {
			t.Fatalf("Core new-session dialog exposed Agent Deck-only field %q:\n%s", forbidden, view)
		}
	}
	if got := home.newDialog.GetSelectedCommand(); got != "client_claude" {
		t.Fatalf("selected Core client profile = %q, want client_claude", got)
	}
	home.newDialog.SetDefaultTool("client_codex")
	if view := home.newDialog.View(); !strings.Contains(view, "Codex") {
		t.Fatalf("Core traffic rule selection did not change:\n%s", view)
	}
}

func TestCoreBackendExistingCreateHotkeysAvoidAgentDeckStorage(t *testing.T) {
	fake := &fakeCoreActionBackend{snapshot: backend.Snapshot{
		Generation:     15,
		ClientProfiles: []backend.ClientProfile{{ID: "profile_codex", Name: "Codex", Launcher: "codex"}},
		Routes:         []backend.Route{{ID: "route_main", Name: "Main client"}},
		Sessions: []backend.Session{{
			ID: "ses_core_seed", Title: "Seed", ProjectPath: "/workspace/seed", GroupPath: "Main client/route_main",
			Tool: "codex", Status: backend.StatusRunning, UpdatedAt: time.Unix(1_700_000_450, 0).UTC(), ClientProfileID: "profile_codex", RouteID: "route_main",
		}},
	}}
	home := NewHomeWithBackend(fake)
	message := home.Init()()
	model, _ := home.Update(message)
	home = model.(*Home)

	model, command := home.handleMainKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	home = model.(*Home)
	if command != nil || !home.newDialog.IsVisible() || home.storage != nil {
		t.Fatalf("Core new dialog state: command=%v visible=%v storage=%v", command != nil, home.newDialog.IsVisible(), home.storage)
	}
	home.newDialog.Hide()

	model, command = home.handleMainKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'N'}})
	home = model.(*Home)
	if command == nil {
		t.Fatal("Core quick-create returned no command")
	}
	created, ok := command().(sessionCreatedMsg)
	if !ok || created.err != nil {
		t.Fatalf("Core quick-create message = %#v (%v)", created, created.err)
	}
	if len(fake.createRequests) != 1 || fake.createRequests[0].ExpectedSnapshotGeneration != 15 || fake.createRequests[0].Workspace != "/workspace/seed" || fake.createRequests[0].ClientProfileID != "profile_codex" || fake.createRequests[0].RouteID != "route_main" {
		t.Fatalf("Core quick-create request = %#v", fake.createRequests)
	}
}

func TestCoreBackendLifecycleActionsNeverCallLocalSessionRuntime(t *testing.T) {
	updatedAt := time.Unix(1_700_000_500, 0).UTC()
	fake := &fakeCoreActionBackend{snapshot: backend.Snapshot{Sessions: []backend.Session{{
		ID: "ses_core_lifecycle", Title: "Lifecycle", ProjectPath: "/workspace/lifecycle", GroupPath: "Main/route_main",
		Tool: "codex", Status: backend.StatusIdle, UpdatedAt: updatedAt, CredentialGeneration: 2,
	}}}}
	home := NewHomeWithBackend(fake)
	message := home.Init()()
	model, _ := home.Update(message)
	home = model.(*Home)
	inst := home.getInstanceByID("ses_core_lifecycle")
	if inst == nil {
		t.Fatal("Core session was not loaded")
	}

	t.Setenv("PATH", t.TempDir())
	if message = home.restartSession(inst)(); message.(sessionRestartedMsg).err != nil {
		t.Fatalf("restart message = %#v", message)
	}
	if len(fake.resumeRefs) != 1 || !fake.resumeRefs[0].ExpectedUpdatedAt.Equal(updatedAt) {
		t.Fatalf("resume refs = %#v", fake.resumeRefs)
	}
	if message = home.restartSessionFresh(inst)(); message.(sessionRestartedMsg).err != nil {
		t.Fatalf("restart fresh message = %#v", message)
	}
	if len(fake.launchRefs) != 1 || fake.launchRefs[0].ID != inst.ID {
		t.Fatalf("launch refs = %#v", fake.launchRefs)
	}
	closed := home.closeSession(inst)()
	if message = closed; message.(sessionDeletedMsg).killErr != nil {
		t.Fatalf("close message = %#v", message)
	}
	if message = home.deleteSession(inst)(); message.(sessionDeletedMsg).killErr != nil {
		t.Fatalf("delete message = %#v", message)
	}
	if len(fake.endRequests) != 2 || fake.endRequests[0].Mode != backend.EndGraceful || fake.endRequests[1].Mode != backend.EndForce {
		t.Fatalf("end requests = %#v", fake.endRequests)
	}
	if message = home.archiveSession(inst)(); !errors.Is(message.(sessionArchivedMsg).killErr, backend.ErrUnsupported) {
		t.Fatalf("archive message = %#v", message)
	}
	if message = home.removeSession(inst)(); message.(sessionDeletedMsg).killErr != nil {
		t.Fatalf("remove message = %#v", message)
	}
	if len(fake.endRequests) != 3 || fake.endRequests[2].Mode != backend.EndForce {
		t.Fatalf("remove end requests = %#v", fake.endRequests)
	}

	model, _ = home.Update(closed)
	home = model.(*Home)
	if current := home.getInstanceByID(inst.ID); current != nil {
		t.Fatalf("gracefully ended Core row remains = %#v", current)
	}
}

func TestCoreBackendRestartRefreshesProjectionWithoutClearingUIState(t *testing.T) {
	createdAt := time.Unix(1_700_000_400, 0).UTC()
	updatedAt := time.Unix(1_700_000_500, 0).UTC()
	fake := &fakeCoreActionBackend{snapshot: backend.Snapshot{Sessions: []backend.Session{{
		ID: "ses_core_restart", Title: "Before", ProjectPath: "/workspace/before", GroupPath: "Main/route_main",
		Tool: "codex", Status: backend.StatusIdle, CreatedAt: createdAt,
	}}}}
	home := NewHomeWithBackend(fake)
	model, _ := home.Update(home.Init()())
	home = model.(*Home)
	inst := home.getInstanceByID("ses_core_restart")
	if inst == nil {
		t.Fatal("Core session was not loaded")
	}
	inst.Notes = "keep local UI state"

	updated := backend.Session{
		ID: "ses_core_restart", Title: "After", ProjectPath: "/workspace/after", GroupPath: "Main/route_main",
		Tool: "claude", Status: backend.StatusRunning, CreatedAt: createdAt, LastAccessedAt: updatedAt,
	}
	model, _ = home.Update(sessionRestartedMsg{sessionID: updated.ID, backendSession: &updated})
	home = model.(*Home)
	current := home.getInstanceByID(updated.ID)

	if current != inst {
		t.Fatal("Core restart replaced the Agent Deck instance pointer")
	}
	if current.Notes != "keep local UI state" {
		t.Fatalf("Core restart cleared UI state: Notes = %q", current.Notes)
	}
	if current.Title != updated.Title || current.ProjectPath != updated.ProjectPath || current.Tool != updated.Tool || current.Status != coreBackendStatus(updated.Status) || !current.LastAccessedAt.Equal(updatedAt) {
		t.Fatalf("Core restart projection = %#v", current)
	}
}

func TestCoreAttachPassesOneTimeGrantOnlyThroughDescriptorThree(t *testing.T) {
	directory := t.TempDir()
	argsPath := filepath.Join(directory, "args")
	grantPath := filepath.Join(directory, "grant")
	scriptPath := filepath.Join(directory, "agent-harbor")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" > \"$CAPTURE_ARGS\"\ndd bs=43 count=1 <&3 > \"$CAPTURE_GRANT\" 2>/dev/null\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CAPTURE_ARGS", argsPath)
	t.Setenv("CAPTURE_GRANT", grantPath)

	updatedAt := time.Unix(1_700_000_700, 123).UTC()
	fake := &fakeCoreActionBackend{attachCommand: backend.AttachCommand{
		Executable: scriptPath, InstanceID: "ins_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", AdminSocket: "/tmp/agent-harbor.sock",
		SessionID: "ses_core_attach", ExpectedUpdatedAt: updatedAt,
	}}
	command := &coreAttachExecCmd{ctx: context.Background(), source: fake, ref: backend.SessionRef{ID: "ses_core_attach", ExpectedUpdatedAt: updatedAt}}
	if err := command.Run(); err != nil {
		t.Fatalf("Run: %v", err)
	}
	args, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatal(err)
	}
	wantArgs := "attach\n--instance-id\nins_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\n--admin-socket\n/tmp/agent-harbor.sock\n--session-id\nses_core_attach\n--expected-updated-at\n" + updatedAt.Format(time.RFC3339Nano) + "\n--grant-fd\n3\n"
	if string(args) != wantArgs {
		t.Fatalf("args = %q, want %q", args, wantArgs)
	}
	grant, err := os.ReadFile(grantPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(grant) != "abcdefghijklmnopqrstuvwxyzABCDEFGH123456789" {
		t.Fatalf("grant fd = %q", grant)
	}
}
