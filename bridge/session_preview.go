package main

import (
	"context"
	"errors"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

var (
	errPreviewRead        = errors.New("Preview unavailable · connection interrupted")
	errPreviewTarget      = errors.New("Terminal changed · waiting for discovery")
	errPreviewLarge       = errors.New("Preview exceeds the 64 KiB limit")
	errPreviewUnsupported = errors.New("Server does not support visible ANSI previews")
)

type previewTarget struct {
	id, socket, instance, workspace, tab, pane, terminal, client, run string
	group                                                             int
	external, associated, verified                                    bool
	profile                                                           profileBinding
	reference                                                         herdrIdentity
}

func targetForPreview(view sessionView) (previewTarget, string) {
	runtime := view.Runtime
	switch {
	case view.Archived:
		return previewTarget{}, "Session is archived · Unarchive to start or resume."
	case view.DiscoveryWarning != "" || runtime.Presence == "stale":
		return previewTarget{}, "Herdr connection unavailable"
	case runtime.Presence == "historical_exit" || runtime.Presence == "confirmed_exit":
		return previewTarget{}, "Terminal stopped"
	case runtime.Presence == "not_started":
		return previewTarget{}, "Session has not started"
	case runtime.Presence != "observed":
		return previewTarget{}, "Terminal not currently observed"
	case view.ID == "" || view.HerdrPane == "" || view.HerdrTab == "" || runtime.Terminal == "" || runtime.Workspace == "" || view.CLI == "" || view.HerdrSocket == "" || runtime.Instance == "" || view.HerdrSocketIdentity != runtime.Instance:
		return previewTarget{}, "Terminal identity unavailable"
	case !view.External && (!runtime.Associated && !runtime.RunVerified || view.RunToken == "" || view.ProcessGroup <= 1 || recoveryConflict(view.session) && !runtime.RunVerified):
		return previewTarget{}, "Terminal ownership needs review"
	}
	target := previewTarget{id: view.ID, socket: view.HerdrSocket, instance: runtime.Instance, workspace: runtime.Workspace, tab: view.HerdrTab, pane: view.HerdrPane, terminal: runtime.Terminal, client: view.CLI, run: view.RunToken, group: view.ProcessGroup, external: view.External, associated: runtime.Associated, verified: runtime.RunVerified}
	if view.Profile != nil {
		target.profile = *view.Profile
	}
	if runtime.Reference != nil {
		target.reference = *runtime.Reference
	}
	return target, ""
}

type previewSnapshot struct {
	lines     []string
	truncated bool
}

func (h harborService) ReadPreview(ctx context.Context, target previewTarget) (previewSnapshot, error) {
	return readSessionPreview(ctx, target, h.GetContext)
}

func readSessionPreview(ctx context.Context, target previewTarget, observe func(context.Context, string) (sessionView, error)) (previewSnapshot, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	verify := func() error {
		view, err := observe(ctx, target.id)
		if err != nil || view.DiscoveryWarning != "" {
			return errPreviewRead
		}
		current, reason := targetForPreview(view)
		if reason != "" || current != target || socketIdentity(target.socket) != target.instance {
			return errPreviewTarget
		}
		return nil
	}
	if err := verify(); err != nil {
		return previewSnapshot{}, err
	}
	var response struct {
		Read struct {
			Workspace      string `json:"workspace_id"`
			Tab            string `json:"tab_id"`
			Pane           string `json:"pane_id"`
			Source, Format string
			Text           *string
			Truncated      *bool
		} `json:"read"`
	}
	err := agentRPCContext(ctx, target.socket, "pane.read", map[string]any{"pane_id": target.pane, "source": "visible", "format": "ansi", "strip_ansi": false}, &response)
	if err != nil {
		switch err {
		case errPreviewLarge, errPreviewUnsupported, errPreviewTarget:
			return previewSnapshot{}, err
		}
		return previewSnapshot{}, errPreviewRead
	}
	read := response.Read
	if read.Workspace != target.workspace || read.Tab != target.tab || read.Pane != target.pane {
		return previewSnapshot{}, errPreviewTarget
	}
	if read.Source != "visible" || read.Format != "ansi" || read.Text == nil || read.Truncated == nil {
		return previewSnapshot{}, errPreviewUnsupported
	}
	if err := verify(); err != nil {
		return previewSnapshot{}, err
	}
	return parsePreview(*read.Text, *read.Truncated), nil
}

type previewReader interface {
	ReadPreview(context.Context, previewTarget) (previewSnapshot, error)
}
type previewDue uint64
type previewDone struct {
	generation uint64
	frame      previewSnapshot
	err        error
}
type previewState struct {
	target           previewTarget
	generation, busy uint64
	ctx              context.Context
	cancel           context.CancelFunc
	due              bool
	frame            previewSnapshot
	message          string
	offset           int
}

func waitView(ctx context.Context, delay time.Duration, msg tea.Msg) tea.Cmd {
	return func() tea.Msg {
		timer := time.NewTimer(delay)
		defer timer.Stop()
		select {
		case <-timer.C:
			return msg
		case <-ctx.Done():
			return nil
		}
	}
}

func (m *harborModel) syncPreview(msg tea.Msg, visible bool) tea.Cmd {
	state := &m.preview
	var target previewTarget
	reader, available := m.service.(previewReader)
	var geometry sessionGeometry
	if visible && available && m.discoveryNotice == "" {
		geometry = m.sessionGeometry()
	}
	if geometry.split {
		if selected, ok := m.selected(); ok {
			target, _ = targetForPreview(selected)
		}
	}
	var timer tea.Cmd
	if target != state.target {
		if state.cancel != nil {
			state.cancel()
		}
		*state = previewState{target: target, generation: state.generation + 1, busy: state.busy}
		if target.id != "" {
			state.ctx, state.cancel = context.WithCancel(m.refreshContext)
			timer = waitView(state.ctx, 150*time.Millisecond, previewDue(state.generation))
		}
	}
	switch event := msg.(type) {
	case previewDue:
		if uint64(event) == state.generation {
			state.due = true
		}
	case previewDone:
		if state.busy == event.generation {
			state.busy = 0
		}
		if event.generation == state.generation && target.id != "" {
			state.frame = previewSnapshot{}
			state.message = ""
			if event.err != nil {
				state.message = event.err.Error()
			} else if len(event.frame.lines) == 0 {
				state.message = "Terminal screen is blank"
			} else {
				state.frame = event.frame
			}
			timer = waitView(state.ctx, 2*time.Second, previewDue(state.generation))
		}
	}
	if selected, ok := m.selected(); state.offset != 0 && ok {
		state.offset = min(state.offset, m.previewScrollLimit(selected, geometry))
	}
	if target.id != "" && state.due && state.busy == 0 {
		state.due = false
		state.busy = state.generation
		ctx, gen := state.ctx, state.generation
		return func() tea.Msg { frame, err := reader.ReadPreview(ctx, target); return previewDone{gen, frame, err} }
	}
	return timer
}
