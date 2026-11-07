package plugin

import (
	"context"

	lua "github.com/yuin/gopher-lua"

	"github.com/tbxark/gvim/internal/pluginapi"
)

// gvim.diff: line diffs computed by Services in a Work.
//
//	gvim.diff.lines(a, b) ⏳        arrays of lines, at most maxDiffLines per side
//	gvim.diff.text(a, b, opts?) ⏳  strings; opts.ignore_eol (default true)
//	gvim.diff.buffer(buf, text, opts?) ⏳
//	                                the buffer text is snapshotted synchronously, `text` is
//	                                the old side and the buffer the new one; both sides are
//	                                EOL-normalised; the result carries `version`, the token
//	                                of the snapshot
//	gvim.diff.saved(buf) ⏳          saved text (old) against the current text (new)
//	gvim.diff.next_hunk(hunks, line), gvim.diff.prev_hunk(hunks, line)    pure
const (
	maxDiffLines     = 100_000   // lines per side for lines and saved
	maxDiffTextLines = 1_000_000 // lines per side for text and buffer
	maxDiffTextBytes = 16 << 20  // bytes per side for text and buffer
)

func init() {
	registerAwait("diff_lines", awaitDiffLines)
	registerAwait("diff_text", awaitDiffText)
	registerAwait("diff_buffer", awaitDiffBuffer)
	registerAwait("diff_saved", awaitDiffSaved)
}

func installDiff(e *apiEnv) {
	t := e.area("diff")
	t.RawSetString("lines", e.async("diff_lines"))
	t.RawSetString("text", e.async("diff_text"))
	t.RawSetString("buffer", e.async("diff_buffer"))
	t.RawSetString("saved", e.async("diff_saved"))
	e.set(t, "next_hunk", func(L *lua.LState) int { return pickHunk(L, true) })
	e.set(t, "prev_hunk", func(L *lua.LState) int { return pickHunk(L, false) })
}

// pickHunk returns the hunk with the smallest new_start after line (next) or the largest
// before it (prev), or nil. Hunks are the tables diff.* returned.
func pickHunk(L *lua.LState, next bool) int {
	hunks := L.CheckTable(1)
	line := float64(L.CheckNumber(2))
	best := lua.LValue(lua.LNil)
	bestStart := 0.0
	for i := 1; i <= hunks.Len(); i++ {
		h, ok := hunks.RawGetInt(i).(*lua.LTable)
		if !ok {
			continue
		}
		start, ok := h.RawGetString("new_start").(lua.LNumber)
		if !ok {
			continue
		}
		s := float64(start)
		switch {
		case next && s > line && (best == lua.LNil || s < bestStart),
			!next && s < line && (best == lua.LNil || s > bestStart):
			best, bestStart = h, s
		}
	}
	L.Push(best)
	return 1
}

// stringLines reads a Lua array of strings (numbers count as their text).
func stringLines(t *lua.LTable, max int) ([]string, error) {
	n := t.Len()
	if n > max {
		return nil, pluginapi.Errf(pluginapi.CodeTooLarge, "diff.lines: more than %d lines per side", max)
	}
	out := make([]string, n)
	for i := range out {
		switch v := t.RawGetInt(i + 1).(type) {
		case lua.LString:
			out[i] = string(v)
		case lua.LNumber:
			out[i] = v.String()
		default:
			return nil, invalidErr("diff.lines: line %d is not a string", i+1)
		}
	}
	return out, nil
}

// parkDiff parks the task on a Work that computes hunks; version, when set, is attached to
// the result table as `version`.
func parkDiff(c *awaitCall, run func() []pluginapi.Hunk, version *pluginapi.Version) {
	if err := c.CheckPark(true); err != nil {
		c.Fail(err)
		return
	}
	p := c.p
	c.Park(&parkSpec{
		Work: &pluginapi.Work{Run: func(context.Context) pluginapi.Completion {
			return pluginapi.Completion{Values: []any{run()}}
		}},
		Convert: func(comp pluginapi.Completion) []lua.LValue {
			if comp.Err != "" || len(comp.Values) == 0 {
				return []lua.LValue{lua.LTrue, lua.LNil, lua.LString(comp.Err)}
			}
			hunks, _ := comp.Values[0].([]pluginapi.Hunk)
			out := p.L.CreateTable(len(hunks), 1)
			for _, h := range hunks {
				out.Append(p.toLua(h))
			}
			if version != nil {
				out.RawSetString("version", lua.LString(version.String()))
			}
			return []lua.LValue{lua.LTrue, out}
		},
	})
}

func awaitDiffLines(c *awaitCall) {
	a, b, ok := twoTables(c, "lines")
	if !ok {
		return
	}
	x, err := stringLines(a, maxDiffLines)
	if err != nil {
		c.Fail(err)
		return
	}
	y, err := stringLines(b, maxDiffLines)
	if err != nil {
		c.Fail(err)
		return
	}
	svc := c.r.svc
	parkDiff(c, func() []pluginapi.Hunk { return svc.Diff(x, y) }, nil)
}

func twoTables(c *awaitCall, name string) (a, b *lua.LTable, ok bool) {
	if len(c.args) >= 2 {
		a, _ = c.args[0].(*lua.LTable)
		b, _ = c.args[1].(*lua.LTable)
	}
	if a == nil || b == nil {
		c.Bad(1, name, "two arrays of lines expected")
		return nil, nil, false
	}
	return a, b, true
}

// ignoreEOL reads opts.ignore_eol (default true) from argument i.
func ignoreEOL(c *awaitCall, i int) bool {
	if len(c.args) > i {
		if o, ok := c.args[i].(*lua.LTable); ok {
			if v := o.RawGetString("ignore_eol"); v != lua.LNil {
				return lua.LVAsBool(v)
			}
		}
	}
	return true
}

func checkDiffText(c *awaitCall, name, s string) bool {
	if len(s) > maxDiffTextBytes {
		c.Fail(pluginapi.Errf(pluginapi.CodeTooLarge, "diff.%s: text larger than %d bytes", name, maxDiffTextBytes))
		return false
	}
	return true
}

func awaitDiffText(c *awaitCall) {
	if len(c.args) < 2 {
		c.Bad(1, "text", "two strings expected")
		return
	}
	a, ok1 := c.args[0].(lua.LString)
	b, ok2 := c.args[1].(lua.LString)
	if !ok1 || !ok2 {
		c.Bad(1, "text", "two strings expected")
		return
	}
	if !checkDiffText(c, "text", string(a)) || !checkDiffText(c, "text", string(b)) {
		return
	}
	svc, o := c.r.svc, pluginapi.DiffOptions{IgnoreEOL: ignoreEOL(c, 2)}
	parkDiff(c, func() []pluginapi.Hunk { return svc.DiffText(string(a), string(b), o) }, nil)
}

func awaitDiffBuffer(c *awaitCall) {
	if len(c.args) < 2 {
		c.Bad(1, "buffer", "Buffer and text expected")
		return
	}
	id, ok := bufferArg(c.args[0])
	text, ok2 := c.args[1].(lua.LString)
	if !ok || !ok2 {
		c.Bad(1, "buffer", "Buffer and text expected")
		return
	}
	if !checkDiffText(c, "buffer", string(text)) {
		return
	}
	snapshot, v, err := c.r.guard.FullText(id)
	if err != nil {
		c.Fail(err)
		return
	}
	if len(snapshot) > maxDiffTextBytes {
		c.Fail(pluginapi.Errf(pluginapi.CodeTooLarge, "diff.buffer: buffer larger than %d bytes", maxDiffTextBytes))
		return
	}
	svc, old := c.r.svc, string(text)
	parkDiff(c, func() []pluginapi.Hunk {
		return svc.DiffText(old, snapshot, pluginapi.DiffOptions{IgnoreEOL: true})
	}, &v)
}

func awaitDiffSaved(c *awaitCall) {
	var id pluginapi.BufferID
	ok := false
	if len(c.args) > 0 {
		id, ok = bufferArg(c.args[0])
	}
	if !ok {
		c.Bad(1, "saved", "Buffer expected")
		return
	}
	saved, err := c.r.guard.SavedLines(id)
	if err != nil {
		c.Fail(err)
		return
	}
	cur, err := c.r.guard.Lines(id, 0, -1)
	if err != nil {
		c.Fail(err)
		return
	}
	if len(saved) > maxDiffLines || len(cur) > maxDiffLines {
		c.Fail(pluginapi.Errf(pluginapi.CodeTooLarge, "diff.saved: more than %d lines", maxDiffLines))
		return
	}
	svc := c.r.svc
	parkDiff(c, func() []pluginapi.Hunk { return svc.Diff(saved, cur) }, nil)
}

// bufferArg reads a Buffer handle from an await argument.
func bufferArg(v lua.LValue) (pluginapi.BufferID, bool) {
	if ud, ok := v.(*lua.LUserData); ok {
		if h, ok := ud.Value.(idHandle); ok && h.kind == kindBuffer {
			return pluginapi.BufferID(h.id), true
		}
	}
	return 0, false
}
