package app

// Extract tab to new workspace and the close-workspace running-terminal report. Fresh's
// extract_tab_to_new_workspace moves a file tab into a new CO-TENANT workspace over the same
// project root (labels "proj", "proj (2)", ...), carrying the live buffer's unsaved text and
// activating the new workspace. Registry v2 allows several workspaces per canonical root, so
// this now matches Fresh: the new workspace shares the current workspace's root, its session
// is written with the tab's cursor/top/page view and a stash of the unsaved text before the
// source tab is force-closed, and both workspaces keep independent sessions. The
// large-file-overlay and no-file-path cases stay refused (the tab is left untouched).
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
	"github.com/tbxark/gvim/editor"
	"github.com/tbxark/gvim/internal/daemon"
	"github.com/tbxark/gvim/internal/tui"
	"github.com/tbxark/gvim/session"
)

// extractTab is the captured state of the tab being extracted.
type extractTab struct {
	sourceID string // the workspace the tab comes from
	path     string
	name     string
	destRoot string // the workspace root shared with the source workspace (the co-tenant)
	file     session.FileState
	unsaved  string
	// mru is the source editor's most-recently-used order restricted to the extracted
	// documents (newest first). The new workspace's session carries it so completion and
	// dabbrev order survive the extraction; a moved tab is a named file, so only Path is
	// used.
	mru []session.MruFile
}

// extractMru returns the source editor's MRU order for the selected named documents,
// newest first. It reads RecentTabIndices before any capture activates tabs; a path that
// is no longer open is simply absent.
func extractMru(ed *editor.Editor, paths map[string]bool) []session.MruFile {
	if ed == nil || len(paths) == 0 {
		return nil
	}
	var mru []session.MruFile
	seen := make(map[string]bool, len(paths))
	tabs := ed.Tabs()
	for _, tabIndex := range ed.RecentTabIndices() {
		if tabIndex < 0 || tabIndex >= len(tabs) {
			continue
		}
		path := tabs[tabIndex].Path
		if path == "" || !paths[path] || seen[path] {
			continue
		}
		seen[path] = true
		mru = append(mru, session.MruFile{Path: path})
	}
	return mru
}

// activeForMru returns the saved-files index of the first MRU reference that matches a
// file, or 0. A restored session makes that tab active, and RecentTabIndices puts the
// active tab first, so this is what makes the saved MRU order the restored order after a
// new/modified session is materialized.
func activeForMru(files []session.FileState, mru []session.MruFile) int {
	for _, ref := range mru {
		for i, f := range files {
			if f.Path != ref.Path {
				continue
			}
			if f.Path == "" && f.Label != ref.Label {
				continue
			}
			return i
		}
	}
	return 0
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
		// Fresh's extract keeps the source project root; the registry v2 co-tenant is
		// another workspace over the same root, distinguished by its durable id and its
		// numbered label.
		sourceID: sess.ws.File.StableID,
		path:     doc.Path,
		name:     filepath.Base(doc.Path),
		destRoot: sess.ws.File.Root,
		mru:      []session.MruFile{{Path: doc.Path}},
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
	return tui.WorkspaceCmdOf(func() { m.runExtractTab(ext) }), nil
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
	// Registry v2 co-tenant: a new workspace over the SAME root, never a reuse (Create
	// would return the root's freshest workspace). The label gets Fresh's precedence.
	ws, _, err := m.reg.CreateCoTenant(canonical, "")
	if err != nil {
		m.notify(fmt.Errorf("cannot extract: create the co-tenant workspace: %w", err))
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
		Active:   activeForMru([]session.FileState{file}, ext.mru),
		Files:    []session.FileState{file},
		MRU:      ext.mru,
	}
	if err := session.Save(dir, layout); err != nil {
		discard()
		m.notify(fmt.Errorf("cannot extract: write the new workspace's session: %w", err))
		return
	}
	_, _ = m.reg.UpdateLayout(id, &layout)

	var wait func() error
	var serr error
	var closed bool
	if !m.runOnUI(func() {
		m.mu.Lock()
		active := m.active
		cur := m.sessions[ext.sourceID]
		m.mu.Unlock()
		if active != ext.sourceID || cur == nil {
			serr = errors.New("cannot extract: the active workspace changed while extracting; the tab stays open")
			return
		}
		if !cur.rt.model.CanCloseTabForWorkspaceExtract(ext.path) {
			serr = errors.New("cannot extract: the active tab changed while extracting; the tab stays open")
			return
		}
		// Safety order: the switch is queued FIRST and the source tab is closed only
		// after it was accepted. A refused switch therefore leaves the tab (and its
		// unsaved text) exactly where it was; no scenario closes the tab and then loses
		// the workspace it was moving to.
		wait, serr = m.beginExtractSelect(id)
		if serr != nil {
			return
		}
		if !cur.rt.model.CloseTabForWorkspaceExtract(ext.path) {
			// The tab changed between the check above and the close; the switch is
			// already queued, so the extracted copy is kept in the new workspace and the
			// source tab stays open (reported below, nothing is dropped).
			return
		}
		closed = true
		// The switch's own capture was taken before this close and still shows the
		// extracted tab; a second snapshot now records the source workspace without it,
		// so switching back never resurrects a duplicate tab.
		m.enqueueCapture(cur)
	}) {
		discard()
		m.notify(errors.New("cannot extract: the daemon is not ready; the tab stays open"))
		return
	}
	if serr != nil {
		// The switch was refused synchronously, before the tab was closed: the tab is
		// untouched, so dropping the just-created co-tenant loses nothing (its session
		// data is kept like any closed workspace's).
		discard()
		m.notify(fmt.Errorf("cannot extract: %w; the tab stays open", serr))
		return
	}
	if !closed {
		// The switch is queued but the source tab changed before it could be closed: the
		// text is in the new workspace's session and the source tab is still open. Keep
		// both and report exactly that state.
		if wait != nil {
			if err := wait(); err != nil {
				m.notify(fmt.Errorf("cannot extract: the switch failed (%v); the source tab is still open and the text is kept in workspace %q", err, ws.File.Label))
				return
			}
		}
		m.report(fmt.Sprintf("Extracted %s into workspace %q; the source tab changed and is still open", ext.name, ws.File.Label))
		return
	}
	if wait != nil {
		if err := wait(); err != nil {
			// The tab was closed and its unsaved text lives in the new workspace's
			// session: never discard it, and say where it was kept.
			m.notify(fmt.Errorf("cannot extract: open the new workspace %q: %w; the tab's text is kept there", ws.File.Label, err))
			return
		}
	}
	m.report(fmt.Sprintf("Extracted %s into workspace %q", ext.name, ws.File.Label))
}

// beginExtractSelect starts the switch of an extraction. beginSelectFn is a test hook so
// the data-safety regressions can inject a synchronous refusal and a queued-switch failure.
func (m *workspaceManager) beginExtractSelect(id string) (func() error, error) {
	m.mu.Lock()
	hook := m.beginSelectFn
	m.mu.Unlock()
	if hook != nil {
		return hook(id)
	}
	return m.beginSelect(id)
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
