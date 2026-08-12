//go:build unix

package coreclient

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/asheshgoplani/agent-deck/internal/coreclient/generated"
)

func TestUnixBackendLoadsGeneratedAgentSessionsWithoutProjection(t *testing.T) {
	socketPath := shortSocketPath(t)
	identity := testIdentity(socketPath, testInstanceID, AdminProtocolVersion)
	updatedAt := time.Unix(1_700_000_200, 0).UTC()
	serveUnixHTTP(t, socketPath, http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/v1/readiness":
			writeJSON(t, writer, generated.Readiness{Ready: true, Identity: identity})
		case "/v1/sessions":
			if request.URL.Query().Get("instance_id") != testInstanceID {
				http.Error(writer, "missing instance identity", http.StatusBadRequest)
				return
			}
			writeJSON(t, writer, []generated.AgentSession{{
				Id:             "ses_bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
				Label:          "Authoritative session",
				Lifecycle:      generated.AgentSessionLifecycleRunning,
				NativeActivity: generated.AgentSessionNativeActivityWaiting,
				ActivitySource: generated.SessionActivitySourceHook,
				UpdatedAt:      updatedAt,
			}})
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

	sessions, err := client.LoadAgentSessions(context.Background())
	if err != nil {
		t.Fatalf("LoadAgentSessions: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("sessions = %d, want 1", len(sessions))
	}
	got := sessions[0]
	if got.Lifecycle != generated.AgentSessionLifecycleRunning ||
		got.NativeActivity != generated.AgentSessionNativeActivityWaiting ||
		got.ActivitySource != generated.SessionActivitySourceHook ||
		!got.UpdatedAt.Equal(updatedAt) {
		t.Fatalf("generated session was projected or changed: %#v", got)
	}
}
