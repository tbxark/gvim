package plugin

import (
	"context"

	lua "github.com/yuin/gopher-lua"

	"github.com/tbxark/gvim/pluginapi"
)

// gvim.diff.compare_buffers(left, right) ⏳
// gvim.diff.replace_range(buf, start, end, text, expected_version?) -> boolean | nil, err
//
// The compare/diff surface of parity backlog #15, Fresh's compareBuffers and
// replaceRange. compare_buffers reads both buffers synchronously (the host calls do
// not yield, so the two versions are read from one editor state), then parks a Work
// that runs the line diff and builds the byte geometry; the Lua table uses
// snake_case, and every offset is an absolute byte offset of the buffer in gvim's
// byte model, usable with replace_range as it stands. replace_range applies one
// edit and one undo step through the host's version-guarded atomic path and reports
// false (not an error) for the refusals Fresh documents.
//
// One shape difference from the TypeScript API: an unset span is `false`, not
// `null`, because a Lua array with a nil hole has no length a `# loop can use.
func installCompareDiff(e *apiEnv, t *lua.LTable) {
	t.RawSetString("compare_buffers", e.async("diff_compare_buffers"))
	e.set(t, "replace_range", func(L *lua.LState) int {
		id, ok := bufferArg(L.Get(1))
		if !ok {
			return invalidErrReturn(L, "replace_range: a Buffer handle expected")
		}
		start := int(L.CheckInt64(2))
		end := int(L.CheckInt64(3))
		text := L.CheckString(4)
		var expect *pluginapi.Version
		if L.Get(5) != lua.LNil {
			s, ok := L.Get(5).(lua.LString)
			if !ok {
				return invalidErrReturn(L, "replace_range: the expected version is a version token")
			}
			v, ok := pluginapi.ParseVersion(string(s))
			if !ok {
				return invalidErrReturn(L, "replace_range: the expected version is a version token")
			}
			expect = &v
		}
		applied, err := e.p.r.guard.ReplaceRange(id, start, end, text, expect)
		if err != nil {
			return bufRet(L, err)
		}
		L.Push(lua.LBool(applied))
		return 1
	})
}

// invalidErrReturn pushes nil and a CodeInvalid error.
func invalidErrReturn(L *lua.LState, msg string) int {
	v, s := errTuple(pluginapi.Errf(pluginapi.CodeInvalid, "%s", msg))
	L.Push(v)
	L.Push(s)
	return 2
}

func init() {
	registerAwait("diff_compare_buffers", awaitDiffCompareBuffers)
}

// awaitDiffCompareBuffers is gvim.diff.compare_buffers: reads both sources, then
// computes the diff and the geometry in a Work.
func awaitDiffCompareBuffers(c *awaitCall) {
	var left, right pluginapi.BufferID
	okLeft, okRight := false, false
	if len(c.args) >= 2 {
		left, okLeft = bufferArg(c.args[0])
		right, okRight = bufferArg(c.args[1])
	}
	if !okLeft || !okRight {
		c.Bad(1, "compare_buffers", "two Buffer handles expected")
		return
	}
	if _, none := c.r.svc.(noServices); none {
		c.Fail(pluginapi.Errf(pluginapi.CodeUnsupported, "compare_buffers: no diff service"))
		return
	}
	l, err := c.r.guard.DiffSource(left)
	if err != nil {
		c.Fail(err)
		return
	}
	r, err := c.r.guard.DiffSource(right)
	if err != nil {
		c.Fail(err)
		return
	}
	if err := c.CheckPark(true); err != nil {
		c.Fail(err)
		return
	}
	p, svc := c.p, c.r.svc
	c.Park(&parkSpec{
		Work: &pluginapi.Work{Run: func(context.Context) pluginapi.Completion {
			res := buildCompareResult(l, r, svc.Diff(l.Lines, r.Lines))
			return pluginapi.Completion{Values: []any{res}}
		}},
		Convert: func(comp pluginapi.Completion) []lua.LValue {
			// The leading true is the resume convention's ok flag (see
			// defaultConvert): the await call returns the values after it.
			if comp.Err != "" || len(comp.Values) == 0 {
				return []lua.LValue{lua.LTrue, lua.LNil, lua.LString(comp.Err)}
			}
			res, _ := comp.Values[0].(pluginapi.CompareResult)
			return []lua.LValue{lua.LTrue, compareResultTable(p.L, res)}
		},
	})
}

// compareResultTable renders a CompareResult as the Lua table compare_buffers
// returns.
func compareResultTable(L *lua.LState, res pluginapi.CompareResult) *lua.LTable {
	t := L.CreateTable(7, 0)
	hunks := L.CreateTable(len(res.Hunks), 0)
	for _, h := range res.Hunks {
		hunks.Append(compareHunkTable(L, h))
	}
	t.RawSetString("hunks", hunks)
	t.RawSetString("left_lines", lua.LNumber(res.LeftLines))
	t.RawSetString("right_lines", lua.LNumber(res.RightLines))
	t.RawSetString("left_len", lua.LNumber(res.LeftLen))
	t.RawSetString("right_len", lua.LNumber(res.RightLen))
	t.RawSetString("left_version", lua.LString(res.LeftVersion.String()))
	t.RawSetString("right_version", lua.LString(res.RightVersion.String()))
	return t
}

// compareHunkTable renders one hunk's two sides.
func compareHunkTable(L *lua.LState, h pluginapi.CompareHunk) *lua.LTable {
	t := L.CreateTable(0, 3)
	t.RawSetString("left", compareSideTable(L, h.Left))
	t.RawSetString("right", compareSideTable(L, h.Right))
	t.RawSetString("eol_only", lua.LBool(h.EOLOnly))
	return t
}

// compareSideTable renders one side of a hunk: lines as {start, end} byte ranges and
// spans as the differing range or false.
func compareSideTable(L *lua.LState, s pluginapi.CompareSide) *lua.LTable {
	t := L.CreateTable(0, 7)
	t.RawSetString("start", lua.LNumber(s.Start))
	t.RawSetString("count", lua.LNumber(s.Count))
	t.RawSetString("from", lua.LNumber(s.From))
	t.RawSetString("to", lua.LNumber(s.To))
	t.RawSetString("jump", lua.LNumber(s.Jump))
	lines := L.CreateTable(len(s.Lines), 0)
	for _, r := range s.Lines {
		pair := L.CreateTable(0, 2)
		pair.Append(lua.LNumber(r[0]))
		pair.Append(lua.LNumber(r[1]))
		lines.Append(pair)
	}
	t.RawSetString("lines", lines)
	spans := L.CreateTable(len(s.Spans), 0)
	for _, span := range s.Spans {
		if !span.Set {
			spans.Append(lua.LFalse)
			continue
		}
		pair := L.CreateTable(0, 2)
		pair.Append(lua.LNumber(span.Start))
		pair.Append(lua.LNumber(span.End))
		spans.Append(pair)
	}
	t.RawSetString("spans", spans)
	return t
}
