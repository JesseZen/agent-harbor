package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
)

func TestDirectoryFilesystem(t *testing.T) {
	root := t.TempDir()
	for _, p := range []string{"日本語 🚢/nested", "other"} {
		if err := os.MkdirAll(filepath.Join(root, p), 0700); err != nil {
			t.Fatal(err)
		}
	}
	os.Symlink(root, filepath.Join(root, "loop"))
	r := scanDirectories(context.Background(), root, true, 9)
	if r.err != nil || len(r.entries) != 4 || r.generation != 9 {
		t.Fatal(r)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if r := scanDirectories(ctx, root, true, 0); r.err != context.Canceled {
		t.Fatal(r)
	}
	if r := scanDirectories(context.Background(), filepath.Join(root, "missing"), false, 0); r.err == nil {
		t.Fatal("missing root accepted")
	}
	if r := scanDirectories(context.Background(), filepath.Join(root, "loop"), true, 0); r.err == nil {
		t.Fatal("symlink root followed")
	}
	denied := filepath.Join(root, "denied")
	os.Mkdir(denied, 0000)
	if os.Geteuid() != 0 {
		if result := scanDirectories(context.Background(), denied, false, 0); result.err == nil {
			t.Fatal("unreadable directory hidden as empty")
		}
	}
	os.Chmod(denied, 0700)

	for i := 0; i < directoryCap+10; i++ {
		os.Mkdir(filepath.Join(root, fmt.Sprintf("large-%04d", i)), 0700)
	}
	r = scanDirectories(context.Background(), root, true, 0)
	if !r.truncated || len(r.entries) > directoryCap+1 {
		t.Fatal("unbounded", len(r.entries), r)
	}
}
func TestDirectoryPickerIntegration(t *testing.T) {
	root := t.TempDir()
	child := filepath.Join(root, "日本語")
	os.Mkdir(child, 0700)
	m := searchFixture()
	m.screen = "new-project"
	m.environment.Directory = root
	m.environment.Projects = []string{child, root}
	m.input.SetValue("original draft")
	m = keySearch(m, "tab")
	if m.screen != "new-directory" {
		t.Fatal(m.screen)
	}
	m = keySearch(m, "q")
	if m.picker.query.Value() != "q" {
		t.Fatal("typing dispatched")
	}
	m = keySearch(m, "esc")
	if m.input.Value() != "original draft" {
		t.Fatal("cancel lost draft")
	}
	m = keySearch(m, "tab")
	old := m.picker.generation
	cmd := m.browseDirectory(root, false)
	n, _ := m.Update(cmd())
	m = n.(harborModel)
	if len(m.picker.entries) != 2 {
		t.Fatal(m.picker)
	}
	n, _ = m.Update(directoryResult{generation: old, err: fmt.Errorf("stale")})
	m = n.(harborModel)
	if m.picker.err != nil {
		t.Fatal("stale result applied")
	}
	m = keySearch(m, "tab")
	if !m.picker.list {
		t.Fatal("focus")
	}
	n, cmd = m.updateDirectory(tea.KeyMsg{Type: tea.KeyRight})
	m = n.(harborModel)
	n, _ = m.Update(cmd())
	m = n.(harborModel)
	if m.picker.root != child {
		t.Fatal("right did not browse")
	}
	m = keySearch(m, "esc")
	m = keySearch(m, "tab")
	m.picker.entries = []directoryEntry{{child, "browse"}}
	m = keySearch(m, "enter")
	if m.screen != "new-project" || m.input.Value() != child {
		t.Fatal("selection", m.screen, m.input.Value())
	}
	// Real service path/permissions/identity validation, rather than the fixture's no-op.
	if err := (harborService{}).ValidateDirectory(child); err != nil {
		t.Fatal(err)
	}
	os.Remove(child)
	if err := (harborService{}).ValidateDirectory(child); err == nil {
		t.Fatal("removed directory accepted")
	}
}
func TestDirectoryVisual(t *testing.T) {
	old := lipgloss.ColorProfile()
	defer lipgloss.SetColorProfile(old)
	output := os.Getenv("HARBOR_DIRECTORY_VISUAL")
	if os.Getenv("NO_COLOR") == "" {
		lipgloss.SetColorProfile(termenv.TrueColor)
	}
	for _, w := range []int{60, 100, 160} {
		for _, state := range []string{"known", "empty", "searching", "error", "truncated"} {
			m := searchFixture()
			m.width = w
			m.height = map[int]int{60: 20, 100: 30, 160: 45}[w]
			m.environment.Directory = "/projects"
			m.openDirectoryPicker()
			m.picker.entries = []directoryEntry{{"/projects/日本語 🚢/harbor-long-directory-component", "current pane / launch"}, {"/projects/harbor-api", "recent"}}
			m.picker.filter = "hbr"
			m.picker.query.SetValue("hbr")
			switch state {
			case "empty":
				m.picker.filter = "no-such-directory"
			case "searching":
				m.picker.searching = true
			case "error":
				m.picker.err = fmt.Errorf("permission denied; choose another root")
			case "truncated":
				m.picker.truncated = true
			}
			view := m.View()
			for _, line := range strings.Split(view, "\n") {
				if ansi.StringWidth(line) > w {
					t.Fatalf("overflow %d %q", w, line)
				}
			}
			if !strings.Contains(ansi.Strip(view), "Use this folder") && !strings.Contains(ansi.Strip(view), "Cancel") {
				t.Fatal("footer missing")
			}
			if output != "" {
				os.MkdirAll(output, 0700)
				os.WriteFile(filepath.Join(output, fmt.Sprintf("directory-%s-%d.ansi", state, w)), []byte(view), 0600)
			}
		}
	}
}

func TestDirectoryAsyncInputAndCancellation(t *testing.T) {
	root := t.TempDir()
	os.Mkdir(filepath.Join(root, "child"), 0700)
	m := searchFixture()
	m.environment.Directory = root
	m.openDirectoryPicker()
	next, cmd := m.updateDirectory(tea.KeyMsg{Type: tea.KeyCtrlR})
	m = next.(harborModel)
	if cmd == nil || !m.picker.searching || m.picker.scope != "recursive" {
		t.Fatal("recursive request not visible")
	}
	stale := m.picker.generation
	next, _ = m.updateDirectory(tea.KeyMsg{Type: tea.KeyCtrlX})
	m = next.(harborModel)
	next, _ = m.Update(cmd())
	m = next.(harborModel)
	if m.picker.searching || m.picker.err == nil || m.picker.generation <= stale {
		t.Fatal("cancel/stale response")
	}
	next, cmd = m.updateDirectory(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(root + "/")})
	m = next.(harborModel)
	if cmd == nil || m.picker.root != root || m.picker.scope != "children" {
		t.Fatal("absolute query did not browse parent")
	}
	// The returned batch contains the text-input command and directory scan; execute
	// the filesystem command directly to check the same generation boundary.
	result := scanDirectories(context.Background(), root, false, m.picker.generation)
	next, _ = m.Update(result)
	m = next.(harborModel)
	g := m.picker.generation
	next, cmd = m.updateDirectory(tea.KeyMsg{Type: tea.KeyLeft})
	m = next.(harborModel)
	if m.picker.generation != g || m.picker.root != root {
		t.Fatal("editor left navigated directories")
	}
	next, _ = m.updateDirectory(tea.KeyMsg{Type: tea.KeyEsc})
	m = next.(harborModel)
	next, _ = m.Update(result)
	if next.(harborModel).screen != "new-project" {
		t.Fatal("late result reopened picker")
	}
}

func TestDirectoryOrderingAndPreviewEffects(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"zebra", "日本語", "alpha"} {
		os.Mkdir(filepath.Join(root, name), 0700)
	}
	r := scanDirectories(context.Background(), root, false, 1)
	want := []string{filepath.Join(root, "alpha"), filepath.Join(root, "zebra"), filepath.Join(root, "日本語"), root}
	for i, e := range r.entries {
		if e.path != want[i] {
			t.Fatalf("unstable ordering: %#v", r.entries)
		}
	}
	if r.entries[len(r.entries)-1].source != "browse root" {
		t.Fatal("root not labeled")
	}
	m := searchFixture()
	m.width = 160
	m.environment.Directory = root
	m.environment.Projects = []string{want[1], want[0]}
	cmd := m.openDirectoryPicker()
	if m.picker.entries[1].path != want[1] {
		t.Fatal("known source priority reordered")
	}
	result := cmd().(directoryPreviewResult)
	n, _ := m.Update(result)
	m = n.(harborModel)
	if !strings.Contains(m.picker.preview, "alpha") {
		t.Fatal("preview not loaded", m.picker.preview)
	}
	// Removing a child after loading must not change View: rendering is pure.
	before := m.directoryPage().view()
	os.Remove(filepath.Join(root, "alpha"))
	if m.directoryPage().view() != before {
		t.Fatal("View performed a filesystem read")
	}
	n, cmd = m.updateDirectory(tea.KeyMsg{Type: tea.KeyDown})
	m = n.(harborModel)
	if cmd == nil {
		t.Fatal("selection did not schedule preview")
	}
	newer := cmd().(directoryPreviewResult)
	n, _ = m.Update(result)
	m = n.(harborModel)
	if m.picker.preview != "Preview: loading…" {
		t.Fatal("stale selection preview applied")
	}
	n, _ = m.Update(newer)
	m = n.(harborModel)
	// Even a current generation cannot paint another path.
	wrong := newer
	wrong.path = root
	wrong.text = "WRONG"
	n, _ = m.Update(wrong)
	m = n.(harborModel)
	if m.picker.preview == "WRONG" {
		t.Fatal("path guard")
	}
	n, _ = m.updateDirectory(tea.KeyMsg{Type: tea.KeyEsc})
	m = n.(harborModel)
	n, _ = m.Update(newer)
	m = n.(harborModel)
	if m.picker.preview != "" {
		t.Fatal("preview survived cancel")
	}
	m.openDirectoryPicker()
	n, _ = m.Update(newer)
	m = n.(harborModel)
	if m.picker.preview != "Preview: loading…" {
		t.Fatal("old preview entered reopened picker")
	}
	for _, line := range strings.Split(m.directoryPage().view(), "\n") {
		if ansi.StringWidth(line) > m.width {
			t.Fatal("picker exceeds frame width")
		}
	}
}

func TestDirectoryFeedbackShortcutOwnership(t *testing.T) {
	m := searchFixture()
	m.input.SetValue("draft")
	m.notice = "A directory needs attention"
	m.openDirectoryPicker()
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("!")})
	m = next.(harborModel)
	if m.screen != "new-directory" || m.picker.query.Value() != "!" {
		t.Fatal("feedback shortcut intercepted directory query", m.screen)
	}
	if strings.Contains(m.feedbackLine(), "! details") {
		t.Fatal("query advertises unavailable action")
	}
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlG})
	m = next.(harborModel)
	if m.notice != "" || m.picker.query.Value() != "!" || m.screen != "new-directory" {
		t.Fatal("dismiss altered directory query")
	}
	m.picker.stopPreview()
}
