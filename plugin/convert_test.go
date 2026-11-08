package plugin_test

import (
	"strings"
	"testing"
	"time"

	"github.com/tbxark/gvim/plugin"
	"github.com/tbxark/gvim/plugin/plugintest"
	"github.com/tbxark/gvim/pluginapi"
)

func TestConvertLuaToHost(t *testing.T) {
	deep := func(n int) string { // a table nested n levels deep
		return strings.Repeat("{", n) + strings.Repeat("}", n)
	}
	rows := []struct {
		name, expr, want string
	}{
		{"nil", `nil`, "nil"},
		{"true", `true`, "true"},
		{"string", `"héllo"`, `"héllo"`},
		{"integral number is int64", `3`, "i3"},
		{"negative integral", `-7`, "i-7"},
		{"fraction is float64", `0.5`, "f0.5"},
		{"empty table is an empty object", `{}`, "{}"},
		{"object", `{b = 2, a = 1}`, "{a=i1,b=i2}"},
		{"array", `{"x", "y", 3}`, `["x","y",i3]`},
		{"nested", `{x = {1, {y = "s"}}}`, `{x=[i1,{y="s"}]}`},
		{"shared subtable is not a cycle", `(function() local s = {1} return {a = s, b = s} end)()`, "{a=[i1],b=[i1]}"},
		{"mixed array and object", `{1, 2, a = 3}`, "ERR neither an array nor an object"},
		{"hole", `{1, nil, 3}`, "ERR holes"},
		{"sparse integer keys", `{[1] = 1, [3] = 3}`, "ERR holes"},
		{"fractional key", `{[1.5] = 1}`, "ERR neither an array nor an object"},
		{"boolean key", `{[true] = 1}`, "ERR neither an array nor an object"},
		{"function", `print`, "ERR cannot pass a function"},
		{"function in table", `{f = print}`, "ERR cannot pass a function"},
		{"thread", `coroutine.create(print)`, "ERR cannot pass a thread"},
		{"cycle", `(function() local t = {} t.me = t return t end)()`, "ERR cycle"},
		{"depth 32 is fine", deep(32), "OK"},
		{"depth 33 is too deep", deep(33), "ERR deeper than 32"},
		{"Buffer handle", `T.buf(3)`, "buf3"},
		{"Window handle", `T.win(2)`, "win2"},
		{"handle in table", `{b = T.buf(4)}`, "{b=buf4}"},
	}
	for _, r := range rows {
		t.Run(r.name, func(t *testing.T) {
			h := start(t, `T.spy(T.to_host(`+r.expr+`))`)
			got := spyText(h)
			switch {
			case strings.HasPrefix(r.want, "ERR "):
				if !strings.Contains(got, strings.TrimPrefix(r.want, "ERR ")) {
					t.Fatalf("got %q, want an error containing %q", got, r.want)
				}
			case r.want == "OK":
				if strings.Contains(got, "invalid") || strings.Contains(got, "limit") || got == "" {
					t.Fatalf("got %q", got)
				}
			default:
				if got != r.want {
					t.Fatalf("got %q, want %q (log %v)", got, r.want, h.LogText(""))
				}
			}
		})
	}
}

func TestConvertSizeLimit(t *testing.T) {
	h := start(t, `
local t = {}
for i = 1, 1000001 do t[i] = true end
T.spy(T.to_host(t))`, plugintest.Options{Timeout: 5 * time.Second})
	if got := spyText(h); !strings.Contains(got, "limit: table has more than 1000000 nodes") {
		t.Fatalf("spy = %q log=%v", got, h.LogText(""))
	}
}

func TestConvertHostToLua(t *testing.T) {
	rows := []struct {
		spec, expr, want string
	}{
		{"buf:3", `tostring(v)`, "Buffer(3)"},
		{"buf:3", `type(v)`, "userdata"},
		{"buf:0", `tostring(v)`, "nil"},
		{"win:2", `tostring(v)`, "Window(2)"},
		{"pos:3,7", `v.line .. ":" .. v.col`, "3:7"},
		{"range:1,2,3,4", `v.from.line .. v.from.col .. v.to.line .. v.to.col`, "1234"},
		{"version:2.9", `type(v) .. v`, "string2.9"},
		{"key:k", `v.key .. tostring(v.text) .. tostring(v.ctrl) .. tostring(v.alt)`, "kktruefalse"},
		{"hunk:", `v.kind .. v.old_start .. v.old_count .. v.new_start .. v.new_count`, "change1234"},
	}
	for _, r := range rows {
		t.Run(r.spec+" "+r.expr, func(t *testing.T) {
			h := start(t, `local v = T.from_host("`+r.spec+`") T.spy(`+r.expr+`)`)
			if got := spyText(h); got != r.want {
				t.Fatalf("got %q, want %q (log %v)", got, r.want, h.LogText(""))
			}
		})
	}
	t.Run("map round trip", func(t *testing.T) {
		h := start(t, `T.spy(T.to_host(T.from_host("map")))`)
		if got := spyText(h); got != `{a=i1,b=["x",true,f2.5],c={}}` {
			t.Fatalf("got %q", got)
		}
	})
}

func TestHandleIdentity(t *testing.T) {
	rows := []struct {
		name, expr, want string
	}{
		{"same id is the same handle", `return T.buf(1) == T.buf(1)`, "true"},
		{"same userdata", `return rawequal(T.buf(1), T.buf(1))`, "true"},
		{"usable as a table key", `local t = {} t[T.buf(1)] = "x" return t[T.buf(1)]`, "x"},
		{"other id differs", `return T.buf(1) == T.buf(2)`, "false"},
		{"kinds differ", `return T.buf(1) == T.win(1)`, "false"},
		{"tostring", `return tostring(T.buf(7)) .. tostring(T.win(2))`, "Buffer(7)Window(2)"},
		{"id field", `return T.buf(7).id`, "7"},
		{"unknown field", `return tostring(T.buf(7).nothing)`, "nil"},
		{"locked metatable", `return getmetatable(T.buf(1))`, "locked"},
		{"cannot set fields", `return (pcall(function() T.buf(1).x = 1 end))`, "false"},
		{"cannot change the metatable", `return (pcall(setmetatable, T.buf(1), {}))`, "false"},
		{"type", `return type(T.win(1))`, "userdata"},
	}
	for _, r := range rows {
		t.Run(r.name, func(t *testing.T) {
			h := start(t, `T.spy((function() `+r.expr+` end)())`)
			if got := spyText(h); got != r.want {
				t.Fatalf("got %q, want %q (log %v)", got, r.want, h.LogText(""))
			}
		})
	}
}

func TestHandleValid(t *testing.T) {
	host := plugintest.NewFakeHost()
	id := host.AddBuffer("a.go", "x")
	h := start(t, `
T.command("go", function() T.spy(T.buf(1):valid(), T.buf(99):valid()) end)`, plugintest.Options{Host: host})
	_ = h.Run("t.go", nil)
	host.RemoveBuffer(id)
	_ = h.Run("t.go", nil)
	if got := spyText(h); got != "true false|false false" {
		t.Fatalf("spy = %q", got)
	}
}

func TestHandleCachePerStatePurged(t *testing.T) {
	host := plugintest.NewFakeHost()
	var ids []pluginapi.BufferID
	for i := 0; i < 1000; i++ {
		ids = append(ids, host.AddBuffer("f", "x"))
	}
	h := startFiles(t, plugintest.Files{
		"a.lua": `T.command("make", function() for i = 1, 1000 do T.buf(i) end T.spy("a", T.cache_size()) end)`,
		"b.lua": `T.command("make", function() T.buf(1) T.spy("b", T.cache_size()) end)`,
	}, plugintest.Options{Host: host})
	base := plugin.HandleCacheSize(h.RT, "a")
	if err := h.Run("a.make", nil); err != nil {
		t.Fatal(err)
	}
	if err := h.Run("b.make", nil); err != nil {
		t.Fatal(err)
	}
	if got := spyText(h); got != "a 1000|b 1" {
		t.Fatalf("each state has its own cache: %q", got)
	}
	// Closing the buffers drops the handles from every state, with or without a subscriber.
	for _, id := range ids {
		host.RemoveBuffer(id)
		h.RT.Emit(pluginapi.Event{Name: "buffer_closed", Data: map[string]any{"id": id}})
	}
	h.Settle()
	if n := plugin.HandleCacheSize(h.RT, "a"); n != base {
		t.Fatalf("cache of a = %d after closing 1000 buffers, want %d", n, base)
	}
	if n := plugin.HandleCacheSize(h.RT, "b"); n != base {
		t.Fatalf("cache of b = %d, want %d", n, base)
	}
}

func TestHandleCacheCapEvictsInvalidFirst(t *testing.T) {
	host := plugintest.NewFakeHost()
	for i := 0; i < 10_050; i++ {
		host.AddBuffer("f", "x")
	}
	h := start(t, `T.command("make", function() for i = 1, 10050 do T.buf(i) end end)`, plugintest.Options{Host: host, Timeout: 5 * time.Second})
	if err := h.Run("t.make", nil); err != nil {
		t.Fatal(err)
	}
	n := plugin.HandleCacheSize(h.RT, "t")
	if n > 10_000 || n < 8_000 {
		t.Fatalf("cache size = %d, want at most 10000", n)
	}
}

func TestConvertHostStructs(t *testing.T) {
	rows := []struct{ spec, expr, want string }{
		{"changerec:", `v.range.from.col .. v.range.to.col .. v.text .. v.removed`, "25newold"},
		{"caret:", `#v .. v[1].pos.line .. v[1].anchor.col .. tostring(v[2].anchor)`, "214nil"},
		{"info:", `tostring(v.id) .. v.name .. v.line_count .. v.version .. tostring(v.modified)`, "Buffer(9)n31.2false"},
	}
	for _, r := range rows {
		t.Run(r.spec, func(t *testing.T) {
			h := start(t, `local v = T.from_host("`+r.spec+`") T.spy(`+r.expr+`)`)
			if got := spyText(h); got != r.want {
				t.Fatalf("got %q, want %q (log %v)", got, r.want, h.LogText(""))
			}
		})
	}
}

func TestSnakeCase(t *testing.T) {
	for in, want := range map[string]string{
		"ID": "id", "Name": "name", "OldStart": "old_start", "LineCount": "line_count", "ReadOnly": "read_only",
		"HTTPServer": "http_server", "Text": "text", "A": "a", "FG": "fg", "NewCount": "new_count",
	} {
		if got := plugin.SnakeCase(in); got != want {
			t.Errorf("SnakeCase(%q) = %q, want %q", in, got, want)
		}
	}
}
