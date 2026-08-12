//go:build unix

package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/asheshgoplani/agent-deck/internal/coreclient"
	"github.com/asheshgoplani/agent-deck/internal/testcore"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
)

func TestComposedAppGoldenCapturesAtRequiredSizes(t *testing.T) {
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(termenv.Ascii) })

	server, err := testcore.Start(testcore.Options{})
	if err != nil {
		t.Fatalf("start fake Core: %v", err)
	}
	t.Cleanup(func() { _ = server.Close() })
	client, err := coreclient.NewUnixBackend(context.Background(), coreclient.Options{
		SocketPath:              server.SocketPath(),
		ExpectedInstanceID:      server.InstanceID(),
		ExpectedProtocolVersion: coreclient.AdminProtocolVersion,
	})
	if err != nil {
		t.Fatalf("create public Core client: %v", err)
	}
	t.Cleanup(client.Close)

	root := filepath.Join("testdata", "captures")
	update := os.Getenv("UPDATE_GOLDEN") == "1"
	for _, size := range []struct{ width, height int }{{160, 45}, {120, 30}, {90, 30}, {70, 30}} {
		size := size
		t.Run(fmt.Sprintf("%dx%d", size.width, size.height), func(t *testing.T) {
			directory := filepath.Join(root, fmt.Sprintf("%dx%d", size.width, size.height))
			sessions, dialog, editor := renderComposedCaptures(t, client, size.width, size.height)
			captures := map[string]string{
				"app-sessions.ansi":       sessions,
				"app-session-dialog.ansi": dialog,
				"app-editor.ansi":         editor,
			}
			if update {
				if err := os.MkdirAll(directory, 0o755); err != nil {
					t.Fatal(err)
				}
				for name, content := range captures {
					if err := os.WriteFile(filepath.Join(directory, name), []byte(content), 0o644); err != nil {
						t.Fatal(err)
					}
				}
			}
			for name, got := range captures {
				wantBytes, err := os.ReadFile(filepath.Join(directory, name))
				if err != nil {
					t.Fatalf("read %s (run UPDATE_GOLDEN=1): %v", name, err)
				}
				if got != string(wantBytes) {
					t.Fatalf("capture %s changed; review and run UPDATE_GOLDEN=1", name)
				}
				assertFrameBounds(t, name, got, size.width, size.height)
			}

			plainSessions := ansi.Strip(sessions)
			for _, expected := range []string{
				"Overview", "Sessions", "Upstreams", "Traffic Rules", "Observations",
				"SESSIONS", "PREVIEW",
			} {
				if !strings.Contains(plainSessions, expected) {
					t.Fatalf("Sessions capture omitted %q:\n%s", expected, plainSessions)
				}
			}
			// Backend-backed Home must load default hotkeys so the Sessions help
			// bar is not reduced to only the hardcoded Tab/Enter Toggle hint.
			if size.width >= 120 {
				for _, expected := range []string{"New/Quick", "Rename", "Delete"} {
					if !strings.Contains(plainSessions, expected) {
						t.Fatalf("Sessions help bar omitted %q:\n%s", expected, plainSessions)
					}
				}
			}
			for _, forbidden := range []string{"Placeholder", "Configuration"} {
				if strings.Contains(plainSessions, forbidden) {
					t.Fatalf("legacy text %q remains in capture", forbidden)
				}
			}
			plainDialog := ansi.Strip(dialog)
			if !strings.Contains(plainDialog, "New Session") {
				t.Fatalf("Sessions capture lost the native New Session workflow:\n%s", plainDialog)
			}
			upstreamRoot := filepath.Join("..", "..", "testdata", "captures", fmt.Sprintf("%dx%d", size.width, size.height))
			for name, content := range map[string]string{
				"Session workbench": ansi.Strip(readCapture(t, filepath.Join(upstreamRoot, "agent-deck.ansi"))),
				"Session dialog":    ansi.Strip(readCapture(t, filepath.Join(upstreamRoot, "agent-deck-dialog.ansi"))),
			} {
				for _, expected := range []string{"SESSIONS", "PREVIEW"} {
					if name == "Session dialog" {
						expected = "New Session"
					}
					if !strings.Contains(content, expected) {
						t.Fatalf("upstream %s baseline omitted %q", name, expected)
					}
					if name == "Session dialog" {
						break
					}
				}
			}
			plainEditor := ansi.Strip(editor)
			if !strings.Contains(plainEditor, "Edit Instance.log_level") ||
				!strings.Contains(plainEditor, "enter save") {
				t.Fatalf("editor capture omitted real owning-tab editor:\n%s", plainEditor)
			}
		})
	}
}

func readCapture(t *testing.T, path string) string {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read capture %s: %v", path, err)
	}
	return string(content)
}

func renderComposedCaptures(t *testing.T, client *coreclient.Client, width, height int) (string, string, string) {
	t.Helper()
	model := New(client)
	updated, _ := model.Update(tea.WindowSizeMsg{Width: width, Height: height})
	model = updated.(*Model)
	updated, command := model.Update(model.loadAll(false)())
	model = updated.(*Model)
	drainModelCommand(t, &model, command, 0)
	updated, command = model.Update(model.sessionHome.Init()())
	model = updated.(*Model)
	drainModelCommand(t, &model, command, 0)
	sessions := normalizeFrame(model.View(), width, height)

	updated, command = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	model = updated.(*Model)
	drainModelCommand(t, &model, command, 0)
	dialog := normalizeFrame(model.View(), width, height)
	updated, command = model.Update(tea.KeyMsg{Type: tea.KeyEsc})
	model = updated.(*Model)
	drainModelCommand(t, &model, command, 0)

	updated, command = model.Update(tea.KeyMsg{Type: tea.KeyCtrlLeft})
	model = updated.(*Model)
	drainModelCommand(t, &model, command, 0)
	updated, command = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	model = updated.(*Model)
	drainModelCommand(t, &model, command, 0)
	return sessions, dialog, normalizeFrame(model.View(), width, height)
}

func drainModelCommand(t *testing.T, model **Model, command tea.Cmd, depth int) {
	t.Helper()
	if command == nil || depth > 12 {
		return
	}
	message := command()
	if batch, ok := message.(tea.BatchMsg); ok {
		for _, child := range batch {
			drainModelCommand(t, model, child, depth+1)
		}
		return
	}
	updated, next := (*model).Update(message)
	*model = updated.(*Model)
	drainModelCommand(t, model, next, depth+1)
}

func normalizeFrame(view string, width, height int) string {
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

func assertFrameBounds(t *testing.T, name, frame string, width, height int) {
	t.Helper()
	lines := strings.Split(strings.TrimSuffix(frame, "\n"), "\n")
	if len(lines) != height {
		t.Fatalf("%s has %d rows, want %d", name, len(lines), height)
	}
	for row, line := range lines {
		if got := ansi.StringWidth(line); got > width {
			t.Fatalf("%s row %d width=%d exceeds %d: %q", name, row+1, got, width, ansi.Strip(line))
		}
	}
}
