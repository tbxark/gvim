package plugin

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"

	lua "github.com/yuin/gopher-lua"

	"github.com/tbxark/gvim/pluginapi"
)

// gvim.decor and the Namespace handle (FINAL 5.7, AMENDMENTS C4, C13, C23).
//
//	local ns = gvim.decor.namespace("blame")
//	ns:set(buf, items, {version = tok}) -> true | nil, err
//	ns:add(buf, item, {version = tok})  -> id | nil, err
//	ns:remove(buf, id)                  -> true
//	ns:clear(buf?)                      -> true
//
// A decoration is drawn over the document and is not part of it. Its position is an anchor
// that follows the text: the host moves it as the document changes, so a position that
// was right when it was set stays on the same text. A decoration that was anchored to text
// that is gone (the range is deleted, the line is removed) disappears. Positions are 0-based
// lines and byte columns, and are clamped, never an error: a line past the end is the last
// line, and a column of 2^30 or more (2^31 works) is the end of its line.
//
// A decoration can be set with {version = tok}, the token b:version() gave: the call then
// fails with "stale" when the buffer is not in that state any more, so positions computed
// against old text are never applied. A buffer that is replaced as a whole (reverted,
// reloaded) or a large file that is edited drops its decorations: set them again when the
// buffer_changed event reports overflow.
//
// Everything is synchronous. Nothing here reads the document; the host answers per painted
// line, so a decoration costs nothing on a large buffer until it is on screen.

const (
	kindNamespace = "Namespace"

	// decorMaxItems is the most items one set call takes: the quota of a namespace on one
	// buffer. The host enforces it again, with the quota of the plugin.
	decorMaxItems = 20_000
	// decorMaxName bounds the name of a namespace.
	decorMaxName = 128
	// decorMaxVirtualText bounds the text of a virtual-text, gutter or replace item.
	decorMaxVirtualText = 1024
	// maxPluginRangeCount bounds one set_folding_ranges call.
	maxPluginRangeCount = 2000
)

var decorKinds = map[string]pluginapi.DecorKind{
	"highlight": pluginapi.DecorHighlight,
	"line":      pluginapi.DecorLine,
	"eol":       pluginapi.DecorEOL,
	"gutter":    pluginapi.DecorGutter,
	"scroll":    pluginapi.DecorScroll,
	"replace":   pluginapi.DecorReplace,
	// tier 1c-A
	"virtual_line_above":  pluginapi.DecorVirtualLineAbove,
	"virtual_line_below":  pluginapi.DecorVirtualLineBelow,
	"conceal":             pluginapi.DecorConceal,
	"soft_break":          pluginapi.DecorSoftBreak,
	"cursor_line_overlay": pluginapi.DecorCursorLine,
}

// activationFromLua reads an activation name.
func activationFromLua(v lua.LValue) (pluginapi.Activation, error) {
	if v == lua.LNil {
		return pluginapi.ActAlways, nil
	}
	s, ok := v.(lua.LString)
	if !ok {
		return pluginapi.ActAlways, fmt.Errorf("activation must be a string")
	}
	switch string(s) {
	case "", "always":
		return pluginapi.ActAlways, nil
	case "unless-cursor-in":
		return pluginapi.ActUnlessCursorIn, nil
	case "if-cursor-in":
		return pluginapi.ActIfCursorIn, nil
	}
	return pluginapi.ActAlways, fmt.Errorf("unknown activation %q", string(s))
}

// scopeFromLua reads an optional scope {start_line=, end_line=}.
func scopeFromLua(t *lua.LTable, d *pluginapi.Decoration) error {
	v := t.RawGetString("scope")
	if v == lua.LNil {
		d.ScopeStart, d.ScopeEnd = 0, -1
		return nil
	}
	st, ok := v.(*lua.LTable)
	if !ok {
		return fmt.Errorf("scope must be a table")
	}
	o := bufTableOpts{t: st}
	d.ScopeStart = o.integer("start_line", 0)
	d.ScopeEnd = o.integer("end_line", -1)
	if o.err != "" {
		return errors.New(o.err)
	}
	return nil
}

// prefixFromLua reads an optional soft-break prefix {text=, style=}.
func prefixFromLua(t *lua.LTable, d *pluginapi.Decoration) error {
	v := t.RawGetString("prefix")
	if v == lua.LNil {
		return nil
	}
	pt, ok := v.(*lua.LTable)
	if !ok {
		return fmt.Errorf("prefix must be a table")
	}
	o := bufTableOpts{t: pt}
	d.PrefixText = clipText(optString(&o, "text"), decorMaxVirtualText)
	if o.err != "" {
		return errors.New(o.err)
	}
	st, err := styleFromLua(pt.RawGetString("style"))
	if err != nil {
		return err
	}
	d.PrefixStyle = st
	return nil
}

// namespace is the Namespace handle.
type namespace struct {
	name string
}

func (n *namespace) handleString() string { return n.name }

// colorFromLua reads a Color: "#rrggbb", an array {r, g, b} (or a table with r, g and b
// fields), a theme key "section.key" or a semantic name.
func colorFromLua(v lua.LValue) (pluginapi.Color, error) {
	switch c := v.(type) {
	case *lua.LNilType:
		return pluginapi.Color{}, nil
	case lua.LString:
		s := string(c)
		switch {
		case s == "":
			return pluginapi.Color{}, nil
		case s[0] == '#':
			if len(s) != 7 {
				return pluginapi.Color{}, fmt.Errorf("color %q is not #rrggbb", s)
			}
			n, err := strconv.ParseUint(s[1:], 16, 32)
			if err != nil {
				return pluginapi.Color{}, fmt.Errorf("color %q is not #rrggbb", s)
			}
			return pluginapi.Color{R: uint8(n >> 16), G: uint8(n >> 8), B: uint8(n), Set: true}, nil
		case len(s) > 64 || strings.ContainsAny(s, " \t\r\n"):
			return pluginapi.Color{}, fmt.Errorf("color name %q is not a theme key or a color name", s)
		}
		return pluginapi.Color{Key: s}, nil
	case *lua.LTable:
		var rgb [3]uint8
		for i, key := range []string{"r", "g", "b"} {
			n, ok := c.RawGetInt(i + 1).(lua.LNumber)
			if !ok {
				n, ok = c.RawGetString(key).(lua.LNumber)
			}
			if !ok {
				return pluginapi.Color{}, fmt.Errorf("color table needs three numbers {r, g, b}")
			}
			f := float64(n)
			if math.IsNaN(f) || f < 0 || f > 255 {
				return pluginapi.Color{}, fmt.Errorf("color component %v is outside 0..255", f)
			}
			rgb[i] = uint8(math.Round(f))
		}
		return pluginapi.Color{R: rgb[0], G: rgb[1], B: rgb[2], Set: true}, nil
	}
	return pluginapi.Color{}, fmt.Errorf("a color is a string or a table, got %s", v.Type())
}

// styleFromLua reads a Style: {fg=, bg=, bold=, italic=, underline=, strike=, reverse=, extend=},
// every field optional.
func styleFromLua(v lua.LValue) (pluginapi.Style, error) {
	var st pluginapi.Style
	if v == lua.LNil {
		return st, nil
	}
	t, ok := v.(*lua.LTable)
	if !ok {
		return st, fmt.Errorf("style must be a table, got %s", v.Type())
	}
	var err error
	if st.FG, err = colorFromLua(t.RawGetString("fg")); err != nil {
		return st, fmt.Errorf("style fg: %w", err)
	}
	if st.BG, err = colorFromLua(t.RawGetString("bg")); err != nil {
		return st, fmt.Errorf("style bg: %w", err)
	}
	o := bufTableOpts{t: t}
	st.Bold, st.Italic, st.Underline = o.boolean("bold", false), o.boolean("italic", false), o.boolean("underline", false)
	st.Strike, st.Reverse, st.Extend = o.boolean("strike", false), o.boolean("reverse", false), o.boolean("extend", false)
	if o.err != "" {
		return st, fmt.Errorf("style: %s", o.err)
	}
	return st, nil
}

// decorationFromLua reads one item.
func decorationFromLua(v lua.LValue) (pluginapi.Decoration, error) {
	var d pluginapi.Decoration
	t, ok := v.(*lua.LTable)
	if !ok {
		return d, fmt.Errorf("an item must be a table, got %s", v.Type())
	}
	name, ok := t.RawGetString("kind").(lua.LString)
	if !ok {
		return d, fmt.Errorf("an item needs a string 'kind'")
	}
	kind, ok := decorKinds[string(name)]
	if !ok {
		return d, fmt.Errorf("unknown kind %q (highlight, line, eol, gutter, scroll, replace)", string(name))
	}
	d.Kind = kind
	o := bufTableOpts{t: t}
	d.Priority = o.integer("priority", 0)
	if o.err != "" {
		return d, errors.New(o.err)
	}
	var err error
	if d.Style, err = styleFromLua(t.RawGetString("style")); err != nil {
		return d, err
	}
	line := func() error {
		n, ok := t.RawGetString("line").(lua.LNumber)
		if !ok {
			return fmt.Errorf("%s needs a numeric 'line'", name)
		}
		d.Line = clampInt(float64(n))
		return nil
	}
	text := func() error {
		s, ok := t.RawGetString("text").(lua.LString)
		if !ok {
			return fmt.Errorf("%s needs a string 'text'", name)
		}
		if len(s) > decorMaxVirtualText {
			return fmt.Errorf("%s text is longer than %d bytes", name, decorMaxVirtualText)
		}
		d.Text = string(s)
		return nil
	}
	switch kind {
	case pluginapi.DecorHighlight:
		if d.Range, err = rangeFromLua(t.RawGetString("range")); err != nil {
			return d, fmt.Errorf("highlight 'range': %w", err)
		}
	case pluginapi.DecorLine:
		err = line()
	case pluginapi.DecorEOL, pluginapi.DecorGutter:
		if err = line(); err == nil {
			err = text()
		}
	case pluginapi.DecorScroll:
		if r := t.RawGetString("range"); r != lua.LNil {
			if d.Range, err = rangeFromLua(r); err != nil {
				return d, fmt.Errorf("scroll 'range': %w", err)
			}
		} else {
			err = line()
		}
		if err == nil {
			d.Color, err = colorFromLua(t.RawGetString("color"))
		}
	case pluginapi.DecorReplace:
		if d.Pos, err = posFromLua(t.RawGetString("pos")); err != nil {
			return d, fmt.Errorf("replace 'pos': %w", err)
		}
		err = text()
	case pluginapi.DecorVirtualLineAbove, pluginapi.DecorVirtualLineBelow:
		if err = line(); err == nil {
			err = text()
		}
		if err == nil {
			if g, ok := t.RawGetString("gutter_glyph").(lua.LString); ok {
				d.GutterText = clipText(string(g), 8)
			}
			d.GutterStyle, err = styleFromLua(t.RawGetString("gutter_style"))
			if err == nil {
				d.GutterStyle.FG, _ = func() (pluginapi.Color, error) { return colorFromLua(t.RawGetString("gutter_color")) }()
			}
			d.ComposeOnly = o.boolean("compose_only", false)
			d.Activation, _ = activationFromLua(t.RawGetString("activation"))
			if err = scopeFromLua(t, &d); err != nil {
				return d, err
			}
			d.Above = kind == pluginapi.DecorVirtualLineAbove
			if tv := t.RawGetString("text_overlays"); tv != lua.LNil {
				list, ok := tv.(*lua.LTable)
				if !ok {
					return d, fmt.Errorf("text_overlays must be a table")
				}
				for i := 1; i <= min(list.Len(), 8); i++ {
					row, ok := list.RawGetInt(i).(*lua.LTable)
					if !ok {
						continue
					}
					ro := bufTableOpts{t: row}
					st, serr := styleFromLua(row.RawGetString("style"))
					if serr != nil {
						return d, serr
					}
					startByte := ro.integer("start", ro.integer("start_byte", 0))
					endByte := ro.integer("end", ro.integer("end_byte", startByte))
					ov := pluginapi.TextOverlay{
						Range: pluginapi.Range{
							From: pluginapi.Pos{Col: startByte},
							To:   pluginapi.Pos{Col: endByte},
						},
						Style: st,
					}
					if ro.err != "" {
						return d, errors.New(ro.err)
					}
					d.TextOverlays = append(d.TextOverlays, ov)
				}
			}
		}
	case pluginapi.DecorConceal:
		if d.Range, err = rangeFromLua(t.RawGetString("range")); err != nil {
			return d, fmt.Errorf("conceal 'range': %w", err)
		}
		if tv := t.RawGetString("text"); tv != lua.LNil {
			err = text()
		}
		if err == nil {
			d.Activation, err = activationFromLua(t.RawGetString("activation"))
		}
		if err == nil {
			d.ComposeOnly = o.boolean("compose_only", false)
			err = scopeFromLua(t, &d)
		}
	case pluginapi.DecorSoftBreak:
		if d.Pos, err = posFromLua(t.RawGetString("pos")); err != nil {
			return d, fmt.Errorf("soft_break 'pos': %w", err)
		}
		if err == nil {
			d.Indent = o.integer("indent", 0)
			d.Activation, err = activationFromLua(t.RawGetString("activation"))
		}
		if err == nil {
			d.ComposeOnly = o.boolean("compose_only", false)
			err = prefixFromLua(t, &d)
		}
		if err == nil {
			err = scopeFromLua(t, &d)
		}
	case pluginapi.DecorCursorLine:
		// style only
	}
	if err == nil && o.err != "" {
		err = errors.New(o.err)
	}
	if kind != pluginapi.DecorCursorLine {
		d.Line = max(d.Line, 0)
	}
	return d, err
}

// installDecor installs decorations: gvim.decor and the Namespace handle (stage S15).
func installDecor(e *apiEnv) {
	p := e.p
	e.provides("decor")
	area := e.area("decor")
	host := func() pluginapi.Host { return p.r.guard }

	names := map[string]bool{}
	p.decorNS = names
	// The host may report that it paints the tier-1c surface (virtual lines, conceal, soft
	// breaks); gvim.has("decor_virtual_paint") is true exactly then. A host without the
	// optional Feature seam is treated as not painting it, so plugins keep working with an
	// older host.
	if f, ok := p.r.host.(interface{ Feature(string) bool }); ok && f.Feature("decor_virtual_paint") {
		e.provides("decor_virtual_paint")
	}
	e.set(area, "namespace", func(L *lua.LState) int {
		name := L.CheckString(1)
		switch {
		case name == "" || len(name) > decorMaxName:
			return bufRet(L, pluginapi.Errf(pluginapi.CodeInvalid, "namespace name must be 1 to %d bytes", decorMaxName))
		case !names[name] && len(names) >= QuotaNamespaces:
			return bufRet(L, pluginapi.Errf(pluginapi.CodeLimit, "namespace quota (%d)", QuotaNamespaces))
		}
		names[name] = true
		L.Push(p.newObject(kindNamespace, &namespace{name: name}))
		return 1
	})

	methods := e.kind(kindNamespace).methods
	self := func(L *lua.LState) *namespace { return checkObject(L, 1, kindNamespace).(*namespace) }
	// version reads opts.version (argument n): absent means no check. A malformed token is
	// returned as an error (AMENDMENTS C4), like b:edit does.
	version := func(L *lua.LState, n int) (*pluginapi.Version, error) {
		o := bufTableOpts{t: bufOptTable(L, n)}
		return bufParseVersion(o.get("version"))
	}

	e.set(methods, "set", func(L *lua.LState) int {
		ns := self(L)
		id := pluginapi.BufferID(checkID(L, 2, kindBuffer))
		list := L.CheckTable(3)
		expect, verr := version(L, 4)
		if verr != nil {
			return bufRet(L, verr)
		}
		n := list.Len()
		if n > decorMaxItems {
			return bufRet(L, pluginapi.Errf(pluginapi.CodeLimit, "too many items (max %d per namespace)", decorMaxItems))
		}
		items := make([]pluginapi.Decoration, 0, n)
		for i := 1; i <= n; i++ {
			d, err := decorationFromLua(list.RawGetInt(i))
			if err != nil {
				return bufRet(L, pluginapi.Errf(pluginapi.CodeInvalid, "item %d: %v", i, err))
			}
			items = append(items, d)
		}
		return bufTrue(L, host().DecorSet(p.name, ns.name, id, items, expect))
	})
	e.set(methods, "add", func(L *lua.LState) int {
		ns := self(L)
		id := pluginapi.BufferID(checkID(L, 2, kindBuffer))
		d, err := decorationFromLua(L.CheckTable(3))
		if err != nil {
			return bufRet(L, pluginapi.Errf(pluginapi.CodeInvalid, "%v", err))
		}
		expect, verr := version(L, 4)
		if verr != nil {
			return bufRet(L, verr)
		}
		item, err := host().DecorAdd(p.name, ns.name, id, d, expect)
		if err != nil {
			return bufRet(L, err)
		}
		L.Push(lua.LNumber(item))
		return 1
	})
	e.set(methods, "remove", func(L *lua.LState) int {
		ns := self(L)
		id := pluginapi.BufferID(checkID(L, 2, kindBuffer))
		host().DecorRemove(p.name, ns.name, id, int(L.CheckInt64(3)))
		L.Push(lua.LTrue)
		return 1
	})
	e.set(methods, "clear", func(L *lua.LState) int {
		ns := self(L)
		var id pluginapi.BufferID
		if L.Get(2) != lua.LNil {
			id = pluginapi.BufferID(checkID(L, 2, kindBuffer))
		}
		host().DecorClear(p.name, ns.name, id)
		L.Push(lua.LTrue)
		return 1
	})

	// ---- tier-1c-A convenience functions -------------------------------------------------
	//
	// They take a Buffer as their first argument and an optional opts table with at least
	// namespace (default "default"), priority, version and the kind-specific fields the
	// matching item kind documents. They are thin wrappers over DecorAdd/DecorSet so the
	// item parser above stays the single validation path.

	defaultNS := func(L *lua.LState, o *bufTableOpts) (string, error) {
		name, ok := o.str("namespace")
		if !ok || name == "" {
			name = "default"
		}
		if len(name) > decorMaxName {
			return "", pluginapi.Errf(pluginapi.CodeInvalid, "namespace must be at most %d bytes", decorMaxName)
		}
		if !names[name] && len(names) >= QuotaNamespaces {
			return "", pluginapi.Errf(pluginapi.CodeLimit, "namespace quota (%d)", QuotaNamespaces)
		}
		names[name] = true
		return name, nil
	}
	optsOf := func(L *lua.LState, n int) bufTableOpts {
		t, _ := L.Get(n).(*lua.LTable)
		return bufTableOpts{t: t}
	}

	addItem := func(L *lua.LState, buf int, o *bufTableOpts, item pluginapi.Decoration) int {
		ns, err := defaultNS(L, o)
		if err != nil {
			return bufRet(L, err)
		}
		id := pluginapi.BufferID(checkID(L, buf, kindBuffer))
		expect, verr := bufParseVersion(o.get("version"))
		if verr != nil {
			return bufRet(L, verr)
		}
		item.Priority = o.integer("priority", item.Priority)
		if o.err != "" {
			return bufRet(L, invalidErr("%s", o.err))
		}
		n, err := host().DecorAdd(p.name, ns, id, item, expect)
		if err != nil {
			return bufRet(L, err)
		}
		L.Push(lua.LNumber(n))
		return 1
	}

	addVirtual := func(L *lua.LState, above bool) int {
		o := optsOf(L, 4)
		d := pluginapi.Decoration{
			Kind: pluginapi.DecorVirtualLineAbove, Line: clampInt(float64(L.CheckNumber(2))),
			Text: clipText(L.CheckString(3), decorMaxVirtualText), ComposeOnly: o.boolean("compose_only", false),
		}
		if !above {
			d.Kind = pluginapi.DecorVirtualLineBelow
		}
		d.Style, _ = styleFromLua(o.get("style"))
		if g, ok := o.get("gutter_glyph").(lua.LString); ok {
			d.GutterText = clipText(string(g), 8)
		}
		d.GutterStyle, _ = styleFromLua(o.get("gutter_style"))
		if c, err := colorFromLua(o.get("gutter_color")); err == nil {
			d.GutterStyle.FG = c
		}
		d.Activation, _ = activationFromLua(o.get("activation"))
		_ = scopeFromLua(o.t, &d)
		if o.err != "" {
			return bufRet(L, invalidErr("%s", o.err))
		}
		return addItem(L, 1, &o, d)
	}
	e.set(area, "add_virtual_line", func(L *lua.LState) int { return addVirtual(L, true) })
	e.set(area, "add_virtual_line_below", func(L *lua.LState) int { return addVirtual(L, false) })
	e.set(area, "add_conceal", func(L *lua.LState) int {
		o := optsOf(L, 5)
		d := pluginapi.Decoration{Kind: pluginapi.DecorConceal, ComposeOnly: o.boolean("compose_only", false)}
		from, ferr := posFromLua(L.Get(2))
		if ferr != nil {
			return bufRet(L, invalidErr("conceal start: %s", ferr))
		}
		to, terr := posFromLua(L.Get(3))
		if terr != nil {
			return bufRet(L, invalidErr("conceal end: %s", terr))
		}
		d.Range = pluginapi.Range{From: from, To: to}
		if d.Range.From == d.Range.To {
			return bufRet(L, invalidErr("conceal needs a non-empty range"))
		}
		if tv := L.Get(4); tv != lua.LNil {
			d.Text = clipText(L.CheckString(4), decorMaxVirtualText)
		}
		d.Style, _ = styleFromLua(o.get("style"))
		d.Activation, _ = activationFromLua(o.get("activation"))
		_ = scopeFromLua(o.t, &d)
		if o.err != "" {
			return bufRet(L, invalidErr("%s", o.err))
		}
		return addItem(L, 1, &o, d)
	})
	e.set(area, "add_soft_break", func(L *lua.LState) int {
		o := optsOf(L, 3)
		d := pluginapi.Decoration{Kind: pluginapi.DecorSoftBreak, ComposeOnly: o.boolean("compose_only", false)}
		d.Pos, _ = posFromLua(L.Get(2))
		d.Indent = o.integer("indent", 0)
		_ = prefixFromLua(o.t, &d)
		d.Activation, _ = activationFromLua(o.get("activation"))
		_ = scopeFromLua(o.t, &d)
		if o.err != "" {
			return bufRet(L, invalidErr("%s", o.err))
		}
		return addItem(L, 1, &o, d)
	})
	e.set(area, "set_cursor_line_overlay", func(L *lua.LState) int {
		o := optsOf(L, 3)
		d := pluginapi.Decoration{Kind: pluginapi.DecorCursorLine}
		d.Style, _ = styleFromLua(L.Get(2))
		if o.err != "" {
			return bufRet(L, invalidErr("%s", o.err))
		}
		return addItem(L, 1, &o, d)
	})
	e.set(area, "set_scrollbar_markers_in_range", func(L *lua.LState) int {
		o := optsOf(L, 4)
		d := pluginapi.Decoration{Kind: pluginapi.DecorScroll}
		d.Range, _ = rangeFromLua(L.Get(2))
		if c, err := colorFromLua(L.Get(3)); err == nil {
			d.Color = c
		}
		if o.err != "" {
			return bufRet(L, invalidErr("%s", o.err))
		}
		return addItem(L, 1, &o, d)
	})
	e.set(area, "clear_decorations", func(L *lua.LState) int {
		o := optsOf(L, 3)
		ns, err := defaultNS(L, &o)
		if err != nil {
			return bufRet(L, err)
		}
		var id pluginapi.BufferID
		if L.Get(1) != lua.LNil {
			id = pluginapi.BufferID(checkID(L, 1, kindBuffer))
		}
		if L.Get(2) != lua.LNil && L.Get(2).Type() == lua.LTTable {
			r, rerr := rangeFromLua(L.Get(2))
			if rerr != nil {
				return bufRet(L, pluginapi.Errf(pluginapi.CodeInvalid, "%s", rerr))
			}
			host().DecorClearRange(p.name, ns, id, r.From.Line, r.To.Line)
			L.Push(lua.LTrue)
			return 1
		}
		host().DecorClear(p.name, ns, id)
		L.Push(lua.LTrue)
		return 1
	})
	e.set(area, "refresh_lines", func(L *lua.LState) int {
		if L.Get(1) != lua.LNil {
			host().DecorRefreshLines(pluginapi.BufferID(checkID(L, 1, kindBuffer)))
		}
		L.Push(lua.LTrue)
		return 1
	})
	e.set(area, "add_fold", func(L *lua.LState) int {
		o := optsOf(L, 4)
		buf := pluginapi.BufferID(checkID(L, 1, kindBuffer))
		start := clampInt(float64(L.CheckNumber(2)))
		end := clampInt(float64(L.CheckNumber(3)))
		ns, err := defaultNS(L, &o)
		if err != nil {
			return bufRet(L, err)
		}
		fr := pluginapi.FoldRange{
			Range:       pluginapi.Range{From: pluginapi.Pos{Line: start}, To: pluginapi.Pos{Line: end}},
			Collapsed:   o.boolean("collapsed", false),
			Placeholder: clipText(optString(&o, "placeholder"), decorMaxVirtualText),
			Kind:        clipText(optString(&o, "fold_kind"), 64),
		}
		expect, verr := bufParseVersion(o.get("version"))
		if verr != nil {
			return bufRet(L, verr)
		}
		if o.err != "" {
			return bufRet(L, invalidErr("%s", o.err))
		}
		return bufTrue(L, host().DecorSetFoldingRanges(p.name, ns, buf, []pluginapi.FoldRange{fr}, expect))
	})
	e.set(area, "set_folding_ranges", func(L *lua.LState) int {
		o := optsOf(L, 3)
		buf := pluginapi.BufferID(checkID(L, 1, kindBuffer))
		list := L.CheckTable(2)
		n := min(list.Len(), maxPluginRangeCount)
		ranges := make([]pluginapi.FoldRange, 0, n)
		for i := 1; i <= n; i++ {
			row, ok := list.RawGetInt(i).(*lua.LTable)
			if !ok {
				continue
			}
			ro := bufTableOpts{t: row}
			fr := pluginapi.FoldRange{
				Range: pluginapi.Range{
					From: pluginapi.Pos{Line: ro.integer("start_line", 0)},
					To:   pluginapi.Pos{Line: ro.integer("end_line", 0)},
				},
				Collapsed:   ro.boolean("collapsed", false),
				Placeholder: clipText(optString(&ro, "placeholder"), decorMaxVirtualText),
				Kind:        clipText(optString(&ro, "kind"), 64),
			}
			ranges = append(ranges, fr)
		}
		ns, err := defaultNS(L, &o)
		if err != nil {
			return bufRet(L, err)
		}
		expect, verr := bufParseVersion(o.get("version"))
		if verr != nil {
			return bufRet(L, verr)
		}
		return bufTrue(L, host().DecorSetFoldingRanges(p.name, ns, buf, ranges, expect))
	})
	e.set(area, "clear_folds", func(L *lua.LState) int {
		o := optsOf(L, 2)
		ns, err := defaultNS(L, &o)
		if err != nil {
			return bufRet(L, err)
		}
		var buf pluginapi.BufferID
		if L.Get(1) != lua.LNil {
			buf = pluginapi.BufferID(checkID(L, 1, kindBuffer))
		}
		host().DecorClearFolds(p.name, ns, buf)
		L.Push(lua.LTrue)
		return 1
	})

}
