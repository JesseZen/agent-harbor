package main

import (
	"encoding/json"
	"fmt"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestClientPickerDoesNotPromoteIntegrationFixtureToLifecycleSupport(t *testing.T) {
	m, _ := formFixture()
	m.screen = "new-cli"
	m.environment.Clients = []clientCapability{{Target: "future-kind", Label: "Future client", Integration: "current"}}
	m.clientCursor = 1
	view := m.View()
	for _, want := range []string{"Future client", "subset", "Native start: unknown", "Identity: unknown", "Recovery: unknown", "Magpie: unavailable"} {
		if !strings.Contains(view, want) {
			t.Fatalf("missing %q in %s", want, view)
		}
	}
	m = appKey(m, "enter").(harborModel)
	if m.screen != "new-cli" || m.draft.Client == "future-kind" || !strings.Contains(m.notice, "upstream") {
		t.Fatalf("unsupported selection accepted: %+v", m.draft)
	}
}

func TestClientPickerPreservesExercisedNativeSubsetAndConnectionIntent(t *testing.T) {
	t.Setenv("AGENT_HARBOR_IDENTITY_CANDIDATE", "1")
	m, _ := formFixture()
	m.screen = "new-cli"
	m.environment.Clients = []clientCapability{{Target: "future-kind", Label: "Future client", Command: "future-kind", Available: true, Integration: "current"}}
	m = appKey(m, "down").(harborModel)
	m = appKey(m, "enter").(harborModel)
	if m.screen != "new-form" || m.draft.Client != "future-kind" {
		t.Fatalf("native executable subset inaccessible: %+v", m.draft)
	}
	m.screen = "new-connection"
	m.connectionCursor = 1
	m = appKey(m, "enter").(harborModel)
	if m.screen != "new-connection" || m.draft.Connection != "local" || !strings.Contains(m.notice, "isolation") {
		t.Fatal("unproven Magpie offered", m.screen, m.draft, m.notice)
	}
}

func TestNeutralRecoveryRequiresSeparateUncertaintyAcceptance(t *testing.T) {
	t.Setenv("AGENT_HARBOR_IDENTITY_CANDIDATE", "1")
	m, _ := formFixture()
	m.formActive = false
	m.screen = "sessions"
	m.sessions = []sessionView{{session: session{ID: "neutral", CLI: "future-kind", Started: true, Conversation: &conversationEvidence{Reference: herdrIdentity{Agent: "future-kind", Kind: "path", Value: "/opaque/conversation"}}}}}
	m = appKey(m, "enter").(harborModel)
	if !strings.Contains(m.View(), "Query proves syntax only") {
		t.Fatal(m.View())
	}
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil || next.(harborModel).screen != "confirm" {
		t.Fatal("uncertainty bypassed")
	}
	m = appKey(next, "a").(harborModel)
	_, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("explicit approval cannot proceed")
	}
}

func TestEnvironmentReportsUpstreamSubsetWithoutInventingMissingClients(t *testing.T) {
	newTestRegistry(t)
	t.Setenv("PATH", t.TempDir())
	dir, err := os.MkdirTemp("/tmp", "h54-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	socket := filepath.Join(dir, "s")
	l, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	t.Setenv("HERDR_SOCKET_PATH", socket)
	go func() {
		c, e := l.Accept()
		if e != nil {
			return
		}
		defer c.Close()
		var req map[string]any
		json.NewDecoder(c).Decode(&req)
		json.NewEncoder(c).Encode(map[string]any{"result": map[string]any{"integrations": []map[string]any{{"target": "opaque-target", "label": "Upstream label", "command": "different-alias", "available": true, "state": "outdated"}}}})
	}()
	env, err := (harborService{}).Environment()
	if err != nil {
		t.Fatal(err)
	}
	if len(env.Clients) != 1 || env.Clients[0].Target != "opaque-target" || env.Clients[0].Integration != "outdated" || !strings.Contains(env.ClientDiscovery, "subset") {
		t.Fatalf("upstream data lost: %+v", env)
	}
	m, _ := formFixture()
	m.screen = "new-cli"
	m.environment = env
	m.clientCursor = 1
	m = appKey(m, "enter").(harborModel)
	if m.screen != "new-cli" {
		t.Fatal("alias silently accepted")
	}
}

func TestOpenQueryRequiresExplicitUncertaintyApprovalBeforeAnyPane(t *testing.T) {
	err := (harborService{}).Open(sessionApproval{Confirmed: true, DevelopmentQuery: true})
	if err == nil || !strings.Contains(err.Error(), "explicitly accept") {
		t.Fatalf("unsafe query approval: %v", err)
	}
}

func TestClientPickerKeepsSelectedUpstreamRecordVisibleAtPracticalSizes(t *testing.T) {
	for _, size := range [][2]int{{80, 24}, {120, 36}, {60, 20}} {
		m, _ := formFixture()
		m.screen = "new-cli"
		m.width = size[0]
		m.height = size[1]
		for i := 0; i < 17; i++ {
			m.environment.Clients = append(m.environment.Clients, clientCapability{Target: fmt.Sprintf("target-%d", i), Label: fmt.Sprintf("Client %d", i)})
		}
		m.clientCursor = 17
		view := m.View()
		if !strings.Contains(view, "Client 16") || !strings.Contains(view, "Native start: unknown") {
			t.Fatalf("selection hidden at %v: %s", size, view)
		}
		for _, line := range strings.Split(view, "\n") {
			if ansi.StringWidth(line) > size[0] {
				t.Fatalf("overflow at %v", size)
			}
		}
	}
}

// The UI's public service boundary records the exact approval delivered by
// confirmation, without starting a client or inspecting UI implementation state.
type reviewedApprovalService struct {
	*fakeApplicationService
	approved sessionApproval
}

func (f *reviewedApprovalService) Open(a sessionApproval) error { f.approved = a; return nil }

func TestRecoveryConfirmationShowsSavedNeutralReferenceAndApprovesSameRecord(t *testing.T) {
	for _, width := range []int{60, 100, 160} {
		t.Run(fmt.Sprint(width), func(t *testing.T) {
			original := session{ID: "review-path", CLI: "future-kind", Name: "Saved path session", Mode: "local", Started: true, Conversation: &conversationEvidence{Reference: herdrIdentity{Source: "official:fixture", Agent: "future-kind", Kind: "path", Value: "/saved/project with spaces/chat.json"}}}
			f := &reviewedApprovalService{fakeApplicationService: &fakeApplicationService{}}
			m := newHarborModel(f)
			m.screen = "sessions"
			m.width = width
			m.height = 45
			m.sessions = []sessionView{{session: original}}
			m = appKey(m, "enter").(harborModel)
			view := m.View()
			for _, want := range []string{"Client", "future-kind", "Reference kind", "path", "/saved/project with spaces/chat.json"} {
				if !strings.Contains(view, want) {
					t.Fatalf("missing %q at width %d: %s", want, width, view)
				}
			}
			if strings.Contains(view, "Conversation    unknown") {
				t.Fatal("neutral identity presented as unknown")
			}
			for _, line := range strings.Split(view, "\n") {
				if ansi.StringWidth(line) > width {
					t.Fatalf("confirmation overflows width %d", width)
				}
			}
			next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
			if cmd != nil {
				t.Fatal("uncertainty acceptance bypassed")
			}
			m = appKey(next, "a").(harborModel)
			_, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
			if cmd == nil {
				t.Fatal("accepted recovery cannot proceed")
			}
			cmd()
			if !sameSession(original, f.approved.Session) || !f.approved.DevelopmentQuery || !f.approved.QueryUncertaintyAccepted || !f.approved.Confirmed {
				t.Fatalf("approval changed the reviewed record: %+v", f.approved)
			}
		})
	}
}

func TestRecoveryConfirmationPreservesLegacyCodexConversation(t *testing.T) {
	m, _ := formFixture()
	m.screen = "confirm"
	m.action = "resume"
	m.formActive = false
	m.reviewed = session{CLI: "codex", CodexSessionID: "legacy-codex-conversation"}
	if !strings.Contains(m.View(), "legacy-codex-conversation") {
		t.Fatal(m.View())
	}
}
