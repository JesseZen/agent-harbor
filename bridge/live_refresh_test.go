package main

import (
	"context"
	"encoding/json"
	"errors"
	tea "github.com/charmbracelet/bubbletea"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRefreshRetainsStableSelectionAndReview(t *testing.T) {
	f := &fakeApplicationService{}
	m := newHarborModel(f)
	m.sessions = []sessionView{{session: session{ID: "a", Name: "A"}}, {session: session{ID: "b", Name: "B"}}}
	m.cursor = 1
	m.filter = ""
	m.screen = "confirm"
	m.reviewed = m.sessions[1].session
	m.action = "remove"
	next, _ := m.Update(appLoaded{generation: m.listGeneration, sessions: []sessionView{m.sessions[1], m.sessions[0]}})
	m = next.(harborModel)
	s, ok := m.selected()
	if !ok || s.ID != "b" || m.screen != "confirm" || m.reviewed.ID != "b" {
		t.Fatalf("refresh changed intent: %+v", m)
	}
	next, _ = m.Update(appLoaded{generation: m.listGeneration, sessions: []sessionView{m.sessions[1]}})
	m = next.(harborModel)
	if _, ok = m.selected(); ok {
		t.Fatal("vanished selection moved to another target")
	}
}

func TestRefreshFailureInvalidatesCachedActivity(t *testing.T) {
	m := newHarborModel(&fakeApplicationService{})
	m.sessions = []sessionView{{session: session{ID: "a", Name: "A"}, Running: true, Status: "Working", Runtime: runtimeObservation{Activity: "working", Presence: "observed"}}}
	next, _ := m.Update(appLoaded{generation: m.listGeneration, err: errors.New("offline")})
	m = next.(harborModel)
	if m.sessions[0].Running || m.sessions[0].Runtime.Activity != "unknown" || !strings.Contains(m.View(), "offline") {
		t.Fatal("cached activity remained live", m.View())
	}
}

func TestRefreshRejectsLateReply(t *testing.T) {
	m := newHarborModel(&fakeApplicationService{})
	m.listGeneration = 2
	next, cmd := m.Update(appLoaded{generation: 1, sessions: []sessionView{{session: session{ID: "old"}}}})
	if len(next.(harborModel).sessions) != 0 || cmd != nil {
		t.Fatal("old response applied")
	}
	// Existing forms and typed input survive an ordinary background response.
	m.screen = "rename"
	m.input.SetValue("kept")
	next, _ = m.Update(appLoaded{generation: 2})
	m = next.(harborModel)
	if m.screen != "rename" || m.input.Value() != "kept" {
		t.Fatal("refresh reset input")
	}
	_ = tea.Quit
}

type cancelListService struct {
	fakeApplicationService
	entered chan struct{}
	exited  chan struct{}
}

func (s *cancelListService) ListContext(ctx context.Context) ([]sessionView, error) {
	close(s.entered)
	<-ctx.Done()
	close(s.exited)
	return nil, ctx.Err()
}
func TestRefreshSingleFlightAndExitCancellation(t *testing.T) {
	f := &cancelListService{entered: make(chan struct{}), exited: make(chan struct{})}
	m := newHarborModel(f)
	done := make(chan tea.Msg, 1)
	go func() { done <- m.Init()() }()
	<-f.entered
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("r")})
	if cmd != nil {
		t.Fatal("manual refresh stacked a request")
	}
	m = next.(harborModel)
	next, cmd = m.Update(appRefreshTick{})
	if next.(harborModel).listGeneration != m.listGeneration {
		t.Fatal("timer stacked a request")
	}
	m = next.(harborModel)
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	select {
	case <-f.exited:
	case <-time.After(time.Second):
		t.Fatal("popup exit did not cancel list")
	}
	<-done
}

func TestRefreshReconnectClearsOnlyDiscoveryWarning(t *testing.T) {
	m := newHarborModel(&fakeApplicationService{})
	next, _ := m.Update(appLoaded{err: errors.New("offline")})
	m = next.(harborModel)
	next, _ = m.Update(appLoaded{sessions: []sessionView{{session: session{ID: "a"}, Status: "Idle"}}})
	m = next.(harborModel)
	if m.notice != "" || m.discoveryNotice != "" {
		t.Fatal("reconnection retained error", m.notice)
	}
	m.notice = "Review original connection"
	next, _ = m.Update(appLoaded{sessions: m.sessions})
	m = next.(harborModel)
	if m.notice != "Review original connection" {
		t.Fatal("refresh erased review notice")
	}
}

func TestRuntimeQueryCancellationClosesSocketRead(t *testing.T) {
	root := visibilityTestRoot(t)
	path := filepath.Join(root, "herdr", "cancel.sock")
	os.MkdirAll(filepath.Dir(path), 0700)
	l, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	t.Setenv("HERDR_SOCKET_PATH", path)
	accepted := make(chan struct{})
	released := make(chan struct{})
	go func() {
		c, e := l.Accept()
		if e != nil {
			return
		}
		defer c.Close()
		var request any
		json.NewDecoder(c).Decode(&request)
		close(accepted)
		buffer := make([]byte, 1)
		c.Read(buffer)
		close(released)
	}()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { _, e := (harborService{}).ListContext(ctx); done <- e }()
	<-accepted
	cancel()
	select {
	case err = <-done:
		if err == nil {
			t.Fatal("cancellation returned success")
		}
	case <-time.After(time.Second):
		t.Fatal("read did not cancel")
	}
	select {
	case <-released:
	case <-time.After(time.Second):
		t.Fatal("connection remained open")
	}
}

func TestVanishedSelectionCanBeExplicitlySelectedAgain(t *testing.T) {
	m := newHarborModel(&fakeApplicationService{})
	m.sessions = []sessionView{{session: session{ID: "old"}}, {session: session{ID: "next"}}}
	next, _ := m.Update(appLoaded{sessions: []sessionView{m.sessions[1]}})
	m = next.(harborModel)
	if _, ok := m.selected(); ok {
		t.Fatal("vanished target inherited another row")
	}
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = next.(harborModel)
	if s, ok := m.selected(); !ok || s.ID != "next" {
		t.Fatal("explicit selection could not recover", s, ok)
	}
}

func TestVanishedSelectionCanBeReplacedByExplicitClick(t *testing.T) {
	m := newHarborModel(&fakeApplicationService{})
	m.sessions = []sessionView{{session: session{ID: "old"}}, {session: session{ID: "next"}}}
	next, _ := m.Update(appLoaded{sessions: []sessionView{m.sessions[1]}})
	m = next.(harborModel)
	next, _ = m.Update(tea.MouseMsg{X: 2, Y: 6, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	m = next.(harborModel)
	if s, ok := m.selected(); !ok || s.ID != "next" {
		t.Fatal("explicit click could not choose current row", s, ok)
	}
}

type cancelInspectionService struct {
	fakeApplicationService
	entered, exited chan struct{}
}

func (s *cancelInspectionService) GetContext(ctx context.Context, id string) (sessionView, error) {
	close(s.entered)
	<-ctx.Done()
	close(s.exited)
	return sessionView{}, ctx.Err()
}
func TestEnterInspectionCanBeCancelled(t *testing.T) {
	f := &cancelInspectionService{entered: make(chan struct{}), exited: make(chan struct{})}
	m := newHarborModel(f)
	m.sessions = []sessionView{{session: session{ID: "a"}, Runtime: runtimeObservation{Presence: "observed"}}}
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(harborModel)
	done := make(chan tea.Msg, 1)
	go func() { done <- cmd() }()
	select {
	case <-f.entered:
	case <-time.After(time.Second):
		t.Fatal("inspection did not use cancellable service")
	}
	next, cmd = m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	m = next.(harborModel)
	if cmd == nil {
		t.Fatal("busy inspection ignored exit")
	}
	select {
	case <-f.exited:
	case <-time.After(time.Second):
		t.Fatal("inspection read not cancelled")
	}
	next, cmd = m.Update(<-done)
	if cmd != nil || next.(harborModel).screen != "busy" {
		t.Fatal("late cancelled inspection changed intent")
	}
}
