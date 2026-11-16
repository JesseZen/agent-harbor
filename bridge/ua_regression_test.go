package main

import (
	"encoding/json"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// The client's first conversation reference may arrive only after its first
// turn. A verified foreground run is still focusable without asserting that
// its conversation identity has been observed.
func TestFreshManagedRunIsOneFocusableTargetBeforeConversationReport(t *testing.T) {
	root := visibilityTestRoot(t)
	socket := filepath.Join(root, "herdr", "state.sock")
	if err := os.MkdirAll(filepath.Dir(socket), 0700); err != nil {
		t.Fatal(err)
	}
	l, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	t.Setenv("HERDR_SOCKET_PATH", socket)
	const token = "fixture-current-run"
	child := exec.Command("sleep", "30")
	child.Env = replaceEnvironment(os.Environ(), map[string]*string{"AH_SESSION_RUN_TOKEN": new(token)})
	child.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = child.Process.Kill(); _ = child.Wait() }()
	var foreground atomic.Int64
	foreground.Store(int64(child.Process.Pid))
	var focused atomic.Int32
	var renamed atomic.Int32
	var reportConflict atomic.Bool
	go func() {
		for {
			conn, e := l.Accept()
			if e != nil {
				return
			}
			go func() {
				defer conn.Close()
				var req struct {
					Method string `json:"method"`
				}
				if json.NewDecoder(conn).Decode(&req) != nil {
					return
				}
				var result any
				switch req.Method {
				case "agent.list":
					agent := map[string]any{"terminal_id": "term", "workspace_id": "w1", "pane_id": "w1:p1", "tab_id": "w1:t1", "agent": "codex", "agent_status": "idle"}
					if reportConflict.Load() {
						agent["agent_session"] = herdrIdentity{Source: "herdr:codex", Agent: "codex", Kind: "id", Value: "other"}
					}
					result = map[string]any{"agents": []any{agent}}
				case "pane.process_info":
					result = map[string]any{"process_info": map[string]any{"pane_id": "w1:p1", "foreground_process_group_id": foreground.Load()}}
				case "agent.focus":
					focused.Add(1)
					result = map[string]any{}
				case "agent.rename", "pane.rename", "tab.rename":
					renamed.Add(1)
					result = map[string]any{}
				default:
					return
				}
				_ = json.NewEncoder(conn).Encode(map[string]any{"result": result})
			}()
		}
	}()
	s := session{ID: "0123456789abcdef0123456789abcdef", Name: "Fresh", CLI: "codex", Mode: "local", Started: true, ProcessGroup: child.Process.Pid, RunToken: token, HerdrPane: "w1:p1", HerdrTab: "w1:t1", HerdrSocket: socket, HerdrSocketIdentity: socketIdentity(socket)}
	if err := withSessions(func([]session) ([]session, error) { return []session{s}, nil }); err != nil {
		t.Fatal(err)
	}
	h := harborService{}
	views, err := h.List()
	if err != nil || len(views) != 1 {
		t.Fatalf("fresh managed run appeared as duplicate or unavailable: %+v %v", views, err)
	}
	v := views[0]
	raw, _ := json.Marshal(v.Runtime)
	var runtime map[string]any
	_ = json.Unmarshal(raw, &runtime)
	if v.External || !v.Running || v.Runtime.Associated || runtime["run_verified"] != true || v.Runtime.Activity != "idle" {
		t.Fatalf("fresh run lost its process/conversation distinction: %s", raw)
	}
	if strings.Contains(v.Reason, "Legacy") {
		t.Fatalf("new conversation was presented as a legacy record: %s", v.Reason)
	}
	if detail := newHarborModel(h).details(v); !strings.Contains(strings.ToLower(detail), "conversation") || !strings.Contains(strings.ToLower(detail), "unverified") {
		t.Fatalf("conversation uncertainty missing from details: %s", detail)
	}
	if err := h.FocusObserved(v); err != nil || focused.Load() != 1 {
		t.Fatalf("verified foreground run cannot be focused: %v", err)
	}
	if err := validateLaunchState(v.session); err == nil {
		t.Fatal("an unverified live conversation allowed another launch")
	}
	if err := h.RenameObserved(v, "Renamed"); err != nil || renamed.Load() != 3 {
		t.Fatalf("verified foreground run did not rename its Herdr target: %v", err)
	}
	s, err = getSession(s.ID)
	if err != nil {
		t.Fatal(err)
	}
	v, err = h.Get(s.ID)
	if err != nil {
		t.Fatal(err)
	}
	foreground.Store(int64(child.Process.Pid + 1))
	if err := h.FocusObserved(v); err == nil {
		t.Fatal("focused after foreground process changed")
	}
	views, err = h.List()
	if err != nil || len(views) != 2 || !views[1].External {
		t.Fatalf("unverified replacement was claimed: %+v %v", views, err)
	}
	foreground.Store(int64(child.Process.Pid))
	wrong := s
	wrong.RunToken = "another-run"
	_ = withSessions(func([]session) ([]session, error) { return []session{wrong}, nil })
	views, err = h.List()
	if err != nil || len(views) != 2 || !views[1].External {
		t.Fatalf("wrong process token claimed terminal: %+v %v", views, err)
	}
	duplicate := s
	duplicate.ID = "fedcba9876543210fedcba9876543210"
	_ = withSessions(func([]session) ([]session, error) { return []session{s, duplicate}, nil })
	views, err = h.List()
	if err != nil || len(views) != 3 || !views[2].External {
		t.Fatalf("ambiguous records claimed terminal: %+v %v", views, err)
	}
	s.Conversation = &conversationEvidence{Reference: herdrIdentity{Source: "herdr:codex", Agent: "codex", Kind: "id", Value: "original"}, Status: "association_lost"}
	_ = withSessions(func([]session) ([]session, error) { return []session{s}, nil })
	views, err = h.List()
	if err != nil || len(views) != 1 || !views[0].Runtime.RunVerified || views[0].Runtime.Associated {
		t.Fatalf("transport loss was hidden as verified conversation identity: %+v %v", views, err)
	}
	if queryReferenceCheck(views[0].session) == nil {
		t.Fatal("verified foreground run bypassed conversation recovery review")
	}
	s.Conversation = &conversationEvidence{Reference: herdrIdentity{Source: "herdr:codex", Agent: "codex", Kind: "id", Value: "original"}, Status: "conflict"}
	_ = withSessions(func([]session) ([]session, error) { return []session{s}, nil })
	reportConflict.Store(true)
	views, err = h.List()
	if err != nil || len(views) != 2 || !views[1].External {
		t.Fatalf("conflicting conversation claimed terminal: %+v %v", views, err)
	}
}

func TestManagedMagpieManagementDoesNotInheritNativeClientHomes(t *testing.T) {
	root := tempRoot(t)
	t.Setenv("AGENT_HARBOR_BRIDGE_HOME", root)
	t.Setenv("CODEX_HOME", "/native/codex")
	t.Setenv("CLAUDE_CONFIG_DIR", "/native/claude")
	layout := managedLayoutFor(filepath.Join(root, "magpie"))
	probe := verifiedProbe("http://127.0.0.1:3435", layout.DataDir)
	probe.Serves = "serve"
	probe.variables = map[string]string{"HOME": layout.Home, "CODEX_HOME": layout.Home, "CLAUDE_CONFIG_DIR": "/native/claude"}
	decision := planManagement(magpieInstance{ID: managedInstanceID, Kind: "managed", Address: probe.Address, ConfigDir: probe.ConfigDir}, probe, "/managed/magpie")
	if !decision.Allowed {
		t.Fatal(decision.Reason)
	}
	env := environmentValues(mergedManagementEnvironment(&decision))
	if env["CODEX_HOME"] != layout.Home {
		t.Fatalf("managed Magpie inherited native CODEX_HOME: %q", env["CODEX_HOME"])
	}
	if _, ok := env["CLAUDE_CONFIG_DIR"]; ok {
		t.Fatal("managed Magpie inherited native CLAUDE_CONFIG_DIR")
	}
	for _, managedEnv := range [][]string{managedProcessEnvironment(layout, probe.Address, "fixture", time.Now()), managedChildEnvironment(layout)} {
		values := environmentValues(managedEnv)
		if values["CODEX_HOME"] != layout.Home || values["CODEX_SQLITE_HOME"] != layout.Home {
			t.Fatal("managed Magpie process inherited a native Codex home")
		}
		if _, ok := values["CLAUDE_CONFIG_DIR"]; ok {
			t.Fatal("managed Magpie process inherited native Claude configuration")
		}
	}
}

func TestNativeCodexReviewShowsHomeDefaultsWithoutSecrets(t *testing.T) {
	home := tempRoot(t)
	config := "model = \"zcode/test-model\"\nmodel_provider = \"magpie\"\n[model_providers.magpie]\napi_key = \"never-show-this-secret\"\n"
	if err := os.WriteFile(filepath.Join(home, "config.toml"), []byte(config), 0600); err != nil {
		t.Fatal(err)
	}
	summary := nativeCodexConfigSummary([]string{"HOME=" + home, "CODEX_HOME=" + home})
	if !strings.Contains(summary, "provider magpie") || !strings.Contains(summary, "model zcode/test-model") || !strings.Contains(summary, "may override") || strings.Contains(summary, "never-show-this-secret") {
		t.Fatalf("unsafe or misleading native review: %s", summary)
	}
	m, _ := formFixture()
	m.environment.NativeConfig = summary
	if !strings.Contains(m.View(), "provider magpie") {
		t.Fatal("new-session form hid native configuration")
	}
	m.width, m.height = 60, 20
	if !strings.Contains(m.View(), "zcode/test-model") {
		t.Fatal("new-session form hid the native model at a practical terminal size")
	}
	m.action, m.reviewed, m.nativeReview = "query-resume", session{CLI: "codex", Mode: "local"}, summary
	if !strings.Contains(m.confirmView(), "provider magpie") {
		t.Fatal("recovery review hid native configuration")
	}
	m.screen = "confirm"
	if !strings.Contains(strings.Join(m.reviewEvidence(), "\n"), "zcode/test-model") {
		t.Fatal("recovery review hid the native model at a practical terminal size")
	}
	if err := os.WriteFile(filepath.Join(home, "config.toml"), []byte("model = \"test-model\"\nopenai_base_url = \"https://secret-user:secret-pass@example.invalid/v1\"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	summary = nativeCodexConfigSummary([]string{"CODEX_HOME=" + home})
	if !strings.Contains(summary, "provider client default") || !strings.Contains(summary, "OpenAI base URL override is set") || strings.Contains(summary, "secret-") || strings.Contains(summary, "example.invalid") {
		t.Fatalf("base URL override was hidden or exposed: %s", summary)
	}
	if err := os.WriteFile(filepath.Join(home, "config.toml"), []byte("bad = ["), 0600); err != nil {
		t.Fatal(err)
	}
	if got := nativeCodexConfigSummary([]string{"CODEX_HOME=" + home}); !strings.Contains(got, "unavailable") {
		t.Fatalf("invalid config was presented as known: %s", got)
	}
}

func TestNativeRecoveryReviewShowsRouteUncertaintyAndNavigation(t *testing.T) {
	for _, size := range [][2]int{{60, 20}, {100, 30}, {160, 45}} {
		row := sessionView{session: session{
			ID: "native", Name: "Native", WorkingDirectory: "/project", CLI: "codex", Mode: "local", Started: true,
			Conversation: &conversationEvidence{Reference: herdrIdentity{Source: "herdr:codex", Agent: "codex", Kind: "id", Value: "fixture-conversation"}},
		}, Runtime: runtimeObservation{Presence: "historical_exit", Activity: "unknown"}}
		service := &fakeApplicationService{Sessions: []sessionView{row}}
		m := newHarborModel(service)
		m.width, m.height, m.sessions, m.scroll = size[0], size[1], []sessionView{row}, 99
		model := appKey(m, "enter").(harborModel)
		if model.screen != "confirm" || model.action != "query-resume" || model.scroll != 0 {
			t.Fatalf("%v recovery review did not start at the top: %+v", size, model)
		}
		view := cleanUI(strings.Join(model.reviewEvidence(), "\n")) + " " + cleanUI(model.View())
		for _, want := range []string{"Native route or login may have changed", "A provider name does not verify its destination", "Inspect evidence", "Use recorded conversation", "Resume conversation"} {
			if !strings.Contains(view, want) {
				t.Fatalf("%v recovery review hid %q: %s", size, want, view)
			}
		}
		if next, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEnter}); cmd != nil || next.(harborModel).screen != "confirm" {
			t.Fatalf("%v allowed recovery without accepting the uncertainty", size)
		}
		model.scroll = model.scrollLimit()
		model = appKey(model, "esc").(harborModel)
		model = appKey(model, "enter").(harborModel)
		if model.scroll != 0 || !strings.Contains(cleanUI(model.View()), "Native route or login may have changed") {
			t.Fatalf("%v reused the previous review's scroll position", size)
		}
		model.action = "review-reference"
		if !strings.Contains(model.confirmView(), "Native route or login may have changed") {
			t.Fatalf("%v saved identity review hid native connection uncertainty", size)
		}
		model.cancelRefresh()
	}
}

func TestManagedMagpieManagementAcceptsEquivalentSymlinkedState(t *testing.T) {
	root := tempRoot(t)
	state := filepath.Join(root, "state")
	if err := os.MkdirAll(filepath.Join(state, "magpie", "bin", "data"), 0700); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(root, "state-link")
	if err := os.Symlink(state, alias); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AGENT_HARBOR_BRIDGE_HOME", alias)
	probe := verifiedProbe("http://127.0.0.1:3435", filepath.Join(state, "magpie", "bin", "data"))
	probe.Serves = "serve"
	decision := planManagement(magpieInstance{ID: managedInstanceID, Kind: "managed", Address: probe.Address, ConfigDir: probe.ConfigDir}, probe, "/managed/magpie")
	if !decision.Allowed {
		t.Fatalf("equivalent state path was rejected: %s", decision.Reason)
	}
}
