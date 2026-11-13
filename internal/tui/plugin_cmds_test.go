package tui

import (
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/buffer"
	commands "github.com/tbxark/gvim/commands"
	"github.com/tbxark/gvim/editor"
	"github.com/tbxark/gvim/internal/tui/menubar"
	"github.com/tbxark/gvim/internal/tui/palette"
	"github.com/tbxark/gvim/pluginapi"
)

// errEditor is a fake editor whose actions fail with err (for the actions in only, or
// for all when only is empty).
type errEditor struct {
	*fakeEditor
	err  error
	only []string
}

func (e *errEditor) Do(a editor.Action, args map[string]any) editor.Result {
	res := e.fakeEditor.Do(a, args)
	if len(e.only) == 0 || slices.Contains(e.only, string(a)) {
		res.Err = e.err
	}
	return res
}

// pluginKeysFor returns the test keymap with the key "k" bound to action.
func pluginKeysFor(action string) fakeKeymap {
	keys := testKeys()
	keys["normal|k|"] = Command{Action: action}
	return keys
}

// TestHostDoCollectsCmdIntoPending checks that the tea.Cmd an action returns when a
// plugin runs it through Host.Do is not dropped: it is in the result of the Update that
// started the plugin, from every entry point, and it does not stay in m.pending.
func TestHostDoCollectsCmdIntoPending(t *testing.T) {
	t.Parallel()
	clearScreen := reflect.TypeOf(tea.ClearScreen())
	tests := []struct {
		name  string
		start func(m *Model) tea.Cmd
	}{
		{"palette row", func(m *Model) tea.Cmd {
			_, cmd := m.Update(palette.RunCommandMsg{Action: "x.cmd"})
			return cmd
		}},
		{"key", func(m *Model) tea.Cmd {
			m.keys = pluginKeysFor("x.cmd")
			return press(m, typed("k"))
		}},
		{"menu row", func(m *Model) tea.Cmd {
			must(t, m.Run("x.cmd"))
			cmd := m.pending
			m.pending = nil
			return cmd
		}},
		{"a completion", func(m *Model) tea.Cmd {
			_, cmd := m.Update(pluginWorkMsg{c: pluginapi.Completion{Token: 1}})
			return cmd
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			rt := newFakeRuntime(t, "x.cmd")
			do := func() {
				if err := rt.host.Do("redraw_screen", nil, 1); err != nil {
					t.Error(err)
				}
			}
			rt.onRun = func(string, map[string]any) { do() }
			rt.onResolve = func(pluginapi.Completion) { do() }
			m, _ := newPluginModel(t, rt)
			m.Init()
			cmd := tt.start(m)
			var found bool
			for _, msg := range runCmd(cmd) {
				found = found || reflect.TypeOf(msg) == clearScreen
			}
			if !found {
				t.Fatal("the command redraw_screen returned is not in the result of the Update")
			}
			if m.pending != nil {
				t.Fatal("the command was also left in m.pending, where the next menu row would pick it up")
			}
		})
	}
}

// TestHostSaveDeferredVetoRunsAtDepthZero checks that Host.Save only queues the save: the
// before_file_save veto and the save itself run after the Lua slice returned, with the
// runtime free, and the wait is resolved when the save ended. A veto cancels it.
func TestHostSaveDeferredVetoRunsAtDepthZero(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		veto       func(pluginapi.Event) (bool, string)
		wantSaved  bool
		wantResult pluginapi.Completion
	}{
		{name: "allowed", wantSaved: true, wantResult: pluginapi.Completion{Token: 21, Values: []any{true}}},
		{
			name: "vetoed", veto: func(pluginapi.Event) (bool, string) { return true, "not today" },
			wantResult: pluginapi.Completion{Token: 21, Err: "cancelled: not today"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			rt := newFakeRuntime(t, "x.cmd")
			rt.strictSlice = true
			rt.wants["before_file_save"] = true
			rt.veto = tt.veto
			m, ed, pe := newPluginEditorModel(t, rt)
			rt.onRun = func(string, map[string]any) {
				if err := rt.host.Save(21, 1, ""); err != nil {
					t.Error(err)
				}
				if len(rt.vetoed) != 0 || len(ed.actions) != 0 {
					t.Errorf("the save ran inside the slice: vetoes %d, editor actions %v", len(rt.vetoed), ed.actions)
				}
			}
			m.Init()
			rt.log = nil
			must(t, m.Run("x.cmd"))

			if len(rt.vetoed) != 1 || rt.vetoDepth[0] {
				t.Fatalf("vetoes %d at depth flags %v, want one with InSlice false", len(rt.vetoed), rt.vetoDepth)
			}
			data := rt.vetoed[0].Data
			if rt.vetoed[0].Name != "before_file_save" || data["source"] != "api" || data["buf"] != pluginapi.BufferID(1) || data["path"] != "/w/a.txt" {
				t.Fatalf("veto event = %+v, want before_file_save from the api for buffer 1", rt.vetoed[0])
			}
			if saved := slices.Contains(actionsOf(ed), "save"); saved != tt.wantSaved {
				t.Fatalf("editor actions %v, saved = %v, want %v", actionsOf(ed), saved, tt.wantSaved)
			}
			if len(rt.resolved) != 1 || !reflect.DeepEqual(rt.resolved[0], tt.wantResult) {
				t.Fatalf("resolved %+v, want %+v", rt.resolved, tt.wantResult)
			}
			if len(pe.activations) != 0 {
				t.Fatalf("saving the active buffer activated %v", pe.activations)
			}
		})
	}
}

// TestHostSaveValidation checks what Host.Save refuses at once, without taking the token.
func TestHostSaveValidation(t *testing.T) {
	t.Parallel()
	rt := newFakeRuntime(t)
	m, _, pe := newPluginEditorModel(t, rt)
	pe.bufs = append(pe.bufs, pluginapi.BufferInfo{ID: 4, Name: "[No Name]"}, pluginapi.BufferInfo{ID: 5, Name: "*v*", Virtual: true})
	tests := []struct {
		name string
		id   pluginapi.BufferID
		path string
		want pluginapi.Code
	}{
		{"closed buffer", 99, "", pluginapi.CodeClosed},
		{"untitled without a path", 4, "", pluginapi.CodeInvalid},
		{"virtual buffer", 5, "x", pluginapi.CodeInvalid},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := m.plug.host.Save(30, tt.id, tt.path)
			if pluginapi.CodeOf(err) != tt.want {
				t.Fatalf("Save = %v, want code %s", err, tt.want)
			}
			if _, waiting := m.plug.waits[30]; waiting || len(m.plug.later) != 0 {
				t.Fatal("a refused Save took the token or queued work")
			}
		})
	}
}

// TestNoActionReachesRuntimeInSlice is the audit behind reentrantActions. Every editor
// action, every built-in command of the palette and the ui actions the palette does not
// list run through Host.Do while a slice is running, with a runtime that fails the test
// on Run, Resolve, Control, Veto, Drain and Stop. An action in reentrantActions, or one
// a plugin owns, must only be queued; any other must run at once, without touching the
// runtime. Then the queue runs with the runtime free. A new action that reaches the
// runtime fails here until it is added to reentrantActions.
func TestNoActionReachesRuntimeInSlice(t *testing.T) {
	t.Parallel()
	probe, _ := newPluginModel(t, newFakeRuntime(t))
	names := map[string]bool{"x.cmd": true}
	for _, a := range probe.plug.host.Actions() {
		names[a] = true
	}
	for _, a := range []string{
		"file_explorer_up", "file_explorer_down", "next_buffer", "prev_buffer", "stop_macro_recording",
		"prompt_record_macro", "no_such_action",
	} {
		names[a] = true
	}
	for a := range reentrantActions {
		names[a] = true
	}
	if len(names) < 200 {
		t.Fatalf("the audit covers only %d actions, the action list has shrunk", len(names))
	}
	for name := range names {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			rt := newFakeRuntime(t, "x.cmd")
			rt.strictSlice = true
			rt.inSlice = true
			rt.wants["before_file_save"] = true
			m, _ := newPluginModel(t, rt)
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("Do(%q) panicked: %v", name, r)
				}
			}()
			err := m.plug.host.Do(name, nil, 1)
			deferred := len(m.plug.later) == 1
			if len(rt.log) != 0 {
				t.Fatalf("Do(%q) called the runtime inside a slice: %v", name, rt.log)
			}
			if want := reentrantActions[name] || name == "x.cmd"; deferred != want {
				t.Fatalf("Do(%q) deferred = %v, want %v (err %v)", name, deferred, want, err)
			}
			if deferred && err != nil {
				t.Fatalf("a deferred Do(%q) returned %v", name, err)
			}
			rt.inSlice = false
			m.pluginPump() // the queue runs with the runtime free; the fake allows every call now
			if len(m.plug.later) != 0 {
				t.Fatalf("%q left %d closures after the pump", name, len(m.plug.later))
			}
		})
	}
}

// TestMacroRecordsOnlyOutermostPluginCommand checks C8: a plugin command that runs
// actions through Host.Do is recorded as the one command the user issued, not once more
// for each action it ran, and a replay re-runs the command once.
func TestMacroRecordsOnlyOutermostPluginCommand(t *testing.T) {
	t.Parallel()
	rt := newFakeRuntime(t, "x.cmd")
	rt.onRun = func(string, map[string]any) {
		for range 2 {
			if err := rt.host.Do("delete_line", nil, 1); err != nil {
				t.Error(err)
			}
		}
	}
	m, ed := newPluginModel(t, rt)
	m.Init()
	m.keys = pluginKeysFor("x.cmd")
	must(t, m.Run("prompt_record_macro"))
	press(m, typed("k"))
	must(t, m.Run("stop_macro_recording"))

	var steps []string
	for _, s := range m.root.macro.last {
		steps = append(steps, s.cmd.Action)
	}
	if want := []string{"x.cmd"}; !slices.Equal(steps, want) {
		t.Fatalf("recorded steps = %v, want %v", steps, want)
	}
	if got := actionsOf(ed); !slices.Equal(got, []string{"delete_line", "delete_line"}) {
		t.Fatalf("recording ran %v, want two delete_line from one execution", got)
	}

	ed.actions, ed.args = nil, nil
	must(t, m.Run("play_last_macro"))
	if got := actionsOf(ed); !slices.Equal(got, []string{"delete_line", "delete_line"}) {
		t.Fatalf("playback ran %v, want one execution (two delete_line)", got)
	}
	if got := slices.Compact(slices.Clone(rt.runs)); !slices.Equal(got, []string{"x.cmd"}) || len(rt.runs) != 2 {
		t.Fatalf("the runtime ran %v, want x.cmd once for the recording and once for the playback", rt.runs)
	}
	if len(m.root.macro.cmds) != 0 {
		t.Fatalf("the playback recorded %d commands", len(m.root.macro.cmds))
	}
}

// TestMacroSkipsCommandsThatAskNotToBeRecorded checks CommandInfo.Record: a command that
// set record=false is not in the recording even as the outermost command.
func TestMacroSkipsCommandsThatAskNotToBeRecorded(t *testing.T) {
	t.Parallel()
	rt := newFakeRuntime(t, "x.rec", "x.norec")
	m, _ := newPluginModel(t, rt)
	m.Init()
	m.plug.host.SetCommands("x", []pluginapi.CommandInfo{
		{ID: "x.rec", Plugin: "x", Record: true},
		{ID: "x.norec", Plugin: "x", Record: false},
	})
	must(t, m.Run("prompt_record_macro"))
	must(t, m.Run("x.rec"))
	must(t, m.Run("x.norec"))
	must(t, m.Run("x.unregistered"))
	must(t, m.Run("stop_macro_recording"))
	var steps []string
	for _, s := range m.root.macro.last {
		steps = append(steps, s.cmd.Action)
	}
	if want := []string{"x.rec", "x.unregistered"}; !slices.Equal(steps, want) {
		t.Fatalf("recorded steps = %v, want %v", steps, want)
	}
}

// TestPluginActionConsultedBeforeEditor checks that the runtime's Owns is asked before
// the editor from every entry point, with the right command source, and that a name
// nobody owns still reaches the editor and fails as unknown.
func TestPluginActionConsultedBeforeEditor(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		action  string
		start   func(m *Model)
		wantRun []string
		wantSrc pluginapi.CommandSource
	}{
		{"palette", "x.cmd", func(m *Model) { m.Update(palette.RunCommandMsg{Action: "x.cmd"}) }, []string{"x.cmd"}, srcPalette},
		{"key", "x.cmd", func(m *Model) { m.keys = pluginKeysFor("x.cmd"); press(m, typed("k")) }, []string{"x.cmd"}, srcKey},
		{"menu", "x.cmd", func(m *Model) { must(t, m.Run("x.cmd")) }, []string{"x.cmd"}, srcMenu},
		{"typo through the menu", "x.typo", func(m *Model) {
			if err := m.Run("x.typo"); !errors.Is(err, menubar.ErrUnsupported) {
				t.Errorf("Run(typo) = %v, want ErrUnsupported", err)
			}
		}, nil, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			rt := newFakeRuntime(t, "x.cmd")
			ed := newFakeEditor("x")
			ed.quitAction = "quit"
			unknown := &errEditor{fakeEditor: ed, err: fmt.Errorf("%w: x.typo", editor.ErrUnknownAction)}
			m := New(Deps{Editor: unknown, Keys: testKeys(), Clock: testClock, Root: RootPorts{Plugins: rt}})
			m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
			useHost(rt, m)
			tt.start(m)
			if !slices.Equal(rt.runs, tt.wantRun) {
				t.Fatalf("the runtime ran %v, want %v", rt.runs, tt.wantRun)
			}
			if tt.wantRun != nil {
				if rt.runSrc[0] != tt.wantSrc {
					t.Fatalf("source = %q, want %q", rt.runSrc[0], tt.wantSrc)
				}
				if len(ed.actions) != 0 {
					t.Fatalf("the editor also ran %v", actionsOf(ed))
				}
			}
			if m.plug.src != "" {
				t.Fatalf("the command source %q was left set", m.plug.src)
			}
		})
	}
}

// TestPluginRunRefusalIsTheNotice checks that a command the runtime refuses to start shows
// its reason.
func TestPluginRunRefusalIsTheNotice(t *testing.T) {
	t.Parallel()
	rt := newFakeRuntime(t, "x.cmd")
	rt.runErr = pluginapi.Errf(pluginapi.CodeBusy, "plugin 'x' is suspended")
	m, _ := newPluginModel(t, rt)
	must(t, m.Run("x.cmd"))
	if m.notice != "x.cmd: busy: plugin 'x' is suspended" {
		t.Fatalf("notice = %q", m.notice)
	}
}

// TestPluginCommandWhenGatesTheRun checks the when-clause of a registered command: it is
// evaluated when the command is invoked, and a failing one does nothing and says so.
func TestPluginCommandWhenGatesTheRun(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		when       []string
		title      string
		wantRun    bool
		wantNotice string
	}{
		{name: "no when", wantRun: true},
		{name: "holds", when: []string{"has_buffer"}, wantRun: true},
		{name: "negated holds", when: []string{"!modified"}, wantRun: true},
		{name: "fails", when: []string{"modified"}, title: "Git: Blame", wantNotice: "Git: Blame is not available now"},
		{name: "one of two fails", when: []string{"has_buffer", "large"}, wantNotice: "cmd is not available now"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			rt := newFakeRuntime(t, "x.cmd")
			m, _ := newPluginModel(t, rt)
			m.plug.host.SetCommands("x", []pluginapi.CommandInfo{{ID: "x.cmd", Title: tt.title, Plugin: "x", When: tt.when, Record: true}})
			must(t, m.Run("x.cmd"))
			if got := len(rt.runs) == 1; got != tt.wantRun {
				t.Fatalf("ran = %v, want %v", got, tt.wantRun)
			}
			if m.notice != tt.wantNotice {
				t.Fatalf("notice = %q, want %q", m.notice, tt.wantNotice)
			}
		})
	}
}

// TestInSliceHookDefersInsteadOfRunning checks the guard in pluginAction: reached while a
// slice runs (which no audited action does), the command waits for the pump.
func TestInSliceHookDefersInsteadOfRunning(t *testing.T) {
	t.Parallel()
	rt := newFakeRuntime(t, "x.cmd")
	rt.strictSlice = true
	rt.inSlice = true
	m, _ := newPluginModel(t, rt)
	if cmd, ok := m.pluginAction("x.cmd", nil); cmd != nil || !ok {
		t.Fatalf("pluginAction = (%v, %v), want the action taken and queued", cmd != nil, ok)
	}
	if len(rt.log) != 0 || len(m.plug.later) != 1 {
		t.Fatalf("log %v, queue %d: want nothing run and one closure queued", rt.log, len(m.plug.later))
	}
	rt.inSlice = false
	m.pluginPump()
	if !slices.Equal(rt.runs, []string{"x.cmd"}) {
		t.Fatalf("the queued command ran %v", rt.runs)
	}
}

// TestAllCommandsListsPluginRows checks the palette table: the built-in rows come
// first and unchanged, the plugin rows follow in registration order, a hidden command
// and one whose when-clause fails are not listed, a registration replaces the plugin's
// earlier rows, and OwnerGone removes them.
func TestAllCommandsListsPluginRows(t *testing.T) {
	t.Parallel()
	rt := newFakeRuntime(t)
	m, _ := newPluginModel(t, rt)
	host := m.plug.host
	builtin := len(m.commands)
	if builtin == 0 {
		t.Fatal("no built-in commands")
	}
	host.SetCommands("b", []pluginapi.CommandInfo{{ID: "b.one", Title: "B: One", Plugin: "b", Record: true}})
	host.SetCommands("a", []pluginapi.CommandInfo{
		{ID: "a.show", Title: "A: Show", Description: "shows", Plugin: "a", When: []string{"has_buffer"}},
		{ID: "a.hidden", Title: "A: Hidden", Plugin: "a", Hidden: true},
		{ID: "a.never", Title: "A: Never", Plugin: "a", When: []string{"large"}},
		{ID: "a.notitle", Plugin: "a"},
	})

	rows := func() []commands.Entry { return m.allCommands()[builtin:] }
	got := rows()
	var ids []string
	for _, e := range got {
		ids = append(ids, e.Action)
		if e.Source != commands.SourcePlugin {
			t.Fatalf("row %q has source %q", e.Action, e.Source)
		}
	}
	if want := []string{"b.one", "a.show", "a.notitle"}; !slices.Equal(ids, want) {
		t.Fatalf("plugin rows = %v, want %v", ids, want)
	}
	if got[1].Name != "A: Show" || got[1].Description != "shows" || !slices.Equal(got[1].When, []string{"has_buffer"}) {
		t.Fatalf("row a.show = %+v", got[1])
	}
	if got[2].Name != "notitle" {
		t.Fatalf("a command with no title is listed as %q, want its name", got[2].Name)
	}
	if !reflect.DeepEqual(m.allCommands()[:builtin], m.commands) {
		t.Fatal("the built-in rows changed")
	}

	host.SetCommands("a", []pluginapi.CommandInfo{{ID: "a.only", Title: "A: Only", Plugin: "a"}})
	if ids = actionsOfRows(rows()); !slices.Equal(ids, []string{"b.one", "a.only"}) {
		t.Fatalf("after a replace: %v, want b.one then a.only (the order the plugins registered in)", ids)
	}
	host.OwnerGone("b", 1)
	if ids = actionsOfRows(rows()); !slices.Equal(ids, []string{"a.only"}) {
		t.Fatalf("after OwnerGone(b): %v", ids)
	}
	host.SetCommands("a", nil)
	if len(m.allCommands()) != builtin || len(m.plug.order) != 0 {
		t.Fatalf("an empty registration left %d rows and order %v", len(m.allCommands())-builtin, m.plug.order)
	}
}

func actionsOfRows(rows []commands.Entry) []string {
	var out []string
	for _, e := range rows {
		out = append(out, e.Action)
	}
	return out
}

// kindLabels gives the primary binding of any action as "<ctrl+>action".
type kindLabels struct{ fakeKeymap }

func (kindLabels) Label(action string) string { return "^" + action }

// TestPaletteOpensWithPluginRows checks that the palette overlay is built from
// allCommands, with the key label the keymap gives.
func TestPaletteOpensWithPluginRows(t *testing.T) {
	t.Parallel()
	rt := newFakeRuntime(t)
	m, _ := newPluginModel(t, rt)
	m.keys = kindLabels{testKeys()}
	m.plug.host.SetCommands("x", []pluginapi.CommandInfo{{ID: "x.go", Title: "X: Go", Plugin: "x"}})
	var row commands.Entry
	for _, e := range m.allCommands() {
		if e.Action == "x.go" {
			row = e
		}
	}
	if row.Keybinding != "^x.go" {
		t.Fatalf("row = %+v, want the keymap's label", row)
	}
	if got := m.plug.host.KeyLabel("x.go"); got != "^x.go" {
		t.Fatalf("KeyLabel = %q", got)
	}
	if got := newPluginHostWithoutLabels(t).KeyLabel("x.go"); got != "" {
		t.Fatalf("KeyLabel without a labelling keymap = %q, want empty", got)
	}
	m.openPalette(">")
	if len(m.overlays) != 1 {
		t.Fatalf("overlays = %d, want the palette", len(m.overlays))
	}
}

func newPluginHostWithoutLabels(t *testing.T) *pluginHost {
	m, _ := newPluginModel(t, newFakeRuntime(t))
	return m.plug.host
}

// lockedDoc is a document with the read-only, modified, large and language states.
type lockedDoc struct {
	*fakeDoc
	name            string
	ro, mod, large_ bool
}

func (d lockedDoc) Name() string            { return d.name }
func (d lockedDoc) ReadOnly() bool          { return d.ro }
func (d lockedDoc) Modified() bool          { return d.mod }
func (d lockedDoc) Large() bool             { return d.large_ }
func (d lockedDoc) Index() buffer.IndexInfo { return buffer.IndexInfo{} }

// holdsDocEditor shows doc as the active document.
type holdsDocEditor struct {
	*fakeEditor
	doc Document
}

func (e holdsDocEditor) Active() Document { return e.doc }

// TestHoldsAnswersPluginClauses checks the when-clause names S5 adds to Holds.
func TestHoldsAnswersPluginClauses(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		doc  Document
		key  string
		want bool
	}{
		{"read_only on", lockedDoc{fakeDoc: &fakeDoc{}, name: "a.go", ro: true}, "read_only", true},
		{"read_only off", lockedDoc{fakeDoc: &fakeDoc{}, name: "a.go"}, "read_only", false},
		{"read_only on a document that cannot say", &fakeDoc{}, "read_only", false},
		{"modified on", lockedDoc{fakeDoc: &fakeDoc{}, name: "a.go", mod: true}, "modified", true},
		{"modified off", lockedDoc{fakeDoc: &fakeDoc{}, name: "a.go"}, "modified", false},
		{"large on", lockedDoc{fakeDoc: &fakeDoc{}, name: "a.go", large_: true}, "large", true},
		{"large off", lockedDoc{fakeDoc: &fakeDoc{}, name: "a.go"}, "large", false},
		{"language match", lockedDoc{fakeDoc: &fakeDoc{}, name: "main.go"}, "language:go", true},
		{"language match ignores case", lockedDoc{fakeDoc: &fakeDoc{}, name: "main.go"}, "language:Go", true},
		{"language mismatch", lockedDoc{fakeDoc: &fakeDoc{}, name: "main.go"}, "language:rust", false},
		{"no document", nil, "modified", false},
		{"no document and a language", nil, "language:go", false},
		{"unknown name", &fakeDoc{}, "no_such_context", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ed := newFakeEditor("x")
			m := New(Deps{Editor: holdsDocEditor{ed, tt.doc}, Keys: testKeys(), Clock: testClock})
			if got := m.Holds(tt.key); got != tt.want {
				t.Fatalf("Holds(%q) = %v, want %v", tt.key, got, tt.want)
			}
			// The host answers the same, and a leading "!" negates in a when-clause.
			if got := m.whenHolds([]string{"!" + tt.key}); got == tt.want {
				t.Fatalf("whenHolds(!%s) = %v, want the opposite of Holds", tt.key, got)
			}
		})
	}
}

// TestDoRepeatsAndMapsErrors checks Host.Do on a plain editor action: the count repeats
// it, a failure stops the repeat, and the editor's error comes back with the code a
// plugin sees.
func TestDoRepeatsAndMapsErrors(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		err       error
		count     int
		wantCode  pluginapi.Code
		wantCalls int
	}{
		{"three times", nil, 3, "", 3},
		{"count 0 is once", nil, 0, "", 1},
		{"unknown action", fmt.Errorf("%w: nope", editor.ErrUnknownAction), 3, pluginapi.CodeUnsupported, 1},
		{"read-only", editor.ErrReadOnly, 2, pluginapi.CodeReadOnly, 1},
		{"large file", editor.ErrLargeFile, 1, pluginapi.CodeTooLarge, 1},
		{"unsaved", editor.ErrUnsaved, 1, pluginapi.CodeConflict, 1},
		{"no document", editor.ErrNoDocument, 1, pluginapi.CodeNotFound, 1},
		{"anything else", errors.New("boom"), 1, pluginapi.CodeInvalid, 1},
		{"too many", nil, maxDoCount + 1, pluginapi.CodeLimit, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ed := newFakeEditor("x")
			ee := &errEditor{fakeEditor: ed, err: tt.err}
			rt := newFakeRuntime(t)
			m := New(Deps{Editor: ee, Keys: testKeys(), Clock: testClock, Root: RootPorts{Plugins: rt}})
			m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
			useHost(rt, m)
			err := m.plug.host.Do("delete_line", nil, tt.count)
			if tt.wantCode == "" {
				if err != nil {
					t.Fatalf("Do = %v", err)
				}
			} else if pluginapi.CodeOf(err) != tt.wantCode {
				t.Fatalf("Do = %v, want code %q", err, tt.wantCode)
			}
			if got := len(ed.actions); got != tt.wantCalls {
				t.Fatalf("the editor ran the action %d times, want %d", got, tt.wantCalls)
			}
		})
	}
	m, _ := newPluginModel(t, newFakeRuntime(t))
	if err := m.plug.host.Do("", nil, 1); pluginapi.CodeOf(err) != pluginapi.CodeInvalid {
		t.Fatalf("Do(\"\") = %v, want invalid", err)
	}
}

// TestDoAllGroupsStopsAndDefers checks Host.DoAll: the steps run in one undo group, in
// order with their counts, and stop at the first error; a list with a reentrant step is
// deferred whole and runs in order after the slice.
func TestDoAllGroupsStopsAndDefers(t *testing.T) {
	t.Parallel()
	t.Run("one group in order with counts", func(t *testing.T) {
		t.Parallel()
		m, ed, pe := newPluginEditorModel(t, newFakeRuntime(t))
		err := m.plug.host.DoAll([]pluginapi.Step{{Action: "move_down", Count: 2}, {Action: "delete_line"}})
		if err != nil {
			t.Fatal(err)
		}
		if got := actionsOf(ed); !slices.Equal(got, []string{"move_down", "move_down", "delete_line"}) {
			t.Fatalf("ran %v", got)
		}
		if pe.groups != 1 {
			t.Fatalf("%d undo groups, want 1", pe.groups)
		}
	})
	t.Run("stops at the first error", func(t *testing.T) {
		t.Parallel()
		ed := newFakeEditor("x")
		ee := &errEditor{fakeEditor: ed, err: editor.ErrReadOnly, only: []string{"delete_line"}}
		rt := newFakeRuntime(t)
		m := New(Deps{Editor: ee, Keys: testKeys(), Clock: testClock, Root: RootPorts{Plugins: rt}})
		m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
		useHost(rt, m)
		err := m.plug.host.DoAll([]pluginapi.Step{{Action: "move_down"}, {Action: "delete_line"}, {Action: "move_up"}})
		if pluginapi.CodeOf(err) != pluginapi.CodeReadOnly {
			t.Fatalf("DoAll = %v, want read_only", err)
		}
		if got := actionsOf(ed); !slices.Equal(got, []string{"move_down", "delete_line"}) {
			t.Fatalf("ran %v, want the third step skipped", got)
		}
	})
	t.Run("a reentrant step defers the list", func(t *testing.T) {
		t.Parallel()
		rt := newFakeRuntime(t, "x.cmd")
		rt.strictSlice = true
		m, ed := newPluginModel(t, rt)
		first := true
		rt.onRun = func(string, map[string]any) {
			if !first {
				return
			}
			first = false
			err := rt.host.DoAll([]pluginapi.Step{{Action: "move_down"}, {Action: "x.cmd", Count: 2, Args: map[string]any{"a": 1}}, {Action: "move_up"}})
			if err != nil {
				t.Error(err)
			}
			if len(ed.actions) != 0 {
				t.Errorf("a step ran inside the slice: %v", actionsOf(ed))
			}
		}
		rt.onDrain = nil
		must(t, m.Run("x.cmd"))
		if got := actionsOf(ed); !slices.Equal(got, []string{"move_down", "move_up"}) {
			t.Fatalf("ran %v", got)
		}
		if len(rt.runs) != 2 || rt.runSrc[1] != srcAPI || rt.runArgs[1]["count"] != 2 || rt.runArgs[1]["a"] != 1 {
			t.Fatalf("runs %v args %v src %v, want the nested x.cmd as an api command with count 2", rt.runs, rt.runArgs, rt.runSrc)
		}
	})
	t.Run("empty action name", func(t *testing.T) {
		t.Parallel()
		m, _ := newPluginModel(t, newFakeRuntime(t))
		if err := m.plug.host.DoAll([]pluginapi.Step{{Action: ""}}); pluginapi.CodeOf(err) != pluginapi.CodeInvalid {
			t.Fatalf("DoAll = %v, want invalid", err)
		}
	})
}

// TestDeferredDoPassesCountAndSource checks that a plugin command run through Host.Do
// gets the count as args.count, the api source, and a copy of the arguments.
func TestDeferredDoPassesCountAndSource(t *testing.T) {
	t.Parallel()
	rt := newFakeRuntime(t, "x.cmd", "y.cmd")
	m, _ := newPluginModel(t, rt)
	args := map[string]any{"k": "v"}
	rt.onRun = func(action string, _ map[string]any) {
		if action != "x.cmd" {
			return
		}
		if err := rt.host.Do("y.cmd", args, 3); err != nil {
			t.Error(err)
		}
		args["k"] = "changed after the call"
	}
	must(t, m.Run("x.cmd"))
	if len(rt.runs) != 2 {
		t.Fatalf("runs = %v", rt.runs)
	}
	got := rt.runArgs[1]
	if got["k"] != "v" || got["count"] != 3 || rt.runSrc[1] != srcAPI {
		t.Fatalf("y.cmd got args %v from %q, want k=v count=3 from api", got, rt.runSrc[1])
	}
	if _, polluted := args["count"]; polluted {
		t.Fatal("Do added count to the caller's map")
	}
}

// TestQueuedActionFailureIsTheNotice checks that an action Do deferred and that failed
// later is reported: the plugin that asked is no longer on the stack.
func TestQueuedActionFailureIsTheNotice(t *testing.T) {
	t.Parallel()
	rt := newFakeRuntime(t, "x.cmd")
	ed := newFakeEditor("x")
	// Every reentrant action has a handler, so the action is deferred the other way: while a prompt
	// validator runs (plug.ui.busy) every action waits for the pump, and the editor refuses
	// this one as unknown.
	ee := &errEditor{fakeEditor: ed, err: editor.ErrUnknownAction, only: []string{"move_left"}}
	m := New(Deps{Editor: ee, Keys: testKeys(), Clock: testClock, Root: RootPorts{Plugins: rt}})
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	useHost(rt, m)
	rt.onRun = func(string, map[string]any) {
		m.plug.ui.busy++
		defer func() { m.plug.ui.busy-- }()
		if err := rt.host.Do("move_left", nil, 1); err != nil {
			t.Error(err)
		}
	}
	must(t, m.Run("x.cmd"))
	if !strings.Contains(m.notice, "move_left") || !strings.Contains(m.notice, "unsupported") {
		t.Fatalf("notice = %q, want the failed queued action", m.notice)
	}
}

// TestOpenPaletteIsDeferred checks that Host.OpenPalette opens the palette after the slice.
func TestOpenPaletteIsDeferred(t *testing.T) {
	t.Parallel()
	rt := newFakeRuntime(t, "x.cmd")
	m, _ := newPluginModel(t, rt)
	rt.onRun = func(string, map[string]any) {
		rt.host.(pluginapi.CommandHost).OpenPalette("#")
		if len(m.overlays) != 0 {
			t.Error("the palette opened inside the slice")
		}
	}
	must(t, m.Run("x.cmd"))
	if len(m.overlays) != 1 {
		t.Fatalf("overlays = %d, want the palette", len(m.overlays))
	}
}

// TestActionsListsEditorAndBuiltinNames checks Host.Actions.
func TestActionsListsEditorAndBuiltinNames(t *testing.T) {
	t.Parallel()
	m, _ := newPluginModel(t, newFakeRuntime(t))
	got := m.plug.host.Actions()
	if !slices.IsSorted(got) || len(slices.Compact(slices.Clone(got))) != len(got) {
		t.Fatal("the action list is not sorted and unique")
	}
	for _, want := range []string{"delete_line", "move_down", "save", "quit", "command_palette", "plugin_reload_all", "toggle_file_explorer"} {
		if _, found := slices.BinarySearch(got, want); !found {
			t.Errorf("action %q is missing", want)
		}
	}
}

// TestSetKeymapReplacesTheKeymap checks that SetKeymap changes what the next key press
// resolves to, so a rebuilt keymap with plugin bindings takes effect.
func TestSetKeymapReplacesTheKeymap(t *testing.T) {
	t.Parallel()
	rt := newFakeRuntime(t, "x.cmd")
	m, ed := newPluginModel(t, rt)
	press(m, typed("k"))
	if len(rt.runs) != 0 || len(ed.actions) != 1 || ed.actions[0] != "insert_char" {
		t.Fatalf("before: runs %v, actions %v, want the key typed", rt.runs, actionsOf(ed))
	}
	m.SetKeymap(pluginKeysFor("x.cmd"))
	press(m, typed("k"))
	if want := []string{"x.cmd"}; !slices.Equal(rt.runs, want) {
		t.Fatalf("after: runs %v, want %v", rt.runs, want)
	}
}

// TestDoForPluginSkipsThePluginHookAndRecording checks the two things doForPlugin is
// dispatch without: it never starts a plugin command (a Lua call must not run Lua), and
// it never records into a macro. The counters are balanced afterwards.
func TestDoForPluginSkipsThePluginHookAndRecording(t *testing.T) {
	t.Parallel()
	rt := newFakeRuntime(t, "x.cmd")
	rt.strictSlice = true
	rt.inSlice = true
	m, ed := newPluginModel(t, rt)
	m.root.macro.recording = true
	if _, err := m.doForPlugin("x.cmd", nil, 1); err != nil {
		t.Fatal(err)
	}
	if len(rt.log) != 0 {
		t.Fatalf("the runtime was called: %v", rt.log)
	}
	if got := actionsOf(ed); len(got) != 1 || got[0] != "x.cmd" {
		t.Fatalf("editor actions = %v, want the name passed on as a plain action", got)
	}
	if len(m.root.macro.cmds) != 0 {
		t.Fatalf("recorded %d commands", len(m.root.macro.cmds))
	}
	if m.plug.quiet != 0 || m.plug.direct != 0 {
		t.Fatalf("counters left at quiet %d, direct %d", m.plug.quiet, m.plug.direct)
	}
}

// TestRecordCommandIgnoresWhileQuiet checks the recorder guard of C8 directly: nothing is
// recorded while a plugin works, and the recorder works again afterwards.
func TestRecordCommandIgnoresWhileQuiet(t *testing.T) {
	t.Parallel()
	m, _ := newPluginModel(t, newFakeRuntime(t))
	m.root.macro.recording = true
	m.plug.quiet = 1
	m.recordCommand(Command{Action: "delete_line"})
	if len(m.root.macro.cmds) != 0 {
		t.Fatal("a command was recorded while quiet")
	}
	m.plug.quiet = 0
	m.recordCommand(Command{Action: "delete_line"})
	if len(m.root.macro.cmds) != 1 {
		t.Fatal("a command was not recorded once the plugin was done")
	}
}

// TestDoStopsWhenTheTimeBudgetIsSpent checks that the repeat count is not the only bound on
// Host.Do and DoAll: an action repeated into a spent budget stops, reports a timeout and
// says how far it got, and a step that follows a spent budget does not start. The first
// repetition always runs, so a call makes progress however small the budget is.
func TestDoStopsWhenTheTimeBudgetIsSpent(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		run       func(h *pluginHost) error
		wantCalls int
	}{
		{"one action", func(h *pluginHost) error { return h.Do("delete_line", nil, 1000) }, 1},
		{"repeated step", func(h *pluginHost) error {
			return h.DoAll([]pluginapi.Step{{Action: "delete_line", Count: 5}, {Action: "move_down", Count: 5}})
		}, 1},
		{"single steps", func(h *pluginHost) error {
			return h.DoAll([]pluginapi.Step{{Action: "delete_line"}, {Action: "move_down"}, {Action: "move_up"}})
		}, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			m, ed := newPluginModel(t, newFakeRuntime(t))
			m.plug.doBudget = -time.Second
			err := tt.run(m.plug.host)
			if pluginapi.CodeOf(err) != pluginapi.CodeTimeout {
				t.Fatalf("error = %v, want code %q", err, pluginapi.CodeTimeout)
			}
			if got := len(ed.actions); got != tt.wantCalls {
				t.Fatalf("the editor ran %d actions, want %d", got, tt.wantCalls)
			}
		})
	}
	m, ed := newPluginModel(t, newFakeRuntime(t))
	m.plug.doBudget = time.Hour
	if err := m.plug.host.Do("delete_line", nil, 1000); err != nil {
		t.Fatalf("Do within the budget = %v", err)
	}
	if got := len(ed.actions); got != 1000 {
		t.Fatalf("the editor ran %d actions, want 1000", got)
	}
}

// TestPluginDoRunsHexAndLineTargetActions checks that an action a plugin runs through
// Do takes the same routes as the user's key: the hex dump consumes navigation, and Enter
// on a line target opens the target.
func TestPluginDoRunsHexAndLineTargetActions(t *testing.T) {
	t.Run("hex navigation", func(t *testing.T) {
		m, ed, _ := newHexModel(t, make([]byte, hexBytesPerRow*200))
		m.toggleHexView()
		ed.actions = nil
		if _, err := m.doForPlugin("move_down", nil, 1); err != nil {
			t.Fatal(err)
		}
		if len(ed.actions) != 0 {
			t.Fatalf("hex navigation reached the editor: %v", ed.actions)
		}
	})
	t.Run("line target", func(t *testing.T) {
		m, ed, _ := targetModel(t)
		if err := m.plug.host.SetLineTargets("p", 1, []pluginapi.LineTarget{{Line: 0, Path: "dir/target.go", Target: 5}}); err != nil {
			t.Fatal(err)
		}
		m.ed.SetCursor(buffer.Pos{Line: 0, Col: 0}, false)
		cmd, err := m.doForPlugin("insert_newline", nil, 1)
		if err != nil {
			t.Fatal(err)
		}
		if cmd != nil {
			cmd()
		}
		if len(ed.opened) != 1 {
			t.Fatalf("opened = %v, want the line target", ed.opened)
		}
	})
}
