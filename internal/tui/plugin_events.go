package tui

import (
	"slices"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/buffer"
	"github.com/tbxark/gvim/editor"
	"github.com/tbxark/gvim/pluginapi"
)

// This file is the events side of the plugin host (FINAL section 6, X5). The root raises
// events from two kinds of place:
//
//   - Direct raise sites call plugEmit (or one of the helpers below) where the thing
//     happens: a save, a revert, a theme change, a command.
//   - The snapshot pair. pluginSnapshot records what the plugins listen to at the top of
//     Update, pluginTail diffs it against the state after the message was handled and
//     raises the events of the difference: tabs opened, closed and activated, language,
//     version and text changes, caret and selection, the window size. pluginViewTail does
//     the same for the viewport, which only settles at the end of Update.
//
// Nothing here runs Lua: events are queued for the runtime and the pump delivers them
// (plugin_pump.go). A hot event costs a Runtime.Wants call per Update and nothing else
// while no plugin subscribed to it, and a snapshot of nothing when no plugin listens.

// evWant says which of the events the snapshot derives some plugin listens to. Zero is
// the common case, and then a snapshot holds the window size only.
type evWant uint16

const (
	wantOpen evWant = 1 << iota
	wantClosed
	wantActivated
	wantDeactivated
	wantLanguage
	wantModified
	wantChanged
	wantCursor
	wantSelection
	wantViewport

	// wantList is every event that needs the tab list.
	wantList = wantOpen | wantClosed | wantActivated | wantDeactivated | wantLanguage | wantModified | wantChanged
)

// snapEvents maps the events of the snapshot diff to their bit.
var snapEvents = [...]struct {
	name string
	bit  evWant
}{
	{"after_file_open", wantOpen}, {"buffer_closed", wantClosed},
	{"buffer_activated", wantActivated}, {"buffer_deactivated", wantDeactivated},
	{"language_changed", wantLanguage}, {"buffer_modified", wantModified},
	{"buffer_changed", wantChanged}, {"cursor_moved", wantCursor},
	{"selection_changed", wantSelection}, {"viewport_changed", wantViewport},
}

// changesConsumer is the name under which the root reads the change records of a
// document: the "events" consumer of AMENDMENTS C13.
const changesConsumer = "events"

// maxChangedEdits is the number of change records one buffer_changed event carries; more
// is an overflow, which carries no edits.
const maxChangedEdits = 256

// pluginSnap is what pluginSnapshot saw before a message was handled, for pluginTail to
// diff against the state after it.
type pluginSnap struct {
	want   evWant
	w, h   int
	active pluginapi.BufferID
	bufs   []pluginapi.BufferInfo // tab order; only with wantList
	caret  snapCaret              // with wantCursor or wantSelection
	view   snapView               // with wantViewport
}

// snapCaret is the primary caret and selection of the active buffer.
type snapCaret struct {
	buf    pluginapi.BufferID
	pos    buffer.Pos // rune column, to compare
	api    pluginapi.Pos
	hasSel bool
	anchor buffer.Pos
	head   buffer.Pos
	sel    pluginapi.Range
}

// snapView is the viewport of the focused pane.
type snapView struct {
	ok              bool
	buf             pluginapi.BufferID
	top, rows, cols int
}

// plugWants reports whether a plugin listens to the event. It is the guard of every raise
// site that builds a payload.
func (m *Model) plugWants(name string) bool {
	return m.plug.rt != nil && m.plug.rt.Wants(name)
}

// plugWanted returns which snapshot events a plugin listens to.
func (m *Model) plugWanted() evWant {
	if m.plug.pe == nil {
		return 0
	}
	var w evWant
	for _, e := range snapEvents {
		if m.plug.rt.Wants(e.name) {
			w |= e.bit
		}
	}
	return w
}

// pluginSnapshot runs at the top of Update. It reads only what Runtime.Wants says a
// plugin listens to, so with no runtime it is a nil check and with no subscriber it
// records the window size.
func (m *Model) pluginSnapshot() pluginSnap {
	if m.plug.rt == nil {
		return pluginSnap{}
	}
	want := m.plugWanted()
	m.plugTrack(want&wantChanged != 0)
	return m.plugFill(want)
}

// plugTrack keeps the editor's change tracking on exactly while a plugin listens to
// buffer_changed. The decoration layer tracks under its own key.
func (m *Model) plugTrack(on bool) {
	p := &m.plug
	if p.tracking == on || p.pe == nil {
		return
	}
	p.tracking = on
	p.pe.TrackChanges(changesConsumer, on)
}

// plugFill reads the state the events in want are derived from.
func (m *Model) plugFill(want evWant) pluginSnap {
	s := pluginSnap{want: want, w: m.width, h: m.height}
	pe := m.plug.pe
	if want == 0 || pe == nil {
		return s
	}
	if want&wantList != 0 {
		s.bufs = pe.ListBuffers()
	}
	s.active = pe.ActiveBuffer()
	if want&(wantCursor|wantSelection) != 0 {
		s.caret = m.plugCaret(s.active)
	}
	if want&wantViewport != 0 {
		s.view = m.plugView(s.active)
	}
	return s
}

// plugCaret reads the caret and selection of the active document.
func (m *Model) plugCaret(id pluginapi.BufferID) snapCaret {
	doc := m.ed.Active()
	if doc == nil || id == 0 {
		return snapCaret{}
	}
	c := snapCaret{buf: id, pos: doc.Cursor()}
	c.api = apiPos(doc, c.pos)
	if a, h, ok := doc.Selection(); ok {
		c.hasSel, c.anchor, c.head = true, a, h
		from, to := apiPos(doc, a), apiPos(doc, h)
		if h.Less(a) {
			from, to = to, from
		}
		c.sel = pluginapi.Range{From: from, To: to}
	}
	return c
}

// plugView reads the viewport of the focused pane.
func (m *Model) plugView(id pluginapi.BufferID) snapView {
	doc := m.ed.Active()
	if doc == nil || id == 0 || m.width == 0 {
		return snapView{}
	}
	fp := m.layout().FocusedPane()
	return snapView{ok: true, buf: id, top: doc.Top(), rows: m.paneGeometry(fp, doc).BodyRows(fp.W, fp.H), cols: fp.TextW}
}

// apiPos converts a rune position of doc to an API position: the column is a byte offset
// into the line text.
func apiPos(doc Document, p buffer.Pos) pluginapi.Pos {
	if p.Col <= 0 || p.Line < 0 || p.Line >= doc.LineCount() {
		return pluginapi.Pos{Line: p.Line, Col: max(p.Col, 0)}
	}
	line := doc.Line(p.Line)
	i := 0
	for n := range line {
		if i == p.Col {
			return pluginapi.Pos{Line: p.Line, Col: n}
		}
		i++
	}
	return pluginapi.Pos{Line: p.Line, Col: len(line)}
}

// pluginTail runs in Update once the root has handled msg and reconciled the tabs. It
// turns the difference to before into events, hands them and the events that plugEmit
// queued to the runtime, and returns the pump's commands.
//
// A handler may edit, move the caret or open a file while the pump delivers the events.
// The difference is read once more after a pump that delivered something, so the plugins
// hear about it in the same Update. It is read once and not until it settles: a handler
// that reacts to every change by making one is slowed to one round per Update.
func (m *Model) pluginTail(before pluginSnap, msg tea.Msg) tea.Cmd {
	_ = msg
	p := &m.plug
	if p.rt == nil {
		return nil
	}
	cur := m.plugFill(before.want)
	m.plugDiff(&before, &cur)
	if len(p.events) == 0 {
		return nil
	}
	cmd := m.pumpAfter(func() {})
	if before.want == 0 {
		return cmd
	}
	next := m.plugFill(before.want)
	m.plugDiff(&cur, &next)
	if len(p.events) == 0 {
		return cmd
	}
	return tea.Batch(cmd, m.pumpAfter(func() {}))
}

// pluginViewTail runs at the end of Update, after the root scrolled the caret into view,
// and raises viewport_changed for the focused pane.
func (m *Model) pluginViewTail(before pluginSnap) tea.Cmd {
	p := &m.plug
	if p.rt == nil || before.want&wantViewport == 0 {
		return nil
	}
	cur := m.plugView(m.plug.pe.ActiveBuffer())
	b := before.view
	if !b.ok || !cur.ok || b.buf != cur.buf || b == cur {
		return nil
	}
	m.plugEmit("viewport_changed", map[string]any{
		"buf": cur.buf, "top_line": cur.top, "height": cur.rows, "width": cur.cols,
	})
	return m.pumpAfter(func() {})
}

// plugDiff queues the events of the difference between two snapshots. Lifecycle events
// come first, then text, then caret and selection (FINAL section 6).
func (m *Model) plugDiff(before, cur *pluginSnap) {
	if before.w != cur.w || before.h != cur.h {
		if before.w != 0 && before.h != 0 {
			m.plugEmit("resize", map[string]any{"width": cur.w, "height": cur.h})
		}
	}
	if before.want == 0 {
		return
	}
	if before.want&wantList != 0 {
		m.plugDiffTabs(before, cur)
		m.plugDiffText(before, cur)
	}
	m.plugDiffCaret(before, cur)
}

// plugDiffTabs raises the events of tabs that came, went or changed their language, and
// of the active tab changing.
func (m *Model) plugDiffTabs(before, cur *pluginSnap) {
	has := func(bufs []pluginapi.BufferInfo, id pluginapi.BufferID) bool {
		return slices.ContainsFunc(bufs, func(b pluginapi.BufferInfo) bool { return b.ID == id })
	}
	for _, b := range before.bufs {
		if !has(cur.bufs, b.ID) {
			m.plugEmit("buffer_closed", map[string]any{"id": b.ID, "path": b.Path})
			m.plugViewStateDrop(b.ID)
			m.plugLineTargetsDrop(pluginapi.BufferID(b.ID))
			m.plug.folds.dropBuffer(pluginapi.BufferID(b.ID))
		}
	}
	for _, b := range cur.bufs {
		for _, old := range before.bufs {
			if old.ID == b.ID && old.Path != b.Path && b.Path != "" {
				m.plugEmit("path_changed", map[string]any{"buf": b.ID, "path": b.Path, "old_path": old.Path})
				break
			}
		}
	}
	for _, b := range cur.bufs {
		if !has(before.bufs, b.ID) && b.Path != "" {
			m.plugEmit("after_file_open", map[string]any{"buf": b.ID, "path": b.Path})
		}
	}
	if before.active != cur.active {
		prev := before.active
		if !has(cur.bufs, prev) {
			prev = 0
		}
		if prev != 0 {
			d := map[string]any{"buf": prev}
			if cur.active != 0 {
				d["next"] = cur.active
			}
			m.plugEmit("buffer_deactivated", d)
		}
		if cur.active != 0 {
			d := map[string]any{"buf": cur.active}
			if prev != 0 {
				d["previous"] = prev
			}
			m.plugEmit("buffer_activated", d)
		}
	}
	if before.want&wantLanguage == 0 {
		return
	}
	for _, b := range cur.bufs {
		if old, ok := infoOf(before.bufs, b.ID); ok && old.Language != b.Language {
			m.plugEmit("language_changed", map[string]any{"buf": b.ID, "language": b.Language})
		}
	}
}

// infoOf finds the buffer id in bufs.
func infoOf(bufs []pluginapi.BufferInfo, id pluginapi.BufferID) (pluginapi.BufferInfo, bool) {
	for _, b := range bufs {
		if b.ID == id {
			return b, true
		}
	}
	return pluginapi.BufferInfo{}, false
}

// plugDiffText raises buffer_modified for the buffers whose version or modified flag
// changed, and buffer_changed with the change records of every buffer. The records of one
// Update arrive together, so the event carries them concatenated, in order.
func (m *Model) plugDiffText(before, cur *pluginSnap) {
	pe := m.plug.pe
	for _, b := range cur.bufs {
		if before.want&wantModified != 0 {
			if old, ok := infoOf(before.bufs, b.ID); ok && (old.Version != b.Version || old.Modified != b.Modified) {
				m.plugEmit("buffer_modified", map[string]any{"buf": b.ID, "version": b.Version, "modified": b.Modified})
			}
		}
		if before.want&wantChanged == 0 {
			continue
		}
		c := pe.TakeChanges(b.ID, changesConsumer)
		if len(c.Edits) == 0 && !c.Overflow {
			continue
		}
		d := map[string]any{"buf": b.ID, "version": c.Version, "undo": c.Undo, "overflow": c.Overflow}
		if len(c.Edits) > maxChangedEdits {
			d["overflow"] = true
		} else if !c.Overflow {
			d["edits"] = c.Edits
		}
		m.plugEmit("buffer_changed", d)
	}
}

// plugDiffCaret raises cursor_moved and selection_changed when the caret or selection of
// one buffer changed. A switch of the active tab is buffer_activated, not a cursor move.
func (m *Model) plugDiffCaret(before, cur *pluginSnap) {
	b, c := &before.caret, &cur.caret
	if b.buf == 0 || b.buf != c.buf {
		return
	}
	if before.want&wantCursor != 0 && b.pos != c.pos {
		m.plugEmit("cursor_moved", map[string]any{"buf": c.buf, "pos": c.api, "previous": b.api})
	}
	if before.want&wantSelection != 0 && (b.hasSel != c.hasSel || (c.hasSel && (b.anchor != c.anchor || b.head != c.head))) {
		d := map[string]any{"buf": c.buf}
		if c.hasSel {
			d["range"] = c.sel
		}
		m.plugEmit("selection_changed", d)
	}
}

// ---- Direct raise sites.

// plugSaved raises after_file_save for each file a save or auto-save wrote. The results
// are applied already, so a handler sees the document as saved.
func (m *Model) plugSaved(results []editor.SaveResult) {
	if !m.plugWants("after_file_save") {
		return
	}
	for _, r := range results {
		if path, id, ok := r.Written(); ok {
			m.plugEmit("after_file_save", map[string]any{"buf": pluginapi.BufferID(id), "path": path})
		}
	}
}

// plugReverted raises after_file_revert for the document of path, once the revert is
// applied.
func (m *Model) plugReverted(path string) {
	if pe := m.plug.pe; pe != nil && m.plugWants("after_file_revert") {
		if id := pe.FindBuffer(path); id != 0 {
			m.plugEmit("after_file_revert", map[string]any{"buf": id, "path": path})
		}
	}
}

// plugThemeChanged raises theme_changed after the theme was applied. The payload
// names the theme and says whether its editor background is dark.
func (m *Model) plugThemeChanged(name string, dark bool) {
	if m.plugWants("theme_changed") {
		m.plugEmit("theme_changed", map[string]any{"name": name, "dark": dark})
	}
}

// plugConfigChanged raises config_changed after a setting that plugins can read changed.
func (m *Model) plugConfigChanged() {
	if m.plugWants("config_changed") {
		m.plugEmit("config_changed", map[string]any{})
	}
}

// plugNoop is what plugCommand returns when there is nothing to raise afterwards.
func plugNoop() {}

// plugCommand raises pre_command for a command the user started and returns the function
// that raises post_command. A command a plugin runs through command.run does not come
// here: the macro recorder skips it the same way (C8), and a plugin that listens to
// commands would otherwise hear its own.
func (m *Model) plugCommand(action string, args map[string]any) func() {
	p := &m.plug
	if p.rt == nil || p.quiet > 0 {
		return plugNoop
	}
	pre, post := p.rt.Wants("pre_command"), p.rt.Wants("post_command")
	if pre {
		m.plugEmit("pre_command", map[string]any{"action": action, "args": args})
	}
	if !post {
		return plugNoop
	}
	p.cmdErr = nil
	return func() {
		d := map[string]any{"action": action, "args": args, "ok": p.cmdErr == nil}
		if p.cmdErr != nil {
			d["err"] = p.cmdErr.Error()
		}
		p.cmdErr = nil
		m.plugEmit("post_command", d)
	}
}

// plugFailed notes why the command now running failed, for post_command.
func (m *Model) plugFailed(err error) {
	if err != nil {
		m.plug.cmdErr = err
	}
}

// ---- before_file_save beyond the active document's Ctrl+S.

// saveGateBuffers returns the documents a Save All or an auto-save would write: modified,
// named, writable. A modified large document is left out when autosave is set, because
// PlanAutoSave never writes one (Save All does).
func (m *Model) saveGateBuffers(autosave bool) []pluginapi.BufferInfo {
	pe := m.plug.pe
	if pe == nil || !m.plugWants("before_file_save") {
		return nil
	}
	var out []pluginapi.BufferInfo
	for _, b := range pe.ListBuffers() {
		if b.Modified && b.Path != "" && !b.ReadOnly && !b.Virtual && (!autosave || !b.Large) {
			out = append(out, b)
		}
	}
	return out
}

// pluginSaveAllGate runs before_file_save for every document Save All would write, with
// source ("manual" for the command). One handler that cancels cancels the whole save:
// the editor plans all files together. The reason goes to the notice row.
func (m *Model) pluginSaveAllGate(source string) (cancel bool, cmd tea.Cmd) {
	var cmds []tea.Cmd
	for _, b := range m.saveGateBuffers(false) {
		c, reason, gate := m.pluginVeto(pluginapi.Event{Name: "before_file_save", Data: map[string]any{
			"buf": b.ID, "path": b.Path, "source": source,
		}})
		cmds = append(cmds, gate)
		if c {
			if reason == "" {
				reason = "Save cancelled by a plugin"
			}
			m.notice = reason
			return true, tea.Batch(cmds...)
		}
	}
	return false, tea.Batch(cmds...)
}

// pluginAutoSaveGate runs before_file_save with source "autosave" for every document the
// next auto-save would write. A cancel skips this round; the tick that follows asks again.
// It says nothing on the status row, because the round repeats.
func (m *Model) pluginAutoSaveGate() (cancel bool, cmd tea.Cmd) {
	var cmds []tea.Cmd
	for _, b := range m.saveGateBuffers(true) {
		c, _, gate := m.pluginVeto(pluginapi.Event{Name: "before_file_save", Data: map[string]any{
			"buf": b.ID, "path": b.Path, "source": "autosave",
		}})
		cmds = append(cmds, gate)
		if c {
			return true, tea.Batch(cmds...)
		}
	}
	return false, tea.Batch(cmds...)
}

// pluginSaveAsGate runs before_file_save for the active document before it is written to
// path, the target of Save As.
func (m *Model) pluginSaveAsGate(path string) (cancel bool, cmd tea.Cmd) {
	pe := m.plug.pe
	if pe == nil || !m.plugWants("before_file_save") {
		return false, nil
	}
	c, reason, gate := m.pluginVeto(pluginapi.Event{Name: "before_file_save", Data: map[string]any{
		"buf": pe.ActiveBuffer(), "path": path, "source": "manual",
	}})
	if c {
		if reason == "" {
			reason = "Save cancelled by a plugin"
		}
		m.notice = reason
	}
	return c, gate
}
