package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func optionNumber(value int64) *int64 { return &value }

func TestCodexOptionArgumentsKeepInheritanceAndExplicitZero(t *testing.T) {
	directory := filepath.Join(t.TempDir(), `folder 'and' "quotes"`)
	if err := os.Mkdir(directory, 0700); err != nil {
		t.Fatal(err)
	}
	options := codexOptions{SandboxMode: "workspace-write", ApprovalPolicy: "never", ReasoningEffort: "high", ContextWindow: optionNumber(128000), PostTurnCompactPercent: optionNumber(0), WritableDirs: []string{directory}}
	if err := options.validate("codex", true); err != nil {
		t.Fatal(err)
	}
	wantTUI := []string{"-c", "model_context_window=128000", "-c", "model_post_turn_compact_threshold_percent=0", "-c", `model_reasoning_effort="high"`, "--sandbox", "workspace-write", "--ask-for-approval", "never", "--add-dir", directory}
	if actual := options.arguments(false, nil); !slices.Equal(actual, wantTUI) {
		t.Fatalf("TUI arguments: %q", actual)
	}
	server := options.arguments(true, []string{"/inherited", directory})
	if slices.Contains(server, "--sandbox") || slices.Contains(server, "--add-dir") || !slices.Contains(server, `sandbox_mode="workspace-write"`) || !slices.Contains(server, `approval_policy="never"`) {
		t.Fatalf("App Server arguments: %q", server)
	}
	var dirs []string
	if err := json.Unmarshal([]byte(strings.TrimPrefix(server[len(server)-1], "sandbox_workspace_write.writable_roots=")), &dirs); err != nil || !slices.Equal(dirs, []string{"/inherited", directory}) {
		t.Fatalf("writable roots: %q %v", dirs, err)
	}
	if actual := (codexOptions{}).arguments(false, nil); len(actual) != 0 {
		t.Fatalf("inherited settings changed invocation: %q", actual)
	}
	for _, invalid := range []struct {
		options codexOptions
		field   string
	}{
		{codexOptions{SandboxMode: "unsafe"}, "Sandbox"},
		{codexOptions{ApprovalPolicy: "arbitrary"}, "Approval"},
		{codexOptions{ContextWindow: optionNumber(0)}, "Context window"},
		{codexOptions{AutoCompactTokenLimit: optionNumber(-1)}, "Auto compact"},
		{codexOptions{PostTurnCompactPercent: optionNumber(101)}, "Post-turn compact"},
		{codexOptions{WritableDirs: []string{"relative"}}, "Writable dirs"},
		{codexOptions{WritableDirs: []string{directory, directory}}, "Writable dirs"},
		{codexOptions{SandboxMode: "read-only", WritableDirs: []string{directory}}, "Writable dirs"},
	} {
		var err *codexOptionError
		if !errors.As(invalid.options.validate("codex", true), &err) || err.field != invalid.field {
			t.Errorf("wrong validation for %+v: %v", invalid.options, err)
		}
	}
	if options.validate("other", true) == nil {
		t.Fatal("Codex arguments accepted for another client")
	}
}

func TestCodexOptionMemoryIsScopedAndIndependentOfSessions(t *testing.T) {
	service, _ := profileFixture(t)
	profileA, profileB := mustProfile(t, service, "A"), mustProfile(t, service, "B")
	bindingA, bindingB := bindingFor(profileA), bindingFor(profileB)
	options := codexOptions{ContextWindow: optionNumber(12000), ApprovalPolicy: "on-request", PostTurnCompactPercent: optionNumber(0)}
	if err := service.RememberCodexOptions(bindingA, options); err != nil {
		t.Fatal(err)
	}
	project := t.TempDir()
	record, err := createSessionIntent(sessionDraft{Client: "codex", Profile: &bindingA, Connection: "local", Directory: project, CodexOptions: options.saved()}, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	other, err := (harborService{}).ProfileEnvironment(bindingB)
	if err != nil || !other.CodexOptions.empty() {
		t.Fatalf("another profile inherited overrides: %+v %v", other.CodexOptions, err)
	}
	reopened, err := (harborService{}).ProfileEnvironment(bindingA)
	if err != nil || !reflect.DeepEqual(reopened.CodexOptions, options) {
		t.Fatalf("reopened profile lost options: %+v %v", reopened.CodexOptions, err)
	}
	for _, binding := range []profileBinding{
		{WorkspaceID: bindingB.ProfileID, ProfileID: bindingA.ProfileID, EnvironmentID: bindingA.EnvironmentID, Client: "codex"},
		{WorkspaceID: bindingA.WorkspaceID, ProfileID: bindingA.ProfileID, EnvironmentID: bindingB.EnvironmentID, Client: "codex"},
		{WorkspaceID: bindingA.WorkspaceID, ProfileID: bindingA.ProfileID, EnvironmentID: bindingA.EnvironmentID, Client: "other"},
	} {
		if service.RememberCodexOptions(binding, options) == nil {
			t.Fatalf("wrote memory for unproven binding: %+v", binding)
		}
	}
	if err := service.RememberCodexOptions(bindingA, codexOptions{}); err != nil {
		t.Fatal(err)
	}
	reopened, err = service.ProfileEnvironment(bindingA)
	if err != nil || !reopened.CodexOptions.empty() {
		t.Fatalf("reset did not persist: %+v %v", reopened.CodexOptions, err)
	}
	persisted, err := getSession(record.ID)
	if err != nil || !reflect.DeepEqual(persisted.CodexOptions, options.saved()) {
		t.Fatalf("memory reset rewrote saved session: %+v %v", persisted.CodexOptions, err)
	}
}

func TestCodexWindowReportsOnlyCompleteUsageFromTheSelectedRun(t *testing.T) {
	home, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	const thread = "01912345-6789-7abc-8def-0123456789ab"
	path := filepath.Join(home, "sessions", "rollout-"+thread+".jsonl")
	if err := os.Mkdir(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	data := `{"timestamp":"2026-11-19T02:00:00Z","type":"event_msg","payload":{"type":"token_count","info":{"model_context_window":22800}}}
{"timestamp":"2026-11-19T03:00:00Z","type":"event_msg","payload":{"type":"token_count","info":{"model_context_window":11400}}}
{"timestamp":"2026-11-19T04:00:00Z","type":"event_msg","payload":{"type":"token_count","info":{"model_context_window":50000}}}`
	if err := os.WriteFile(path, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	started := time.Date(2026, 11, 19, 2, 30, 0, 0, time.UTC)
	if observed := lastCodexWindow(home, path, thread, started); observed != 11400 {
		t.Fatalf("unexpected observed window: %d", observed)
	}
	for _, attempt := range []struct {
		home, thread string
		since        time.Time
	}{{home, thread, started.Add(time.Hour)}, {home, "other", started}, {t.TempDir(), thread, started}} {
		if observed := lastCodexWindow(attempt.home, path, attempt.thread, attempt.since); observed != 0 {
			t.Fatalf("used stale or cross-thread window: %d", observed)
		}
	}
}

type optionUIService struct {
	*fakeApplicationService
	codexOptionService
	saved   codexOptions
	binding profileBinding
}

func (service *optionUIService) RememberCodexOptions(binding profileBinding, options codexOptions) error {
	service.saved, service.binding = options, binding
	return nil
}

func (service *optionUIService) CodexReasoningChoices(sessionDraft) (codexReasoningChoices, error) {
	return codexReasoningChoices{Model: "fixture", Efforts: []string{"low", "high"}}, nil
}

func TestCodexOptionsPageApplyCancelResetAndReasoningChoices(t *testing.T) {
	model, fake := formFixture()
	fixture := &optionUIService{fakeApplicationService: fake}
	model.service = fixture
	binding := profileBinding{WorkspaceID: "workspace", ProfileID: "profile", Client: "codex", EnvironmentID: "environment"}
	model.environment.Profile = &binding
	model.draft.Profile = &binding
	model.formFocus = formOptions
	var state tea.Model = model
	press := func(keys ...string) {
		for _, key := range keys {
			updated, cmd := state.Update(uiKey(key))
			state = updated
			if updated.(harborModel).servicePending && cmd != nil {
				state, _ = updated.Update(cmd())
			}
		}
	}
	press("enter", "enter", "down", "down", "enter", "esc")
	if current := state.(harborModel); current.screen != "new-form" || current.formFocus != formOptions || current.draft.CodexOptions != nil || fixture.binding != (profileBinding{}) {
		t.Fatalf("cancel applied a draft: %+v", current.draft)
	}
	press("enter", "enter", "down", "down", "enter", "down", "enter", "down", "enter", "down", "enter")
	if rendered := cleanUI(state.View()); !strings.Contains(rendered, "fixture") || !strings.Contains(rendered, "high") || strings.Contains(rendered, "medium") {
		t.Fatalf("reasoning did not use advertised choices: %s", rendered)
	}
	press("down", "down", "enter", "down", "enter", "128000", "enter", "down", "down", "enter", "0", "enter")
	for _, size := range [][2]int{{160, 45}, {100, 30}, {80, 24}, {60, 24}, {48, 12}} {
		state, _ = state.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		frame := state.View()
		for _, text := range []string{"Options", "Post-turn compact", "Off (0%)", "Apply", "Cancel"} {
			if !strings.Contains(frame, text) {
				t.Fatalf("%v missing %s: %s", size, text, frame)
			}
		}
		for _, line := range strings.Split(frame, "\n") {
			if ansi.StringWidth(line) > size[0] {
				t.Fatalf("%v overflow: %q", size, line)
			}
		}
	}
	press("ctrl+s")
	current := state.(harborModel)
	if fixture.binding != binding || current.screen != "new-form" || current.formFocus != formOptions || len(fake.Sessions) != 0 {
		t.Fatalf("Apply created a session or lost scope/focus: %s %+v", current.screen, fixture.binding)
	}
	want := codexOptions{SandboxMode: "workspace-write", ApprovalPolicy: "on-request", ReasoningEffort: "high", ContextWindow: optionNumber(128000), PostTurnCompactPercent: optionNumber(0)}
	if !reflect.DeepEqual(fixture.saved, want) || !reflect.DeepEqual(current.draft.CodexOptions, want.saved()) {
		t.Fatalf("Apply saved %+v, draft %+v", fixture.saved, current.draft.CodexOptions)
	}
	press("esc")
	if fixture.saved.empty() {
		t.Fatal("cancelling creation cleared applied settings")
	}
	current = state.(harborModel)
	current.screen = "new-project-loading"
	state, _ = current.Update(appNewReady{generation: current.generation, environment: appEnvironment{Profile: &binding, CodexOptions: fixture.saved, LastConnection: "magpie"}})
	current = state.(harborModel)
	if current.draft.Connection != "magpie" || !reflect.DeepEqual(current.draft.CodexOptions, want.saved()) {
		t.Fatal("Magpie draft did not recall Native options", current.draft)
	}
	current.formFocus = formOptions
	state = current
	press("enter", "ctrl+r", "ctrl+s")
	if !fixture.saved.empty() || state.(harborModel).draft.CodexOptions != nil {
		t.Fatal("Reset did not apply inherited settings")
	}
}

func TestProfileNativeCodexDefaultsVersionIsolationAndProjectPrecedence(t *testing.T) {
	native := os.Getenv("HARBOR_TEST_CODEX_BIN")
	if native == "" {
		t.Skip("set HARBOR_TEST_CODEX_BIN for native Codex configuration RPC")
	}
	bin := t.TempDir()
	if err := os.Symlink(native, filepath.Join(bin, "codex")); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	service, _ := profileFixture(t)
	profileA, profileB := mustProfile(t, service, "A"), mustProfile(t, service, "B")
	bindingA, bindingB := bindingFor(profileA), bindingFor(profileB)
	project, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(project, ".codex"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(project, ".codex/config.toml"), []byte("model_context_window = 24000\n"), 0600); err != nil {
		t.Fatal(err)
	}
	configA := filepath.Join(profileA.Clients[0].Home, "config.toml")
	configB := filepath.Join(profileB.Clients[0].Home, "config.toml")
	if err := os.WriteFile(configA, []byte("# preserve this comment\n[projects."+mustJSON(project)+"]\ntrust_level = \"trusted\"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	initialB, err := service.ProfileCodexDefaults(bindingB, project)
	if err != nil || initialB.Version == "" || !initialB.Saved.empty() {
		t.Fatalf("unset B: %+v %v", initialB, err)
	}
	initial, err := service.ProfileCodexDefaults(bindingA, project)
	if err != nil || initial.Saved.ContextWindow != nil || initial.Resolved.ContextWindow == nil || *initial.Resolved.ContextWindow != 24000 || initial.Origins["model_context_window"].Name.Type != "project" {
		t.Fatalf("initial A: %+v %v", initial, err)
	}
	options := codexOptions{ContextWindow: optionNumber(12000), AutoCompactTokenLimit: optionNumber(10000), PostTurnCompactPercent: optionNumber(0)}
	updated, err := service.SaveProfileCodexDefaults(initial, options)
	if err != nil || !reflect.DeepEqual(updated.Saved, options) || *updated.Resolved.ContextWindow != 24000 {
		t.Fatalf("saved/effective A: %+v %v", updated, err)
	}
	if _, err := service.SaveProfileCodexDefaults(initial, codexOptions{}); !errors.Is(err, errCodexConfigConflict) {
		t.Fatalf("stale save: %v", err)
	}
	if _, err := os.Stat(configB); !os.IsNotExist(err) {
		t.Fatalf("B config created or modified: %v", err)
	}
	if data, err := os.ReadFile(configA); err != nil || !strings.Contains(string(data), "# preserve this comment") {
		t.Fatalf("unrelated TOML discarded: %v", err)
	}
	env, err := service.ResolveProfileClient(profileClientRequest{bindingA})
	if err != nil {
		t.Fatal(err)
	}
	s := session{CLI: "codex", WorkingDirectory: project, Mode: "local", Profile: &bindingA}
	choices, err := service.CodexReasoningChoices(sessionDraft{Directory: project, Profile: &bindingA, Connection: "local", Client: "codex"})
	if err != nil || choices.Model == "" || len(choices.Efforts) == 0 {
		t.Fatalf("real model list did not resolve default: %+v %v", choices, err)
	}
	options.SandboxMode, options.ApprovalPolicy, options.ReasoningEffort = "workspace-write", "on-request", choices.Efforts[0]
	options.WritableDirs = []string{project}
	_, connected, err := codexOptionLaunchPlans(context.Background(), native, env, s, newConversation, nil, options)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(connected, `sandbox_mode="workspace-write"`) {
		t.Fatalf("workspace mode missing from connected launch: %q", connected)
	}
	fullAccess := codexOptions{SandboxMode: "danger-full-access", ApprovalPolicy: "never", ContextWindow: optionNumber(500000)}
	_, connected, err = codexOptionLaunchPlans(context.Background(), native, env, s, newConversation, nil, fullAccess)
	if err != nil {
		t.Fatal(err)
	}
	if err := withCodexConfigPipe(context.Background(), native, env, project, connected, func(rpc *codexConfigPipe) error {
		resolved, err := rpc.config(project)
		if err != nil {
			return err
		}
		if window := configValue[int64](resolved.Config, "model_context_window"); window != 500000 {
			return fmt.Errorf("requested context window = %d, want 500000", window)
		}
		sandbox := configValue[string](resolved.Config, "sandbox_mode")
		approval := configValue[string](resolved.Config, "approval_policy")
		if sandbox != "danger-full-access" || approval != "never" {
			return fmt.Errorf("connected configuration resolved to %q / %q", sandbox, approval)
		}
		// The TUI can send its resolved policy when starting another thread.
		for range 2 {
			var reply struct {
				Sandbox        struct{ Type string }
				ApprovalPolicy string
			}
			if err := rpc.call("thread/start", map[string]any{"cwd": project, "sandbox": sandbox, "approvalPolicy": approval, "ephemeral": true}, &reply); err != nil {
				return err
			}
			if reply.Sandbox.Type != "dangerFullAccess" || reply.ApprovalPolicy != "never" {
				return fmt.Errorf("thread reported %q / %q", reply.Sandbox.Type, reply.ApprovalPolicy)
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	reset, err := service.SaveProfileCodexDefaults(updated, codexOptions{})
	if err != nil || !reset.Saved.empty() || *reset.Resolved.ContextWindow != 24000 {
		t.Fatalf("reset defaults cleared project layer or failed: %+v %v", reset, err)
	}
}

func TestProfileNativeCodexOptionsTriggerPostTurnCompaction(t *testing.T) {
	native := os.Getenv("HARBOR_TEST_CODEX_BIN")
	if native == "" {
		t.Skip("set HARBOR_TEST_CODEX_BIN for native Codex compaction")
	}
	service, _ := profileFixture(t)
	var compactions atomic.Int32
	backend := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPost {
			writer.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(writer, `{"data":[],"models":[]}`)
			return
		}
		body, err := io.ReadAll(io.LimitReader(request.Body, 2<<20))
		if err != nil {
			http.Error(writer, "cannot read synthetic request", http.StatusBadRequest)
			return
		}
		input := 5975
		if strings.Contains(string(body), "HARBOR_ISOLATED_COMPACTION_PROBE") {
			compactions.Add(1)
			input = 95
		}
		message := `{"id":"msg_fixture","type":"message","status":"completed","role":"assistant","content":[{"type":"output_text","text":"ISOLATED_REPLY","annotations":[]}]}`
		response := fmt.Sprintf(`{"id":"response_fixture","object":"response","status":"completed","model":"test-model","output":[%s],"usage":{"input_tokens":%d,"output_tokens":25,"total_tokens":%d}}`, message, input, input+25)
		writer.Header().Set("Content-Type", "text/event-stream")
		for _, event := range []string{`{"type":"response.output_item.done","output_index":0,"item":` + message + `}`, `{"type":"response.completed","response":` + response + `}`} {
			fmt.Fprintf(writer, "data: %s\n\n", event)
		}
	}))
	defer backend.Close()
	for _, percent := range []int64{40, 70, 0} {
		t.Run(fmt.Sprint(percent), func(t *testing.T) {
			profile := mustProfile(t, service, "Probe "+fmt.Sprint(percent))
			binding := bindingFor(profile)
			environment, err := service.ResolveProfileClient(profileClientRequest{binding})
			if err != nil {
				t.Fatal(err)
			}
			config := `model = "test-model"
model_provider = "fixture"
compact_prompt = "HARBOR_ISOLATED_COMPACTION_PROBE"
check_for_update_on_startup = false
[model_providers.fixture]
name = "Fixture"
base_url = "` + backend.URL + `/v1"
wire_api = "responses"
requires_openai_auth = false
supports_websockets = false
`
			path := filepath.Join(environment.Home, "config.toml")
			if err := os.WriteFile(path, []byte(config), 0600); err != nil {
				t.Fatal(err)
			}
			record := session{CLI: "codex", Profile: &binding, Mode: "local", WorkingDirectory: t.TempDir()}
			options := codexOptions{ContextWindow: optionNumber(12000), AutoCompactTokenLimit: optionNumber(10000), PostTurnCompactPercent: &percent}
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			args, _, err := codexOptionLaunchPlans(ctx, native, environment, record, newConversation, nil, options)
			if err != nil {
				t.Fatal(err)
			}
			argv := append([]string{"exec", "--skip-git-repo-check", "--json"}, profilePolicyArguments(environment)...)
			argv = append(append(argv, args...), "Reply to this isolated compaction test")
			cmd := exec.CommandContext(ctx, native, argv...)
			cmd.Dir, cmd.Env = record.WorkingDirectory, environment.Environment
			compactions.Store(0)
			if data, err := cmd.CombinedOutput(); err != nil || !strings.Contains(string(data), "ISOLATED_REPLY") {
				t.Fatalf("native Codex compaction probe: %v\n%s", err, data)
			}
			var want int32
			if percent == 40 {
				want = 1
			}
			if got := compactions.Load(); got != want {
				t.Fatalf("measured %d compaction requests; expected %d", got, want)
			}
			if data, err := os.ReadFile(path); err != nil || string(data) != config {
				t.Fatalf("launch edited Profile defaults: %v", err)
			}
		})
	}
}
