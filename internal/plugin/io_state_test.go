package plugin_test

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/tbxark/gvim/internal/plugin/plugintest"
	"github.com/tbxark/gvim/internal/pluginapi"
)

const statePath = "/home/u/.config/gvim/plugin-data/t/state.json"

func TestState(t *testing.T) {
	rows := []struct{ name, body, want string }{
		{"get default", `T.spy(gvim.state.get("none", "dflt"), tostring(gvim.state.get("none")))`, "dflt nil"},
		{"set and get", `T.spy(gvim.state.set("a", 1), gvim.state.get("a"))`, "true 1"},
		{"values round trip", `
gvim.state.set("t", {x = {1, 2, 3}, y = "s", z = true, n = {m = 1.5}})
local v = gvim.state.get("t")
T.spy(#v.x, v.x[3], v.y, v.z, v.n.m)`, "3 3 s true 1.5"},
		{"get returns a copy", `
gvim.state.set("t", {x = 1})
local v = gvim.state.get("t")
v.x = 2
T.spy(gvim.state.get("t").x)`, "1"},
		{"keys are sorted", `gvim.state.set("b", 1) gvim.state.set("a", 1) gvim.state.set("c", 1) T.spy(table.concat(gvim.state.keys(), ","))`, "a,b,c"},
		{"delete", `gvim.state.set("a", 1) T.spy(gvim.state.delete("a"), tostring(gvim.state.get("a")), #gvim.state.keys())`, "true nil 0"},
		{"setting nil deletes", `gvim.state.set("a", 1) gvim.state.set("a", nil) T.spy(tostring(gvim.state.get("a")))`, "nil"},
		{"a function is refused", `local v, e = gvim.state.set("a", function() end) T.spy(tostring(v), gvim.err_code(e))`, "nil invalid"},
		{"NaN is refused", `local v, e = gvim.state.set("a", 0/0) T.spy(tostring(v), gvim.err_code(e))`, "nil invalid"},
		{"infinity is refused", `local v, e = gvim.state.set("a", 1/0) T.spy(tostring(v), gvim.err_code(e))`, "nil invalid"},
		{"invalid UTF-8 is refused", `local v, e = gvim.state.set("a", "\255") T.spy(tostring(v), gvim.err_code(e))`, "nil invalid"},
		{"a cycle is refused", `local c = {} c.self = c local v, e = gvim.state.set("a", c) T.spy(tostring(v), gvim.err_code(e))`, "nil invalid"},
		{"a handle is refused", `local v, e = gvim.state.set("a", gvim.timer.after(1000, function() end)) T.spy(tostring(v), gvim.err_code(e))`, "nil invalid"},
		{"empty key raises", `local ok, e = pcall(function() return gvim.state.get("") end) T.spy(ok, e:match("bad argument.*$") ~= nil)`, "false true"},
		{"state is capped at 1 MiB", `
local big = string.rep("x", 600000)
T.spy(gvim.state.set("a", big))
local v, e = gvim.state.set("b", big)
T.spy(tostring(v), gvim.err_code(e))
T.spy(gvim.state.set("a", "small"), gvim.state.set("b", big))`, "true|nil limit|true true"},
	}
	for _, r := range rows {
		t.Run(r.name, func(t *testing.T) {
			h := cmd(t, "", r.body)
			if got := spyText(h); got != r.want {
				t.Fatalf("spy = %q, want %q\nlog: %v", got, r.want, h.LogText(""))
			}
		})
	}
}

func fileJSON(t *testing.T, fos *plugintest.FakeOS, path string) map[string]any {
	t.Helper()
	data, ok := fos.File(path)
	if !ok {
		t.Fatalf("%s was not written", path)
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(data), &m); err != nil {
		t.Fatalf("%s: %v\n%s", path, err, data)
	}
	return m
}

func TestStateIsWrittenBehind(t *testing.T) {
	fos := plugintest.NewFakeOS()
	h := startOS(t, fos, `
T.command("set", function(ctx) gvim.state.set(ctx.args.k, ctx.args.v) end)`)
	set := func(k string, v int) {
		t.Helper()
		if err := h.Run("t.set", map[string]any{"k": k, "v": v}); err != nil {
			t.Fatal(err)
		}
	}
	set("a", 1)
	set("b", 2)
	if h.PendingTimers() != 1 {
		t.Fatalf("pending timers = %d, want one debounce timer for both writes", h.PendingTimers())
	}
	h.Advance(499 * ms)
	if fos.Exists(statePath) {
		t.Fatal("written before the debounce ended")
	}
	h.Advance(1 * ms)
	if got := fileJSON(t, fos, statePath); !reflect.DeepEqual(got, map[string]any{"a": 1.0, "b": 2.0}) {
		t.Fatalf("file = %v", got)
	}
	set("a", 3)
	h.Advance(500 * ms)
	if got := fileJSON(t, fos, statePath); !reflect.DeepEqual(got, map[string]any{"a": 3.0, "b": 2.0}) {
		t.Fatalf("file = %v", got)
	}
}

func TestStateFlushedByStop(t *testing.T) {
	fos := plugintest.NewFakeOS()
	h := startOS(t, fos, `
T.command("set", function() gvim.state.set("k", "v") end)`)
	if err := h.Run("t.set", nil); err != nil {
		t.Fatal(err)
	}
	if fos.Exists(statePath) {
		t.Fatal("written at once")
	}
	h.RT.Stop()
	if got := fileJSON(t, fos, statePath); !reflect.DeepEqual(got, map[string]any{"k": "v"}) {
		t.Fatalf("file = %v", got)
	}
}

// TestStateSurvivesReloadInMemory: the store belongs to the plugin name; a reload reads
// it from memory, not from the (not yet written) file.
func TestStateSurvivesReloadInMemory(t *testing.T) {
	src := `
local gvim = require "gvim"
T.command("set", function() gvim.state.set("k", {n = 42}) end)
T.command("get", function() T.spy("got", gvim.state.get("k").n) end)`
	fos := plugintest.NewFakeOS()
	h := start(t, src, plugintest.Options{OS: fos})
	if err := h.Run("t.set", nil); err != nil {
		t.Fatal(err)
	}
	h.Unload("t")
	if err := h.Load("t", src); err != nil {
		t.Fatal(err)
	}
	if err := h.Run("t.get", nil); err != nil {
		t.Fatal(err)
	}
	if got := spyText(h); got != "got 42" {
		t.Fatalf("spy = %q log=%v", got, h.LogText(""))
	}
	if fos.Exists(statePath) {
		t.Fatal("the test needs the file to be unwritten")
	}
}

// TestStateWriterSerialAcrossGenerations: one write in flight per plugin name, later
// changes (also from the next generation) coalesce into the following write, and the
// completion of a write is handled although its generation is gone.
func TestStateWriterSerialAcrossGenerations(t *testing.T) {
	src := `
local gvim = require "gvim"
T.command("set", function(ctx) gvim.state.set(ctx.args.k, ctx.args.v) end)`
	fos := plugintest.NewFakeOS()
	h := start(t, src, plugintest.Options{OS: fos})
	rt := h.RT
	set := func(k string, v int) {
		t.Helper()
		if err := rt.Run("t.set", map[string]any{"k": k, "v": v}, pluginapi.SrcAPI); err != nil {
			t.Fatal(err)
		}
	}
	finish := func(w pluginapi.Work) {
		t.Helper()
		c := w.Run(context.Background())
		c.Token = w.Token
		rt.Resolve(c)
	}

	set("a", 1)
	work := drainAll(rt)
	if len(work) != 1 || work[0].After != 500*time.Millisecond {
		t.Fatalf("want one 500 ms debounce Work, got %+v", work)
	}
	rt.Resolve(pluginapi.Completion{Token: work[0].Token})
	work = drainAll(rt)
	if len(work) != 1 || work[0].Run == nil {
		t.Fatalf("want one write Work, got %+v", work)
	}
	inflight := work[0]

	set("b", 2)
	if w := drainAll(rt); len(w) != 0 {
		t.Fatalf("a second Work started while a write was in flight: %+v", w)
	}
	h.Unload("t")
	if err := h.Load("t", src); err != nil {
		t.Fatal(err)
	}
	set("c", 3)
	if w := drainAll(rt); len(w) != 0 {
		t.Fatalf("the next generation started its own write: %+v", w)
	}

	finish(inflight) // completes for a generation that is gone
	if got := fileJSON(t, fos, statePath); !reflect.DeepEqual(got, map[string]any{"a": 1.0}) {
		t.Fatalf("first write = %v, want the snapshot taken when it started", got)
	}
	work = drainAll(rt)
	if len(work) != 1 || work[0].After != 500*time.Millisecond {
		t.Fatalf("the completion did not schedule the next write: %+v", work)
	}
	rt.Resolve(pluginapi.Completion{Token: work[0].Token})
	work = drainAll(rt)
	if len(work) != 1 || work[0].Run == nil {
		t.Fatalf("want the second write Work, got %+v", work)
	}
	finish(work[0])
	if got := fileJSON(t, fos, statePath); !reflect.DeepEqual(got, map[string]any{"a": 1.0, "b": 2.0, "c": 3.0}) {
		t.Fatalf("second write = %v", got)
	}
	rt.Resolve(pluginapi.Completion{}) // nothing is pending any more
	if w := drainAll(rt); len(w) != 0 {
		t.Fatalf("unexpected Work: %+v", w)
	}
}

func TestStateSeedAndMerge(t *testing.T) {
	t.Run("seed", func(t *testing.T) {
		fos := plugintest.NewFakeOS()
		h := plugintest.New(t, plugintest.Files{"t.lua": `
local gvim = require "gvim"
T.spy(gvim.state.get("x"), gvim.state.get("y"), table.concat(gvim.state.keys(), ","))
gvim.state.set("y", 3)`}, plugintest.Options{OS: fos})
		if err := h.RT.SeedState("t", []byte(`{"x": 1, "y": 2}`)); err != nil {
			t.Fatal(err)
		}
		h.Start()
		if got := spyText(h); got != "1 2 x,y" {
			t.Fatalf("spy = %q", got)
		}
		h.Advance(500 * ms)
		if got := fileJSON(t, fos, statePath); !reflect.DeepEqual(got, map[string]any{"x": 1.0, "y": 3.0}) {
			t.Fatalf("file = %v", got)
		}
	})
	t.Run("a corrupt file is an error", func(t *testing.T) {
		h := plugintest.New(t, plugintest.Files{}, plugintest.Options{})
		if err := h.RT.SeedState("t", []byte(`{nope`)); err == nil {
			t.Fatal("no error")
		}
	})
	t.Run("without a seed the first write keeps the other keys of the file", func(t *testing.T) {
		fos := plugintest.NewFakeOS()
		fos.SetFile(statePath, `{"x": 1, "y": 2, "z": 3}`)
		h := startOS(t, fos, `
gvim.state.set("y", 20)
gvim.state.delete("z")
gvim.state.set("w", 4)`)
		h.Advance(500 * ms)
		if got := fileJSON(t, fos, statePath); !reflect.DeepEqual(got, map[string]any{"x": 1.0, "y": 20.0, "w": 4.0}) {
			t.Fatalf("file = %v", got)
		}
	})
	t.Run("no config directory means memory only", func(t *testing.T) {
		fos := plugintest.NewFakeOS()
		fos.Dir.Config = ""
		h := startOS(t, fos, `gvim.state.set("k", 1) T.spy(gvim.state.get("k"), "[" .. gvim.dirs.data() .. "]")`)
		h.Advance(time.Second)
		h.RT.Stop()
		if got := spyText(h); got != "1 []" {
			t.Fatalf("spy = %q", got)
		}
		for _, c := range fos.Calls() {
			if len(c) > 9 && c[:9] == "WriteFile" {
				t.Fatalf("wrote: %v", fos.Calls())
			}
		}
	})
}

// TestStateFlushDoesNotWaitForAWriteThatNeverStarted: if the program ends while a write Work
// was handed out but never ran, Stop writes the content itself at once, and the late Work
// must not overwrite it with older data.
func TestStateFlushDoesNotWaitForAWriteThatNeverStarted(t *testing.T) {
	fos := plugintest.NewFakeOS()
	h := startOS(t, fos, `
T.command("set", function(ctx) gvim.state.set(ctx.args.k, ctx.args.v) end)`)
	rt := h.RT
	set := func(k string, v int) {
		t.Helper()
		if err := rt.Run("t.set", map[string]any{"k": k, "v": v}, pluginapi.SrcAPI); err != nil {
			t.Fatal(err)
		}
	}
	set("a", 1)
	work := drainAll(rt)
	rt.Resolve(pluginapi.Completion{Token: work[0].Token})
	work = drainAll(rt)
	if len(work) != 1 || work[0].Run == nil {
		t.Fatalf("want the write Work, got %+v", work)
	}
	set("b", 2)
	begin := time.Now()
	rt.Stop()
	if d := time.Since(begin); d > time.Second {
		t.Fatalf("Stop waited %v for a write that never started", d)
	}
	if got := fileJSON(t, fos, statePath); !reflect.DeepEqual(got, map[string]any{"a": 1.0, "b": 2.0}) {
		t.Fatalf("file = %v", got)
	}
	fos.SetFile(statePath, `{"sentinel": true}`)
	work[0].Run(context.Background()) // the late Work
	if got, _ := fos.File(statePath); got != `{"sentinel": true}` {
		t.Fatalf("the late Work overwrote the flushed state: %q", got)
	}
}
