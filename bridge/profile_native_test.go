package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// Real client/state evidence, deliberately separate from real-account acceptance.
// Only synthetic API keys and local HTTP endpoints are used here.
func TestProfileNativeCodexStateIsolation(t *testing.T) {
	native := os.Getenv("HARBOR_TEST_CODEX_BIN")
	if native == "" {
		t.Skip("set HARBOR_TEST_CODEX_BIN for native Codex state isolation")
	}
	h, _ := profileFixture(t)
	a, b := mustProfile(t, h, "A"), mustProfile(t, h, "B")
	ab, bb := bindingFor(a), bindingFor(b)
	project := t.TempDir()
	for _, binding := range []profileBinding{ab, bb} {
		if _, err := h.Create(sessionDraft{Directory: project, Connection: "local", Client: "codex", Profile: &binding}); err != nil {
			t.Fatal(err)
		}
	}
	var mu sync.Mutex
	calls := []string{}
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			w.Header().Set("Content-Type", "application/json")
			io.WriteString(w, `{"models":[]}`)
			return
		}
		if !strings.HasSuffix(r.URL.Path, "/responses") {
			http.Error(w, "unexpected endpoint", 404)
			return
		}
		key := r.Header.Get("Authorization")
		if key != "Bearer sk-profile-a" && key != "Bearer sk-profile-b" && key != "Bearer sk-profile-a-next" {
			http.Error(w, "wrong synthetic caller", 401)
			return
		}
		mu.Lock()
		calls = append(calls, key)
		mu.Unlock()
		msg := `{"id":"msg_profile","type":"message","status":"completed","role":"assistant","content":[{"type":"output_text","text":"PROFILE_NATIVE_OK","annotations":[]}]}`
		response := `{"id":"resp_profile","object":"response","status":"completed","model":"gpt-5.5","output":[` + msg + `],"usage":{"input_tokens":10,"output_tokens":5,"total_tokens":15}}`
		w.Header().Set("Content-Type", "text/event-stream")
		for _, event := range []string{`{"type":"response.created","response":{"id":"resp_profile","object":"response","status":"in_progress","output":[]}}`, `{"type":"response.output_item.done","output_index":0,"item":` + msg + `}`, `{"type":"response.completed","response":` + response + `}`} {
			fmt.Fprintf(w, "data: %s\n\n", event)
		}
	}))
	defer backend.Close()
	// Personal settings and grants are independently created, never copied.
	for _, item := range []struct {
		binding profileBinding
		key     string
	}{{ab, "sk-profile-a"}, {bb, "sk-profile-b"}} {
		err := h.WithProfileClient(context.Background(), profileClientRequest{item.binding}, profileAccountMutation, func(scope profileClientScope) error {
			if err := os.WriteFile(filepath.Join(scope.Environment.Home, "auth.json"), []byte(`{"OPENAI_API_KEY":`+strconv.Quote(item.key)+`}`), 0600); err != nil {
				return err
			}
			return os.WriteFile(filepath.Join(scope.Environment.Home, "config.toml"), []byte("model = \"gpt-5.5\"\nmodel_context_window = 1000000\nmodel_auto_compact_token_limit = 900000\ncheck_for_update_on_startup = false\ncli_auth_credentials_store = \"file\"\nmcp_oauth_credentials_store = \"file\"\n"), 0600)
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	run := func(binding profileBinding, thread, prompt string) (string, error) {
		result := ""
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()
		err := h.WithProfileClient(ctx, profileClientRequest{binding}, profileLaunch, func(scope profileClientScope) error {
			args := []string{"exec", "--skip-git-repo-check", "--dangerously-bypass-approvals-and-sandbox", "--json", "-c", "openai_base_url=" + strconv.Quote(backend.URL+"/backend-api/codex")}
			args = append(args, profilePolicyArguments(scope.Environment)...)
			if thread != "" {
				args = append(args, "resume", thread)
			} else {
				args = append(args, "--model", "gpt-5.5")
			}
			args = append(args, prompt)
			cmd := exec.CommandContext(ctx, native, args...)
			cmd.Dir = project
			noProxy := "127.0.0.1,localhost"
			cmd.Env = replaceEnvironment(scope.Environment.Environment, map[string]*string{"NO_PROXY": &noProxy})
			output, err := cmd.CombinedOutput()
			if err != nil {
				return fmt.Errorf("native client failed: %w: %s", err, output)
			}
			if !strings.Contains(string(output), "PROFILE_NATIVE_OK") {
				return fmt.Errorf("missing native reply: %s", output)
			}
			for _, line := range strings.Split(string(output), "\n") {
				var event struct {
					Type     string `json:"type"`
					ThreadID string `json:"thread_id"`
				}
				if json.Unmarshal([]byte(line), &event) == nil && event.Type == "thread.started" {
					result = event.ThreadID
				}
			}
			if result == "" {
				return fmt.Errorf("native client did not identify its Conversation: %s", output)
			}
			return nil
		})
		return result, err
	}
	type outcome struct {
		id  string
		err error
	}
	ach, bch := make(chan outcome, 1), make(chan outcome, 1)
	go func() { id, err := run(ab, "", "PROFILE_A_ONLY"); ach <- outcome{id, err} }()
	go func() { id, err := run(bb, "", "PROFILE_B_ONLY"); bch <- outcome{id, err} }()
	ar, br := <-ach, <-bch
	if ar.err != nil || br.err != nil {
		t.Fatalf("A=%v B=%v", ar.err, br.err)
	}
	if ar.id == br.id {
		t.Fatal("Profiles merged Conversations")
	}
	be, _ := h.ResolveProfileClient(profileClientRequest{bb})
	before, _ := os.ReadFile(filepath.Join(be.Home, "auth.json"))
	if err := h.WithProfileClient(context.Background(), profileClientRequest{ab}, profileAccountMutation, func(scope profileClientScope) error {
		return os.WriteFile(filepath.Join(scope.Environment.Home, "auth.json"), []byte(`{"OPENAI_API_KEY":"sk-profile-a-next"}`), 0600)
	}); err != nil {
		t.Fatal(err)
	}
	id, err := run(ab, ar.id, "PROFILE_A_RESUMED")
	if err != nil || id != ar.id {
		t.Fatalf("A recovery changed Conversation: %s %v", id, err)
	}
	after, _ := os.ReadFile(filepath.Join(be.Home, "auth.json"))
	if string(before) != string(after) {
		t.Fatal("A mutation changed B login")
	}
	mu.Lock()
	recorded := append([]string{}, calls...)
	mu.Unlock()
	if len(recorded) < 3 || recorded[len(recorded)-1] != "Bearer sk-profile-a-next" {
		t.Fatal("subsequent request did not use A's changed identity", recorded)
	}
	for _, item := range []struct {
		binding    profileBinding
		own, other string
	}{{ab, "PROFILE_A_ONLY", "PROFILE_B_ONLY"}, {bb, "PROFILE_B_ONLY", "PROFILE_A_ONLY"}} {
		e, _ := h.ResolveProfileClient(profileClientRequest{item.binding})
		found := false
		databases := 0
		filepath.WalkDir(e.Home, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if !d.IsDir() && strings.HasSuffix(path, ".jsonl") {
				data, _ := os.ReadFile(path)
				if strings.Contains(string(data), item.other) {
					t.Error("other Profile history entered client home")
				}
				found = found || strings.Contains(string(data), item.own)
			}
			return nil
		})
		filepath.WalkDir(e.SQLite, func(path string, d os.DirEntry, err error) error {
			if err == nil && !d.IsDir() && strings.HasSuffix(path, ".sqlite") {
				databases++
			}
			return nil
		})
		if !found || databases == 0 {
			t.Fatalf("native history/database not retained in bound roots: history=%v databases=%d", found, databases)
		}
	}
}

func TestProfileNativeCodexOwnedServerIsolation(t *testing.T) {
	native := os.Getenv("HARBOR_TEST_CODEX_BIN")
	if native == "" {
		t.Skip("set HARBOR_TEST_CODEX_BIN for native App Server isolation")
	}
	h, _ := profileFixture(t)
	a, b := mustProfile(t, h, "A"), mustProfile(t, h, "B")
	bindings := []profileBinding{bindingFor(a), bindingFor(b)}
	var records []session
	for index, binding := range bindings {
		e, err := h.ResolveProfileClient(profileClientRequest{binding})
		if err != nil {
			t.Fatal(err)
		}
		token := fmt.Sprintf("synthetic-profile-server-%d", index)
		env := append(profileProcessEnvironment(e.Environment, &binding), "AH_SESSION_RUN_TOKEN="+token)
		// Startup remains entirely offline; account state is deliberately empty.
		proxy := "http://127.0.0.1:9"
		env = replaceEnvironment(env, map[string]*string{"HTTP_PROXY": &proxy, "HTTPS_PROXY": &proxy, "ALL_PROXY": &proxy})
		server, err := startOwnedCodexServer(native, profilePolicyArguments(e), t.TempDir(), env)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(server.Close)
		record := session{CLI: "codex", Profile: &binding, CodexHome: e.Home, RunToken: token, CodexServerPID: server.cmd.Process.Pid, CodexServerSocket: server.socket, CodexServerSocketID: server.socketID}
		if !verifyCodexServer(record) {
			t.Fatal("bound App Server ownership not verified")
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		rpc, err := connectCodexRPC(ctx, server.socket)
		if err != nil {
			cancel()
			t.Fatal(err)
		}
		var account map[string]any
		err = rpc.call(ctx, "account/read", map[string]bool{"refreshToken": false}, &account)
		rpc.conn.CloseNow()
		cancel()
		if err != nil {
			t.Fatal(err)
		}
		if account["account"] != nil {
			t.Fatal("empty Profile inherited an account")
		}
		records = append(records, record)
	}
	if records[0].CodexServerPID == records[1].CodexServerPID || records[0].CodexServerSocketID == records[1].CodexServerSocketID {
		t.Fatal("A/B shared an App Server")
	}
	wrong := records[0]
	wrong.Profile = records[1].Profile
	if verifyCodexServer(wrong) {
		t.Fatal("A server accepted B Profile identity")
	}
}
