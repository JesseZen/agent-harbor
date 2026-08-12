package e2e_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/asheshgoplani/agent-deck/internal/app"
	"github.com/asheshgoplani/agent-deck/internal/backend"
	"github.com/asheshgoplani/agent-deck/internal/configdraft"
	"github.com/asheshgoplani/agent-deck/internal/coreclient"
	"github.com/asheshgoplani/agent-deck/internal/coreclient/generated"
	"github.com/asheshgoplani/agent-deck/internal/secretinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

type flowBackend struct {
	mu sync.Mutex

	configs   []generated.MutableConfigSnapshot
	configErr error
	patchErr  error
	status    generated.ConfigMutationStatus

	loadConfigCalls int
	validateCalls   []generated.ValidateConfigCommand
	patchCalls      []generated.PatchConfigCommand
	previewErrors   []error
	previewCalls    int
	stageBodies     [][]byte
}

func newFlowBackend() *flowBackend {
	snapshot := configdraft.FixtureSnapshot(
		configdraft.WithInstanceID("ins_0123456789abcdef0123456789abcdef"),
		configdraft.WithManagedCredential("credential_primary", "Primary", "openai", 3),
	)
	snapshot.MutableConfig.ClientProfiles = []generated.MutableClientProfile{{
		Id:          "profile_default",
		Name:        "Default profile",
		Launcher:    generated.MutableClientProfileLauncherCodex,
		Arguments:   []string{},
		Environment: []generated.EnvironmentVariableConfig{},
	}}
	return &flowBackend{
		configs: []generated.MutableConfigSnapshot{snapshot},
		status:  generated.ConfigMutationStatusAvailable,
	}
}

func (source *flowBackend) LoadSessions(context.Context) (backend.Snapshot, error) {
	return backend.Snapshot{
		Identity: backend.Identity{
			InstanceID: "ins_0123456789abcdef0123456789abcdef",
			Binary:     "agent-harbor-core",
			Version:    "e2e",
		},
		Generation:     1,
		ConfigRevision: "rev-1",
		Ready:          true,
		Sessions: []backend.Session{{
			ID:                   "ses_0123456789abcdef0123456789abcdef",
			Title:                "Cross-flow session",
			ProjectPath:          "/workspace/e2e",
			GroupPath:            "route_default",
			Tool:                 "codex",
			Status:               backend.StatusRunning,
			NativeActivity:       backend.NativeActivityWaiting,
			ActivitySource:       backend.ActivitySourceHook,
			NativeProvider:       backend.NativeProviderCodex,
			NativeSessionID:      "native-e2e",
			HookHealth:           backend.HookHealthActive,
			HookHealthObservedAt: time.Unix(1_700_000_009, 0),
			CreatedAt:            time.Unix(1_700_000_000, 0),
			LastAccessedAt:       time.Unix(1_700_000_010, 0),
			UpdatedAt:            time.Unix(1_700_000_010, 0),
			ClientProfileID:      "profile_default",
			RouteID:              "route_default",
		}},
	}, nil
}

func (source *flowBackend) LoadConfig(context.Context) (generated.MutableConfigSnapshot, error) {
	source.mu.Lock()
	defer source.mu.Unlock()
	source.loadConfigCalls++
	if source.configErr != nil {
		return generated.MutableConfigSnapshot{}, source.configErr
	}
	index := source.loadConfigCalls - 1
	if index >= len(source.configs) {
		index = len(source.configs) - 1
	}
	return source.configs[index], nil
}

func (source *flowBackend) LoadConfigMutationStatus(context.Context) (generated.ConfigMutationStatus, error) {
	source.mu.Lock()
	defer source.mu.Unlock()
	return source.status, nil
}

func (source *flowBackend) LoadAgentSessions(context.Context) ([]generated.AgentSession, error) {
	observed := time.Unix(1_700_000_009, 0).UTC()
	provider := generated.AgentSessionNativeProviderCodex
	nativeID := "native-e2e"
	return []generated.AgentSession{{
		Id:                       "ses_0123456789abcdef0123456789abcdef",
		Label:                    "Cross-flow session",
		Workspace:                "/workspace/e2e",
		ClientProfileId:          "profile_default",
		RouteId:                  "route_default",
		Lifecycle:                generated.AgentSessionLifecycleRunning,
		NativeActivity:           generated.AgentSessionNativeActivityWaiting,
		NativeActivityObservedAt: &observed,
		ActivitySource:           generated.SessionActivitySourceHook,
		NativeProvider:           &provider,
		NativeSessionId:          &nativeID,
		HookHealth:               generated.AgentSessionHookHealthActive,
		HookHealthObservedAt:     observed,
		CreatedAt:                time.Unix(1_700_000_000, 0).UTC(),
		UpdatedAt:                time.Unix(1_700_000_010, 0).UTC(),
		SessionCredential:        generated.SessionCredentialView{Generation: 1, Status: generated.Active},
	}}, nil
}

func (source *flowBackend) ReloadAgentSession(ctx context.Context, id string) (generated.AgentSession, error) {
	sessions, err := source.LoadAgentSessions(ctx)
	if err != nil || len(sessions) == 0 || string(sessions[0].Id) != id {
		return generated.AgentSession{}, errors.New("session not found")
	}
	sessions[0].UpdatedAt = sessions[0].UpdatedAt.Add(time.Second)
	return sessions[0], nil
}

func (source *flowBackend) ValidateConfig(_ context.Context, command generated.ValidateConfigCommand) (generated.ValidationResult, error) {
	source.mu.Lock()
	defer source.mu.Unlock()
	source.validateCalls = append(source.validateCalls, command)
	return generated.ValidationResult{Valid: true}, nil
}

func (source *flowBackend) PatchConfig(_ context.Context, command generated.PatchConfigCommand) (generated.SnapshotIdentity, error) {
	source.mu.Lock()
	defer source.mu.Unlock()
	source.patchCalls = append(source.patchCalls, command)
	if source.patchErr != nil {
		return generated.SnapshotIdentity{}, source.patchErr
	}
	return generated.SnapshotIdentity{
		Generation:     command.ExpectedGeneration + 1,
		ConfigRevision: command.ConfigRevision,
		PublishedAt:    time.Unix(1_700_000_020, 0),
	}, nil
}

func (*flowBackend) CreateSession(context.Context, backend.CreateSessionRequest) (backend.Session, error) {
	return backend.Session{}, backend.ErrUnsupported
}

func (*flowBackend) LaunchSession(context.Context, backend.SessionRef, time.Duration) (backend.Session, error) {
	return backend.Session{}, backend.ErrUnsupported
}

func (*flowBackend) ResumeSession(context.Context, backend.SessionRef, time.Duration) (backend.Session, error) {
	return backend.Session{}, backend.ErrUnsupported
}

func (*flowBackend) EndSession(context.Context, backend.EndSessionRequest) error {
	return backend.ErrUnsupported
}

func (*flowBackend) RotateCredential(context.Context, backend.SessionRef, io.Writer) (int, error) {
	return 0, backend.ErrUnsupported
}

func (*flowBackend) RevokeCredential(context.Context, backend.SessionRef) error {
	return backend.ErrUnsupported
}

func (*flowBackend) ResetAffinity(context.Context, backend.SessionRef) error {
	return backend.ErrUnsupported
}

func (source *flowBackend) Preview(context.Context, backend.SessionRef) (backend.Preview, error) {
	source.mu.Lock()
	defer source.mu.Unlock()
	index := source.previewCalls
	source.previewCalls++
	if index < len(source.previewErrors) && source.previewErrors[index] != nil {
		return backend.Preview{}, source.previewErrors[index]
	}
	return backend.Preview{
		SessionID:  "ses_0123456789abcdef0123456789abcdef",
		Lines:      []string{"redacted preview ready"},
		ObservedAt: time.Unix(1_700_000_011, 0).UTC(),
	}, nil
}

func (*flowBackend) HookHealth(context.Context, backend.SessionRef) (backend.HookHealth, error) {
	return backend.HookHealth{}, backend.ErrUnsupported
}

func (*flowBackend) PrepareAttach(context.Context, backend.SessionRef, io.Writer) (backend.AttachCommand, error) {
	return backend.AttachCommand{}, backend.ErrUnsupported
}

func (*flowBackend) WatchInvalidations(context.Context, string) (<-chan backend.Invalidation, error) {
	return nil, backend.ErrUnsupported
}

func (source *flowBackend) CreateProviderSecretStageWithBodyWithResponse(
	_ context.Context,
	_ string,
	body io.Reader,
	_ ...generated.RequestEditorFn,
) (*generated.CreateProviderSecretStageResponse, error) {
	raw, err := io.ReadAll(body)
	if err != nil {
		return nil, err
	}
	source.mu.Lock()
	source.stageBodies = append(source.stageBodies, append([]byte(nil), raw...))
	source.mu.Unlock()
	return &generated.CreateProviderSecretStageResponse{
		HTTPResponse: &http.Response{StatusCode: http.StatusCreated},
		JSON201: &generated.ProviderSecretStage{
			StageId:   "stage_e2e_1",
			ExpiresAt: time.Now().Add(time.Hour).UTC(),
		},
	}, nil
}

func (*flowBackend) DeleteProviderSecretStageWithResponse(
	context.Context,
	generated.SecretStageID,
	...generated.RequestEditorFn,
) (*generated.DeleteProviderSecretStageResponse, error) {
	return &generated.DeleteProviderSecretStageResponse{
		HTTPResponse: &http.Response{StatusCode: http.StatusNoContent},
	}, nil
}

func TestFLOW001AndFLOW005ComposeOwningTabsWithoutLegacyNavigation(t *testing.T) {
	model := startApp(t, newFlowBackend(), 120, 30)
	row := ansi.Strip(strings.SplitN(model.View(), "\n", 2)[0])

	for _, label := range []string{"Overview", "Sessions", "Upstreams", "Traffic Rules", "Observations"} {
		if !strings.Contains(row, label) {
			t.Fatalf("global tab row omitted %q: %q", label, row)
		}
	}
	for _, forbidden := range []string{"Placeholder", "Configuration"} {
		if strings.Contains(row, forbidden) {
			t.Fatalf("global tab row retained %q: %q", forbidden, row)
		}
	}

	model = send(t, model, tea.KeyMsg{Type: tea.KeyCtrlLeft})
	if view := ansi.Strip(model.View()); !strings.Contains(view, "Instance") || !strings.Contains(view, "log_level") {
		t.Fatalf("Overview did not render its real page factory:\n%s", view)
	}

	model = openAdvancedResource(t, model, "profiles")
	if view := ansi.Strip(model.View()); !strings.Contains(view, "Default profile") {
		t.Fatalf("Advanced Resources did not expose Profiles:\n%s", view)
	}
}

func TestFLOW002PublishesOneSharedDraftAcrossTabs(t *testing.T) {
	source := newFlowBackend()
	model := startApp(t, source, 120, 30)

	model = send(t, model, tea.KeyMsg{Type: tea.KeyCtrlLeft})
	model = send(t, model, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	model = send(t, model, tea.KeyMsg{Type: tea.KeyRight})
	model = send(t, model, tea.KeyMsg{Type: tea.KeyCtrlS})

	source.mu.Lock()
	defer source.mu.Unlock()
	if len(source.validateCalls) != 1 || len(source.patchCalls) != 1 {
		t.Fatalf("publish calls validate=%d patch=%d, want one each", len(source.validateCalls), len(source.patchCalls))
	}
	validate := source.validateCalls[0]
	patch := source.patchCalls[0]
	if validate.MutableConfig.Instance.LogLevel != generated.MutableInstanceConfigLogLevelDetail {
		t.Fatalf("validation omitted Overview change: %q", validate.MutableConfig.Instance.LogLevel)
	}
	if patch.MutableConfig.Instance.LogLevel != generated.MutableInstanceConfigLogLevelDetail {
		t.Fatalf("patch omitted Overview change: %q", patch.MutableConfig.Instance.LogLevel)
	}
	if patch.InstanceId != source.configs[0].InstanceId ||
		patch.ExpectedGeneration != source.configs[0].ActiveGeneration {
		t.Fatalf("patch lost snapshot identity: %#v", patch)
	}
	if patch.ConfigRevision == source.configs[0].ConfigRevision || !strings.HasPrefix(patch.ConfigRevision, "tui-") {
		t.Fatalf("patch did not allocate a new revision: %#v", patch)
	}
}

func TestFLOW002ConflictReloadsCurrentGenerationAndRetainsLocalDraft(t *testing.T) {
	source := newFlowBackend()
	current := source.configs[0]
	current.ActiveGeneration = 2
	current.ConfigRevision = "rev-2"
	source.configs = append(source.configs, current)
	source.patchErr = &coreclient.APIError{
		Operation:  "patchConfig",
		StatusCode: 409,
		Code:       "generation_conflict",
	}
	model := startApp(t, source, 120, 30)

	model = send(t, model, tea.KeyMsg{Type: tea.KeyCtrlLeft})
	model = send(t, model, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	model = send(t, model, tea.KeyMsg{Type: tea.KeyRight})
	model = send(t, model, tea.KeyMsg{Type: tea.KeyCtrlS})

	view := strings.ToLower(ansi.Strip(model.View()))
	if !strings.Contains(view, "conflict") || !strings.Contains(view, "detail") {
		t.Fatalf("conflict did not retain the local draft and recovery guidance:\n%s", view)
	}
	source.mu.Lock()
	defer source.mu.Unlock()
	if source.loadConfigCalls != 2 {
		t.Fatalf("generation conflict performed %d config loads, want initial + current", source.loadConfigCalls)
	}
	if len(source.patchCalls) != 1 {
		t.Fatalf("generation conflict performed %d patches, want one", len(source.patchCalls))
	}
}

func TestFLOW001DisconnectAndFLOW005NarrowMouseNavigation(t *testing.T) {
	source := newFlowBackend()
	source.configErr = errors.New("core disconnected")
	model := startApp(t, source, 70, 30)

	model = send(t, model, tea.KeyMsg{Type: tea.KeyCtrlLeft})
	view := ansi.Strip(model.View())
	if !strings.Contains(view, "Disconnected") {
		t.Fatalf("configuration read failure did not enter disconnected state:\n%s", view)
	}
	model = send(t, model, tea.KeyMsg{Type: tea.KeyCtrlS})
	source.mu.Lock()
	if len(source.patchCalls) != 0 {
		source.mu.Unlock()
		t.Fatal("disconnected draft attempted publication")
	}
	source.mu.Unlock()

	model = send(t, model, tea.MouseMsg{
		X:      strings.Index(ansi.Strip(strings.SplitN(model.View(), "\n", 2)[0]), "Observations") + 1,
		Y:      0,
		Button: tea.MouseButtonLeft,
		Action: tea.MouseActionPress,
	})
	if !strings.Contains(ansi.Strip(model.View()), "Observations") {
		t.Fatalf("mouse navigation did not reach Observations:\n%s", ansi.Strip(model.View()))
	}
	assertFrameFits(t, model.View(), 70, 30)
}

func TestFLOW003UsesCoreAuthoritativeSessionActivity(t *testing.T) {
	model := startApp(t, newFlowBackend(), 120, 30)
	model = send(t, model, tea.KeyMsg{Type: tea.KeyDown})
	view := strings.ToLower(ansi.Strip(model.View()))
	for _, expected := range []string{"sessions", "cross-flow session", "waiting", "hook", "codex"} {
		if !strings.Contains(view, expected) {
			t.Fatalf("Sessions omitted Core-authoritative %q:\n%s", expected, view)
		}
	}
	if strings.Contains(view, "working") {
		t.Fatalf("Sessions inferred a local working state:\n%s", view)
	}
}

func TestFLOW004PreviewFailuresExitLoading(t *testing.T) {
	t.Run("stale", func(t *testing.T) {
		source := newFlowBackend()
		source.previewErrors = []error{&coreclient.APIError{
			Operation: "preview", StatusCode: http.StatusConflict, Code: string(generated.StalePrecondition),
		}}
		model := startApp(t, source, 120, 30)
		model = send(t, model, tea.KeyMsg{Type: tea.KeyDown})
		view := ansi.Strip(model.View())
		if !strings.Contains(view, "Preview unavailable") || strings.Contains(view, "Loading preview") {
			t.Fatalf("stale preview did not terminate in an error state:\n%s", view)
		}
		source.mu.Lock()
		defer source.mu.Unlock()
		if source.previewCalls != 1 {
			t.Fatalf("stale preview calls=%d, want one owner and one request", source.previewCalls)
		}
	})

	t.Run("canceled", func(t *testing.T) {
		source := newFlowBackend()
		source.previewErrors = []error{context.Canceled}
		model := startApp(t, source, 120, 30)
		model = send(t, model, tea.KeyMsg{Type: tea.KeyDown})
		view := ansi.Strip(model.View())
		if !strings.Contains(view, "Preview unavailable") || strings.Contains(view, "Loading preview") {
			t.Fatalf("canceled preview did not exit loading:\n%s", view)
		}
	})
}

func TestFLOW002StageLossAndCleanupPendingBlockPublication(t *testing.T) {
	t.Run("stage expired", func(t *testing.T) {
		source := newFlowBackend()
		source.patchErr = &coreclient.APIError{
			Operation: "patchConfig", StatusCode: http.StatusGone, Code: string(secretinput.CodeExpired),
		}
		model := startApp(t, source, 120, 30)
		model = openAdvancedResource(t, model, "credentials")
		model = send(t, model, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
		model = send(t, model, tea.KeyMsg{Type: tea.KeyTab})
		model = send(t, model, tea.KeyMsg{Type: tea.KeyEnter})
		model = send(t, model, tea.KeyMsg{Type: tea.KeyDown})
		model = send(t, model, tea.KeyMsg{Type: tea.KeyEnter})
		model = send(t, model, tea.KeyMsg{Type: tea.KeyTab})
		model = send(t, model, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("e2e-stage-value")})
		model = send(t, model, tea.KeyMsg{Type: tea.KeyCtrlS})

		view := strings.ToLower(ansi.Strip(model.View()))
		if !strings.Contains(view, "paste the api key again") {
			t.Fatalf("expired stage did not enter replace-required recovery:\n%s", view)
		}
		source.mu.Lock()
		defer source.mu.Unlock()
		if len(source.stageBodies) != 1 || len(source.patchCalls) != 1 {
			t.Fatalf("stage bodies=%d patches=%d, want one each", len(source.stageBodies), len(source.patchCalls))
		}
		if strings.Contains(view, string(source.stageBodies[0])) {
			t.Fatal("staged bytes leaked into rendered UI")
		}
	})

	t.Run("cleanup pending", func(t *testing.T) {
		source := newFlowBackend()
		source.status = generated.ConfigMutationStatusSecretCleanupPending
		model := startApp(t, source, 120, 30)
		model = send(t, model, tea.KeyMsg{Type: tea.KeyCtrlRight})
		view := strings.ToLower(ansi.Strip(model.View()))
		if !strings.Contains(view, "cleanup") {
			t.Fatalf("cleanup-pending readiness was not surfaced:\n%s", view)
		}
		model = send(t, model, tea.KeyMsg{Type: tea.KeyCtrlS})
		source.mu.Lock()
		defer source.mu.Unlock()
		if len(source.patchCalls) != 0 {
			t.Fatal("cleanup-pending state allowed publication")
		}
	})
}

func openAdvancedResource(t *testing.T, model *app.Model, resource string) *app.Model {
	t.Helper()
	model = send(t, model, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{':'}})
	model = send(t, model, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("advanced")})
	model = send(t, model, tea.KeyMsg{Type: tea.KeyEnter})
	model = send(t, model, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(resource)})
	return send(t, model, tea.KeyMsg{Type: tea.KeyEnter})
}

func startApp(t *testing.T, source backend.SessionLoader, width, height int) *app.Model {
	t.Helper()
	model := app.New(source)
	model = send(t, model, tea.WindowSizeMsg{Width: width, Height: height})
	return sendCommand(t, model, model.Init(), 0)
}

func send(t *testing.T, model *app.Model, message tea.Msg) *app.Model {
	t.Helper()
	updated, command := model.Update(message)
	next := updated.(*app.Model)
	return sendCommand(t, next, command, 0)
}

func sendCommand(t *testing.T, model *app.Model, command tea.Cmd, depth int) *app.Model {
	t.Helper()
	if command == nil {
		return model
	}
	if depth > 32 {
		t.Fatal("Bubble Tea command recursion exceeded 32 steps")
	}
	message := command()
	if batch, ok := message.(tea.BatchMsg); ok {
		for _, child := range batch {
			model = sendCommand(t, model, child, depth+1)
		}
		return model
	}
	updated, next := model.Update(message)
	model = updated.(*app.Model)
	return sendCommand(t, model, next, depth+1)
}

func assertFrameFits(t *testing.T, view string, width, height int) {
	t.Helper()
	lines := strings.Split(strings.TrimSuffix(view, "\n"), "\n")
	if len(lines) > height {
		t.Fatalf("frame height=%d exceeds %d", len(lines), height)
	}
	for index, line := range lines {
		if got := ansi.StringWidth(line); got > width {
			t.Fatalf("line %d width=%d exceeds %d: %q", index+1, got, width, ansi.Strip(line))
		}
	}
}
