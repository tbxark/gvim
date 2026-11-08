package plugin

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"

	lua "github.com/yuin/gopher-lua"

	"github.com/tbxark/gvim/internal/pluginapi"
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
)

var decorKinds = map[string]pluginapi.DecorKind{
	"highlight": pluginapi.DecorHighlight,
	"line":      pluginapi.DecorLine,
	"eol":       pluginapi.DecorEOL,
	"gutter":    pluginapi.DecorGutter,
	"scroll":    pluginapi.DecorScroll,
	"replace":   pluginapi.DecorReplace,
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
	p.decorNS = names
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
}
