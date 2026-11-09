package app

// Extract-tab variants beyond round 16's "extract to new co-tenant workspace":
// moving a file tab into an EXISTING workspace over the same root (gvim extension;
// Fresh only creates new co-tenants), moving a terminal tab's saved state into
// another workspace (no PTY hand-off: the target revives it on the first dive), and
// extracting several selected file tabs into one new co-tenant workspace (gvim
// extension; Fresh has no multi-tab extract). The data-safety ordering is round
// 16's: the transfer is written/accepted by the target FIRST, the switch is queued
// and accepted, and only then is the source tab closed; every refusal leaves the
// source tab (and its unsaved text) untouched and reports why. Update does no I/O:
// the capture runs on the UI goroutine, the disk/materialization work in the tea
// command, and only the switch/close are marshalled back.

import (
	"errors"
	"fmt"
	"sort"

	"github.com/tbxark/gvim/buffer"
	"github.com/tbxark/gvim/editor"
	"github.com/tbxark/gvim/internal/daemon"
	"github.com/tbxark/gvim/internal/restore"
	"github.com/tbxark/gvim/internal/terminal"
	"github.com/tbxark/gvim/internal/tui"
	"github.com/tbxark/gvim/session"
)

// extractTabTo is a captured file tab on its way to an existing workspace.
type extractTabTo struct {
	extractTab
	destID string
}

// extractTerminalTab is a captured terminal tab on its way to another workspace.
type extractTerminalTab struct {
	sourceID string
	destID   string
	tab      terminal.SavedTab
}

// ExtractTargets implements the TUI's extract-into-existing picker: the other
// co-tenant workspaces over the active workspace's canonical root. It runs on the
// UI goroutine and only reads in-memory registry/session maps (no I/O on Update).
func (m *workspaceManager) ExtractTargets() []tui.WorkspaceEntry {
	m.mu.Lock()
	sess := m.sessions[m.active]
	m.mu.Unlock()
	if sess == nil {
		return nil
	}
	sourceRoot, err := daemon.CanonicalRoot(sess.ws.File.Root)
	if err != nil {
		return nil
	}
	entries, _ := m.reg.List()
	out := make([]tui.WorkspaceEntry, 0, len(entries))
	for _, ws := range entries {
		if ws.File.StableID == sess.ws.File.StableID {
			continue
		}
		root, err := daemon.CanonicalRoot(ws.File.Root)
		if err != nil || root != sourceRoot {
			continue
		}
		m.mu.Lock()
		_, open := m.sessions[ws.File.StableID]
		m.mu.Unlock()
		out = append(out, tui.WorkspaceEntry{
			ID: ws.File.StableID, Label: ws.File.Label, Root: ws.File.Root, Materialized: open,
		})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Label < out[j].Label })
	return out
}

// captureExtractTab reads one document's extract state. It mirrors ExtractTab's
// capture (file path, cursor/top, unsaved text) for a tab that need not be active.
func captureExtractTab(doc *editor.Document, sourceID, destRoot string) (*extractTab, error) {
	if doc == nil || doc.Virtual() || doc.Path == "" {
		return nil, errors.New("cannot extract: buffer has no file path")
	}
	if doc.Large() {
		return nil, errors.New("cannot extract: a large-file buffer's unsaved overlay is not carried yet")
	}
	ext := &extractTab{
		sourceID: sourceID,
		path:     doc.Path,
		name:     doc.Name(),
		destRoot: destRoot,
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
	return ext, nil
}

// ExtractTabTo captures the active file tab for an existing target workspace.
func (m *workspaceManager) ExtractTabTo(id string) (tui.WorkspaceCmd, error) {
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
	ext, err := captureExtractTab(doc, sess.ws.File.StableID, sess.ws.File.Root)
	if err != nil {
		return nil, err
	}
	ext.mru = extractMru(sess.rt.ed, map[string]bool{ext.path: true})
	extTo := &extractTabTo{extractTab: *ext, destID: id}
	return tui.WorkspaceCmdOf(func() { m.runExtractTabTo(extTo) }), nil
}

// runExtractTabTo transfers the captured file state into the existing target
// workspace, then queues the switch and closes the source tab only after both were
// accepted. A target that refuses the transfer leaves the source tab untouched.
func (m *workspaceManager) runExtractTabTo(extTo *extractTabTo) {
	ext := &extTo.extractTab
	m.mu.Lock()
	sess := m.sessions[ext.sourceID]
	active := m.active
	m.mu.Unlock()
	if sess == nil || active != ext.sourceID {
		m.notify(errors.New("cannot extract: the active workspace changed while extracting; nothing was moved"))
		return
	}
	target, ok, err := m.reg.Get(extTo.destID)
	if err != nil || !ok {
		m.notify(errors.New("cannot extract: the target workspace is gone; the tab stays open"))
		return
	}
	sourceRoot, err := daemon.CanonicalRoot(sess.ws.File.Root)
	if err != nil {
		m.notify(fmt.Errorf("cannot extract: %w", err))
		return
	}
	targetRoot, err := daemon.CanonicalRoot(target.File.Root)
	if err != nil || targetRoot != sourceRoot {
		m.notify(errors.New("cannot extract: the target workspace is not over this root; the tab stays open"))
		return
	}
	dir, err := m.reg.SessionDir(target.File.StableID)
	if err != nil {
		m.notify(fmt.Errorf("cannot extract: %w", err))
		return
	}
	file := ext.file
	if ext.unsaved != "" {
		name, err := session.StashContent(dir, ext.unsaved)
		if err != nil {
			m.notify(fmt.Errorf("cannot extract: stash the unsaved text in the target workspace: %w", err))
			return
		}
		file.StashFile = name
	}
	if err := m.acceptExtractTab(target.File.StableID, targetRoot, dir, file, ext.mru); err != nil {
		m.notify(fmt.Errorf("cannot extract: %w; the tab stays open", err))
		return
	}

	var wait func() error
	var serr error
	var closed bool
	if !m.runOnUI(func() {
		m.mu.Lock()
		cur := m.sessions[ext.sourceID]
		activeNow := m.active
		m.mu.Unlock()
		if activeNow != ext.sourceID || cur == nil {
			serr = errors.New("the active workspace changed while extracting")
			return
		}
		if !cur.rt.model.CanCloseTabForWorkspaceExtract(ext.path) {
			serr = errors.New("the active tab changed while extracting")
			return
		}
		// Safety order: transfer (above) -> queued switch accepted -> source tab closed.
		wait, serr = m.beginExtractSelect(target.File.StableID)
		if serr != nil {
			return
		}
		if !cur.rt.model.CloseTabForWorkspaceExtract(ext.path) {
			return
		}
		closed = true
		m.enqueueCapture(cur)
	}) {
		m.notify(errors.New("cannot extract: the daemon is not ready; the tab stays open"))
		return
	}
	if serr != nil {
		m.notify(fmt.Errorf("cannot extract: %w; the tab stays open", serr))
		return
	}
	if !closed {
		if wait != nil {
			if err := wait(); err != nil {
				m.notify(fmt.Errorf("cannot extract: open workspace %q: %v; the text is kept there and the source tab is still open", target.File.Label, err))
				return
			}
		}
		m.report(appTextVars("gvim.workspace.report_named_changed", "Extracted %{name} into workspace %{label}; the source tab changed and is still open", map[string]any{"name": ext.name, "label": fmt.Sprintf("%q", target.File.Label)}))
		return
	}
	if wait != nil {
		if err := wait(); err != nil {
			m.notify(fmt.Errorf("cannot extract: open workspace %q: %v; the tab's text is kept there", target.File.Label, err))
			return
		}
	}
	m.report(appTextVars("gvim.workspace.report_named", "Extracted %{name} into workspace %{label}", map[string]any{"name": ext.name, "label": fmt.Sprintf("%q", target.File.Label)}))
}

// mergeExtractMru puts the moved documents (newest first) before the target's existing
// MRU refs, dropping any old ref for a moved path. Existing refs to documents that were
// not moved keep their relative order, and an old target session without MRU simply
// gains the moved refs (the field is additive and optional).
func mergeExtractMru(existing, moved []session.MruFile) []session.MruFile {
	if len(moved) == 0 {
		return existing
	}
	removed := make(map[string]bool, len(moved))
	for _, ref := range moved {
		if ref.Path != "" {
			removed[ref.Path] = true
		}
	}
	out := append([]session.MruFile(nil), moved...)
	for _, ref := range existing {
		if ref.Path != "" && removed[ref.Path] {
			continue
		}
		out = append(out, ref)
	}
	return out
}

// acceptExtractTab writes the file state into the target workspace: a materialized
// target opens it in its live editor (so its own capture keeps it), a dormant one
// gets it merged into its saved session. The error means the target refused and
// nothing in the source was touched.
func (m *workspaceManager) acceptExtractTab(id, root, dir string, file session.FileState, moved []session.MruFile) error {
	m.mu.Lock()
	target := m.sessions[id]
	m.mu.Unlock()
	if target == nil {
		layout, ok, err := session.Load(dir)
		if err != nil {
			return fmt.Errorf("read the target workspace's session: %w", err)
		}
		if !ok {
			layout = session.Session{Version: session.Version, Root: root, Explorer: true}
		}
		replaced := false
		for i := range layout.Files {
			if layout.Files[i].Path == file.Path {
				layout.Files[i] = file
				replaced = true
				break
			}
		}
		if !replaced {
			layout.Files = append(layout.Files, file)
		}
		layout.MRU = mergeExtractMru(layout.MRU, moved)
		if len(moved) > 0 {
			layout.Active = activeForMru(layout.Files, layout.MRU)
		}
		if err := session.Save(dir, layout); err != nil {
			return fmt.Errorf("write the target workspace's session: %w", err)
		}
		_, _ = m.reg.UpdateLayout(id, &layout)
		return nil
	}
	var serr error
	if !m.runOnUI(func() {
		m.mu.Lock()
		target := m.sessions[id]
		m.mu.Unlock()
		if target == nil {
			serr = errors.New("the target workspace closed while extracting")
			return
		}
		failed := restore.RestoreSession(target.rt.ed, dir, session.Session{
			Version: session.Version,
			Root:    root,
			Files:   []session.FileState{file},
			MRU:     moved,
		})
		for _, u := range failed {
			if u.File.Path == file.Path {
				serr = fmt.Errorf("the target workspace could not open %s: %v", file.Path, u.Err)
				return
			}
		}
		m.enqueueCapture(target)
	}) {
		return errors.New("the daemon is not ready")
	}
	return serr
}

// ExtractTerminalTabTo captures the active dock terminal tab for an existing
// workspace. No PTY moves: the target receives the saved record and revives it on
// the first dive.
func (m *workspaceManager) ExtractTerminalTabTo(id string) (tui.WorkspaceCmd, error) {
	m.mu.Lock()
	sess := m.sessions[m.active]
	m.mu.Unlock()
	if sess == nil {
		return nil, errors.New("cannot extract: the daemon is not ready")
	}
	tab, ok := sess.rt.model.ActiveTerminalTab()
	if !ok {
		return nil, errors.New("cannot extract: there is no terminal tab")
	}
	ext := &extractTerminalTab{sourceID: sess.ws.File.StableID, destID: id, tab: tab}
	return tui.WorkspaceCmdOf(func() { m.runExtractTerminalTab(ext) }), nil
}

// runExtractTerminalTab moves the saved terminal tab into the target, then queues
// the switch and closes the source dock tab only after both were accepted.
func (m *workspaceManager) runExtractTerminalTab(ext *extractTerminalTab) {
	m.mu.Lock()
	sess := m.sessions[ext.sourceID]
	active := m.active
	m.mu.Unlock()
	if sess == nil || active != ext.sourceID {
		m.notify(errors.New("cannot extract: the active workspace changed while extracting; nothing was moved"))
		return
	}
	target, ok, err := m.reg.Get(ext.destID)
	if err != nil || !ok {
		m.notify(errors.New("cannot extract: the target workspace is gone; the terminal tab stays open"))
		return
	}
	sourceRoot, err := daemon.CanonicalRoot(sess.ws.File.Root)
	if err != nil {
		m.notify(fmt.Errorf("cannot extract: %w", err))
		return
	}
	targetRoot, err := daemon.CanonicalRoot(target.File.Root)
	if err != nil || targetRoot != sourceRoot {
		m.notify(errors.New("cannot extract: the target workspace is not over this root; the terminal tab stays open"))
		return
	}
	dir, err := m.reg.SessionDir(target.File.StableID)
	if err != nil {
		m.notify(fmt.Errorf("cannot extract: %w", err))
		return
	}
	if err := m.acceptExtractTerminalTab(target, dir, ext.tab); err != nil {
		m.notify(fmt.Errorf("cannot extract: %w; the terminal tab stays open", err))
		return
	}

	var wait func() error
	var serr error
	var closed bool
	if !m.runOnUI(func() {
		m.mu.Lock()
		cur := m.sessions[ext.sourceID]
		activeNow := m.active
		m.mu.Unlock()
		if activeNow != ext.sourceID || cur == nil {
			serr = errors.New("the active workspace changed while extracting")
			return
		}
		wait, serr = m.beginExtractSelect(target.File.StableID)
		if serr != nil {
			return
		}
		if !cur.rt.model.CloseActiveTerminalTabForExtract(ext.tab.Name) {
			return
		}
		closed = true
		m.enqueueCapture(cur)
	}) {
		m.notify(errors.New("cannot extract: the daemon is not ready; the terminal tab stays open"))
		return
	}
	if serr != nil {
		m.notify(fmt.Errorf("cannot extract: %w; the terminal tab stays open", serr))
		return
	}
	if !closed {
		if wait != nil {
			_ = wait()
		}
		m.report(appTextVars("gvim.workspace.report_terminal_tab_changed", "Extracted terminal tab %{name} into workspace %{label}; the source tab changed and is still open", map[string]any{"name": fmt.Sprintf("%q", terminalTabName(ext.tab)), "label": fmt.Sprintf("%q", target.File.Label)}))
		return
	}
	if wait != nil {
		if err := wait(); err != nil {
			m.notify(fmt.Errorf("cannot extract: open workspace %q: %v; the terminal state is kept there", target.File.Label, err))
			return
		}
	}
	m.report(appTextVars("gvim.workspace.report_terminal_tab_revives", "Extracted terminal tab %{name} into workspace %{label}; it revives on the first dive", map[string]any{"name": fmt.Sprintf("%q", terminalTabName(ext.tab)), "label": fmt.Sprintf("%q", target.File.Label)}))
}

// acceptExtractTerminalTab puts the saved terminal tab into the target: a
// materialized target appends a dormant restored tab, a dormant one merges it into
// its terminal record.
func (m *workspaceManager) acceptExtractTerminalTab(target daemon.Workspace, dir string, tab terminal.SavedTab) error {
	m.mu.Lock()
	open := m.sessions[target.File.StableID]
	m.mu.Unlock()
	if open != nil {
		var serr error
		if !m.runOnUI(func() {
			m.mu.Lock()
			open := m.sessions[target.File.StableID]
			m.mu.Unlock()
			if open == nil {
				serr = errors.New("the target workspace closed while extracting")
				return
			}
			if !open.rt.model.AppendRestoredTerminalTab(tab) {
				serr = errors.New("the target workspace refused the terminal record")
				return
			}
			m.enqueueCapture(open)
		}) {
			return errors.New("the daemon is not ready")
		}
		return serr
	}
	st, ok, err := restore.LoadTerminal(dir)
	if err != nil {
		return fmt.Errorf("read the target workspace's terminal record: %w", err)
	}
	if !ok {
		st = terminal.SavedState{Version: terminal.SavedStateVersion}
	}
	if st.Version == 0 {
		st.Version = terminal.SavedStateVersion
	}
	st.Tabs = append(st.Tabs, tab)
	return restore.SaveTerminal(dir, st)
}

func terminalTabName(tab terminal.SavedTab) string {
	if tab.Name != "" {
		return tab.Name
	}
	return "terminal"
}

// ExtractTabs extracts the selected open file tabs into one new co-tenant
// workspace. It is gvim's bounded multi-tab extension: Fresh has no multi-tab
// extract. Every selected tab must be a file-backed, non-large buffer; otherwise
// the whole action is refused and nothing moves.
func (m *workspaceManager) ExtractTabs(paths []string) (tui.WorkspaceCmd, error) {
	m.mu.Lock()
	sess := m.sessions[m.active]
	m.mu.Unlock()
	if sess == nil {
		return nil, errors.New("cannot extract: the daemon is not ready")
	}
	seen := make(map[string]bool, len(paths))
	exts := make([]*extractTab, 0, len(paths))
	ed := sess.rt.ed
	for _, path := range paths {
		if path == "" || seen[path] {
			continue
		}
		seen[path] = true
		doc := documentForPath(ed, path)
		if doc == nil {
			return nil, fmt.Errorf("cannot extract: %s is not open any more; no tab was moved", path)
		}
		ext, err := captureExtractTab(doc, sess.ws.File.StableID, sess.ws.File.Root)
		if err != nil {
			return nil, fmt.Errorf("cannot extract %s: %v; no tab was moved", path, err)
		}
		exts = append(exts, ext)
	}
	if len(exts) == 0 {
		return nil, errors.New("cannot extract: no file tabs were selected")
	}
	pathsSet := make(map[string]bool, len(exts))
	for _, ext := range exts {
		pathsSet[ext.path] = true
	}
	mru := extractMru(ed, pathsSet)
	return tui.WorkspaceCmdOf(func() { m.runExtractTabs(exts, mru) }), nil
}

// documentForPath returns the open document whose path is path.
func documentForPath(ed *editor.Editor, path string) *editor.Document {
	for i, tab := range ed.Tabs() {
		if tab.Path == path {
			return ed.DocAt(i)
		}
	}
	return nil
}

// runExtractTabs creates the co-tenant with all selected files, then queues the
// switch and closes every source tab only after it was accepted.
func (m *workspaceManager) runExtractTabs(exts []*extractTab, mru []session.MruFile) {
	m.mu.Lock()
	sess := m.sessions[exts[0].sourceID]
	active := m.active
	m.mu.Unlock()
	if sess == nil || active != exts[0].sourceID {
		m.notify(errors.New("cannot extract: the active workspace changed while extracting; nothing was moved"))
		return
	}
	canonical, err := daemon.CanonicalRoot(exts[0].destRoot)
	if err != nil {
		m.notify(fmt.Errorf("cannot extract: %w", err))
		return
	}
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
	layout := session.Session{Version: session.Version, Root: canonical, Explorer: true}
	for _, ext := range exts {
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
		layout.Files = append(layout.Files, file)
	}
	layout.MRU = mru
	layout.Active = activeForMru(layout.Files, mru)
	if err := session.Save(dir, layout); err != nil {
		discard()
		m.notify(fmt.Errorf("cannot extract: write the new workspace's session: %w", err))
		return
	}
	_, _ = m.reg.UpdateLayout(id, &layout)

	var wait func() error
	var serr error
	closed := 0
	if !m.runOnUI(func() {
		m.mu.Lock()
		cur := m.sessions[exts[0].sourceID]
		activeNow := m.active
		m.mu.Unlock()
		if activeNow != exts[0].sourceID || cur == nil {
			serr = errors.New("the active workspace changed while extracting")
			return
		}
		wait, serr = m.beginExtractSelect(id)
		if serr != nil {
			return
		}
		for _, ext := range exts {
			if cur.rt.model.CloseTabForWorkspaceExtractAt(ext.path) {
				closed++
			}
		}
		if closed > 0 {
			m.enqueueCapture(cur)
		}
	}) {
		discard()
		m.notify(errors.New("cannot extract: the daemon is not ready; no tab was moved"))
		return
	}
	if serr != nil {
		discard()
		m.notify(fmt.Errorf("cannot extract: %w; no tab was moved", serr))
		return
	}
	if wait != nil {
		if err := wait(); err != nil {
			m.notify(fmt.Errorf("cannot extract: open workspace %q: %v; the tabs' text is kept there", ws.File.Label, err))
			return
		}
	}
	noun := "tab"
	if closed != 1 {
		noun = "tabs"
	}
	m.report(appTextVars("gvim.workspace.report_count_noun", "Extracted %{count} %{noun} into workspace %{label}", map[string]any{"count": closed, "noun": noun, "label": fmt.Sprintf("%q", ws.File.Label)}))
}
