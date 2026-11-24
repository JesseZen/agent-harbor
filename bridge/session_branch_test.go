package main

import (
	"context"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func TestSessionBranchUsesTheProjectsWorktree(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("Git is unavailable")
	}
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	home := t.TempDir()
	t.Setenv("HOME", home)
	project := t.TempDir()
	run := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = project
		output, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v: %s", args, err, output)
		}
		return strings.TrimSpace(string(output))
	}
	run("init", "-q", "-b", "fixture-main")
	service := harborService{}
	ctx := context.Background()
	if got := service.SessionBranch(ctx, project); got != "fixture-main" {
		t.Fatal(got)
	}
	run("-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "commit", "--quiet", "--allow-empty", "-m", "Fixture")
	worktree := filepath.Join(t.TempDir(), "worktree")
	run("worktree", "add", "-qb", "fixture-topic", worktree)
	commit := run("rev-parse", "--short", "HEAD")
	run("checkout", "--quiet", "--detach")
	t.Setenv("GIT_DIR", filepath.Join(project, ".git"))
	if got := service.SessionBranch(ctx, worktree); got != "fixture-topic" {
		t.Fatal(got)
	}
	if got := service.SessionBranch(ctx, project); got != "detached "+commit {
		t.Fatal(got)
	}
	if got := service.SessionBranch(ctx, t.TempDir()); got != "" {
		t.Fatal("not a repository", got)
	}
}

type sessionBranchFixture struct{ fakeApplicationService }

func (sessionBranchFixture) SessionBranch(_ context.Context, directory string) string {
	return filepath.Base(directory) + "-branch"
}

func TestSessionBranchDiscardsPreviousSelection(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	model := newHarborModel(&sessionBranchFixture{})
	t.Cleanup(model.cancelRefresh)
	model.width, model.height = 104, 33
	model.sessions = []sessionView{previewFixture("alpha"), previewFixture("beta")}
	model.sessions[0].WorkingDirectory = "/fixture/alpha"
	model.sessions[1].WorkingDirectory = "/fixture/beta"
	alpha := model.syncSessionBranch(nil, true)
	model.cursor = 1
	if early := model.syncSessionBranch(nil, true); early != nil {
		t.Fatal("spawned another Git command while the old one was cancelling")
	}
	beta := model.syncSessionBranch(alpha(), true)
	if model.projectBranch.branch != "" || beta == nil {
		t.Fatal("late alpha branch was shown or beta did not start")
	}
	model.syncSessionBranch(beta(), true)
	view, _ := model.selected()
	if branch := model.previewBranch(view); branch != "beta-branch" {
		t.Fatal(branch)
	}
	model.width = 80
	if cmd := model.syncSessionBranch(tea.WindowSizeMsg{}, true); cmd != nil || model.projectBranch.branch != "" {
		t.Fatal("Git lookup stayed active in narrow layout")
	}
}

func TestSessionsContextToggleAndSearch(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	home := t.TempDir()
	t.Setenv("HOME", home)
	model := newHarborModel(&sessionBranchFixture{})
	t.Cleanup(model.cancelRefresh)
	model.width, model.height = 104, 33
	view := previewFixture("fixture")
	view.WorkingDirectory = "/fixture/nested/project"
	view.Mode, view.InstanceID, view.MagpieModel = "magpie", "fixture-instance", "provider/model-name"
	model.sessions = []sessionView{view}
	model.projectBranch = sessionBranchState{session: view.ID, directory: view.WorkingDirectory, branch: "fixture-topic"}
	collapsed := model.previewHeader(view, 65)
	if !strings.Contains(collapsed[1], "project fixture-topic") || strings.Contains(collapsed[1], "/fixture/nested") || !strings.Contains(collapsed[2], "codex · Magpie") || strings.Contains(collapsed[2], "model-name") {
		t.Fatal(collapsed)
	}
	view.WorkingDirectory = "/this/is/a/very/long/worktree/path/nested/project-with-a-very-long-name"
	longPath := middleTruncate(previewPathDisplay(view.WorkingDirectory), 34)
	if !strings.HasPrefix(longPath, "/this") || !strings.Contains(longPath, "…") || !strings.HasSuffix(longPath, "very-long-name") || ansi.StringWidth(longPath) != 34 {
		t.Fatalf("expanded path was not middle-truncated: %q (%d cells)", longPath, ansi.StringWidth(longPath))
	}
	if got := previewPathDisplay(filepath.Join(home, "project")); got != "~/project" {
		t.Fatal("home path was not abbreviated", got)
	}
	view.WorkingDirectory = "/fixture/nested/project"
	step := func(key string) {
		next, _ := model.Update(uiKey(key))
		model = next.(harborModel)
	}
	step("o")
	if !model.previewExpanded || model.screen != "sessions" || !strings.Contains(model.View(), "/fixture/nested/project") || !strings.Contains(model.View(), "Route: fixture-instance/provider/model-name") || strings.Contains(model.View(), "codex · Magpie · model-name") || strings.Count(model.View(), "fixture-topic") != 1 {
		t.Fatal(model.View())
	}
	if header := model.previewHeaderContext(view, 65, true); len(header) > 4 || !strings.Contains(header[1], "/fixture/nested/project") || !strings.Contains(header[2], "Route: fixture-instance/provider/model-name") {
		t.Fatal("expanded context lost its anchor or exceeded four rows", header)
	}
	view.MagpieModel = "provider/" + strings.Repeat("long-model-segment-", 5)
	longHeader := model.previewHeaderContext(view, 65, true)
	plainRoute := strings.Join(strings.Fields(ansi.Strip(strings.Join(longHeader[2:], "\n"))), "")
	if len(longHeader) != 4 || !strings.HasPrefix(plainRoute, "codex·Magpie·Route:fixture-instance/") || !strings.HasSuffix(plainRoute, "long-model-segment-") || !strings.Contains(plainRoute, "…") {
		t.Fatal("long route was cut off before the four-row limit", longHeader)
	}
	step("o")
	if model.previewExpanded || strings.Contains(model.View(), "Path: ") {
		t.Fatal("did not collapse")
	}
	step("/")
	step("o")
	if model.previewExpanded || model.searchQuery() != "o" {
		t.Fatal("toggle intercepted search input")
	}
	step("esc")
	step("esc")
	model.width = 46
	step("o")
	if model.screen != "detail" {
		t.Fatal("narrow context did not open Details", model.screen)
	}
}
