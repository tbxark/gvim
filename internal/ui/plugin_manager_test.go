package ui

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/internal/pluginapi"
	"github.com/tbxark/gvim/internal/theme"
	"github.com/tbxark/gvim/internal/ui/popup"
)

// mgrRuntime is the fake runtime with a log, an init.lua path and the --no-plugins flag:
// the optional parts of the runtime that the manager reads.
type mgrRuntime struct {
	*fakeRuntime
	entries   []pluginapi.LogEntry
	initFile  string
	noPlugins bool
}

func (r *mgrRuntime) Log() []pluginapi.LogEntry { return r.entries }
func (r *mgrRuntime) InitFile() string          { return r.initFile }
func (r *mgrRuntime) NoPlugins() bool           { return r.noPlugins }

// mgrEditor is a fake editor that records the files it is asked to open.
type mgrEditor struct {
	*fakeEditor
	opened []string
}

func (e *mgrEditor) OpenFile(path string) error {
	e.opened = append(e.opened, path)
	return nil
}
func (e *mgrEditor) OpenPreview(path string) error { return e.OpenFile(path) }
func (e *mgrEditor) GotoLine(int, int) error       { return nil }

// newMgrModel returns a sized model over ed with rt as its plugin runtime. A nil rt is
// a session without plugins.
func newMgrModel(t *testing.T, ed EditorState, rt pluginapi.Runtime) *Model {
	t.Helper()
	m := New(Deps{Editor: ed, Keys: testKeys(), Theme: theme.Theme{}, Clock: testClock, Root: RootPorts{Plugins: rt}})
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	return m
}

func newMgrFixture(t *testing.T, plugins ...pluginapi.PluginInfo) (*Model, *mgrRuntime, *mgrEditor) {
	t.Helper()
	rt := &mgrRuntime{fakeRuntime: newFakeRuntime(t)}
	rt.plugins = plugins
	ed := &mgrEditor{fakeEditor: newFakeEditor("hello", "world")}
	ed.quitAction = "quit"
	return newMgrModel(t, ed, rt), rt, ed
}

// frameString is the whole frame, overlays included, as one string.
func frameString(m *Model) string { return strings.Join(frameText(m), "\n") }

// run runs the palette action and feeds back the commands it starts.
func runPaletteAction(m *Model, action string) {
	feed(m, rootPaletteRun(m, action))
}

var samplePlugins = []pluginapi.PluginInfo{
	{Name: "git_gutter", Version: "1.0.0", Source: "bundled", Status: "loaded", Commands: 2, Subscriptions: 5, Enabled: true},
	{Name: "hello", Source: "user", Status: "failed", Err: "hello.lua:3: boom\nstack traceback", Enabled: true, Dir: "/cfg/plugins"},
	{Name: "flash", Source: "bundled", Status: "disabled", Locked: "set in config"},
	{Name: "old.js", Source: "user", Status: "legacy", Locked: "JavaScript plugins are not supported", Dir: "/cfg/plugins"},
}

// TestManagerItems checks the columns and the detail of the manager's rows: running
// plugins show their counts, a locked plugin says why, a legacy file shows the reason it
// is not run, and the first line of an error is shown with control characters cleaned.
func TestManagerItems(t *testing.T) {
	t.Parallel()
	items := managerItems(samplePlugins)
	tests := []struct {
		row    int
		text   string
		detail string
	}{
		{0, "git_gutter  1.0.0  bundled  loaded", "(2 cmds, 5 subs)"},
		{1, "hello       -      user     failed", "hello.lua:3: boom"},
		{2, "flash       -      bundled  disabled", "locked: set in config"},
		{3, "old.js      -      user     legacy", "JavaScript plugins are not supported"},
	}
	if len(items) != len(tests) {
		t.Fatalf("%d items, want %d", len(items), len(tests))
	}
	for _, tt := range tests {
		it := items[tt.row]
		if it.ID != tt.row || it.Text != tt.text || it.Detail != tt.detail {
			t.Errorf("row %d = {%d %q %q}, want {%d %q %q}", tt.row, it.ID, it.Text, it.Detail, tt.row, tt.text, tt.detail)
		}
	}
	odd := managerItems([]pluginapi.PluginInfo{{Name: "a\x1b[31mb", Status: "failed", Err: "x\x1b]0;y\a"}})
	if strings.ContainsAny(odd[0].Text+odd[0].Detail, "\x1b\a") {
		t.Errorf("control characters reach the picker: %q %q", odd[0].Text, odd[0].Detail)
	}
}

// TestManagerChoices lists the actions each kind of row allows.
func TestManagerChoices(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		in   pluginapi.PluginInfo
		want []string
	}{
		{"bundled and loaded", pluginapi.PluginInfo{Name: "a", Source: "bundled", Status: "loaded"}, []string{"Reload", "Disable", "Show Log"}},
		{"user and failed", pluginapi.PluginInfo{Name: "a", Source: "user", Status: "failed", Err: "x"}, []string{"Reload", "Disable", "Show Log", "Open Source", "Copy Error"}},
		{"user and disabled", pluginapi.PluginInfo{Name: "a", Source: "user", Status: "disabled"}, []string{"Enable", "Show Log", "Open Source"}},
		{"suspended", pluginapi.PluginInfo{Name: "a", Source: "user", Status: "suspended"}, []string{"Reload", "Disable", "Show Log", "Open Source"}},
		{"cli cannot be switched off", pluginapi.PluginInfo{Name: "a", Source: "cli", Status: "loaded"}, []string{"Reload", "Show Log", "Open Source"}},
		{"init cannot be switched off", pluginapi.PluginInfo{Name: "init", Source: "init", Status: "loaded"}, []string{"Reload", "Show Log", "Open Source"}},
		{"buffer cannot be reloaded", pluginapi.PluginInfo{Name: "a", Source: "buffer", Status: "loaded"}, []string{"Disable", "Show Log"}},
		{"legacy can only be read", pluginapi.PluginInfo{Name: "a.js", Source: "user", Status: "legacy"}, []string{"Open Source"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var got []string
			for _, it := range managerChoices(tt.in) {
				got = append(got, it.Label)
			}
			if !slices.Equal(got, tt.want) {
				t.Fatalf("choices = %v, want %v", got, tt.want)
			}
		})
	}
	// A locked plugin shows why the toggle will be refused.
	for _, it := range managerChoices(pluginapi.PluginInfo{Name: "a", Source: "bundled", Status: "disabled", Locked: "set in config"}) {
		if it.Label == "Enable" && it.Detail != "set in config" {
			t.Errorf("Enable detail = %q, want the lock", it.Detail)
		}
	}
}

// TestPluginManagerFlow drives the manager as a user does: the palette action opens a
// picker with a row for each plugin, Enter opens the actions of the highlighted row, and
// Enter on an action asks the runtime.
func TestPluginManagerFlow(t *testing.T) {
	t.Parallel()
	m, rt, _ := newMgrFixture(t, samplePlugins...)
	runPaletteAction(m, "plugin_manager")
	ov, ok := m.overlays[len(m.overlays)-1].(*managerOverlay)
	if !ok {
		t.Fatalf("top overlay is %T, want the manager picker", m.overlays[len(m.overlays)-1])
	}
	if got := ov.p.Rows(); got != len(samplePlugins) {
		t.Fatalf("picker has %d rows, want %d", got, len(samplePlugins))
	}
	text := frameString(m)
	for _, want := range []string{"Plugins", "git_gutter", "(2 cmds, 5 subs)", "locked: set in config", "JavaScript plugins are not supported"} {
		if !strings.Contains(text, want) {
			t.Errorf("the frame does not show %q:\n%s", want, text)
		}
	}
	// Down to "hello" (failed), Enter: the popup of its actions.
	feed(m, runKey(m, special(tea.KeyDown)))
	feed(m, runKey(m, special(tea.KeyEnter)))
	if _, ok := m.overlays[len(m.overlays)-1].(popupOverlay); !ok || len(m.overlays) != 1 {
		t.Fatalf("overlays = %v, want only the actions popup", m.overlays)
	}
	text = frameString(m)
	for _, want := range []string{"hello", "Reload", "Disable", "Show Log", "Open Source", "Copy Error"} {
		if !strings.Contains(text, want) {
			t.Errorf("the popup does not show %q:\n%s", want, text)
		}
	}
	feed(m, runKey(m, special(tea.KeyEnter))) // Reload
	if len(m.overlays) != 0 {
		t.Fatalf("%d overlays left after the choice", len(m.overlays))
	}
	if want := []pluginapi.Control{{Op: "reload", Name: "hello"}}; !slices.Equal(rt.controls, want) {
		t.Fatalf("controls = %+v, want %+v", rt.controls, want)
	}
}

// TestPluginManagerFilterAndEsc checks that the picker filters by what is typed and that
// Esc closes it without asking the runtime anything.
func TestPluginManagerFilterAndEsc(t *testing.T) {
	t.Parallel()
	m, rt, _ := newMgrFixture(t, samplePlugins...)
	runPaletteAction(m, "plugin_manager")
	typeInto(m, "flash")
	ov := m.overlays[len(m.overlays)-1].(*managerOverlay)
	if ids := ov.p.RowIDs(); !slices.Equal(ids, []int{2}) {
		t.Fatalf("rows after typing flash = %v, want [2]", ids)
	}
	feed(m, runKey(m, special(tea.KeyEscape)))
	if len(m.overlays) != 0 || len(rt.controls) != 0 {
		t.Fatalf("overlays %d, controls %v after Esc", len(m.overlays), rt.controls)
	}
}

// TestPluginManagerTitleSaysNoPlugins checks the --no-plugins title, and the notice when
// there is nothing to list.
func TestPluginManagerTitleSaysNoPlugins(t *testing.T) {
	t.Parallel()
	t.Run("rows with --no-plugins", func(t *testing.T) {
		t.Parallel()
		m, rt, _ := newMgrFixture(t, pluginapi.PluginInfo{Name: "dev", Source: "cli", Status: "loaded"})
		rt.noPlugins = true
		runPaletteAction(m, "plugin_manager")
		if !strings.Contains(frameString(m), "disabled by --no-plugins") {
			t.Fatalf("the title does not say plugins are off:\n%s", frameString(m))
		}
	})
	t.Run("none with --no-plugins", func(t *testing.T) {
		t.Parallel()
		m, rt, _ := newMgrFixture(t)
		rt.noPlugins = true
		runPaletteAction(m, "plugin_manager")
		if m.notice != "Plugins are disabled by --no-plugins" || len(m.overlays) != 0 {
			t.Fatalf("notice %q, %d overlays", m.notice, len(m.overlays))
		}
	})
	t.Run("none", func(t *testing.T) {
		t.Parallel()
		m, _, _ := newMgrFixture(t)
		runPaletteAction(m, "plugin_manager")
		if m.notice != "No plugins found" || len(m.overlays) != 0 {
			t.Fatalf("notice %q, %d overlays", m.notice, len(m.overlays))
		}
	})
}

// TestPluginReloadPicker checks that Plugin: Reload... offers only the plugins that can be
// reloaded and that choosing one reloads it.
func TestPluginReloadPicker(t *testing.T) {
	t.Parallel()
	m, rt, _ := newMgrFixture(t, samplePlugins...)
	runPaletteAction(m, "plugin_reload")
	ov := m.overlays[len(m.overlays)-1].(*managerOverlay)
	if got := ov.p.Rows(); got != 2 {
		t.Fatalf("reload picker has %d rows, want git_gutter and hello", got)
	}
	feed(m, runKey(m, special(tea.KeyDown)))
	feed(m, runKey(m, special(tea.KeyEnter)))
	if want := []pluginapi.Control{{Op: "reload", Name: "hello"}}; !slices.Equal(rt.controls, want) {
		t.Fatalf("controls = %+v, want %+v", rt.controls, want)
	}

	m, _, _ = newMgrFixture(t, pluginapi.PluginInfo{Name: "x", Source: "user", Status: "disabled"})
	runPaletteAction(m, "plugin_reload")
	if m.notice != "No plugin can be reloaded" || len(m.overlays) != 0 {
		t.Fatalf("notice %q, %d overlays", m.notice, len(m.overlays))
	}
}

// TestPluginControlActions checks the actions that go straight to the runtime.
func TestPluginControlActions(t *testing.T) {
	t.Parallel()
	tests := []struct {
		action string
		want   pluginapi.Control
	}{
		{"plugin_reload_all", pluginapi.Control{Op: "reload_all"}},
		{"init_check", pluginapi.Control{Op: "init_check"}},
		{"init_reload", pluginapi.Control{Op: "init_reload"}},
	}
	for _, tt := range tests {
		t.Run(tt.action, func(t *testing.T) {
			t.Parallel()
			m, rt, _ := newMgrFixture(t)
			runPaletteAction(m, tt.action)
			if !slices.Equal(rt.controls, []pluginapi.Control{tt.want}) {
				t.Fatalf("controls = %+v, want %+v", rt.controls, tt.want)
			}
		})
	}
}

// TestPluginControlNoticesReachTheStatusRow checks that what the runtime says about a
// lifecycle request is shown.
func TestPluginControlNoticesReachTheStatusRow(t *testing.T) {
	t.Parallel()
	m, rt, _ := newMgrFixture(t)
	rt.onControl = func(c pluginapi.Control) ([]pluginapi.Work, []pluginapi.Notice) {
		return nil, []pluginapi.Notice{{Text: "plugins are disabled by --no-plugins", Warn: true}}
	}
	runPaletteAction(m, "plugin_reload_all")
	if m.notice != "plugins are disabled by --no-plugins" {
		t.Fatalf("notice = %q", m.notice)
	}
}

// TestPluginActionsWithoutRuntime checks that every plugin action says so when the session
// has no plugin runtime, and is not reported as unsupported or as a Fresh-only command.
func TestPluginActionsWithoutRuntime(t *testing.T) {
	t.Parallel()
	actions := []string{
		"plugin_manager", "plugin_show_log", "plugin_reload", "plugin_reload_all", "load_plugin_from_buffer",
		"init_check", "init_edit", "init_reload", "prompt_save_macro_to_init", "prompt_promote_macro",
	}
	for _, action := range actions {
		t.Run(action, func(t *testing.T) {
			t.Parallel()
			m := newMgrModel(t, newFakeEditor("x"), nil)
			if cmd := rootPaletteRun(m, action); cmd != nil {
				t.Fatalf("%s started a command", action)
			}
			if m.notice != noPluginsNotice {
				t.Fatalf("notice = %q, want %q", m.notice, noPluginsNotice)
			}
		})
	}
}

// TestPluginManagerActionsAreNotNotApplicable checks that the ten actions this stage
// handles are gone from the table of commands that cannot work.
func TestPluginManagerActionsAreNotNotApplicable(t *testing.T) {
	t.Parallel()
	for _, a := range []string{
		"plugin_manager", "plugin_show_log", "plugin_reload", "plugin_reload_all", "load_plugin_from_buffer",
		"init_check", "init_edit", "init_reload", "prompt_save_macro_to_init", "prompt_promote_macro",
	} {
		if _, ok := notApplicable[a]; ok {
			t.Errorf("%s is still in the table of commands that cannot work", a)
		}
	}
}

// TestManagerRunActions checks what each action of the popup does.
func TestManagerRunActions(t *testing.T) {
	t.Parallel()
	in := pluginapi.PluginInfo{Name: "hello", Source: "user", Status: "failed", Err: "boom", Dir: "/cfg/plugins"}
	tests := []struct {
		op   string
		want []pluginapi.Control
	}{
		{"reload", []pluginapi.Control{{Op: "reload", Name: "hello"}}},
		{"enable", []pluginapi.Control{{Op: "enable", Name: "hello"}}},
		{"disable", []pluginapi.Control{{Op: "disable", Name: "hello"}}},
	}
	for _, tt := range tests {
		t.Run(tt.op, func(t *testing.T) {
			t.Parallel()
			m, rt, _ := newMgrFixture(t, in)
			_, cmd := m.Update(popup.Chosen{Item: popup.Item{Data: managerChoice{op: tt.op, info: in}}})
			feed(m, cmd)
			if !slices.Equal(rt.controls, tt.want) {
				t.Fatalf("controls = %+v, want %+v", rt.controls, tt.want)
			}
		})
	}

	t.Run("copy puts the error on the clipboard", func(t *testing.T) {
		t.Parallel()
		m, rt, _ := newMgrFixture(t, in)
		_, cmd := m.Update(popup.Chosen{Item: popup.Item{Data: managerChoice{op: "copy", info: in}}})
		if cmd == nil || len(rt.controls) != 0 {
			t.Fatalf("cmd %v, controls %v", cmd, rt.controls)
		}
		if got := clipboardWrites(cmd); !slices.Contains(got, "boom") {
			t.Fatalf("clipboard writes = %q, want the error", got)
		}
		if !strings.Contains(m.notice, "hello") {
			t.Fatalf("notice = %q", m.notice)
		}
	})

	t.Run("log opens the plugin's lines only", func(t *testing.T) {
		t.Parallel()
		m, rt, ed := newMgrFixture(t, in)
		at := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
		rt.entries = []pluginapi.LogEntry{
			{Time: at, Plugin: "hello", Level: "error", Msg: "boom"},
			{Time: at, Plugin: "other", Level: "info", Msg: "quiet"},
		}
		m.Update(popup.Chosen{Item: popup.Item{Data: managerChoice{op: "log", info: in}}})
		got := scratchText(t, ed.fakeEditor)
		if want := "03:04:05 error hello: boom\n"; got != want {
			t.Fatalf("log = %q, want %q", got, want)
		}
	})

	t.Run("a foreign choice is not ours", func(t *testing.T) {
		t.Parallel()
		m, _, _ := newMgrFixture(t)
		if _, ok := m.pluginManagerMessage(popup.Chosen{Item: popup.Item{Data: 42}}); ok {
			t.Fatal("the manager took a popup choice that is not its own")
		}
	})
}

// scratchText is the text that openScratch inserted into the fake editor.
func scratchText(t *testing.T, ed *fakeEditor) string {
	t.Helper()
	for i, a := range ed.actions {
		if a == "insert_char" {
			s, _ := ed.args[i]["text"].(string)
			return s
		}
	}
	t.Fatalf("no text was inserted; actions = %v", ed.actions)
	return ""
}

// TestPluginShowLog checks the command: every plugin's lines, newest last, and the
// loader's lines as "gvim".
func TestPluginShowLog(t *testing.T) {
	t.Parallel()
	m, rt, ed := newMgrFixture(t)
	at := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	rt.entries = []pluginapi.LogEntry{
		{Time: at, Plugin: "", Level: "info", Msg: "2 plugin(s) loaded"},
		{Time: at.Add(time.Second), Plugin: "hello", Level: "warn", Msg: "slow"},
	}
	runPaletteAction(m, "plugin_show_log")
	want := "03:04:05 info  gvim: 2 plugin(s) loaded\n03:04:06 warn  hello: slow\n"
	if got := scratchText(t, ed.fakeEditor); got != want {
		t.Fatalf("log = %q, want %q", got, want)
	}
}

func TestPluginLogText(t *testing.T) {
	t.Parallel()
	at := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	tests := []struct {
		name    string
		entries []pluginapi.LogEntry
		plugin  string
		want    string
	}{
		{"empty", nil, "", "No plugin log entries yet.\n"},
		{"empty for one plugin", nil, "a", "No log entries for plugin 'a'.\n"},
		{"filtered to nothing", []pluginapi.LogEntry{{Time: at, Plugin: "b", Level: "info", Msg: "x"}}, "a", "No log entries for plugin 'a'.\n"},
		{"order is kept", []pluginapi.LogEntry{
			{Time: at, Plugin: "a", Level: "info", Msg: "first"},
			{Time: at, Plugin: "a", Level: "debug", Msg: "second"},
		}, "", "03:04:05 info  a: first\n03:04:05 debug a: second\n"},
		{"multi-line messages are indented, control characters cleaned", []pluginapi.LogEntry{
			{Time: at, Plugin: "a", Level: "error", Msg: "bad\x1b[0m\nline 2\tx"},
		}, "a", "03:04:05 error a: bad [0m\n    line 2\tx\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := pluginLogText(tt.entries, tt.plugin); got != tt.want {
				t.Fatalf("pluginLogText = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestPluginSourcePath checks which file Open Source finds for each kind of plugin. The
// runtime reports the entry file of a plugin; the ui only checks that it is a file.
func TestPluginSourcePath(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	write := func(rel string) string {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, nil, 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	single := write("plugins/hello.lua")
	entry := write("plugins/withinit/init.lua")
	write("plugins/withinit/withinit.lua") // a helper must not be mistaken for the entry
	legacy := write("plugins/old.js")
	initLua := write("init.lua")
	if err := os.MkdirAll(filepath.Join(root, "plugins", "adir"), 0o755); err != nil {
		t.Fatal(err)
	}
	plugins := filepath.Join(root, "plugins")

	tests := []struct {
		name    string
		in      pluginapi.PluginInfo
		want    string
		wantErr string
	}{
		{"single file", pluginapi.PluginInfo{Name: "hello", Source: "user", File: single}, single, ""},
		{"cli single file", pluginapi.PluginInfo{Name: "hello", Source: "cli", File: single}, single, ""},
		{"directory entry", pluginapi.PluginInfo{Name: "withinit", Source: "user", Dir: filepath.Join(plugins, "withinit"), File: entry}, entry, ""},
		{"legacy file", pluginapi.PluginInfo{Name: "old.js", Source: "user", Status: "legacy", Dir: plugins}, legacy, ""},
		{"init.lua", pluginapi.PluginInfo{Name: "init", Source: "init", Dir: root}, initLua, ""},
		{"bundled", pluginapi.PluginInfo{Name: "git_gutter", Source: "bundled"}, "", "no file on disk"},
		{"buffer", pluginapi.PluginInfo{Name: "scratch", Source: "buffer"}, "", "from a buffer"},
		{"no source file", pluginapi.PluginInfo{Name: "x", Source: "user"}, "", "no source file"},
		{"entry is a directory", pluginapi.PluginInfo{Name: "adir", Source: "user", File: filepath.Join(plugins, "adir")}, "", "not a regular file"},
		{"entry is gone", pluginapi.PluginInfo{Name: "gone", Source: "user", File: filepath.Join(plugins, "gone.lua")}, "", "no such file"},
		{"missing legacy file", pluginapi.PluginInfo{Name: "gone.js", Source: "user", Status: "legacy", Dir: plugins}, "", "no such file"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := pluginSourcePath(tt.in, initLua)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("err = %v, want one mentioning %q", err, tt.wantErr)
				}
				return
			}
			if err != nil || got != tt.want {
				t.Fatalf("path = %q, %v; want %q", got, err, tt.want)
			}
		})
	}
}

// TestOpenSourceOpensTheFile checks the whole path: the command looks at the disk, the
// message it sends opens the file in the editor.
func TestOpenSourceOpensTheFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	file := filepath.Join(dir, "hello.lua")
	if err := os.WriteFile(file, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	in := pluginapi.PluginInfo{Name: "hello", Source: "user", File: file}
	m, _, ed := newMgrFixture(t, in)
	_, cmd := m.Update(popup.Chosen{Item: popup.Item{Data: managerChoice{op: "source", info: in}}})
	feed(m, cmd)
	if !slices.Equal(ed.opened, []string{file}) {
		t.Fatalf("opened %v, want %s", ed.opened, file)
	}

	in = pluginapi.PluginInfo{Name: "gone", Source: "user", File: filepath.Join(dir, "nowhere.lua")}
	_, cmd = m.Update(popup.Chosen{Item: popup.Item{Data: managerChoice{op: "source", info: in}}})
	feed(m, cmd)
	if len(ed.opened) != 1 || !strings.HasPrefix(m.notice, "Plugin 'gone': ") {
		t.Fatalf("opened %v, notice %q", ed.opened, m.notice)
	}
}

// TestLoadPluginFromBuffer checks the command: the buffer's text and file stem go to the
// runtime, and a buffer that cannot be loaded says why without asking it.
func TestLoadPluginFromBuffer(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		doc        Document
		want       []pluginapi.Control
		wantNotice string
	}{
		{"named file", lockedDoc{fakeDoc: &fakeDoc{lines: []string{"print(1)", "", "x"}}, name: "hello.lua"},
			[]pluginapi.Control{{Op: "load_text", Name: "hello", Text: "print(1)\n\nx"}}, ""},
		{"path in the name", lockedDoc{fakeDoc: &fakeDoc{lines: []string{"x"}}, name: "dir/My Tool.lua"},
			[]pluginapi.Control{{Op: "load_text", Name: "My Tool", Text: "x"}}, ""},
		{"large file", lockedDoc{fakeDoc: &fakeDoc{lines: []string{"x"}}, name: "big.lua", large_: true},
			nil, "a large file cannot be loaded as a plugin"},
		{"over the size limit", lockedDoc{fakeDoc: &fakeDoc{lines: []string{strings.Repeat("x", pluginSourceMax+1)}}, name: "big.lua"},
			nil, "larger than 256 KiB"},
		{"no document", nil, nil, noDocumentNotice},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			rt := &mgrRuntime{fakeRuntime: newFakeRuntime(t)}
			m := newMgrModel(t, holdsDocEditor{fakeEditor: newFakeEditor("x"), doc: tt.doc}, rt)
			runPaletteAction(m, "load_plugin_from_buffer")
			if !slices.Equal(rt.controls, tt.want) {
				t.Fatalf("controls = %+v, want %+v", rt.controls, tt.want)
			}
			if tt.wantNotice != "" && !strings.Contains(m.notice, tt.wantNotice) {
				t.Fatalf("notice = %q, want it to mention %q", m.notice, tt.wantNotice)
			}
		})
	}
}

// TestPluginActionsAreDeferredInASlice checks that a plugin that runs one of the
// management actions through command.run does not enter the runtime from inside its own
// slice: the action waits for the pump.
func TestPluginActionsAreDeferredInASlice(t *testing.T) {
	t.Parallel()
	rt := newFakeRuntime(t, "x.cmd")
	rt.strictSlice = true
	m, _ := newPluginModel(t, rt)
	rt.onRun = func(string, map[string]any) {
		if err := rt.host.Do("plugin_reload_all", nil, 1); err != nil {
			t.Error(err)
		}
		if len(rt.controls) != 0 {
			t.Error("the runtime was entered inside the slice")
		}
	}
	must(t, m.Run("x.cmd"))
	feed(m, m.pending)
	m.pending = nil
	if !slices.Equal(rt.controls, []pluginapi.Control{{Op: "reload_all"}}) {
		t.Fatalf("controls = %+v, want the deferred reload_all", rt.controls)
	}
}

// TestShowLogIsReadOnlyVirtualBuffer checks that the log opens in a read-only virtual
// buffer (which is never modified and never asks to be saved), not in an editable
// untitled tab, and that the filtered log of one plugin does too.
func TestShowLogIsReadOnlyVirtualBuffer(t *testing.T) {
	t.Parallel()
	at := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	entries := []pluginapi.LogEntry{
		{Time: at, Plugin: "hello", Level: "warn", Msg: "slow"},
		{Time: at, Plugin: "other", Level: "info", Msg: "quiet"},
	}
	tests := []struct {
		name string
		run  func(m *Model)
		want string
	}{
		{"command", func(m *Model) { runPaletteAction(m, "plugin_show_log") },
			"03:04:05 warn  hello: slow\n03:04:05 info  other: quiet\n"},
		{"one plugin", func(m *Model) {
			m.Update(popup.Chosen{Item: popup.Item{Data: managerChoice{op: "log", info: pluginapi.PluginInfo{Name: "hello"}}}})
		}, "03:04:05 warn  hello: slow\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			m, pe := newVirtualModel(t)
			rt := &mgrRuntime{fakeRuntime: newFakeRuntime(t), entries: entries}
			m.plug.rt = rt
			useHost(rt, m)
			tt.run(m)
			if len(pe.made) != 1 {
				t.Fatalf("virtual buffers made = %d, want 1", len(pe.made))
			}
			got := pe.made[0]
			if !got.ReadOnly || !got.Focus || got.Text != tt.want || got.Name != "Plugin Log" {
				t.Fatalf("CreateBuffer = %+v, want a focused read-only %q with %q", got, "Plugin Log", tt.want)
			}
			if slices.Contains(m.ed.(*fakeEditor).actions, "new") {
				t.Fatal("the log opened an untitled editable tab")
			}
		})
	}
}

// TestPluginNoticesOfOneUpdateShareTheStatusRow checks that the notices a plugin runtime
// reports in one update (the load errors of startup) are all on the status row, joined,
// and each of them is in the warnings log, once.
func TestPluginNoticesOfOneUpdateShareTheStatusRow(t *testing.T) {
	t.Parallel()
	m, _, _ := newMgrFixture(t)
	host := m.plug.host
	host.Notify("", pluginapi.LevelWarn, "2 JavaScript plugin(s) ignored")
	host.Notify("", pluginapi.LevelError, "plugin 'a': boom")
	host.Notify("", pluginapi.LevelError, "plugin 'b': bang")
	if want := "2 JavaScript plugin(s) ignored; plugin 'a': boom; plugin 'b': bang"; m.notice != want {
		t.Fatalf("notice = %q, want %q", m.notice, want)
	}
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	if want := []string{"2 JavaScript plugin(s) ignored", "plugin 'a': boom", "plugin 'b': bang"}; !slices.Equal(m.notes.items, want) {
		t.Fatalf("warnings = %q, want %q", m.notes.items, want)
	}
	// A notice of a later update replaces the status row, as it did.
	host.Notify("", pluginapi.LevelError, "plugin 'c': late")
	if m.notice != "plugin 'c': late" {
		t.Fatalf("notice = %q, want only the late one", m.notice)
	}
	// A plugin's own notice is not joined and has its name in front; nor is a quiet (info) one.
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	host.Notify("hello", pluginapi.LevelWarn, "careful")
	if m.notice != "hello: careful" {
		t.Fatalf("notice = %q, want the plugin notice alone", m.notice)
	}
	host.Notify("", pluginapi.LevelInfo, "fyi")
	if m.notice != "fyi" {
		t.Fatalf("notice = %q, want the info notice alone", m.notice)
	}
}
