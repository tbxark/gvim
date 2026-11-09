package bundled_test

import (
	"encoding/json"
	"io/fs"
	"strings"
	"testing"
	"time"

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

// welcomeScreenHarness starts one runtime directly from the bundled plugin files. The
// caller can seed gvim.state before Start and Stop the runtime explicitly, so persistence
// can be checked across two runtimes rather than within one in-memory session.
func welcomeScreenHarness(t *testing.T, osys *plugintest.FakeOS, seed []byte) *plugintest.Harness {
	t.Helper()
	init, err := fs.ReadFile(bundled.FS, "plugins/welcome_screen/init.lua")
	if err != nil {
		t.Fatal(err)
	}
	i18n, err := fs.ReadFile(bundled.FS, "plugins/welcome_screen/welcome_screen.i18n.json")
	if err != nil {
		t.Fatal(err)
	}
	files := plugintest.Files{
		"welcome_screen/init.lua":                 string(init),
		"welcome_screen/welcome_screen.i18n.json": string(i18n),
	}
	h := plugintest.New(t, files, plugintest.Options{
		Host: plugintest.NewFakeHost(), OS: osys, Timeout: time.Second, EventBudget: 5 * time.Second,
	})
	if seed != nil {
		if err := h.RT.SeedState(welcomeScreenPluginName, seed); err != nil {
			t.Fatal(err)
		}
	}
	h.Start()
	return h
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
	if !commands["welcome_screen.show"] || !commands["welcome_screen.toggle_startup"] {
		t.Fatalf("visible commands = %v, want show and toggle_startup", commands)
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
		"File explorer", "Help", "[✓] Show this screen on startup",
		"Enter toggles it for future launches", "j/k or arrows move",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("welcome text lacks %q:\n%s", want, text)
		}
	}
	id := welcomeScreenBuffer(r.host)
	foundAction, foundPath, foundStartup := false, false, false
	for _, target := range r.host.LineTargets(id) {
		if strings.HasPrefix(target.Action, welcomeScreenPluginName+".click_action_") {
			foundAction = true
		}
		if target.Action == welcomeScreenPluginName+".toggle_startup" {
			foundStartup = true
		}
		if target.Path == "/work/a.go" {
			foundPath = true
		}
	}
	if !foundAction || !foundPath || !foundStartup {
		t.Fatalf("line targets = %+v, want action, startup and recent path targets", r.host.LineTargets(id))
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

// TestWelcomeScreenAutoOpenHonorsFileLaunch: the immutable file-launch property (a
// gvim FILE/--simple/--diff/stdin invocation) suppresses the automatic background page;
// the explicit Welcome command still opens it.
func TestWelcomeScreenAutoOpenHonorsFileLaunch(t *testing.T) {
	r := welcomeScreenRig(t, rigOpts{
		buffers:    []rigBuffer{{"/work/a.go", "package a\n"}},
		fileLaunch: true,
		script:     func(o *plugintest.FakeOS) { o.SetDir("/work") },
	})
	r.h.Emit(pluginapi.Event{Name: "plugins_loaded"})
	r.h.Emit(pluginapi.Event{Name: "ready"})
	r.h.Settle()
	if id := welcomeScreenBuffer(r.host); id != 0 {
		t.Fatalf("welcome page auto-opened for a file launch: %d", id)
	}
	if err := r.h.Run("welcome_screen.show", nil); err != nil {
		t.Fatal(err)
	}
	if id := welcomeScreenBuffer(r.host); id == 0 {
		t.Fatal("explicit Welcome command was blocked by the file-launch guard")
	}
}

func TestWelcomeScreenStartupTogglePersistsInThePage(t *testing.T) {
	r := welcomeScreenRig(t, rigOpts{script: func(o *plugintest.FakeOS) { o.SetDir("/work") }})
	if err := r.h.Run("welcome_screen.show", nil); err != nil {
		t.Fatal(err)
	}
	if text := welcomeScreenText(t, r.host); !strings.Contains(text, "[✓] Show this screen on startup") {
		t.Fatalf("default startup row missing:\n%s", text)
	}
	if err := r.h.Run("welcome_screen.toggle_startup", nil); err != nil {
		t.Fatal(err)
	}
	if text := welcomeScreenText(t, r.host); !strings.Contains(text, "[ ] Show this screen on startup") {
		t.Fatalf("startup row did not turn off:\n%s", text)
	}
	notices := r.h.NoticeTexts()
	if len(notices) == 0 || notices[len(notices)-1] != "Welcome: hidden - reopen via the palette" {
		t.Fatalf("notices = %q", notices)
	}
	// The visible command is the same action the page row targets.
	if err := r.h.Run("welcome_screen.toggle_startup", nil); err != nil {
		t.Fatal(err)
	}
	if text := welcomeScreenText(t, r.host); !strings.Contains(text, "[✓] Show this screen on startup") {
		t.Fatalf("startup row did not turn back on:\n%s", text)
	}
}

func TestWelcomeScreenStartupToggleControlsNextAutoOpen(t *testing.T) {
	r := welcomeScreenRig(t, rigOpts{script: func(o *plugintest.FakeOS) { o.SetDir("/work") }})
	// State override off (the config auto_open default is true) must suppress the page.
	if err := r.h.Run("welcome_screen.toggle_startup", nil); err != nil {
		t.Fatal(err)
	}
	r.h.Emit(pluginapi.Event{Name: "plugins_loaded"})
	r.h.Settle()
	if id := welcomeScreenBuffer(r.host); id != 0 {
		t.Fatalf("welcome page opened despite show_on_startup=false: %d", id)
	}
	// Turning it back on before the first automatic decision lets the next startup event open it.
	if err := r.h.Run("welcome_screen.toggle_startup", nil); err != nil {
		t.Fatal(err)
	}
	r.h.Emit(pluginapi.Event{Name: "ready"})
	r.h.Settle()
	if welcomeScreenBuffer(r.host) == 0 {
		t.Fatal("welcome page did not open after show_on_startup was turned back on")
	}
}

func TestWelcomeScreenStartupToggleOverridesAutoOpenSetting(t *testing.T) {
	r := welcomeScreenRig(t, rigOpts{
		settings: map[string]plugin.PluginSettings{welcomeScreenPluginName: {User: map[string]any{"auto_open": false}}},
		script:   func(o *plugintest.FakeOS) { o.SetDir("/work") },
	})
	r.h.Emit(pluginapi.Event{Name: "plugins_loaded"})
	r.h.Settle()
	if id := welcomeScreenBuffer(r.host); id != 0 {
		t.Fatalf("welcome page opened with auto_open=false before an override: %d", id)
	}
	// The persistent startup toggle can turn the page on even when the config setting is off.
	if err := r.h.Run("welcome_screen.toggle_startup", nil); err != nil {
		t.Fatal(err)
	}
	r.h.Emit(pluginapi.Event{Name: "ready"})
	r.h.Settle()
	if welcomeScreenBuffer(r.host) == 0 {
		t.Fatal("show_on_startup=true did not override auto_open=false")
	}
}

func TestWelcomeScreenStartupTogglePersistsAcrossStopAndNewRuntime(t *testing.T) {
	// First runtime: choose off, then Stop (the state writer's synchronous flush).
	os1 := plugintest.NewFakeOS()
	h1 := welcomeScreenHarness(t, os1, nil)
	if err := h1.Run("welcome_screen.toggle_startup", nil); err != nil {
		t.Fatal(err)
	}
	statePath := h1.RT.StateFile(welcomeScreenPluginName)
	if statePath == "" {
		t.Fatal("no state file path")
	}
	h1.RT.Stop()
	state1, ok := os1.File(statePath)
	if !ok || !strings.Contains(state1, "show_on_startup") || !strings.Contains(state1, "false") {
		t.Fatalf("persisted state = %q (ok %v), want show_on_startup=false", state1, ok)
	}

	// Second runtime seeded from that file: the persistent override suppresses startup.
	os2 := plugintest.NewFakeOS()
	os2.SetFile(statePath, state1)
	h2 := welcomeScreenHarness(t, os2, []byte(state1))
	h2.Emit(pluginapi.Event{Name: "plugins_loaded"})
	h2.Settle()
	if id := welcomeScreenBuffer(h2.Host); id != 0 {
		t.Fatalf("second runtime opened the page despite persisted false: %d", id)
	}
	// Turn it back on; Stop flushes true for the third runtime.
	if err := h2.Run("welcome_screen.toggle_startup", nil); err != nil {
		t.Fatal(err)
	}
	h2.RT.Stop()
	state2, ok := os2.File(statePath)
	if !ok || !strings.Contains(state2, "true") {
		t.Fatalf("second persisted state = %q (ok %v), want true", state2, ok)
	}

	// Third runtime: persisted true opens the background page on plugins_loaded.
	os3 := plugintest.NewFakeOS()
	os3.SetFile(statePath, state2)
	h3 := welcomeScreenHarness(t, os3, []byte(state2))
	h3.Emit(pluginapi.Event{Name: "plugins_loaded"})
	h3.Settle()
	if welcomeScreenBuffer(h3.Host) == 0 {
		t.Fatal("third runtime did not open the page despite persisted true")
	}
}
