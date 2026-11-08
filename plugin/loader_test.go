package plugin_test

import (
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/tbxark/gvim/pluginapi"
)

func TestDiscoveryLoadsEverySourceInOrder(t *testing.T) {
	e := newEnv(t)
	e.bundle("b_two", map[string]string{"plugin.json": `{"version": "1.2.3"}`, "init.lua": marker("b_two")})
	e.bundle("a_one", map[string]string{"init.lua": marker("a_one")})
	e.user("zed.lua", marker("zed"))
	e.user("alpha/init.lua", marker("alpha"))
	e.user("README.md", "not a plugin")
	e.user(".hidden.lua", marker("hidden"))
	e.disk.put("/dev/mine.lua", marker("mine"))
	e.disk.put("/dev/dirplug/init.lua", marker("dirplug"))
	e.disk.put(initFile, marker("init"))
	e.opts.Paths = []string{"/dev/mine.lua", "/dev/dirplug"}

	e.start()

	want := []string{"load a_one", "load b_two", "load alpha", "load zed", "load mine", "load dirplug", "load init"}
	if got := e.spyLines(); !slices.Equal(got, want) {
		t.Fatalf("load order = %v\nwant %v\nlog %v", got, want, e.h.LogText(""))
	}
	for name, src := range map[string]string{"a_one": "bundled", "b_two": "bundled", "alpha": "user", "zed": "user", "mine": "cli", "dirplug": "cli", "init": "init"} {
		p := e.info(name)
		if p.Source != src || p.Status != "loaded" || !p.Enabled {
			t.Errorf("%s = %+v", name, p)
		}
	}
	if p := e.info("b_two"); p.Version != "1.2.3" {
		t.Errorf("b_two version = %q", p.Version)
	}
	if p := e.info("alpha"); p.Dir != userDir+"/alpha" {
		t.Errorf("alpha dir = %q", p.Dir)
	}
	if _, ok := e.h.Plugin("hidden"); ok {
		t.Error("a hidden file was loaded")
	}
	if len(e.notices()) != 0 {
		t.Errorf("notices = %v", e.notices())
	}
}

// TestPluginInfoReportsTheEntryFile checks the file the manager opens for Open Source: the
// .lua file of a single-file plugin, the manifest main or init.lua of a directory plugin,
// also for a plugin that failed, and nothing for a bundled one.
func TestPluginInfoReportsTheEntryFile(t *testing.T) {
	e := newEnv(t)
	e.bundle("b_one", map[string]string{"init.lua": marker("b_one")})
	e.user("single.lua", marker("single"))
	e.user("broken.lua", "this is not lua")
	e.user("dirinit/init.lua", marker("dirinit"))
	e.user("dirinit/dirinit.lua", "-- helper")
	e.user("dirmain/plugin.json", `{"main": "src/start.lua"}`)
	e.user("dirmain/src/start.lua", marker("dirmain"))
	e.user("dirmain/init.lua", "-- not the entry")
	e.disk.put("/dev/mine.lua", marker("mine"))
	e.opts.Paths = []string{"/dev/mine.lua"}

	e.start()

	for name, want := range map[string]string{
		"b_one":   "",
		"single":  userDir + "/single.lua",
		"broken":  userDir + "/broken.lua",
		"dirinit": userDir + "/dirinit/init.lua",
		"dirmain": userDir + "/dirmain/src/start.lua",
		"mine":    "/dev/mine.lua",
	} {
		if got := e.info(name).File; got != want {
			t.Errorf("%s: File = %q, want %q", name, got, want)
		}
	}
}

func TestOrderingAndDependencies(t *testing.T) {
	type pl struct{ name, manifest string }
	tests := []struct {
		name      string
		plugins   []pl
		wantOrder []string          // names that load, in order
		wantState map[string]string // name -> status
		wantErr   map[string]string // name -> substring of the row error
	}{
		{
			name:      "requires moves a dependency before its dependant",
			plugins:   []pl{{"a", `{"requires": ["z"]}`}, {"m", ``}, {"z", ``}},
			wantOrder: []string{"m", "z", "a"},
		},
		{
			name:      "after orders without requiring",
			plugins:   []pl{{"a", `{"after": ["b", "ghost"]}`}, {"b", ``}},
			wantOrder: []string{"b", "a"},
		},
		{
			name:      "ties go by source then name",
			plugins:   []pl{{"c", ``}, {"a", ``}, {"b", ``}},
			wantOrder: []string{"a", "b", "c"},
		},
		{
			name:      "missing dependency",
			plugins:   []pl{{"a", `{"requires": ["ghost"]}`}, {"b", ``}},
			wantOrder: []string{"b"},
			wantState: map[string]string{"a": "failed"},
			wantErr:   map[string]string{"a": `requires "ghost" (not installed)`},
		},
		{
			name:      "disabled dependency",
			plugins:   []pl{{"a", `{"requires": ["off"]}`}, {"off", `{"default_enabled": false}`}},
			wantOrder: nil,
			wantState: map[string]string{"a": "failed", "off": "disabled"},
			wantErr:   map[string]string{"a": `requires "off" (disabled)`},
		},
		{
			name:      "cycle",
			plugins:   []pl{{"a", `{"requires": ["b"]}`}, {"b", `{"requires": ["a"]}`}, {"c", ``}},
			wantOrder: []string{"c"},
			wantState: map[string]string{"a": "failed", "b": "failed"},
			wantErr:   map[string]string{"a": "dependency cycle", "b": "dependency cycle"},
		},
		{
			name:      "blocked dependency blocks its dependants",
			plugins:   []pl{{"a", `{"requires": ["b"]}`}, {"b", `{"requires": ["ghost"]}`}},
			wantState: map[string]string{"a": "failed", "b": "failed"},
			wantErr:   map[string]string{"a": "cannot be loaded"},
		},
		{
			name:      "api too new",
			plugins:   []pl{{"a", `{"api": 3}`}, {"b", `{"api": 1}`}},
			wantOrder: []string{"b"},
			wantState: map[string]string{"a": "failed"},
			wantErr:   map[string]string{"a": "needs plugin API 3"},
		},
		{
			name:      "dependency main chunk fails",
			plugins:   []pl{{"a", `{"requires": ["b"]}`}, {"b", `{"after": []}`}},
			wantOrder: nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newEnv(t)
			for _, p := range tt.plugins {
				files := map[string]string{"init.lua": marker(p.name)}
				if tt.name == "dependency main chunk fails" && p.name == "b" {
					files["init.lua"] = `error("b broke")`
				}
				if p.manifest != "" {
					files["plugin.json"] = p.manifest
				}
				e.bundle(p.name, files)
			}
			e.start()
			var got []string
			for _, l := range e.spyLines() {
				got = append(got, strings.TrimPrefix(l, "load "))
			}
			if !slices.Equal(got, tt.wantOrder) {
				t.Fatalf("load order = %v, want %v (rows %v, log %v)", got, tt.wantOrder, e.names(), e.h.LogText(""))
			}
			for name, st := range tt.wantState {
				if got := e.info(name).Status; got != st {
					t.Errorf("%s status = %q, want %q", name, got, st)
				}
			}
			for name, sub := range tt.wantErr {
				if got := e.info(name).Err; !strings.Contains(got, sub) {
					t.Errorf("%s error = %q, want it to contain %q", name, got, sub)
				}
			}
			if tt.name == "dependency main chunk fails" {
				if got := e.info("b").Status; got != "failed" {
					t.Errorf("b status = %q", got)
				}
				a := e.info("a")
				if a.Status != "failed" || !strings.Contains(a.Err, `"b", which failed to load`) {
					t.Errorf("a = %+v", a)
				}
			}
		})
	}
}

func TestDependantStartsAfterDependencyMainFinished(t *testing.T) {
	e := newEnv(t)
	e.bundle("base", map[string]string{"init.lua": `T.spy("base start") T.host() T.spy("base done")`})
	e.bundle("user_of_base", map[string]string{"plugin.json": `{"requires": ["base"]}`, "init.lua": marker("user_of_base")})
	e.bundle("zz_free", map[string]string{"init.lua": marker("zz_free")})
	e.start()

	if got := e.spyLines(); len(got) != 3 || got[0] != "base start" || got[2] != "load zz_free" {
		t.Fatalf("spy while base is parked = %v (a plugin that does not need base must not wait for it)", got)
	}
	if st := e.info("base").Status; st != "loading" {
		t.Fatalf("base = %q", st)
	}
	if slices.Contains(e.spyLines(), "load user_of_base") {
		t.Fatal("the dependant started before the dependency's main chunk finished")
	}
	e.h.Resolve(pluginapi.Completion{Token: lastToken(e), Values: []any{"go"}})
	got := e.spyLines()
	if got[len(got)-2] != "base done" || got[len(got)-1] != "load user_of_base" {
		t.Fatalf("spy after resolve = %v", got)
	}
	if st := e.info("user_of_base").Status; st != "loaded" {
		t.Fatalf("user_of_base = %q", st)
	}
}

// lastToken is the token of the latest T.host wait.
func lastToken(e *env) pluginapi.Token {
	for _, l := range slices.Backward(e.spyLines()) {
		if rest, ok := strings.CutPrefix(l, "token:"); ok {
			n, _ := strconv.Atoi(rest)
			return pluginapi.Token(n)
		}
	}
	return 0
}

func TestShadowing(t *testing.T) {
	e := newEnv(t)
	e.bundle("dup", map[string]string{"init.lua": `T.spy("bundled dup")`})
	e.bundle("only_bundled", map[string]string{"init.lua": marker("only_bundled")})
	e.user("dup.lua", `T.spy("user dup")`)
	e.user("both.lua", `T.spy("both file")`)
	e.user("both/init.lua", `T.spy("both dir")`)
	e.user("init.lua", `T.spy("user init")`)
	e.disk.put("/dev/dup.lua", `T.spy("cli dup")`)
	e.disk.put("/dev/both.lua", `T.spy("cli both")`)
	e.disk.put(initFile, `T.spy("real init")`)
	e.opts.Paths = []string{"/dev/dup.lua"}

	e.start()

	got := e.spyLines()
	for _, want := range []string{"cli dup", "both dir", "load only_bundled", "real init"} {
		if !slices.Contains(got, want) {
			t.Errorf("%q missing from %v", want, got)
		}
	}
	for _, bad := range []string{"bundled dup", "user dup", "both file", "user init"} {
		if slices.Contains(got, bad) {
			t.Errorf("%q must not have loaded: %v", bad, got)
		}
	}
	if p := e.info("dup"); p.Source != "cli" {
		t.Errorf("dup = %+v", p)
	}
	if !logHas(e.h, "dup", "overrides the bundled, user plugin of the same name") {
		t.Errorf("the override is not logged: %v", e.h.LogText("dup"))
	}
	if !e.noticeHas(`two plugins named "both"`) {
		t.Errorf("no notice about the file/directory clash: %v", e.notices())
	}
	if !e.noticeHas(`"init" is a reserved plugin name`) {
		t.Errorf("no notice about the reserved name: %v", e.notices())
	}
}

func TestEnablementPrecedence(t *testing.T) {
	tests := []struct {
		name         string
		manifest     string
		config       map[string]bool
		stateFile    string
		wantLoaded   bool
		wantLocked   string
		pluginSource string // "bundled" or "user"
	}{
		{name: "default is enabled", pluginSource: "user", wantLoaded: true},
		{name: "bundled default_enabled false", pluginSource: "bundled", manifest: `{"default_enabled": false}`},
		{name: "default_enabled is for bundled only", pluginSource: "user", manifest: `{"default_enabled": false}`, wantLoaded: true},
		{name: "state file enables a default-off bundled plugin", pluginSource: "bundled", manifest: `{"default_enabled": false}`, stateFile: `{"enabled": ["p"]}`, wantLoaded: true},
		{name: "state file disables", pluginSource: "user", stateFile: `{"disabled": ["p"]}`},
		{name: "config false beats state enabled", pluginSource: "user", config: map[string]bool{"p": false}, stateFile: `{"enabled": ["p"]}`, wantLocked: "set in config"},
		{name: "config true beats state disabled", pluginSource: "bundled", manifest: `{"default_enabled": false}`, config: map[string]bool{"p": true}, stateFile: `{"disabled": ["p"]}`, wantLoaded: true, wantLocked: "set in config"},
		{name: "damaged state file is ignored", pluginSource: "user", stateFile: `{not json`, wantLoaded: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newEnv(t)
			if tt.pluginSource == "bundled" {
				files := map[string]string{"init.lua": marker("p")}
				if tt.manifest != "" {
					files["plugin.json"] = tt.manifest
				}
				e.bundle("p", files)
			} else {
				if tt.manifest != "" {
					e.user("p/plugin.json", tt.manifest)
					e.user("p/init.lua", marker("p"))
				} else {
					e.user("p.lua", marker("p"))
				}
			}
			e.opts.Enabled = tt.config
			if tt.stateFile != "" {
				e.os.files[stateFile] = []byte(tt.stateFile)
			}
			e.start()
			p := e.info("p")
			if loaded := p.Status == "loaded"; loaded != tt.wantLoaded {
				t.Fatalf("p = %+v, want loaded=%v", p, tt.wantLoaded)
			}
			if !tt.wantLoaded && (p.Status != "disabled" || p.Enabled) {
				t.Errorf("p = %+v, want a disabled row", p)
			}
			if p.Locked != tt.wantLocked {
				t.Errorf("locked = %q, want %q", p.Locked, tt.wantLocked)
			}
		})
	}
}

func TestLegacyJavaScriptPluginsAreIgnoredWithOneNotice(t *testing.T) {
	e := newEnv(t)
	e.user("old.js", `console.log("x")`)
	e.user("older.ts", `export {}`)
	e.user("types.d.ts", `declare const x: number`)
	e.user("new.lua", marker("new"))
	e.start()

	var legacy []string
	for _, n := range e.notices() {
		if strings.Contains(n, "JavaScript plugin(s) ignored") {
			legacy = append(legacy, n)
		}
	}
	if len(legacy) != 1 || legacy[0] != "2 JavaScript plugin(s) ignored (gvim plugins are Lua now): old.js, older.ts" {
		t.Fatalf("legacy notices = %q (all %q)", legacy, e.notices())
	}
	for _, name := range []string{"old.js", "older.ts"} {
		p := e.info(name)
		if p.Status != "legacy" || p.Enabled || p.Locked == "" {
			t.Errorf("%s = %+v", name, p)
		}
	}
	if _, ok := e.h.Plugin("types.d.ts"); ok {
		t.Error("a .d.ts file was listed")
	}
	if e.info("new").Status != "loaded" {
		t.Error("the Lua plugin did not load")
	}
}

func TestNoPluginsFlag(t *testing.T) {
	e := newEnv(t)
	e.bundle("b", map[string]string{"init.lua": marker("b")})
	e.user("u.lua", marker("u"))
	e.disk.put(initFile, marker("init"))
	e.disk.put("/dev/c.lua", marker("c"))
	e.opts.Disabled = true
	e.opts.Paths = []string{"/dev/c.lua"}
	e.start()
	if got := e.spyLines(); !slices.Equal(got, []string{"load c"}) {
		t.Fatalf("loaded %v with --no-plugins, want only the --plugin path", got)
	}
	if work, _ := e.h.RT.Control(pluginapi.Control{Op: "reload_all"}); len(work) != 0 {
		t.Errorf("reload_all discovered under --no-plugins")
	}
}

func TestNoPluginsAloneStartsNothing(t *testing.T) {
	e := newEnv(t)
	e.bundle("b", map[string]string{"init.lua": marker("b")})
	e.opts.Disabled = true
	h := e.start()
	if got := e.spyLines(); len(got) != 0 {
		t.Fatalf("loaded %v", got)
	}
	// ready is not held when nothing is loading
	h.RT.Emit(pluginapi.Event{Name: "ready"})
}

func TestStartupErrorsBecomeAtMostThreeNotices(t *testing.T) {
	e := newEnv(t)
	for _, n := range []string{"e1", "e2", "e3", "e4", "e5"} {
		e.user(n+".lua", `local x = = 1`)
	}
	e.user("fine.lua", marker("fine"))
	e.start()
	var errs []string
	for _, n := range e.h.Host.Notices {
		if n.Level == pluginapi.LevelError {
			errs = append(errs, n.Msg)
		}
	}
	if len(errs) != 4 || !strings.HasPrefix(errs[0], `plugin 'e1': `) || errs[3] != "2 more plugin errors (Plugin: Show Log)" {
		t.Fatalf("error notices = %q", errs)
	}
	if !strings.Contains(errs[0], "e1/e1.lua") {
		t.Errorf("the notice should name the file: %q", errs[0])
	}
	if e.info("e4").Status != "failed" || e.info("fine").Status != "loaded" {
		t.Errorf("rows = %v", e.names())
	}
	n := 0
	for _, l := range e.h.LogText("") {
		if strings.Contains(l, "cannot load") {
			n++
		}
	}
	if n != 5 {
		t.Errorf("log has %d 'cannot load' lines, want one per broken plugin: %v", n, e.h.LogText(""))
	}
}

func TestMainChunkErrorFailsOnlyThatPlugin(t *testing.T) {
	e := newEnv(t)
	e.user("bad.lua", "T.command('x', function() end)\nerror('boom')")
	e.user("good.lua", marker("good"))
	e.start()
	bad := e.info("bad")
	if bad.Status != "failed" || !strings.Contains(bad.Err, "boom") || bad.Commands != 0 {
		t.Fatalf("bad = %+v", bad)
	}
	if e.info("good").Status != "loaded" {
		t.Fatal("good did not load")
	}
	if !e.noticeHas(`plugin 'bad': `) {
		t.Errorf("notices = %v", e.notices())
	}
}

func TestPluginsLoadedPrecedesReadyEvenWhenReadyComesFirst(t *testing.T) {
	e := newEnv(t)
	e.user("watch.lua", `
T.on("plugins_loaded", function() T.spy("plugins_loaded") end)
T.on("ready", function() T.spy("ready") end)
T.on("resize", function() T.spy("resize") end)`)
	h := e.build()
	work := h.RT.Start(h.Host)
	// The first WindowSizeMsg arrives while discovery is still running.
	h.RT.Emit(pluginapi.Event{Name: "ready"})
	h.RunWork(work)
	h.Settle()
	if got := e.spyLines(); !slices.Equal(got, []string{"plugins_loaded", "ready"}) {
		t.Fatalf("spy = %v", got)
	}
}

func TestPluginsLoadedWaitsForParkedMainChunks(t *testing.T) {
	e := newEnv(t)
	e.user("slow.lua", `T.host() T.spy("slow done")`)
	e.user("watch.lua", `T.on("plugins_loaded", function() T.spy("plugins_loaded") end)`)
	e.start()
	if slices.Contains(e.spyLines(), "plugins_loaded") {
		t.Fatal("plugins_loaded fired while a main chunk is parked")
	}
	e.h.Resolve(pluginapi.Completion{Token: lastToken(e), Values: []any{true}})
	got := e.spyLines()
	if got[len(got)-1] != "plugins_loaded" || !slices.Contains(got, "slow done") {
		t.Fatalf("spy = %v", got)
	}
}

// TestStartupNoticesComeInOneBurst checks the order and the text of the startup notices:
// the one about ignored JavaScript files is said together with the load errors, after
// them, and the count of the errors that are not named is pluralised. (The ui shows the
// notices of one update as one status message.)
func TestStartupNoticesComeInOneBurst(t *testing.T) {
	tests := []struct {
		name  string
		files map[string]string
		want  []string // prefixes of the notices that are not info, in order
	}{
		{"one more error is singular", map[string]string{"e1.lua": `local x = = 1`, "e2.lua": `local x = = 1`,
			"e3.lua": `local x = = 1`, "e4.lua": `local x = = 1`},
			[]string{"plugin 'e1': ", "plugin 'e2': ", "plugin 'e3': ", "1 more plugin error (Plugin: Show Log)"}},
		{"legacy files come after the errors", map[string]string{"old.js": `x`, "e1.lua": `local x = = 1`},
			[]string{"plugin 'e1': ", "1 JavaScript plugin(s) ignored (gvim plugins are Lua now): old.js"}},
		{"legacy files alone", map[string]string{"old.js": `x`},
			[]string{"1 JavaScript plugin(s) ignored (gvim plugins are Lua now): old.js"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newEnv(t)
			for f, src := range tt.files {
				e.user(f, src)
			}
			e.start()
			var got []string
			for _, n := range e.h.Host.Notices {
				if n.Level != pluginapi.LevelInfo {
					got = append(got, n.Msg)
				}
			}
			if len(got) != len(tt.want) {
				t.Fatalf("notices = %q, want %d starting %q", got, len(tt.want), tt.want)
			}
			for i, w := range tt.want {
				if !strings.HasPrefix(got[i], w) {
					t.Errorf("notice %d = %q, want prefix %q", i, got[i], w)
				}
			}
		})
	}
}
