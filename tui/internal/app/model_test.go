package app

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/asheshgoplani/agent-deck/internal/backend"
	"github.com/asheshgoplani/agent-deck/internal/configdraft"
	"github.com/asheshgoplani/agent-deck/internal/coreclient/generated"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

type appBackend struct {
	backend.UnimplementedBackend
	loads   int
	patches []generated.PatchConfigCommand
	events  chan backend.Invalidation
}

func (source *appBackend) LoadSessions(context.Context) (backend.Snapshot, error) {
	source.loads++
	return backend.Snapshot{
		Identity:       backend.Identity{InstanceID: "ins_test", Binary: "agent-harbor-core", Version: "test"},
		Generation:     int64(source.loads),
		ConfigRevision: "rev-1",
		Ready:          true,
		Targets: []backend.Target{{
			ID: "target_primary", Name: "Primary", Health: "healthy", BaseEligible: true,
		}},
		QuotaGroups: []backend.QuotaGroup{{
			ID: "quota_default", Name: "Default", ActiveConcurrency: 1, MaxConcurrency: 4, RPM: 60,
		}},
		Observations: []backend.Observation{{
			ID: "obs_1", Type: "request", TargetID: "target_primary", SemanticOutcome: "success",
		}},
		Sessions: []backend.Session{{
			ID: "ses_test", Title: "Composed session", ProjectPath: "/workspace/app",
			GroupPath: "route_default", Tool: "codex", Status: backend.StatusRunning,
			ClientProfileID: "profile_default", RouteID: "route_default",
			NativeActivity: backend.NativeActivityWaiting, ActivitySource: backend.ActivitySourceHook,
			HookHealth: backend.HookHealthActive,
		}},
	}, nil
}

func (*appBackend) LoadConfig(context.Context) (generated.MutableConfigSnapshot, error) {
	snapshot := configdraft.FixtureSnapshot(configdraft.WithInstanceID("ins_test"))
	snapshot.MutableConfig.ClientProfiles = []generated.MutableClientProfile{{
		Id: "profile_default", Name: "Default profile", Launcher: generated.MutableClientProfileLauncherCodex,
		Arguments: []string{}, Environment: []generated.EnvironmentVariableConfig{}, CompatibilityTransformIds: []generated.ConfigID{},
	}}
	return snapshot, nil
}

func (*appBackend) LoadConfigMutationStatus(context.Context) (generated.ConfigMutationStatus, error) {
	return generated.ConfigMutationStatusAvailable, nil
}

func (*appBackend) LoadAgentSessions(context.Context) ([]generated.AgentSession, error) {
	provider := generated.AgentSessionNativeProviderCodex
	nativeID := "native-test"
	return []generated.AgentSession{{
		Id: "ses_test", Label: "Composed session", Workspace: "/workspace/app",
		Lifecycle: generated.AgentSessionLifecycleRunning, NativeActivity: generated.AgentSessionNativeActivityWaiting,
		ActivitySource: generated.SessionActivitySourceHook, NativeProvider: &provider, NativeSessionId: &nativeID,
		HookHealth: generated.AgentSessionHookHealthActive, HookHealthObservedAt: time.Unix(1_700_000_000, 0).UTC(),
		CreatedAt: time.Unix(1_699_999_000, 0).UTC(), UpdatedAt: time.Unix(1_700_000_000, 0).UTC(),
	}}, nil
}

func (*appBackend) ValidateConfig(context.Context, generated.ValidateConfigCommand) (generated.ValidationResult, error) {
	return generated.ValidationResult{Valid: true}, nil
}

func (source *appBackend) PatchConfig(_ context.Context, command generated.PatchConfigCommand) (generated.SnapshotIdentity, error) {
	source.patches = append(source.patches, command)
	return generated.SnapshotIdentity{Generation: 2, ConfigRevision: "rev-2"}, nil
}

func (*appBackend) Preview(context.Context, backend.SessionRef) (backend.Preview, error) {
	return backend.Preview{SessionID: "ses_test", Lines: []string{"preview ready"}}, nil
}

func (source *appBackend) WatchInvalidations(context.Context, string) (<-chan backend.Invalidation, error) {
	if source.events == nil {
		return nil, backend.ErrUnsupported
	}
	return source.events, nil
}

func (*appBackend) CreateProviderSecretStageWithBodyWithResponse(
	context.Context,
	string,
	io.Reader,
	...generated.RequestEditorFn,
) (*generated.CreateProviderSecretStageResponse, error) {
	return &generated.CreateProviderSecretStageResponse{
		HTTPResponse: &http.Response{StatusCode: http.StatusCreated},
		JSON201:      &generated.ProviderSecretStage{StageId: "stage_test", ExpiresAt: time.Now().Add(time.Minute)},
	}, nil
}

func (*appBackend) DeleteProviderSecretStageWithResponse(
	context.Context,
	generated.SecretStageID,
	...generated.RequestEditorFn,
) (*generated.DeleteProviderSecretStageResponse, error) {
	return &generated.DeleteProviderSecretStageResponse{
		HTTPResponse: &http.Response{StatusCode: http.StatusNoContent},
	}, nil
}

func loadedRootModel(t *testing.T, source *appBackend) *Model {
	t.Helper()
	model := New(source)
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	model = updated.(*Model)
	updated, _ = model.Update(model.loadAll(false)())
	model = updated.(*Model)
	updated, command := model.Update(model.sessionHome.Init()())
	model = updated.(*Model)
	drainModelCommand(t, &model, command, 0)
	return model
}

func TestRootComposesFiveUserTabsAndRealFactories(t *testing.T) {
	model := loadedRootModel(t, &appBackend{})
	row := ansi.Strip(strings.SplitN(model.View(), "\n", 2)[0])
	for _, label := range []string{"Overview", "Sessions", "Upstreams", "Traffic Rules", "Observations"} {
		if !strings.Contains(row, label) {
			t.Fatalf("tab row omitted %q: %q", label, row)
		}
	}
	if strings.Contains(row, "Placeholder") || strings.Contains(row, "Configuration") {
		t.Fatalf("legacy navigation remains: %q", row)
	}
	if model.overview == nil || model.sessions == nil || model.profiles == nil ||
		model.routes == nil || model.targets == nil || model.quotas == nil || model.observations == nil {
		t.Fatal("one or more real page factories were not composed")
	}
	if view := ansi.Strip(model.View()); !strings.Contains(view, "Composed session") ||
		!strings.Contains(view, "◐ 1") {
		t.Fatalf("Sessions did not render generated authoritative state:\n%s", view)
	}
	snapshot := model.sessionBackend.Snapshot()
	if len(snapshot.Sessions) != 1 ||
		snapshot.Sessions[0].NativeActivity != backend.NativeActivityWaiting ||
		snapshot.Sessions[0].ActivitySource != backend.ActivitySourceHook {
		t.Fatalf("Session workbench lost authoritative activity metadata: %#v", snapshot.Sessions)
	}
}

func TestGlobalNavigationHasKeyboardMouseParity(t *testing.T) {
	model := loadedRootModel(t, &appBackend{})
	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyCtrlLeft})
	model = updated.(*Model)
	if model.active != tabOverview || !strings.Contains(ansi.Strip(model.View()), "log_level") {
		t.Fatalf("Ctrl+Left did not reach Overview:\n%s", model.View())
	}

	row := ansi.Strip(strings.SplitN(model.View(), "\n", 2)[0])
	x := strings.Index(row, "Sessions") + 1
	updated, _ = model.Update(tea.MouseMsg{X: x, Y: 0, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	model = updated.(*Model)
	if model.active != tabSessions {
		t.Fatal("tab-row mouse click did not reach Sessions")
	}

	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyCtrlRight})
	model = updated.(*Model)
	if model.active != tabTargets || !strings.Contains(ansi.Strip(model.View()), "Upstreams") {
		t.Fatalf("Ctrl+Right did not reach Upstreams:\n%s", model.View())
	}
	row = ansi.Strip(strings.SplitN(model.View(), "\n", 2)[0])
	x = strings.Index(row, "Traffic Rules") + 1
	updated, _ = model.Update(tea.MouseMsg{X: x, Y: 0, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	model = updated.(*Model)
	if model.active != tabRoutes {
		t.Fatal("tab-row mouse click did not reach Traffic Rules")
	}
}

func TestRuntimeInvalidationReloadsComposedPages(t *testing.T) {
	source := &appBackend{events: make(chan backend.Invalidation, 1)}
	model := loadedRootModel(t, source)
	if source.loads != 2 {
		t.Fatalf("initial runtime loads=%d", source.loads)
	}

	started := model.startInvalidationWatch()()
	updated, listen := model.Update(started)
	model = updated.(*Model)
	source.events <- backend.Invalidation{EventID: "2", Type: "session_changed"}
	updated, batch := model.Update(listen())
	model = updated.(*Model)
	commands := batch().(tea.BatchMsg)
	if len(commands) != 3 {
		t.Fatalf("invalidation commands=%d, want app reload + Session reload + next listen", len(commands))
	}
	message := commands[0]()
	if _, isLoad := message.(loadResultMsg); !isLoad {
		t.Fatalf("first invalidation command=%T, want loadResultMsg", message)
	}
	updated, _ = model.Update(message)
	model = updated.(*Model)
	if source.loads != 3 || model.eventCursor != "2" {
		t.Fatalf("invalidation reload loads=%d cursor=%q", source.loads, model.eventCursor)
	}
}

func TestEventStreamRecoveryClearsOnlyDisconnectStatus(t *testing.T) {
	model := New(&appBackend{})
	events := make(chan backend.Invalidation)

	model.status = "Event stream disconnected"
	updated, listen := model.Update(invalidationWatchStartedMsg{events: events})
	model = updated.(*Model)
	if model.status != "" {
		t.Fatalf("successful event watch retained stale status %q", model.status)
	}
	if listen == nil {
		t.Fatal("successful event watch did not start listening")
	}

	model.status = "Configuration validation failed"
	updated, _ = model.Update(invalidationWatchStartedMsg{events: events})
	model = updated.(*Model)
	if model.status != "Configuration validation failed" {
		t.Fatalf("successful event watch cleared unrelated status %q", model.status)
	}

	model.status = "Event stream disconnected"
	updated, _ = model.Update(invalidationMsg{
		event:  backend.Invalidation{EventID: "2", Type: "session_changed"},
		events: events,
	})
	model = updated.(*Model)
	if model.status != "" {
		t.Fatalf("successful invalidation retained stale status %q", model.status)
	}

	model.status = "Configuration validation failed"
	updated, _ = model.Update(invalidationMsg{
		event:  backend.Invalidation{EventID: "3", Type: "target_gate_changed"},
		events: events,
	})
	model = updated.(*Model)
	if model.status != "Configuration validation failed" {
		t.Fatalf("successful invalidation cleared unrelated status %q", model.status)
	}
}

func TestSpotlightHelpAndFilteringOwnInput(t *testing.T) {
	model := loadedRootModel(t, &appBackend{})
	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{':'}})
	model = updated.(*Model)
	if model.commands == nil || !model.commands.IsOpen() || !strings.Contains(model.View(), "Spotlight") {
		t.Fatalf("colon did not open Spotlight:\n%s", model.View())
	}
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyEsc})
	model = updated.(*Model)

	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'?'}})
	model = updated.(*Model)
	if model.help != nil || !strings.Contains(model.View(), "KEYBOARD SHORTCUTS") {
		t.Fatalf("question mark did not open help:\n%s", model.View())
	}
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyEsc})
	model = updated.(*Model)

	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	model = updated.(*Model)
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{':'}})
	model = updated.(*Model)
	if model.commands != nil && model.commands.IsOpen() {
		t.Fatal("filter input opened Spotlight")
	}
	if model.sessionHome.CanOpenGlobalCommandMenu() || !strings.Contains(model.View(), "Local Search") {
		t.Fatal("Sessions filter did not retain input focus")
	}
}
