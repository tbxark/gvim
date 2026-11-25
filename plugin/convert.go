package plugin

import (
	"errors"
	"fmt"
	"math"
	"reflect"
	"sort"
	"strings"
	"sync"
	"unicode"

	lua "github.com/yuin/gopher-lua"

	"github.com/tbxark/gvim/pluginapi"
)

// ---------- Go -> Lua ----------

// toLua converts a host value to a Lua value of this plugin's state. Plain values map to
// their Lua counterparts, slices to 1-based arrays, maps to tables; BufferID and WinID
// become handles, Pos/Range/Key/Hunk tables, Version its string token. A lua.LValue
// passes through (runtime-owned opaque values).
func (p *plugin) toLua(v any) lua.LValue {
	switch x := v.(type) {
	case nil:
		return lua.LNil
	case lua.LValue:
		return x
	case bool:
		return lua.LBool(x)
	case string:
		return lua.LString(x)
	case []byte:
		return lua.LString(x)
	case int:
		return lua.LNumber(x)
	case int8:
		return lua.LNumber(x)
	case int16:
		return lua.LNumber(x)
	case int32:
		return lua.LNumber(x)
	case int64:
		return lua.LNumber(x)
	case uint:
		return lua.LNumber(x)
	case uint8:
		return lua.LNumber(x)
	case uint16:
		return lua.LNumber(x)
	case uint32:
		return lua.LNumber(x)
	case uint64:
		return lua.LNumber(x)
	case float32:
		return lua.LNumber(x)
	case float64:
		return lua.LNumber(x)
	case pluginapi.Token:
		return lua.LNumber(x)
	case pluginapi.BufferID:
		if x == 0 {
			return lua.LNil
		}
		return p.idHandle(kindBuffer, uint64(x))
	case pluginapi.WinID:
		if x == 0 {
			return lua.LNil
		}
		return p.idHandle(kindWindow, uint64(x))
	case pluginapi.Pos:
		return p.posTable(x)
	case *pluginapi.Pos:
		if x == nil {
			return lua.LNil
		}
		return p.posTable(*x)
	case pluginapi.Range:
		return p.rangeTable(x)
	case pluginapi.Version:
		return lua.LString(x.String())
	case pluginapi.Key:
		t := p.L.CreateTable(0, 6)
		t.RawSetString("key", lua.LString(x.Key))
		if x.Text != "" {
			t.RawSetString("text", lua.LString(x.Text))
		}
		t.RawSetString("ctrl", lua.LBool(x.Ctrl))
		t.RawSetString("alt", lua.LBool(x.Alt))
		t.RawSetString("shift", lua.LBool(x.Shift))
		t.RawSetString("super", lua.LBool(x.Super))
		return t
	case pluginapi.Hunk:
		return p.hunkTable(x)
	case []pluginapi.Hunk:
		t := p.L.CreateTable(len(x), 0)
		for _, h := range x {
			t.Append(p.hunkTable(h))
		}
		return t
	case pluginapi.ChangeRec:
		return p.changeRecTable(x)
	case []pluginapi.ChangeRec:
		return p.changeRecsTable(x)
	case pluginapi.Changes:
		t := p.L.CreateTable(0, 4)
		t.RawSetString("version", lua.LString(x.Version.String()))
		t.RawSetString("edits", p.changeRecsTable(x.Edits))
		t.RawSetString("undo", lua.LBool(x.Undo))
		t.RawSetString("overflow", lua.LBool(x.Overflow))
		return t
	case pluginapi.Edit:
		return p.editTable(x)
	case []pluginapi.Edit:
		t := p.L.CreateTable(len(x), 0)
		for _, e := range x {
			t.Append(p.editTable(e))
		}
		return t
	case pluginapi.Caret:
		return p.caretTable(x)
	case []pluginapi.Caret:
		t := p.L.CreateTable(len(x), 0)
		for _, c := range x {
			t.Append(p.caretTable(c))
		}
		return t
	case pluginapi.GrepHit:
		return p.grepHitTable(x)
	case []pluginapi.GrepHit:
		t := p.L.CreateTable(len(x), 0)
		for _, h := range x {
			t.Append(p.grepHitTable(h))
		}
		return t
	case pluginapi.BufferInfo:
		return p.bufferInfoTable(x)
	case []pluginapi.BufferInfo:
		t := p.L.CreateTable(len(x), 0)
		for _, b := range x {
			t.Append(p.bufferInfoTable(b))
		}
		return t
	case pluginapi.WindowInfo:
		return p.windowInfoTable(x)
	case []pluginapi.WindowInfo:
		t := p.L.CreateTable(len(x), 0)
		for _, w := range x {
			t.Append(p.windowInfoTable(w))
		}
		return t
	case pluginapi.VisibleRow:
		return p.visibleRowTable(x)
	case []pluginapi.VisibleRow:
		t := p.L.CreateTable(len(x), 0)
		for _, r := range x {
			t.Append(p.visibleRowTable(r))
		}
		return t
	case pluginapi.DiagnosticInfo:
		return p.diagnosticInfoTable(x)
	case []pluginapi.DiagnosticInfo:
		return p.diagnosticInfosTable(x)
	case pluginapi.FileDiagnostics:
		return p.fileDiagnosticsTable(x)
	case []pluginapi.FileDiagnostics:
		t := p.L.CreateTable(len(x), 0)
		for _, fd := range x {
			t.Append(p.fileDiagnosticsTable(fd))
		}
		return t
	case pluginapi.AgentSession:
		return p.agentSessionTable(x)
	case []pluginapi.AgentSession:
		return p.agentSessionsTable(x)
	case pluginapi.AgentReport:
		t := p.L.CreateTable(0, 5)
		t.RawSetString("supported", lua.LBool(x.Supported))
		t.RawSetString("installed", lua.LBool(x.Installed))
		t.RawSetString("truncated", lua.LBool(x.Truncated))
		t.RawSetString("sessions", p.agentSessionsTable(x.Sessions))
		t.RawSetString("problems", p.stringsTable(x.Problems))
		return t
	case pluginapi.WidgetEvent:
		t := p.L.CreateTable(0, 15)
		t.RawSetString("panel", lua.LNumber(x.Panel))
		t.RawSetString("event", lua.LString(x.Event))
		t.RawSetString("widget", lua.LString(x.Widget))
		t.RawSetString("index", lua.LNumber(x.Index))
		t.RawSetString("checked", lua.LBool(x.Checked))
		t.RawSetString("value", lua.LString(x.Value))
		t.RawSetString("cursor_byte", lua.LNumber(x.CursorByte))
		t.RawSetString("included", p.stringsTable(x.Included))
		t.RawSetString("focus_key", lua.LString(x.FocusKey))
		t.RawSetString("target_key", lua.LString(x.TargetKey))
		t.RawSetString("target_index", lua.LNumber(x.TargetIndex))
		t.RawSetString("x", lua.LNumber(x.X))
		t.RawSetString("y", lua.LNumber(x.Y))
		t.RawSetString("button", lua.LString(x.Button))
		t.RawSetString("wheel", lua.LNumber(x.Wheel))
		return t
	case pluginapi.EnvInfo:
		t := p.L.CreateTable(0, 3)
		t.RawSetString("name", lua.LString(x.Name))
		t.RawSetString("kind", lua.LString(x.Kind))
		t.RawSetString("snippet", lua.LString(x.Snippet))
		return t
	case pluginapi.FuzzyMatch:
		return p.fuzzyMatchTable(x)
	case []pluginapi.FuzzyMatch:
		t := p.L.CreateTable(len(x), 0)
		for _, m := range x {
			t.Append(p.fuzzyMatchTable(m))
		}
		return t
	case []int:
		return p.intsTable(x)
	case []pluginapi.Pos:
		t := p.L.CreateTable(len(x), 0)
		for _, pos := range x {
			t.Append(p.posTable(pos))
		}
		return t
	case []pluginapi.Range:
		t := p.L.CreateTable(len(x), 0)
		for _, r := range x {
			t.Append(p.rangeTable(r))
		}
		return t
	case []any:
		t := p.L.CreateTable(len(x), 0)
		for _, e := range x {
			t.Append(p.toLua(e))
		}
		return t
	case []string:
		return p.stringsTable(x)
	case map[string]any:
		t := p.L.CreateTable(0, len(x))
		for k, e := range x {
			t.RawSetString(k, p.toLua(e))
		}
		return t
	case map[string]string:
		t := p.L.CreateTable(0, len(x))
		for k, e := range x {
			t.RawSetString(k, lua.LString(e))
		}
		return t
	}
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Slice, reflect.Array:
		t := p.L.CreateTable(rv.Len(), 0)
		for i := range rv.Len() {
			t.Append(p.toLua(rv.Index(i).Interface()))
		}
		return t
	case reflect.Map:
		if rv.Type().Key().Kind() == reflect.String {
			t := p.L.CreateTable(0, rv.Len())
			for _, k := range rv.MapKeys() {
				t.RawSetString(k.String(), p.toLua(rv.MapIndex(k).Interface()))
			}
			return t
		}
	case reflect.Pointer:
		if rv.IsNil() {
			return lua.LNil
		}
		return p.toLua(rv.Elem().Interface())
	case reflect.Struct:
		// Any other host struct (ChangeRec, Edit, Caret ...) becomes a table of its exported
		// fields with snake_case keys; a nil pointer or empty string field stays nil/"".
		rt := rv.Type()
		t := p.L.CreateTable(0, rt.NumField())
		for i := range rt.NumField() {
			if f := rt.Field(i); f.IsExported() {
				t.RawSetString(snakeCase(f.Name), p.toLua(rv.Field(i).Interface()))
			}
		}
		return t
	}
	return lua.LString(fmt.Sprint(v))
}

var snakeCaseCache sync.Map

// snakeCase turns a Go field name into the key Lua sees: OldStart -> old_start, ID -> id.
func snakeCase(name string) string {
	if cached, ok := snakeCaseCache.Load(name); ok {
		return cached.(string)
	}
	var sb strings.Builder
	rs := []rune(name)
	for i, r := range rs {
		if unicode.IsUpper(r) {
			prevLower := i > 0 && unicode.IsLower(rs[i-1])
			nextLower := i+1 < len(rs) && unicode.IsLower(rs[i+1])
			if i > 0 && (prevLower || (unicode.IsUpper(rs[i-1]) && nextLower)) {
				sb.WriteByte('_')
			}
			r = unicode.ToLower(r)
		}
		sb.WriteRune(r)
	}
	out := sb.String()
	snakeCaseCache.Store(name, out)
	return out
}

func (p *plugin) stringsTable(xs []string) *lua.LTable {
	t := p.L.CreateTable(len(xs), 0)
	for _, s := range xs {
		t.Append(lua.LString(s))
	}
	return t
}

func (p *plugin) intsTable(xs []int) *lua.LTable {
	t := p.L.CreateTable(len(xs), 0)
	for _, n := range xs {
		t.Append(lua.LNumber(n))
	}
	return t
}

func (p *plugin) hunkTable(x pluginapi.Hunk) *lua.LTable {
	t := p.L.CreateTable(0, 5)
	t.RawSetString("kind", lua.LString(x.Kind))
	t.RawSetString("old_start", lua.LNumber(x.OldStart))
	t.RawSetString("old_count", lua.LNumber(x.OldCount))
	t.RawSetString("new_start", lua.LNumber(x.NewStart))
	t.RawSetString("new_count", lua.LNumber(x.NewCount))
	return t
}

func (p *plugin) changeRecTable(x pluginapi.ChangeRec) *lua.LTable {
	t := p.L.CreateTable(0, 3)
	t.RawSetString("range", p.rangeTable(x.Range))
	t.RawSetString("text", lua.LString(x.Text))
	t.RawSetString("removed", lua.LString(x.Removed))
	return t
}

func (p *plugin) changeRecsTable(xs []pluginapi.ChangeRec) *lua.LTable {
	t := p.L.CreateTable(len(xs), 0)
	for _, rec := range xs {
		t.Append(p.changeRecTable(rec))
	}
	return t
}

func (p *plugin) editTable(x pluginapi.Edit) *lua.LTable {
	t := p.L.CreateTable(0, 2)
	t.RawSetString("range", p.rangeTable(x.Range))
	t.RawSetString("text", lua.LString(x.Text))
	return t
}

func (p *plugin) caretTable(x pluginapi.Caret) *lua.LTable {
	t := p.L.CreateTable(0, 2)
	t.RawSetString("pos", p.posTable(x.Pos))
	if x.Anchor != nil {
		t.RawSetString("anchor", p.posTable(*x.Anchor))
	}
	return t
}

func (p *plugin) grepHitTable(x pluginapi.GrepHit) *lua.LTable {
	t := p.L.CreateTable(0, 4)
	t.RawSetString("path", lua.LString(x.Path))
	t.RawSetString("line", lua.LNumber(x.Line))
	t.RawSetString("col", lua.LNumber(x.Col))
	t.RawSetString("text", lua.LString(x.Text))
	return t
}

func (p *plugin) bufferInfoTable(x pluginapi.BufferInfo) *lua.LTable {
	t := p.L.CreateTable(0, 15)
	if x.ID != 0 {
		t.RawSetString("id", p.idHandle(kindBuffer, uint64(x.ID)))
	}
	t.RawSetString("path", lua.LString(x.Path))
	t.RawSetString("name", lua.LString(x.Name))
	t.RawSetString("language", lua.LString(x.Language))
	t.RawSetString("encoding", lua.LString(x.Encoding))
	t.RawSetString("line_ending", lua.LString(x.LineEnding))
	t.RawSetString("mode", lua.LString(x.Mode))
	t.RawSetString("modified", lua.LBool(x.Modified))
	t.RawSetString("read_only", lua.LBool(x.ReadOnly))
	t.RawSetString("virtual", lua.LBool(x.Virtual))
	t.RawSetString("large", lua.LBool(x.Large))
	t.RawSetString("preview", lua.LBool(x.Preview))
	t.RawSetString("active", lua.LBool(x.Active))
	t.RawSetString("line_count", lua.LNumber(x.LineCount))
	t.RawSetString("version", lua.LString(x.Version.String()))
	return t
}

func (p *plugin) windowInfoTable(x pluginapi.WindowInfo) *lua.LTable {
	t := p.L.CreateTable(0, 9)
	if x.ID != 0 {
		t.RawSetString("id", p.idHandle(kindWindow, uint64(x.ID)))
	}
	if x.Buffer != 0 {
		t.RawSetString("buffer", p.idHandle(kindBuffer, uint64(x.Buffer)))
	}
	t.RawSetString("x", lua.LNumber(x.X))
	t.RawSetString("y", lua.LNumber(x.Y))
	t.RawSetString("width", lua.LNumber(x.Width))
	t.RawSetString("height", lua.LNumber(x.Height))
	t.RawSetString("top_line", lua.LNumber(x.TopLine))
	t.RawSetString("left_col", lua.LNumber(x.LeftCol))
	t.RawSetString("active", lua.LBool(x.Active))
	return t
}

func (p *plugin) visibleRowTable(x pluginapi.VisibleRow) *lua.LTable {
	t := p.L.CreateTable(0, 4)
	t.RawSetString("line", lua.LNumber(x.Line))
	t.RawSetString("col_from", lua.LNumber(x.ColFrom))
	t.RawSetString("col_to", lua.LNumber(x.ColTo))
	t.RawSetString("wrapped", lua.LBool(x.Wrapped))
	return t
}

func (p *plugin) diagnosticInfoTable(x pluginapi.DiagnosticInfo) *lua.LTable {
	t := p.L.CreateTable(0, 4)
	t.RawSetString("range", p.rangeTable(x.Range))
	t.RawSetString("severity", lua.LString(x.Severity))
	t.RawSetString("message", lua.LString(x.Message))
	t.RawSetString("source", lua.LString(x.Source))
	return t
}

func (p *plugin) diagnosticInfosTable(xs []pluginapi.DiagnosticInfo) *lua.LTable {
	t := p.L.CreateTable(len(xs), 0)
	for _, d := range xs {
		t.Append(p.diagnosticInfoTable(d))
	}
	return t
}

func (p *plugin) fileDiagnosticsTable(x pluginapi.FileDiagnostics) *lua.LTable {
	t := p.L.CreateTable(0, 2)
	t.RawSetString("path", lua.LString(x.Path))
	t.RawSetString("diagnostics", p.diagnosticInfosTable(x.Diagnostics))
	return t
}

func (p *plugin) agentSessionTable(x pluginapi.AgentSession) *lua.LTable {
	t := p.L.CreateTable(0, 12)
	t.RawSetString("kind", lua.LString(x.Kind))
	t.RawSetString("tool", lua.LString(x.Tool))
	t.RawSetString("id", lua.LString(x.ID))
	t.RawSetString("title", lua.LString(x.Title))
	t.RawSetString("path", lua.LString(x.Path))
	t.RawSetString("dir", lua.LString(x.Dir))
	t.RawSetString("agent", lua.LString(x.Agent))
	t.RawSetString("agents", p.stringsTable(x.Agents))
	t.RawSetString("attached", lua.LBool(x.Attached))
	t.RawSetString("socket_only", lua.LBool(x.SocketOnly))
	t.RawSetString("exact", lua.LBool(x.Exact))
	t.RawSetString("mod_time", lua.LNumber(x.ModTime))
	return t
}

func (p *plugin) agentSessionsTable(xs []pluginapi.AgentSession) *lua.LTable {
	t := p.L.CreateTable(len(xs), 0)
	for _, s := range xs {
		t.Append(p.agentSessionTable(s))
	}
	return t
}

func (p *plugin) fuzzyMatchTable(x pluginapi.FuzzyMatch) *lua.LTable {
	t := p.L.CreateTable(0, 3)
	t.RawSetString("index", lua.LNumber(x.Index))
	t.RawSetString("score", lua.LNumber(x.Score))
	t.RawSetString("indexes", p.intsTable(x.Indexes))
	return t
}

func (p *plugin) posTable(x pluginapi.Pos) *lua.LTable {
	t := p.L.CreateTable(0, 2)
	t.RawSetString("line", lua.LNumber(x.Line))
	t.RawSetString("col", lua.LNumber(x.Col))
	return t
}

func (p *plugin) rangeTable(x pluginapi.Range) *lua.LTable {
	t := p.L.CreateTable(0, 2)
	t.RawSetString("from", p.posTable(x.From))
	t.RawSetString("to", p.posTable(x.To))
	return t
}

// ---------- Lua -> Go ----------

// maxExactInt is the largest integer a float64 holds exactly (2^53).
const maxExactInt = 1 << 53

// fromLua converts a Lua value to a plain Go value for the host: nil, bool, int64 (an
// integral number within 2^53), float64, string, []any (a table with keys 1..n),
// map[string]any (a table with string keys; an empty table is an empty object, unless it
// is marked with json.array: then it is an empty []any),
// pluginapi.BufferID/WinID (handles), json.null (nil). Functions, threads and other userdata are
// rejected, a table with other keys, a cycle, more than maxHostNodes nodes or more than
// maxHostDepth levels is an "invalid" error.
func (p *plugin) fromLua(v lua.LValue) (any, error) {
	c := converter{p: p, seen: map[*lua.LTable]bool{}}
	return c.from(v, 0)
}

type converter struct {
	p     *plugin
	nodes int
	seen  map[*lua.LTable]bool
}

func (c *converter) from(v lua.LValue, depth int) (any, error) {
	c.nodes++
	if c.nodes > maxHostNodes {
		return nil, pluginapi.Errf(pluginapi.CodeLimit, "table has more than %d nodes", maxHostNodes)
	}
	switch x := v.(type) {
	case *lua.LNilType:
		return nil, nil
	case lua.LBool:
		return bool(x), nil
	case lua.LNumber:
		f := float64(x)
		if f == math.Trunc(f) && math.Abs(f) < maxExactInt {
			return int64(f), nil
		}
		return f, nil
	case lua.LString:
		if len(x) > MaxString {
			return nil, pluginapi.Errf(pluginapi.CodeLimit, "string is longer than %d bytes", MaxString)
		}
		return string(x), nil
	case *lua.LUserData:
		if x == c.p.data.null {
			return nil, nil // json.null: a null element keeps its place in an array
		}
		if h, ok := x.Value.(idHandle); ok {
			switch h.kind {
			case kindBuffer:
				return pluginapi.BufferID(h.id), nil
			case kindWindow:
				return pluginapi.WinID(h.id), nil
			}
		}
		return nil, pluginapi.Errf(pluginapi.CodeInvalid, "cannot pass a %s to the host", x.Type())
	case *lua.LTable:
		return c.table(x, depth)
	}
	return nil, pluginapi.Errf(pluginapi.CodeInvalid, "cannot pass a %s to the host", v.Type())
}

func (c *converter) table(t *lua.LTable, depth int) (any, error) {
	if depth >= maxHostDepth {
		return nil, pluginapi.Errf(pluginapi.CodeLimit, "table is nested deeper than %d levels", maxHostDepth)
	}
	if c.seen[t] {
		return nil, pluginapi.Errf(pluginapi.CodeInvalid, "table contains a cycle")
	}
	c.seen[t] = true
	defer delete(c.seen, t)

	var strKeys []string
	var intKeys []int
	other := false
	t.ForEach(func(k, _ lua.LValue) {
		switch kk := k.(type) {
		case lua.LString:
			strKeys = append(strKeys, string(kk))
		case lua.LNumber:
			f := float64(kk)
			if f == math.Trunc(f) && f >= 1 && f < maxExactInt {
				intKeys = append(intKeys, int(f))
				return
			}
			other = true
		default:
			other = true
		}
	})
	marked := c.p.data.arrayMT != nil && t.Metatable == lua.LValue(c.p.data.arrayMT)
	switch {
	case other || (len(strKeys) > 0 && len(intKeys) > 0):
		return nil, pluginapi.Errf(pluginapi.CodeInvalid, "table is neither an array nor an object")
	case len(intKeys) > 0 || (marked && len(strKeys) == 0):
		sort.Ints(intKeys)
		for i, k := range intKeys {
			if k != i+1 {
				return nil, pluginapi.Errf(pluginapi.CodeInvalid, "array has holes (key %d missing)", i+1)
			}
		}
		out := make([]any, len(intKeys))
		for i := range out {
			e, err := c.from(t.RawGetInt(i+1), depth+1)
			if err != nil {
				return nil, err
			}
			out[i] = e
		}
		return out, nil
	}
	out := make(map[string]any, len(strKeys))
	for _, k := range strKeys {
		e, err := c.from(t.RawGetString(k), depth+1)
		if err != nil {
			return nil, err
		}
		out[k] = e
	}
	return out, nil
}

// posFromLua reads {line=, col=} (missing col is 0). Negative values are kept: the host
// clamps positions.
func posFromLua(v lua.LValue) (pluginapi.Pos, error) {
	t, ok := v.(*lua.LTable)
	if !ok {
		return pluginapi.Pos{}, pluginapi.Errf(pluginapi.CodeInvalid, "expected a position table {line=, col=}, got %s", v.Type())
	}
	line, ok := t.RawGetString("line").(lua.LNumber)
	if !ok {
		return pluginapi.Pos{}, pluginapi.Errf(pluginapi.CodeInvalid, "position needs a numeric 'line' field")
	}
	var col lua.LNumber
	switch c := t.RawGetString("col").(type) {
	case lua.LNumber:
		col = c
	case *lua.LNilType:
	default:
		return pluginapi.Pos{}, pluginapi.Errf(pluginapi.CodeInvalid, "position 'col' must be a number")
	}
	return pluginapi.Pos{Line: clampInt(float64(line)), Col: clampInt(float64(col))}, nil
}

// rangeFromLua reads {from=Pos, to=Pos}.
func rangeFromLua(v lua.LValue) (pluginapi.Range, error) {
	t, ok := v.(*lua.LTable)
	if !ok {
		return pluginapi.Range{}, pluginapi.Errf(pluginapi.CodeInvalid, "expected a range table {from=, to=}, got %s", v.Type())
	}
	from, err := posFromLua(t.RawGetString("from"))
	if err != nil {
		return pluginapi.Range{}, err
	}
	to, err := posFromLua(t.RawGetString("to"))
	if err != nil {
		return pluginapi.Range{}, err
	}
	return pluginapi.Range{From: from, To: to}, nil
}

// clampInt converts a float64 to an int without overflow (NaN becomes 0).
func clampInt(f float64) int {
	switch {
	case math.IsNaN(f):
		return 0
	case f >= math.MaxInt32:
		return math.MaxInt32
	case f <= math.MinInt32:
		return math.MinInt32
	}
	return int(f)
}

// errorText renders an error value for logs and notices without ever calling a
// metamethod: a string or number as is, a table by its string `message`
// field, anything else as a type placeholder.
func errorText(v lua.LValue) string {
	switch x := v.(type) {
	case lua.LString:
		return string(x)
	case lua.LNumber:
		return x.String()
	case *lua.LTable:
		if m, ok := x.RawGetString("message").(lua.LString); ok {
			return string(m)
		}
		return "error object (table)"
	case *lua.LUserData:
		return "error object (userdata)"
	case *lua.LNilType:
		return "nil"
	}
	return "error object (" + v.Type().String() + ")"
}

// errTuple turns a host error into the `nil, "<code>: <msg>"` pair of the API.
func errTuple(err error) (lua.LValue, lua.LValue) {
	return lua.LNil, lua.LString(errString(err))
}

// errString is the "<code>: <message>" text of an error; a foreign error has code io.
func errString(err error) string {
	if e, ok := errors.AsType[*pluginapi.Error](err); ok {
		return e.Error()
	}
	return string(pluginapi.CodeIO) + ": " + err.Error()
}

// wholeNumber reads a Go integer of any width, a BufferID, WinID or a float64 with no
// fraction. Callers that take counts and ids from loosely typed maps use it so the Go
// type a producer happened to pick does not matter.
func wholeNumber(v any) (int64, bool) {
	switch x := v.(type) {
	case int:
		return int64(x), true
	case int8:
		return int64(x), true
	case int16:
		return int64(x), true
	case int32:
		return int64(x), true
	case int64:
		return x, true
	case uint:
		return int64(x), x <= math.MaxInt64
	case uint8:
		return int64(x), true
	case uint16:
		return int64(x), true
	case uint32:
		return int64(x), true
	case uint64:
		return int64(x), x <= math.MaxInt64
	case pluginapi.BufferID:
		return int64(x), true
	case pluginapi.WinID:
		return int64(x), true
	case float32:
		return wholeNumber(float64(x))
	case float64:
		if x != math.Trunc(x) || math.Abs(x) >= 1<<53 {
			return 0, false
		}
		return int64(x), true
	}
	return 0, false
}
