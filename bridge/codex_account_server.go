package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

// Each experimental session owns a private official App Server. Its socket is
// published only in that run's Harbor record, never in the user's Codex config.
type ownedCodexServer struct {
	cmd       *exec.Cmd
	directory string
	socket    string
	socketID  string
	done      chan error
	closeOnce sync.Once
}

func codexSocketIdentity(path string) string {
	info, err := os.Stat(path) // Codex may expose its socket through a symlink.
	if err != nil || info.Mode()&os.ModeSocket == 0 {
		return ""
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return ""
	}
	return fmt.Sprintf("%d:%d", stat.Dev, stat.Ino)
}

func startOwnedCodexServer(binary string, settings []string, directory string, env []string) (*ownedCodexServer, error) {
	tmp, err := os.MkdirTemp("", "ah-codex-")
	if err != nil {
		return nil, errors.New("could not create the private Codex App Server directory")
	}
	s := &ownedCodexServer{directory: tmp, socket: filepath.Join(tmp, "rpc.sock"), done: make(chan error, 1)}
	args := append([]string{"app-server", "--listen", "unix://" + s.socket, "-c", "check_for_update_on_startup=false"}, settings...)
	s.cmd = exec.Command(binary, args...)
	s.cmd.Dir, s.cmd.Env = directory, env
	if err := s.cmd.Start(); err != nil {
		_ = os.RemoveAll(tmp)
		return nil, errors.New("could not start the experimental Codex App Server")
	}
	go func() { s.done <- s.cmd.Wait() }()
	deadline := time.NewTimer(15 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-s.done:
			_ = os.RemoveAll(tmp)
			return nil, errors.New("experimental Codex App Server exited before it was ready")
		case <-deadline.C:
			s.Close()
			return nil, errors.New("experimental Codex App Server did not become ready")
		case <-ticker.C:
			if s.socketID = codexSocketIdentity(s.socket); s.socketID != "" {
				return s, nil
			}
		}
	}
}

func (s *ownedCodexServer) Close() {
	s.closeOnce.Do(func() {
		_ = s.cmd.Process.Signal(syscall.SIGTERM)
		select {
		case <-s.done:
		case <-time.After(3 * time.Second):
			_ = s.cmd.Process.Kill()
			<-s.done
		}
		_ = os.RemoveAll(s.directory)
	})
}

// Pane closure may terminate the Harbor runner before it can run its defers.
func watchCodexServerSignals(s *ownedCodexServer) func() {
	ch, stop := make(chan os.Signal, 1), make(chan struct{})
	signal.Notify(ch, syscall.SIGHUP, syscall.SIGTERM)
	go func() {
		select {
		case sig := <-ch:
			s.Close()
			if n, ok := sig.(syscall.Signal); ok {
				os.Exit(128 + int(n))
			}
			os.Exit(1)
		case <-stop:
		}
	}()
	return func() { signal.Stop(ch); close(stop) }
}

type codexRPC struct {
	conn *websocket.Conn
	id   int
}

func connectCodexRPC(ctx context.Context, socket string) (*codexRPC, error) {
	transport := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", socket)
	}}
	conn, _, err := websocket.Dial(ctx, "ws://localhost/rpc", &websocket.DialOptions{HTTPClient: &http.Client{Transport: transport}})
	if err != nil {
		return nil, errors.New("could not connect to the session's Codex App Server")
	}
	conn.SetReadLimit(1 << 20)
	rpc := &codexRPC{conn: conn}
	if err := rpc.call(ctx, "initialize", map[string]any{"clientInfo": map[string]string{"name": "agent-harbor", "version": version}, "capabilities": map[string]bool{"experimentalApi": true}}, nil); err != nil {
		conn.CloseNow()
		return nil, err
	}
	if err := wsjson.Write(ctx, conn, map[string]any{"method": "initialized", "params": map[string]any{}}); err != nil {
		conn.CloseNow()
		return nil, errors.New("could not initialize the session's Codex App Server")
	}
	return rpc, nil
}

func (r *codexRPC) call(ctx context.Context, method string, params any, result any) error {
	r.id++
	id := r.id
	if err := wsjson.Write(ctx, r.conn, map[string]any{"id": id, "method": method, "params": params}); err != nil {
		return fmt.Errorf("Codex %s request failed", method)
	}
	for {
		var response struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
			Result json.RawMessage `json:"result"`
			Error  json.RawMessage `json:"error"`
		}
		if err := wsjson.Read(ctx, r.conn, &response); err != nil {
			return fmt.Errorf("Codex %s response unavailable", method)
		}
		var responseID int
		if response.Method != "" || json.Unmarshal(response.ID, &responseID) != nil || responseID != id {
			continue // Notifications and unrelated server requests contain no switch result.
		}
		if len(response.Error) > 0 && string(response.Error) != "null" {
			return fmt.Errorf("Codex %s was not accepted", method)
		}
		if result != nil && json.Unmarshal(response.Result, result) != nil {
			return fmt.Errorf("Codex %s returned an unexpected response", method)
		}
		return nil
	}
}

func verifyCodexServer(s session) bool {
	if s.CodexServerPID <= 1 || s.RunToken == "" || s.CodexServerSocketID == "" || codexSocketIdentity(s.CodexServerSocket) != s.CodexServerSocketID {
		return false
	}
	facts, err := readProcessFacts(s.CodexServerPID)
	return err == nil && facts.Variables["AH_SESSION_RUN_TOKEN"] == s.RunToken
}
