package plugin

import (
	"fmt"
	"reflect"
	"sort"
	"sync"
	"testing"

	lua "github.com/yuin/gopher-lua"

	"github.com/tbxark/gvim/pluginapi"
)

// reflectToLua mirrors the generic reflection path for structs and slices so tests can
// verify that every fast-path case in toLua produces an identical Lua table.
func reflectToLua(p *plugin, v any) lua.LValue {
	switch x := v.(type) {
	case nil, lua.LValue, bool, string, []byte,
		int, int8, int16, int32, int64,
		uint, uint8, uint16, uint32, uint64,
		float32, float64,
		pluginapi.Token, pluginapi.BufferID, pluginapi.WinID,
		pluginapi.Pos, *pluginapi.Pos, pluginapi.Range,
		pluginapi.Version, pluginapi.Key, pluginapi.Hunk,
		[]any, []string, map[string]any, map[string]string:
		return p.toLua(x)
	}
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Slice, reflect.Array:
		t := p.L.CreateTable(rv.Len(), 0)
		for i := range rv.Len() {
			t.Append(reflectToLua(p, rv.Index(i).Interface()))
		}
		return t
	case reflect.Map:
		if rv.Type().Key().Kind() == reflect.String {
			t := p.L.CreateTable(0, rv.Len())
			for _, k := range rv.MapKeys() {
				t.RawSetString(k.String(), reflectToLua(p, rv.MapIndex(k).Interface()))
			}
			return t
		}
	case reflect.Pointer:
		if rv.IsNil() {
			return lua.LNil
		}
		return reflectToLua(p, rv.Elem().Interface())
	case reflect.Struct:
		rt := rv.Type()
		t := p.L.CreateTable(0, rt.NumField())
		for i := range rt.NumField() {
			if f := rt.Field(i); f.IsExported() {
				t.RawSetString(snakeCase(f.Name), reflectToLua(p, rv.Field(i).Interface()))
			}
		}
		return t
	}
	return lua.LString(fmt.Sprint(v))
}

func assertLuaEqual(t *testing.T, got, want lua.LValue, path string) {
	t.Helper()
	if got.Type() != want.Type() {
		t.Fatalf("%s: type = %s (%v), want %s (%v)", path, got.Type(), got, want.Type(), want)
	}
	switch g := got.(type) {
	case *lua.LTable:
		w := want.(*lua.LTable)
		if g.Len() != w.Len() {
			t.Fatalf("%s: array len = %d, want %d", path, g.Len(), w.Len())
		}
		for i := 1; i <= g.Len(); i++ {
			assertLuaEqual(t, g.RawGetInt(i), w.RawGetInt(i), fmt.Sprintf("%s[%d]", path, i))
		}
		gKeys := map[string]lua.LValue{}
		wKeys := map[string]lua.LValue{}
		g.ForEach(func(k, v lua.LValue) {
			if ks, ok := k.(lua.LString); ok {
				gKeys[string(ks)] = v
			}
		})
		w.ForEach(func(k, v lua.LValue) {
			if ks, ok := k.(lua.LString); ok {
				wKeys[string(ks)] = v
			}
		})
		if len(gKeys) != len(wKeys) {
			var gk, wk []string
			for k := range gKeys {
				gk = append(gk, k)
			}
			for k := range wKeys {
				wk = append(wk, k)
			}
			sort.Strings(gk)
			sort.Strings(wk)
			t.Fatalf("%s: string keys = %v, want %v", path, gk, wk)
		}
		for k, wv := range wKeys {
			gv, ok := gKeys[k]
			if !ok {
				t.Fatalf("%s: missing key %q", path, k)
			}
			assertLuaEqual(t, gv, wv, path+"."+k)
		}
	case *lua.LUserData:
		if g != want {
			t.Fatalf("%s: userdata %v != %v", path, g.Value, want.(*lua.LUserData).Value)
		}
	default:
		if got.String() != want.String() {
			t.Fatalf("%s: got %q, want %q", path, got.String(), want.String())
		}
	}
}

func newConvertTestPlugin(t *testing.T) *plugin {
	t.Helper()
	L := lua.NewState()
	t.Cleanup(L.Close)
	return &plugin{
		L:       L,
		handles: map[idHandle]*lua.LUserData{},
		kinds:   map[string]*handleKind{},
	}
}

func TestToLuaDirectMatchesReflection(t *testing.T) {
	p := newConvertTestPlugin(t)
	anchor := pluginapi.Pos{Line: 3, Col: 7}
	r := pluginapi.Range{From: pluginapi.Pos{Line: 1, Col: 2}, To: pluginapi.Pos{Line: 4, Col: 5}}
	ver := pluginapi.Version{Gen: 2, Ver: 9}

	cases := []struct {
		name string
		val  any
	}{
		{"ChangeRec zero", pluginapi.ChangeRec{}},
		{"ChangeRec populated", pluginapi.ChangeRec{Range: r, Text: "ins", Removed: "del"}},
		{"[]ChangeRec nil", []pluginapi.ChangeRec(nil)},
		{"[]ChangeRec populated", []pluginapi.ChangeRec{{Range: r, Text: "a"}, {Text: "", Removed: "b"}}},
		{"Changes zero", pluginapi.Changes{}},
		{"Changes populated", pluginapi.Changes{Version: ver, Undo: true, Overflow: false, Edits: []pluginapi.ChangeRec{{Range: r, Text: "x"}}}},
		{"Edit zero", pluginapi.Edit{}},
		{"[]Edit populated", []pluginapi.Edit{{Range: r, Text: "hi"}}},
		{"Caret nil anchor", pluginapi.Caret{Pos: anchor}},
		{"[]Caret mixed", []pluginapi.Caret{{Pos: anchor, Anchor: &anchor}, {Pos: pluginapi.Pos{Line: 5}}}},
		{"[]Hunk populated", []pluginapi.Hunk{{Kind: "insert", OldStart: 1, OldCount: 0, NewStart: 1, NewCount: 2}}},
		{"GrepHit zero", pluginapi.GrepHit{}},
		{"[]GrepHit populated", []pluginapi.GrepHit{{Path: "a.go", Line: 3, Col: 8, Text: "hello"}}},
		{"BufferInfo zero", pluginapi.BufferInfo{}},
		{"BufferInfo populated", pluginapi.BufferInfo{
			ID: 9, Path: "/a.go", Name: "a.go", Language: "go",
			Encoding: "UTF-8", LineEnding: "LF", Mode: "normal",
			Modified: true, ReadOnly: true, Virtual: true, Large: true,
			Preview: true, Active: true, LineCount: -1, Version: ver,
		}},
		{"[]BufferInfo", []pluginapi.BufferInfo{{ID: 1, Name: "a"}, {ID: 0, Name: "b"}}},
		{"WindowInfo zero", pluginapi.WindowInfo{}},
		{"[]WindowInfo populated", []pluginapi.WindowInfo{{ID: 2, Buffer: 5, X: 1, Y: 2, Width: 80, Height: 24, TopLine: 10, LeftCol: 4, Active: true}}},
		{"[]VisibleRow", []pluginapi.VisibleRow{{Line: 3, ColFrom: 0, ColTo: 40, Wrapped: true}}},
		{"[]DiagnosticInfo", []pluginapi.DiagnosticInfo{{Range: r, Severity: "error", Message: "boom", Source: "gopls"}}},
		{"[]FileDiagnostics", []pluginapi.FileDiagnostics{{Path: "a.go", Diagnostics: []pluginapi.DiagnosticInfo{{Range: r, Severity: "hint"}}}}},
		{"AgentReport zero", pluginapi.AgentReport{}},
		{"AgentReport populated", pluginapi.AgentReport{
			Supported: true, Installed: true, Truncated: true,
			Sessions: []pluginapi.AgentSession{{
				Kind: "tmux", Tool: "tmux", ID: "s1", Title: "t", Path: "/p", Dir: "/d",
				Agent: "codex", Agents: []string{"codex", "claude"}, Attached: true, SocketOnly: true, Exact: true, ModTime: 1234,
			}},
			Problems: []string{"warn"},
		}},
		{"WidgetEvent zero", pluginapi.WidgetEvent{}},
		{"WidgetEvent populated", pluginapi.WidgetEvent{
			Panel: 7, Event: "click", Widget: "btn", Index: 2, Checked: true,
			Value: "v", CursorByte: 4, Included: []string{"a", "b"}, FocusKey: "fk",
			TargetKey: "tk", TargetIndex: 1, X: 10, Y: 20, Button: "left", Wheel: -1,
		}},
		{"EnvInfo", pluginapi.EnvInfo{Name: ".venv", Kind: "shell", Snippet: "source .venv/bin/activate"}},
		{"[]FuzzyMatch", []pluginapi.FuzzyMatch{{Index: 2, Score: 99, Indexes: []int{0, 3, 4}}}},
		{"[]int nil", []int(nil)},
		{"[]int populated", []int{1, 2, 3}},
		{"[]Pos", []pluginapi.Pos{anchor}},
		{"[]Range", []pluginapi.Range{r}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := p.toLua(tc.val)
			want := reflectToLua(p, tc.val)
			assertLuaEqual(t, got, want, tc.name)
		})
	}
}

func TestSnakeCaseCacheConcurrent(t *testing.T) {
	inputs := []string{"ID", "OldStart", "LineCount", "HTTPServer", "SocketOnly", "CursorByte", "FG"}
	want := []string{"id", "old_start", "line_count", "http_server", "socket_only", "cursor_byte", "fg"}
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			for range 100 {
				for i, in := range inputs {
					if got := snakeCase(in); got != want[i] {
						t.Errorf("snakeCase(%q) = %q, want %q", in, got, want[i])
					}
				}
			}
		})
	}
	wg.Wait()
}

func BenchmarkSnakeCase(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		_ = snakeCase("LineCount")
		_ = snakeCase("OldStart")
		_ = snakeCase("SocketOnly")
	}
}

func BenchmarkToLuaChangeRecs(b *testing.B) {
	L := lua.NewState()
	defer L.Close()
	p := &plugin{
		L:       L,
		handles: map[idHandle]*lua.LUserData{},
		kinds:   map[string]*handleKind{},
	}
	recs := make([]pluginapi.ChangeRec, 32)
	for i := range recs {
		recs[i] = pluginapi.ChangeRec{
			Range:   pluginapi.Range{From: pluginapi.Pos{Line: i, Col: 1}, To: pluginapi.Pos{Line: i, Col: 4}},
			Text:    "abc",
			Removed: "xyz",
		}
	}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		_ = p.toLua(recs)
	}
}

func BenchmarkToLuaGrepHits(b *testing.B) {
	L := lua.NewState()
	defer L.Close()
	p := &plugin{
		L:       L,
		handles: map[idHandle]*lua.LUserData{},
		kinds:   map[string]*handleKind{},
	}
	hits := make([]pluginapi.GrepHit, 100)
	for i := range hits {
		hits[i] = pluginapi.GrepHit{Path: "internal/app/run.go", Line: i * 3, Col: 12, Text: "func Run(opts Options) error {"}
	}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		_ = p.toLua(hits)
	}
}
