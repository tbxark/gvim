package plugin

import (
	"github.com/tbxark/gvim/pluginapi"
)

// pauseSlice stops charging the current slice for a heavy Host call and returns the func
// that charges again. Outside a slice both are no-ops.
func (r *Runtime) pauseSlice() func() {
	s := r.cur
	if s == nil || s.paused {
		return func() {}
	}
	s.pause()
	return s.resume
}

// hostGuard is the Host the API installers see. It forwards every call and, for the heavy
// ones (edits, big reads, searches, decoration bulk writes, action runs), pauses the slice
// clock so a slow editor operation is not charged to the plugin. Light
// calls (Buffer, Cursors, ...) are not paused. The hard deadline is never paused.
type hostGuard struct {
	pluginapi.Host
	r *Runtime
}

// heavyLines is the line count above which Lines counts as a heavy call.
const heavyLines = 1000

func (g hostGuard) Edit(id pluginapi.BufferID, edits []pluginapi.Edit, o pluginapi.EditOptions) error {
	defer g.r.pauseSlice()()
	return g.Host.Edit(id, edits, o)
}

func (g hostGuard) Lines(id pluginapi.BufferID, first, count int) ([]string, error) {
	if count < 0 || count > heavyLines {
		defer g.r.pauseSlice()()
	}
	return g.Host.Lines(id, first, count)
}

func (g hostGuard) Slice(id pluginapi.BufferID, rg pluginapi.Range) (string, error) {
	defer g.r.pauseSlice()()
	return g.Host.Slice(id, rg)
}

func (g hostGuard) Search(id pluginapi.BufferID, q pluginapi.SearchQuery) (pluginapi.SearchResult, error) {
	defer g.r.pauseSlice()()
	return g.Host.Search(id, q)
}

func (g hostGuard) FindLines(id pluginapi.BufferID, q pluginapi.FindLinesQuery) (pluginapi.FindLinesResult, error) {
	defer g.r.pauseSlice()()
	return g.Host.FindLines(id, q)
}

func (g hostGuard) OffsetOf(id pluginapi.BufferID, p pluginapi.Pos) (int, error) {
	defer g.r.pauseSlice()()
	return g.Host.OffsetOf(id, p)
}

func (g hostGuard) PosOf(id pluginapi.BufferID, offset int) (pluginapi.Pos, error) {
	defer g.r.pauseSlice()()
	return g.Host.PosOf(id, offset)
}

func (g hostGuard) FullText(id pluginapi.BufferID) (string, pluginapi.Version, error) {
	defer g.r.pauseSlice()()
	return g.Host.FullText(id)
}

// DiffSource reads a whole buffer's lines and byte geometry: heavy like FullText.
func (g hostGuard) DiffSource(id pluginapi.BufferID) (pluginapi.DiffSource, error) {
	defer g.r.pauseSlice()()
	return g.Host.DiffSource(id)
}

// ReplaceRange is one atomic edit through the version-guarded path: heavy like Edit.
func (g hostGuard) ReplaceRange(id pluginapi.BufferID, start, end int, text string, expect *pluginapi.Version) (bool, error) {
	defer g.r.pauseSlice()()
	return g.Host.ReplaceRange(id, start, end, text, expect)
}

func (g hostGuard) SetContent(id pluginapi.BufferID, entries []pluginapi.ContentEntry) error {
	defer g.r.pauseSlice()()
	return g.Host.SetContent(id, entries)
}

func (g hostGuard) DecorSet(plugin, ns string, id pluginapi.BufferID, items []pluginapi.Decoration, expect *pluginapi.Version) error {
	defer g.r.pauseSlice()()
	return g.Host.DecorSet(plugin, ns, id, items, expect)
}

func (g hostGuard) DecorAdd(plugin, ns string, id pluginapi.BufferID, item pluginapi.Decoration, expect *pluginapi.Version) (int, error) {
	defer g.r.pauseSlice()()
	return g.Host.DecorAdd(plugin, ns, id, item, expect)
}

func (g hostGuard) Do(action string, args map[string]any, count int) error {
	defer g.r.pauseSlice()()
	return g.Host.Do(action, args, count)
}

func (g hostGuard) DoAll(steps []pluginapi.Step) error {
	defer g.r.pauseSlice()()
	return g.Host.DoAll(steps)
}

// UndoGroup pauses the clock around the host's work but not around fn, which is Lua
// running inside the group and is charged to the plugin as usual.
func (g hostGuard) UndoGroup(id pluginapi.BufferID, fn func() error) error {
	end := g.r.pauseSlice()
	defer func() { end() }()
	return g.Host.UndoGroup(id, func() error {
		end() // Lua runs again: charge it
		defer func() { end = g.r.pauseSlice() }()
		return fn()
	})
}
