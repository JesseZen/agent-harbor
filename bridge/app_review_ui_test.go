package main

import (
	"errors"
	tea "github.com/charmbracelet/bubbletea"
	"strings"
	"testing"
	"time"
)

func reviewClick(t *testing.T, m harborModel, id string) harborModel {
	t.Helper()
	m.reviewOpenedAt = time.Time{}
	for _, r := range m.reviewLayout().Hits {
		if r.ID == id {
			n, cmd := m.Update(tea.MouseMsg{X: r.X, Y: r.Y, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
			for cmd != nil {
				n, cmd = n.Update(cmd())
			}
			return n.(harborModel)
		}
	}
	t.Fatal("missing", id)
	return m
}
func TestReviewSavedMouseDoesNotLaunchAndCanRetry(t *testing.T) {
	row := savedReviewFixture()
	f := &savedReviewService{fakeApplicationService: &fakeApplicationService{Sessions: []sessionView{row}}}
	m := newHarborModel(f)
	m.sessions = []sessionView{row}
	m = appKey(m, "v").(harborModel)
	m = reviewClick(t, m, "review:a")
	f.Err = errors.New("synthetic save failure")
	m = reviewClick(t, m, "review:enter")
	if m.screen != "confirm" || !strings.Contains(m.notice, "synthetic save failure") {
		t.Fatal("lost retryable review", m.screen, m.notice)
	}
	f.Err = nil
	m = reviewClick(t, m, "review:enter")
	if len(f.reviews) != 2 || strings.Contains(strings.Join(f.Calls, "|"), "open ") {
		t.Fatal("review launched or did not retry")
	}
}
func TestReviewRejectsClickAndKeyThroughNewPanel(t *testing.T) {
	f := &fakeApplicationService{}
	m := newHarborModel(f)
	m.screen = "confirm"
	m.action = "stop"
	m.reviewed = session{ID: "target", Name: "Target"}
	m.reviewOpenedAt = time.Now()
	for _, r := range m.reviewLayout().Hits {
		if r.ID == "review:enter" {
			n, cmd := m.Update(tea.MouseMsg{X: r.X, Y: r.Y, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
			if cmd != nil || n.(harborModel).screen != "confirm" {
				t.Fatal("click penetrated")
			}
		}
	}
	_, cmd := m.Update(uiKey("enter"))
	if cmd != nil || len(f.Calls) != 0 {
		t.Fatal("repeated Enter penetrated")
	}
}
func TestReviewOldAsyncResultCannotAffectNewTarget(t *testing.T) {
	m := newHarborModel(&fakeApplicationService{})
	m.screen = "busy"
	m.generation = 8
	m.reviewed = session{ID: "new"}
	n, cmd := m.Update(appReviewedResult{generation: 7, target: "old", result: appResult{err: errors.New("old failure")}})
	if cmd != nil || n.(harborModel).screen != "busy" || n.(harborModel).notice != "" {
		t.Fatal("stale result applied")
	}
}
func TestReviewActionsReachableAtPopupSizes(t *testing.T) {
	for _, sz := range [][2]int{{44, 12}, {58, 21}, {104, 32}} {
		m := newHarborModel(&fakeApplicationService{})
		m.width, m.height = sz[0], sz[1]
		m.screen = "confirm"
		m.action = "query-resume"
		m.reviewed = savedReviewFixture().session
		for _, id := range []string{"review:a", "evidence", "review:esc"} {
			found := false
			for _, r := range m.reviewLayout().Hits {
				if r.ID == id {
					found = true
				}
			}
			if !found {
				t.Fatal(sz, id, "clipped")
			}
		}
	}
}

func TestInspectionRejectsChangedOrDisappearedRecord(t *testing.T) {
	for _, missing := range []bool{false, true} {
		row := sessionView{session: session{ID: "target", Name: "Before", Mode: "local"}, Runtime: runtimeObservation{Presence: "not_started"}}
		f := &fakeApplicationService{Sessions: []sessionView{row}}
		m := newHarborModel(f)
		m.sessions = []sessionView{row}
		next, cmd := m.Update(uiKey("enter"))
		m = next.(harborModel)
		if cmd == nil {
			t.Fatal("inspection not scheduled")
		}
		result := cmd()
		if missing {
			m.sessions = nil
		} else {
			m.sessions[0].Name = "After"
		}
		next, _ = m.Update(result)
		m = next.(harborModel)
		if m.screen != "sessions" || !strings.Contains(m.notice, "inspection") || strings.Contains(strings.Join(f.Calls, "|"), "open ") {
			t.Fatal("stale inspection offered recovery", m.screen, m.notice)
		}
	}
}
