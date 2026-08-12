//go:build unix

package coreclient

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/asheshgoplani/agent-deck/internal/backend"
	"github.com/asheshgoplani/agent-deck/internal/coreclient/generated"
)

const testInstanceID = "ins_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

func TestUnixBackendLoadsOneIdentityConsistentSnapshot(t *testing.T) {
	socketPath := shortSocketPath(t)
	snapshot := generated.SnapshotIdentity{
		ConfigRevision: "cfg-test",
		Generation:     7,
		PublishedAt:    time.Unix(1_700_000_000, 0).UTC(),
	}
	identity := testIdentity(socketPath, testInstanceID, AdminProtocolVersion)
	createdAt := time.Unix(1_700_000_100, 0).UTC()
	updatedAt := time.Unix(1_700_000_200, 0).UTC()
	observedAt := time.Unix(1_700_000_300, 0).UTC()

	requests := make(chan string, 8)
	serveUnixHTTP(t, socketPath, http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requests <- request.URL.Path
		switch request.URL.Path {
		case "/v1/readiness":
			writeJSON(t, writer, generated.Readiness{Ready: true, Identity: identity, Snapshot: snapshot, DatabaseSchemaVersion: 2})
		case "/v1/config":
			managed := []generated.ManagedObject{{
				Id: "rule-main", Name: "Main rule", Kind: generated.ManagedObjectKindTrafficRule,
				Members: []generated.ManagedResourceRef{{Kind: generated.ManagedResourceRefKindClientProfile, Id: "profile_codex"}},
			}}
			writeJSON(t, writer, generated.MutableConfigSnapshot{
				ActiveGeneration: snapshot.Generation,
				ConfigRevision:   snapshot.ConfigRevision,
				InstanceId:       testInstanceID,
				MutableConfig: generated.MutableConfigView{
					ManagedObjects: &managed,
					Instance: generated.MutableInstanceConfig{
						LogLevel: generated.MutableInstanceConfigLogLevelSimple,
					},
					ClientProfiles: []generated.MutableClientProfile{{
						Id:                        "profile_codex",
						Name:                      "Codex",
						Launcher:                  generated.MutableClientProfileLauncherCodex,
						DefaultRouteId:            "route_main",
						ModelProjectionId:         "mp_main",
						Arguments:                 []string{},
						Environment:               []generated.EnvironmentVariableConfig{},
						CompatibilityTransformIds: []generated.ConfigID{},
					}},
					Routes: []generated.RouteConfig{{
						Id:                  "route_main",
						Name:                "Main client",
						BackendSetId:        "backend_main",
						IngressProtocol:     generated.RouteConfigIngressProtocolOpenaiChat,
						MaxAttempts:         2,
						MaxRequestBodyBytes: 33554432,
						ModelPolicyId:       "mp_main",
						RequestDeadlineMs:   60000,
						RetryDeadlineMs:     30000,
						RoutingPolicy:       generated.RouteConfigRoutingPolicyAutomatic,
						StreamIdleTimeoutMs: 30000,
					}},
					QuotaGroups: []generated.QuotaGroupConfig{{
						Id:                 "quota_default",
						Name:               "Default quota",
						Rpm:                60,
						MaxConcurrency:     4,
						ForegroundCapacity: 2,
						BackgroundCapacity: 2,
						ForegroundWeight:   1,
						BackgroundWeight:   1,
						QueueTimeoutMs:     30000,
					}},
				},
			})
		case "/v1/status":
			writeJSON(t, writer, generated.RuntimeStatus{
				Identity: identity,
				Snapshot: snapshot,
				Routes: []generated.RouteStatus{{
					Id: "route_main", Name: "Main client", BackendSetId: "backend_main", Policy: generated.RouteStatusPolicyAutomatic,
					EligibleTargetIds: []string{"target_primary"},
				}},
				Targets: []generated.TargetStatus{{
					Id: "target_primary", Name: "Primary", Adapter: generated.TargetStatusAdapterOpenai, Health: generated.Healthy,
					BaseEligible: true, QuotaGroupId: "quota_default", CredentialAccess: generated.TargetStatusCredentialAccessAllowed,
					CredentialGeneration: 2, TargetGeneration: 3,
				}},
				QuotaGroups: []generated.QuotaGroupStatus{{
					Id: "quota_default", ActiveConcurrency: 1, MaxConcurrency: 4, Rpm: 60,
				}},
				Observations: generated.ObservationStatus{DroppedEvents: 2, LastEventId: "evt_9"},
				RecentObservations: []generated.ObservationRecord{{
					Id: "obs_1", Type: generated.Request, OccurredAt: observedAt, SnapshotGeneration: snapshot.Generation,
					DecisionReason: "selected", RouteId: stringPointer("route_main"), TargetId: stringPointer("target_primary"),
				}},
				Sessions: []generated.AgentSession{{
					Id:              "ses_bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
					Label:           "Core session",
					Workspace:       "/workspace/core",
					RouteId:         "route_main",
					ClientProfileId: "profile_codex",
					Lifecycle:       generated.AgentSessionLifecycleRunning,
					CreatedAt:       createdAt,
					UpdatedAt:       updatedAt,
					SessionCredential: generated.SessionCredentialView{
						Generation: 3,
					},
				}},
			})
		default:
			http.NotFound(writer, request)
		}
	}))

	client, err := NewUnixBackend(context.Background(), Options{
		SocketPath:              socketPath,
		ExpectedInstanceID:      testInstanceID,
		ExpectedProtocolVersion: AdminProtocolVersion,
	})
	if err != nil {
		t.Fatalf("NewUnixBackend: %v", err)
	}
	t.Cleanup(client.Close)

	loaded, err := client.LoadSessions(context.Background())
	if err != nil {
		t.Fatalf("LoadSessions: %v", err)
	}
	if loaded.Generation != snapshot.Generation {
		t.Fatalf("snapshot generation = %d, want %d", loaded.Generation, snapshot.Generation)
	}
	if loaded.Identity.InstanceID != testInstanceID || loaded.Identity.AdminSocket != socketPath || !loaded.Ready || loaded.ConfigRevision != "cfg-test" {
		t.Fatalf("overview = %#v ready=%v revision=%q", loaded.Identity, loaded.Ready, loaded.ConfigRevision)
	}
	if len(loaded.ClientProfiles) != 1 || loaded.ClientProfiles[0].ID != "profile_codex" ||
		loaded.ClientProfiles[0].Launcher != "codex" || loaded.ClientProfiles[0].DefaultRouteID != "route_main" ||
		loaded.ClientProfiles[0].DefaultRouteName != "Main client" {
		t.Fatalf("client profiles = %#v", loaded.ClientProfiles)
	}
	if len(loaded.Routes) != 1 || loaded.Routes[0].ID != "route_main" || loaded.Routes[0].Name != "Main client" || loaded.Routes[0].Policy != "automatic" || len(loaded.Routes[0].EligibleTargetIDs) != 1 {
		t.Fatalf("routes = %#v", loaded.Routes)
	}
	if len(loaded.Targets) != 1 || loaded.Targets[0].ID != "target_primary" || loaded.Targets[0].Health != "healthy" || !loaded.Targets[0].BaseEligible {
		t.Fatalf("targets = %#v", loaded.Targets)
	}
	if len(loaded.QuotaGroups) != 1 || loaded.QuotaGroups[0].Name != "Default quota" || loaded.QuotaGroups[0].MaxConcurrency != 4 {
		t.Fatalf("quota groups = %#v", loaded.QuotaGroups)
	}
	if len(loaded.Observations) != 1 || loaded.Observations[0].ID != "obs_1" || loaded.Observations[0].RouteID != "route_main" || loaded.DroppedEvents != 2 || loaded.LastEventID != "evt_9" {
		t.Fatalf("observations = %#v dropped=%d last=%q", loaded.Observations, loaded.DroppedEvents, loaded.LastEventID)
	}
	if len(loaded.Sessions) != 1 {
		t.Fatalf("sessions = %d, want 1", len(loaded.Sessions))
	}
	got := loaded.Sessions[0]
	if got.ID != "ses_bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb" || got.Title != "Core session" ||
		got.ProjectPath != "/workspace/core" || got.GroupPath != "Main client/route_main" ||
		got.Tool != "codex" || got.Status != backend.StatusRunning ||
		!got.CreatedAt.Equal(createdAt) || !got.LastAccessedAt.Equal(updatedAt) || !got.UpdatedAt.Equal(updatedAt) ||
		got.ClientProfileID != "profile_codex" || got.RouteID != "route_main" || got.CredentialGeneration != 3 {
		t.Fatalf("projected session = %#v", got)
	}

	wantPaths := []string{"/v1/readiness", "/v1/readiness", "/v1/config", "/v1/status"}
	for _, want := range wantPaths {
		select {
		case gotPath := <-requests:
			if gotPath != want {
				t.Fatalf("request path = %q, want %q", gotPath, want)
			}
		case <-time.After(time.Second):
			t.Fatalf("missing request %q", want)
		}
	}
}

func TestUnixBackendRejectsReadinessIdentityAndProtocolMismatch(t *testing.T) {
	tests := []struct {
		name       string
		mutate     func(*generated.InstanceIdentity)
		wantTarget error
	}{
		{name: "instance", mutate: func(identity *generated.InstanceIdentity) {
			identity.InstanceId = "ins_bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
		}, wantTarget: ErrInstanceMismatch},
		{name: "protocol", mutate: func(identity *generated.InstanceIdentity) {
			identity.AdminProtocolVersion = generated.InstanceIdentityAdminProtocolVersion(2)
		}, wantTarget: ErrProtocolIncompatible},
		{name: "socket", mutate: func(identity *generated.InstanceIdentity) { identity.AdminSocket += ".other" }, wantTarget: ErrSocketMismatch},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			socketPath := shortSocketPath(t)
			identity := testIdentity(socketPath, testInstanceID, AdminProtocolVersion)
			test.mutate(&identity)
			serveUnixHTTP(t, socketPath, http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				writeJSON(t, writer, generated.Readiness{Ready: true, Identity: identity})
			}))

			_, err := NewUnixBackend(context.Background(), Options{
				SocketPath:              socketPath,
				ExpectedInstanceID:      testInstanceID,
				ExpectedProtocolVersion: AdminProtocolVersion,
			})
			if !errors.Is(err, test.wantTarget) {
				t.Fatalf("error = %v, want %v", err, test.wantTarget)
			}
		})
	}
}

func TestUnixBackendRejectsMismatchedRuntimeResponseIdentity(t *testing.T) {
	socketPath := shortSocketPath(t)
	snapshot := generated.SnapshotIdentity{Generation: 4}
	identity := testIdentity(socketPath, testInstanceID, AdminProtocolVersion)
	serveUnixHTTP(t, socketPath, http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/v1/readiness":
			writeJSON(t, writer, generated.Readiness{Ready: true, Identity: identity, Snapshot: snapshot})
		case "/v1/config":
			writeJSON(t, writer, generated.MutableConfigSnapshot{
				ActiveGeneration: snapshot.Generation,
				InstanceId:       testInstanceID,
				MutableConfig: generated.MutableConfigView{
					Instance: generated.MutableInstanceConfig{LogLevel: generated.MutableInstanceConfigLogLevelSimple},
				},
			})
		case "/v1/status":
			mismatched := identity
			mismatched.InstanceId = "ins_bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
			writeJSON(t, writer, generated.RuntimeStatus{Identity: mismatched, Snapshot: snapshot})
		}
	}))

	client, err := NewUnixBackend(context.Background(), Options{SocketPath: socketPath, ExpectedInstanceID: testInstanceID, ExpectedProtocolVersion: AdminProtocolVersion})
	if err != nil {
		t.Fatalf("NewUnixBackend: %v", err)
	}
	t.Cleanup(client.Close)
	_, err = client.LoadSessions(context.Background())
	if !errors.Is(err, ErrInstanceMismatch) {
		t.Fatalf("LoadSessions error = %v, want %v", err, ErrInstanceMismatch)
	}
}

func TestUnixBackendRejectsTornSnapshotGeneration(t *testing.T) {
	socketPath := shortSocketPath(t)
	identity := testIdentity(socketPath, testInstanceID, AdminProtocolVersion)
	serveUnixHTTP(t, socketPath, http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/v1/readiness":
			writeJSON(t, writer, generated.Readiness{Ready: true, Identity: identity, Snapshot: generated.SnapshotIdentity{Generation: 4}})
		case "/v1/config":
			writeJSON(t, writer, generated.MutableConfigSnapshot{ActiveGeneration: 5, InstanceId: testInstanceID})
		case "/v1/status":
			writeJSON(t, writer, generated.RuntimeStatus{Identity: identity, Snapshot: generated.SnapshotIdentity{Generation: 4}})
		}
	}))

	client, err := NewUnixBackend(context.Background(), Options{SocketPath: socketPath, ExpectedInstanceID: testInstanceID, ExpectedProtocolVersion: AdminProtocolVersion})
	if err != nil {
		t.Fatalf("NewUnixBackend: %v", err)
	}
	t.Cleanup(client.Close)
	_, err = client.LoadSessions(context.Background())
	if !errors.Is(err, ErrSnapshotMismatch) {
		t.Fatalf("LoadSessions error = %v, want %v", err, ErrSnapshotMismatch)
	}
}

func TestUnixBackendRejectsUnclosedSuccessShape(t *testing.T) {
	socketPath := shortSocketPath(t)
	serveUnixHTTP(t, socketPath, http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "text/plain")
		writer.WriteHeader(http.StatusOK)
		_, _ = writer.Write([]byte("not the declared readiness shape"))
	}))

	_, err := NewUnixBackend(context.Background(), Options{SocketPath: socketPath, ExpectedInstanceID: testInstanceID, ExpectedProtocolVersion: AdminProtocolVersion})
	if !errors.Is(err, ErrProtocolIncompatible) {
		t.Fatalf("error = %v, want %v", err, ErrProtocolIncompatible)
	}
}

func TestUnixBackendPropagatesCancellation(t *testing.T) {
	socketPath := shortSocketPath(t)
	serveUnixHTTP(t, socketPath, http.HandlerFunc(func(_ http.ResponseWriter, request *http.Request) {
		<-request.Context().Done()
	}))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := NewUnixBackend(ctx, Options{SocketPath: socketPath, ExpectedInstanceID: testInstanceID, ExpectedProtocolVersion: AdminProtocolVersion})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context cancellation", err)
	}
}

func TestUnixBackendRejectsNonSocketAndSymlinkPaths(t *testing.T) {
	t.Run("regular file", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "not-a-socket")
		if err := os.WriteFile(path, []byte("not a socket"), 0o600); err != nil {
			t.Fatal(err)
		}
		_, err := NewUnixBackend(context.Background(), Options{SocketPath: path, ExpectedInstanceID: testInstanceID, ExpectedProtocolVersion: AdminProtocolVersion})
		if !errors.Is(err, ErrInvalidSocket) {
			t.Fatalf("error = %v, want %v", err, ErrInvalidSocket)
		}
	})

	t.Run("symlink", func(t *testing.T) {
		directory := shortSocketDir(t)
		target := filepath.Join(directory, "target.sock")
		link := filepath.Join(directory, "admin.sock")
		listener, err := net.Listen("unix", target)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = listener.Close() })
		if err := os.Symlink(target, link); err != nil {
			t.Fatal(err)
		}
		_, err = NewUnixBackend(context.Background(), Options{SocketPath: link, ExpectedInstanceID: testInstanceID, ExpectedProtocolVersion: AdminProtocolVersion})
		if !errors.Is(err, ErrInvalidSocket) {
			t.Fatalf("error = %v, want %v", err, ErrInvalidSocket)
		}
	})
}

func testIdentity(socketPath, instanceID, version string) generated.InstanceIdentity {
	return generated.InstanceIdentity{
		AdminProtocolVersion: generated.N1,
		AdminSocket:          socketPath,
		Binary:               generated.AgentHarborCore,
		InstanceId:           instanceID,
		SupervisorPid:        os.Getpid(),
		Version:              "core-build-" + version,
		AdminTcpEnabled:      false,
	}
}

func stringPointer(value string) *string { return &value }

func shortSocketPath(t *testing.T) string {
	t.Helper()
	return filepath.Join(shortSocketDir(t), "admin.sock")
}

func shortSocketDir(t *testing.T) string {
	t.Helper()
	directory, err := os.MkdirTemp("", "aht-")
	if err != nil {
		t.Fatalf("create short socket directory: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(directory) })
	return directory
}

func serveUnixHTTP(t *testing.T, socketPath string, handler http.Handler) {
	t.Helper()
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatalf("listen on fake Core socket: %v", err)
	}
	server := &http.Server{Handler: handler}
	go func() {
		if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			t.Errorf("serve fake Core: %v", err)
		}
	}()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = server.Shutdown(ctx)
		_ = listener.Close()
	})
}

func writeJSON(t *testing.T, writer http.ResponseWriter, value any) {
	t.Helper()
	writer.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(writer).Encode(value); err != nil {
		t.Errorf("encode fake Core response: %v", err)
	}
}
