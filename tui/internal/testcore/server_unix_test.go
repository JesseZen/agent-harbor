//go:build unix

package testcore

import (
	"context"
	"strings"
	"testing"

	"github.com/asheshgoplani/agent-deck/internal/coreclient"
)

func TestServerExercisesPublicUnixProtocolWithoutCoreSource(t *testing.T) {
	server, err := Start(Options{})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = server.Close() })

	client, err := coreclient.NewUnixBackend(context.Background(), coreclient.Options{
		SocketPath:              server.SocketPath(),
		ExpectedInstanceID:      server.InstanceID(),
		ExpectedProtocolVersion: coreclient.AdminProtocolVersion,
	})
	if err != nil {
		t.Fatalf("NewUnixBackend: %v", err)
	}
	t.Cleanup(client.Close)

	snapshot, err := client.LoadSessions(context.Background())
	if err != nil {
		t.Fatalf("LoadSessions: %v", err)
	}
	if !snapshot.Ready || len(snapshot.Sessions) < 4 || len(snapshot.Routes) < 2 || len(snapshot.Targets) < 3 || len(snapshot.Observations) < 3 {
		t.Fatalf("fixture is incomplete: %#v", snapshot)
	}
	preview, err := client.Preview(context.Background(), sessionRef(snapshot.Sessions[0]))
	if err != nil || len(preview.Lines) < 3 || !strings.Contains(strings.Join(preview.Lines, "\n"), "Agent Harbor") {
		t.Fatalf("Preview = %#v, %v", preview, err)
	}
}
