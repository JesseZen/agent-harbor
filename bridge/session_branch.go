package main

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

type sessionBranchReader interface {
	SessionBranch(context.Context, string) string
}

type sessionBranchResult struct {
	generation uint64
	branch     string
}

type sessionBranchState struct {
	session, directory string
	branch             string
	generation, busy   uint64
	cancel             context.CancelFunc
	checked            time.Time
}

func (harborService) SessionBranch(ctx context.Context, directory string) string {
	if !filepath.IsAbs(directory) {
		return ""
	}
	ctx, cancel := context.WithTimeout(ctx, 750*time.Millisecond)
	defer cancel()
	env := make([]string, 0)
	for _, entry := range os.Environ() {
		if !strings.HasPrefix(entry, "GIT_") {
			env = append(env, entry)
		}
	}
	read := func(args ...string) ([]byte, error) {
		cmd := exec.CommandContext(ctx, "git", args...)
		cmd.Dir, cmd.Env = directory, env
		return cmd.Output()
	}
	output, err := read("symbolic-ref", "--quiet", "HEAD")
	if err == nil {
		return cleanUI(strings.TrimPrefix(strings.TrimSpace(string(output)), "refs/heads/"))
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) && exit.ExitCode() == 1 {
		if output, err = read("rev-parse", "--short", "HEAD"); err == nil {
			return "detached " + cleanUI(strings.TrimSpace(string(output)))
		}
	}
	return ""
}

func (m *harborModel) syncSessionBranch(msg tea.Msg, visible bool) tea.Cmd {
	state := &m.projectBranch
	reader, available := m.service.(sessionBranchReader)
	id, directory := "", ""
	if visible && available && m.sessionGeometry().split {
		if view, ok := m.selected(); ok && filepath.IsAbs(view.WorkingDirectory) {
			id, directory = view.ID, view.WorkingDirectory
		}
	}
	if id != state.session || directory != state.directory {
		if state.cancel != nil {
			state.cancel()
		}
		*state = sessionBranchState{session: id, directory: directory, generation: state.generation + 1, busy: state.busy}
	}
	if result, ok := msg.(sessionBranchResult); ok && state.busy == result.generation {
		state.busy = 0
		if result.generation == state.generation {
			state.cancel()
			state.cancel = nil
			state.branch = result.branch
			state.checked = time.Now()
		}
	}
	if id == "" || state.busy != 0 || time.Since(state.checked) < 5*time.Second {
		return nil
	}
	if !state.checked.IsZero() {
		if _, refreshed := msg.(appLoaded); !refreshed {
			return nil
		}
	}
	ctx, cancel := context.WithCancel(m.refreshContext)
	state.cancel, state.busy = cancel, state.generation
	generation := state.generation
	return func() tea.Msg { return sessionBranchResult{generation, reader.SessionBranch(ctx, directory)} }
}
