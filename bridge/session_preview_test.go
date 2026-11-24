package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
)

func previewFixture(id string) sessionView {
	return sessionView{session: session{ID: id, Name: id, CLI: "codex", Started: true, ProcessGroup: 80, RunToken: "run-" + id,
		HerdrSocket: "socket", HerdrSocketIdentity: "instance", HerdrPane: "pane-" + id, HerdrTab: "tab", WorkingDirectory: "/fixtures/project"},
		Runtime: runtimeObservation{Presence: "observed", Activity: "working", RunVerified: true, Terminal: "term-" + id, Workspace: "workspace", Instance: "instance"}, Running: true}
}

type previewServiceFixture struct {
	fakeApplicationService
	reads []previewTarget
}

func (service *previewServiceFixture) ReadPreview(_ context.Context, target previewTarget) (previewSnapshot, error) {
	service.reads = append(service.reads, target)
	return previewSnapshot{lines: []string{"frame-" + target.id}}, nil
}

func TestPreviewGenerationAndNarrowLayout(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	service := &previewServiceFixture{}
	model := newHarborModel(service)
	t.Cleanup(model.cancelRefresh)
	model.sessions = []sessionView{previewFixture("alpha"), previewFixture("beta")}
	step := func(event tea.Msg) tea.Cmd {
		next, cmd := model.Update(event)
		model = next.(harborModel)
		return cmd
	}
	step(tea.WindowSizeMsg{Width: 102, Height: 30})
	first := model.preview.generation
	firstCtx := model.preview.ctx
	readAlpha := step(previewDue(first))
	step(uiKey("down"))
	if firstCtx.Err() == nil || model.preview.target.id != "beta" || len(model.preview.frame.lines) != 0 {
		t.Fatal("moving to beta did not immediately clear alpha")
	}
	step(previewDue(model.preview.generation))
	step(uiKey("up"))
	latest := model.preview.generation
	if cmd := step(previewDue(latest)); cmd != nil || first == latest {
		t.Fatal("new selection started a second in-flight read")
	}
	readLatest := step(readAlpha())
	if len(model.preview.frame.lines) != 0 || readLatest == nil {
		t.Fatal("late alpha response was applied, or current alpha never scheduled")
	}
	step(readLatest())
	if !strings.Contains(model.View(), "frame-alpha") || len(service.reads) != 2 {
		t.Fatal("current alpha did not render")
	}
	step(tea.WindowSizeMsg{Width: 88, Height: 30})
	if cmd := step(previewDue(latest)); cmd != nil || model.preview.target.id != "" || strings.Contains(model.View(), "frame-alpha") || model.cursor != 0 {
		t.Fatal("narrow view leaked or polled preview")
	}
	step(tea.WindowSizeMsg{Width: 102, Height: 30})
	model.preview.frame.lines = []string{"DISCARDED-TERMINAL"}
	model.sessions[0].Runtime.Terminal = "replacement"
	step(nil)
	if model.preview.target.terminal != "replacement" || strings.Contains(model.View(), "DISCARDED-TERMINAL") {
		t.Fatal("terminal replacement retained the previous screen")
	}
	step(uiKey("?"))
	if model.preview.target.id != "" || model.screen != "help" {
		t.Fatal("preview persisted on Help")
	}
	step(appLoaded{sessions: model.sessions, generation: model.listGeneration})
	if model.preview.target.id != "" || model.motion.cancel != nil {
		t.Fatal("background refresh started work under Help")
	}
	step(uiKey("esc"))
	if model.preview.target.id != "alpha" || len(model.preview.frame.lines) != 0 {
		t.Fatal("returning from Help did not verify a new preview")
	}
}

func TestPreviewReadsOnlyVerifiedPane(t *testing.T) {
	for _, scenario := range []string{"managed", "external", "unverified", "wrong coordinates", "changed terminal", "changed run", "changed profile", "new socket", "disconnected", "oversize", "unsupported", "cancelled"} {
		t.Run(scenario, func(t *testing.T) {
			path, err := os.MkdirTemp("", "pv-")
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = os.RemoveAll(path) })
			socketPath := filepath.Join(path, "p.sock")
			server, err := net.Listen("unix", socketPath)
			if err != nil {
				t.Fatal(err)
			}
			defer server.Close()
			view := previewFixture("alpha")
			view.HerdrSocket, view.HerdrSocketIdentity = socketPath, socketIdentity(socketPath)
			view.Runtime.Instance = view.HerdrSocketIdentity
			if scenario == "external" || scenario == "unverified" {
				view.Runtime.RunVerified = false
				view.External = scenario == "external"
			}
			target, reason := targetForPreview(view)
			if scenario == "unverified" {
				if reason == "" {
					t.Fatal("allowed unverified target")
				}
				return
			}
			if reason != "" {
				t.Fatal(reason)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			requests := make(chan map[string]any, 1)
			go func() {
				conn, err := server.Accept()
				if err != nil {
					return
				}
				defer conn.Close()
				var request map[string]any
				if json.NewDecoder(bufio.NewReader(conn)).Decode(&request) != nil {
					return
				}
				requests <- request
				body := map[string]any{"workspace_id": "workspace", "tab_id": "tab", "pane_id": target.pane, "source": "visible", "format": "ansi", "text": "visible frame", "truncated": false}
				switch scenario {
				case "wrong coordinates":
					body["tab_id"] = "old tab"
				case "oversize":
					body["text"] = strings.Repeat("x", 65536)
				case "unsupported":
					body["format"] = "text"
				case "cancelled":
					cancel()
					return
				}
				_ = json.NewEncoder(conn).Encode(map[string]any{"id": request["id"], "result": map[string]any{"read": body}})
			}()
			observations := 0
			frame, err := readSessionPreview(ctx, target, func(context.Context, string) (sessionView, error) {
				observations++
				if observations == 2 {
					switch scenario {
					case "changed terminal":
						view.Runtime.Terminal = "reused"
					case "changed run":
						view.RunToken = "replacement-run"
					case "changed profile":
						view.Profile = &profileBinding{ProfileID: "changed"}
					case "disconnected":
						view.DiscoveryWarning = agentDiscoveryWarning
					case "new socket":
						if err := os.Rename(socketPath, socketPath+".old"); err != nil {
							t.Fatal(err)
						}
						replacement, err := net.Listen("unix", socketPath)
						if err != nil {
							t.Fatal(err)
						}
						t.Cleanup(func() { replacement.Close() })
					}
				}
				return view, nil
			})
			select {
			case request := <-requests:
				params := request["params"].(map[string]any)
				if request["method"] != "pane.read" || params["pane_id"] != target.pane || params["source"] != "visible" || params["format"] != "ansi" || params["strip_ansi"] != false || len(params) != 4 {
					t.Fatal(request)
				}
			default:
				t.Fatal("no pane.read request", err)
			}
			want := map[string]error{"wrong coordinates": errPreviewTarget, "changed terminal": errPreviewTarget, "changed run": errPreviewTarget, "changed profile": errPreviewTarget, "new socket": errPreviewTarget, "disconnected": errPreviewRead, "oversize": errPreviewLarge, "unsupported": errPreviewUnsupported, "cancelled": errPreviewRead}[scenario]
			if !errors.Is(err, want) || (want != nil && len(frame.lines) > 0) || (want == nil && (observations != 2 || strings.Join(frame.lines, "") != "visible frame")) {
				t.Fatalf("frame=%+v err=%v, want %v", frame, err, want)
			}
		})
	}
}

func TestPreviewSanitizesAndCropsVisibleScreen(t *testing.T) {
	t.Setenv("TERM", "xterm-256color")
	old := lipgloss.ColorProfile()
	t.Cleanup(func() { lipgloss.SetColorProfile(old) })
	lipgloss.SetColorProfile(termenv.TrueColor)
	text := "\x1b[31m  中e\u0301👩‍💻\r\n\n\x1b]52;fixture-payload\a\u009dhidden\u009c\x1bPdiscard\aignore\x1b\\\u009fsecret\x1b\\\x1b[H\x1bc\x1b[48;2;30;40;50;38;5;202mTAIL\x1b[0m\n   \n"
	frame := parsePreview(text, false)
	plain := ansi.Strip(strings.Join(frame.lines, "\n"))
	if plain != "  中e\u0301👩‍💻\n\nTAIL" || !strings.Contains(strings.Join(frame.lines, ""), "48;2;30;40;50") || !strings.HasSuffix(frame.lines[2], ansi.ResetStyle) {
		t.Fatalf("preview lost text or styling: %+v", frame)
	}
	if got := ansi.Strip(ansi.Truncate(frame.lines[0], 6, "")); got != "  中e\u0301" {
		t.Fatal("partial grapheme", got)
	}
	t.Setenv("NO_COLOR", "1")
	if got := strings.Join(parsePreview(text, false).lines, "\n"); got != plain {
		t.Fatalf("no-color differs: %q", got)
	}
}

func TestSessionColumnsAndMotion(t *testing.T) {
	t.Setenv("TERM", "xterm-256color")
	t.Setenv("AGENT_HARBOR_ASCII", "0")
	t.Setenv("AGENT_HARBOR_REDUCED_MOTION", "0")
	for width, want := range map[int][2]int{90: {32, 56}, 100: {34, 64}, 160: {55, 103}} {
		g := sessionColumns(width, 12)
		if !g.split || g.left != want[0] || g.right != want[1] || g.row != 1 || sessionColumns(width, 11).split {
			t.Fatal(g)
		}
	}
	old := lipgloss.ColorProfile()
	t.Cleanup(func() { lipgloss.SetColorProfile(old) })
	lipgloss.SetColorProfile(termenv.TrueColor)
	service := &previewServiceFixture{}
	model := newHarborModel(service)
	t.Cleanup(model.cancelRefresh)
	model.sessions = []sessionView{previewFixture("alpha")}
	model.syncMotion(true)
	begin := model.motion.start
	earlyIcon, earlyWord := model.sessionIndicator(model.sessions[0], uiRowStyle{}, true)
	model.motion.now = begin.Add(1300 * time.Millisecond)
	lateIcon, lateWord := model.sessionIndicator(model.sessions[0], uiRowStyle{}, true)
	if earlyIcon == lateIcon || earlyWord == lateWord || ansi.Strip(lateWord) != "Working" || ansi.StringWidth(earlyIcon) != ansi.StringWidth(lateIcon) {
		t.Fatal("motion did not preserve text/width")
	}
	model.syncMotion(true)
	if !model.motion.start.Equal(begin) {
		t.Fatal("refresh reset motion phase")
	}
	_, cmd := model.Update(motionTick(model.motion.generation))
	if cmd == nil || len(service.reads) != 0 {
		t.Fatal("animation used preview reader")
	}
	model.sessions[0].Runtime.Activity = "idle"
	model.syncMotion(true)
	if model.motion.cancel != nil || model.motion.ctx != nil {
		t.Fatal("idle state kept clock running")
	}
}

func TestPreviewResponsiveMetadataAndMouse(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	service := &previewServiceFixture{}
	model := newHarborModel(service)
	t.Cleanup(model.cancelRefresh)
	alpha, beta := previewFixture("alpha"), previewFixture("beta")
	alpha.Name, beta.Name = "同名 é 👩‍💻", "同名 é 👩‍💻"
	alpha.WorkingDirectory = "/fixtures/" + strings.Repeat("项目", 30)
	alpha.Mode, alpha.MagpieModel = "magpie", strings.Repeat("model-", 15)
	model.sessions = []sessionView{alpha, beta}
	model.width, model.height = 160, 38
	geometry := model.sessionGeometry()
	entries, _, _ := model.sessionListPlan(geometry)
	suffixes := model.sessionSuffixes(entries)
	if suffixes[alpha.ID] == "" || suffixes[alpha.ID] == suffixes[beta.ID] {
		t.Fatal("duplicate names remained ambiguous", suffixes)
	}
	if line := model.previewHeader(alpha, geometry.right)[2]; !strings.Contains(line, "codex · Magpie") || ansi.StringWidth(line) > geometry.right {
		t.Fatal("long project obscured the connection", line)
	}
	frame := make([]string, 50)
	frame[len(frame)-1] = "BOTTOM-FIXTURE"
	model.preview.target, _ = targetForPreview(alpha)
	model.preview.frame.lines = frame
	step := func(msg tea.Msg) {
		next, _ := model.Update(msg)
		model = next.(harborModel)
	}
	step(tea.MouseMsg{X: geometry.left + 3, Y: geometry.top, Button: tea.MouseButtonWheelUp})
	if model.cursor != 0 || model.preview.offset != 1 || strings.Contains(model.View(), "BOTTOM-FIXTURE") {
		t.Fatal("preview scroll changed the selection or did not crop the screen")
	}
	step(tea.MouseMsg{X: 2, Y: geometry.top, Button: tea.MouseButtonWheelDown})
	if model.cursor != 1 || model.preview.offset != 0 || model.preview.target.id != "beta" {
		t.Fatal("list scroll did not switch cleanly")
	}
	step(tea.WindowSizeMsg{Width: 46, Height: 23})
	for _, hit := range model.sessionsLayout().Hits {
		if hit.ID == "session:alpha" && hit.H != 2 {
			t.Fatal("narrow hit area", hit)
		}
	}
	line := model.renderSessionSecondaryLine(alpha, uiRowStyle{}, 44)
	if !strings.Contains(line, "codex  Magpie  Unbound") || ansi.StringWidth(line) != 44 {
		t.Fatal("narrow metadata lost a required field", line)
	}
}
