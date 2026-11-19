package main

import (
	"errors"
	"fmt"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
	"os"
	"strings"
	"testing"
)

func formFixture() (harborModel, *fakeApplicationService) {
	f := &fakeApplicationService{}
	m := newHarborModel(f)
	m.formActive = true
	m.screen = "new-form"
	m.formFocus = formCreate
	m.environment = appEnvironment{Directory: "/projects/harbor", CodexVersion: "0.167.0", LocalHome: "/fixture/codex"}
	m.draft = sessionDraft{Directory: "/projects/harbor", Connection: "local"}
	return m, f
}
func TestFormOnlyCreateSubmitsAndGuardsCompletion(t *testing.T) {
	m, f := formFixture()
	model := appKey(m, "up")
	model = appKey(model, "enter")
	model = appKey(model, "custom")
	model = appKey(model, "enter")
	if strings.Contains(strings.Join(f.Calls, "|"), "create") {
		t.Fatal(f.Calls)
	}
	model = appKey(model, "down")
	model, cmd := model.Update(uiKey("ctrl+s"))
	if cmd == nil {
		t.Fatal("no create")
	}
	for _, k := range []string{"enter", "enter", "esc", "n"} {
		model = appKey(model, k)
	}
	if len(f.Sessions) != 0 {
		t.Fatal("command executed twice")
	}
	result := cmd()
	model, open := model.Update(result)
	if len(f.Sessions) != 1 || open == nil {
		t.Fatal("missing create/open")
	}
	model, duplicate := model.Update(result)
	if duplicate != nil {
		t.Fatal("duplicate completion opened again")
	}
	_ = model
}
func TestFormFailurePreservesDraftAndStaleResult(t *testing.T) {
	m, f := formFixture()
	m.draft.Name = "keep"
	m.generation = 8
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	f.Err = errors.New("create unavailable")
	next, _ = next.Update(cmd())
	n := next.(harborModel)
	if n.screen != "new-form" || n.draft.Name != "keep" || n.notice != "create unavailable" {
		t.Fatal(n.screen, n.draft, n.notice)
	}
	n.creating = true
	n.screen = "busy"
	n.generation = 9
	next, cmd = n.Update(appFormCreated{8, appCreated{}})
	if cmd != nil || next.(harborModel).screen != "busy" {
		t.Fatal("stale create accepted")
	}
}
func TestFormConnectionCancelRestoresTupleAndAckNeverCreates(t *testing.T) {
	m, f := formFixture()
	m.draft = sessionDraft{Directory: "/projects/harbor", Name: "keep", Connection: "magpie", Instance: "original", Model: "group"}
	m.formSaved = m.draft
	m.screen = "new-catalog"
	m.draft.Instance = "pending"
	m.draft.Model = ""
	n := appKey(m, "esc").(harborModel)
	if n.draft != m.formSaved {
		t.Fatal(n.draft)
	}
	n.formFocus = formCreate
	n = appKey(n, "enter").(harborModel)
	if n.screen != "new-notice" {
		t.Fatal(n.screen)
	}
	n = appKey(n, "enter").(harborModel)
	if n.screen != "new-form" || strings.Contains(strings.Join(f.Calls, "|"), "create") {
		t.Fatal(n.screen, f.Calls)
	}
}
func TestFormPracticalSizesAndVisual(t *testing.T) {
	old := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	defer lipgloss.SetColorProfile(old)
	for _, size := range [][2]int{{60, 20}, {100, 30}, {160, 45}} {
		for _, mode := range []string{"local", "magpie"} {
			m, _ := formFixture()
			m.width, m.height = size[0], size[1]
			m.draft.Connection = mode
			m.draft.Instance = "managed"
			m.draft.Model = "relay-a/test-model"
			m.draft.Directory = "/projects/日本語/" + strings.Repeat("long-project/", 12)
			m.draft.Name = "作業 " + strings.Repeat("name", 24)
			view := m.View()
			for _, v := range []string{"Project", "Client", "Connection", "Name", "Create", "Cancel", "↵ open"} {
				if !strings.Contains(cleanUI(view), v) {
					t.Fatalf("%v missing %s: %s", size, v, view)
				}
			}
			if len(strings.Split(view, "\n")) > size[1] {
				t.Fatal("height")
			}
			for _, line := range strings.Split(view, "\n") {
				if lipgloss.Width(line) > size[0] {
					t.Fatal("width")
				}
			}
			if out := os.Getenv("HARBOR_VISUAL_OUT"); out != "" {
				for _, focus := range []int{formProject, formName} {
					m.formFocus = focus
					os.WriteFile(fmt.Sprintf("%s/form-focus%d-%s-%d.ansi", out, focus, mode, size[0]), []byte(m.View()), 0600)
				}
				m.screen = "new-name"
				m.input.SetValue("draft ! q 日本語")
				m.input.Focus()
				os.WriteFile(fmt.Sprintf("%s/form-edit-%s-%d.ansi", out, mode, size[0]), []byte(m.View()), 0600)
				if e := os.WriteFile(fmt.Sprintf("%s/form-%s-%d.ansi", out, mode, size[0]), []byte(view), 0600); e != nil {
					t.Fatal(e)
				}
			}
		}
	}
}

func TestFormDirectoryPickerSelectAndCancelStayLocal(t *testing.T) {
	m, f := formFixture()
	m.formFocus = 0
	m.draft.Name = "keep"
	m.environment.Directory = t.TempDir()
	m.draft.Directory = m.environment.Directory
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	n := next.(harborModel)
	if n.screen != "new-directory" {
		t.Fatal(n.screen)
	}
	n = appKey(n, "esc").(harborModel)
	if n.screen != "new-form" || n.draft != m.draft {
		t.Fatal(n.screen, n.draft)
	}
	n = appKey(n, "enter").(harborModel)
	n = appKey(n, "enter").(harborModel)
	if n.screen != "new-form" || n.draft.Name != "keep" || strings.Contains(strings.Join(f.Calls, "|"), "create") {
		t.Fatal(n.screen, n.draft, f.Calls)
	}
}

func TestFormCompactPopupFitsEveryControl(t *testing.T) {
	m, _ := formFixture()
	m.width = 48
	m.height = 12
	for _, focus := range []int{formProject, formOptions, formName, formCreate, formCancel} {
		m.formFocus = focus
		view := m.View()
		for _, text := range []string{"Project", "Client", "Connection", "Model", "Name", "Create", "Cancel"} {
			if !strings.Contains(view, text) {
				t.Fatal(text, view)
			}
		}
		if len(strings.Split(view, "\n")) > 12 {
			t.Fatal(view)
		}
	}
	m.notice = "Gateway unavailable"
	if !strings.Contains(m.View(), "Details") {
		t.Fatal(m.View())
	}
}
func TestFormNavigationAndCatalogFailurePreserveIntent(t *testing.T) {
	m, f := formFixture()
	m.draft = sessionDraft{Directory: "/projects/harbor", Name: "custom", Connection: "magpie", Instance: "original", Model: "vanished"}
	f.Reports = []instanceReport{{magpieInstance: magpieInstance{ID: "replacement"}}}
	var model tea.Model = m
	model, _ = model.Update(tea.KeyMsg{Type: tea.KeyUp})
	if model.(harborModel).formFocus != formName {
		t.Fatal("up")
	}
	model, _ = model.Update(tea.KeyMsg{Type: tea.KeyDown})
	if model.(harborModel).formFocus != 0 {
		t.Fatal("down")
	}
	for _, k := range []string{"up", "enter", "discard", "esc"} {
		model = appKey(model, k)
	}
	if model.(harborModel).draft != m.draft {
		t.Fatal("name cancel changed draft")
	}
	for _, k := range []string{"up", "up", "up", "enter", "enter"} {
		model = appKey(model, k)
	}
	// Instance Enter begins a request; failure must restore the entire accepted tuple.
	model, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("no catalog request", model.(harborModel).screen)
	}
	f.Err = errors.New("catalog unavailable")
	model, _ = model.Update(cmd())
	n := model.(harborModel)
	if n.screen != "new-form" || n.draft != m.draft {
		t.Fatal(n.screen, n.draft)
	}
	if len(f.Sessions) != 0 {
		t.Fatal("created on failure")
	}
}

func TestFormNoColorFocusAndHelp(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	m, _ := formFixture()
	view := m.View()
	if strings.Contains(view, "\x1b[") || !strings.Contains(view, "^S Create") {
		t.Fatal(view)
	}
	n := appKey(m, "?").(harborModel)
	if n.screen != "help" || !strings.Contains(n.helpView(), "Create saves") {
		t.Fatal(n.screen, n.helpView())
	}
}
