package plugin

import (
	"context"
	"errors"
	"math"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	lua "github.com/yuin/gopher-lua"

	"github.com/tbxark/gvim/internal/pluginapi"
)

// gvim.buf and the Buffer handle (FINAL 5.2, amended by C4, C7, C18, C22 and C24).
//
// Everything here is synchronous except open, save, revert and diff_saved, which are
// awaitable. Positions are 0-based line and 0-based byte column; Lua arrays are 1-based.
// The host speaks the same coordinates, so this file only validates arguments, enforces
// the caps, and turns host values into Lua values.

// Limits of the buffer API.
const (
	bufMaxEdits       = 50_000        // edits in one b:edit call (C18)
	bufMaxEditBytes   = 8 << 20       // inserted bytes in one edit call
	bufMaxReadLines   = 200_000       // lines in one b:lines call
	bufMaxReadBytes   = 16 << 20      // bytes in one b:lines / b:text call
	bufDefaultFind    = 1000          // b:find max matches
	bufMaxFind        = 100_000       // hard cap of b:find matches
	bufDefaultLines   = 10_000        // b:find_lines max lines
	bufMaxPattern     = 4096          // bytes of a search pattern
	bufFindLinesMs    = 20            // default b:find_lines time box
	bufFindLinesMaxMs = 100           // longest b:find_lines time box
	bufEndOfText      = math.MaxInt32 // a line or column past anything: clamps to the end
)

func init() {
	registerAwait("buf_open", awaitBufOpen)
	registerAwait("buf_save", awaitBufSave)
	registerAwait("buf_revert", awaitBufRevert)
	registerAwait("buf_diff_saved", awaitBufDiffSaved)
}

// bufRet pushes the `nil, "<code>: <msg>"` pair of an expected failure.
func bufRet(L *lua.LState, err error) int {
	v, s := errTuple(err)
	L.Push(v)
	L.Push(s)
	return 2
}

// bufTrue pushes true, or the failure pair when err is not nil.
func bufTrue(L *lua.LState, err error) int {
	if err != nil {
		return bufRet(L, err)
	}
	L.Push(lua.LTrue)
	return 1
}

func bufClosed(id pluginapi.BufferID) error {
	return pluginapi.Errf(pluginapi.CodeClosed, "buffer %d is closed", id)
}

// bufInt reads an optional integer argument.
func bufInt(L *lua.LState, n, def int) int {
	switch v := L.Get(n).(type) {
	case *lua.LNilType:
		return def
	case lua.LNumber:
		return clampInt(float64(v))
	}
	L.ArgError(n, "number expected, got "+L.Get(n).Type().String())
	return def
}

// bufOptTable returns the optional table argument n (nil when absent).
func bufOptTable(L *lua.LState, n int) *lua.LTable {
	switch v := L.Get(n).(type) {
	case *lua.LNilType:
		return nil
	case *lua.LTable:
		return v
	}
	L.ArgError(n, "table expected, got "+L.Get(n).Type().String())
	return nil
}

// bufTableOpts reads typed fields of an options table; a wrong type is a message for the
// caller to raise as a bad argument. A nil table has no fields.
type bufTableOpts struct {
	t   *lua.LTable
	err string
}

func (o *bufTableOpts) get(key string) lua.LValue {
	if o.t == nil {
		return lua.LNil
	}
	return o.t.RawGetString(key)
}

func (o *bufTableOpts) boolean(key string, def bool) bool {
	switch v := o.get(key).(type) {
	case *lua.LNilType:
		return def
	case lua.LBool:
		return bool(v)
	}
	o.fail("option '" + key + "' must be a boolean")
	return def
}

func (o *bufTableOpts) integer(key string, def int) int {
	switch v := o.get(key).(type) {
	case *lua.LNilType:
		return def
	case lua.LNumber:
		return clampInt(float64(v))
	}
	o.fail("option '" + key + "' must be a number")
	return def
}

func (o *bufTableOpts) str(key string) (string, bool) {
	switch v := o.get(key).(type) {
	case *lua.LNilType:
		return "", false
	case lua.LString:
		return string(v), true
	}
	o.fail("option '" + key + "' must be a string")
	return "", false
}

func (o *bufTableOpts) fail(msg string) {
	if o.err == "" {
		o.err = msg
	}
}

// bufParseVersion reads the `version=` option: absent or nil means no check.
func bufParseVersion(v lua.LValue) (*pluginapi.Version, error) {
	if v == lua.LNil {
		return nil, nil
	}
	if s, ok := v.(lua.LString); ok {
		if ver, ok := pluginapi.ParseVersion(string(s)); ok {
			return &ver, nil
		}
	}
	return nil, pluginapi.Errf(pluginapi.CodeInvalid, "bad version token")
}

// bufCheckRead enforces the caps of one read of whole lines.
func bufCheckRead(lines []string) error {
	if len(lines) > bufMaxReadLines {
		return pluginapi.Errf(pluginapi.CodeTooLarge, "more than %d lines requested", bufMaxReadLines)
	}
	total := 0
	for _, s := range lines {
		total += len(s) + 1
	}
	if total > bufMaxReadBytes {
		return pluginapi.Errf(pluginapi.CodeTooLarge, "more than %d bytes requested", bufMaxReadBytes)
	}
	return nil
}

// bufEditBytes checks the inserted size of one edit call.
func bufEditBytes(n int) error {
	if n > bufMaxEditBytes {
		return pluginapi.Errf(pluginapi.CodeLimit, "edit too large (max %d bytes)", bufMaxEditBytes)
	}
	return nil
}

// bufWholeRange is the range of the whole text; the host clamps it to the document.
var bufWholeRange = pluginapi.Range{To: pluginapi.Pos{Line: bufEndOfText}}

// bufEndPos is the position after the last character of the document.
var bufEndPos = pluginapi.Pos{Line: bufEndOfText}

// bufSetLinesEdit turns b:set_lines(first, last, lines) on a document of n lines into one
// edit (C24). The line array L[0..n) becomes L[0..first) ++ lines ++ L[last..n); the edit
// is the text between the lines first-1 and last, so that no newline is invented or lost.
// noop reports an edit that changes nothing.
func bufSetLinesEdit(n, first, last int, lines []string) (ed pluginapi.Edit, noop bool, err error) {
	first = min(max(first, 0), n)
	last = min(max(last, 0), n)
	if first > last {
		return ed, false, pluginapi.Errf(pluginapi.CodeInvalid, "set_lines: first (%d) is after last (%d)", first, last)
	}
	empty := len(lines) == 0
	if empty && first == last {
		return ed, true, nil
	}
	body := strings.Join(lines, "\n")
	switch {
	case last < n: // whole lines follow the replaced span: each new line ends with "\n"
		if !empty {
			body += "\n"
		}
		return pluginapi.Edit{Range: pluginapi.Range{From: pluginapi.Pos{Line: first}, To: pluginapi.Pos{Line: last}}, Text: body}, false, nil
	case first == 0: // the span reaches the end: the whole document is replaced
		return pluginapi.Edit{Range: pluginapi.Range{From: pluginapi.Pos{}, To: pluginapi.Pos{Line: n}}, Text: body}, false, nil
	}
	// last == n and first > 0: replace the tail, starting at the end of the line before it
	if !empty {
		body = "\n" + body
	}
	from := pluginapi.Pos{Line: first - 1, Col: bufEndOfText}
	return pluginapi.Edit{Range: pluginapi.Range{From: from, To: pluginapi.Pos{Line: n}}, Text: body}, false, nil
}

// installBuf installs buffers: gvim.buf and the Buffer handle methods (stage S6).
func installBuf(e *apiEnv) {
	p := e.p
	area := e.area("buf")
	methods := e.kind(kindBuffer).methods
	host := func() pluginapi.Host { return p.r.guard }

	// ---------- gvim.buf ----------

	e.set(area, "current", func(L *lua.LState) int {
		L.Push(p.toLua(host().ActiveBuffer()))
		return 1
	})
	e.set(area, "get", func(L *lua.LState) int {
		id := pluginapi.BufferID(max(L.CheckInt64(1), 0))
		if _, ok := host().Buffer(id); !ok {
			L.Push(lua.LNil)
			return 1
		}
		L.Push(p.toLua(id))
		return 1
	})
	e.set(area, "list", func(L *lua.LState) int {
		infos := host().ListBuffers()
		t := L.CreateTable(len(infos), 0)
		for _, bi := range infos {
			t.Append(p.toLua(bi.ID))
		}
		L.Push(t)
		return 1
	})
	e.set(area, "find", func(L *lua.LState) int {
		L.Push(p.toLua(host().FindBuffer(L.CheckString(1))))
		return 1
	})
	e.set(area, "new", func(L *lua.LState) int {
		h := host()
		if err := h.Do("new", nil, 1); err != nil {
			return bufRet(L, err)
		}
		id := h.ActiveBuffer()
		if id == 0 {
			return bufRet(L, pluginapi.Errf(pluginapi.CodeClosed, "no buffer was created"))
		}
		L.Push(p.toLua(id))
		return 1
	})
	area.RawSetString("open", e.async("buf_open"))

	// ---------- the Buffer handle ----------

	// meth defines a method; f receives the id of the handle argument.
	meth := func(name string, f func(L *lua.LState, id pluginapi.BufferID) int) {
		e.set(methods, name, func(L *lua.LState) int {
			return f(L, pluginapi.BufferID(checkID(L, 1, kindBuffer)))
		})
	}
	// info-derived accessors
	getter := func(name string, f func(bi pluginapi.BufferInfo) lua.LValue) {
		meth(name, func(L *lua.LState, id pluginapi.BufferID) int {
			bi, ok := host().Buffer(id)
			if !ok {
				return bufRet(L, bufClosed(id))
			}
			L.Push(f(bi))
			return 1
		})
	}
	optStr := func(s string) lua.LValue {
		if s == "" {
			return lua.LNil
		}
		return lua.LString(s)
	}
	getter("path", func(bi pluginapi.BufferInfo) lua.LValue { return optStr(bi.Path) })
	getter("name", func(bi pluginapi.BufferInfo) lua.LValue { return lua.LString(bi.Name) })
	getter("language", func(bi pluginapi.BufferInfo) lua.LValue { return optStr(bi.Language) })
	getter("is_modified", func(bi pluginapi.BufferInfo) lua.LValue { return lua.LBool(bi.Modified) })
	getter("is_read_only", func(bi pluginapi.BufferInfo) lua.LValue { return lua.LBool(bi.ReadOnly) })
	getter("is_virtual", func(bi pluginapi.BufferInfo) lua.LValue { return lua.LBool(bi.Virtual) })
	getter("is_large", func(bi pluginapi.BufferInfo) lua.LValue { return lua.LBool(bi.Large) })
	getter("version", func(bi pluginapi.BufferInfo) lua.LValue { return lua.LString(bi.Version.String()) })
	getter("line_count", func(bi pluginapi.BufferInfo) lua.LValue {
		if bi.LineCount < 0 {
			return lua.LNil
		}
		return lua.LNumber(bi.LineCount)
	})
	getter("info", func(bi pluginapi.BufferInfo) lua.LValue {
		t := p.L.CreateTable(0, 15)
		t.RawSetString("id", lua.LNumber(bi.ID))
		for _, kv := range []struct{ k, v string }{
			{"path", bi.Path}, {"language", bi.Language}, {"mode", bi.Mode},
		} {
			if kv.v != "" {
				t.RawSetString(kv.k, lua.LString(kv.v))
			}
		}
		t.RawSetString("name", lua.LString(bi.Name))
		t.RawSetString("encoding", lua.LString(bi.Encoding))
		t.RawSetString("line_ending", lua.LString(bi.LineEnding))
		t.RawSetString("modified", lua.LBool(bi.Modified))
		t.RawSetString("read_only", lua.LBool(bi.ReadOnly))
		t.RawSetString("virtual", lua.LBool(bi.Virtual))
		t.RawSetString("large", lua.LBool(bi.Large))
		t.RawSetString("preview", lua.LBool(bi.Preview))
		t.RawSetString("active", lua.LBool(bi.Active))
		t.RawSetString("version", lua.LString(bi.Version.String()))
		if bi.LineCount >= 0 {
			t.RawSetString("line_count", lua.LNumber(bi.LineCount))
		}
		return t
	})

	// ---------- reads ----------

	meth("line", func(L *lua.LState, id pluginapi.BufferID) int {
		n := float64(L.CheckNumber(2))
		if !(n >= 0 && n < bufEndOfText) { // also catches NaN
			if _, ok := host().Buffer(id); !ok {
				return bufRet(L, bufClosed(id))
			}
			L.Push(lua.LNil)
			return 1
		}
		lines, err := host().Lines(id, int(n), 1)
		switch {
		case err != nil:
			return bufRet(L, err)
		case len(lines) == 0:
			L.Push(lua.LNil)
		default:
			L.Push(lua.LString(lines[0]))
		}
		return 1
	})
	meth("lines", func(L *lua.LState, id pluginapi.BufferID) int {
		h := host()
		first := max(bufInt(L, 2, 0), 0)
		count := bufInt(L, 3, -1)
		bi, ok := h.Buffer(id)
		if !ok {
			return bufRet(L, bufClosed(id))
		}
		// Refuse before reading when the answer is known to be too big; a large file whose
		// line count is still unknown is asked for one line more than the cap, and bufCheckRead
		// refuses only if that many lines come back.
		want := count
		if bi.LineCount >= 0 {
			rest := max(bi.LineCount-first, 0)
			if want < 0 || want > rest {
				want = rest
			}
			if want > bufMaxReadLines {
				return bufRet(L, pluginapi.Errf(pluginapi.CodeTooLarge, "more than %d lines requested", bufMaxReadLines))
			}
		} else if want < 0 || want > bufMaxReadLines {
			want = bufMaxReadLines + 1
		}
		lines, err := h.Lines(id, first, want)
		if err != nil {
			return bufRet(L, err)
		}
		if err := bufCheckRead(lines); err != nil {
			return bufRet(L, err)
		}
		t := L.CreateTable(len(lines), 0)
		for _, s := range lines {
			t.Append(lua.LString(s))
		}
		L.Push(t)
		return 1
	})
	meth("text", func(L *lua.LState, id pluginapi.BufferID) int {
		r := bufWholeRange
		if L.Get(2) != lua.LNil {
			r = checkRange(L, 2)
		}
		s, err := host().Slice(id, r)
		if err != nil {
			return bufRet(L, err)
		}
		if len(s) > bufMaxReadBytes {
			return bufRet(L, pluginapi.Errf(pluginapi.CodeTooLarge, "more than %d bytes requested", bufMaxReadBytes))
		}
		L.Push(lua.LString(s))
		return 1
	})
	meth("byte_length", func(L *lua.LState, id pluginapi.BufferID) int {
		n, err := host().ByteLength(id)
		if err != nil {
			return bufRet(L, err)
		}
		L.Push(lua.LNumber(n))
		return 1
	})
	meth("offset_of", func(L *lua.LState, id pluginapi.BufferID) int {
		n, err := host().OffsetOf(id, checkPos(L, 2))
		if err != nil {
			return bufRet(L, err)
		}
		L.Push(lua.LNumber(n))
		return 1
	})
	meth("pos_of", func(L *lua.LState, id pluginapi.BufferID) int {
		pos, err := host().PosOf(id, clampInt(float64(L.CheckNumber(2))))
		if err != nil {
			return bufRet(L, err)
		}
		L.Push(p.posTable(pos))
		return 1
	})

	// ---------- edits ----------

	// apply runs one edit call and pushes true or the failure pair.
	apply := func(L *lua.LState, id pluginapi.BufferID, edits []pluginapi.Edit, o pluginapi.EditOptions) int {
		n := 0
		for _, ed := range edits {
			n += len(ed.Text)
		}
		if err := bufEditBytes(n); err != nil {
			return bufRet(L, err)
		}
		return bufTrue(L, host().Edit(id, edits, o))
	}
	meth("insert", func(L *lua.LState, id pluginapi.BufferID) int {
		pos := checkPos(L, 2)
		text := L.CheckString(3)
		return apply(L, id, []pluginapi.Edit{{Range: pluginapi.Range{From: pos, To: pos}, Text: text}}, pluginapi.EditOptions{})
	})
	meth("delete", func(L *lua.LState, id pluginapi.BufferID) int {
		return apply(L, id, []pluginapi.Edit{{Range: checkRange(L, 2)}}, pluginapi.EditOptions{})
	})
	meth("replace", func(L *lua.LState, id pluginapi.BufferID) int {
		r := checkRange(L, 2)
		return apply(L, id, []pluginapi.Edit{{Range: r, Text: L.CheckString(3)}}, pluginapi.EditOptions{})
	})
	meth("set_text", func(L *lua.LState, id pluginapi.BufferID) int {
		return apply(L, id, []pluginapi.Edit{{Range: bufWholeRange, Text: L.CheckString(2)}}, pluginapi.EditOptions{})
	})
	meth("append", func(L *lua.LState, id pluginapi.BufferID) int {
		return apply(L, id, []pluginapi.Edit{{Range: pluginapi.Range{From: bufEndPos, To: bufEndPos}, Text: L.CheckString(2)}}, pluginapi.EditOptions{})
	})
	meth("set_lines", func(L *lua.LState, id pluginapi.BufferID) int {
		first, last := bufInt(L, 2, 0), bufInt(L, 3, 0)
		t := L.CheckTable(4)
		lines := make([]string, 0, min(t.Len(), 1024))
		total := 0
		for i := 1; i <= t.Len(); i++ {
			s, ok := t.RawGetInt(i).(lua.LString)
			if !ok {
				L.ArgError(4, "lines["+strconv.Itoa(i)+"] must be a string")
			}
			total += len(s) + 1
			if total > bufMaxEditBytes {
				return bufRet(L, pluginapi.Errf(pluginapi.CodeLimit, "edit too large (max %d bytes)", bufMaxEditBytes))
			}
			lines = append(lines, string(s))
		}
		bi, ok := host().Buffer(id)
		if !ok {
			return bufRet(L, bufClosed(id))
		}
		if bi.LineCount < 0 {
			return bufRet(L, pluginapi.Errf(pluginapi.CodeTooLarge, "set_lines: the line count of this file is not known yet"))
		}
		ed, noop, err := bufSetLinesEdit(bi.LineCount, first, last, lines)
		switch {
		case err != nil:
			return bufRet(L, err)
		case noop:
			L.Push(lua.LTrue)
			return 1
		}
		return apply(L, id, []pluginapi.Edit{ed}, pluginapi.EditOptions{})
	})
	meth("edit", func(L *lua.LState, id pluginapi.BufferID) int {
		t := L.CheckTable(2)
		opts := bufTableOpts{t: bufOptTable(L, 3)}
		cursor, hasCursor := opts.str("cursor")
		verV := opts.get("version")
		if opts.err != "" {
			L.ArgError(3, opts.err)
		}
		var eo pluginapi.EditOptions
		if hasCursor {
			switch cursor {
			case "map":
				eo.Cursor = pluginapi.CursorMap
			case "keep":
				eo.Cursor = pluginapi.CursorKeep
			case "end":
				eo.Cursor = pluginapi.CursorEnd
			default:
				L.ArgError(3, `option 'cursor' must be "map", "keep" or "end"`)
			}
		}
		ver, err := bufParseVersion(verV)
		if err != nil {
			return bufRet(L, err)
		}
		eo.Expect = ver
		n := t.Len()
		if n > bufMaxEdits {
			return bufRet(L, pluginapi.Errf(pluginapi.CodeLimit, "too many edits (max %d)", bufMaxEdits))
		}
		edits := make([]pluginapi.Edit, 0, n)
		for i := 1; i <= n; i++ {
			et, ok := t.RawGetInt(i).(*lua.LTable)
			if !ok {
				L.ArgError(2, "edit #"+strconv.Itoa(i)+" must be a table {range=, text=}")
			}
			r, rerr := rangeFromLua(et.RawGetString("range"))
			if rerr != nil {
				L.ArgError(2, "edit #"+strconv.Itoa(i)+": "+rerr.Error())
			}
			var text string
			switch tv := et.RawGetString("text").(type) {
			case *lua.LNilType:
			case lua.LString:
				text = string(tv)
			default:
				L.ArgError(2, "edit #"+strconv.Itoa(i)+": text must be a string")
			}
			edits = append(edits, pluginapi.Edit{Range: r, Text: text})
		}
		if n == 0 {
			if _, ok := host().Buffer(id); !ok {
				return bufRet(L, bufClosed(id))
			}
			L.Push(lua.LTrue)
			return 1
		}
		return apply(L, id, edits, eo)
	})
	meth("undo_group", func(L *lua.LState, id pluginapi.BufferID) int {
		fn := L.CheckFunction(2)
		var vals []lua.LValue
		var fnErr error
		err := host().UndoGroup(id, func() error {
			v, e := p.callNested(L, fn)
			if e != nil {
				fnErr = e
				return e
			}
			vals = v
			return nil
		})
		if fnErr != nil {
			var ae *lua.ApiError
			if errors.As(fnErr, &ae) && ae.Object != nil {
				L.Error(ae.Object, 0)
			}
			L.RaiseError("%s", fnErr.Error())
		}
		if err != nil {
			return bufRet(L, err)
		}
		// gopher-lua reports a body that returns nothing as a single nil; that is not a result.
		if len(vals) == 1 && vals[0] == lua.LNil {
			return 0
		}
		for _, v := range vals {
			L.Push(v)
		}
		return len(vals)
	})
	meth("undo", func(L *lua.LState, id pluginapi.BufferID) int { return bufTrue(L, host().Undo(id)) })
	meth("redo", func(L *lua.LState, id pluginapi.BufferID) int { return bufTrue(L, host().Redo(id)) })

	// ---------- cursors ----------

	meth("cursor", func(L *lua.LState, id pluginapi.BufferID) int {
		cs, err := host().Cursors(id)
		if err != nil {
			return bufRet(L, err)
		}
		if len(cs) == 0 {
			return bufRet(L, pluginapi.Errf(pluginapi.CodeNotFound, "buffer %d has no cursor", id))
		}
		L.Push(p.posTable(cs[0].Pos))
		return 1
	})
	meth("cursors", func(L *lua.LState, id pluginapi.BufferID) int {
		cs, err := host().Cursors(id)
		if err != nil {
			return bufRet(L, err)
		}
		t := L.CreateTable(len(cs), 0)
		for _, c := range cs {
			ct := L.CreateTable(0, 2)
			ct.RawSetString("pos", p.posTable(c.Pos))
			if c.Anchor != nil {
				ct.RawSetString("anchor", p.posTable(*c.Anchor))
			}
			t.Append(ct)
		}
		L.Push(t)
		return 1
	})
	selection := func(id pluginapi.BufferID) (pluginapi.Range, bool, error) {
		cs, err := host().Cursors(id)
		if err != nil {
			return pluginapi.Range{}, false, err
		}
		if len(cs) == 0 || cs[0].Anchor == nil || *cs[0].Anchor == cs[0].Pos {
			return pluginapi.Range{}, false, nil
		}
		from, to := *cs[0].Anchor, cs[0].Pos
		if cmpPos(from, to) > 0 {
			from, to = to, from
		}
		return pluginapi.Range{From: from, To: to}, true, nil
	}
	meth("selection", func(L *lua.LState, id pluginapi.BufferID) int {
		r, ok, err := selection(id)
		switch {
		case err != nil:
			return bufRet(L, err)
		case !ok:
			L.Push(lua.LNil)
		default:
			L.Push(p.rangeTable(r))
		}
		return 1
	})
	meth("selected_text", func(L *lua.LState, id pluginapi.BufferID) int {
		r, ok, err := selection(id)
		if err != nil {
			return bufRet(L, err)
		}
		if !ok {
			L.Push(lua.LNil)
			return 1
		}
		s, err := host().Slice(id, r)
		if err != nil {
			return bufRet(L, err)
		}
		L.Push(lua.LString(s))
		return 1
	})
	meth("set_cursor", func(L *lua.LState, id pluginapi.BufferID) int {
		pos := checkPos(L, 2)
		opts := bufTableOpts{t: bufOptTable(L, 3)}
		extend, reveal, center := opts.boolean("extend", false), opts.boolean("reveal", true), opts.boolean("center", false)
		if opts.err != "" {
			L.ArgError(3, opts.err)
		}
		return bufTrue(L, host().SetCursor(id, pos, extend, reveal, center))
	})
	meth("select", func(L *lua.LState, id pluginapi.BufferID) int {
		return bufTrue(L, host().SetSelection(id, checkRange(L, 2)))
	})

	// ---------- search ----------

	pattern := func(L *lua.LState) (string, error) {
		s := L.CheckString(2)
		switch {
		case s == "":
			return "", pluginapi.Errf(pluginapi.CodeInvalid, "empty pattern")
		case len(s) > bufMaxPattern:
			return "", pluginapi.Errf(pluginapi.CodeLimit, "pattern is longer than %d bytes", bufMaxPattern)
		}
		return s, nil
	}
	meth("find", func(L *lua.LState, id pluginapi.BufferID) int {
		pat, err := pattern(L)
		opts := bufTableOpts{t: bufOptTable(L, 3)}
		q := pluginapi.SearchQuery{Text: pat}
		q.Regex = opts.boolean("regex", false)
		q.CaseSensitive = opts.boolean("case_sensitive", false)
		q.WholeWord = opts.boolean("whole_word", false)
		q.Max = opts.integer("max", bufDefaultFind)
		within := opts.get("within")
		if opts.err != "" {
			L.ArgError(3, opts.err)
		}
		if err != nil {
			return bufRet(L, err)
		}
		if q.Max <= 0 {
			q.Max = bufDefaultFind
		}
		q.Max = min(q.Max, bufMaxFind)
		if within != lua.LNil {
			r, rerr := rangeFromLua(within)
			if rerr != nil {
				L.ArgError(3, "option 'within': "+rerr.Error())
			}
			q.Within = &r
		}
		res, err := host().Search(id, q)
		if err != nil {
			return bufRet(L, err)
		}
		t := L.CreateTable(len(res.Ranges), 0)
		for _, r := range res.Ranges {
			t.Append(p.rangeTable(r))
		}
		L.Push(t)
		L.Push(lua.LBool(res.Truncated))
		return 2
	})
	meth("find_lines", func(L *lua.LState, id pluginapi.BufferID) int {
		pat, err := pattern(L)
		opts := bufTableOpts{t: bufOptTable(L, 3)}
		q := pluginapi.FindLinesQuery{Pattern: pat}
		q.Regex = opts.boolean("regex", false)
		q.CaseSensitive = opts.boolean("case_sensitive", false)
		q.From = max(opts.integer("from", 0), 0)
		to := opts.get("to")
		q.Max = opts.integer("max", bufDefaultLines)
		ms := opts.integer("budget_ms", bufFindLinesMs)
		if _, ok := to.(lua.LNumber); !ok && to != lua.LNil {
			opts.fail("option 'to' must be a number")
		}
		if opts.err != "" {
			L.ArgError(3, opts.err)
		}
		if err != nil {
			return bufRet(L, err)
		}
		if q.Max <= 0 {
			q.Max = bufDefaultLines
		}
		q.Max = min(q.Max, bufMaxReadLines)
		if ms <= 0 {
			ms = bufFindLinesMs
		}
		q.Budget = time.Duration(min(ms, bufFindLinesMaxMs)) * time.Millisecond
		if n, ok := to.(lua.LNumber); ok {
			q.To = clampInt(float64(n))
			if q.To <= q.From { // an empty range, not "to the end"
				if _, ok := host().Buffer(id); !ok {
					return bufRet(L, bufClosed(id))
				}
				L.Push(L.CreateTable(0, 0))
				L.Push(lua.LNil)
				return 2
			}
		}
		res, err := host().FindLines(id, q)
		if err != nil {
			return bufRet(L, err)
		}
		t := L.CreateTable(len(res.Lines), 0)
		for _, n := range res.Lines {
			t.Append(lua.LNumber(n))
		}
		L.Push(t)
		if res.Next < 0 {
			L.Push(lua.LNil)
		} else {
			L.Push(lua.LNumber(res.Next))
		}
		return 2
	})

	// ---------- tabs and state ----------

	meth("close", func(L *lua.LState, id pluginapi.BufferID) int {
		opts := bufTableOpts{t: bufOptTable(L, 2)}
		force := opts.boolean("force", false)
		if opts.err != "" {
			L.ArgError(2, opts.err)
		}
		return bufTrue(L, host().Close(id, force))
	})
	meth("show", func(L *lua.LState, id pluginapi.BufferID) int { return bufTrue(L, host().Activate(id)) })
	meth("move_tab", func(L *lua.LState, id pluginapi.BufferID) int {
		return bufTrue(L, host().MoveTab(id, clampInt(float64(L.CheckNumber(2)))))
	})
	meth("set_language", func(L *lua.LState, id pluginapi.BufferID) int {
		return bufTrue(L, host().SetLanguage(id, L.CheckString(2)))
	})
	methods.RawSetString("save", e.async("buf_save"))
	methods.RawSetString("revert", e.async("buf_revert"))
	methods.RawSetString("diff_saved", e.async("buf_diff_saved"))

	installBufVirtual(e)
}

// Limits of the virtual-buffer API.
const (
	bufMaxContentEntries  = 200_000 // entries in one b:set_content call
	bufMaxContentSegments = 100_000 // styled segments in one b:set_content call
	bufMaxModeName        = 128     // bytes of a buffer mode name
)

// contentFromLua reads the entries of b:set_content: an array of tables {text=, style=,
// segments={{text=, style=}, ...}, props=table}. A wrong shape raises a bad-argument error;
// a content over a limit is the returned error. The properties of all entries share one
// node budget, so many small tables cannot add up to more than one big one.
func (p *plugin) contentFromLua(L *lua.LState, t *lua.LTable) ([]pluginapi.ContentEntry, error) {
	n := t.Len()
	if n > bufMaxContentEntries {
		return nil, pluginapi.Errf(pluginapi.CodeLimit, "too many entries (max %d)", bufMaxContentEntries)
	}
	conv := converter{p: p, seen: map[*lua.LTable]bool{}}
	out := make([]pluginapi.ContentEntry, 0, n)
	bytes, segments := 0, 0
	addBytes := func(s lua.LString) error {
		bytes += len(s)
		if bytes > bufMaxReadBytes {
			return pluginapi.Errf(pluginapi.CodeTooLarge, "content is over %d bytes", bufMaxReadBytes)
		}
		return nil
	}
	style := func(i int, v lua.LValue) pluginapi.Style {
		st, err := styleFromLua(v)
		if err != nil {
			L.ArgError(2, "entry #"+strconv.Itoa(i)+": "+err.Error())
		}
		return st
	}
	for i := 1; i <= n; i++ {
		et, ok := t.RawGetInt(i).(*lua.LTable)
		if !ok {
			L.ArgError(2, "entry #"+strconv.Itoa(i)+" must be a table {text=, style=, segments=, props=}")
		}
		var e pluginapi.ContentEntry
		e.Style = style(i, et.RawGetString("style"))
		switch sv := et.RawGetString("segments").(type) {
		case *lua.LNilType:
		case *lua.LTable:
			for j := 1; j <= sv.Len(); j++ {
				segments++
				if segments > bufMaxContentSegments {
					return nil, pluginapi.Errf(pluginapi.CodeLimit, "too many segments (max %d)", bufMaxContentSegments)
				}
				st, ok := sv.RawGetInt(j).(*lua.LTable)
				text, tok := lua.LValue(lua.LNil), false
				if ok {
					text = st.RawGetString("text")
					_, tok = text.(lua.LString)
				}
				if !tok {
					L.ArgError(2, "entry #"+strconv.Itoa(i)+": segment #"+strconv.Itoa(j)+" must be a table with a string 'text'")
				}
				if err := addBytes(text.(lua.LString)); err != nil {
					return nil, err
				}
				e.Segments = append(e.Segments, pluginapi.Segment{Text: string(text.(lua.LString)), Style: style(i, st.RawGetString("style"))})
			}
		default:
			L.ArgError(2, "entry #"+strconv.Itoa(i)+": segments must be an array")
		}
		switch tv := et.RawGetString("text").(type) {
		case *lua.LNilType:
			if len(e.Segments) == 0 {
				L.ArgError(2, "entry #"+strconv.Itoa(i)+" needs a string 'text' or 'segments'")
			}
		case lua.LString:
			if len(e.Segments) == 0 { // segments are the text when there are any
				if err := addBytes(tv); err != nil {
					return nil, err
				}
				e.Text = string(tv)
			}
		default:
			L.ArgError(2, "entry #"+strconv.Itoa(i)+": text must be a string")
		}
		if pv := et.RawGetString("props"); pv != lua.LNil {
			if _, ok := pv.(*lua.LTable); !ok {
				L.ArgError(2, "entry #"+strconv.Itoa(i)+": props must be a table")
			}
			v, err := conv.from(pv, 0)
			if err != nil {
				return nil, err
			}
			m, ok := v.(map[string]any)
			if !ok {
				L.ArgError(2, "entry #"+strconv.Itoa(i)+": props must be a table with string keys")
			}
			e.Props = m
		}
		out = append(out, e)
	}
	return out, nil
}

// installBufVirtual is the virtual-buffer part of the buffer API (stage S18): gvim.buf.create
// and the methods set_content, props_at, set_read_only, set_mode, set_option and get_option.
func installBufVirtual(e *apiEnv) {
	p := e.p
	area := e.area("buf")
	methods := e.kind(kindBuffer).methods
	host := func() pluginapi.Host { return p.r.guard }
	e.provides("virtual_buffers")

	meth := func(name string, f func(L *lua.LState, id pluginapi.BufferID) int) {
		e.set(methods, name, func(L *lua.LState) int {
			return f(L, pluginapi.BufferID(checkID(L, 1, kindBuffer)))
		})
	}

	// gvim.buf.create{name=, text=, language=, read_only=false, mode=, focus=false}
	e.set(area, "create", func(L *lua.LState) int {
		opts := bufTableOpts{t: L.CheckTable(1)}
		name, _ := opts.str("name")
		text, _ := opts.str("text")
		lang, _ := opts.str("language")
		mode, _ := opts.str("mode")
		o := pluginapi.CreateBuffer{
			Plugin: p.name, Name: name, Text: text, Language: lang, Mode: mode,
			ReadOnly: opts.boolean("read_only", false), Focus: opts.boolean("focus", false),
		}
		if opts.err != "" {
			L.ArgError(1, opts.err)
		}
		switch {
		case strings.TrimSpace(name) == "":
			return bufRet(L, pluginapi.Errf(pluginapi.CodeInvalid, "a virtual buffer needs a name"))
		case len(text) > bufMaxReadBytes:
			return bufRet(L, pluginapi.Errf(pluginapi.CodeTooLarge, "text is over %d bytes", bufMaxReadBytes))
		case len(mode) > bufMaxModeName:
			return bufRet(L, pluginapi.Errf(pluginapi.CodeInvalid, "mode name is longer than %d bytes", bufMaxModeName))
		}
		id, err := host().CreateBuffer(o)
		if err != nil {
			return bufRet(L, err)
		}
		L.Push(p.toLua(id))
		return 1
	})

	meth("set_content", func(L *lua.LState, id pluginapi.BufferID) int {
		entries, err := p.contentFromLua(L, L.CheckTable(2))
		if err != nil {
			return bufRet(L, err)
		}
		return bufTrue(L, host().SetContent(id, entries))
	})
	meth("props_at", func(L *lua.LState, id pluginapi.BufferID) int {
		var pos pluginapi.Pos
		if L.Get(2) != lua.LNil {
			pos = checkPos(L, 2)
		} else {
			carets, err := host().Cursors(id)
			if err != nil {
				return bufRet(L, err)
			}
			if len(carets) > 0 {
				pos = carets[0].Pos
			}
		}
		props, err := host().PropsAt(id, pos)
		if err != nil {
			return bufRet(L, err)
		}
		if props == nil {
			L.Push(lua.LNil)
			return 1
		}
		L.Push(p.toLua(props))
		return 1
	})
	meth("set_read_only", func(L *lua.LState, id pluginapi.BufferID) int {
		v, ok := L.CheckAny(2).(lua.LBool)
		if !ok {
			L.ArgError(2, "boolean expected, got "+L.Get(2).Type().String())
		}
		return bufTrue(L, host().SetReadOnly(id, bool(v)))
	})
	meth("set_mode", func(L *lua.LState, id pluginapi.BufferID) int {
		mode := ""
		switch v := L.Get(2).(type) {
		case *lua.LNilType:
		case lua.LString:
			mode = string(v)
		default:
			L.ArgError(2, "string or nil expected, got "+v.Type().String())
		}
		if len(mode) > bufMaxModeName {
			return bufRet(L, pluginapi.Errf(pluginapi.CodeInvalid, "mode name is longer than %d bytes", bufMaxModeName))
		}
		return bufTrue(L, host().SetBufferMode(id, mode))
	})
	meth("set_option", func(L *lua.LState, id pluginapi.BufferID) int {
		name := L.CheckString(2)
		var v any
		switch x := L.Get(3).(type) {
		case *lua.LNilType:
		case lua.LBool:
			v = bool(x)
		case lua.LNumber:
			v = float64(x)
		default:
			L.ArgError(3, "boolean, number or nil expected, got "+x.Type().String())
		}
		return bufTrue(L, host().SetOption(id, name, v))
	})
	meth("get_option", func(L *lua.LState, id pluginapi.BufferID) int {
		v, err := host().GetOption(id, L.CheckString(2))
		if err != nil {
			return bufRet(L, err)
		}
		L.Push(p.toLua(v))
		return 1
	})
}

// ---------- awaitable calls ----------

// bufArg returns the Buffer handle argument i of an await call.
func bufArg(c *awaitCall, i int, fn string) (pluginapi.BufferID, bool) {
	if i < len(c.args) {
		if ud, ok := c.args[i].(*lua.LUserData); ok {
			if h, ok := ud.Value.(idHandle); ok && h.kind == kindBuffer {
				return pluginapi.BufferID(h.id), true
			}
		}
	}
	c.Bad(i+1, fn, "Buffer expected")
	return 0, false
}

// bufOptional returns the optional table argument i of an await call; ok is false after
// the call was failed with Bad.
func bufOptional(c *awaitCall, i int, fn string) (t *lua.LTable, ok bool) {
	if i >= len(c.args) || c.args[i] == lua.LNil {
		return nil, true
	}
	if t, ok := c.args[i].(*lua.LTable); ok {
		return t, true
	}
	c.Bad(i+1, fn, "table expected")
	return nil, false
}

// bufConvertTrue resumes with true, or with the failure of the completion.
func bufConvertTrue(c pluginapi.Completion) []lua.LValue {
	if c.Err != "" {
		return []lua.LValue{lua.LTrue, lua.LNil, lua.LString(c.Err)}
	}
	return []lua.LValue{lua.LTrue, lua.LTrue}
}

// rootPath resolves a relative path against the workspace root, the base of the paths that
// gvim.workspace.files and grep return, so that those can be handed to gvim.buf.open as they are.
// An absolute path, or a session with no root, is left as it is.
func (r *Runtime) rootPath(path string) string {
	if filepath.IsAbs(path) {
		return path
	}
	if root := r.os.Dirs().Root; root != "" {
		return filepath.Join(root, path)
	}
	return path
}

// awaitBufOpen is gvim.buf.open(path, opts?).
func awaitBufOpen(c *awaitCall) {
	var req pluginapi.OpenRequest
	var s lua.LString
	ok := false
	if len(c.args) > 0 {
		s, ok = c.args[0].(lua.LString)
	}
	if !ok {
		c.Bad(1, "open", "string expected")
		return
	}
	if s == "" {
		c.Fail(pluginapi.Errf(pluginapi.CodeInvalid, "empty path"))
		return
	}
	req.Path = c.r.rootPath(string(s))
	t, ok := bufOptional(c, 1, "open")
	if !ok {
		return
	}
	opts := bufTableOpts{t: t}
	req.Focus = opts.boolean("focus", true)
	req.Preview = opts.boolean("preview", false)
	req.Background = opts.boolean("background", false)
	if pv := opts.get("pos"); pv != lua.LNil {
		pos, err := posFromLua(pv)
		if err != nil {
			c.Bad(2, "open", "option 'pos': "+err.Error())
			return
		}
		req.Pos = &pos
	}
	if opts.err != "" {
		c.Bad(2, "open", opts.err)
		return
	}
	if err := c.CheckPark(false); err != nil {
		c.Fail(err)
		return
	}
	tok := c.NewToken()
	if err := c.r.guard.Open(tok, req); err != nil {
		c.Fail(err)
		return
	}
	p := c.p
	c.Park(&parkSpec{Token: tok, Convert: func(cp pluginapi.Completion) []lua.LValue {
		if cp.Err != "" {
			return []lua.LValue{lua.LTrue, lua.LNil, lua.LString(cp.Err)}
		}
		if len(cp.Values) > 0 {
			if n, ok := wholeNumber(cp.Values[0]); ok && n > 0 {
				return []lua.LValue{lua.LTrue, p.toLua(pluginapi.BufferID(n))}
			}
		}
		return []lua.LValue{lua.LTrue, lua.LNil, lua.LString("io: open finished without a buffer")}
	}})
}

// awaitBufSave is b:save(path?).
func awaitBufSave(c *awaitCall) {
	id, ok := bufArg(c, 0, "save")
	if !ok {
		return
	}
	var path string
	if len(c.args) > 1 && c.args[1] != lua.LNil {
		s, ok := c.args[1].(lua.LString)
		if !ok {
			c.Bad(2, "save", "string expected")
			return
		}
		path = string(s)
	}
	bufAsyncHost(c, func(tok pluginapi.Token) error { return c.r.guard.Save(tok, id, path) })
}

// awaitBufRevert is b:revert().
func awaitBufRevert(c *awaitCall) {
	id, ok := bufArg(c, 0, "revert")
	if !ok {
		return
	}
	bufAsyncHost(c, func(tok pluginapi.Token) error { return c.r.guard.Revert(tok, id) })
}

// bufAsyncHost starts an async host call that completes with success or an error.
func bufAsyncHost(c *awaitCall, start func(pluginapi.Token) error) {
	if err := c.CheckPark(false); err != nil {
		c.Fail(err)
		return
	}
	tok := c.NewToken()
	if err := start(tok); err != nil {
		c.Fail(err)
		return
	}
	c.Park(&parkSpec{Token: tok, Convert: bufConvertTrue})
}

// awaitBufDiffSaved is b:diff_saved(): the line hunks between the saved text and the
// buffer. Both sides are read now; only the diff itself runs in Work.
func awaitBufDiffSaved(c *awaitCall) {
	id, ok := bufArg(c, 0, "diff_saved")
	if !ok {
		return
	}
	if _, none := c.r.svc.(noServices); none {
		c.Fail(pluginapi.Errf(pluginapi.CodeUnsupported, "Buffer:diff_saved: no diff service"))
		return
	}
	if err := c.CheckPark(true); err != nil {
		c.Fail(err)
		return
	}
	saved, err := c.r.guard.SavedLines(id)
	if err != nil {
		c.Fail(err)
		return
	}
	cur, err := c.r.guard.Lines(id, 0, -1)
	if err == nil {
		if err = bufCheckRead(cur); err == nil {
			err = bufCheckRead(saved)
		}
	}
	if err != nil {
		c.Fail(err)
		return
	}
	svc := c.r.svc
	c.Park(&parkSpec{Work: &pluginapi.Work{Run: func(context.Context) pluginapi.Completion {
		return pluginapi.Completion{Values: []any{svc.Diff(saved, cur)}}
	}}})
}
