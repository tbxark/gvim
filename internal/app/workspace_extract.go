package app

// Extract tab to new workspace (#19b first slice) and the close-workspace running-terminal
// report. Fresh's extract_tab_to_new_workspace moves a file tab into a new workspace
// co-tenanting the same project root, carrying the live buffer's unsaved text and activating
// the new workspace. gvim's registry v1 still keeps one workspace per canonical root, so
// this slice roots the extracted tab at the FILE'S FOLDER: a new workspace for that folder,
// its session written with the tab's cursor/top/page view and a stash of its unsaved text
// before the source tab is force-closed. A folder that already is a workspace root (the
// current one, or any other) is refused with a clear notice - the co-tenant model over one
// root stays #19b - so the tab and its text are never silently dropped.
//
// Update does no blocking I/O here: ExtractTab only reads the in-memory editor state and
// returns a tea command; the registry, stash, session and materialization work runs in that
// command (off the UI goroutine), and only the force-close of the source tab and the switch
// are marshalled back onto the UI goroutine.

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/tbxark/gvim/buffer"
	"github.com/tbxark/gvim/internal/daemon"
	"github.com/tbxark/gvim/internal/tui"
	"github.com/tbxark/gvim/session"
)

// extractTab is the captured state of the tab being extracted.
type extractTab struct {
	sourceID string // the workspace the tab comes from
	path     string
	name     string
	destRoot string // the file's folder, as a path (the command canonicalizes it)
	label    string
	file     session.FileState
	unsaved  string
}

// ExtractTab implements the TUI's optional workspace extract action. It runs on the UI
// goroutine (the palette row or the tab context menu): it captures the active tab's state
// and returns a command for the disk work, or an error the UI shows while the tab stays
// open. A large-file buffer is refused: its overlay stash is not carried by this slice
// (the tab is left untouched).
func (m *workspaceManager) ExtractTab() (tui.WorkspaceCmd, error) {
	m.mu.Lock()
	sess := m.sessions[m.active]
	m.mu.Unlock()
	if sess == nil {
		return nil, errors.New("cannot extract: the daemon is not ready")
	}
	doc := sess.rt.ed.Active()
	if doc == nil || doc.Virtual() || doc.Path == "" {
		return nil, errors.New("cannot extract: buffer has no file path")
	}
	if doc.Large() {
		return nil, errors.New("cannot extract: a large-file buffer's unsaved overlay is not carried yet")
	}
	ext := &extractTab{
		sourceID: sess.ws.File.StableID,
		path:     doc.Path,
		name:     filepath.Base(doc.Path),
		destRoot: filepath.Dir(doc.Path),
		label:    filepath.Base(filepath.Clean(filepath.Dir(doc.Path))),
		file: session.FileState{
			Path: doc.Path,
			Line: doc.Cursor.Line + 1,
			Col:  doc.Cursor.Col + 1,
			Top:  doc.Top,
		},
	}
	if view := doc.View(); view != nil {
		if view.PageView != nil {
			on := *view.PageView
			ext.file.PageView = &on
		}
		ext.file.PageWidth = view.PageWidth
	}
	if n := doc.VirtualLines(); n > 0 {
		ext.file.VirtualLines, ext.file.VirtualColumn = n, doc.VirtualLineColumn()
	}
	if doc.Modified() {
		text, err := doc.ReadRange(buffer.Pos{}, doc.Buffer.End())
		if err != nil {
			return nil, fmt.Errorf("cannot extract: read the unsaved text: %w", err)
		}
		ext.unsaved = text
		if size, mtime, ok := doc.DiskStamp(); ok {
			ext.file.Size, ext.file.MTimeUnixNano = size, mtime
		}
	}
	// The same-root and existing-workspace decisions need canonical paths, i.e. disk
	// metadata, so the command makes them (Update does no blocking I/O). A refusal then
	// arrives as a notice and leaves the tab open, exactly as if it had been immediate.
	return tui.WorkspaceCmdOf(func() { m.runExtractTab(ext) }), nil
}

// sameRootExtractError is the refusal of an extraction whose file folder already is the
// workspace root: the tab would have to move into a co-tenant workspace over that root,
// which the registry v1 (one workspace per root) does not have yet.
func sameRootExtractError(root string) error {
	return fmt.Errorf("cannot extract: %s is already the workspace root; a second workspace over the same root is not supported yet (the tab stays open)", root)
}

// runExtractTab is the command body: canonicalize and validate the destination, create the
// workspace and write its session (with the unsaved stash), then close the source tab and
// switch on the UI goroutine. Any failure before the tab closes leaves the tab and its text
// untouched and reports why.
func (m *workspaceManager) runExtractTab(ext *extractTab) {
	m.mu.Lock()
	sess := m.sessions[ext.sourceID]
	active := m.active
	m.mu.Unlock()
	if sess == nil || active != ext.sourceID {
		m.notify(errors.New("cannot extract: the active workspace changed while extracting; nothing was moved"))
		return
	}
	canonical, err := daemon.CanonicalRoot(ext.destRoot)
	if err != nil {
		m.notify(fmt.Errorf("cannot extract: %w", err))
		return
	}
	if canonical == sess.ws.File.Root {
		m.notify(sameRootExtractError(canonical))
		return
	}
	if list, _ := m.reg.List(); len(list) > 0 {
		for _, w := range list {
			if w.File.Root == canonical {
				m.notify(fmt.Errorf("cannot extract: a workspace already exists for %s (%q); extracting into an existing workspace is not supported yet; the tab stays open", canonical, w.File.Label))
				return
			}
		}
	}
	ws, created, err := m.reg.Create(canonical, ext.label)
	if err != nil {
		m.notify(fmt.Errorf("cannot extract: create the workspace: %w", err))
		return
	}
	if !created {
		m.notify(fmt.Errorf("cannot extract: a workspace already exists for %s (%q); the tab stays open", canonical, ws.File.Label))
		return
	}
	id := ws.File.StableID
	discard := func() { _ = m.reg.Remove(id) }
	dir, err := m.reg.SessionDir(id)
	if err != nil {
		discard()
		m.notify(fmt.Errorf("cannot extract: %w", err))
		return
	}
	file := ext.file
	if ext.unsaved != "" {
		name, err := session.StashContent(dir, ext.unsaved)
		if err != nil {
			discard()
			m.notify(fmt.Errorf("cannot extract: stash the unsaved text: %w", err))
			return
		}
		file.StashFile = name
	}
	layout := session.Session{
		Version:  session.Version,
		Root:     canonical,
		Explorer: true,
		Active:   0,
		Files:    []session.FileState{file},
	}
	if err := session.Save(dir, layout); err != nil {
		discard()
		m.notify(fmt.Errorf("cannot extract: write the new workspace's session: %w", err))
		return
	}
	_, _ = m.reg.UpdateLayout(id, &layout)

	var wait func() error
	var serr error
	if !m.runOnUI(func() {
		m.mu.Lock()
		active := m.active
		cur := m.sessions[ext.sourceID]
		m.mu.Unlock()
		if active != ext.sourceID || cur == nil {
			serr = errors.New("cannot extract: the active workspace changed while extracting; the tab stays open")
			return
		}
		if !cur.rt.model.CloseTabForWorkspaceExtract(ext.path) {
			serr = errors.New("cannot extract: the active tab changed while extracting; the tab stays open")
			return
		}
		wait, serr = m.beginSelect(id)
		if serr != nil {
			// The switch could not start: put the tab back is not available here, so
			// report and keep the new workspace (its session has the text) for a retry.
			return
		}
	}) {
		discard()
		m.notify(errors.New("cannot extract: the daemon is not ready; the tab stays open"))
		return
	}
	if serr != nil {
		err := serr
		if errors.Is(err, errWorkspaceStale) {
			// Another request won the switch: the tab was already closed and the new
			// workspace holds its text; report the switch instead of a loss.
			m.notify(errors.New("cannot extract: another workspace request superseded the switch; the text is kept in the new workspace"))
			return
		}
		discard()
		m.notify(fmt.Errorf("cannot extract: %w; the tab stays open", err))
		return
	}
	if wait != nil {
		if err := wait(); err != nil {
			m.notify(fmt.Errorf("cannot extract: switch to the new workspace: %w", err))
			return
		}
	}
	m.report(fmt.Sprintf("Extracted %s into workspace %q", ext.name, ws.File.Label))
}

// RunningTerminals implements the TUI's optional close surface: the running dock terminals
// of one materialized workspace, for the close confirmation and report. A dormant
// workspace has no runtime and no processes.
func (m *workspaceManager) RunningTerminals(id string) []tui.WorkspaceRunningTerminal {
	m.mu.Lock()
	sess := m.sessions[id]
	m.mu.Unlock()
	if sess == nil {
		return nil
	}
	return sess.rt.model.RunningTerminals()
}

// notifyText shows a workspace action's successful report on the status row without
// recording it for the socket protocol (a rename answers with the list only; the close
// report is the protocol's one report).
func (m *workspaceManager) notifyText(text string) {
	if text == "" {
		return
	}
	m.mu.Lock()
	m.lastNotice = text
	h := m.host
	m.mu.Unlock()
	if h != nil {
		h.Notify(text)
	}
}

// report shows a successful workspace action on the status row (a close that stopped
// terminals, an extraction) and remembers it for the socket protocol's close answer.
func (m *workspaceManager) report(text string) {
	if text == "" {
		return
	}
	m.notifyText(text)
	m.mu.Lock()
	m.lastReport = text
	m.mu.Unlock()
}

// takeReport returns and clears the report of the last workspace action.
func (m *workspaceManager) takeReport() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	text := m.lastReport
	m.lastReport = ""
	return text
}

// runningTerminalSummary names the terminals a close stops, with their agents: the same
// wording the TUI confirmation uses.
func runningTerminalSummary(running []tui.WorkspaceRunningTerminal) string {
	parts := make([]string, len(running))
	for i, r := range running {
		name := r.Name
		if name == "" {
			name = "terminal"
		}
		if r.Agent != "" {
			parts[i] = name + " (agent " + r.Agent + ")"
		} else {
			parts[i] = name
		}
	}
	noun := "terminal"
	if len(running) != 1 {
		noun = "terminals"
	}
	return fmt.Sprintf("%d running %s: %s", len(running), noun, strings.Join(parts, ", "))
}
