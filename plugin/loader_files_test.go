package plugin_test

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/tbxark/gvim/plugin"
)

func TestPluginDirectoryProblems(t *testing.T) {
	big := "--" + strings.Repeat("x", 250<<10)
	tests := []struct {
		name       string
		files      map[string]string // relative to plugins/p/
		wantStatus string
		wantErr    string
	}{
		{name: "main from the manifest", files: map[string]string{"plugin.json": `{"main": "src/main.lua"}`, "src/main.lua": marker("p")}, wantStatus: "loaded"},
		{name: "missing main", files: map[string]string{"plugin.json": `{"main": "x.lua"}`, "init.lua": marker("p")}, wantStatus: "failed", wantErr: `main file "x.lua" not found`},
		{name: "no entry at all", files: map[string]string{"lua/m.lua": "return {}"}, wantStatus: "failed", wantErr: `main file "init.lua" not found`},
		{name: "broken manifest", files: map[string]string{"plugin.json": `{`, "init.lua": marker("p")}, wantStatus: "failed", wantErr: "plugin.json:"},
		{name: "name differs from the directory", files: map[string]string{"plugin.json": `{"name": "other"}`, "init.lua": marker("p")}, wantStatus: "failed", wantErr: `does not match the directory name "p"`},
		{name: "requires itself", files: map[string]string{"plugin.json": `{"requires": ["p"]}`, "init.lua": marker("p")}, wantStatus: "failed", wantErr: "cannot require itself"},
		{name: "hidden directories are not scanned", files: map[string]string{"init.lua": marker("p"), ".git/hooks/x.lua": "syntax error here"}, wantStatus: "loaded"},
		{name: "non-lua files are not read", files: map[string]string{"init.lua": marker("p"), "data.json": "{", "notes.txt": "x"}, wantStatus: "loaded"},
		{name: "oversize module that is never required", files: map[string]string{"init.lua": marker("p"), "lua/huge.lua": "--" + strings.Repeat("x", plugin.MaxLuaFile)}, wantStatus: "loaded"},
		{name: "oversize module that is required", files: map[string]string{"init.lua": `require "huge"`, "lua/huge.lua": "--" + strings.Repeat("x", plugin.MaxLuaFile)}, wantStatus: "failed", wantErr: "source too large"},
		{name: "oversize entry", files: map[string]string{"init.lua": "--" + strings.Repeat("x", plugin.MaxLuaFile)}, wantStatus: "failed", wantErr: "source too large"},
		{name: "oversize manifest", files: map[string]string{"plugin.json": `{"description": "` + strings.Repeat("x", plugin.MaxManifest) + `"}`, "init.lua": marker("p")}, wantStatus: "failed", wantErr: "plugin.json:"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newEnv(t)
			for rel, src := range tt.files {
				e.user("p/"+rel, src)
			}
			e.start()
			p := e.info("p")
			if p.Status != tt.wantStatus || !strings.Contains(p.Err, tt.wantErr) {
				t.Fatalf("p = %+v, want %s with %q", p, tt.wantStatus, tt.wantErr)
			}
		})
	}

	t.Run("too many files", func(t *testing.T) {
		e := newEnv(t)
		e.user("p/init.lua", marker("p"))
		for i := 0; i < 64; i++ {
			e.user(fmt.Sprintf("p/lua/m%02d.lua", i), "return {}")
		}
		e.start()
		if p := e.info("p"); p.Status != "failed" || !strings.Contains(p.Err, "too many .lua files") {
			t.Fatalf("p = %+v", p)
		}
	})
	t.Run("exactly 64 files", func(t *testing.T) {
		e := newEnv(t)
		e.user("p/init.lua", marker("p"))
		for i := 0; i < 63; i++ {
			e.user(fmt.Sprintf("p/lua/m%02d.lua", i), "return {}")
		}
		e.start()
		if p := e.info("p"); p.Status != "loaded" {
			t.Fatalf("p = %+v", p)
		}
	})
	t.Run("more than 5 MiB", func(t *testing.T) {
		e := newEnv(t)
		e.user("p/init.lua", marker("p"))
		for i := 0; i < 21; i++ {
			e.user(fmt.Sprintf("p/lua/m%02d.lua", i), big)
		}
		e.start()
		if p := e.info("p"); p.Status != "failed" || !strings.Contains(p.Err, "larger than 5 MiB") {
			t.Fatalf("p = %+v", p)
		}
	})
	// Data files are never read by the loader and do not count towards any limit (the
	// walk cap of earlier versions refused such plugins outright).
	t.Run("data files do not count", func(t *testing.T) {
		e := newEnv(t)
		e.user("p/init.lua", marker("p"))
		for i := 0; i < 5000; i++ {
			e.user(fmt.Sprintf("p/data/f%d.json", i), "{}")
		}
		e.start()
		if p := e.info("p"); p.Status != "loaded" {
			t.Fatalf("p = %+v; notices %v", p, e.notices())
		}
	})
}

func TestPluginNamesAndPaths(t *testing.T) {
	e := newEnv(t)
	e.user("Bad-Name.lua", marker("bad"))
	e.user("9lives/init.lua", marker("nine"))
	e.disk.put("/dev/dash-name/plugin.json", `{"name": "dash"}`)
	e.disk.put("/dev/dash-name/init.lua", marker("dash"))
	e.disk.put("/dev/notes.txt", "hello")
	e.opts.Paths = []string{"/dev/dash-name", "/dev/notes.txt", "/dev/missing.lua", "/dev/missing_dir"}
	e.start()

	for _, name := range []string{"Bad-Name", "9lives"} {
		if p := e.info(name); p.Status != "failed" || !strings.Contains(p.Err, "invalid plugin name") {
			t.Errorf("%s = %+v", name, p)
		}
	}
	if p := e.info("dash"); p.Status != "loaded" || p.Source != "cli" {
		t.Errorf("a --plugin directory takes its name from the manifest: %+v", p)
	}
	if p := e.info("notes.txt"); p.Status != "failed" || !strings.Contains(p.Err, "not a .lua file or a plugin directory") {
		t.Errorf("notes.txt = %+v", p)
	}
	if p := e.info("missing"); p.Status != "failed" {
		t.Errorf("missing = %+v", p)
	}
	if got := e.spyLines(); !slices.Equal(got, []string{"load dash"}) {
		t.Errorf("spy = %v", got)
	}
}

func TestModuleErrorSurfacesWhenRequiredNotAtStartup(t *testing.T) {
	e := newEnv(t)
	e.user("p/init.lua", `T.command("use", function() local ok, err = pcall(require, "p.bad") T.spy(tostring(ok), err and "err") end)`)
	e.user("p/lua/p/bad.lua", "return {")
	e.start()
	if p := e.info("p"); p.Status != "loaded" {
		t.Fatalf("p = %+v", p)
	}
	e.run("p.use")
	if got := e.spyLines(); len(got) != 1 || !strings.HasPrefix(got[0], "false err") {
		t.Fatalf("spy = %v", got)
	}
}

func TestUnknownManifestFieldsAreIgnoredWithADebugLog(t *testing.T) {
	e := newEnv(t)
	e.user("p/plugin.json", `{"future": 1, "another": true}`)
	e.user("p/init.lua", marker("p"))
	e.start()
	if e.info("p").Status != "loaded" || !logHas(e.h, "p", "unknown fields ignored: another, future") {
		t.Fatalf("p = %+v, log %v", e.info("p"), e.h.LogText("p"))
	}
}

// TestStateSurvivesRestart: state saved by one session is what gvim.state.get returns in the
// next one, before the plugin's main chunk runs (S8-2). A second plugin's state stays apart.
func TestStateSurvivesRestart(t *testing.T) {
	src := `local gvim = require "gvim"
local n = gvim.state.get("n", 0)
T.spy(gvim.state.get("n", 0), tostring(gvim.state.get("other")))
gvim.state.set("n", n + 1)`
	e := newEnv(t)
	e.os.config = "/cfg"
	e.user("counter.lua", src)
	e.user("twin.lua", `local gvim = require "gvim" T.spy("twin", tostring(gvim.state.get("n")))`)
	e.start()
	if got := strings.Join(e.spyLines(), "|"); got != "0 nil|twin nil" && got != "twin nil|0 nil" {
		t.Fatalf("first session spy = %q", got)
	}
	e.h.RT.Stop()
	if got := e.os.file("/cfg/plugin-data/counter/state.json"); got != `{"n":1}` {
		t.Fatalf("state file = %q", got)
	}

	e2 := &env{t: t, disk: e.disk, bundled: e.bundled, os: e.os, opts: e.opts}
	e2.start()
	if got := strings.Join(e2.spyLines(), "|"); got != "1 nil|twin nil" && got != "twin nil|1 nil" {
		t.Fatalf("second session spy = %q", got)
	}
}

// TestFullStateSurvivesRestart: a state within its quota (keys plus values) is larger on disk
// by the JSON framing; the next session must still read it back.
func TestFullStateSurvivesRestart(t *testing.T) {
	src := `local gvim = require "gvim"
local n = gvim.state.get("n", 0)
T.spy(n, tostring(gvim.state.get("big") ~= nil))
gvim.state.set("n", n + 1)
if n == 0 then assert(gvim.state.set("big", string.rep("z", 1048569))) end`
	e := newEnv(t)
	e.os.config = "/cfg"
	e.user("full.lua", src)
	e.start()
	e.h.RT.Stop()
	if got := len(e.os.file("/cfg/plugin-data/full/state.json")); got <= 1<<20 {
		t.Fatalf("state file is %d bytes, want more than the 1 MiB quota (the test needs the framing)", got)
	}

	e2 := &env{t: t, disk: e.disk, bundled: e.bundled, os: e.os, opts: e.opts}
	e2.start()
	if got := strings.Join(e2.spyLines(), "|"); got != "1 true" {
		t.Fatalf("second session spy = %q, want %q", got, "1 true")
	}
}

// TestStateIsSeededWhenAPluginIsEnabledLater: a plugin switched on in the manager gets its
// saved state too (S8-2).
func TestStateIsSeededWhenAPluginIsEnabledLater(t *testing.T) {
	e := newEnv(t)
	e.os.config = "/cfg"
	e.os.files["/cfg/plugin-data/late/state.json"] = []byte(`{"k":"saved"}`)
	e.user("late.lua", `local gvim = require "gvim" T.spy(gvim.state.get("k"))`)
	e.os.files[stateFile] = []byte(`{"enabled":[],"disabled":["late"]}`)
	e.start()
	if got := e.spyLines(); len(got) != 0 {
		t.Fatalf("a disabled plugin ran: %v", got)
	}
	e.control("enable", "late")
	if got := strings.Join(e.spyLines(), "|"); got != "saved" {
		t.Fatalf("spy = %q", got)
	}
}
