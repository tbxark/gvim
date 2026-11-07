package plugin_test

import (
	"io/fs"
	"slices"
	"strings"
	"testing"

	"github.com/tbxark/gvim/internal/plugin"
	"github.com/tbxark/gvim/internal/pluginapi"
)

func (e *env) goneCount(name string) int {
	n := 0
	for _, g := range e.h.Host.Gone {
		if g.Plugin == name {
			n++
		}
	}
	return n
}

func (e *env) control(op, name string) []pluginapi.Notice {
	e.t.Helper()
	return e.h.Control(pluginapi.Control{Op: op, Name: name})
}

func (e *env) loadText(stem, text string) []pluginapi.Notice {
	e.t.Helper()
	return e.h.Control(pluginapi.Control{Op: "load_text", Name: stem, Text: text})
}

func (e *env) run(action string) {
	e.t.Helper()
	plugin.ResetSpy(e.h.RT)
	if err := e.h.Run(action, nil); err != nil {
		e.t.Fatalf("run %s: %v", action, err)
	}
}

func TestReloadPicksUpNewSourceAndUnloadsTheOldOne(t *testing.T) {
	e := newEnv(t)
	e.user("foo.lua", `T.on_unload(function() T.spy("unload v1") end) T.command("v", function() T.spy("v1") end)`)
	e.start()
	e.user("foo.lua", `T.command("v", function() T.spy("v2") end)`)
	plugin.ResetSpy(e.h.RT)
	if n := e.control("reload", "foo"); len(n) != 0 {
		t.Fatalf("sync notices = %v", n)
	}
	if got := e.spyLines(); !slices.Equal(got, []string{"unload v1"}) {
		t.Fatalf("spy = %v", got)
	}
	e.run("foo.v")
	if got := e.spyLines(); !slices.Equal(got, []string{"v2"}) {
		t.Fatalf("after reload the command ran %v", got)
	}
	if e.goneCount("foo") != 1 {
		t.Fatalf("OwnerGone for foo called %d times", e.goneCount("foo"))
	}
	if !e.noticeHas("Plugin 'foo' reloaded") {
		t.Fatalf("notices = %v", e.notices())
	}
}

func TestReloadCompileCheckCoversAllFiles(t *testing.T) {
	e := newEnv(t)
	e.user("foo/init.lua", `T.command("v", function() T.spy("v1") end)`)
	e.user("foo/lua/foo/unused.lua", `return {}`)
	e.start()
	gone := e.goneCount("foo")
	before := e.info("foo")

	// A syntax error in a file the entry never requires still aborts the reload.
	e.user("foo/init.lua", `T.command("v", function() T.spy("v2") end)`)
	e.user("foo/lua/foo/unused.lua", "return {")
	e.control("reload", "foo")
	if !e.noticeHas("reload aborted") || !e.noticeHas("foo/lua/foo/unused.lua") {
		t.Fatalf("notices = %v", e.notices())
	}
	e.run("foo.v")
	if got := e.spyLines(); !slices.Equal(got, []string{"v1"}) {
		t.Fatalf("the old version must keep running, ran %v", got)
	}
	if e.goneCount("foo") != gone || e.info("foo").Status != "loaded" {
		t.Fatalf("the plugin was unloaded: %+v", e.info("foo"))
	}

	// Fixed: the reload goes through.
	e.user("foo/lua/foo/unused.lua", `return {}`)
	e.control("reload", "foo")
	e.run("foo.v")
	if got := e.spyLines(); !slices.Equal(got, []string{"v2"}) || e.goneCount("foo") != gone+1 {
		t.Fatalf("after the fix: spy %v, gone %d", got, e.goneCount("foo"))
	}
	_ = before
}

func TestReloadSyntaxErrorInRequiredModuleKeepsOld(t *testing.T) {
	e := newEnv(t)
	e.user("foo/init.lua", `local u = require "foo.util" T.command("v", function() T.spy(u.value) end)`)
	e.user("foo/lua/foo/util.lua", `return {value = "old"}`)
	e.start()
	e.user("foo/lua/foo/util.lua", `return {value = = "new"}`)
	e.control("reload", "foo")
	if !e.noticeHas("foo/lua/foo/util.lua") || !e.noticeHas("the running version is kept") {
		t.Fatalf("notices = %v", e.notices())
	}
	e.run("foo.v")
	if got := e.spyLines(); !slices.Equal(got, []string{"old"}) {
		t.Fatalf("ran %v", got)
	}
}

func TestReloadRuntimeErrorLeavesPluginFailedAndRolledBack(t *testing.T) {
	e := newEnv(t)
	e.user("foo.lua", `T.register_all()`)
	e.start()
	if len(e.h.Host.Resources()) == 0 {
		t.Fatal("setup: nothing registered")
	}
	e.user("foo.lua", `T.register_all() error("v2 broke")`)
	e.control("reload", "foo")
	p := e.info("foo")
	if p.Status != "failed" || !strings.Contains(p.Err, "v2 broke") {
		t.Fatalf("foo = %+v", p)
	}
	if res := e.h.Host.Resources(); len(res) != 0 {
		t.Fatalf("registrations of the failed version are still there: %v", res)
	}
	if !e.noticeHas(`plugin 'foo': `) {
		t.Fatalf("notices = %v", e.notices())
	}
}

func TestReloadAbortsWhenNewManifestNeedsAMissingPlugin(t *testing.T) {
	e := newEnv(t)
	e.user("foo/init.lua", `T.command("v", function() T.spy("v1") end)`)
	e.start()
	e.user("foo/plugin.json", `{"requires": ["ghost"]}`)
	e.control("reload", "foo")
	if !e.noticeHas(`requires "ghost" (not installed)`) {
		t.Fatalf("notices = %v", e.notices())
	}
	e.run("foo.v")
	if got := e.spyLines(); !slices.Equal(got, []string{"v1"}) {
		t.Fatalf("ran %v", got)
	}
}

func TestReloadRefusals(t *testing.T) {
	e := newEnv(t)
	e.user("off.lua", `T.spy("off")`)
	e.os.files[stateFile] = []byte(`{"disabled": ["off"]}`)
	e.start()
	e.loadText("scratch", `T.spy("x")`)
	for _, tt := range []struct{ name, want string }{
		{"ghost", "is not loaded"},
		{"off", "is disabled"},
		{"scratch", "loaded from a buffer"},
	} {
		n := e.control("reload", tt.name)
		if len(n) != 1 || !n[0].Warn || !strings.Contains(n[0].Text, tt.want) {
			t.Errorf("reload %s: notices %+v, want %q", tt.name, n, tt.want)
		}
	}
}

func TestReloadDependantRebuildsDecorationsViaReplay(t *testing.T) {
	// Decorations come with the decoration API (a later stage); status items and commands
	// stand in here: all of it is owned per generation and vanishes with OwnerGone.
	e := newEnv(t)
	e.bundle("base", map[string]string{"init.lua": `T.on_unload(function() T.spy("unload base") end) T.spy("base load")`})
	e.bundle("dep", map[string]string{
		"plugin.json": `{"requires": ["base"]}`,
		"init.lua": `T.register_all()
T.on_unload(function() T.spy("unload dep") end)
T.on("buffer_activated", function(ev) T.spy("dep rebuilt", tostring(ev.replay)) end)
T.spy("dep load")`,
	})
	e.build()
	e.h.Host.AddBuffer("/p/a.go", "x")
	e.h.Start()
	e.h.Settle()
	if res := e.h.Host.Resources(); !slices.Contains(res, "status:dep/s") {
		t.Fatalf("resources = %v", res)
	}
	depGen := e.goneCount("dep")

	plugin.ResetSpy(e.h.RT)
	e.control("reload", "base")
	want := []string{"unload dep", "cleanup:dep", "unload base", "base load", "dep load", "dep rebuilt true"}
	if got := e.spyLines(); !slices.Equal(got, want) {
		t.Fatalf("spy = %v\nwant %v", got, want)
	}
	if e.goneCount("dep") != depGen+1 || e.goneCount("base") != 1 {
		t.Fatalf("OwnerGone: dep %d (was %d), base %d", e.goneCount("dep"), depGen, e.goneCount("base"))
	}
	if res := e.h.Host.Resources(); !slices.Contains(res, "status:dep/s") || !slices.Contains(res, "command:dep/dep.reg") {
		t.Fatalf("the dependant did not rebuild its resources: %v", res)
	}
	if !e.noticeHas("Plugin 'base' reloaded (with 1 dependant(s))") {
		t.Fatalf("notices = %v", e.notices())
	}
}

func TestLateLoadGetsReplay(t *testing.T) {
	e := newEnv(t)
	e.user("early.lua", marker("early"))
	e.start()
	h := e.h
	a := h.Host.AddBuffer("/p/a.go", "a")
	h.Host.AddBuffer("", "untitled")
	b := h.Host.AddBuffer("/p/b.go", "b")
	h.Host.SetActive(b)
	_ = a

	plugin.ResetSpy(h.RT)
	e.loadText("late", `
T.on("after_file_open", function(ev) T.spy("open", ev.path, tostring(ev.replay)) end)
T.on("buffer_activated", function(ev) T.spy("activated", tostring(ev.replay), tostring(ev.previous)) end)`)
	want := []string{"open /p/a.go true", "open /p/b.go true", "activated true nil"}
	if got := e.spyLines(); !slices.Equal(got, want) {
		t.Fatalf("spy = %v, want %v", got, want)
	}

	// A plugin that listens to neither gets no event.
	plugin.ResetSpy(h.RT)
	e.loadText("quiet", `T.on("after_file_save", function() T.spy("never") end) T.spy("quiet loaded")`)
	if got := e.spyLines(); !slices.Equal(got, []string{"quiet loaded"}) {
		t.Fatalf("spy = %v", got)
	}
}

func TestStartupReplayFollowsPluginsLoaded(t *testing.T) {
	e := newEnv(t)
	e.user("watch.lua", `
T.on("plugins_loaded", function() T.spy("plugins_loaded") end)
T.on("after_file_open", function(ev) T.spy("open", ev.path, tostring(ev.replay)) end)`)
	e.build()
	e.h.Host.AddBuffer("/p/a.go", "a")
	e.h.Start()
	e.h.Settle()
	if got := e.spyLines(); !slices.Equal(got, []string{"plugins_loaded", "open /p/a.go true"}) {
		t.Fatalf("spy = %v", got)
	}
}

func TestLoadText(t *testing.T) {
	e := newEnv(t)
	e.user("taken.lua", marker("taken"))
	e.start()

	n := e.loadText("My Plugin-1", `T.on_unload(function() T.spy("unload v1") end) T.command("v", function() T.spy("v1") end)`)
	if len(n) != 0 || !e.noticeHas("Plugin 'my_plugin_1' loaded from buffer") {
		t.Fatalf("notices = %v / %v", n, e.notices())
	}
	if p := e.info("my_plugin_1"); p.Source != "buffer" || p.Status != "loaded" {
		t.Fatalf("plugin = %+v", p)
	}

	// Same name again: a reload; a syntax error keeps the first version.
	e.loadText("My Plugin-1", `T.command("v", function() T.spy("v2") end) +`)
	if !e.noticeHas("plugin 'my_plugin_1': ") {
		t.Fatalf("notices = %v", e.notices())
	}
	e.run("my_plugin_1.v")
	if got := e.spyLines(); !slices.Equal(got, []string{"v1"}) {
		t.Fatalf("ran %v", got)
	}
	e.loadText("My Plugin-1", `T.command("v", function() T.spy("v2") end)`)
	if !e.noticeHas("Plugin 'my_plugin_1' reloaded from buffer") {
		t.Fatalf("notices = %v", e.notices())
	}
	e.run("my_plugin_1.v")
	if got := e.spyLines(); !slices.Equal(got, []string{"v2"}) {
		t.Fatalf("ran %v", got)
	}

	for _, tt := range []struct{ stem, want string }{
		{"taken", "is already loaded from user"},
		{"init", `"init" is reserved`},
	} {
		n := e.loadText(tt.stem, `T.spy("never")`)
		if len(n) != 1 || !n[0].Warn || !strings.Contains(n[0].Text, tt.want) {
			t.Errorf("load_text %q: %+v, want %q", tt.stem, n, tt.want)
		}
	}
	if e.info("taken").Source != "user" {
		t.Error("the user plugin was replaced by the buffer")
	}

	// Names are sanitised.
	for stem, want := range map[string]string{"": "buffer", "9lives": "lives", "ÄÖ": "buffer", strings.Repeat("a", 60): strings.Repeat("a", 40)} {
		e.loadText(stem, `-- empty`)
		if _, ok := e.h.Plugin(want); !ok {
			t.Errorf("stem %q: no plugin %q in %v", stem, want, e.names())
		}
	}

	// Disabling a buffer plugin unloads it.
	if n := e.control("disable", "my_plugin_1"); len(n) != 1 || !strings.Contains(n[0].Text, "unloaded") {
		t.Errorf("disable = %+v", n)
	}
	if _, ok := e.h.Plugin("my_plugin_1"); ok {
		t.Error("still listed")
	}
}

func TestLoadTextTooLarge(t *testing.T) {
	e := newEnv(t)
	e.start()
	big := "-- " + strings.Repeat("x", plugin.MaxLuaFile)
	work, notices := e.h.RT.Control(pluginapi.Control{Op: "load_text", Name: "big", Text: big})
	if len(work) != 0 || len(notices) != 1 || !notices[0].Warn || !strings.HasPrefix(notices[0].Text, "limit: source too large") {
		t.Fatalf("work %d notices %+v", len(work), notices)
	}
	if _, ok := e.h.Plugin("big"); ok {
		t.Fatal("loaded")
	}
	// Exactly at the cap is fine.
	atCap := "--" + strings.Repeat("x", plugin.MaxLuaFile-2)
	e.loadText("cap", atCap)
	if e.info("cap").Status != "loaded" {
		t.Fatalf("cap = %+v, notices %v", e.info("cap"), e.notices())
	}
}

func elseifChain(size int) string {
	var b strings.Builder
	b.WriteString("local x = 1\nif x == 0 then\n")
	for i := 1; b.Len() < size; i++ {
		b.WriteString("elseif x == ")
		b.WriteString(strings.Repeat("1", 1+i%5))
		b.WriteString(" then\n")
	}
	b.WriteString("end\n")
	return b.String()
}

func TestHugeElseifChainCappedBySize(t *testing.T) {
	e := newEnv(t)
	e.user("huge.lua", elseifChain(plugin.MaxLuaFile+1000))
	e.user("big_ok.lua", elseifChain(plugin.MaxLuaFile-1000)+marker("big_ok"))
	e.start()
	huge := e.info("huge")
	if huge.Status != "failed" || !strings.Contains(huge.Err, "source too large") {
		t.Fatalf("huge = %+v", huge)
	}
	// The biggest accepted chain compiles and loads: the cap is what bounds the parser.
	if got := e.info("big_ok").Status; got != "loaded" {
		t.Fatalf("big_ok = %+v", e.info("big_ok"))
	}
}

func TestLoaderCompilesEveryFileInWork(t *testing.T) {
	var mu struct {
		names []string
		onUI  bool
	}
	uiG := testGoid()
	restore := plugin.SetCompileHook(func(name string) {
		mu.names = append(mu.names, name)
		if testGoid() == uiG {
			mu.onUI = true
		}
	})
	defer restore()

	e := newEnv(t)
	e.bundle("b", map[string]string{"init.lua": marker("b")})
	e.user("u.lua", marker("u"))
	e.start()
	if mu.onUI {
		t.Fatal("a plugin was compiled on the UI goroutine")
	}
	slices.Sort(mu.names)
	if !slices.Equal(mu.names, []string{"b", "u"}) {
		t.Fatalf("compiled %v", mu.names)
	}

	// Reload and load_text compile in Work too.
	mu.names = nil
	e.control("reload", "u")
	e.loadText("buf", `T.spy("buf")`)
	if mu.onUI || len(mu.names) != 2 {
		t.Fatalf("reload/load_text compiled %v, on the UI goroutine: %v", mu.names, mu.onUI)
	}

	// The runtime refuses a source that was not compiled in Work.
	if err := e.h.RT.LoadPlugin(plugin.Source{Name: "raw", Kind: plugin.SourceUser}); err == nil {
		t.Fatal("LoadPlugin accepted a plugin without compiled files")
	}
}

// TestReloadAllKeepsUserPluginsWhenTheDirectoryCannotBeRead: a plugin directory that cannot be
// opened or listed (permissions, a flaky disk) says nothing about the plugins in it, so they
// keep running; only a directory that was read and no longer lists a plugin removes it. The
// final notice must say what happened.
func TestReloadAllKeepsUserPluginsWhenTheDirectoryCannotBeRead(t *testing.T) {
	tests := []struct {
		name string
		fail func(e *env, down *bool)
	}{
		{"cannot be opened", func(e *env, down *bool) {
			open := e.opts.Open
			e.opts.Open = func(dir string) (fs.FS, func(), error) {
				if *down && dir == userDir {
					return nil, nil, &fs.PathError{Op: "open", Path: dir, Err: fs.ErrPermission}
				}
				return open(dir)
			}
		}},
		{"cannot be listed", func(e *env, down *bool) {
			open := e.opts.Open
			e.opts.Open = func(dir string) (fs.FS, func(), error) {
				fsys, closeFn, err := open(dir)
				if err == nil && *down && dir == userDir {
					fsys = unlistableFS{fsys}
				}
				return fsys, closeFn, err
			}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newEnv(t)
			down := false
			tt.fail(e, &down)
			e.bundle("b_one", map[string]string{"init.lua": marker("b_one")})
			e.user("hello.lua", `T.command("v", function() T.spy("hello1") end)`)
			e.user("dirplug/init.lua", marker("dirplug"))
			e.start()

			down = true
			e.user("hello.lua", `T.command("v", function() T.spy("hello2") end)`)
			e.control("reload_all", "")
			for _, name := range []string{"hello", "dirplug"} {
				if p := e.info(name); p.Status != "loaded" || !p.Enabled {
					t.Errorf("%s = %+v after an unreadable directory, want it kept", name, p)
				}
				if e.goneCount(name) != 0 {
					t.Errorf("%s was unloaded", name)
				}
			}
			e.run("hello.v")
			if got := e.spyLines(); !slices.Equal(got, []string{"hello1"}) {
				t.Errorf("hello ran %v, want the old version", got)
			}
			if p := e.info("b_one"); p.Status != "loaded" {
				t.Errorf("b_one = %+v", p)
			}
			last := e.h.Host.Notices[len(e.h.Host.Notices)-1]
			if !strings.Contains(last.Msg, "plugin directory") {
				t.Errorf("the last notice = %q, want it to mention the unreadable plugin directory", last.Msg)
			}

			// Readable again: the new source is picked up, a deleted plugin goes.
			down = false
			e.disk.remove(userDir + "/dirplug/init.lua")
			e.control("reload_all", "")
			e.run("hello.v")
			if got := e.spyLines(); !slices.Equal(got, []string{"hello2"}) {
				t.Errorf("hello ran %v, want the new version", got)
			}
			if _, ok := e.h.Plugin("dirplug"); ok {
				t.Errorf("dirplug is gone from the disk but still listed: %v", e.names())
			}
		})
	}
}

// unlistableFS opens but cannot list its root.
type unlistableFS struct{ fs.FS }

func (unlistableFS) ReadDir(string) ([]fs.DirEntry, error) { return nil, fs.ErrPermission }
