package plugin_test

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/tbxark/gvim/plugin"
	"github.com/tbxark/gvim/plugin/plugintest"
	"github.com/tbxark/gvim/pluginapi"
)

type stateJSON struct {
	Enabled  []string `json:"enabled"`
	Disabled []string `json:"disabled"`
}

func (e *env) state() stateJSON {
	e.t.Helper()
	var s stateJSON
	if err := json.Unmarshal([]byte(e.os.file(stateFile)), &s); err != nil {
		e.t.Fatalf("plugin-state.json = %q: %v", e.os.file(stateFile), err)
	}
	return s
}

func TestDisableAndEnableStopAndRestartDependants(t *testing.T) {
	e := newEnv(t)
	e.bundle("base", map[string]string{"init.lua": `T.register_all()`})
	e.bundle("dep", map[string]string{"plugin.json": `{"requires": ["base"]}`, "init.lua": `T.register_all()`})
	e.bundle("other", map[string]string{"init.lua": marker("other")})
	e.start()

	n := e.control("disable", "base")
	if len(n) != 1 || n[0].Text != "plugin 'base' disabled (also stopped: dep)" {
		t.Fatalf("disable notices = %+v", n)
	}
	if got := e.state(); !reflect.DeepEqual(got, stateJSON{Enabled: []string{}, Disabled: []string{"base"}}) {
		t.Fatalf("state = %+v", got)
	}
	if p := e.info("base"); p.Status != "disabled" || p.Enabled {
		t.Fatalf("base = %+v", p)
	}
	if p := e.info("dep"); p.Status != "failed" || !strings.Contains(p.Err, `requires "base" (disabled)`) {
		t.Fatalf("dep = %+v", p)
	}
	if e.info("other").Status != "loaded" {
		t.Fatal("an unrelated plugin was stopped")
	}
	for _, r := range e.h.Host.Resources() {
		if strings.Contains(r, "base") || strings.Contains(r, "dep") {
			t.Fatalf("resource %q survived the disable: %v", r, e.h.Host.Resources())
		}
	}
	if n := e.control("disable", "base"); len(n) != 1 || !strings.Contains(n[0].Text, "already disabled") {
		t.Errorf("second disable = %+v", n)
	}

	e.control("enable", "base")
	if p := e.info("base"); p.Status != "loaded" || !p.Enabled {
		t.Fatalf("base after enable = %+v (notices %v)", p, e.notices())
	}
	if p := e.info("dep"); p.Status != "loaded" || p.Err != "" {
		t.Fatalf("dep after enable = %+v", p)
	}
	if got := e.state(); !reflect.DeepEqual(got, stateJSON{Enabled: []string{"base"}, Disabled: []string{}}) {
		t.Fatalf("state = %+v", got)
	}
	if !e.noticeHas("Plugin 'base' enabled") {
		t.Fatalf("notices = %v", e.notices())
	}
	if n := e.control("enable", "base"); len(n) != 1 || !strings.Contains(n[0].Text, "already enabled") {
		t.Errorf("second enable = %+v", n)
	}
	// The manager's choice survives a restart.
	e2 := newEnv(t)
	e2.bundled = e.bundled
	e2.opts.Bundled = e.bundled
	e2.os.files[stateFile] = []byte(e.os.file(stateFile))
	e2.start()
	if e2.info("base").Status != "loaded" || e2.info("dep").Status != "loaded" {
		t.Fatalf("rows after restart: %v", e2.names())
	}
}

func TestToggleRefusals(t *testing.T) {
	e := newEnv(t)
	e.user("pinned.lua", marker("pinned"))
	e.user("free.lua", marker("free"))
	e.disk.put("/dev/mine.lua", marker("mine"))
	e.disk.put(initFile, marker("init"))
	e.opts.Paths = []string{"/dev/mine.lua"}
	e.opts.Enabled = map[string]bool{"pinned": true}
	e.start()
	rows := []struct{ op, name, want string }{
		{"disable", "pinned", "set in config"},
		{"enable", "pinned", "set in config"},
		{"disable", "mine", "--plugin"},
		{"disable", "init", "init.lua"},
		{"enable", "ghost", "is not installed"},
		{"disable", "ghost", "is not loaded"},
		{"enable", "free", "already enabled"},
	}
	for _, r := range rows {
		n := e.control(r.op, r.name)
		if len(n) != 1 || !strings.Contains(n[0].Text, r.want) {
			t.Errorf("%s %s: %+v, want %q", r.op, r.name, n, r.want)
		}
	}
	for _, name := range []string{"pinned", "mine", "init", "free"} {
		if e.info(name).Status != "loaded" {
			t.Errorf("%s was stopped by a refused toggle", name)
		}
	}
	if e.os.writeCount() != 0 {
		t.Errorf("a refused toggle wrote the state file")
	}
}

func TestStateWriterIsSerialAndKeepsTheLatest(t *testing.T) {
	e := newEnv(t)
	for _, n := range []string{"a", "b", "c"} {
		e.user(n+".lua", marker(n))
	}
	e.start()
	e.os.delay = 20 * time.Millisecond
	h := e.h
	var work []pluginapi.Work
	for _, n := range []string{"a", "b", "c"} {
		w, _ := h.RT.Control(pluginapi.Control{Op: "disable", Name: n})
		work = append(work, w...)
	}
	if len(work) != 1 {
		t.Fatalf("three toggles in a row started %d writes, want one in flight and the rest coalesced", len(work))
	}
	h.RunWork(work)
	h.Settle()
	if e.os.maxIn != 1 {
		t.Fatalf("%d writes ran at once", e.os.maxIn)
	}
	if got := e.state(); !reflect.DeepEqual(got.Disabled, []string{"a", "b", "c"}) {
		t.Fatalf("final state = %+v: a toggle was lost", got)
	}
	if e.os.writeCount() != 2 {
		t.Fatalf("%d writes, want 2 (the first and one for everything after it)", e.os.writeCount())
	}
}

func TestStateWriteFailureIsReported(t *testing.T) {
	e := newEnv(t)
	e.user("a.lua", marker("a"))
	e.start()
	e.os.failNext = errors.New("disk full")
	e.control("disable", "a")
	if !e.noticeHas("cannot save plugin-state.json") {
		t.Fatalf("notices = %v", e.notices())
	}
	if e.info("a").Status != "disabled" {
		t.Error("the toggle itself must still apply")
	}
}

func TestStopFlushesToggleThatNeverReachedTheDisk(t *testing.T) {
	e := newEnv(t)
	e.user("a.lua", marker("a"))
	e.start()
	work, _ := e.h.RT.Control(pluginapi.Control{Op: "disable", Name: "a"})
	if len(work) != 1 || e.os.writeCount() != 0 {
		t.Fatalf("work %d, writes %d", len(work), e.os.writeCount())
	}
	e.h.RT.Stop() // the Work never ran
	if got := e.state(); !slices.Equal(got.Disabled, []string{"a"}) {
		t.Fatalf("state after Stop = %+v", got)
	}
}

func TestInitCheckAndReload(t *testing.T) {
	e := newEnv(t)
	e.start()
	for _, op := range []string{"init_check", "init_reload"} {
		plugin.ResetSpy(e.h.RT)
		e.control(op, "")
		if !e.noticeHas("init.lua: ") {
			t.Fatalf("%s with no init.lua: notices %v", op, e.notices())
		}
	}
	if _, ok := e.h.Plugin("init"); ok {
		t.Fatal("init appeared from nowhere")
	}

	// A new init.lua is picked up by init_reload even though startup did not see it.
	e.disk.put(initFile, `T.on_unload(function() T.spy("unload init v1") end) T.spy("init v1")`)
	if e.control("init_check", ""); !e.noticeHas("init.lua: syntax OK") {
		t.Fatalf("notices = %v", e.notices())
	}
	e.control("init_reload", "")
	if p := e.info("init"); p.Status != "loaded" || p.Source != "init" {
		t.Fatalf("init = %+v", p)
	}
	e.disk.put(initFile, `T.spy("init v2")`)
	plugin.ResetSpy(e.h.RT)
	e.control("init_reload", "")
	if got := e.spyLines(); !slices.Equal(got, []string{"unload init v1", "init v2"}) {
		t.Fatalf("spy = %v", got)
	}

	// A syntax error is reported with its line, and neither check nor reload disturbs v2.
	e.disk.put(initFile, "T.spy('init v3')\nlocal x = = 1")
	e.control("init_check", "")
	if !e.noticeHas("syntax error near '=' (init/init.lua:2:11)") {
		t.Fatalf("notices = %v", e.notices())
	}
	plugin.ResetSpy(e.h.RT)
	e.control("init_reload", "")
	if len(e.spyLines()) != 0 || e.info("init").Status != "loaded" || !e.noticeHas("reload aborted") {
		t.Fatalf("spy %v, init %+v, notices %v", e.spyLines(), e.info("init"), e.notices())
	}
}

func TestReloadAllFollowsTheDisk(t *testing.T) {
	e := newEnv(t)
	e.user("a.lua", `T.command("v", function() T.spy("a1") end)`)
	e.user("b.lua", `T.command("v", function() T.spy("b1") end)`)
	e.user("c.lua", marker("c"))
	e.start()
	e.loadText("scratch", `T.command("v", function() T.spy("scratch") end)`)

	e.user("a.lua", `T.command("v", function() T.spy("a2") end)`)
	e.user("b.lua", `T.command("v", function() T.spy("b2") end) +`)
	e.disk.remove(userDir + "/c.lua")
	e.user("d.lua", `T.command("v", function() T.spy("d1") end)`)
	e.control("reload_all", "")

	for action, want := range map[string]string{"a.v": "a2", "b.v": "b1", "d.v": "d1", "scratch.v": "scratch"} {
		e.run(action)
		if got := e.spyLines(); !slices.Equal(got, []string{want}) {
			t.Errorf("%s ran %v, want %s", action, got, want)
		}
	}
	if _, ok := e.h.Plugin("c"); ok {
		t.Errorf("c is gone from the disk but still listed: %v", e.names())
	}
	if e.goneCount("c") == 0 {
		t.Error("c was not unloaded")
	}
	if !e.noticeHas(`plugin 'b': reload aborted`) {
		t.Errorf("notices = %v", e.notices())
	}
}

func TestControlWithoutLoader(t *testing.T) {
	h := plugintest.New(t, plugintest.Files{"t.lua": `T.spy("t")`}, plugintest.Options{})
	h.Start()
	for _, op := range []string{"reload_all", "enable", "init_check", "init_reload"} {
		n := h.Control(pluginapi.Control{Op: op, Name: "t"})
		if len(n) != 1 || !n[0].Warn || !strings.Contains(n[0].Text, "no plugin loader") {
			t.Errorf("%s: %+v", op, n)
		}
	}
	if n := h.Control(pluginapi.Control{Op: "bogus"}); len(n) != 1 || !strings.Contains(n[0].Text, "unknown") {
		t.Errorf("bogus: %+v", n)
	}
}

func TestBundledPlaceholdersAreNotPlugins(t *testing.T) {
	e := newEnv(t)
	// A bundled tree that holds only the README placeholders (the binary's own tree has
	// plugins since the app wiring added the first two ports).
	e.opts.Bundled = fstest.MapFS{
		"plugins/README.md": {Data: []byte("placeholder")},
		"lib/README.md":     {Data: []byte("placeholder")},
	}
	e.start()
	if rows := e.h.RT.Plugins(); len(rows) != 0 || len(e.notices()) != 0 {
		t.Fatalf("rows %v notices %v", rows, e.notices())
	}
}

// TestBinaryBundledPluginsLoad: the plugins embedded in the binary are found, load without a
// notice, and are enabled unless their manifest or the configuration says otherwise.
func TestBinaryBundledPluginsLoad(t *testing.T) {
	e := newEnv(t)
	e.opts.Bundled = nil
	e.start()
	// Plugins whose plugin.json says default_enabled: false stay off until the user turns them on.
	// live_diff is opt-in like Fresh's; vi_mode ships with "default_enabled": false too, and
	// git_log is off for the idle-heap gate (it pushed the default set over 8 MiB).
	offByDefault := map[string]bool{
		"env_manager": true, "flash": true, "vi_mode": true, "live_diff": true, "git_log": true, "compare_files": true, "lsp_navigation": true, "find_references": true, "dashboard": true, "code_tour": true, "merge_conflict": true, "git_grep": true, "git_explorer": true, "asm_lsp": true, "csharp_support": true, "clangd_support": true}
	var names []string
	for _, r := range e.h.RT.Plugins() {
		switch {
		case r.Source != plugin.SourceBundled:
			t.Errorf("row %+v, want a bundled plugin", r)
		case offByDefault[r.Name]:
			if r.Status != "disabled" || r.Enabled {
				t.Errorf("bundled row %+v, want a disabled plugin (default_enabled is false)", r)
			}
		case r.Status != "loaded" || !r.Enabled:
			t.Errorf("bundled row %+v, want an enabled, loaded bundled plugin", r)
		}
		names = append(names, r.Name)
	}
	for _, want := range []string{"goto_with_selection", "tab_actions", "flash", "merge_conflict", "vi_mode"} {
		if !slices.Contains(names, want) {
			t.Errorf("bundled plugins = %v, want %s among them", names, want)
		}
	}
	if len(e.notices()) != 0 {
		t.Errorf("notices = %v", e.notices())
	}
}

func TestUnloadThroughLoaderLeavesNothingBehind(t *testing.T) {
	// Baseline: one plugin that never changes.
	e := newEnv(t)
	e.user("base.lua", marker("base"))
	e.start()
	baseRes := e.h.Host.Resources()
	baseReg := plugin.RegistryOf(e.h.RT)

	// A plugin discovered by the loader that registers every kind of resource and parks tasks.
	e.user("leaky.lua", leaky)
	e.control("reload_all", "")
	for _, c := range []string{"park", "park", "susp", "spawn"} {
		if err := e.h.Run("leaky."+c, nil); err != nil {
			t.Fatal(err)
		}
	}
	if res := e.h.Host.Resources(); len(res) <= len(baseRes) {
		t.Fatalf("setup: resources = %v", res)
	}
	tokens := plugin.ParkedTokens(e.h.RT)

	// It leaves the disk, and reload_all unloads it.
	e.disk.remove(userDir + "/leaky.lua")
	e.control("reload_all", "")

	if got := e.h.Host.Resources(); !reflect.DeepEqual(got, baseRes) {
		t.Fatalf("host resources = %v, before = %v", got, baseRes)
	}
	if got := plugin.RegistryOf(e.h.RT); got != baseReg {
		t.Fatalf("registry = %+v, before = %+v", got, baseReg)
	}
	before := len(e.spyLines())
	for _, tok := range tokens {
		e.h.Resolve(pluginapi.Completion{Token: tok, Values: []any{"late"}})
	}
	if after := e.spyLines(); len(after) != before {
		t.Fatalf("a late completion woke a dead task: %v", after[before:])
	}

	// Disabling through the manager releases the same things (a disabled row remains).
	e.user("leaky.lua", leaky)
	e.control("reload_all", "")
	e.control("disable", "leaky")
	if got := e.h.Host.Resources(); !reflect.DeepEqual(got, baseRes) {
		t.Fatalf("after disable: host resources = %v, before = %v", got, baseRes)
	}
	reg := plugin.RegistryOf(e.h.RT)
	reg.Plugins--
	if reg != baseReg {
		t.Fatalf("after disable: registry = %+v, before = %+v", reg, baseReg)
	}
}

func TestUnloadWhileMainChunkIsParkedFailsDependantsAndLeaksNoWait(t *testing.T) {
	e := newEnv(t)
	e.user("slow.lua", `T.register_all() T.host() T.spy("slow done")`)
	e.user("child/plugin.json", `{"requires": ["slow"]}`)
	e.user("child/init.lua", marker("child"))
	e.user("late.lua", `T.on("plugins_loaded", function() T.spy("plugins_loaded") end)`)
	e.start()
	if e.info("slow").Status != "loading" {
		t.Fatalf("rows = %v", e.names())
	}
	e.h.RT.UnloadPlugin("slow")
	e.h.Settle()

	if p := e.info("child"); p.Status != "failed" || !strings.Contains(p.Err, `requires "slow", which failed to load`) {
		t.Fatalf("child = %+v", p)
	}
	if !slices.Contains(e.spyLines(), "plugins_loaded") {
		t.Fatalf("the batch never finished: %v", e.spyLines())
	}
	if reg := plugin.RegistryOf(e.h.RT); reg.Waits != 0 {
		t.Fatalf("registry = %+v", reg)
	}
}

func TestDefaultOpenRefusesSymlinksOutOfThePluginDirectory(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks")
	}
	tmp := t.TempDir()
	write := func(path, src string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	link := func(target, name string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(name), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(target, name); err != nil {
			t.Skipf("symlink: %v", err)
		}
	}
	plugins := filepath.Join(tmp, "plugins")
	write(filepath.Join(tmp, "outside", "secret.lua"), `T.spy("SECRET ran") return {}`)
	write(filepath.Join(tmp, "outside", "dotfile.lua"), `T.spy("dotfile ran")`)
	// A module that is a symlink out of the plugin directory.
	write(filepath.Join(plugins, "evil", "init.lua"), `T.spy("evil main") require "mod"`)
	link(filepath.Join(tmp, "outside", "secret.lua"), filepath.Join(plugins, "evil", "lua", "mod.lua"))
	// A single-file plugin that is a symlink to a dotfiles checkout loads: the user put the
	// link there, and only directory plugins are confined.
	link(filepath.Join(tmp, "outside", "dotfile.lua"), filepath.Join(plugins, "dots.lua"))
	// A directory plugin that is a symlink to a checkout elsewhere: the development flow.
	write(filepath.Join(tmp, "dev", "linked", "init.lua"), `T.spy("linked ran")`)
	link(filepath.Join(tmp, "dev", "linked"), filepath.Join(plugins, "linked"))
	write(filepath.Join(plugins, "plain.lua"), `T.spy("plain ran")`)

	h := plugintest.New(t, nil, plugintest.Options{})
	rt := plugin.New(plugin.Options{Timeout: 100 * time.Millisecond, CheckGoroutine: true, Now: h.Now,
		Loader: &plugin.LoaderOptions{Bundled: nil, UserDir: plugins}})
	t.Cleanup(rt.Stop)
	h.RT = rt
	h.Start()
	h.Settle()

	got := plugin.Spy(rt)
	for _, want := range []string{"plain ran", "linked ran", "evil main", "dotfile ran"} {
		if !slices.Contains(got, want) {
			t.Errorf("%q missing from %v", want, got)
		}
	}
	if slices.Contains(got, "SECRET ran") {
		t.Fatalf("a symlink led out of the plugin directory: %v", got)
	}
	if p, _ := h.Plugin("evil"); p.Status != "failed" {
		t.Errorf("evil = %+v", p)
	}
	if p, _ := h.Plugin("dots"); p.Status != "loaded" {
		t.Errorf("dots = %+v", p)
	}
}

// TestReloadAllSaysWhenAReloadWasAborted: the last notice of Reload All must not claim a clean
// success when a plugin's reload was aborted by a syntax error; it names the plugin and the file.
func TestReloadAllSaysWhenAReloadWasAborted(t *testing.T) {
	e := newEnv(t)
	e.user("a.lua", `T.command("v", function() T.spy("a1") end)`)
	e.user("b.lua", `T.command("v", function() T.spy("b1") end)`)
	e.start()
	e.user("b.lua", `T.command("v", function() T.spy("b2") end) +`)
	e.control("reload_all", "")
	last := e.h.Host.Notices[len(e.h.Host.Notices)-1]
	if last.Level != pluginapi.LevelWarn {
		t.Errorf("the last notice has level %q, want warn: %q", last.Level, last.Msg)
	}
	for _, want := range []string{"plugin 'b'", "reload aborted", "b/b.lua"} {
		if !strings.Contains(last.Msg, want) {
			t.Errorf("the last notice %q lacks %q", last.Msg, want)
		}
	}
	if strings.HasPrefix(last.Msg, "2 plugin(s) loaded") {
		t.Errorf("the last notice starts with a success count: %q", last.Msg)
	}
}
