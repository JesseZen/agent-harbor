package main

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

func accountFixtureServer(t *testing.T, active string, failRefresh bool) (string, <-chan []string) {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "harbor-account-test-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	socket := filepath.Join(dir, "rpc.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	seen := make(chan []string, 1)
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer conn.CloseNow()
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		methods := []string{}
		defer func() { seen <- methods }()
		for {
			var request struct {
				ID     json.RawMessage `json:"id"`
				Method string          `json:"method"`
				Params json.RawMessage `json:"params"`
			}
			if wsjson.Read(ctx, conn, &request) != nil {
				return
			}
			if len(request.ID) == 0 {
				continue
			}
			methods = append(methods, request.Method)
			var result any = map[string]any{}
			switch request.Method {
			case "thread/start":
				var params struct {
					Ephemeral bool `json:"ephemeral"`
					Config    struct {
						Features struct {
							Apps bool `json:"apps"`
						} `json:"features"`
					} `json:"config"`
				}
				if json.Unmarshal(request.Params, &params) != nil || !params.Ephemeral || params.Config.Features.Apps {
					t.Error("refresh did not use an ephemeral apps-disabled thread")
				}
				result = map[string]any{"thread": map[string]any{"id": "fixture-thread"}}
			case "app/list":
				// Notifications may contain a string ID or share the next request's ID.
				_ = wsjson.Write(ctx, conn, map[string]any{"id": "server-request", "method": "fixture/notification"})
				if failRefresh {
					_ = wsjson.Write(ctx, conn, map[string]any{"id": request.ID, "error": map[string]any{"code": -32603}})
					continue
				}
			case "accountSession/list":
				result = map[string]any{"sessions": []any{map[string]any{"userId": "user", "selectedWorkspaceAccountId": active, "isActive": true}}}
			case "account/rateLimits/read":
				result = map[string]any{"accountId": active, "rateLimits": map[string]any{"primary": map[string]any{"usedPercent": 42, "windowDurationMins": 300, "resetsAt": 2000000000}}}
			}
			if wsjson.Write(ctx, conn, map[string]any{"id": request.ID, "result": result}) != nil {
				return
			}
		}
	})}
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() { _ = server.Close() })
	return socket, seen
}

// Optional isolated integration harness: the caller owns the native Codex
// server and synthetic account, and no real credentials enter the test log.
func TestNativeCodexAccountFixture(t *testing.T) {
	socket := os.Getenv("HARBOR_TEST_CODEX_ACCOUNT_SOCKET")
	if socket == "" {
		t.Skip("no isolated native Codex fixture")
	}
	pid, err := strconv.Atoi(os.Getenv("HARBOR_TEST_CODEX_ACCOUNT_PID"))
	if err != nil {
		t.Fatal("fixture server PID is missing")
	}
	s := session{
		CodexServerPID: pid, CodexServerSocket: socket, CodexServerSocketID: codexSocketIdentity(socket),
		RunToken: "synthetic-account-fixture", WorkingDirectory: os.Getenv("HARBOR_TEST_CODEX_ACCOUNT_DIRECTORY"),
	}
	if !verifyCodexServer(s) {
		t.Fatal("fixture server ownership could not be verified")
	}
	quota, err := refreshCodexAccount(s, os.Getenv("HARBOR_TEST_CODEX_ACCOUNT_KEY"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(quota, "5 hours") || !strings.Contains(quota, "weekly") {
		t.Fatalf("expected fixture quota windows, got %q", quota)
	}
}

func TestAccountRefreshVerifiesIdentityAndReadsOfficialQuota(t *testing.T) {
	for _, tc := range []struct {
		name, actual string
		fail         bool
		wantError    bool
	}{
		{"selected account", "account-b", false, false},
		{"different account", "account-a", false, true},
		{"refresh rejected", "account-b", true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			socket, seen := accountFixtureServer(t, tc.actual, tc.fail)
			quota, err := refreshCodexAccountSocket(socket, "/fixture/project", "user::account-b")
			if (err != nil) != tc.wantError {
				t.Fatalf("refresh error = %v", err)
			}
			if !tc.wantError && (!strings.Contains(quota, "42%") || !strings.Contains(quota, "5 hours")) {
				t.Fatalf("quota = %q", quota)
			}
			methods := <-seen
			joined := strings.Join(methods, ",")
			if !strings.HasPrefix(joined, "initialize,thread/start,app/list") {
				t.Fatalf("unexpected requests: %s", joined)
			}
			if tc.wantError && strings.Contains(joined, "account/rateLimits/read") {
				t.Fatalf("quota read after unverified selection: %s", joined)
			}
		})
	}
}
