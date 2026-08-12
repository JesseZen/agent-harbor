package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/asheshgoplani/agent-deck/internal/session"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// TestGenerateAgentHarborUpstreamCaptures is copied into an archive of the
// immutable Agent Deck v1.9.73 commit by generate-upstream-captures.sh. It is
// deliberately kept outside internal/ui in the public fork so normal tests
// cannot accidentally compare the fork with itself.
func TestGenerateAgentHarborUpstreamCaptures(t *testing.T) {
	outputRoot := os.Getenv("CAPTURE_OUTPUT_DIR")
	if outputRoot == "" {
		t.Fatal("CAPTURE_OUTPUT_DIR is required")
	}
	t.Setenv("HOME", t.TempDir())
	t.Setenv("AGENT_DECK_NO_UPDATE_CHECK", "1")

	for _, size := range []struct{ width, height int }{{160, 45}, {120, 30}, {90, 30}, {70, 30}} {
		home := upstreamFixtureHome(t, size.width, size.height)
		sessions := upstreamNormalizeFrame(home.View(), size.width, size.height)
		updated, command := home.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
		home = updated.(*Home)
		if command != nil {
			message := command()
			updated, _ = home.Update(message)
			home = updated.(*Home)
		}
		home.newDialog.pathInput.SetValue("/workspace/core-adapter")
		dialog := upstreamNormalizeFrame(home.View(), size.width, size.height)

		directory := filepath.Join(outputRoot, fmt.Sprintf("%dx%d", size.width, size.height))
		if err := os.MkdirAll(directory, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(directory, "agent-deck.ansi"), []byte(sessions), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(directory, "agent-deck-dialog.ansi"), []byte(dialog), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func upstreamFixtureHome(t *testing.T, width, height int) *Home {
	t.Helper()
	base := time.Date(2026, time.July, 25, 4, 0, 0, 0, time.UTC)
	instances := []*session.Instance{
		{ID: "ses_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Title: "Review routing rollout", ProjectPath: "/workspace/harbor", GroupPath: "Interactive/route_primary", Tool: "codex", Command: "codex", Status: session.StatusRunning, Order: 0, CreatedAt: base.Add(-45 * time.Minute), LastAccessedAt: base.Add(-30 * time.Second)},
		{ID: "ses_bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", Title: "Investigate quota pressure", ProjectPath: "/workspace/observability", GroupPath: "Interactive/route_primary", Tool: "claude", Command: "claude", Status: session.StatusIdle, Order: 1, CreatedAt: base.Add(-2 * time.Hour), LastAccessedAt: base.Add(-4 * time.Minute)},
		{ID: "ses_cccccccccccccccccccccccccccccccc", Title: "Prepare release notes", ProjectPath: "/workspace/release", GroupPath: "Background/route_batch", Tool: "opencode", Command: "opencode", Status: session.StatusRunning, Order: 0, CreatedAt: base.Add(-25 * time.Minute), LastAccessedAt: base.Add(-50 * time.Second)},
		{ID: "ses_dddddddddddddddddddddddddddddddd", Title: "Repair target health", ProjectPath: "/workspace/core-adapter", GroupPath: "Background/route_batch", Tool: "codex", Command: "codex", Status: session.StatusError, Order: 1, CreatedAt: base.Add(-3 * time.Hour), LastAccessedAt: base.Add(-18 * time.Minute)},
	}

	home := NewHome()
	home.initialLoading = false
	home.instances = instances
	home.instanceByID = make(map[string]*session.Instance, len(instances))
	for _, instance := range instances {
		home.instanceByID[instance.ID] = instance
	}
	home.groupTree = session.NewGroupTree(instances)
	home.search.SetItems(instances)
	home.refreshSessionRenderSnapshot(instances)
	home.rebuildFlatItems()
	updated, _ := home.Update(tea.WindowSizeMsg{Width: width, Height: height})
	home = updated.(*Home)
	updated, command := home.Update(tea.KeyMsg{Type: tea.KeyDown})
	home = updated.(*Home)
	if command != nil {
		// The upstream comparison has no live tmux target. Rendering the group
		// preview is sufficient and avoids executing any runtime command.
	}
	return home
}

func upstreamNormalizeFrame(view string, width, height int) string {
	lines := strings.Split(view, "\n")
	if len(lines) > height {
		lines = lines[:height]
	}
	for len(lines) < height {
		lines = append(lines, "")
	}
	for index, line := range lines {
		lines[index] = ansi.Truncate(line, width, "")
	}
	return strings.Join(lines, "\n") + "\n"
}
