package plugin_test

import (
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/tbxark/gvim/pluginapi"
)

// realDiskEnv is an env whose loader reads the real file system (nil Open: os.OpenRoot for
// directories) under a temporary configuration directory.
func realDiskEnv(t *testing.T) (e *env, cfg string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("symlinks")
	}
	cfg = filepath.Join(t.TempDir(), "cfg")
	e = newEnv(t)
	e.opts.Open = nil
	e.opts.UserDir = filepath.Join(cfg, "plugins")
	e.opts.InitFile = filepath.Join(cfg, "init.lua")
	e.opts.StateFile = ""
	return e, cfg
}

func writeFile(t *testing.T, path, src string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
}

func symlink(t *testing.T, target, name string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(name), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, name); err != nil {
		t.Skipf("symlink: %v", err)
	}
}

// init.lua and single-file plugins may be symlinks (stow, dotfiles repositories); only
// directory plugins are confined.
func TestSymlinkedInitAndSingleFilePluginsLoad(t *testing.T) {
	e, cfg := realDiskEnv(t)
	dots := filepath.Join(filepath.Dir(cfg), "dots")
	writeFile(t, filepath.Join(dots, "init.lua"), `T.spy("init ran")`)
	writeFile(t, filepath.Join(dots, "foo.lua"), `T.spy("foo ran")`)
	symlink(t, filepath.Join(dots, "init.lua"), filepath.Join(cfg, "init.lua"))
	symlink(t, filepath.Join(dots, "foo.lua"), filepath.Join(cfg, "plugins", "foo.lua"))
	e.start()

	if got := e.spyLines(); !slices.Equal(got, []string{"foo ran", "init ran"}) {
		t.Fatalf("spy = %v; rows %v; notices %v", got, e.names(), e.notices())
	}
	if p := e.info("init"); p.Status != "loaded" {
		t.Errorf("init = %+v", p)
	}
	if p := e.info("foo"); p.Status != "loaded" {
		t.Errorf("foo = %+v", p)
	}

	// init_check and init_reload read it the same way.
	n := e.control("init_check", "")
	if len(n) != 0 || !e.noticeHas("init.lua: syntax OK") {
		t.Errorf("init_check: %v / %v", n, e.notices())
	}
}

// A dangling init.lua is not silently skipped: the user sees a failed row and a notice.
func TestUnreadableInitFileIsReported(t *testing.T) {
	e, cfg := realDiskEnv(t)
	symlink(t, filepath.Join(filepath.Dir(cfg), "missing.lua"), filepath.Join(cfg, "init.lua"))
	e.start()

	if p := e.info("init"); p.Status != "failed" || p.Err == "" {
		t.Fatalf("init = %+v; rows %v", p, e.names())
	}
	if !e.noticeHas("plugin 'init': ") {
		t.Errorf("notices = %v", e.notices())
	}
}

// A missing init.lua is the normal case and says nothing.
func TestMissingInitFileIsSilent(t *testing.T) {
	e, _ := realDiskEnv(t)
	e.start()
	if len(e.h.Host.Notices) != 0 || len(e.h.RT.Plugins()) != 0 {
		t.Fatalf("rows %v, notices %v", e.names(), e.notices())
	}
}

// A main chunk that parks and never resumes must not hold back everybody else.
func TestParkedMainChunkDoesNotWithholdStartupOutput(t *testing.T) {
	e := newEnv(t)
	e.user("a_slow.lua", `T.host() T.spy("slow done")`)
	e.user("bad.lua", `error("boom")`)
	e.user("watch.lua", `
T.on("plugins_loaded", function() T.spy("plugins_loaded") end)
T.on("ready", function() T.spy("ready") end)
T.on("after_file_open", function(ev) T.spy("open", ev.path, tostring(ev.replay)) end)`)
	e.user("dep/plugin.json", `{"requires": ["a_slow"]}`)
	e.user("dep/init.lua", `T.spy("dep ran") T.on("plugins_loaded", function() T.spy("dep plugins_loaded") end)`)
	e.build()
	e.h.Host.AddBuffer("/p/a.go", "a")
	e.h.Start()
	e.h.Settle()
	e.h.RT.Emit(pluginapi.Event{Name: "ready"})
	e.h.Settle()
	token := lastToken(e)

	spyHas := func(s string) bool { return slices.Contains(e.spyLines(), s) }
	if spyHas("plugins_loaded") || spyHas("ready") {
		t.Fatalf("released before the bound: %v", e.spyLines())
	}
	// Within the bound nothing is released; past it everything that could finish is.
	e.h.Advance(100 * time.Millisecond)
	if spyHas("plugins_loaded") {
		t.Fatalf("released early: %v", e.spyLines())
	}
	e.h.Advance(10 * time.Second)

	for _, want := range []string{"plugins_loaded", "ready", "open /p/a.go true"} {
		if !spyHas(want) {
			t.Errorf("%q missing from %v", want, e.spyLines())
		}
	}
	if i, j := slices.Index(e.spyLines(), "plugins_loaded"), slices.Index(e.spyLines(), "ready"); i > j {
		t.Errorf("plugins_loaded after ready: %v", e.spyLines())
	}
	if !e.noticeHas("plugin 'bad': ") {
		t.Errorf("the failure of bad was not shown: %v", e.notices())
	}
	if spyHas("dep ran") {
		t.Errorf("a dependant of the parked plugin started: %v", e.spyLines())
	}
	if got := e.names(); !slices.Contains(got, "a_slow:loading") || !slices.Contains(got, "watch:loaded") {
		t.Errorf("rows = %v", got)
	}

	// When the parked chunk finishes, its dependant starts and learns of the files.
	e.h.Resolve(pluginapi.Completion{Token: token, Values: []any{true}})
	if !spyHas("slow done") || !spyHas("dep ran") {
		t.Fatalf("spy = %v", e.spyLines())
	}
	if e.info("dep").Status != "loaded" || e.info("a_slow").Status != "loaded" {
		t.Errorf("rows = %v", e.names())
	}
	if n := countOf(e.spyLines(), "plugins_loaded"); n != 1 {
		t.Errorf("plugins_loaded emitted %d times: %v", n, e.spyLines())
	}
}

func countOf(list []string, s string) int {
	n := 0
	for _, x := range list {
		if x == s {
			n++
		}
	}
	return n
}

// A reload or enable that was started before a disable must not load the plugin when its
// Work finishes.
func TestDisableDuringPendingReloadWins(t *testing.T) {
	e := newEnv(t)
	e.user("foo.lua", marker("foo"))
	e.start()

	work, n := e.h.RT.Control(pluginapi.Control{Op: "reload", Name: "foo"})
	if len(n) != 0 || len(work) == 0 {
		t.Fatalf("reload: work %d notices %v", len(work), n)
	}
	n = e.control("disable", "foo")
	if len(n) != 1 || n[0].Text != "plugin 'foo' disabled" {
		t.Fatalf("disable notices = %+v", n)
	}
	e.h.RunWork(work)
	e.h.Settle()
	if got := e.names(); !slices.Equal(got, []string{"foo:disabled"}) {
		t.Fatalf("rows = %v", got)
	}
	if got := e.state().Disabled; !slices.Equal(got, []string{"foo"}) {
		t.Fatalf("state = %+v", e.state())
	}
}

func TestDisableCancelsPendingEnable(t *testing.T) {
	e := newEnv(t)
	e.user("foo.lua", marker("foo"))
	e.os.files[stateFile] = []byte(`{"disabled": ["foo"]}`)
	e.start()
	if got := e.names(); !slices.Equal(got, []string{"foo:disabled"}) {
		t.Fatalf("rows = %v", got)
	}

	work, n := e.h.RT.Control(pluginapi.Control{Op: "enable", Name: "foo"})
	if len(n) != 0 || len(work) == 0 {
		t.Fatalf("enable: work %d notices %v", len(work), n)
	}
	n = e.control("disable", "foo")
	if len(n) != 1 || strings.Contains(n[0].Text, "already") {
		t.Fatalf("disable notices = %+v", n)
	}
	e.h.RunWork(work)
	e.h.Settle()
	if got := e.names(); !slices.Equal(got, []string{"foo:disabled"}) {
		t.Fatalf("rows = %v", got)
	}
	if got := e.state(); !slices.Equal(got.Disabled, []string{"foo"}) || len(got.Enabled) != 0 {
		t.Fatalf("state = %+v", got)
	}
	if slices.Contains(e.spyLines(), "load foo") {
		t.Fatalf("foo ran: %v", e.spyLines())
	}
}

// An enable that is not overtaken still loads the plugin.
func TestEnableStillLoadsWhenNotOvertaken(t *testing.T) {
	e := newEnv(t)
	e.user("foo.lua", marker("foo"))
	e.os.files[stateFile] = []byte(`{"disabled": ["foo"]}`)
	e.start()
	e.control("enable", "foo")
	if got := e.names(); !slices.Equal(got, []string{"foo:loaded"}) {
		t.Fatalf("rows = %v", got)
	}
}
