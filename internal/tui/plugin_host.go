package tui

import (
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/editor"
	"github.com/tbxark/gvim/pluginapi"
	"github.com/tbxark/gvim/theme"
)

// pluginHost is the editor as a plugin sees it: it implements pluginapi.Host over the
// root model and the optional PluginEditor port of the editor. The buffer operations
// delegate to the port, the registrations and overlays live in plugin_<area>.go, and
// whatever no stage has built yet answers CodeUnsupported through the embedded
// pluginapi.Unsupported.
//
// Every method runs on the update loop inside a Lua call. None of them may call the
// runtime (see plugin_pump.go): a method that could reach it validates and defers.
type pluginHost struct {
	pluginapi.Unsupported
	m *Model
	// shared, when set, is the daemon-wide host mux: m is nil and the active model is
	// looked up on every call, so one runtime's host calls route to whichever workspace
	// model is active (#19b shared-runtime slice).
	shared *PluginShared
}

// model is the model a host call routes to: the shared mux's active model, or the owning
// model of a direct launch.
func (h *pluginHost) model() *Model {
	if h.shared != nil {
		return h.shared.Active()
	}
	return h.m
}

var _ pluginapi.Host = (*pluginHost)(nil)

// ed returns the editor's plugin port (RootPorts.PluginEditor), or a port that refuses
// everything when there is none.
// Feature reports the host capabilities gvim.has may ask about (the optional host feature
// seam; see installDecor).
func (h *pluginHost) Feature(name string) bool {
	switch name {
	case "decor_virtual_paint":
		return true // the tier-1c paint surface (B1+B2)
	}
	return false
}

func (h *pluginHost) ed() pluginapi.BufferEditor {
	m := h.model()
	if m == nil {
		return pluginapi.Unsupported{}
	}
	pe := m.plug.pe
	if pe == nil {
		return pluginapi.Unsupported{}
	}
	if h.shared != nil {
		// The plugin API sees editor-global ids; the wrapper encodes/decodes around the
		// active workspace's local port (plugin_global_ids.go).
		return &globalPluginEditor{BufferEditor: pe, m: m}
	}
	return pe
}

// SetKeymap replaces the keymap the root resolves keys with. The app calls it when the
// plugin bindings change.
func (m *Model) SetKeymap(k Keymap) { m.keys = k }

// ---- pluginapi.BufferEditor: reads and edits go straight to the port.

func (h *pluginHost) ListBuffers() []pluginapi.BufferInfo { return h.ed().ListBuffers() }
func (h *pluginHost) Buffer(id pluginapi.BufferID) (pluginapi.BufferInfo, bool) {
	return h.ed().Buffer(id)
}
func (h *pluginHost) ActiveBuffer() pluginapi.BufferID          { return h.ed().ActiveBuffer() }
func (h *pluginHost) FindBuffer(path string) pluginapi.BufferID { return h.ed().FindBuffer(path) }
func (h *pluginHost) Lines(id pluginapi.BufferID, first, count int) ([]string, error) {
	return h.ed().Lines(id, first, count)
}
func (h *pluginHost) Slice(id pluginapi.BufferID, r pluginapi.Range) (string, error) {
	return h.ed().Slice(id, r)
}
func (h *pluginHost) ByteLength(id pluginapi.BufferID) (int, error) { return h.ed().ByteLength(id) }
func (h *pluginHost) OffsetOf(id pluginapi.BufferID, p pluginapi.Pos) (int, error) {
	return h.ed().OffsetOf(id, p)
}
func (h *pluginHost) PosOf(id pluginapi.BufferID, offset int) (pluginapi.Pos, error) {
	return h.ed().PosOf(id, offset)
}
func (h *pluginHost) Edit(id pluginapi.BufferID, edits []pluginapi.Edit, o pluginapi.EditOptions) error {
	return h.ed().Edit(id, edits, o)
}
func (h *pluginHost) UndoGroup(id pluginapi.BufferID, fn func() error) error {
	return h.ed().UndoGroup(id, fn)
}
func (h *pluginHost) Undo(id pluginapi.BufferID) error { return h.ed().Undo(id) }
func (h *pluginHost) Redo(id pluginapi.BufferID) error { return h.ed().Redo(id) }
func (h *pluginHost) Cursors(id pluginapi.BufferID) ([]pluginapi.Caret, error) {
	return h.ed().Cursors(id)
}
func (h *pluginHost) SetCursor(id pluginapi.BufferID, p pluginapi.Pos, extend, reveal, center bool) error {
	return h.ed().SetCursor(id, p, extend, reveal, center)
}
func (h *pluginHost) SetSelection(id pluginapi.BufferID, r pluginapi.Range) error {
	return h.ed().SetSelection(id, r)
}
func (h *pluginHost) Search(id pluginapi.BufferID, q pluginapi.SearchQuery) (pluginapi.SearchResult, error) {
	return h.ed().Search(id, q)
}
func (h *pluginHost) FindLines(id pluginapi.BufferID, q pluginapi.FindLinesQuery) (pluginapi.FindLinesResult, error) {
	return h.ed().FindLines(id, q)
}
func (h *pluginHost) FullText(id pluginapi.BufferID) (string, pluginapi.Version, error) {
	return h.ed().FullText(id)
}
func (h *pluginHost) SetLanguage(id pluginapi.BufferID, lang string) error {
	return h.ed().SetLanguage(id, lang)
}
func (h *pluginHost) SavedLines(id pluginapi.BufferID) ([]string, error) {
	return h.ed().SavedLines(id)
}
func (h *pluginHost) TrackChanges(key string, on bool) { h.ed().TrackChanges(key, on) }
func (h *pluginHost) TakeChanges(id pluginapi.BufferID, consumer string) pluginapi.Changes {
	return h.ed().TakeChanges(id, consumer)
}
func (h *pluginHost) SetReadOnly(id pluginapi.BufferID, ro bool) error {
	return h.ed().SetReadOnly(id, ro)
}
func (h *pluginHost) SetContent(id pluginapi.BufferID, entries []pluginapi.ContentEntry) error {
	return h.ed().SetContent(id, entries)
}
func (h *pluginHost) PropsAt(id pluginapi.BufferID, p pluginapi.Pos) (map[string]any, error) {
	return h.ed().PropsAt(id, p)
}
func (h *pluginHost) SetBufferMode(id pluginapi.BufferID, mode string) error {
	return h.ed().SetBufferMode(id, mode)
}
func (h *pluginHost) SetOption(id pluginapi.BufferID, name string, v any) error {
	return h.ed().SetOption(id, name, v)
}
func (h *pluginHost) GetOption(id pluginapi.BufferID, name string) (any, error) {
	return h.ed().GetOption(id, name)
}

// ---- Tab operations. They go through the port and then keep the root's own tab
// bookkeeping (the split leaves, the viewport) in step, as the actions do.

// tabIndex returns the tab index of buffer id, or -1.
func (h *pluginHost) tabIndex(id pluginapi.BufferID) int {
	for i, b := range h.ed().ListBuffers() {
		if b.ID == id {
			return i
		}
	}
	return -1
}

// Activate makes buffer id the active tab.
func (h *pluginHost) Activate(id pluginapi.BufferID) error {
	err := h.ed().Activate(id)
	h.model().syncViewport()
	return err
}

// Close closes the tab of buffer id. A tab that closes updates the split leaves and
// stops its saves, as close_tab does.
func (h *pluginHost) Close(id pluginapi.BufferID, force bool) error {
	m := h.model()
	i := h.tabIndex(id)
	path := m.tabPath(i)
	before := len(m.ed.Tabs())
	err := h.ed().Close(id, force)
	if i >= 0 && len(m.ed.Tabs()) == before-1 {
		m.cancelSavesOf(path)
		m.tabRemoved(i)
	}
	m.syncViewport()
	return err
}

// MoveTab moves the tab of buffer id by delta places. The split leaves name tabs by
// index, so they follow the move.
func (h *pluginHost) MoveTab(id pluginapi.BufferID, delta int) error {
	from := h.tabIndex(id)
	if err := h.ed().MoveTab(id, delta); err != nil {
		return err
	}
	if to := h.tabIndex(id); from >= 0 && to >= 0 && to != from {
		h.model().tabMoved(from, to)
	}
	return nil
}

// tabMoved updates the leaves after the tab at index from moved to index to. The
// focused leaf takes its tab from the editor, so it is left alone.
func (m *Model) tabMoved(from, to int) {
	for _, leaf := range m.splits.Root.Leaves() {
		if leaf != m.splits.Focus {
			leaf.Tab = movedIndex(leaf.Tab, from, to)
		}
	}
	m.splits.seenTab = movedIndex(m.splits.seenTab, from, to)
	m.splits.prevTab = movedIndex(m.splits.prevTab, from, to)
}

// movedIndex returns where tab i is after the tab at from moved to to. A negative i is
// "no tab" and stays.
func movedIndex(i, from, to int) int {
	switch {
	case i < 0:
		return i
	case i == from:
		return to
	case from < to && i > from && i <= to:
		return i - 1
	case to < from && i >= to && i < from:
		return i + 1
	}
	return i
}

// ---- pluginapi.BufferHost: the async buffer calls.

// pluginOpenedMsg is a large file that Host.Open started, read in its command. inner is
// the message that command produced, which the root applies as it would for any open.
type pluginOpenedMsg struct {
	t     pluginapi.Token
	req   pluginapi.OpenRequest
	prev  pluginapi.BufferID
	inner tea.Msg
}

// pluginSavedMsg is the write of a save that Host.Save started.
type pluginSavedMsg struct {
	t     pluginapi.Token
	inner tea.Msg
}

// pluginRevertedMsg is the read of a revert that Host.Revert started.
type pluginRevertedMsg struct {
	t     pluginapi.Token
	inner tea.Msg
}

// Open opens a file. It validates and defers; the open happens after the slice, and the
// result is the BufferID.
func (h *pluginHost) Open(t pluginapi.Token, r pluginapi.OpenRequest) error {
	m := h.model()
	if r.Path == "" {
		return pluginapi.Errf(pluginapi.CodeInvalid, "open: empty path")
	}
	if _, ok := m.ed.(fileEditor); !ok {
		return pluginapi.Errf(pluginapi.CodeUnsupported, "opening files is not supported in this port")
	}
	if err := m.plugWait(t, "open"); err != nil {
		return err
	}
	m.plug.later = append(m.plug.later, func() tea.Cmd { return m.openDeferred(t, r) })
	return nil
}

// openDeferred opens the file of Host.Open. A file the editor opens at once is resolved
// now. A large file is read in a command, and the message that comes back is applied
// before the wait is resolved.
func (m *Model) openDeferred(t pluginapi.Token, r pluginapi.OpenRequest) tea.Cmd {
	prev := m.plug.host.ed().ActiveBuffer()
	cmd, err := m.openFileCmdErr(r.Path, r.Preview, 0, 0)
	if cmd == nil {
		return m.openFinished(t, r, prev, err)
	}
	return func() tea.Msg { return pluginOpenedMsg{t: t, req: r, prev: prev, inner: cmd()} }
}

// pluginOpened applies the large file that Host.Open read, and resolves the wait.
func (m *Model) pluginOpened(msg pluginOpenedMsg) tea.Cmd {
	_, cmd := m.update(msg.inner)
	var err error
	if loaded, ok := msg.inner.(openLoadedMsg); ok {
		err = loaded.err
	}
	return tea.Batch(cmd, m.openFinished(msg.t, msg.req, msg.prev, err))
}

// openFinished resolves the wait of Host.Open: with the BufferID of the file, placed at
// the position the request gave, or with openErr, the reason the file did not open.
func (m *Model) openFinished(t pluginapi.Token, r pluginapi.OpenRequest, prev pluginapi.BufferID, openErr error) tea.Cmd {
	pe := m.plug.host.ed()
	id := pe.FindBuffer(r.Path)
	if id == 0 {
		reason := "cannot open " + r.Path
		if openErr != nil {
			reason = openErr.Error()
		}
		return m.plugFinish(pluginapi.Completion{Token: t, Err: string(pluginapi.CodeIO) + ": " + reason})
	}
	if r.Pos != nil {
		_ = pe.SetCursor(id, *r.Pos, false, true, false)
	}
	if r.Background && prev != 0 && prev != id {
		_ = pe.Activate(prev)
	} else if r.Focus {
		m.focusEditor()
	}
	m.syncViewport()
	return m.plugFinish(pluginapi.Completion{Token: t, Values: []any{id}})
}

// Save saves buffer id, to path when it is not empty. It validates and defers, so
// Model.save and the before_file_save veto run with no Lua on the stack.
func (h *pluginHost) Save(t pluginapi.Token, id pluginapi.BufferID, path string) error {
	m := h.model()
	info, ok := h.ed().Buffer(id)
	if !ok {
		return pluginapi.Errf(pluginapi.CodeClosed, "buffer %d is closed", id)
	}
	if info.Virtual {
		return pluginapi.Errf(pluginapi.CodeInvalid, "a virtual buffer cannot be saved")
	}
	if path == "" && info.Path == "" {
		return pluginapi.Errf(pluginapi.CodeInvalid, "buffer has no file name; give a path")
	}
	if err := m.plugWait(t, "save"); err != nil {
		return err
	}
	m.plug.later = append(m.plug.later, func() tea.Cmd { return m.saveDeferred(t, id, path) })
	return nil
}

// withBuffer makes buffer id the active tab, runs fn, and then gives the tab back. The
// save and revert plans are made against the active tab, and they carry what they need,
// so the user's tab is not changed for longer than the planning. fn's error stops it.
func (m *Model) withBuffer(id pluginapi.BufferID, fn func()) error {
	pe := m.plug.host.ed()
	prev := pe.ActiveBuffer()
	if prev != id {
		if err := pe.Activate(id); err != nil {
			return err
		}
		m.syncViewport()
	}
	fn()
	if prev != id && prev != 0 {
		_ = pe.Activate(prev)
		m.syncViewport()
	}
	return nil
}

// saveDeferred starts the save of Host.Save: the veto gate first, then the save of the
// buffer. A save that ends at once (a veto, a refusal) resolves the wait now; otherwise
// the write's message resolves it. The gate's commands go out with whichever it is.
func (m *Model) saveDeferred(t pluginapi.Token, id pluginapi.BufferID, path string) tea.Cmd {
	var cmd, gate tea.Cmd
	var cancelled bool
	var reason string
	var planErr error
	err := m.withBuffer(id, func() {
		if cancelled, reason, gate = m.pluginSaveGate("api"); cancelled {
			return
		}
		if path != "" {
			cmd, planErr = m.saveAsErr(path, false, flow{})
		} else {
			cmd, planErr = m.saveThenErr(flow{})
		}
	})
	switch {
	case err != nil:
		return tea.Batch(gate, m.plugFinish(pluginapi.Completion{Token: t, Err: err.Error()}))
	case cancelled:
		return tea.Batch(gate, m.plugFinish(pluginapi.Completion{Token: t, Err: string(pluginapi.CodeCancelled) + ": " + reason}))
	case planErr != nil:
		return tea.Batch(gate, m.plugFinish(refusal(t, "save", planErr)))
	case cmd == nil:
		return tea.Batch(gate, m.plugFinish(pluginapi.Completion{Token: t, Values: []any{true}}))
	}
	return tea.Batch(gate, func() tea.Msg { return pluginSavedMsg{t: t, inner: cmd()} })
}

// refusal is the completion of an operation that the editor refused: its error, with the
// code a plugin sees for it. It does not depend on what the status row showed before.
func refusal(t pluginapi.Token, action string, err error) pluginapi.Completion {
	return pluginapi.Completion{Token: t, Err: plugActionErr(action, err).Error()}
}

// pluginSaved applies the write that Host.Save started, and resolves the wait with its
// outcome.
func (m *Model) pluginSaved(msg pluginSavedMsg) tea.Cmd {
	_, cmd := m.update(msg.inner)
	c := pluginapi.Completion{Token: msg.t, Values: []any{true}}
	if done, ok := msg.inner.(saveDoneMsg); ok {
		for _, r := range done.results {
			if err := r.Err(); err != nil {
				c = pluginapi.Completion{Token: msg.t, Err: plugActionErr("save", err).Error()}
				break
			}
		}
	}
	return tea.Batch(cmd, m.plugFinish(c))
}

// Revert reloads buffer id from its file, discarding its changes. It validates and
// defers.
func (h *pluginHost) Revert(t pluginapi.Token, id pluginapi.BufferID) error {
	m := h.model()
	info, ok := h.ed().Buffer(id)
	if !ok {
		return pluginapi.Errf(pluginapi.CodeClosed, "buffer %d is closed", id)
	}
	if info.Path == "" {
		return pluginapi.Errf(pluginapi.CodeInvalid, "%v", editor.ErrNoPath)
	}
	if err := m.plugWait(t, "revert"); err != nil {
		return err
	}
	m.plug.later = append(m.plug.later, func() tea.Cmd { return m.revertDeferred(t, id) })
	return nil
}

// revertDeferred starts the revert of Host.Revert.
func (m *Model) revertDeferred(t pluginapi.Token, id pluginapi.BufferID) tea.Cmd {
	var cmd tea.Cmd
	var planErr error
	err := m.withBuffer(id, func() { cmd, planErr = m.startRevertErr(true) })
	switch {
	case err != nil:
		return m.plugFinish(pluginapi.Completion{Token: t, Err: err.Error()})
	case planErr != nil:
		return m.plugFinish(refusal(t, "revert", planErr))
	case cmd == nil:
		return m.plugFinish(pluginapi.Completion{Token: t, Values: []any{true}})
	}
	return func() tea.Msg { return pluginRevertedMsg{t: t, inner: cmd()} }
}

// pluginReverted applies the file that Host.Revert read, and resolves the wait.
func (m *Model) pluginReverted(msg pluginRevertedMsg) tea.Cmd {
	c := pluginapi.Completion{Token: msg.t, Values: []any{true}}
	var cmd tea.Cmd
	if done, ok := msg.inner.(revertDoneMsg); ok {
		if err := m.revertDoneErr(done); err != nil {
			c = refusal(msg.t, "revert", err)
		}
	} else {
		_, cmd = m.update(msg.inner)
	}
	return tea.Batch(cmd, m.plugFinish(c))
}

// CancelWait forgets the wait t: its result is dropped when it arrives.
func (h *pluginHost) CancelWait(t pluginapi.Token) { h.model().plugCancelWait(t) }

// ---- pluginapi.UIHost: what the root owns.

// Notify shows msg on the status row. A warning or an error is a notice, which the
// warnings log keeps; an info message is shown and not kept. Several notices can arrive in
// one update (the loader reports every plugin that failed to load): the loader's are joined
// on the status row, a plugin's own replaces it, and each is kept in the log as it comes.
func (h *pluginHost) Notify(plugin string, level pluginapi.Level, msg string) {
	m := h.model()
	msg = cleanText(msg)
	switch {
	case msg == "":
		return
	case level == pluginapi.LevelInfo:
		m.quietNotice(msg)
		return
	case plugin != "":
		// A plugin's own notice replaces the status row, with its name in front.
		m.plug.ui.notices = nil
		m.notice = msg
		// The runtime's own messages already name the plugin ("rterr: boom", "wc suspended ...").
		if !strings.HasPrefix(msg, plugin+": ") && !strings.HasPrefix(msg, plugin+" ") {
			m.notice = plugin + ": " + msg
		}
		m.notes.keep(m.notice)
		return
	}
	// A notice of the loader. The status row shows one notice, so the loader notices of one
	// update (the plugins that failed to load report together) are joined; each one stays
	// in the warnings log.
	ui := &m.plug.ui
	if ui.noticeAt != ui.updates || m.notice != strings.Join(ui.notices, "; ") {
		ui.notices = ui.notices[:0]
	}
	ui.noticeAt = ui.updates
	ui.notices = append(ui.notices, msg)
	m.notice = strings.Join(ui.notices, "; ")
	m.notes.keep(msg)
	if len(ui.notices) > 1 {
		m.notes.quiet = m.notice // the joined text is not a warning of its own
	}
}

// SetClipboard puts text on the clipboard the way a copy does. Calls in one slice are
// coalesced: only the last text is written, once, by the pump (S13-3).
func (h *pluginHost) SetClipboard(text string) {
	m := h.model()
	ui := &m.plug.ui
	ui.clip = text
	if ui.clipQueued {
		return
	}
	ui.clipQueued = true
	m.plug.later = append(m.plug.later, func() tea.Cmd {
		text := ui.clip
		ui.clip, ui.clipQueued = "", false
		return m.clipboardOut("copy", editor.Result{Text: text})
	})
}

// Focus gives the keyboard to the editor or the explorer.
func (h *pluginHost) Focus(area string) error {
	m := h.model()
	switch area {
	case "editor":
		m.focusEditor()
	case "explorer":
		if m.tree == nil {
			return pluginapi.Errf(pluginapi.CodeUnsupported, "there is no explorer")
		}
		m.focusExplorer()
	default:
		return pluginapi.Errf(pluginapi.CodeInvalid, "unknown area %q", area)
	}
	return nil
}

// ScreenSize is the size of the frame in cells.
func (h *pluginHost) ScreenSize() (w, ht int) { return h.model().width, h.model().height }

// OwnerGone removes what plugin registered in the root. Each area drops its own state:
// the commands here, the others in plugin_<area>.go. The Host calls carry no plugin, so
// a wait cannot be matched to its owner here; the runtime drops the completions of a
// dead generation instead.
func (h *pluginHost) OwnerGone(plugin string, gen uint64) {
	m := h.model()
	m.dropCommands(plugin)
	m.plugKeysGone(plugin, gen)
	m.plugPanelsGone(plugin, gen)
	m.plugMenusGone(plugin, gen)
	m.plugGroupsGone(plugin, gen)
	m.plugPromptsGone(plugin, gen)
	m.plugStatusGone(plugin, gen)
	m.plugDecorGone(plugin, gen)
	m.plugWinGone(plugin, gen)
	m.plugVirtualGone(plugin, gen)
	m.plugViewStateDropOwner(plugin)
	m.plugLineTargetsDropOwner(plugin)
	m.plug.explorer.dropOwner(plugin)
}

// ---- pluginapi.WorkspaceHost.

// Root is the workspace root, or "" when there is no explorer.
func (h *pluginHost) Root() string { return h.model().rootDir() }

// Files is the file index snapshot, at most limit entries (all when limit <= 0).
func (h *pluginHost) Files(limit int) (files []string, loading bool) {
	if h.model().files == nil {
		return nil, false
	}
	files, loading = h.model().files.Snapshot()
	if limit > 0 && len(files) > limit {
		files = files[:limit]
	}
	return files, loading
}

// Config returns one of the effective settings a plugin may read, by its config name.
func (h *pluginHost) Config(key string) (any, bool) {
	m := h.model()
	s := m.util.settings
	switch key {
	case "theme":
		return s.Theme, true
	case "keymap":
		return s.Keymap, true
	case "tab_size":
		return s.TabSize, true
	case "insert_spaces":
		return s.InsertSpaces, true
	case "auto_indent":
		return s.AutoIndent, true
	case "line_numbers":
		return !m.hideLineNumbers, true
	case "menu_bar":
		return !m.hideMenuBar, true
	case "line_ending":
		if s.LineEnding == "" {
			return "auto", true
		}
		return s.LineEnding, true
	case "trim_trailing_whitespace":
		return s.TrimTrailingWhitespace, true
	case "ensure_final_newline":
		return s.EnsureFinalNewline, true
	case "line_wrap":
		return m.view.lineWrap, true
	case "highlight_current_line":
		return m.view.currentLine, true
	case "show_whitespace":
		return m.view.whitespace, true
	case "cursor_style":
		return m.view.cursor, true
	case "auto_save_enabled":
		return m.root.ports.AutoSave > 0, true
	case "auto_save_interval_secs":
		return int(m.root.ports.AutoSave / time.Second), true
	}
	return nil, false
}

// Theme names the active theme and says whether its editor background is dark.
func (h *pluginHost) Theme() pluginapi.ThemeInfo {
	th := h.model().effectiveTheme()
	return pluginapi.ThemeInfo{Name: th.Name, Dark: th.Dark()}
}

// ThemeColor returns the RGB of the theme key "section.key". A named color
// answers with Fresh's fixed RGB for it; the terminal default has no RGB.
func (h *pluginHost) ThemeColor(key string) (r, g, b uint8, ok bool) {
	section, name, found := strings.Cut(key, ".")
	if !found {
		return 0, 0, 0, false
	}
	c, ok := h.model().effectiveTheme().Value(section, name)
	if !ok {
		return 0, 0, 0, false
	}
	rgb, ok := c.RGBA()
	if !ok {
		return 0, 0, 0, false
	}
	return rgb.R, rgb.G, rgb.B, true
}

// ThemeNames lists the themes built into the editor.
func (h *pluginHost) ThemeNames() []string { return theme.Names() }

// ThemeOverrides returns the session color overrides and whether any are set.
func (h *pluginHost) ThemeOverrides() (map[string]string, bool) {
	over := h.model().ThemeOverrides()
	return over, len(over) > 0
}

// SetThemeOverrides replaces the session color overrides. An empty map clears
// them. A bad key or value is an error and keeps the current look.
func (h *pluginHost) SetThemeOverrides(overrides map[string]string) error {
	return h.model().SetThemeOverrides(overrides)
}
