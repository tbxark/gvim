package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/buffer"
	"github.com/tbxark/gvim/editor"
	"github.com/tbxark/gvim/internal/tui/dialog"
	"github.com/tbxark/gvim/pluginapi"
	"github.com/tbxark/gvim/theme"
)

// eventsPE is the plugin editor port of the event tests: the fake port of the host tests
// plus the change tracking, with a record of what the root asked of it.
type eventsPE struct {
	*fakePluginEditor
	lists   int // ListBuffers calls
	tracks  []string
	takes   int
	pending map[pluginapi.BufferID][]pluginapi.Changes // answered one per TakeChanges call
}

func (e *eventsPE) ListBuffers() []pluginapi.BufferInfo {
	e.lists++
	return slices.Clone(e.bufs)
}

func (e *eventsPE) TrackChanges(key string, on bool) {
	e.tracks = append(e.tracks, fmt.Sprintf("%s:%v", key, on))
}

func (e *eventsPE) TakeChanges(id pluginapi.BufferID, consumer string) pluginapi.Changes {
	e.takes++
	if consumer != "events" {
		return pluginapi.Changes{}
	}
	q := e.pending[id]
	if len(q) == 0 {
		return pluginapi.Changes{}
	}
	e.pending[id] = q[1:]
	return q[0]
}

// queue makes the next TakeChanges of id answer c.
func (e *eventsPE) queue(id pluginapi.BufferID, c pluginapi.Changes) {
	if e.pending == nil {
		e.pending = map[pluginapi.BufferID][]pluginapi.Changes{}
	}
	e.pending[id] = append(e.pending[id], c)
}

// scriptEditor is a fake editor whose actions run a script, so a test can change the tabs,
// the caret and the text inside the Update that handles a key.
type scriptEditor struct {
	*fakeEditor
	onDo func(a editor.Action)
	err  error // the error every action answers with
	doc  Document
}

func (s *scriptEditor) Do(a editor.Action, args map[string]any) editor.Result {
	if s.onDo != nil {
		s.onDo(a)
	}
	res := s.fakeEditor.Do(a, args)
	res.Err = s.err
	return res
}

func (s *scriptEditor) Active() Document {
	if s.doc != nil {
		return s.doc
	}
	return s.fakeEditor.Active()
}

// evSelDoc is a document with a selection.
type evSelDoc struct {
	*fakeDoc
	anchor, head buffer.Pos
	has          bool
}

func (d *evSelDoc) Selection() (a, h buffer.Pos, ok bool) { return d.anchor, d.head, d.has }

// evHarness is a model over a fake runtime that wants the events in wants, three buffers
// (a.txt is active) and an events port.
type evHarness struct {
	m  *Model
	ed *scriptEditor
	pe *eventsPE
	rt *fakeRuntime
}

func newEvHarness(t *testing.T, wants ...string) *evHarness {
	t.Helper()
	rt := newFakeRuntime(t)
	for _, w := range wants {
		rt.wants[w] = true
	}
	fed := newFakeEditor("hello", "wörld")
	fed.quitAction = "quit"
	fed.tabs = []TabInfo{{Name: "a.txt", Path: "/w/a.txt", Active: true}, {Name: "b.txt", Path: "/w/b.txt"}, {Name: "c.txt", Path: "/w/c.txt"}}
	se := &scriptEditor{fakeEditor: fed}
	pe := &eventsPE{fakePluginEditor: &fakePluginEditor{ed: fed, active: 1, bufs: []pluginapi.BufferInfo{
		{ID: 1, Path: "/w/a.txt", Name: "a.txt", Active: true, Language: "go"},
		{ID: 2, Path: "/w/b.txt", Name: "b.txt"},
		{ID: 3, Path: "/w/c.txt", Name: "c.txt"},
	}}}
	m := New(Deps{Editor: se, Keys: testKeys(), Theme: theme.Theme{}, Clock: testClock, Root: RootPorts{Plugins: rt, PluginEditor: pe}})
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	useHost(rt, m)
	rt.emitted, rt.log = nil, nil
	pe.lists, pe.tracks, pe.takes = 0, nil, 0
	return &evHarness{m: m, ed: se, pe: pe, rt: rt}
}

// update handles msg and returns the events the runtime got, as "name key=value ..." rows.
func (h *evHarness) update(msg tea.Msg) []string {
	h.rt.emitted = nil
	h.m.Update(msg)
	return evRows(h.rt.emitted)
}

// act runs script inside the Update of Ctrl+N, which dispatches the action "new".
func (h *evHarness) act(script func()) []string {
	h.ed.onDo = func(editor.Action) { script() }
	return h.update(ctrl('n'))
}

func evRows(evs []pluginapi.Event) []string {
	var out []string
	for _, e := range evs {
		keys := make([]string, 0, len(e.Data))
		for k := range e.Data {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		row := e.Name
		for _, k := range keys {
			row += fmt.Sprintf(" %s=%v", k, e.Data[k])
		}
		out = append(out, row)
	}
	return out
}

func wantRows(t *testing.T, got, want []string) {
	t.Helper()
	if !slices.Equal(got, want) {
		t.Fatalf("events:\n got  %q\n want %q", got, want)
	}
}

// TestPluginSnapshotCostsNothingWithoutSubscribers checks that when no plugin listens the
// root reads no tab list, turns no tracking on and takes no change records, whatever
// changes in the Update.
func TestPluginSnapshotCostsNothingWithoutSubscribers(t *testing.T) {
	t.Parallel()
	h := newEvHarness(t)
	got := h.act(func() {
		h.pe.bufs = append(h.pe.bufs, pluginapi.BufferInfo{ID: 4, Path: "/w/d.txt"})
		h.pe.active = 4
		h.ed.doc = &fakeDoc{lines: []string{"x"}, cursor: buffer.Pos{Col: 1}}
	})
	wantRows(t, got, nil)
	if h.pe.lists != 0 || h.pe.takes != 0 || len(h.pe.tracks) != 0 {
		t.Fatalf("the root read the port with no subscriber: %d lists, %d takes, tracks %v", h.pe.lists, h.pe.takes, h.pe.tracks)
	}
	if len(h.rt.log) != 0 {
		t.Fatalf("the runtime was called with nothing to say: %v", h.rt.log)
	}
}

// TestPluginTabEvents checks the events of the tab diff: files opened, buffers closed and
// activated, language and version changes.
func TestPluginTabEvents(t *testing.T) {
	t.Parallel()
	all := []string{"after_file_open", "buffer_closed", "buffer_activated", "buffer_deactivated", "language_changed", "buffer_modified"}
	tests := []struct {
		name   string
		change func(pe *eventsPE)
		want   []string
	}{
		{"nothing changes", func(*eventsPE) {}, nil},
		{"a file opens and takes the focus", func(pe *eventsPE) {
			pe.bufs = append(pe.bufs, pluginapi.BufferInfo{ID: 4, Path: "/w/d.txt"})
			pe.active = 4
		}, []string{
			"after_file_open buf=4 path=/w/d.txt",
			"buffer_deactivated buf=1 next=4",
			"buffer_activated buf=4 previous=1",
		}},
		{"an untitled buffer is not a file open", func(pe *eventsPE) {
			pe.bufs = append(pe.bufs, pluginapi.BufferInfo{ID: 4})
			pe.active = 4
		}, []string{"buffer_deactivated buf=1 next=4", "buffer_activated buf=4 previous=1"}},
		{"a file opens in the background", func(pe *eventsPE) {
			pe.bufs = append(pe.bufs, pluginapi.BufferInfo{ID: 4, Path: "/w/d.txt"})
		}, []string{"after_file_open buf=4 path=/w/d.txt"}},
		{"another tab is activated", func(pe *eventsPE) { pe.active = 2 },
			[]string{"buffer_deactivated buf=1 next=2", "buffer_activated buf=2 previous=1"}},
		{"an inactive tab closes", func(pe *eventsPE) { pe.bufs = pe.bufs[:2] },
			[]string{"buffer_closed id=3 path=/w/c.txt"}},
		{"the active tab closes, the next one is activated without a previous", func(pe *eventsPE) {
			pe.bufs = pe.bufs[1:]
			pe.active = 2
		}, []string{"buffer_closed id=1 path=/w/a.txt", "buffer_activated buf=2"}},
		{"the last tab closes", func(pe *eventsPE) {
			pe.bufs = nil
			pe.active = 0
		}, []string{"buffer_closed id=1 path=/w/a.txt", "buffer_closed id=2 path=/w/b.txt", "buffer_closed id=3 path=/w/c.txt"}},
		{"the language changes", func(pe *eventsPE) { pe.bufs[0].Language = "lua" },
			[]string{"language_changed buf=1 language=lua"}},
		{"the version and the modified flag change", func(pe *eventsPE) {
			pe.bufs[0].Version = pluginapi.Version{Gen: 1, Ver: 5}
			pe.bufs[0].Modified = true
		}, []string{"buffer_modified buf=1 modified=true version=1.5"}},
		{"the version changes alone", func(pe *eventsPE) { pe.bufs[1].Version = pluginapi.Version{Gen: 2, Ver: 0} },
			[]string{"buffer_modified buf=2 modified=false version=2.0"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			h := newEvHarness(t, all...)
			wantRows(t, h.act(func() { tt.change(h.pe) }), tt.want)
		})
	}
}

// TestPluginTabEventsAreGatedByWants checks that a plugin that listens to one of the
// events does not hear the others.
func TestPluginTabEventsAreGatedByWants(t *testing.T) {
	t.Parallel()
	h := newEvHarness(t, "buffer_closed")
	got := h.act(func() {
		h.pe.bufs = append(h.pe.bufs[1:], pluginapi.BufferInfo{ID: 4, Path: "/w/d.txt"})
		h.pe.active = 4
	})
	wantRows(t, got, []string{"buffer_closed id=1 path=/w/a.txt"})
}

// TestPluginCaretEvents checks cursor_moved and selection_changed: byte columns of a line
// with multi-byte text, the earliest previous, a sorted range, no event for a tab switch.
func TestPluginCaretEvents(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		start func(d *evSelDoc)
		move  func(d *evSelDoc, pe *eventsPE)
		want  []string
	}{
		{"the caret moves along a line with multi-byte text",
			func(d *evSelDoc) { d.cursor = buffer.Pos{Line: 1, Col: 1} },
			func(d *evSelDoc, _ *eventsPE) { d.cursor = buffer.Pos{Line: 1, Col: 3} },
			// "wörld": the caret before 'l' is rune column 3, byte column 4.
			[]string{"cursor_moved buf=1 pos={1 4} previous={1 1}"}},
		{"the caret stays",
			func(d *evSelDoc) { d.cursor = buffer.Pos{Line: 1, Col: 2} },
			func(*evSelDoc, *eventsPE) {}, nil},
		{"a selection is made backwards",
			func(d *evSelDoc) { d.cursor = buffer.Pos{Line: 1, Col: 0} },
			func(d *evSelDoc, _ *eventsPE) {
				d.has, d.anchor, d.head = true, buffer.Pos{Line: 1, Col: 3}, buffer.Pos{Line: 0, Col: 2}
				d.cursor = d.head
			},
			[]string{
				"cursor_moved buf=1 pos={0 2} previous={1 0}",
				"selection_changed buf=1 range={{0 2} {1 4}}",
			}},
		{"the selection ends",
			func(d *evSelDoc) {
				d.has, d.anchor, d.head = true, buffer.Pos{Line: 0, Col: 1}, buffer.Pos{Line: 0, Col: 3}
				d.cursor = d.head
			},
			func(d *evSelDoc, _ *eventsPE) { d.has = false },
			[]string{"selection_changed buf=1"}},
		{"another tab is activated: the caret of the new tab is no move",
			func(d *evSelDoc) { d.cursor = buffer.Pos{Line: 0, Col: 4} },
			func(d *evSelDoc, pe *eventsPE) {
				pe.active = 2
				d.cursor = buffer.Pos{Line: 0, Col: 1}
			}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			h := newEvHarness(t, "cursor_moved", "selection_changed")
			d := &evSelDoc{fakeDoc: &fakeDoc{lines: []string{"hello", "wörld"}}}
			tt.start(d)
			h.ed.doc = d
			wantRows(t, h.act(func() { tt.move(d, h.pe) }), tt.want)
		})
	}
}

// TestPluginResizeIsRaisedWithoutSubscribers checks that resize is emitted whether or not a
// plugin listens (the plugins load later than the first size), and not for the first size.
func TestPluginResizeIsRaisedWithoutSubscribers(t *testing.T) {
	t.Parallel()
	h := newEvHarness(t)
	wantRows(t, h.update(tea.WindowSizeMsg{Width: 100, Height: 30}), []string{"resize height=30 width=100"})
	wantRows(t, h.update(tea.WindowSizeMsg{Width: 100, Height: 30}), nil)

	fresh := newFakeRuntime(t)
	m := New(Deps{Editor: newFakeEditor("x"), Keys: testKeys(), Theme: theme.Theme{}, Clock: testClock, Root: RootPorts{Plugins: fresh}})
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	if len(fresh.emitted) != 0 {
		t.Fatalf("the first size raised %v", evRows(fresh.emitted))
	}
}

// TestPluginViewportEvent checks viewport_changed: the top line of the focused pane and
// its size, only for the same buffer, and once the Update has scrolled.
func TestPluginViewportEvent(t *testing.T) {
	t.Parallel()
	h := newEvHarness(t, "viewport_changed")
	d := &fakeDoc{lines: make([]string, 100)}
	h.ed.doc = d
	got := h.act(func() { d.top = 7 })
	if len(got) != 1 || !strings.HasPrefix(got[0], "viewport_changed buf=1 height=") || !strings.Contains(got[0], "top_line=7") {
		t.Fatalf("scroll: events %q", got)
	}
	wantRows(t, h.act(func() {}), nil)
	// A resize changes the pane size.
	got = h.update(tea.WindowSizeMsg{Width: 60, Height: 14})
	if len(got) != 2 || got[0] != "resize height=14 width=60" || !strings.HasPrefix(got[1], "viewport_changed buf=1 height=") {
		t.Fatalf("resize: events %q, want resize and viewport_changed", got)
	}
	// Another buffer's viewport is not a viewport change.
	wantRows(t, h.act(func() { h.pe.active = 2; d.top = 3 }), nil)
}

// TestChangeTrackingFollowsTheSubscription checks that the root turns the editor's change
// tracking on while a plugin listens to buffer_changed and off after it stopped.
func TestChangeTrackingFollowsTheSubscription(t *testing.T) {
	t.Parallel()
	h := newEvHarness(t)
	h.update(ctrl('n'))
	h.rt.wants["buffer_changed"] = true
	h.update(ctrl('n'))
	h.update(ctrl('n'))
	h.rt.wants["buffer_changed"] = false
	h.update(ctrl('n'))
	if want := []string{"events:true", "events:false"}; !slices.Equal(h.pe.tracks, want) {
		t.Fatalf("tracking calls = %v, want %v", h.pe.tracks, want)
	}
}

func rec(line, col int, text, removed string) pluginapi.ChangeRec {
	return pluginapi.ChangeRec{
		Range: pluginapi.Range{From: pluginapi.Pos{Line: line, Col: col}, To: pluginapi.Pos{Line: line, Col: col + len(removed)}},
		Text:  text, Removed: removed,
	}
}

// TestBufferChangedPayload checks the buffer_changed event: one event per buffer per
// Update with all the records in order, the undo flag, the overflow rules.
func TestBufferChangedPayload(t *testing.T) {
	t.Parallel()
	many := make([]pluginapi.ChangeRec, maxChangedEdits+1)
	for i := range many {
		many[i] = rec(0, i, "x", "")
	}
	v := pluginapi.Version{Gen: 1, Ver: 9}
	tests := []struct {
		name string
		take map[pluginapi.BufferID]pluginapi.Changes
		want []string
	}{
		{"no change", nil, nil},
		{"records of one Update arrive in order in one event",
			map[pluginapi.BufferID]pluginapi.Changes{1: {Version: v, Edits: []pluginapi.ChangeRec{rec(0, 0, "a", ""), rec(0, 1, "b", ""), rec(0, 0, "", "a")}}},
			[]string{"buffer_changed buf=1 edits=[{{{0 0} {0 0}} a } {{{0 1} {0 1}} b } {{{0 0} {0 1}}  a}] overflow=false undo=false version=1.9"}},
		{"undo",
			map[pluginapi.BufferID]pluginapi.Changes{2: {Version: v, Undo: true, Edits: []pluginapi.ChangeRec{rec(1, 0, "", "x")}}},
			[]string{"buffer_changed buf=2 edits=[{{{1 0} {1 1}}  x}] overflow=false undo=true version=1.9"}},
		{"overflow reported by the editor has no edits",
			map[pluginapi.BufferID]pluginapi.Changes{1: {Version: v, Overflow: true}},
			[]string{"buffer_changed buf=1 overflow=true undo=false version=1.9"}},
		{"more than 256 records overflow",
			map[pluginapi.BufferID]pluginapi.Changes{1: {Version: v, Edits: many}},
			[]string{"buffer_changed buf=1 overflow=true undo=false version=1.9"}},
		{"exactly 256 records are kept",
			map[pluginapi.BufferID]pluginapi.Changes{3: {Version: v, Edits: many[:maxChangedEdits]}},
			nil}, // checked below by count
		{"two buffers",
			map[pluginapi.BufferID]pluginapi.Changes{
				1: {Version: v, Edits: []pluginapi.ChangeRec{rec(0, 0, "a", "")}},
				3: {Version: v, Edits: []pluginapi.ChangeRec{rec(0, 0, "c", "")}},
			},
			[]string{
				"buffer_changed buf=1 edits=[{{{0 0} {0 0}} a }] overflow=false undo=false version=1.9",
				"buffer_changed buf=3 edits=[{{{0 0} {0 0}} c }] overflow=false undo=false version=1.9",
			}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			h := newEvHarness(t, "buffer_changed")
			got := h.act(func() {
				for id, c := range tt.take {
					h.pe.queue(id, c)
				}
			})
			if tt.name == "exactly 256 records are kept" {
				if len(got) != 1 || !strings.Contains(got[0], "overflow=false") || strings.Count(got[0], "{{{0 ") != maxChangedEdits {
					t.Fatalf("256 records: %d rows, %.80q", len(got), got)
				}
				return
			}
			wantRows(t, got, tt.want)
		})
	}
}

// TestHandlerEditsAreHeardOnceInTheSameUpdate checks that what a handler changes while the
// pump delivers the events reaches the plugins in the same Update, and that the root does
// not loop: a handler that edits on every event gets one more round, not an endless one.
func TestHandlerEditsAreHeardOnceInTheSameUpdate(t *testing.T) {
	t.Parallel()
	h := newEvHarness(t, "buffer_changed")
	n := 0
	h.rt.onDrain = func() ([]pluginapi.Work, bool) {
		// Every delivery makes another edit, as a handler that reacts to changes would.
		n++
		h.pe.queue(1, pluginapi.Changes{Version: pluginapi.Version{Gen: 1, Ver: uint64(10 + n)}, Edits: []pluginapi.ChangeRec{rec(0, 0, "h", "")}})
		return nil, false
	}
	got := h.act(func() {
		h.pe.queue(1, pluginapi.Changes{Version: pluginapi.Version{Gen: 1, Ver: 10}, Edits: []pluginapi.ChangeRec{rec(0, 0, "e", "")}})
	})
	if len(got) != 2 {
		t.Fatalf("events = %q, want the user's edit and the handler's edit, then stop", got)
	}
	if !strings.Contains(got[0], "version=1.10") || !strings.Contains(got[1], "version=1.11") {
		t.Fatalf("events = %q, want versions 1.10 then 1.11", got)
	}
}

// TestSaveGateSources checks the source of before_file_save for every way a save starts,
// and that a veto stops it.
func TestSaveGateSources(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		bufs      []pluginapi.BufferInfo
		start     func(h *evHarness)
		want      []string
		vetoAt    int // 1-based number of the veto call that cancels; 0 never
		wantSaved []string
	}{
		{"Ctrl+S", nil, func(h *evHarness) { h.update(ctrl('s')) },
			[]string{"before_file_save buf=1 path=/w/a.txt source=manual"}, 0, []string{"save"}},
		{"Ctrl+S vetoed", nil, func(h *evHarness) { h.update(ctrl('s')) },
			[]string{"before_file_save buf=1 path=/w/a.txt source=manual"}, 1, nil},
		{"Ctrl+S on an untitled buffer asks nobody, the Save As dialog does", []pluginapi.BufferInfo{
			{ID: 1, Name: "untitled", Active: true, Modified: true},
		}, func(h *evHarness) { h.update(ctrl('s')) },
			nil, 0, []string{"save"}},
		{"Ctrl+S on a read-only buffer asks nobody", []pluginapi.BufferInfo{
			{ID: 1, Path: "/w/a.txt", Active: true, Modified: true, ReadOnly: true},
		}, func(h *evHarness) { h.update(ctrl('s')) },
			nil, 0, []string{"save"}},
		{"save all names every modified named writable buffer", []pluginapi.BufferInfo{
			{ID: 1, Path: "/w/a.txt", Modified: true},
			{ID: 2, Path: "/w/b.txt"},
			{ID: 3, Path: "/w/c.txt", Modified: true, ReadOnly: true},
			{ID: 4, Modified: true},
			{ID: 5, Path: "/w/e.txt", Modified: true},
		}, func(h *evHarness) { must(t, h.m.Run("save_all")) },
			[]string{
				"before_file_save buf=1 path=/w/a.txt source=manual",
				"before_file_save buf=5 path=/w/e.txt source=manual",
			}, 0, []string{"save_all"}},
		{"save all vetoed by the first handler asks no more", []pluginapi.BufferInfo{
			{ID: 1, Path: "/w/a.txt", Modified: true},
			{ID: 5, Path: "/w/e.txt", Modified: true},
		}, func(h *evHarness) { must(t, h.m.Run("save_all")) },
			[]string{"before_file_save buf=1 path=/w/a.txt source=manual"}, 1, nil},
		{"save all with nothing modified asks nobody", nil, func(h *evHarness) { must(t, h.m.Run("save_all")) },
			nil, 0, []string{"save_all"}},
		{"the Save As dialog names the target path", nil, func(h *evHarness) {
			h.update(dialog.Result{Tag: saveAsTag{}, Choice: dialog.ChoiceSave, Text: "/w/new.txt"})
		}, []string{"before_file_save buf=1 path=/w/new.txt source=manual"}, 0, []string{"save_as"}},
		{"the Save As dialog vetoed", nil, func(h *evHarness) {
			h.update(dialog.Result{Tag: saveAsTag{}, Choice: dialog.ChoiceSave, Text: "/w/new.txt"})
		}, []string{"before_file_save buf=1 path=/w/new.txt source=manual"}, 1, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			h := newEvHarness(t)
			h.rt.wants["before_file_save"] = true
			if tt.bufs != nil {
				h.pe.bufs = tt.bufs
			}
			calls := 0
			h.rt.veto = func(pluginapi.Event) (bool, string) {
				calls++
				if calls == tt.vetoAt {
					return true, "no thanks"
				}
				return false, ""
			}
			tt.start(h)
			wantRows(t, evRows(h.rt.vetoed), tt.want)
			if got := actionsOf(h.ed.fakeEditor); !slices.Equal(got, tt.wantSaved) {
				t.Fatalf("editor actions = %v, want %v", got, tt.wantSaved)
			}
			if tt.vetoAt > 0 && h.m.notice != "no thanks" {
				t.Fatalf("notice = %q, want the reason of the veto", h.m.notice)
			}
		})
	}
}

// autoSaver is an asyncAutoSaver over a real editor. Planning is counted, and the jobs are
// the editor's.
type autoSaver struct {
	*scriptEditor
	real  *editor.Editor
	plans int
	saved []editor.SaveResult
}

func (a *autoSaver) AutoSaveDue(time.Time) bool { return true }
func (a *autoSaver) PlanAutoSave(now time.Time) []editor.SaveJob {
	a.plans++
	return a.real.PlanAutoSave(now)
}
func (a *autoSaver) ApplyAutoSave(r []editor.SaveResult) error {
	a.saved = r
	return a.real.ApplySaves(r)
}

// openedEditor returns a real editor with one modified file open.
func openedEditor(t *testing.T) (*editor.Editor, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "a.txt")
	must(t, os.WriteFile(path, []byte("abc\n"), 0o644))
	ed := editor.New(editor.Options{AutoSaveInterval: time.Second})
	must(t, ed.OpenFile(path))
	if res := ed.Do("insert_char", map[string]any{"text": "x"}); res.Err != nil {
		t.Fatal(res.Err)
	}
	return ed, path
}

// TestAutoSaveRaisesTheGateAndTheSaveEvent checks the auto-save tick: before_file_save
// with source "autosave" for the modified buffers, no planning when a handler cancels, and
// after_file_save for the files written.
func TestAutoSaveRaisesTheGateAndTheSaveEvent(t *testing.T) {
	t.Parallel()
	for _, veto := range []bool{false, true} {
		t.Run(fmt.Sprintf("veto=%v", veto), func(t *testing.T) {
			t.Parallel()
			real, path := openedEditor(t)
			rt := newFakeRuntime(t)
			rt.wants["before_file_save"], rt.wants["after_file_save"] = true, true
			rt.veto = func(pluginapi.Event) (bool, string) { return veto, "busy" }
			fed := newFakeEditor("x")
			as := &autoSaver{scriptEditor: &scriptEditor{fakeEditor: fed}, real: real}
			pe := &eventsPE{fakePluginEditor: &fakePluginEditor{active: 1, bufs: []pluginapi.BufferInfo{
				{ID: 1, Path: path, Modified: true}, {ID: 2, Path: "/w/clean.txt"},
			}}}
			m := New(Deps{Editor: as, Keys: testKeys(), Theme: theme.Theme{}, Clock: testClock,
				Root: RootPorts{Plugins: rt, PluginEditor: pe, AutoSave: time.Second}})
			m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
			useHost(rt, m)

			_, cmd := m.Update(autoSaveTickMsg{})
			wantRows(t, evRows(rt.vetoed), []string{fmt.Sprintf("before_file_save buf=1 path=%s source=autosave", path)})
			if veto {
				if as.plans != 0 {
					t.Fatalf("a cancelled auto-save planned %d times", as.plans)
				}
				if m.notice != "" {
					t.Fatalf("a cancelled auto-save wrote %q on the status row", m.notice)
				}
				if cmd == nil {
					t.Fatal("a cancelled auto-save did not arm the next tick")
				}
				return
			}
			if as.plans != 1 {
				t.Fatalf("auto-save planned %d times, want 1", as.plans)
			}
			msgs := runCmd(cmd)
			var done autoSaveDoneMsg
			for _, msg := range msgs {
				if d, ok := msg.(autoSaveDoneMsg); ok {
					done = d
				}
			}
			if len(done.results) != 1 {
				t.Fatalf("auto-save wrote %d results from %v", len(done.results), msgs)
			}
			rt.emitted = nil
			m.Update(done)
			id := pluginapi.BufferID(real.Active().ID())
			wantRows(t, evRows(rt.emitted), []string{fmt.Sprintf("after_file_save buf=%d path=%s", id, path)})
		})
	}
}

// TestAutoSaveGateSkipsLargeDocuments checks that the auto-save gate asks about the
// documents PlanAutoSave would write: a modified large document is never auto-saved, so no
// before_file_save is raised for it and a veto of it cannot cancel the round of the others.
func TestAutoSaveGateSkipsLargeDocuments(t *testing.T) {
	t.Parallel()
	real, path := openedEditor(t)
	rt := newFakeRuntime(t)
	rt.wants["before_file_save"] = true
	rt.veto = func(e pluginapi.Event) (bool, string) { return e.Data["buf"] == pluginapi.BufferID(9), "veto" }
	as := &autoSaver{scriptEditor: &scriptEditor{fakeEditor: newFakeEditor("x")}, real: real}
	pe := &eventsPE{fakePluginEditor: &fakePluginEditor{active: 1, bufs: []pluginapi.BufferInfo{
		{ID: 9, Path: "/w/big.txt", Modified: true, Large: true},
		{ID: 1, Path: path, Modified: true},
	}}}
	m := New(Deps{Editor: as, Keys: testKeys(), Theme: theme.Theme{}, Clock: testClock,
		Root: RootPorts{Plugins: rt, PluginEditor: pe, AutoSave: time.Second}})
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	useHost(rt, m)

	m.Update(autoSaveTickMsg{})
	wantRows(t, evRows(rt.vetoed), []string{fmt.Sprintf("before_file_save buf=1 path=%s source=autosave", path)})
	if as.plans != 1 {
		t.Fatalf("auto-save planned %d times, want 1 (the large document's veto must not cancel the round)", as.plans)
	}
}

// saver is an asyncSaver over a real editor.
type saver struct {
	*scriptEditor
	real *editor.Editor
}

func (s *saver) PlanSave() (editor.SavePlan, error)              { return s.real.PlanSave() }
func (s *saver) PlanSaveAll() (editor.SavePlan, error)           { return s.real.PlanSaveAll() }
func (s *saver) WriteSave(p editor.SavePlan) []editor.SaveResult { return p.Run() }
func (s *saver) PlanSaveAs(path string, force bool) (editor.SavePlan, error) {
	return s.real.PlanSaveAs(path, force)
}
func (s *saver) ApplySavePlan(p editor.SavePlan, r []editor.SaveResult) error {
	return s.real.ApplySavePlan(p, r)
}

// TestSaveDoneRaisesAfterFileSave checks after_file_save: one event per file written with
// the buffer and the path, none for a failed write, and none when no plugin listens.
func TestSaveDoneRaisesAfterFileSave(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name  string
		wants bool
		fail  bool
		want  bool
	}{
		{"written", true, false, true},
		{"failed write", true, true, false},
		{"nobody listens", false, false, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			real, path := openedEditor(t)
			rt := newFakeRuntime(t)
			rt.wants["after_file_save"] = tt.wants
			s := &saver{scriptEditor: &scriptEditor{fakeEditor: newFakeEditor("x")}, real: real}
			m := New(Deps{Editor: s, Keys: testKeys(), Theme: theme.Theme{}, Clock: testClock, Root: RootPorts{Plugins: rt}})
			m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
			useHost(rt, m)
			plan, err := real.PlanSave()
			must(t, err)
			if tt.fail {
				// The file changed on disk since the plan: the write is refused.
				must(t, os.WriteFile(path, []byte("changed behind our back\n"), 0o644))
			}
			rt.emitted = nil
			m.Update(saveDoneMsg{plan: plan, results: plan.Run()})
			var want []string
			if tt.want {
				want = []string{fmt.Sprintf("after_file_save buf=%d path=%s", real.Active().ID(), path)}
			}
			wantRows(t, evRows(rt.emitted), want)
		})
	}
}

// reverter is an asyncReverter that always applies.
type reverter struct {
	*scriptEditor
}

func (reverter) PlanRevert(bool) (editor.RevertPlan, error)      { return editor.RevertPlan{}, nil }
func (reverter) RunRevert(editor.RevertPlan) editor.RevertResult { return editor.RevertResult{} }
func (reverter) ApplyRevert(editor.RevertPlan, editor.RevertResult) error {
	return nil
}

// TestRevertRaisesAfterFileRevert checks after_file_revert for a revert that was applied,
// and none for a read that failed.
func TestRevertRaisesAfterFileRevert(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		res  editor.RevertResult
		want []string
	}{
		{"applied", editor.RevertResult{}, []string{"after_file_revert buf=2 path=/w/b.txt"}},
		{"read failed", editor.RevertResult{Err: os.ErrNotExist}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			h := newEvHarness(t, "after_file_revert")
			h.m.ed = reverter{h.ed}
			wantRows(t, h.update(revertDoneMsg{plan: editor.RevertPlan{Path: "/w/b.txt"}, result: tt.res}), tt.want)
		})
	}
}

// TestThemeAndConfigEvents checks theme_changed with the theme name and config_changed
// after a keymap switch.
func TestThemeAndConfigEvents(t *testing.T) {
	t.Parallel()
	names := theme.Names()
	if len(names) == 0 {
		t.Skip("no themes")
	}
	h := newEvHarness(t, "theme_changed", "config_changed")
	wantRows(t, h.update(nil), nil)
	h.m.dispatch(Command{Action: "select_theme", Args: map[string]any{"name": names[0]}})
	wantRows(t, evRowsOf(h), []string{"theme_changed dark=true name=" + names[0]})
	h.m.dispatch(Command{Action: "select_theme", Args: map[string]any{"name": "no such theme"}})
	wantRows(t, evRowsOf(h), nil)
	h.m.util.switchKeymap = func(string) (Keymap, error) { return testKeys(), nil }
	h.m.switchKeymap("emacs")
	wantRows(t, evRowsOf(h), []string{"config_changed"})
}

// evRowsOf pumps the events a direct call queued and returns what the runtime got.
func evRowsOf(h *evHarness) []string {
	h.rt.emitted = nil
	h.m.pluginPump()
	return evRows(h.rt.emitted)
}

// TestPreAndPostCommand checks pre_command and post_command for the commands the user
// starts, with the action, the arguments and the outcome, and that a command a plugin runs
// is not announced.
func TestPreAndPostCommand(t *testing.T) {
	t.Parallel()
	t.Run("a key command", func(t *testing.T) {
		t.Parallel()
		h := newEvHarness(t, "pre_command", "post_command")
		wantRows(t, h.update(typed("a")), []string{
			"pre_command action=insert_char args=map[text:a]",
			"post_command action=insert_char args=map[text:a] ok=true",
		})
	})
	t.Run("a failing command", func(t *testing.T) {
		t.Parallel()
		h := newEvHarness(t, "post_command")
		h.ed.err = os.ErrPermission
		wantRows(t, h.update(ctrl('n')), []string{
			"post_command action=new args=map[] err=permission denied ok=false",
		})
	})
	t.Run("a menu command", func(t *testing.T) {
		t.Parallel()
		h := newEvHarness(t, "pre_command", "post_command")
		h.rt.emitted = nil
		must(t, h.m.Run("copy"))
		h.m.pluginPump()
		wantRows(t, evRows(h.rt.emitted), []string{
			"pre_command action=copy args=map[]",
			"post_command action=copy args=map[] ok=true",
		})
	})
	t.Run("only pre is wanted", func(t *testing.T) {
		t.Parallel()
		h := newEvHarness(t, "pre_command")
		wantRows(t, h.update(ctrl('n')), []string{"pre_command action=new args=map[]"})
	})
	t.Run("nobody listens", func(t *testing.T) {
		t.Parallel()
		h := newEvHarness(t)
		wantRows(t, h.update(ctrl('n')), nil)
	})
	t.Run("a command the plugin runs is not announced", func(t *testing.T) {
		t.Parallel()
		h := newEvHarness(t, "pre_command", "post_command")
		h.m.plug.quiet++
		wantRows(t, h.update(ctrl('n')), nil)
		h.m.plug.quiet--
	})
}

// TestQuitRaisesNoShutdownEvent checks that quitting raises no shutdown event.
// Close stops the runtime, which runs the shutdown handlers itself.
func TestQuitRaisesNoShutdownEvent(t *testing.T) {
	t.Parallel()
	h := newEvHarness(t, "shutdown")
	h.update(ctrl('q'))
	for _, e := range h.rt.emitted {
		if e.Name == "shutdown" {
			t.Fatalf("quit raised %v", evRows(h.rt.emitted))
		}
	}
	h.m.Close()
	if h.rt.stops != 1 {
		t.Fatalf("Close stopped the runtime %d times, want 1", h.rt.stops)
	}
}

// BenchmarkPluginSnapshotAndTail measures the root's own cost per Update of the event
// machinery (snapshot, diff, tail) when nothing changes: with no runtime, with a runtime
// nobody subscribed on, and with every snapshot event wanted.
func BenchmarkPluginSnapshotAndTail(b *testing.B) {
	all := []string{"after_file_open", "buffer_closed", "buffer_activated", "buffer_deactivated", "language_changed",
		"buffer_modified", "buffer_changed", "cursor_moved", "selection_changed", "viewport_changed"}
	for _, bc := range []struct {
		name  string
		wants []string
		off   bool
	}{
		{"no runtime", nil, true},
		{"runtime, nobody listens", nil, false},
		{"runtime, every snapshot event wanted", all, false},
	} {
		b.Run(bc.name, func(b *testing.B) {
			h := newEvHarness(&testing.T{}, bc.wants...)
			if bc.off {
				h.m.plug.rt = nil
			}
			h.ed.doc = &fakeDoc{lines: []string{"hello", "wörld"}}
			b.ReportAllocs()
			for b.Loop() {
				snap := h.m.pluginSnapshot()
				h.m.pluginTail(snap)
				h.m.pluginViewTail(snap)
			}
		})
	}
}

// hostFocus sends a host focus message and delivers the pump tick it returns, so the
// event reaches the runtime through the deferred plugin pump rather than inside the
// focus Update.
func (h *evHarness) hostFocus(msg tea.Msg) []string {
	h.rt.emitted = nil
	_, cmd := h.m.Update(msg)
	for _, out := range deliver(cmd) {
		h.m.Update(out)
	}
	return evRows(h.rt.emitted)
}

// TestHostWindowFocusEventsAreDeduped covers the outer-terminal focus events: the
// initial focused state raises nothing, each real transition raises exactly one
// window_focused/window_blurred, and repeated FocusMsg/BlurMsg reports are ignored.
func TestHostWindowFocusEventsAreDeduped(t *testing.T) {
	h := newEvHarness(t, "window_focused", "window_blurred")
	wantRows(t, h.hostFocus(tea.FocusMsg{}), nil) // the model starts host-focused
	wantRows(t, h.hostFocus(tea.BlurMsg{}), []string{"window_blurred"})
	wantRows(t, h.hostFocus(tea.BlurMsg{}), nil)
	wantRows(t, h.hostFocus(tea.FocusMsg{}), []string{"window_focused"})
	wantRows(t, h.hostFocus(tea.FocusMsg{}), nil)
}

// TestHostWindowFocusDoesNotRaiseChromeFocusGained: window_focused/window_blurred are
// the host-window facts; the existing focus_gained stays the chrome/editor keyboard
// event and is not raised by a terminal focus change.
func TestHostWindowFocusDoesNotRaiseChromeFocusGained(t *testing.T) {
	h := newEvHarness(t, "window_focused", "window_blurred", "focus_gained")
	wantRows(t, h.hostFocus(tea.BlurMsg{}), []string{"window_blurred"})
	wantRows(t, h.hostFocus(tea.FocusMsg{}), []string{"window_focused"})
}

// TestHostWindowFocusEventsNeedASubscriber: with no plugin listening, the terminal focus
// changes queue nothing and raise nothing.
func TestHostWindowFocusEventsNeedASubscriber(t *testing.T) {
	h := newEvHarness(t)
	wantRows(t, h.hostFocus(tea.BlurMsg{}), nil)
	wantRows(t, h.hostFocus(tea.FocusMsg{}), nil)
}

// TestHostWindowFocusEventIsQueuedNotRunInline: the focus message only updates the
// dock/window state and schedules the pump tick; no runtime event is emitted inside
// that Update. Delivering the tick causes exactly one window_blurred.
func TestHostWindowFocusEventIsQueuedNotRunInline(t *testing.T) {
	h := newEvHarness(t, "window_blurred")
	h.rt.emitted = nil
	_, cmd := h.m.Update(tea.BlurMsg{})
	if len(h.rt.emitted) != 0 {
		t.Fatalf("blur ran the plugin event inline: %v", evRows(h.rt.emitted))
	}
	if cmd == nil {
		t.Fatal("blur did not schedule the plugin pump tick")
	}
	for _, out := range deliver(cmd) {
		h.m.Update(out)
	}
	wantRows(t, evRows(h.rt.emitted), []string{"window_blurred"})
}
