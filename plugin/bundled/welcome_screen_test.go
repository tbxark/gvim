package bundled_test

import (
	"encoding/json"
	"io/fs"
	"strings"
	"testing"

	"github.com/tbxark/gvim/plugin"
	"github.com/tbxark/gvim/plugin/bundled"
	"github.com/tbxark/gvim/plugin/plugintest"
	"github.com/tbxark/gvim/pluginapi"
)

const welcomeScreenPluginName = "welcome_screen"

func welcomeScreenBuffer(host *plugintest.FakeHost) pluginapi.BufferID {
	for _, info := range host.ListBuffers() {
		if info.Virtual && strings.Contains(info.Name, "Welcome") {
			return info.ID
		}
	}
	return 0
}

func welcomeScreenText(t *testing.T, host *plugintest.FakeHost) string {
	t.Helper()
	id := welcomeScreenBuffer(host)
	if id == 0 {
		t.Fatalf("no welcome buffer (buffers %v)", host.ListBuffers())
	}
	entries, _ := host.VirtualContent(id)
	return contentText(entries)
}

func welcomeScreenRig(t *testing.T, opts rigOpts) *rig {
	t.Helper()
	opts.plugins = append([]string{welcomeScreenPluginName}, opts.plugins...)
	return startRig(t, opts)
}

func TestWelcomeScreenManifestAndCommands(t *testing.T) {
	data, err := fs.ReadFile(bundled.FS, "plugins/welcome_screen/plugin.json")
	if err != nil {
		t.Fatal(err)
	}
	var manifest struct {
		Name, Version, Description string
		API                        int
		DefaultEnabled             bool `json:"default_enabled"`
	}
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.Name != welcomeScreenPluginName || manifest.Version == "" || manifest.Description == "" || manifest.API != 2 {
		t.Fatalf("manifest = %+v", manifest)
	}
	if manifest.DefaultEnabled {
		t.Fatal("welcome_screen must be opt-in (Fresh's page is default-enabled; gvim keeps it off)")
	}
	r := welcomeScreenRig(t, rigOpts{script: func(o *plugintest.FakeOS) { o.SetDir("/work") }})
	commands := map[string]bool{}
	for _, c := range r.host.Commands[welcomeScreenPluginName] {
		if !c.Hidden {
			commands[c.ID] = true
		}
	}
	if !commands["welcome_screen.show"] {
		t.Fatalf("visible commands = %v, want show", commands)
	}
	if modes := r.host.Modes[welcomeScreenPluginName]; len(modes) != 1 || modes[0].Name != "welcome_screen" {
		t.Fatalf("modes = %+v", modes)
	}
	if problems := r.problems(); len(problems) != 0 {
		t.Fatalf("problems = %v", problems)
	}
}

func TestWelcomeScreenPageContentAndTargets(t *testing.T) {
	r := welcomeScreenRig(t, rigOpts{
		buffers: []rigBuffer{{"/work/a.go", "package a\n"}},
		script:  func(o *plugintest.FakeOS) { o.SetDir("/work") },
	})
	r.emit("ready", "/work/a.go", nil)
	text := welcomeScreenText(t, r.host)
	for _, want := range []string{
		"█████", "A terminal text editor", "DASHBOARD", "No dashboard sections registered.",
		"DOORS", "Open a file", "New buffer", "Recent files",
		"RECENT FILES", "a.go", "VERBS", "Find file", "Command palette", "Settings",
		"File explorer", "Help", "j/k or arrows move",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("welcome text lacks %q:\n%s", want, text)
		}
	}
	id := welcomeScreenBuffer(r.host)
	foundAction, foundPath := false, false
	for _, target := range r.host.LineTargets(id) {
		if strings.HasPrefix(target.Action, welcomeScreenPluginName+".click_action_") {
			foundAction = true
		}
		if target.Path == "/work/a.go" {
			foundPath = true
		}
	}
	if !foundAction || !foundPath {
		t.Fatalf("line targets = %+v, want action and recent path targets", r.host.LineTargets(id))
	}
	if runs := r.os.Runs(); len(runs) != 0 {
		t.Fatalf("welcome page started processes: %+v", runs)
	}
}

func TestWelcomeScreenAutoOpensInBackground(t *testing.T) {
	r := welcomeScreenRig(t, rigOpts{
		buffers: []rigBuffer{{"/work/a.go", "package a\n"}},
		script:  func(o *plugintest.FakeOS) { o.SetDir("/work") },
	})
	fileID := r.ids["/work/a.go"]
	r.h.Emit(pluginapi.Event{Name: "plugins_loaded"})
	r.h.Settle()
	if id := welcomeScreenBuffer(r.host); id == 0 {
		t.Fatalf("welcome page did not auto-open (buffers %v)", r.host.ListBuffers())
	}
	if got := r.host.ActiveBuffer(); got != fileID {
		t.Fatalf("active buffer = %d, want the file %d (background page must not steal focus)", got, fileID)
	}
	r.h.Emit(pluginapi.Event{Name: "after_file_open", Data: map[string]any{"buf": fileID, "path": "/work/a.go"}})
	r.h.Settle()
	if welcomeScreenBuffer(r.host) == 0 {
		t.Fatal("welcome page closed itself")
	}
}

func TestWelcomeScreenAutoOpenCanBeDisabled(t *testing.T) {
	r := welcomeScreenRig(t, rigOpts{
		settings: map[string]plugin.PluginSettings{welcomeScreenPluginName: {User: map[string]any{"auto_open": false}}},
		script:   func(o *plugintest.FakeOS) { o.SetDir("/work") },
	})
	r.h.Emit(pluginapi.Event{Name: "plugins_loaded"})
	r.h.Settle()
	if id := welcomeScreenBuffer(r.host); id != 0 {
		t.Fatalf("welcome page opened with auto_open=false: %d", id)
	}
	if err := r.h.Run("welcome_screen.show", nil); err != nil {
		t.Fatal(err)
	}
	if welcomeScreenBuffer(r.host) == 0 {
		t.Fatal("explicit show did not open the page")
	}
}

func TestWelcomeScreenDashboardSectionRows(t *testing.T) {
	provider := "local gvim = require 'gvim'\ngvim.dashboard.register_section('demo', function() return {} end, { title = 'Demo Section', ttl_ms = 1000 })\n"
	r := welcomeScreenRig(t, rigOpts{
		extra:  map[string]string{"demo/init.lua": provider},
		script: func(o *plugintest.FakeOS) { o.SetDir("/work") },
	})
	if err := r.h.Run("welcome_screen.show", nil); err != nil {
		t.Fatal(err)
	}
	text := welcomeScreenText(t, r.host)
	if !strings.Contains(text, "Demo Section") || !strings.Contains(text, "Open the dashboard") {
		t.Fatalf("dashboard-driven section row missing:\n%s", text)
	}
	if strings.Contains(text, "No dashboard sections registered.") {
		t.Fatalf("empty dashboard state shown despite a registered section:\n%s", text)
	}
}

func TestWelcomeScreenDoorActionIsReal(t *testing.T) {
	r := welcomeScreenRig(t, rigOpts{script: func(o *plugintest.FakeOS) { o.SetDir("/work") }})
	if err := r.h.Run("welcome_screen.show", nil); err != nil {
		t.Fatal(err)
	}
	if err := r.h.Run(welcomeScreenPluginName+".click_action_1", nil); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, action := range r.host.Cmd().Ran() {
		if action == "open" {
			found = true
		}
	}
	if !found {
		t.Fatalf("commands = %v, want open from the Open file door", r.host.Cmd().Ran())
	}
}

func TestWelcomeScreenRecentRowActivationUsesItsPath(t *testing.T) {
	r := welcomeScreenRig(t, rigOpts{
		buffers: []rigBuffer{{"/work/a.go", "package a\n"}, {"/work/b.go", "package b\n"}},
		script:  func(o *plugintest.FakeOS) { o.SetDir("/work") },
	})
	if err := r.h.Run("welcome_screen.show", nil); err != nil {
		t.Fatal(err)
	}
	// Rows: three doors, then the recent list. Move to the first recent row and activate it;
	// the activation must use that row's path, not the global row index.
	for range 3 {
		if err := r.h.Run("welcome_screen.focus_next", nil); err != nil {
			t.Fatal(err)
		}
	}
	if err := r.h.Run("welcome_screen.activate", nil); err != nil {
		t.Fatal(err)
	}
	r.h.CompleteBuffers()
	r.h.Settle()
	if got := r.host.ActiveBuffer(); got != r.ids["/work/b.go"] {
		t.Fatalf("active buffer = %d, want /work/b.go (first seeded recent row)", got)
	}
}

func TestWelcomeScreenCloseIsSticky(t *testing.T) {
	r := welcomeScreenRig(t, rigOpts{
		buffers: []rigBuffer{{"/work/a.go", "package a\n"}},
		script:  func(o *plugintest.FakeOS) { o.SetDir("/work") },
	})
	r.h.Emit(pluginapi.Event{Name: "plugins_loaded"})
	r.h.Settle()
	if welcomeScreenBuffer(r.host) == 0 {
		t.Fatal("welcome page did not auto-open")
	}
	if err := r.h.Run("welcome_screen.close", nil); err != nil {
		t.Fatal(err)
	}
	r.h.Emit(pluginapi.Event{Name: "ready"})
	r.h.Emit(pluginapi.Event{Name: "after_file_open", Data: map[string]any{"buf": r.ids["/work/a.go"], "path": "/work/a.go"}})
	r.h.Settle()
	if id := welcomeScreenBuffer(r.host); id != 0 {
		t.Fatalf("welcome page reopened after the reader closed it: %d", id)
	}
}

func TestWelcomeScreenSanitizesHostilePathsAndSectionTitles(t *testing.T) {
	hostilePath := "/work/evil\nDASHBOARD.md\x1b[31m\u202e.txt"
	provider := "local gvim = require 'gvim'\ngvim.dashboard.register_section('demo', function() return {} end, { title = 'Evil\\nSection\u202e', ttl_ms = 1000 })\n"
	r := welcomeScreenRig(t, rigOpts{
		buffers: []rigBuffer{{hostilePath, "notes\n"}},
		extra:   map[string]string{"demo/init.lua": provider},
		script:  func(o *plugintest.FakeOS) { o.SetDir("/work") },
	})
	if err := r.h.Run("welcome_screen.show", nil); err != nil {
		t.Fatal(err)
	}
	id := welcomeScreenBuffer(r.host)
	entries, _ := r.host.VirtualContent(id)
	lineText := func(e pluginapi.ContentEntry) string {
		if len(e.Segments) == 0 {
			return e.Text
		}
		var b strings.Builder
		for _, seg := range e.Segments {
			b.WriteString(seg.Text)
		}
		return b.String()
	}
	recentLine, dashboardLine := -1, -1
	for i, entry := range entries {
		line := lineText(entry)
		if strings.Contains(line, "evil DASHBOARD.md") {
			recentLine = i
		}
		if strings.Contains(line, "Evil Section") {
			dashboardLine = i
		}
		if strings.Contains(line, "DASHBOARD.md\x1b") || strings.Contains(line, "Evil\nSection") ||
			strings.ContainsRune(line, '\u202e') {
			t.Fatalf("hostile control/newline reached a page line: %q", line)
		}
	}
	if recentLine < 0 || dashboardLine < 0 {
		t.Fatalf("sanitized rows missing (recent %d dashboard %d): entries %d", recentLine, dashboardLine, len(entries))
	}
	var recentTarget, dashboardTarget *pluginapi.LineTarget
	for i := range r.host.LineTargets(id) {
		target := &r.host.LineTargets(id)[i]
		if target.Path == hostilePath {
			recentTarget = target
		}
		if target.Action == welcomeScreenPluginName+".click_dashboard" {
			dashboardTarget = target
		}
	}
	if recentTarget == nil || recentTarget.Line != recentLine {
		t.Fatalf("recent target = %+v, want line %d", recentTarget, recentLine)
	}
	if dashboardTarget == nil || dashboardTarget.Line != dashboardLine {
		t.Fatalf("dashboard target = %+v, want line %d", dashboardTarget, dashboardLine)
	}
}

// TestWelcomeScreenFocusRevealsTheSelectedRow: j/k on a page taller than a small viewport
// moves the buffer caret to the selected action row with reveal=true, so the editor
// scrolls that row into view before Enter can activate it.
func TestWelcomeScreenFocusRevealsTheSelectedRow(t *testing.T) {
	r := welcomeScreenRig(t, rigOpts{
		buffers: []rigBuffer{{"/work/a.go", "package a\n"}, {"/work/b.go", "package b\n"}},
		script:  func(o *plugintest.FakeOS) { o.SetDir("/work") },
	})
	if err := r.h.Run("welcome_screen.show", nil); err != nil {
		t.Fatal(err)
	}
	id := welcomeScreenBuffer(r.host)
	entries, _ := r.host.VirtualContent(id)
	lineText := func(e pluginapi.ContentEntry) string {
		if len(e.Segments) == 0 {
			return e.Text
		}
		var b strings.Builder
		for _, seg := range e.Segments {
			b.WriteString(seg.Text)
		}
		return b.String()
	}
	recentLine := -1
	for i, entry := range entries {
		if strings.Contains(lineText(entry), "b.go") {
			recentLine = i
		}
	}
	if recentLine < 0 {
		t.Fatalf("recent b.go row not found:\n%s", welcomeScreenText(t, r.host))
	}
	before := len(r.host.Calls())
	for range 3 {
		if err := r.h.Run("welcome_screen.focus_next", nil); err != nil {
			t.Fatal(err)
		}
	}
	var lastSet *plugintest.Call
	for _, call := range r.host.Calls()[before:] {
		if call.Method == "SetCursor" {
			c := call
			lastSet = &c
		}
	}
	if lastSet == nil {
		t.Fatal("focus_next did not move the buffer caret to reveal the row")
	}
	pos, ok := lastSet.Args[1].(pluginapi.Pos)
	if !ok || pos.Line != recentLine {
		t.Fatalf("SetCursor line = %+v, want %d", lastSet.Args[1], recentLine)
	}
	if reveal, _ := lastSet.Args[3].(bool); !reveal {
		t.Fatal("SetCursor did not request reveal")
	}
	if center, _ := lastSet.Args[4].(bool); center {
		t.Fatal("SetCursor unexpectedly centered the row")
	}
	if bufID, _ := lastSet.Args[0].(pluginapi.BufferID); bufID != id {
		t.Fatalf("SetCursor buffer = %d, want the welcome buffer %d", bufID, id)
	}
}

// TestWelcomeScreenFocusDoesNotRevealInBackground: the reveal is skipped while another
// buffer is current, so a background welcome page cannot steal focus or scroll.
func TestWelcomeScreenFocusDoesNotRevealInBackground(t *testing.T) {
	r := welcomeScreenRig(t, rigOpts{
		buffers: []rigBuffer{{"/work/a.go", "package a\n"}},
		script:  func(o *plugintest.FakeOS) { o.SetDir("/work") },
	})
	if err := r.h.Run("welcome_screen.show", nil); err != nil {
		t.Fatal(err)
	}
	fileID := r.ids["/work/a.go"]
	r.host.SetActive(fileID)
	before := len(r.host.Calls())
	if err := r.h.Run("welcome_screen.focus_next", nil); err != nil {
		t.Fatal(err)
	}
	for _, call := range r.host.Calls()[before:] {
		if call.Method == "SetCursor" {
			t.Fatalf("background focus moved a caret: %v", call)
		}
	}
	if got := r.host.ActiveBuffer(); got != fileID {
		t.Fatalf("active buffer = %d, want the file %d", got, fileID)
	}
}
