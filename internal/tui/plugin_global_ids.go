package tui

// Editor-global plugin ids (R19). A daemon plugin runtime is shared by every local
// workspace, but each workspace model has its own split-tree window ids and its own editor
// adapter buffer ids, so the same number can name different things in two workspaces. The
// plugin API therefore exposes packed ids: the workspace's sequence in the high bits and
// the workspace-local id in the low pluginLocalBits bits. Direct launches are sequence 0
// and keep their local ids byte-identically. A host call decodes the sequence: it must name
// the ACTIVE workspace model, otherwise the id is answered with CodeClosed ("belongs to
// another workspace") and never silently resolves to a colliding local id in the active
// workspace. Local ids are capped at 2^40 (documented); editors allocate small numbers.

import (
	"github.com/tbxark/gvim/pluginapi"
)

const (
	// pluginWorkspaceShift is the number of low bits kept for a workspace-local id.
	pluginWorkspaceShift = 40
	// pluginLocalMask selects the workspace-local half of a global id.
	pluginLocalMask = uint64(1)<<pluginWorkspaceShift - 1
)

// pluginWorkspaceSeq is the model's sequence in the shared namespace, or 0 for a direct
// launch (no shared host).
func (m *Model) pluginWorkspaceSeq() uint64 {
	s := m.pluginShared()
	if s == nil {
		return 0
	}
	return s.workspaceSeq(m)
}

// pluginShared returns the daemon-wide host mux of the model, or nil.
func (m *Model) pluginShared() *PluginShared {
	if m == nil {
		return nil
	}
	return m.root.ports.PluginShared
}

// pluginGlobalBuffer maps a workspace-local buffer id to the plugin API's global id.
func (m *Model) pluginGlobalBuffer(id pluginapi.BufferID) pluginapi.BufferID {
	if id == 0 {
		return 0
	}
	seq := m.pluginWorkspaceSeq()
	if seq == 0 {
		return id
	}
	return pluginapi.BufferID(seq<<pluginWorkspaceShift | uint64(id)&pluginLocalMask)
}

// pluginGlobalWindow maps a workspace-local split-window id to the global id.
func (m *Model) pluginGlobalWindow(id uint64) pluginapi.WinID {
	if id == 0 {
		return 0
	}
	seq := m.pluginWorkspaceSeq()
	if seq == 0 {
		return pluginapi.WinID(id)
	}
	return pluginapi.WinID(seq<<pluginWorkspaceShift | id&pluginLocalMask)
}

// pluginResolveBuffer decodes a global buffer id against the ACTIVE model: the sequence
// must be this model's, otherwise the id belongs to another (possibly closed) workspace and
// is refused with CodeClosed. A local id that closed is left for the editor port to refuse.
func (m *Model) pluginResolveBuffer(global pluginapi.BufferID) (pluginapi.BufferID, error) {
	if global == 0 {
		return 0, pluginapi.Errf(pluginapi.CodeClosed, "buffer 0 is closed")
	}
	seq := m.pluginWorkspaceSeq()
	if seq == 0 {
		return global, nil
	}
	got := uint64(global) >> pluginWorkspaceShift
	if got != seq {
		return 0, pluginapi.Errf(pluginapi.CodeClosed, "buffer %d belongs to another workspace or a closed workspace", global)
	}
	local := pluginapi.BufferID(uint64(global) & pluginLocalMask)
	if local == 0 {
		return 0, pluginapi.Errf(pluginapi.CodeClosed, "buffer %d is closed", global)
	}
	return local, nil
}

// pluginResolveWindow decodes a global window id against the ACTIVE model.
func (m *Model) pluginResolveWindow(global pluginapi.WinID) (uint64, error) {
	seq := m.pluginWorkspaceSeq()
	if seq == 0 {
		return uint64(global), nil
	}
	got := uint64(global) >> pluginWorkspaceShift
	if got != seq {
		return 0, pluginapi.Errf(pluginapi.CodeClosed, "window %d belongs to another workspace or a closed workspace", global)
	}
	local := uint64(global) & pluginLocalMask
	return local, nil
}

// globalWindow maps a local split id to the id the plugin API returns.
func (h *pluginHost) globalWindow(local uint64) pluginapi.WinID {
	if m := h.model(); m != nil {
		return m.pluginGlobalWindow(local)
	}
	return pluginapi.WinID(local)
}

// globalPluginEditor is the shared-mode wrapper around the active workspace's editor port.
// The host uses it transparently, so every BufferID crossing the plugin API boundary is
// decoded on input and encoded on output while the root's own bookkeeping stays local.
type globalPluginEditor struct {
	pluginapi.BufferEditor
	m *Model
}

func (g *globalPluginEditor) out(id pluginapi.BufferID) pluginapi.BufferID {
	return g.m.pluginGlobalBuffer(id)
}

func (g *globalPluginEditor) in(id pluginapi.BufferID) (pluginapi.BufferID, error) {
	return g.m.pluginResolveBuffer(id)
}

func (g *globalPluginEditor) ListBuffers() []pluginapi.BufferInfo {
	src := g.BufferEditor.ListBuffers()
	bufs := make([]pluginapi.BufferInfo, len(src))
	for i, b := range src {
		b.ID = g.out(b.ID)
		bufs[i] = b
	}
	return bufs
}

func (g *globalPluginEditor) Buffer(id pluginapi.BufferID) (pluginapi.BufferInfo, bool) {
	local, err := g.in(id)
	if err != nil {
		return pluginapi.BufferInfo{}, false
	}
	info, ok := g.BufferEditor.Buffer(local)
	if ok {
		info.ID = g.out(info.ID)
	}
	return info, ok
}

func (g *globalPluginEditor) ActiveBuffer() pluginapi.BufferID {
	return g.out(g.BufferEditor.ActiveBuffer())
}

func (g *globalPluginEditor) FindBuffer(path string) pluginapi.BufferID {
	return g.out(g.BufferEditor.FindBuffer(path))
}

func (g *globalPluginEditor) Lines(id pluginapi.BufferID, first, count int) ([]string, error) {
	local, err := g.in(id)
	if err != nil {
		return nil, err
	}
	return g.BufferEditor.Lines(local, first, count)
}

func (g *globalPluginEditor) Slice(id pluginapi.BufferID, r pluginapi.Range) (string, error) {
	local, err := g.in(id)
	if err != nil {
		return "", err
	}
	return g.BufferEditor.Slice(local, r)
}

func (g *globalPluginEditor) ByteLength(id pluginapi.BufferID) (int, error) {
	local, err := g.in(id)
	if err != nil {
		return 0, err
	}
	return g.BufferEditor.ByteLength(local)
}

func (g *globalPluginEditor) OffsetOf(id pluginapi.BufferID, p pluginapi.Pos) (int, error) {
	local, err := g.in(id)
	if err != nil {
		return 0, err
	}
	return g.BufferEditor.OffsetOf(local, p)
}

func (g *globalPluginEditor) PosOf(id pluginapi.BufferID, offset int) (pluginapi.Pos, error) {
	local, err := g.in(id)
	if err != nil {
		return pluginapi.Pos{}, err
	}
	return g.BufferEditor.PosOf(local, offset)
}

func (g *globalPluginEditor) Edit(id pluginapi.BufferID, edits []pluginapi.Edit, o pluginapi.EditOptions) error {
	local, err := g.in(id)
	if err != nil {
		return err
	}
	return g.BufferEditor.Edit(local, edits, o)
}

func (g *globalPluginEditor) UndoGroup(id pluginapi.BufferID, fn func() error) error {
	local, err := g.in(id)
	if err != nil {
		return err
	}
	return g.BufferEditor.UndoGroup(local, fn)
}

func (g *globalPluginEditor) Undo(id pluginapi.BufferID) error {
	local, err := g.in(id)
	if err != nil {
		return err
	}
	return g.BufferEditor.Undo(local)
}

func (g *globalPluginEditor) Redo(id pluginapi.BufferID) error {
	local, err := g.in(id)
	if err != nil {
		return err
	}
	return g.BufferEditor.Redo(local)
}

func (g *globalPluginEditor) Cursors(id pluginapi.BufferID) ([]pluginapi.Caret, error) {
	local, err := g.in(id)
	if err != nil {
		return nil, err
	}
	return g.BufferEditor.Cursors(local)
}

func (g *globalPluginEditor) SetCursor(id pluginapi.BufferID, p pluginapi.Pos, extend, reveal, center bool) error {
	local, err := g.in(id)
	if err != nil {
		return err
	}
	return g.BufferEditor.SetCursor(local, p, extend, reveal, center)
}

func (g *globalPluginEditor) SetSelection(id pluginapi.BufferID, r pluginapi.Range) error {
	local, err := g.in(id)
	if err != nil {
		return err
	}
	return g.BufferEditor.SetSelection(local, r)
}

func (g *globalPluginEditor) Search(id pluginapi.BufferID, q pluginapi.SearchQuery) (pluginapi.SearchResult, error) {
	local, err := g.in(id)
	if err != nil {
		return pluginapi.SearchResult{}, err
	}
	return g.BufferEditor.Search(local, q)
}

func (g *globalPluginEditor) FindLines(id pluginapi.BufferID, q pluginapi.FindLinesQuery) (pluginapi.FindLinesResult, error) {
	local, err := g.in(id)
	if err != nil {
		return pluginapi.FindLinesResult{}, err
	}
	return g.BufferEditor.FindLines(local, q)
}

func (g *globalPluginEditor) FullText(id pluginapi.BufferID) (string, pluginapi.Version, error) {
	local, err := g.in(id)
	if err != nil {
		return "", pluginapi.Version{}, err
	}
	return g.BufferEditor.FullText(local)
}

func (g *globalPluginEditor) SetLanguage(id pluginapi.BufferID, lang string) error {
	local, err := g.in(id)
	if err != nil {
		return err
	}
	return g.BufferEditor.SetLanguage(local, lang)
}

func (g *globalPluginEditor) SavedLines(id pluginapi.BufferID) ([]string, error) {
	local, err := g.in(id)
	if err != nil {
		return nil, err
	}
	return g.BufferEditor.SavedLines(local)
}

func (g *globalPluginEditor) Activate(id pluginapi.BufferID) error {
	local, err := g.in(id)
	if err != nil {
		return err
	}
	return g.BufferEditor.Activate(local)
}

func (g *globalPluginEditor) Close(id pluginapi.BufferID, force bool) error {
	local, err := g.in(id)
	if err != nil {
		return err
	}
	return g.BufferEditor.Close(local, force)
}

func (g *globalPluginEditor) MoveTab(id pluginapi.BufferID, delta int) error {
	local, err := g.in(id)
	if err != nil {
		return err
	}
	return g.BufferEditor.MoveTab(local, delta)
}

func (g *globalPluginEditor) TakeChanges(id pluginapi.BufferID, consumer string) pluginapi.Changes {
	local, err := g.in(id)
	if err != nil {
		return pluginapi.Changes{}
	}
	return g.BufferEditor.TakeChanges(local, consumer)
}

func (g *globalPluginEditor) CreateBuffer(o pluginapi.CreateBuffer) (pluginapi.BufferID, error) {
	id, err := g.BufferEditor.CreateBuffer(o)
	return g.out(id), err
}

func (g *globalPluginEditor) SetReadOnly(id pluginapi.BufferID, ro bool) error {
	local, err := g.in(id)
	if err != nil {
		return err
	}
	return g.BufferEditor.SetReadOnly(local, ro)
}

func (g *globalPluginEditor) SetContent(id pluginapi.BufferID, entries []pluginapi.ContentEntry) error {
	local, err := g.in(id)
	if err != nil {
		return err
	}
	return g.BufferEditor.SetContent(local, entries)
}

func (g *globalPluginEditor) PropsAt(id pluginapi.BufferID, p pluginapi.Pos) (map[string]any, error) {
	local, err := g.in(id)
	if err != nil {
		return nil, err
	}
	return g.BufferEditor.PropsAt(local, p)
}

func (g *globalPluginEditor) SetBufferMode(id pluginapi.BufferID, mode string) error {
	local, err := g.in(id)
	if err != nil {
		return err
	}
	return g.BufferEditor.SetBufferMode(local, mode)
}

func (g *globalPluginEditor) SetOption(id pluginapi.BufferID, name string, v any) error {
	local, err := g.in(id)
	if err != nil {
		return err
	}
	return g.BufferEditor.SetOption(local, name, v)
}

func (g *globalPluginEditor) GetOption(id pluginapi.BufferID, name string) (any, error) {
	local, err := g.in(id)
	if err != nil {
		return nil, err
	}
	return g.BufferEditor.GetOption(local, name)
}
