package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

const directoryCap = 2000

type directoryEntry struct{ path, source string }
type directoryPicker struct {
	previewPath, preview       string
	previewGeneration          uint64
	previewCancel              context.CancelFunc
	scope                      string
	query                      textinput.Model
	saved, root, filter        string
	entries                    []directoryEntry
	cursor                     int
	list, searching, truncated bool
	err                        error
	generation                 uint64
	cancel                     context.CancelFunc
}
type directoryResult struct {
	generation uint64
	entries    []directoryEntry
	truncated  bool
	err        error
}

// Read bounded batches: even a single huge directory does not allocate an unbounded listing.
// Symlinks are not followed, including the selected root; selection still uses service validation.
func scanDirectories(ctx context.Context, root string, recursive bool, generation uint64) directoryResult {
	r := directoryResult{generation: generation}
	info, err := os.Lstat(root)
	if err != nil {
		r.err = err
		return r
	}
	if !info.IsDir() {
		r.err = fmt.Errorf("browse root must be a directory, not a symlink")
		return r
	}
	visited := 0
	var walk func(string) error
	walk = func(path string) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		defer f.Close()
		for {
			batch, e := f.ReadDir(64)
			for _, d := range batch {
				if err := ctx.Err(); err != nil {
					return err
				}
				visited++
				if visited > directoryCap {
					r.truncated = true
					return io.EOF
				}
				if !d.IsDir() {
					continue
				}
				p := filepath.Join(path, d.Name())
				r.entries = append(r.entries, directoryEntry{p, "browse"})
				if recursive {
					if err := walk(p); err != nil {
						return err
					}
				}
			}
			if e == io.EOF {
				return nil
			}
			if e != nil {
				return e
			}
		}
	}
	err = walk(root)
	if err != nil && !errors.Is(err, io.EOF) {
		r.err = err
	}
	sort.Slice(r.entries, func(i, j int) bool { return r.entries[i].path < r.entries[j].path })
	if r.err == nil {
		r.entries = append(r.entries, directoryEntry{root, "browse root"})
	}
	return r
}
func (m *harborModel) openDirectoryPicker() tea.Cmd {
	m.picker.stopPreview()
	p := directoryPicker{saved: m.input.Value(), generation: m.picker.generation + 1, root: m.environment.Directory, scope: "known sources", previewGeneration: m.picker.previewGeneration + 1}
	p.query = textinput.New()
	p.query.Prompt = "Search: "
	p.query.CharLimit = 4096
	p.query.Focus()
	seen := map[string]int{}
	add := func(path, source string) {
		if path == "" {
			return
		}
		path = filepath.Clean(path)
		if i, ok := seen[path]; ok {
			p.entries[i].source += " / " + source
			return
		}
		seen[path] = len(p.entries)
		p.entries = append(p.entries, directoryEntry{path, source})
	}
	if m.formActive {
		add(m.draft.Directory, "form directory")
		p.root = m.draft.Directory
	}
	add(os.Getenv("HERDR_ACTIVE_PANE_CWD"), "current pane")
	add(os.Getenv("AGENT_HARBOR_BRIDGE_LAUNCH_CWD"), "launch")
	add(m.environment.Directory, "starting directory")
	for _, path := range m.environment.Projects {
		add(path, "recent")
	}
	m.picker = p
	m.screen = "new-directory"
	return m.requestDirectoryPreview()
}
func (m *harborModel) browseDirectory(root string, recursive bool) tea.Cmd {
	p := &m.picker
	p.stopPreview()
	if p.cancel != nil {
		p.cancel()
	}
	p.generation++
	ctx, cancel := context.WithCancel(context.Background())
	p.cancel = cancel
	p.scope = "children"
	if recursive {
		p.scope = "recursive"
	}
	p.root = filepath.Clean(root)
	p.searching = true
	p.err = nil
	p.truncated = false
	p.entries = nil
	p.cursor = 0
	generation, path := p.generation, p.root
	return func() tea.Msg { return scanDirectories(ctx, path, recursive, generation) }
}
func (p directoryPicker) matches() []directoryEntry {
	texts := make([]string, len(p.entries))
	for i, e := range p.entries {
		texts[i] = e.path
	}
	out := []directoryEntry{}
	for _, h := range searchMatches(p.filter, texts) {
		out = append(out, p.entries[h.Index])
	}
	return out
}
func (m harborModel) updateDirectory(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	p := &m.picker
	key := msg.String()
	rows := p.matches()
	switch key {
	case "esc":
		p.stopPreview()
		if p.cancel != nil {
			p.cancel()
		}
		p.generation++
		m.screen = "new-project"
		if m.formActive {
			m.screen = "new-form"
		}
		m.input.SetValue(p.saved)
		return m, nil
	case "tab":
		p.list = !p.list
		if p.list {
			p.query.Prompt = "List: "
			p.query.Blur()
		} else {
			p.query.Prompt = "Search: "
			p.query.Focus()
		}
		return m, nil
	case "ctrl+r":
		root := p.root
		if len(rows) > 0 {
			root = rows[min(p.cursor, len(rows)-1)].path
		}
		return m, m.browseDirectory(root, true)
	case "ctrl+x":
		p.stopPreview()
		if p.cancel != nil {
			p.cancel()
		}
		p.generation++
		p.searching = false
		p.err = errors.New("Search cancelled; browse or search again")
		return m, nil
	case "up":
		p.cursor = max(0, p.cursor-1)
		return m, m.requestDirectoryPreview()
	case "down":
		p.cursor = min(max(0, len(rows)-1), p.cursor+1)
		return m, m.requestDirectoryPreview()
	case "enter":
		if len(rows) == 0 || p.searching {
			return m, nil
		}
		path := rows[min(p.cursor, len(rows)-1)].path
		if err := m.service.ValidateDirectory(path); err != nil {
			p.err = err
			return m, nil
		}
		if p.cancel != nil {
			p.cancel()
		}
		p.generation++
		p.stopPreview()
		m.input.SetValue(path)
		m.input.CursorEnd()
		m.screen = "new-project"
		m.notice = "Directory selected. Enter continues."
		if m.formActive {
			m.draft.Directory = path
			m.screen = "new-form"
			m.notice = "Directory selected"
		}
		return m, nil
	case "right", "left":
		if p.list {
			root := filepath.Dir(p.root)
			if key == "right" {
				if len(rows) == 0 {
					return m, nil
				}
				root = rows[min(p.cursor, len(rows)-1)].path
			}
			p.filter = ""
			p.query.SetValue("")
			return m, m.browseDirectory(root, false)
		}
	}
	if p.list {
		return m, nil
	}
	old := p.query.Value()
	var cmd tea.Cmd
	p.query, cmd = p.query.Update(msg)
	if old != p.query.Value() {
		q := p.query.Value()
		p.filter = q
		p.cursor = 0
		if strings.HasPrefix(q, "/") || strings.HasPrefix(q, "~/") {
			path := q
			if strings.HasPrefix(path, "~/") {
				path = filepath.Join(os.Getenv("HOME"), path[2:])
			}
			root := filepath.Dir(path)
			p.filter = filepath.Base(path)
			if strings.HasSuffix(q, "/") {
				root = path
				p.filter = ""
			}
			return m, tea.Batch(cmd, m.browseDirectory(root, false))
		}
		return m, tea.Batch(cmd, m.requestDirectoryPreview())
	}
	return m, cmd
}
func (m harborModel) directoryView() string {
	p := m.picker
	w := max(20, min(140, m.width-4))
	p.query.Width = max(10, w-12)
	focus := "query"
	if p.list {
		focus = "list"
	}
	lines := []string{"📂 Choose a project directory", p.query.View(), ansi.Truncate("Scope: "+p.scope+" · "+cleanUI(p.root)+" · "+focus, w, "…")}
	status := "Ctrl+R recursive selected root · Ctrl+X stop"
	if p.searching {
		status = "Searching… Ctrl+X cancels"
	} else if p.err != nil {
		status = "Error: " + p.err.Error()
	} else if p.truncated {
		status = fmt.Sprintf("Truncated at %d entries; narrow the root", directoryCap)
	}
	lines = append(lines, ansi.Truncate(cleanUI(status), w, "…"))
	rows := p.matches()
	count := max(1, m.height-12)
	start := m.listStart(p.cursor, count)
	if len(rows) == 0 && !p.searching && p.err == nil {
		lines = append(lines, "No matching directories.")
	}
	for i := start; i < min(len(rows), start+count); i++ {
		e := rows[i]
		mark := "  "
		if i == p.cursor {
			mark = "› "
		}
		line := ansi.Truncate(cleanUI(mark+"["+e.source+"] "+e.path), w, "…")
		lines = append(lines, line)
	}
	if m.width >= 100 && len(rows) > 0 {
		preview := "Preview: loading…"
		if p.previewPath == rows[min(p.cursor, len(rows)-1)].path && p.preview != "" {
			preview = p.preview
		}

		lines = append(lines, ansi.Truncate(cleanUI(preview), w, "…"))
	}
	return strings.Join(lines, "\n")
}

// Apply spans after fitting/painting, which intentionally sanitizes input ANSI.
func (m harborModel) paintDirectoryMatches(lines []string) []string {
	if m.screen != "new-directory" || colorOff() {
		return lines
	}
	p := m.picker
	rows := p.matches()
	count := max(1, m.height-12)
	start := m.listStart(p.cursor, count)
	for i := 4; i < len(lines) && i-4+start < len(rows) && i-4 < count; i++ {
		path := rows[i-4+start].path
		lines[i] = highlightPositions(lines[i], candidatePositions(lines[i], p.filter, []string{path}))
	}
	return lines
}

// Preview effects are bounded and never run from View. Path plus generation
// guards prevent late reads from painting another selection or a reopened picker.
type directoryPreviewResult struct {
	generation uint64
	path, text string
}

func (p *directoryPicker) stopPreview() {
	if p.previewCancel != nil {
		p.previewCancel()
	}
	p.previewGeneration++
	p.previewPath = ""
	p.preview = ""
}
func loadDirectoryPreview(ctx context.Context, path string, generation uint64) directoryPreviewResult {
	r := directoryPreviewResult{generation: generation, path: path, text: "Preview: unreadable"}
	if ctx.Err() != nil {
		return r
	}
	f, err := os.Open(path)
	if err != nil {
		return r
	}
	defer f.Close()
	entries, err := f.ReadDir(4)
	if ctx.Err() != nil {
		return r
	}
	if err == nil || err == io.EOF {
		names := []string{}
		for _, e := range entries {
			names = append(names, e.Name())
		}
		sort.Strings(names)
		r.text = "Children: " + strings.Join(names, " · ")
	}
	return r
}
func (m *harborModel) requestDirectoryPreview() tea.Cmd {
	p := &m.picker
	rows := p.matches()
	path := ""
	if len(rows) > 0 {
		path = rows[min(p.cursor, len(rows)-1)].path
	}
	if path == p.previewPath {
		return nil
	}
	p.stopPreview()
	if path == "" {
		return nil
	}
	p.previewPath = path
	p.preview = "Preview: loading…"
	ctx, cancel := context.WithCancel(context.Background())
	p.previewCancel = cancel
	generation := p.previewGeneration
	return func() tea.Msg { return loadDirectoryPreview(ctx, path, generation) }
}
