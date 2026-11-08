package pluginpanel

import (
	"github.com/tbxark/gvim/pluginapi"
)

// Input handling: the keys, the widget commands a plugin sends and the mouse events of the
// panel's rectangle. Everything runs on the UI goroutine and touches no plugin state; the
// root reports the events of a Result to the runtime.

// ev is a widget event without an index.
func (p *Panel) ev(name string) pluginapi.WidgetEvent {
	return pluginapi.WidgetEvent{Event: name, Index: -1}
}

// HandleKey handles one key press while the panel has the keyboard.
func (p *Panel) HandleKey(k Key) Result {
	if !p.focused || k.Ctrl || k.Alt {
		return Result{}
	}
	switch k.Name {
	case "tab":
		d := 1
		if k.Shift {
			d = -1
		}
		return p.moveFocus(d)
	case "escape":
		if p.closable {
			return Result{Handled: true, Close: true}
		}
		return Result{Handled: true}
	case "enter", "space":
		return p.activate()
	case "up":
		return p.moveSelection(-1)
	case "down":
		return p.moveSelection(1)
	case "pageup":
		return p.moveSelection(-max(p.lastVisibleRows, 1))
	case "pagedown":
		return p.moveSelection(max(p.lastVisibleRows, 1))
	case "home":
		return p.moveSelection(-1 << 30)
	case "end":
		return p.moveSelection(1 << 30)
	case "left":
		return p.foldFocused(false)
	case "right":
		return p.foldFocused(true)
	}
	return Result{}
}

// HandleAction performs a widget command a plugin sent (gvim.ui.widget_command).
func (p *Panel) HandleAction(a pluginapi.WidgetAction) Result {
	switch a.Kind {
	case "focusAdvance":
		return p.moveFocus(a.Delta)
	case "activate":
		return p.activate()
	case "selectMove":
		return p.moveSelection(a.Delta)
	}
	return Result{}
}

// activate activates the focused widget: a button reports it, a toggle flips, a list or a
// tree reports its selected row.
func (p *Panel) activate() Result {
	w, ok := p.focusedWidget()
	if !ok {
		return Result{Handled: true}
	}
	e := p.ev("activate")
	switch w.Kind {
	case pluginapi.WidgetButton:
		e.Widget = w.Key
	case pluginapi.WidgetToggle:
		v := !p.checked(w)
		p.setChecked(w, v)
		e.Event, e.Widget, e.Checked = "toggle", w.Key, v
	case pluginapi.WidgetList:
		sel := p.selected(w)
		if sel < 0 && len(w.Items) > 0 {
			sel = 0
			p.setSelected(w, sel)
		}
		if sel < 0 || sel >= len(w.Items) {
			return Result{Handled: true}
		}
		e.Widget, e.Index = w.Items[sel].Key, sel
		if e.Widget == "" {
			e.Widget = w.Key
		}
	case pluginapi.WidgetTree:
		sel := p.selected(w)
		if sel < 0 || sel >= len(w.Nodes) {
			return Result{Handled: true}
		}
		e.Widget, e.Index = w.Nodes[sel].Key, sel
	}
	return Result{Handled: true, Events: []pluginapi.WidgetEvent{e}}
}

// moveSelection moves the selection of the focused list or tree by delta items (or visible
// nodes, for a tree) and reports the new selection.
func (p *Panel) moveSelection(delta int) Result {
	w, ok := p.focusedWidget()
	if !ok {
		return Result{Handled: true}
	}
	switch w.Kind {
	case pluginapi.WidgetList:
		if len(w.Items) == 0 {
			return Result{Handled: true}
		}
		sel := p.selected(w)
		if sel < 0 {
			sel = 0
		}
		sel = clamp(sel+delta, 0, len(w.Items)-1)
		p.setSelected(w, sel)
		e := p.ev("select")
		e.Index = sel
		if it := w.Items[sel]; it.Key != "" {
			e.Widget = it.Key
		}
		return Result{Handled: true, Events: []pluginapi.WidgetEvent{e}}
	case pluginapi.WidgetTree:
		vis := p.visible(w)
		if len(vis) == 0 {
			return Result{Handled: true}
		}
		sel := p.selected(w)
		pos := -1
		for i, vn := range vis {
			if vn.index == sel {
				pos = i
				break
			}
		}
		if pos < 0 {
			pos = 0
		}
		pos = clamp(pos+delta, 0, len(vis)-1)
		p.setSelected(w, vis[pos].index)
		e := p.ev("select")
		e.Index = vis[pos].index
		e.Widget = vis[pos].node.Key
		return Result{Handled: true, Events: []pluginapi.WidgetEvent{e}}
	}
	return Result{Handled: true}
}

// foldFocused expands or collapses the focused tree's selected node.
func (p *Panel) foldFocused(expand bool) Result {
	w, ok := p.focusedWidget()
	if !ok || w.Kind != pluginapi.WidgetTree {
		return Result{Handled: true}
	}
	vis := p.visible(w)
	sel := p.selected(w)
	parent := -1
	for i, vn := range vis {
		if vn.index == sel {
			if vn.children {
				parent = i
			}
			break
		}
	}
	if parent < 0 {
		return Result{Handled: true}
	}
	vn := vis[parent]
	p.setExpanded(w, vn.node, expand)
	e := p.ev("value")
	e.Widget = vn.node.Key
	e.Value = "collapsed"
	if expand {
		e.Value = "expanded"
	}
	return Result{Handled: true, Events: []pluginapi.WidgetEvent{e}}
}

// HandleMouse handles one mouse message. It reports whether the pointer was inside the
// panel's rectangle; outside, the root routes the message to the frame.
func (p *Panel) HandleMouse(m Mouse) (Result, bool) {
	if !p.rect.Contains(m.X, m.Y) {
		return Result{}, false
	}
	if m.Button == "" && m.Wheel == 0 {
		id := m.Y<<20 | m.X
		if p.haveMouse && p.lastMouse == id {
			return Result{Handled: true}, true
		}
		p.lastMouse, p.haveMouse = id, true
		e := p.ev("mouse_move")
		e.X, e.Y = m.X, m.Y
		return Result{Handled: true, Events: []pluginapi.WidgetEvent{e}}, true
	}
	if m.Wheel != 0 {
		e := p.ev("mouse_wheel")
		e.X, e.Y, e.Wheel = m.X, m.Y, m.Wheel
		res := Result{Handled: true, Events: []pluginapi.WidgetEvent{e}}
		if h, ok := p.hitAt(m.X, m.Y); ok && (h.kind == hitListRow || h.kind == hitTreeRow) && h.focusIdx >= 0 {
			p.focus = h.focusIdx
			p.focused = true
			if sel := p.moveSelection(sign(m.Wheel) * 3); len(sel.Events) > 0 {
				res.Events = append(res.Events, sel.Events...)
			}
		}
		return res, true
	}
	e := p.ev("mouse_click")
	e.X, e.Y, e.Button = m.X, m.Y, m.Button
	res := Result{Handled: true, Events: []pluginapi.WidgetEvent{e}}
	if m.Button != "left" {
		return res, true
	}
	h, ok := p.hitAt(m.X, m.Y)
	if !ok {
		return res, true
	}
	if h.focusIdx >= 0 {
		p.focus = h.focusIdx
		p.focused = true
		res.Events = append(res.Events, p.focusEvent())
	}
	if h.disabled {
		return res, true
	}
	w, ok := widgetAt(p.spec.Root, h.path)
	if !ok {
		return res, true
	}
	switch h.kind {
	case hitButton:
		res.Events = append(res.Events, p.activate().Events...)
	case hitToggle:
		if fw, ok := p.focusedWidget(); ok && fw.Kind == pluginapi.WidgetToggle {
			res.Events = append(res.Events, p.activate().Events...)
		}
	case hitListRow:
		if w.Kind == pluginapi.WidgetList && h.index < len(w.Items) {
			p.setSelected(w, h.index)
			sel := p.ev("select")
			sel.Index = h.index
			sel.Widget = w.Items[h.index].Key
			res.Events = append(res.Events, sel)
		}
	case hitTreeRow:
		if w.Kind == pluginapi.WidgetTree && h.index < len(w.Nodes) {
			p.setSelected(w, h.index)
			sel := p.ev("select")
			sel.Index, sel.Widget = h.index, w.Nodes[h.index].Key
			res.Events = append(res.Events, sel)
		}
	case hitTreeDisclosure:
		if w.Kind == pluginapi.WidgetTree && h.index < len(w.Nodes) {
			p.setSelected(w, h.index)
			cur := false
			for _, vn := range p.visible(w) {
				if vn.index == h.index {
					cur = vn.expanded
					break
				}
			}
			p.setExpanded(w, w.Nodes[h.index], !cur)
			val := p.ev("value")
			val.Widget = w.Nodes[h.index].Key
			val.Value = "collapsed"
			if !cur {
				val.Value = "expanded"
			}
			res.Events = append(res.Events, val)
		}
	}
	return res, true
}

// Mutate applies a targeted mutation and reports whether the widget it names exists.
func (p *Panel) Mutate(m pluginapi.WidgetMutation) bool {
	w, ok := findWidget(p.spec.Root, m.WidgetKey)
	if !ok {
		return false
	}
	switch m.Kind {
	case "setChecked":
		if w.Kind == pluginapi.WidgetToggle {
			p.setChecked(w, m.Checked)
			return true
		}
	case "setSelectedIndex":
		if w.Kind == pluginapi.WidgetList || w.Kind == pluginapi.WidgetTree {
			max := len(w.Items) - 1
			if w.Kind == pluginapi.WidgetTree {
				max = len(w.Nodes) - 1
			}
			p.setSelected(w, clamp(m.Index, 0, max))
			return true
		}
	case "setValue":
		if in := p.instFor(w.Key); in != nil {
			in.value, in.haveValue = m.Value, true
			return true
		}
	}
	return false
}

// findWidget returns the widget with the key.
func findWidget(root pluginapi.Widget, key string) (pluginapi.Widget, bool) {
	if key == "" {
		return pluginapi.Widget{}, false
	}
	if root.Key == key {
		return root, true
	}
	for _, c := range root.Children {
		if w, ok := findWidget(c, key); ok {
			return w, true
		}
	}
	return pluginapi.Widget{}, false
}

// FocusEvent is the widget_event a mount or an explicit focus reports for a panel-level
// focus change: the key of the widget that starts focused.
func (p *Panel) FocusEvent() pluginapi.WidgetEvent { return p.focusEvent() }

func clamp(v, lo, hi int) int {
	if hi < lo {
		return lo
	}
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func sign(v int) int {
	switch {
	case v > 0:
		return 1
	case v < 0:
		return -1
	}
	return 0
}
