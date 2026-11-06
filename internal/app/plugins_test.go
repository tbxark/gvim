package app

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/tbxark/gvim/internal/config"
	"github.com/tbxark/gvim/internal/editor"
	"github.com/tbxark/gvim/internal/keymap"
	"github.com/tbxark/gvim/internal/theme"
	"github.com/tbxark/gvim/internal/ui"
)

// writePluginFiles isolates the user config and writes files into the plugin
// folder it gives, so that buildRuntime finds them.
func writePluginFiles(t *testing.T, files map[string]string) {
	t.Helper()
	isolateConfig(t)
	dir, err := pluginDir()
	if err != nil {
		t.Fatal(err)
	}
	for name, body := range files {
		writeTestFile(t, filepath.Join(dir, name), body)
	}
}

// pluginRuntime builds the runtime of a session that has one file open. It must
// follow writePluginFiles, which isolates the user config that the plugin folder
// comes from.
func pluginRuntime(t *testing.T) *runtime {
	t.Helper()
	rt, err := buildRuntime(t.Context(), projectRoot(t, ""), false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(rt.close)
	rt.model.Update(tea.WindowSizeMsg{Width: 100, Height: 24})
	return rt
}

// pluginMsgs runs cmd and returns the messages it produces, looking into batches.
func pluginMsgs(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	msg := cmd()
	if batch, ok := msg.(tea.BatchMsg); ok {
		var out []tea.Msg
		for _, c := range batch {
			out = append(out, pluginMsgs(c)...)
		}
		return out
	}
	if msg == nil {
		return nil
	}
	return []tea.Msg{msg}
}

// pluginFeed passes every message cmd produces back to the model, as the program
// does.
func pluginFeed(m *ui.Model, cmd tea.Cmd) {
	for _, msg := range pluginMsgs(cmd) {
		_, next := m.Update(msg)
		pluginFeed(m, next)
	}
}

// pluginKey sends one key press and feeds the commands it starts.
func pluginKey(m *ui.Model, msg tea.KeyPressMsg) {
	_, cmd := m.Update(msg)
	pluginFeed(m, cmd)
}

// pluginPalette opens the command palette, types query and returns the frame.
// It closes the palette again with Esc.
func pluginPalette(t *testing.T, m *ui.Model, query string) string {
	t.Helper()
	if err := m.Run("command_palette"); err != nil {
		t.Fatal(err)
	}
	for _, r := range query {
		pluginKey(m, tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	frame := ansi.Strip(m.View().Content)
	pluginKey(m, tea.KeyPressMsg{Code: tea.KeyEscape})
	return frame
}

// pluginRowShows reports whether one line of frame shows label as a plugin
// command. The query line also holds the typed label, so the row must also show
// the plugin source.
func pluginRowShows(frame, label string) bool {
	for line := range strings.SplitSeq(frame, "\n") {
		if strings.Contains(line, label) && strings.Contains(line, "plugin") {
			return true
		}
	}
	return false
}

// pluginChoose opens the command palette, types query and presses Enter, which
// runs the highlighted row.
func pluginChoose(t *testing.T, m *ui.Model, query string) {
	t.Helper()
	if err := m.Run("command_palette"); err != nil {
		t.Fatal(err)
	}
	for _, r := range query {
		pluginKey(m, tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	pluginKey(m, tea.KeyPressMsg{Code: tea.KeyEnter})
}

func TestPluginDirIsPluginsUnderTheUserConfigDir(t *testing.T) {
	isolateConfig(t)
	got, err := pluginDir()
	if err != nil {
		t.Fatal(err)
	}
	base, err := config.Dir()
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(base, "plugins"); got != want {
		t.Fatalf("pluginDir() = %q, want %q", got, want)
	}
}

func TestMissingPluginDirIsSilent(t *testing.T) {
	isolateConfig(t)
	ed := editor.New(editor.Options{})
	r, notices := loadPlugins(ed, newUIDeps(ed, keymap.Default(), theme.Theme{}, nil, nil).Editor, 0)
	if len(notices) != 0 {
		t.Fatalf("notices = %q, want none for a missing plugin folder", notices)
	}
	if cmds := r.Commands(); len(cmds) != 0 {
		t.Fatalf("Commands() = %d entries, want none", len(cmds))
	}
}

func TestPluginCommandsAreListedWithTheirLabels(t *testing.T) {
	writePluginFiles(t, map[string]string{
		"greet.js": `gvim.registerCommand("greet", "Say Hello", () => {});`,
		"shout.ts": `gvim.registerCommand("shout", "Shout It", (): void => {});`,
	})
	m := pluginRuntime(t).model
	for _, label := range []string{"Say Hello", "Shout It"} {
		if frame := pluginPalette(t, m, label); !pluginRowShows(frame, label) {
			t.Errorf("palette typed %q has no plugin row labelled %q:\n%s", label, label, frame)
		}
	}
}

func TestChoosingPluginCommandInsertsTextAtTheCaret(t *testing.T) {
	writePluginFiles(t, map[string]string{
		"stamp.js": `gvim.registerCommand("stamp", "Insert Stamp", () => { gvim.insertText("[ok]"); });`,
	})
	rt := pluginRuntime(t)
	pluginChoose(t, rt.model, "Insert Stamp")
	if got := rt.ed.Active().Buffer.Text(); !strings.HasPrefix(got, "[ok]hello") {
		t.Fatalf("buffer = %q, want the stamp inserted at the caret before hello", got)
	}
}

func TestChoosingPluginCommandShowsItsStatusText(t *testing.T) {
	writePluginFiles(t, map[string]string{
		"status.js": `gvim.registerCommand("status", "Report Status", () => { gvim.setStatus("plugin ready"); });`,
	})
	m := pluginRuntime(t).model
	pluginChoose(t, m, "Report Status")
	if frame := ansi.Strip(m.View().Content); !strings.Contains(frame, "plugin ready") {
		t.Fatalf("status row does not show the plugin's status text:\n%s", frame)
	}
}

func TestBrokenPluginIsNoticedAndTheOthersStillLoad(t *testing.T) {
	writePluginFiles(t, map[string]string{
		"a_broken.js": `gvim.registerCommand("bad", "Bad Command", () => {`,
		"b_good.js":   `gvim.registerCommand("good", "Good Command", () => {});`,
	})
	m := pluginRuntime(t).model
	if frame := ansi.Strip(m.View().Content); !strings.Contains(frame, "plugin a_broken.js") {
		t.Errorf("status row does not name the broken plugin:\n%s", frame)
	}
	if frame := pluginPalette(t, m, "Good Command"); !pluginRowShows(frame, "Good Command") {
		t.Error("the good plugin's command is not listed after the broken one failed")
	}
	if frame := pluginPalette(t, m, "Bad Command"); pluginRowShows(frame, "Bad Command") {
		t.Error("the broken plugin's command is listed")
	}
}

func TestPluginRunnerReturnsNoticesAndErrors(t *testing.T) {
	writePluginFiles(t, map[string]string{
		"say.js":   `gvim.registerCommand("say", "Say", () => { gvim.notify("hi"); gvim.setStatus("there"); });`,
		"lines.js": `gvim.registerCommand("lines", "Lines", () => { gvim.notify("a\nb"); });`,
		"boom.js":  `gvim.registerCommand("boom", "Boom", () => { throw new Error("kaboom"); });`,
	})
	folder, err := pluginDir()
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name       string
		command    string
		wantNotice string
		wantErr    string
	}{
		{name: "notify and set status are joined", command: "say", wantNotice: "hi; there"},
		{name: "a newline stays on one row", command: "lines", wantNotice: "a b"},
		{name: "a thrown error is returned", command: "boom", wantErr: "kaboom"},
		{name: "an unknown command is an error", command: "missing", wantErr: `unknown command "missing"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// The runner is built on this goroutine, which is the one that runs it.
			ed := editor.New(editor.Options{})
			r, notices := loadPlugins(ed, newUIDeps(ed, keymap.Default(), theme.Theme{}, nil, nil).Editor, 0)
			if len(notices) != 0 {
				t.Fatalf("load notices = %q, want none", notices)
			}
			notice, err := r.Run(tt.command)
			if notice != tt.wantNotice {
				t.Errorf("notice = %q, want %q", notice, tt.wantNotice)
			}
			switch {
			case tt.wantErr == "" && err != nil:
				t.Errorf("err = %v, want nil", err)
			case tt.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tt.wantErr)):
				t.Errorf("err = %v, want it to contain %q", err, tt.wantErr)
			}
			if err != nil && strings.Contains(err.Error(), folder) {
				t.Errorf("err %q names the full plugin folder %q", err, folder)
			}
		})
	}
}

func TestPluginErrorsAreOneShortLine(t *testing.T) {
	writePluginFiles(t, map[string]string{
		"boom.js":  `gvim.registerCommand("boom", "Boom", () => { throw new Error("kaboom"); });`,
		"broke.js": `gvim.registerCommand("broke", "Broke", () => {`,
		"fine.js":  `gvim.registerCommand("fine", "Fine", () => {});`,
	})
	folder, err := pluginDir()
	if err != nil {
		t.Fatal(err)
	}
	ed := editor.New(editor.Options{Empty: true})
	r, notices := loadPlugins(ed, newUIDeps(ed, keymap.Default(), theme.Theme{}, nil, nil).Editor, 0)
	if len(notices) != 1 {
		t.Fatalf("load notices = %q, want one for the broken file", notices)
	}
	for _, msg := range notices {
		checkPluginLine(t, msg, folder, "broke.js")
	}
	_, runErr := r.Run("boom")
	if runErr == nil {
		t.Fatal("Run(boom) returned no error")
	}
	checkPluginLine(t, runErr.Error(), folder, "boom.js")
}

// checkPluginLine checks that a notice is one line, names the plugin file and does
// not repeat the plugin folder.
func checkPluginLine(t *testing.T, msg, folder, file string) {
	t.Helper()
	if strings.Contains(msg, "\n") {
		t.Errorf("notice %q spans more than one line", msg)
	}
	if !strings.Contains(msg, file) {
		t.Errorf("notice %q does not name %s", msg, file)
	}
	if strings.Contains(msg, folder) {
		t.Errorf("notice %q repeats the plugin folder %q", msg, folder)
	}
}

func TestPluginCommandLabelDefaultsToItsName(t *testing.T) {
	writePluginFiles(t, map[string]string{
		"nameless.js": `gvim.registerCommand("nameless", "", () => {});`,
	})
	ed := editor.New(editor.Options{})
	r, _ := loadPlugins(ed, newUIDeps(ed, keymap.Default(), theme.Theme{}, nil, nil).Editor, 0)
	cmds := r.Commands()
	if len(cmds) != 1 || cmds[0].Name != "nameless" || cmds[0].Action != "plugin:nameless" {
		t.Fatalf("Commands() = %+v, want one entry named nameless with action plugin:nameless", cmds)
	}
}

func TestPluginEditorActivePathAndInsertText(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "doc.txt")
	writeTestFile(t, path, "hello\n")
	tests := []struct {
		name          string
		open          bool
		wantPath      string
		wantInsertErr bool
	}{
		{name: "no document", open: false, wantPath: "", wantInsertErr: true},
		{name: "a file document", open: true, wantPath: path, wantInsertErr: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Without Empty the editor starts with an untitled document.
			ed := editor.New(editor.Options{Empty: !tt.open})
			if tt.open {
				if err := ed.OpenFile(path); err != nil {
					t.Fatal(err)
				}
			}
			pe := &pluginEditor{ed: ed, state: newUIDeps(ed, keymap.Default(), theme.Theme{}, nil, nil).Editor, owner: goroutineID()}
			if got := pe.ActivePath(); got != tt.wantPath {
				t.Errorf("ActivePath() = %q, want %q", got, tt.wantPath)
			}
			if err := pe.InsertText("X"); (err != nil) != tt.wantInsertErr {
				t.Errorf("InsertText error = %v, want error %v", err, tt.wantInsertErr)
			}
		})
	}
}

func TestPluginEditorRefusesOtherGoroutines(t *testing.T) {
	calls := []struct {
		name string
		call func(*pluginEditor)
	}{
		{name: "Notify", call: func(p *pluginEditor) { p.Notify("x") }},
		{name: "SetStatus", call: func(p *pluginEditor) { p.SetStatus("x") }},
		{name: "InsertText", call: func(p *pluginEditor) { _ = p.InsertText("x") }},
		{name: "ActivePath", call: func(p *pluginEditor) { _ = p.ActivePath() }},
	}
	for _, c := range calls {
		t.Run(c.name, func(t *testing.T) {
			// The adapter is built on this goroutine, so it is the owner here.
			ed := editor.New(editor.Options{})
			pe := &pluginEditor{ed: ed, state: newUIDeps(ed, keymap.Default(), theme.Theme{}, nil, nil).Editor, owner: goroutineID()}
			// On the owner goroutine the call returns normally.
			c.call(pe)
			// On another goroutine it panics, and the panic is caught here.
			done := make(chan any, 1)
			go func() {
				defer func() { done <- recover() }()
				c.call(pe)
			}()
			if got := <-done; got == nil {
				t.Fatalf("%s from another goroutine did not panic", c.name)
			}
		})
	}
}

// TestPluginTimeoutIsTheConfiguredLimit checks that a plugin call is cut off at the
// timeout the runner was loaded with, and at the default when none is given.
func TestPluginTimeoutIsTheConfiguredLimit(t *testing.T) {
	writePluginFiles(t, map[string]string{
		"spin.js": `gvim.registerCommand("spin", "Spin", () => { while (true) {} });`,
	})
	tests := []struct {
		name    string
		timeout time.Duration
		want    string
	}{
		{name: "a configured limit", timeout: 50 * time.Millisecond, want: "timed out after 50ms"},
		{name: "zero is the default limit", timeout: 0, want: "timed out after 500ms"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ed := editor.New(editor.Options{})
			r, notices := loadPlugins(ed, newUIDeps(ed, keymap.Default(), theme.Theme{}, nil, nil).Editor, tt.timeout)
			if len(notices) != 0 {
				t.Fatalf("load notices = %q, want none", notices)
			}
			if _, err := r.Run("spin"); err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Run(spin) error = %v, want it to contain %q", err, tt.want)
			}
		})
	}
}

// TestPluginTimeoutFromConfigReachesTheCommand checks that plugin_timeout_ms in the
// config file is the limit that a palette choice of a plugin command runs under.
func TestPluginTimeoutFromConfigReachesTheCommand(t *testing.T) {
	writePluginFiles(t, map[string]string{
		"spin.js": `gvim.registerCommand("spin", "Spin Forever", () => { while (true) {} });`,
	})
	rt, err := buildRuntime(t.Context(), projectRoot(t, `{"plugin_timeout_ms": 50}`), false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(rt.close)
	rt.model.Update(tea.WindowSizeMsg{Width: 100, Height: 24})

	// The status row cuts the message short, so the limit is read from the time the
	// choice takes: the default limit is 500ms, and the configured 50ms must end the
	// call well before it.
	start := time.Now()
	pluginChoose(t, rt.model, "Spin Forever")
	if elapsed := time.Since(start); elapsed > 400*time.Millisecond {
		t.Errorf("the choice took %v, want the configured 50ms limit to end the call", elapsed)
	}
	if frame := ansi.Strip(rt.model.View().Content); !strings.Contains(frame, "timed out") {
		t.Fatalf("status row does not report the timeout:\n%s", frame)
	}
}
