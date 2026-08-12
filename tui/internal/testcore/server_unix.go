//go:build unix

// Package testcore provides a deterministic same-UID Unix-socket server that
// implements the public Admin read protocol. It is intentionally built only
// from the public generated schema and contains no proprietary Core source.
package testcore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/asheshgoplani/agent-deck/internal/backend"
	"github.com/asheshgoplani/agent-deck/internal/coreclient/generated"
)

const defaultInstanceID = "ins_0123456789abcdef0123456789abcdef"

type Options struct {
	SocketPath string
	InstanceID string
}

type Server struct {
	socketPath string
	instanceID string
	tempDir    string
	server     *http.Server
	listener   net.Listener
	closeOnce  sync.Once
}

func Start(options Options) (*Server, error) {
	instanceID := options.InstanceID
	if instanceID == "" {
		instanceID = defaultInstanceID
	}
	socketPath := options.SocketPath
	tempDir := ""
	if socketPath == "" {
		var err error
		tempDir, err = os.MkdirTemp("", "agent-harbor-fake-core-")
		if err != nil {
			return nil, fmt.Errorf("create fake Core directory: %w", err)
		}
		socketPath = filepath.Join(tempDir, "admin.sock")
	} else if !filepath.IsAbs(socketPath) || filepath.Clean(socketPath) != socketPath {
		return nil, fmt.Errorf("fake Core socket path must be absolute and clean: %q", socketPath)
	}
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		if tempDir != "" {
			_ = os.RemoveAll(tempDir)
		}
		return nil, fmt.Errorf("listen on fake Core socket: %w", err)
	}
	fixture := newFixture(socketPath, instanceID)
	httpServer := &http.Server{Handler: fixture}
	server := &Server{socketPath: socketPath, instanceID: instanceID, tempDir: tempDir, server: httpServer, listener: listener}
	go func() {
		_ = httpServer.Serve(listener)
	}()
	return server, nil
}

func (server *Server) SocketPath() string { return server.socketPath }
func (server *Server) InstanceID() string { return server.instanceID }

func (server *Server) Close() error {
	var closeErr error
	server.closeOnce.Do(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := server.server.Shutdown(ctx); err != nil && !errors.Is(err, http.ErrServerClosed) {
			closeErr = err
		}
		_ = server.listener.Close()
		if server.tempDir != "" {
			_ = os.RemoveAll(server.tempDir)
		} else {
			_ = os.Remove(server.socketPath)
		}
	})
	return closeErr
}

type fixture struct {
	identity generated.InstanceIdentity
	snapshot generated.SnapshotIdentity
	config   generated.MutableConfigSnapshot
	status   generated.RuntimeStatus
}

func newFixture(socketPath, instanceID string) *fixture {
	base := time.Date(2026, time.July, 25, 4, 0, 0, 0, time.UTC)
	snapshot := generated.SnapshotIdentity{ConfigRevision: "fixture-2026-07-25", Generation: 42, PublishedAt: base}
	identity := generated.InstanceIdentity{
		AdminProtocolVersion: generated.N1, AdminSocket: socketPath, AdminTcpEnabled: false, Binary: generated.AgentHarborCore,
		InstanceId: instanceID, SupervisorPid: os.Getpid(), Version: "fixture-core-build",
		ConfigRoot: "/fixture/config", StateRoot: "/fixture/state", LogRoot: "/fixture/logs",
		GatewayAddress: "127.0.0.1:4317", TmuxSession: "agent-harbor-tui", TmuxSocket: "/fixture/tmux.sock",
	}
	profiles := []generated.MutableClientProfile{
		{Id: "profile_codex", Name: "Codex", Launcher: generated.MutableClientProfileLauncherCodex, DefaultRouteId: "route_primary", Arguments: []string{}, Environment: []generated.EnvironmentVariableConfig{}, CompatibilityTransformIds: []generated.ConfigID{}},
		{Id: "profile_claude", Name: "Claude", Launcher: generated.MutableClientProfileLauncherClaude, DefaultRouteId: "route_primary", Arguments: []string{}, Environment: []generated.EnvironmentVariableConfig{}, CompatibilityTransformIds: []generated.ConfigID{}},
		{Id: "profile_opencode", Name: "OpenCode", Launcher: generated.MutableClientProfileLauncherOpencode, DefaultRouteId: "route_batch", Arguments: []string{}, Environment: []generated.EnvironmentVariableConfig{}, CompatibilityTransformIds: []generated.ConfigID{}},
	}
	routeConfigs := []generated.RouteConfig{
		{Id: "route_primary", Name: "Interactive", BackendSetId: "backend_primary", RoutingPolicy: generated.RouteConfigRoutingPolicyAutomatic},
		{Id: "route_batch", Name: "Background", BackendSetId: "backend_batch", RoutingPolicy: generated.RouteConfigRoutingPolicyStickySession},
	}
	quotaConfigs := []generated.QuotaGroupConfig{
		{Id: "quota_interactive", Name: "Interactive quota", MaxConcurrency: 8, Rpm: 240},
		{Id: "quota_background", Name: "Background quota", MaxConcurrency: 3, Rpm: 60},
	}
	config := generated.MutableConfigSnapshot{
		ActiveGeneration: snapshot.Generation, ConfigRevision: snapshot.ConfigRevision, InstanceId: instanceID,
		MutableConfig: generated.MutableConfigView{
			Instance:                generated.MutableInstanceConfig{LogLevel: generated.MutableInstanceConfigLogLevelSimple},
			BackendSets:             []generated.BackendSetConfig{},
			ClientProfiles:          profiles,
			CompatibilityTransforms: []generated.CompatibilityTransformConfig{},
			ContentPolicies:         []generated.ContentPolicyConfig{},
			Credentials: []generated.MutableCredentialView{
				managedCredential("credential_openai", "OpenAI", generated.MutableCredentialViewProviderOpenai, 7),
				managedCredential("credential_anthropic", "Anthropic", generated.MutableCredentialViewProviderAnthropic, 4),
				managedCredential("credential_compatible", "Compatible", generated.MutableCredentialViewProviderOpenaiCompatible, 2),
			},
			Endpoints: []generated.EndpointConfig{
				{Id: "endpoint_openai", Name: "OpenAI", BaseUrl: "https://api.openai.com", Http2Enabled: true, IdleConnectionTimeoutMs: 30000, MaxIdleConnections: 16},
				{Id: "endpoint_anthropic", Name: "Anthropic", BaseUrl: "https://api.anthropic.com", Http2Enabled: true, IdleConnectionTimeoutMs: 30000, MaxIdleConnections: 16},
				{Id: "endpoint_compatible", Name: "Compatible", BaseUrl: "http://127.0.0.1:11434", AllowPrivateNetwork: true, IdleConnectionTimeoutMs: 30000, MaxIdleConnections: 8},
			},
			ModelPolicies:    []generated.ModelPolicyConfig{},
			ModelProjections: []generated.ModelProjectionConfig{},
			QuotaGroups:      quotaConfigs,
			Routes:           routeConfigs,
			Targets: []generated.TargetConfig{
				configTarget("target_openai", "OpenAI primary", generated.TargetConfigAdapterOpenai, generated.TargetConfigBridgeOpenaiChat, "endpoint_openai", "credential_openai", "quota_interactive", 7),
				configTarget("target_anthropic", "Anthropic reserve", generated.TargetConfigAdapterAnthropic, generated.TargetConfigBridgeAnthropicMessages, "endpoint_anthropic", "credential_anthropic", "quota_interactive", 4),
				configTarget("target_compatible", "Local compatible", generated.TargetConfigAdapterOpenaiCompatible, generated.TargetConfigBridgeOpenaiChat, "endpoint_compatible", "credential_compatible", "quota_background", 2),
			},
		},
	}
	routes := []generated.RouteStatus{
		{Id: "route_primary", Name: "Interactive", BackendSetId: "backend_primary", Policy: generated.RouteStatusPolicyAutomatic, EligibleTargetIds: []string{"target_openai", "target_anthropic"}, RecentDecisions: []generated.RouteDecision{{RequestId: "req_1", SessionId: "ses_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", LogicalModel: "frontier", OccurredAt: base, Reason: "healthy weighted candidate", Result: generated.RouteDecisionResultSelected, TargetId: pointer("target_openai")}}},
		{Id: "route_batch", Name: "Background", BackendSetId: "backend_batch", Policy: generated.RouteStatusPolicyStickySession, EligibleTargetIds: []string{"target_compatible"}},
	}
	targets := []generated.TargetStatus{
		target("target_openai", "OpenAI primary", generated.TargetStatusAdapterOpenai, generated.Healthy, true, "quota_interactive", 7),
		target("target_anthropic", "Anthropic reserve", generated.TargetStatusAdapterAnthropic, generated.Recovering, true, "quota_interactive", 4),
		target("target_compatible", "Local compatible", generated.TargetStatusAdapterOpenaiCompatible, generated.Healthy, true, "quota_background", 2),
	}
	quotas := []generated.QuotaGroupStatus{
		{Id: "quota_interactive", ActiveConcurrency: 3, MaxConcurrency: 8, Rpm: 240, ForegroundDepth: 1, NextPermitAt: base.Add(2 * time.Second)},
		{Id: "quota_background", ActiveConcurrency: 1, MaxConcurrency: 3, Rpm: 60, BackgroundDepth: 4, NextPermitAt: base.Add(8 * time.Second)},
	}
	sessions := []generated.AgentSession{
		session("ses_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "Review routing rollout", "/workspace/harbor", "route_primary", "profile_codex", generated.AgentSessionLifecycleRunning, generated.AgentSessionNativeActivityRunning, base.Add(-45*time.Minute), base.Add(-30*time.Second), 5),
		session("ses_bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", "Investigate quota pressure", "/workspace/observability", "route_primary", "profile_claude", generated.AgentSessionLifecycleIdle, generated.AgentSessionNativeActivityWaiting, base.Add(-2*time.Hour), base.Add(-4*time.Minute), 3),
		session("ses_cccccccccccccccccccccccccccccccc", "Prepare release notes", "/workspace/release", "route_batch", "profile_opencode", generated.AgentSessionLifecycleRunning, generated.AgentSessionNativeActivityRunning, base.Add(-25*time.Minute), base.Add(-50*time.Second), 2),
		session("ses_dddddddddddddddddddddddddddddddd", "Repair target health", "/workspace/core-adapter", "route_batch", "profile_codex", generated.AgentSessionLifecycleFailed, generated.AgentSessionNativeActivityFailed, base.Add(-3*time.Hour), base.Add(-18*time.Minute), 6),
	}
	policyAllowed := generated.PolicyDecisionAllowed
	success := generated.SemanticOutcomeSuccess
	observations := []generated.ObservationRecord{
		{Id: "obs_1042", Type: generated.Request, OccurredAt: base.Add(-10 * time.Second), SnapshotGeneration: 42, RouteId: pointer("route_primary"), TargetId: pointer("target_openai"), SessionId: pointer("ses_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"), DecisionReason: "healthy weighted candidate", PolicyDecision: &policyAllowed, SemanticOutcome: &success},
		{Id: "obs_1041", Type: generated.Quota, OccurredAt: base.Add(-28 * time.Second), SnapshotGeneration: 42, QuotaGroupId: pointer("quota_background"), DecisionReason: "background permit queued"},
		{Id: "obs_1040", Type: generated.Gate, OccurredAt: base.Add(-55 * time.Second), SnapshotGeneration: 42, TargetId: pointer("target_anthropic"), DecisionReason: "probe recovering"},
	}
	status := generated.RuntimeStatus{
		Identity: identity, Snapshot: snapshot, Routes: routes, Targets: targets, QuotaGroups: quotas, Sessions: sessions,
		Observations: generated.ObservationStatus{LastEventId: "1042"}, RecentObservations: observations,
	}
	return &fixture{identity: identity, snapshot: snapshot, config: config, status: status}
}

func managedCredential(id, name string, provider generated.MutableCredentialViewProvider, generation int) generated.MutableCredentialView {
	var binding generated.CredentialSecretBinding
	_ = binding.FromCredentialSecretBinding0(generated.CredentialSecretBinding0{
		Mode:       generated.CredentialSecretBindingManaged,
		Configured: generated.CredentialSecretBindingConfiguredTrue,
	})
	return generated.MutableCredentialView{
		Id: id, Name: name, Provider: provider, Generation: &generation, SecretBinding: binding,
	}
}

func configTarget(
	id, name string,
	adapter generated.TargetConfigAdapter,
	bridge generated.TargetConfigBridge,
	endpointID, credentialID, quotaID string,
	generation int,
) generated.TargetConfig {
	return generated.TargetConfig{
		Id: id, Name: name, Adapter: adapter, Bridge: bridge,
		Capabilities: []generated.TargetConfigCapabilities{
			generated.TargetConfigCapabilitiesChat,
			generated.TargetConfigCapabilitiesStreaming,
		},
		EndpointId: endpointID, CredentialId: credentialID, QuotaGroupId: quotaID, Generation: generation,
		HealthPolicy: generated.HealthPolicyConfig{
			FailureThreshold: 3, InitialBackoffMs: 500, JitterPercent: 10, MaxBackoffMs: 5000,
			ProbeTimeoutMs: 2000, RecoverySuccessThreshold: 2, StableProbeIntervalMs: 30000,
		},
		ThrottlePolicy: generated.ThrottlePolicyConfig{DefaultCoolingMs: 1000, MaxCoolingMs: 10000},
	}
}

func target(id, name string, adapter generated.TargetStatusAdapter, health generated.TargetStatusHealth, eligible bool, quota string, generation int) generated.TargetStatus {
	return generated.TargetStatus{
		Id: id, Name: name, Adapter: adapter, Health: health, BaseEligible: eligible, QuotaGroupId: quota,
		CredentialAccess: generated.TargetStatusCredentialAccessAllowed, CredentialGeneration: generation,
		TargetGeneration: generation, Throttle: generated.Available,
	}
}

func session(id, label, workspace, routeID, profileID string, lifecycle generated.AgentSessionLifecycle, activity generated.AgentSessionNativeActivity, createdAt, updatedAt time.Time, generation int) generated.AgentSession {
	provider := generated.AgentSessionNativeProviderCodex
	switch profileID {
	case "profile_claude":
		provider = generated.AgentSessionNativeProviderClaude
	}
	nativeID := "native-" + strings.TrimPrefix(id, "ses_")
	return generated.AgentSession{
		Id: id, Label: label, Workspace: workspace, RouteId: routeID, ClientProfileId: profileID,
		Lifecycle: lifecycle, NativeActivity: activity, NativeActivityEvent: generated.AgentSessionNativeActivityEventStarted,
		ActivitySource: generated.SessionActivitySourceHook, NativeProvider: &provider, NativeSessionId: &nativeID,
		HookHealth: generated.AgentSessionHookHealthActive, HookHealthObservedAt: updatedAt,
		CreatedAt: createdAt, UpdatedAt: updatedAt,
		SessionCredential: generated.SessionCredentialView{Generation: generation, Status: generated.Active},
	}
}

func pointer[T any](value T) *T { return &value }

func (fixture *fixture) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	switch request.URL.Path {
	case "/v1/readiness":
		writeJSON(writer, http.StatusOK, generated.Readiness{
			Ready: true, Identity: fixture.identity, Snapshot: fixture.snapshot, DatabaseSchemaVersion: 3,
			ConfigMutationStatus: generated.ConfigMutationStatusAvailable,
		})
	case "/v1/config":
		writeJSON(writer, http.StatusOK, fixture.config)
	case "/v1/status":
		writeJSON(writer, http.StatusOK, fixture.status)
	case "/v1/sessions":
		if request.URL.Query().Get("instance_id") != string(fixture.identity.InstanceId) {
			http.Error(writer, "instance mismatch", http.StatusNotFound)
			return
		}
		writeJSON(writer, http.StatusOK, fixture.status.Sessions)
	case "/v1/events":
		fixture.streamEvents(writer, request)
	default:
		if strings.HasPrefix(request.URL.Path, "/v1/sessions/") && strings.HasSuffix(request.URL.Path, "/preview") {
			fixture.preview(writer, request)
			return
		}
		if strings.HasPrefix(request.URL.Path, "/v1/sessions/") && strings.HasSuffix(request.URL.Path, "/hook-health") {
			fixture.hookHealth(writer, request)
			return
		}
		http.NotFound(writer, request)
	}
}

func (fixture *fixture) streamEvents(writer http.ResponseWriter, request *http.Request) {
	flusher, ok := writer.(http.Flusher)
	if !ok {
		http.Error(writer, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	writer.Header().Set("Content-Type", "text/event-stream")
	writer.Header().Set("Cache-Control", "no-cache")
	writer.WriteHeader(http.StatusOK)
	flusher.Flush()
	<-request.Context().Done()
}

func (fixture *fixture) preview(writer http.ResponseWriter, request *http.Request) {
	parts := strings.Split(request.URL.Path, "/")
	if len(parts) < 5 {
		http.NotFound(writer, request)
		return
	}
	sessionID := parts[3]
	lines := []string{
		"Agent Harbor Core preview (redacted)",
		"route decision: target_openai selected",
		"tool call: go test ./internal/app/...",
		"result: all focused checks passed",
		"waiting for operator input…",
	}
	writeJSON(writer, http.StatusOK, generated.SessionPreview{SessionId: sessionID, Lines: lines, ObservedAt: fixture.snapshot.PublishedAt, SerializedBytes: len(strings.Join(lines, "\n"))})
}

func (fixture *fixture) hookHealth(writer http.ResponseWriter, request *http.Request) {
	parts := strings.Split(request.URL.Path, "/")
	if len(parts) < 5 {
		http.NotFound(writer, request)
		return
	}
	writeJSON(writer, http.StatusOK, generated.NativeHookHealth{SessionId: parts[3], Provider: generated.NativeHookHealthProviderCodex, State: generated.NativeHookHealthStateActive, ObservedAt: fixture.snapshot.PublishedAt})
}

func writeJSON(writer http.ResponseWriter, status int, value any) {
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(value)
}

func sessionRef(session backend.Session) backend.SessionRef {
	return backend.SessionRef{ID: session.ID, ExpectedUpdatedAt: session.UpdatedAt, CredentialGeneration: session.CredentialGeneration}
}
