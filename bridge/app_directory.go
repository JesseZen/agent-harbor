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
	details                    bool
	detailScroll               int
	previewPath, preview       string
	previewGeneration          uint64
	previewCancel              context.CancelFunc
	scope                      string
	query                      textinput.Model
	saved, root, filter        string
	entries                    []directoryEntry
	known                      []directoryEntry
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
	p.known = append([]directoryEntry(nil), p.entries...)
	m.uiFocus = "search"
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
		m.uiFocus = ""
		m.formFocus = 0
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
		n, c, _ := m.serviceTask(func() error { return m.service.ValidateDirectory(path) }, func(n *harborModel, err error) tea.Cmd {
			p := &n.picker
			if err != nil {
				p.err = err
				return nil
			}
			if p.cancel != nil {
				p.cancel()
			}
			p.generation++
			p.stopPreview()
			n.uiFocus = ""
			n.formFocus = 0
			n.input.SetValue(path)
			n.input.CursorEnd()
			n.screen = "new-project"
			n.notice = "Directory selected. Enter continues."
			if n.formActive {
				n.draft.Directory = path
				n.screen = "new-form"
				n.notice = "Directory selected"
			}
			return nil
		})
		return n, c
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
		if p.cancel != nil {
			p.cancel()
		}
		p.generation++
		p.searching = false
		p.err = nil
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
		if p.scope != "known sources" {
			p.entries = append([]directoryEntry(nil), p.known...)
			p.scope = "known sources"
		}
		return m, tea.Batch(cmd, m.requestDirectoryPreview())
	}
	return m, cmd
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

// Directory controls use the same layout and stable object IDs as the other pages.
func (m harborModel) directoryPage() uiPage {
	p := newUIPage(m.width, m.height)
	d := m.picker
	rows := d.matches()
	p.title("Choose project")
	p.search(d.query.Value(), "Search known projects or enter /path/", m.uiFocus == "search", d.query)
	scope := "Known projects · search only known paths"
	if d.scope != "known sources" {
		scope = "Browse: " + d.scope + " · " + cleanUI(d.root)
	}
	p.line(paint(fg("muted"), scope))
	status := ""
	if d.searching {
		status = "Searching… Ctrl+X stop"
	} else if d.err != nil {
		status = "Error: " + cleanUI(d.err.Error()) + " · Retry or Known"
	} else if d.truncated {
		status = "Results truncated; narrow browse root"
	} else if len(rows) == 0 {
		status = "No matching directories; change search or browse"
	}
	if status != "" {
		p.line(status)
	}
	p.Hint = "↑↓ select · Tab focus"
	// Reserve the fixed shortcut bar before sizing the directory list.
	disabled := ""
	if len(rows) == 0 || d.searching {
		disabled = "Select a directory first"
	}
	p.shortcuts([]uiAction{{ID: "dir-use", Label: "Use this folder", Key: "enter", Primary: true, Disabled: disabled}, {ID: "dir-cancel", Label: "Cancel", Key: "esc"}}, m.uiFocus)
	p.shortcuts([]uiAction{{ID: "dir-browse", Label: "Browse", Key: "ctrl+b"}, {ID: "dir-recursive", Label: "Recursive", Key: "ctrl+r"}, {ID: "dir-known", Label: "Known", Key: "ctrl+k"}}, m.uiFocus)
	p.shortcuts([]uiAction{{ID: "dir-retry", Label: "Retry", Key: "ctrl+t"}, {ID: "dir-details", Label: "Preview", Key: "ctrl+p"}}, m.uiFocus)
	if d.details {
		selected := d.root
		if len(rows) > 0 {
			selected = rows[min(d.cursor, len(rows)-1)].path
		}
		text := cleanUI(selected) + "\n" + cleanUI(d.preview)
		if d.err != nil {
			text += "\nError: " + cleanUI(d.err.Error())
		}
		lines := strings.Split(ansi.Wrap(text, max(1, p.Width-4), ""), "\n")
		p.Hint = "↑↓ scroll path · Esc back to results"
		available := max(0, p.Height-len(p.Lines)-len(p.shortcutBar().Lines))
		offset := min(d.detailScroll, max(0, len(lines)-available))
		for _, line := range lines[offset:min(len(lines), offset+available)] {
			p.line(documentLine(line))
		}
		return p.finish(0)
	}
	remaining := max(0, p.Height-len(p.Lines)-len(p.shortcutBar().Lines))
	listTop := len(p.Lines)
	count := remaining / 2
	start := max(0, d.cursor-count+1)
	for i := start; i < min(len(rows), start+count); i++ {
		e := rows[i]
		y := len(p.Lines)
		id := "dir-path:" + e.path
		selected := e.path == m.draft.Directory
		row := m.rowStyle(id, i == d.cursor && d.list, selected)
		mark := "📂"
		if selected {
			mark = selectorMark(true) + " " + mark
		}
		name := row.marker() + mark + " " + cleanUI(filepath.Base(e.path))
		source := " · " + cleanUI(e.source)
		name = ansi.Truncate(name, max(4, p.Width-2-ansi.StringWidth(source)), "…")
		p.line(paint(row.style("text"), name) + row.line(source, "muted", p.Width-2-ansi.StringWidth(name)))
		p.line(row.line("    "+cleanUI(e.path), "value", p.Width-2))
		p.hit(id, 1, y, p.Width-2, 2)
	}

	p.scrollRail(listTop, len(p.Lines)-listTop, len(rows)*2, start*2)
	return p.finish(0)
}
func (m harborModel) directoryPageInput(msg tea.Msg) (tea.Model, tea.Cmd, bool) {
	activate := func(id string) (tea.Model, tea.Cmd, bool) {
		d := &m.picker
		switch {
		case id == "dir-details":
			d.details = !d.details
			d.detailScroll = 0
			return m, nil, true
		case id == "search":
			m.uiFocus = "search"
			d.list = false
			d.query.Focus()
			return m, nil, true
		case strings.HasPrefix(id, "dir-path:"):
			for i, e := range d.matches() {
				if "dir-path:"+e.path == id {
					d.cursor = i
					break
				}
			}
			m.uiFocus = "dir-list"
			d.list = true
			d.query.Blur()
			return m, m.requestDirectoryPreview(), true
		case id == "dir-known":
			if d.cancel != nil {
				d.cancel()
			}
			d.generation++
			d.searching = false
			d.err = nil
			d.scope = "known sources"
			d.entries = append([]directoryEntry(nil), d.known...)
			d.query.SetValue("")
			d.filter = ""
			d.cursor = 0
			return m, m.requestDirectoryPreview(), true
		case id == "dir-browse" || id == "dir-recursive" || id == "dir-retry":
			root := d.root
			rows := d.matches()
			if id != "dir-retry" && len(rows) > 0 {
				root = rows[min(d.cursor, len(rows)-1)].path
			}
			d.query.SetValue("")
			d.filter = ""
			return m, m.browseDirectory(root, id == "dir-recursive" || id == "dir-retry" && d.scope == "recursive"), true
		case id == "dir-use":
			n, c := m.updateDirectory(uiKey("enter"))
			return n, c, true
		case id == "dir-cancel":
			n, c := m.updateDirectory(uiKey("esc"))
			return n, c, true
		}
		return m, nil, true
	}
	if mouse, ok := msg.(tea.MouseMsg); ok {
		if mouse.Action == tea.MouseActionPress {
			if mouse.Button == tea.MouseButtonLeft {
				return activate(m.directoryPage().target(mouse.X, mouse.Y))
			}
			key := ""
			if mouse.Button == tea.MouseButtonWheelDown {
				key = "down"
			}
			if mouse.Button == tea.MouseButtonWheelUp {
				key = "up"
			}
			if key != "" {
				n, c := m.updateDirectory(uiKey(key))
				return n, c, true
			}
		}
		return m, nil, true
	}
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil, false
	}
	if m.picker.details {
		switch key.String() {
		case "esc":
			m.picker.details = false
			return m, nil, true
		case "up":
			m.picker.detailScroll = max(0, m.picker.detailScroll-1)
			return m, nil, true
		case "down":
			m.picker.detailScroll++
			return m, nil, true
		}
	}
	switch key.String() {
	case "ctrl+p":
		return activate("dir-details")
	case "tab", "shift+tab":
		ids := []string{"search", "dir-list"}
		i := 0
		for j, id := range ids {
			if id == m.uiFocus {
				i = j
			}
		}
		step := 1
		if key.String() == "shift+tab" {
			step = len(ids) - 1
		}
		m.uiFocus = ids[(i+step)%len(ids)]
		m.picker.list = m.uiFocus != "search"
		if m.picker.list {
			m.picker.query.Blur()
		} else {
			m.picker.query.Focus()
		}
		return m, nil, true
	case "ctrl+b":
		return activate("dir-browse")
	case "ctrl+r":
		return activate("dir-recursive")
	case "ctrl+k":
		return activate("dir-known")
	case "ctrl+t":
		return activate("dir-retry")
	case "enter":
		if strings.HasPrefix(m.uiFocus, "dir-") && m.uiFocus != "dir-list" {
			return activate(m.uiFocus)
		}
	}
	n, c := m.updateDirectory(key)
	return n, c, true
}
