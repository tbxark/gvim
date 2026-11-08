package plugin

import (
	"context"
	"strings"
	"unicode/utf8"

	lua "github.com/yuin/gopher-lua"

	"github.com/tbxark/gvim/plugin/pattern"
	"github.com/tbxark/gvim/pluginapi"
)

// gvim.text: display width (Services), case, trim, split, prefix tests, fuzzy matching
// (a Work over Services.Fuzzy) and decode (Services.Decode).

const (
	maxFuzzyItems   = 50_000
	maxFuzzyQuery   = 256  // bytes: the scoring is quadratic in the query length
	maxFuzzyText    = 4096 // bytes of one item that take part in the match (like picker items)
	maxFuzzyLimit   = 1000
	defaultFuzzyMax = 50
	maxDecodeBytes  = 16 << 20 // buffer.MaxMaterialize
)

func init() {
	registerAwait("text_fuzzy", awaitFuzzy)
}

func installText(e *apiEnv) {
	r := e.r
	t := e.area("text")

	e.set(t, "width", func(L *lua.LState) int {
		L.Push(lua.LNumber(r.svc.Width(L.CheckString(1))))
		return 1
	})
	e.set(t, "truncate", func(L *lua.LState) int {
		s := L.CheckString(1)
		w := checkWidth(L, 2)
		L.Push(lua.LString(r.svc.Truncate(s, w, L.OptString(3, ""))))
		return 1
	})
	e.set(t, "pad", func(L *lua.LState) int {
		s := L.CheckString(1)
		w := checkWidth(L, 2)
		align := L.OptString(3, "left")
		if align != "left" && align != "right" && align != "center" {
			L.ArgError(3, `align must be "left", "right" or "center"`)
		}
		missing := w - r.svc.Width(s)
		if missing <= 0 {
			L.Push(lua.LString(s))
			return 1
		}
		switch align {
		case "left":
			s += strings.Repeat(" ", missing)
		case "right":
			s = strings.Repeat(" ", missing) + s
		default:
			s = strings.Repeat(" ", missing/2) + s + strings.Repeat(" ", missing-missing/2)
		}
		L.Push(lua.LString(s))
		return 1
	})
	e.set(t, "upper", func(L *lua.LState) int {
		L.Push(lua.LString(mapCase(L.CheckString(1), true)))
		return 1
	})
	e.set(t, "lower", func(L *lua.LState) int {
		L.Push(lua.LString(mapCase(L.CheckString(1), false)))
		return 1
	})
	e.set(t, "trim", func(L *lua.LState) int {
		L.Push(lua.LString(strings.TrimSpace(L.CheckString(1))))
		return 1
	})
	e.set(t, "starts_with", func(L *lua.LState) int {
		L.Push(lua.LBool(strings.HasPrefix(L.CheckString(1), L.CheckString(2))))
		return 1
	})
	e.set(t, "ends_with", func(L *lua.LState) int {
		L.Push(lua.LBool(strings.HasSuffix(L.CheckString(1), L.CheckString(2))))
		return 1
	})
	e.set(t, "split", textSplit)
	t.RawSetString("fuzzy", e.async("text_fuzzy"))
	e.set(t, "decode", func(L *lua.LState) int {
		data := L.CheckString(1)
		enc := L.CheckString(2)
		normalize := false
		if o := L.OptTable(3, nil); o != nil {
			normalize = lua.LVAsBool(o.RawGetString("normalize"))
		}
		if len(data) > maxDecodeBytes {
			return errReturn(L, pluginapi.Errf(pluginapi.CodeTooLarge, "text.decode: more than %d bytes", maxDecodeBytes))
		}
		out, err := r.svc.Decode(enc, []byte(data))
		if err != nil {
			return errReturn(L, err)
		}
		if normalize {
			out = normalizeEOL(out)
		}
		L.Push(lua.LString(out))
		return 1
	})
}

// normalizeEOL turns CRLF and lone CR into LF.
func normalizeEOL(s string) string {
	if !strings.Contains(s, "\r") {
		return s
	}
	return strings.ReplaceAll(strings.ReplaceAll(s, "\r\n", "\n"), "\r", "\n")
}

// checkWidth reads a display width argument and applies MaxPadWidth.
func checkWidth(L *lua.LState, n int) int {
	w := checkI64(L, n)
	if w > MaxPadWidth {
		L.RaiseError("width too large")
	}
	return int(max(w, 0))
}

// mapCase upper- or lower-cases s. Invalid UTF-8 would be rewritten by the Unicode
// mapping, so such strings only change ASCII letters.
func mapCase(s string, upper bool) string {
	if utf8.ValidString(s) {
		if upper {
			return strings.ToUpper(s)
		}
		return strings.ToLower(s)
	}
	b := []byte(s)
	for i, c := range b {
		switch {
		case upper && c >= 'a' && c <= 'z':
			b[i] = c - 32
		case !upper && c >= 'A' && c <= 'Z':
			b[i] = c + 32
		}
	}
	return string(b)
}

// textSplit is gvim.text.split(s, sep, plain?). sep is a Lua pattern unless plain is true;
// an empty sep is an error; more than MaxSplitParts pieces is an error.
func textSplit(L *lua.LState) int {
	s, sep := L.CheckString(1), L.CheckString(2)
	plain := lua.LVAsBool(L.Get(3))
	if sep == "" {
		L.ArgError(2, "separator is empty")
	}
	t := L.NewTable()
	add := func(piece string) {
		if t.Len() >= MaxSplitParts {
			L.RaiseError("too many pieces (max %d)", MaxSplitParts)
		}
		t.Append(lua.LString(piece))
	}
	if plain {
		if strings.Count(s, sep) >= MaxSplitParts {
			L.RaiseError("too many pieces (max %d)", MaxSplitParts)
		}
		for _, piece := range strings.Split(s, sep) {
			t.Append(lua.LString(piece))
		}
		L.Push(t)
		return 1
	}
	o := patOptions(L)
	pos := 0
	for from := 0; ; {
		res, ok, err := pattern.Find(s, sep, from+1, false, o)
		if err != nil {
			raisePat(L, err)
		}
		if !ok {
			break
		}
		if res.End == res.Start { // an empty match splits nothing
			from = res.Start + 1
			if from > len(s) {
				break
			}
			continue
		}
		add(s[pos:res.Start])
		pos, from = res.End, res.End
	}
	add(s[pos:])
	L.Push(t)
	return 1
}

// ---------- fuzzy ----------

// awaitFuzzy is gvim.text.fuzzy(query, items, opts?): items are strings or tables with a
// string field opts.key; the result keeps the original elements.
func awaitFuzzy(c *awaitCall) {
	if len(c.args) < 2 {
		c.Bad(1, "fuzzy", "query and items expected")
		return
	}
	q, ok := c.args[0].(lua.LString)
	items, ok2 := c.args[1].(*lua.LTable)
	if !ok || !ok2 {
		c.Bad(1, "fuzzy", "string query and table of items expected")
		return
	}
	key, limit := "", defaultFuzzyMax
	if len(c.args) > 2 {
		if o, isT := c.args[2].(*lua.LTable); isT {
			if k, isS := o.RawGetString("key").(lua.LString); isS {
				key = string(k)
			}
			if n, isN := o.RawGetString("limit").(lua.LNumber); isN {
				limit = clampInt(float64(n))
			}
		}
	}
	limit = max(1, min(limit, maxFuzzyLimit))
	if len(q) > maxFuzzyQuery {
		c.Fail(pluginapi.Errf(pluginapi.CodeTooLarge, "text.fuzzy: query longer than %d bytes", maxFuzzyQuery))
		return
	}
	n := items.Len()
	if n > maxFuzzyItems {
		c.Fail(pluginapi.Errf(pluginapi.CodeTooLarge, "text.fuzzy: more than %d items", maxFuzzyItems))
		return
	}
	texts := make([]string, n)
	orig := make([]lua.LValue, n) // the elements as they are now: the table may change while parked
	for i := 1; i <= n; i++ {
		v := items.RawGetInt(i)
		orig[i-1] = v
		if key != "" {
			if tb, isT := v.(*lua.LTable); isT {
				v = tb.RawGetString(key)
			}
		}
		switch x := v.(type) {
		case lua.LString:
			texts[i-1] = clipText(string(x), maxFuzzyText)
		case lua.LNumber:
			texts[i-1] = x.String()
		default:
			c.Bad(2, "fuzzy", "item "+itoaPlugin(i)+" is not a string")
			return
		}
	}
	if err := c.CheckPark(true); err != nil {
		c.Fail(err)
		return
	}
	svc := c.r.svc
	query := string(q)
	c.Park(&parkSpec{
		Work: &pluginapi.Work{Run: func(ctx context.Context) pluginapi.Completion {
			m := svc.Fuzzy(ctx, query, texts, limit)
			if ctx.Err() != nil {
				return pluginapi.Completion{Err: string(pluginapi.CodeCancelled) + ": text.fuzzy cancelled"}
			}
			return pluginapi.Completion{Values: []any{m}}
		}},
		Convert: func(comp pluginapi.Completion) []lua.LValue {
			if comp.Err != "" || len(comp.Values) == 0 {
				return []lua.LValue{lua.LTrue, lua.LNil, lua.LString(comp.Err)}
			}
			out := c.p.L.NewTable()
			for _, m := range comp.Values[0].([]pluginapi.FuzzyMatch) {
				row := c.p.L.CreateTable(0, 3)
				row.RawSetString("item", orig[m.Index])
				row.RawSetString("score", lua.LNumber(m.Score))
				idx := c.p.L.CreateTable(len(m.Indexes), 0)
				for _, i := range m.Indexes {
					idx.Append(lua.LNumber(i + 1)) // 1-based character positions, like utf8.sub
				}
				row.RawSetString("indexes", idx)
				out.Append(row)
			}
			return []lua.LValue{lua.LTrue, out}
		},
	})
}

func itoaPlugin(n int) string { return lua.LNumber(n).String() }
