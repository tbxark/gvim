package plugin

import (
	"sync/atomic"

	lua "github.com/yuin/gopher-lua"

	"github.com/tbxark/gvim/pluginapi"
)

// gvim.ui declarative widget panels and sidebar sections (plugin API tier 1a). The spec is
// plain Lua data: a widget tree of the kinds in widgetKinds. The host validates and paints it, keeps the state of
// keyed widgets, routes the keys and the mouse of the panel and reports what the user did as
// widget_events.
//
//	gvim.ui.mount_widget_panel(spec, opts?) -> id | nil, err
//	gvim.ui.update_widget_panel(id, spec) -> true | nil, err
//	gvim.ui.unmount_widget_panel(id) -> true
//	gvim.ui.widget_command(id, action) -> true | nil, err
//	gvim.ui.widget_mutate(id, mutation) -> true | nil, err
//	gvim.ui.mount_sidebar_section(spec, opts?) -> id | nil, err
//
// A panel id lives until the panel is unmounted or the plugin unloads. The index of a list
// or tree selection is 1-based in Lua and 0-based in the host.

// panelSeq numbers the panels of all plugins; the host keys its panels by it.
var panelSeq atomic.Uint64

// Caps of the widget parser. The host checks the same numbers again (internal/tui/pluginpanel).
const (
	maxPanelDepth   = 8
	maxPanelWidgets = 2000
	maxPanelItems   = 2000
	maxPanelNodes   = 5000
	maxPanelString  = 4 << 10
	maxPanelKey     = 128
	maxPanelTitle   = 120
)

// installPanel installs the widget-panel functions of gvim.ui.
func installPanel(e *apiEnv) {
	p, ui := e.p, e.area("ui")

	e.set(ui, "mount_widget_panel", func(L *lua.LState) int {
		mount, err := panelMountFromLua(L, 1, 2, false)
		if err != nil {
			return bufRet(L, err)
		}
		id := pluginapi.PanelID(panelSeq.Add(1))
		if err := p.panelMount(id); err != nil {
			return bufRet(L, err)
		}
		if err := p.r.guard.MountWidgetPanel(p.name, id, mount); err != nil {
			p.panelUnmount(id)
			return bufRet(L, err)
		}
		L.Push(lua.LNumber(uint64(id)))
		return 1
	})

	e.set(ui, "mount_sidebar_section", func(L *lua.LState) int {
		mount, err := panelMountFromLua(L, 1, 2, true)
		if err != nil {
			return bufRet(L, err)
		}
		if mount.Title == "" {
			mount.Title = p.name
		}
		id := pluginapi.PanelID(panelSeq.Add(1))
		if err := p.panelMount(id); err != nil {
			return bufRet(L, err)
		}
		if err := p.r.guard.MountWidgetPanel(p.name, id, mount); err != nil {
			p.panelUnmount(id)
			return bufRet(L, err)
		}
		L.Push(lua.LNumber(uint64(id)))
		return 1
	})

	e.set(ui, "update_widget_panel", func(L *lua.LState) int {
		id := pluginapi.PanelID(panelIDFromLua(L, 1))
		spec, err := panelSpecFromLua(L.Get(2))
		if err != nil {
			return bufRet(L, err)
		}
		return bufTrue(L, p.r.guard.UpdateWidgetPanel(p.name, id, spec))
	})

	e.set(ui, "unmount_widget_panel", func(L *lua.LState) int {
		id := pluginapi.PanelID(panelIDFromLua(L, 1))
		if err := p.ownsPanel(id); err != nil {
			return bufRet(L, err)
		}
		p.r.guard.UnmountWidgetPanel(id)
		p.panelUnmount(id)
		L.Push(lua.LTrue)
		return 1
	})

	e.set(ui, "widget_command", func(L *lua.LState) int {
		id := pluginapi.PanelID(panelIDFromLua(L, 1))
		t, ok := L.Get(2).(*lua.LTable)
		if !ok {
			L.ArgError(2, "action table expected")
		}
		o := bufTableOpts{t: t}
		a := pluginapi.WidgetAction{
			Kind:  optString(&o, "kind"),
			Delta: o.integer("delta", 0),
			Key:   clipText(optString(&o, "key"), maxPanelKey),
			Text:  clipText(optString(&o, "text"), maxPanelString),
		}
		if o.err != "" {
			L.ArgError(2, o.err)
		}
		if err := p.ownsPanel(id); err != nil {
			return bufRet(L, err)
		}
		return bufTrue(L, p.r.guard.WidgetCommand(id, a))
	})

	e.set(ui, "widget_mutate", func(L *lua.LState) int {
		id := pluginapi.PanelID(panelIDFromLua(L, 1))
		t, ok := L.Get(2).(*lua.LTable)
		if !ok {
			L.ArgError(2, "mutation table expected")
		}
		o := bufTableOpts{t: t}
		mu := pluginapi.WidgetMutation{
			Kind:      optString(&o, "kind"),
			WidgetKey: clipText(optString(&o, "widget_key"), maxPanelKey),
			Checked:   o.boolean("checked", false),
			Index:     o.integer("index", 1) - 1, // 1-based in Lua
			Value:     clipText(optString(&o, "value"), maxPanelString),
			FocusKey:  clipText(optString(&o, "focus_key"), maxPanelKey),
			SourceKey: clipText(optString(&o, "source_key"), maxPanelKey),
			AfterKey:  clipText(optString(&o, "after_key"), maxPanelKey),
			Label:     clipText(optString(&o, "label"), maxPanelString),
			Commit:    o.boolean("commit", false),
			Number:    floatNumber(&o, "number", 0),
		}
		if c := o.get("cursor_byte"); c != lua.LNil {
			if n, ok := c.(lua.LNumber); ok {
				mu.CursorByte, mu.HasCursor = clampInt(float64(n)), true
			} else {
				o.fail("option 'cursor_byte' must be a number")
			}
		}
		if v := o.get("after_key"); v != lua.LNil {
			mu.HasAfter = true
		}
		for _, lit := range []struct {
			key string
			dst *[]string
		}{{"included", &mu.Included}, {"keys", &mu.Keys}, {"item_keys", &mu.ItemKeys}} {
			vals, err := stringArrayField(t, lit.key)
			if err != nil {
				return bufRet(L, err)
			}
			*lit.dst = vals
		}
		if raw := t.RawGetString("items"); raw != lua.LNil {
			list, ok := raw.(*lua.LTable)
			if !ok {
				return bufRet(L, invalidErr("mutation 'items' must be a table"))
			}
			items, err := widgetItemsFromLuaTable(list)
			if err != nil {
				return bufRet(L, err)
			}
			mu.Items = items
		}
		if raw := t.RawGetString("new_nodes"); raw != lua.LNil {
			list, ok := raw.(*lua.LTable)
			if !ok {
				return bufRet(L, invalidErr("mutation 'new_nodes' must be a table"))
			}
			nodes, err := widgetNodesFromLuaTable(list)
			if err != nil {
				return bufRet(L, err)
			}
			mu.NewNodes = nodes
		}
		if raw := t.RawGetString("entries"); raw != lua.LNil {
			list, ok := raw.(*lua.LTable)
			if !ok {
				return bufRet(L, invalidErr("mutation 'entries' must be a table"))
			}
			runs, err := widgetRunsFromLuaTable(list)
			if err != nil {
				return bufRet(L, err)
			}
			mu.Entries = runs
		}
		if o.err != "" {
			L.ArgError(2, o.err)
		}
		if err := p.ownsPanel(id); err != nil {
			return bufRet(L, err)
		}
		return bufTrue(L, p.r.guard.WidgetMutate(id, mu))
	})
}

// panelIDFromLua reads a panel id argument.
func panelIDFromLua(L *lua.LState, n int) uint64 {
	f := float64(L.CheckNumber(n))
	if f < 0 || f != float64(uint64(f)) {
		L.ArgError(n, "a panel id is a positive integer")
	}
	return uint64(f)
}

// panelMountFromLua reads the spec and the options of a mount call.
func panelMountFromLua(L *lua.LState, specIdx, optsIdx int, sidebar bool) (pluginapi.WidgetMount, error) {
	spec, err := panelSpecFromLua(L.Get(specIdx))
	if err != nil {
		return pluginapi.WidgetMount{}, err
	}
	t, ok := luaOptTable(L, optsIdx)
	if !ok {
		return pluginapi.WidgetMount{}, invalidErr("options: table expected")
	}
	o := bufTableOpts{t: t}
	m := pluginapi.WidgetMount{Panel: spec, Closable: o.boolean("closable", true)}
	switch scope, _ := o.str("scope"); scope {
	case "", "window":
		m.Scope = pluginapi.ScopeWindow
	case "editor":
		m.Scope = pluginapi.ScopeEditor
	case "buffer":
		m.Scope = pluginapi.ScopeBuffer
	default:
		return m, invalidErr("option 'scope' must be \"window\", \"editor\" or \"buffer\"")
	}
	m.Title = clipText(optString(&o, "title"), maxPanelTitle)
	m.Focus = o.boolean("focus", false)
	m.WidthPct, m.HeightPct = o.integer("width", 0), o.integer("height", 0)
	m.Page = o.boolean("page", false)
	if v := o.get("auto_focus_first"); v != lua.LNil {
		on := o.boolean("auto_focus_first", true)
		m.AutoFocusFirst = &on
	}
	if sidebar {
		m.Placement = pluginapi.PlacementSidebar
		m.Rows = o.integer("rows", 0)
		if m.Scope == pluginapi.ScopeBuffer {
			if id, ok := bufferIDFromLua(o.get("buf")); ok {
				m.Buffer = id
			} else if o.get("buf") != lua.LNil {
				return m, invalidErr("option 'buf' must be a Buffer")
			}
		}
		if o.err != "" {
			return m, invalidErr("%s", o.err)
		}
		return m, nil
	}
	placement := "float"
	if s, ok := o.str("placement"); ok {
		placement = s
	}
	switch placement {
	case "float":
		m.Placement = pluginapi.PlacementFloat
	case "buffer":
		m.Placement = pluginapi.PlacementBuffer
		id, ok := bufferIDFromLua(o.get("buf"))
		if !ok {
			return m, invalidErr("a buffer panel needs option 'buf' with a Buffer")
		}
		m.Buffer = id
	default:
		return m, invalidErr("option 'placement' must be \"float\" or \"buffer\"")
	}
	if o.err != "" {
		return m, invalidErr("%s", o.err)
	}
	return m, nil
}

// luaOptTable returns the optional table at argument n.
func luaOptTable(L *lua.LState, n int) (*lua.LTable, bool) {
	v := L.Get(n)
	if v == lua.LNil {
		return nil, true
	}
	t, ok := v.(*lua.LTable)
	return t, ok
}

// panelSpecFromLua reads a panel spec: either a widget tree directly (it has "kind") or a
// table {root = <widget>, focus_key = "key"}.
func panelSpecFromLua(v lua.LValue) (pluginapi.WidgetPanel, error) {
	t, ok := v.(*lua.LTable)
	if !ok {
		return pluginapi.WidgetPanel{}, invalidErr("a panel spec is a table, got %s", v.Type())
	}
	if _, ok := t.RawGetString("kind").(lua.LString); ok {
		n := 0
		root, err := widgetFromLua(t, 0, &n)
		if err != nil {
			return pluginapi.WidgetPanel{}, err
		}
		return pluginapi.WidgetPanel{Root: root}, nil
	}
	rv := t.RawGetString("root")
	if rv == lua.LNil {
		return pluginapi.WidgetPanel{}, invalidErr("a panel spec needs 'root' or a 'kind'")
	}
	n := 0
	root, err := widgetFromLua(rv, 0, &n)
	if err != nil {
		return pluginapi.WidgetPanel{}, err
	}
	p := pluginapi.WidgetPanel{Root: root}
	if fk, ok := t.RawGetString("focus_key").(lua.LString); ok {
		p.FocusKey = clipText(string(fk), maxPanelKey)
	}
	return p, nil
}

// widgetKinds are the widget kinds a spec may use.
var widgetKinds = map[string]pluginapi.WidgetKind{
	"row":     pluginapi.WidgetRow,
	"col":     pluginapi.WidgetCol,
	"label":   pluginapi.WidgetLabel,
	"text":    pluginapi.WidgetText,
	"raw":     pluginapi.WidgetRaw,
	"hintBar": pluginapi.WidgetHintBar,
	"spacer":  pluginapi.WidgetSpacer,
	"divider": pluginapi.WidgetDivider,
	"button":  pluginapi.WidgetButton,
	"toggle":  pluginapi.WidgetToggle,
	"list":    pluginapi.WidgetList,
	"tree":    pluginapi.WidgetTree,

	"dropdown":       pluginapi.WidgetDropdown,
	"number":         pluginapi.WidgetNumber,
	"radio":          pluginapi.WidgetRadio,
	"dualList":       pluginapi.WidgetDualList,
	"labeledSection": pluginapi.WidgetLabeledSection,
	"component":      pluginapi.WidgetComponent,
	"overlay":        pluginapi.WidgetOverlay,
	"popup":          pluginapi.WidgetPopup,
	"windowEmbed":    pluginapi.WidgetWindowEmbed,
}

// widgetFromLua reads one widget node. n counts the nodes of the whole spec.
func widgetFromLua(v lua.LValue, depth int, n *int) (pluginapi.Widget, error) {
	var w pluginapi.Widget
	t, ok := v.(*lua.LTable)
	if !ok {
		return w, invalidErr("a widget is a table, got %s", v.Type())
	}
	*n++
	if *n > maxPanelWidgets {
		return w, pluginapi.Errf(pluginapi.CodeLimit, "a panel takes at most %d widgets", maxPanelWidgets)
	}
	if depth > maxPanelDepth {
		return w, pluginapi.Errf(pluginapi.CodeLimit, "widget nesting is deeper than %d", maxPanelDepth)
	}
	kindName, ok := t.RawGetString("kind").(lua.LString)
	if !ok {
		return w, invalidErr("a widget needs a string 'kind'")
	}
	kind, ok := widgetKinds[string(kindName)]
	if !ok {
		return w, invalidErr("unknown widget kind %q", string(kindName))
	}
	w.Kind = kind
	o := bufTableOpts{t: t}
	if k, ok := o.str("key"); ok {
		if len(k) > maxPanelKey {
			return w, pluginapi.Errf(pluginapi.CodeLimit, "a widget key is longer than %d bytes", maxPanelKey)
		}
		w.Key = k
	}
	if err := widgetTextFromLua(t, &w); err != nil {
		return w, err
	}
	w.Checked = o.boolean("checked", false)
	w.Indeterminate = o.boolean("indeterminate", false)
	w.Focused = o.boolean("focused", false)
	w.Disabled = o.boolean("disabled", false)
	w.Checkable = o.boolean("checkable", false)
	w.Flex = o.boolean("flex", false)
	w.VisibleRows = o.integer("visible_rows", 0)
	w.Columns = o.integer("cols", 1)
	if intent, ok := o.str("intent"); ok {
		w.Intent = intent
	}
	w.Placeholder = clipText(optString(&o, "placeholder"), maxPanelString)
	w.FieldWidth = o.integer("field_width", 0)
	w.FullWidth = o.boolean("full_width", false)
	w.Rows = o.integer("rows", 0)
	w.MinRows = o.integer("min_rows", 0)
	w.MaxRows = o.integer("max_rows", 0)
	w.ReadOnly = o.boolean("read_only", false)
	w.Markdown = o.boolean("markdown", false)
	w.Combo = o.boolean("combo", false)
	w.Integer = o.boolean("integer", false)
	w.Percent = o.boolean("percent", false)
	w.Open = o.boolean("open", false)
	w.ScrollOffset = o.integer("scroll_offset", 0)
	if _, isText := o.get("value").(lua.LString); !isText { // a string value is the text seed
		w.Number = floatNumber(&o, "value", 0)
	}
	w.Min = floatNumber(&o, "min", 0)
	w.Max = floatNumber(&o, "max", 0)
	w.Step = floatNumber(&o, "step", 0)
	w.Title = clipText(optString(&o, "title"), maxPanelString)
	w.WidthPct = o.integer("width_pct", 0)
	w.WidthCols = o.integer("width_cols", 0)
	if win := o.integer("window", 0); win > 0 {
		w.Window = pluginapi.WinID(win)
	}
	if o.err != "" {
		return w, invalidErr("%s", o.err)
	}
	if err := widgetListFields(t, &w); err != nil {
		return w, err
	}
	switch kind {
	case pluginapi.WidgetRow, pluginapi.WidgetCol, pluginapi.WidgetLabeledSection,
		pluginapi.WidgetComponent, pluginapi.WidgetOverlay, pluginapi.WidgetPopup:
		children, err := widgetChildrenFromLua(t)
		if err != nil {
			return w, err
		}
		for _, c := range children {
			child, err := widgetFromLua(c, depth+1, n)
			if err != nil {
				return w, err
			}
			w.Children = append(w.Children, child)
		}
	case pluginapi.WidgetList:
		items, err := widgetItemsFromLua(t)
		if err != nil {
			return w, err
		}
		w.Items = items
		sel, err := selectedFromLua(&o, len(items))
		if err != nil {
			return w, err
		}
		w.Selected = sel
	case pluginapi.WidgetTree:
		nodes, err := widgetNodesFromLua(t)
		if err != nil {
			return w, err
		}
		w.Nodes = nodes
		sel, err := selectedFromLua(&o, len(nodes))
		if err != nil {
			return w, err
		}
		w.Selected = sel
	}
	return w, nil
}

// widgetTextFromLua reads the text-ish fields: text/value and the styled runs/entries.
func widgetTextFromLua(t *lua.LTable, w *pluginapi.Widget) error {
	o := bufTableOpts{t: t}
	text, ok := o.str("text")
	if !ok {
		// "value" is the text field's content; the number widget's value is a number and is
		// read by the widget parser itself, so only a string is taken here.
		if v, isStr := o.get("value").(lua.LString); isStr {
			text = string(v)
		}
	}
	if len(text) > maxPanelString {
		return pluginapi.Errf(pluginapi.CodeLimit, "widget text is longer than %d bytes", maxPanelString)
	}
	w.Text = text
	if label, ok := o.str("label"); ok {
		if len(label) > maxPanelString {
			return pluginapi.Errf(pluginapi.CodeLimit, "a label is longer than %d bytes", maxPanelString)
		}
		w.Label = label
	}
	if o.err != "" {
		return invalidErr("%s", o.err)
	}
	if style, ok := t.RawGetString("style").(*lua.LTable); ok {
		st, err := styleFromLua(style)
		if err != nil {
			return invalidErr("style: %s", err)
		}
		w.Style = st
	}
	runs := t.RawGetString("runs")
	if runs == lua.LNil {
		runs = t.RawGetString("entries")
	}
	if runs == lua.LNil {
		runs = t.RawGetString("segments")
	}
	if runs == lua.LNil {
		return nil
	}
	list, ok := runs.(*lua.LTable)
	if !ok {
		return invalidErr("runs/entries must be a table, got %s", runs.Type())
	}
	for i := 1; i <= list.Len(); i++ {
		run, err := widgetRunFromLua(list.RawGetInt(i))
		if err != nil {
			return invalidErr("run %d: %s", i, err)
		}
		w.Runs = append(w.Runs, run)
	}
	return nil
}

// widgetRunFromLua reads one styled run: a string or a table {text =, fg =, bg =, bold =, ...}.
func widgetRunFromLua(v lua.LValue) (pluginapi.WidgetRun, error) {
	var r pluginapi.WidgetRun
	switch x := v.(type) {
	case lua.LString:
		r.Text = clipText(string(x), maxPanelString)
		return r, nil
	case *lua.LTable:
		o := bufTableOpts{t: x}
		text := optString(&o, "text")
		if text == "" {
			text = optString(&o, "label")
		}
		if len(text) > maxPanelString {
			return r, pluginapi.Errf(pluginapi.CodeLimit, "a run is longer than %d bytes", maxPanelString)
		}
		st, err := styleFromLua(x)
		if err != nil {
			return r, err
		}
		if o.err != "" {
			return r, invalidErr("%s", o.err)
		}
		r.Text, r.Style = text, st
		return r, nil
	}
	return r, invalidErr("a run is a string or a table, got %s", v.Type())
}

// widgetChildrenFromLua reads a children array.
func widgetChildrenFromLua(t *lua.LTable) ([]lua.LValue, error) {
	vt := t.RawGetString("children")
	if vt == lua.LNil {
		return nil, nil
	}
	list, ok := vt.(*lua.LTable)
	if !ok {
		return nil, invalidErr("children must be a table, got %s", vt.Type())
	}
	out := make([]lua.LValue, 0, list.Len())
	for i := 1; i <= list.Len(); i++ {
		out = append(out, list.RawGetInt(i))
	}
	return out, nil
}

// widgetItemsFromLua reads the items of a list.
func widgetItemsFromLua(t *lua.LTable) ([]pluginapi.WidgetItem, error) {
	vt := t.RawGetString("items")
	if vt == lua.LNil {
		return nil, nil
	}
	list, ok := vt.(*lua.LTable)
	if !ok {
		return nil, invalidErr("items must be a table, got %s", vt.Type())
	}
	return widgetItemsFromLuaTable(list)
}

// widgetItemsFromLuaTable reads a list's item array directly.
func widgetItemsFromLuaTable(list *lua.LTable) ([]pluginapi.WidgetItem, error) {
	if list.Len() > maxPanelItems {
		return nil, pluginapi.Errf(pluginapi.CodeLimit, "a list takes at most %d items", maxPanelItems)
	}
	out := make([]pluginapi.WidgetItem, 0, list.Len())
	for i := 1; i <= list.Len(); i++ {
		v := list.RawGetInt(i)
		it := pluginapi.WidgetItem{}
		switch x := v.(type) {
		case lua.LString:
			it.Text = clipText(string(x), maxPanelString)
		case lua.LNumber:
			it.Text = x.String()
		case *lua.LTable:
			o := bufTableOpts{t: x}
			it.Text = optString(&o, "text")
			it.Detail = optString(&o, "detail")
			it.Key = clipText(optString(&o, "key"), maxPanelKey)
			it.Checked = o.boolean("checked", false)
			it.Disabled = o.boolean("disabled", false)
			if o.err != "" {
				return nil, invalidErr("item %d: %s", i, o.err)
			}
		default:
			return nil, invalidErr("item %d must be a string or a table", i)
		}
		if len(it.Text) > maxPanelString || len(it.Detail) > maxPanelString {
			return nil, pluginapi.Errf(pluginapi.CodeLimit, "item %d is too long", i)
		}
		out = append(out, it)
	}
	return out, nil
}

// widgetNodesFromLua reads the nodes of a tree.
func widgetNodesFromLua(t *lua.LTable) ([]pluginapi.WidgetNode, error) {
	vt := t.RawGetString("nodes")
	if vt == lua.LNil {
		return nil, nil
	}
	list, ok := vt.(*lua.LTable)
	if !ok {
		return nil, invalidErr("nodes must be a table, got %s", vt.Type())
	}
	return widgetNodesFromLuaTable(list)
}

// widgetNodesFromLuaTable reads a tree's node array directly.
func widgetNodesFromLuaTable(list *lua.LTable) ([]pluginapi.WidgetNode, error) {
	if list.Len() > maxPanelNodes {
		return nil, pluginapi.Errf(pluginapi.CodeLimit, "a tree takes at most %d nodes", maxPanelNodes)
	}
	out := make([]pluginapi.WidgetNode, 0, list.Len())
	for i := 1; i <= list.Len(); i++ {
		x, ok := list.RawGetInt(i).(*lua.LTable)
		if !ok {
			return nil, invalidErr("node %d must be a table", i)
		}
		o := bufTableOpts{t: x}
		nd := pluginapi.WidgetNode{
			Text:      optString(&o, "text"),
			Detail:    optString(&o, "detail"),
			Key:       clipText(optString(&o, "key"), maxPanelKey),
			Depth:     o.integer("depth", 0),
			Checked:   o.boolean("checked", false),
			Expanded:  o.boolean("expanded", false),
			Leaf:      o.boolean("leaf", false),
			Draggable: o.boolean("draggable", false),
			Flush:     o.boolean("flush", false),
		}
		if ev := x.RawGetString("extra_lines"); ev != lua.LNil {
			list, ok := ev.(*lua.LTable)
			if !ok {
				return nil, invalidErr("node %d: extra_lines must be a table", i)
			}
			for j := 1; j <= list.Len() && j <= maxPanelItems; j++ {
				run, err := widgetRunFromLua(list.RawGetInt(j))
				if err != nil {
					return nil, invalidErr("node %d: extra_lines: %s", i, err)
				}
				nd.ExtraLines = append(nd.ExtraLines, run)
			}
		}
		if o.err != "" {
			return nil, invalidErr("node %d: %s", i, o.err)
		}
		if len(nd.Text) > maxPanelString || len(nd.Detail) > maxPanelString || nd.Depth < 0 || nd.Depth > maxPanelDepth {
			return nil, pluginapi.Errf(pluginapi.CodeInvalid, "node %d is malformed", i)
		}
		out = append(out, nd)
	}
	return out, nil
}

// widgetListFields reads the options, completions and included lists.
func widgetListFields(t *lua.LTable, w *pluginapi.Widget) error {
	var err error
	if w.Options, err = stringArrayField(t, "options"); err != nil {
		return err
	}
	if w.Completions, err = stringArrayField(t, "completions"); err != nil {
		return err
	}
	if w.Included, err = stringArrayField(t, "included"); err != nil {
		return err
	}
	return nil
}

// stringArrayField reads an optional array of strings, clipped to the text cap.
func stringArrayField(t *lua.LTable, name string) ([]string, error) {
	v := t.RawGetString(name)
	if v == lua.LNil {
		return nil, nil
	}
	list, ok := v.(*lua.LTable)
	if !ok {
		return nil, invalidErr("option %q: table expected", name)
	}
	if list.Len() > maxPanelItems {
		return nil, pluginapi.Errf(pluginapi.CodeLimit, "option %q takes at most %d values", name, maxPanelItems)
	}
	out := make([]string, 0, list.Len())
	for i := 1; i <= list.Len(); i++ {
		s, ok := list.RawGetInt(i).(lua.LString)
		if !ok {
			return nil, invalidErr("option %q: strings expected", name)
		}
		out = append(out, clipText(string(s), maxPanelString))
	}
	return out, nil
}

// floatNumber reads an optional number option.
func floatNumber(o *bufTableOpts, key string, def float64) float64 {
	switch v := o.get(key).(type) {
	case *lua.LNilType:
		return def
	case lua.LNumber:
		return float64(v)
	}
	o.fail("option '" + key + "' must be a number")
	return def
}

// widgetRunsFromLuaTable reads an array of styled runs.
func widgetRunsFromLuaTable(list *lua.LTable) ([]pluginapi.WidgetRun, error) {
	if list.Len() > maxPanelItems {
		return nil, pluginapi.Errf(pluginapi.CodeLimit, "at most %d entries", maxPanelItems)
	}
	out := make([]pluginapi.WidgetRun, 0, list.Len())
	for i := 1; i <= list.Len(); i++ {
		run, err := widgetRunFromLua(list.RawGetInt(i))
		if err != nil {
			return nil, invalidErr("entry %d: %s", i, err)
		}
		out = append(out, run)
	}
	return out, nil
}

// selectedFromLua reads the 1-based selected_index; 0 or absent means none (-1).
func selectedFromLua(o *bufTableOpts, n int) (int, error) {
	sel := o.integer("selected_index", 0)
	if o.err != "" {
		return -1, invalidErr("%s", o.err)
	}
	if sel <= 0 {
		return -1, nil
	}
	if sel > n {
		return -1, invalidErr("selected_index %d is outside the %d rows", sel, n)
	}
	return sel - 1, nil
}

// panelMount records a mounted panel, enforcing the quota.
func (p *plugin) panelMount(id pluginapi.PanelID) error {
	if p.panels == nil {
		p.panels = map[pluginapi.PanelID]bool{}
	}
	if len(p.panels) >= QuotaPanels {
		return pluginapi.Errf(pluginapi.CodeLimit, "panel quota (%d)", QuotaPanels)
	}
	p.panels[id] = true
	return nil
}

// panelUnmount forgets a panel the plugin unmounted.
func (p *plugin) panelUnmount(id pluginapi.PanelID) {
	delete(p.panels, id)
}

// ownsPanel refuses a panel id the plugin did not mount: panel ids are global.
func (p *plugin) ownsPanel(id pluginapi.PanelID) error {
	if !p.panels[id] {
		return pluginapi.Errf(pluginapi.CodeClosed, "panel %d is not mounted by this plugin", id)
	}
	return nil
}

// bufferIDFromLua reads a Buffer handle option; 0, false when there is none.
func bufferIDFromLua(v lua.LValue) (pluginapi.BufferID, bool) {
	switch x := v.(type) {
	case *lua.LNilType:
		return 0, false
	case *lua.LUserData:
		if h, ok := x.Value.(idHandle); ok && h.kind == kindBuffer {
			return pluginapi.BufferID(h.id), true
		}
	}
	return 0, false
}
