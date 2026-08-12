//go:build unix

package coreclient

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/asheshgoplani/agent-deck/internal/coreclient/generated"
)

func TestUnixBackendStreamsIdentityCheckedInvalidations(t *testing.T) {
	socketPath := shortSocketPath(t)
	identity := testIdentity(socketPath, testInstanceID, AdminProtocolVersion)
	serveUnixHTTP(t, socketPath, http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/v1/readiness" {
			writeJSON(t, writer, generated.Readiness{Ready: true, Identity: identity})
			return
		}
		if request.URL.Path != "/v1/events" || request.Header.Get("Last-Event-ID") != "9" {
			t.Errorf("event request = %s header=%q", request.URL.Path, request.Header.Get("Last-Event-ID"))
			http.NotFound(writer, request)
			return
		}
		writer.Header().Set("Content-Type", "text/event-stream")
		writer.WriteHeader(http.StatusOK)
		flusher, ok := writer.(http.Flusher)
		if !ok {
			t.Fatal("test server has no flusher")
		}
		flusher.Flush()
		_, _ = writer.Write([]byte("id: 10\nevent: session\ndata: {\"id\":\"10\",\"instance_id\":\"" + testInstanceID + "\",\"occurred_at\":\"2026-07-25T00:00:00Z\",\"snapshot_generation\":7,\"type\":\"session\"}\n\n"))
		flusher.Flush()
		<-request.Context().Done()
	}))

	client, err := NewUnixBackend(context.Background(), Options{SocketPath: socketPath, ExpectedInstanceID: testInstanceID, ExpectedProtocolVersion: AdminProtocolVersion})
	if err != nil {
		t.Fatalf("NewUnixBackend: %v", err)
	}
	t.Cleanup(client.Close)
	ctx, cancel := context.WithCancel(context.Background())
	invalidations, err := client.WatchInvalidations(ctx, "9")
	if err != nil {
		t.Fatalf("WatchInvalidations: %v", err)
	}
	select {
	case event := <-invalidations:
		if event.EventID != "10" || event.Type != "session" {
			t.Fatalf("invalidation = %#v", event)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for invalidation")
	}
	cancel()
	select {
	case _, open := <-invalidations:
		if open {
			t.Fatal("invalidation stream remained open after cancellation")
		}
	case <-time.After(time.Second):
		t.Fatal("invalidation stream did not close after cancellation")
	}
}
