package pluginpanel

import (
	"strings"
	"unicode/utf8"

	"github.com/charmbracelet/x/ansi"

	"github.com/tbxark/gvim/internal/tui/space"
	"github.com/tbxark/gvim/pluginapi"
)

// Input handling: the keys, the widget commands a plugin sends and the mouse events of the
// panel's rectangle. Everything runs on the UI goroutine and touches no plugin state; the
// root reports the events of a Result to the runtime.
//
// The host smart dispatch is the fallback after the plugin's own key layers (modes and key
// waits) have seen the key: Tab/Shift+Tab advance focus, Enter/Space activate, the arrows
// move a selection or scroll a page, and a focused text/number/dropdown/radio/dual list
// handles its own editing keys and reports a change widget_event.

// ev is a widget event without an index.
func (p *Panel) ev(name string) pluginapi.WidgetEvent {
	return pluginapi.WidgetEvent{Event: name, Index: -1, CursorByte: -1, TargetIndex: -1}
}

// HandleKey handles one key press while the panel has the keyboard.
func (p *Panel) HandleKey(k Key) Result {
	if !p.focused {
		return Result{}
	}
	w, haveFocused := p.focusedWidget()
	// An open completion list takes the navigation and acceptance keys first.
	if haveFocused && w.Kind == pluginapi.WidgetText && p.completionsOpen(w) {
		switch k.Name {
		case "up":
			p.cycleCompletion(w, -1)
			return Result{Handled: true}
		case "down":
			p.cycleCompletion(w, 1)
			return Result{Handled: true}
		case "tab", "enter":
			return p.acceptCompletion(w)
		case "escape":
			p.setCompletionsOpen(w, false)
			e := evNamed("completion_dismiss", w.Key)
			return Result{Handled: true, Events: []pluginapi.WidgetEvent{e}}
		}
		if k.Text != "" && !k.Ctrl && !k.Alt {
			p.setCompletionsOpen(w, false)
		}
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
	}
	if !haveFocused {
		if res, ok := p.pageScrollKey(k); ok {
			return res
		}
		return Result{Handled: true}
	}
	if k.Ctrl || k.Alt {
		return Result{}
	}
	switch w.Kind {
	case pluginapi.WidgetText:
		if res, ok := p.editFocusedText(w, k); ok {
			return res
		}
	case pluginapi.WidgetNumber:
		if res, ok := p.numberKey(w, k); ok {
			return res
		}
	case pluginapi.WidgetDropdown:
		if res, ok := p.dropdownKey(w, k); ok {
			return res
		}
	case pluginapi.WidgetRadio:
		if res, ok := p.radioKey(w, k); ok {
			return res
		}
	case pluginapi.WidgetDualList:
		if res, ok := p.dualKey(w, k); ok {
			return res
		}
	case pluginapi.WidgetButton, pluginapi.WidgetToggle:
		if k.Name == "enter" || k.Name == "space" {
			return p.activate()
		}
	case pluginapi.WidgetList:
		switch k.Name {
		case "enter", "space":
			return p.activate()
		}
		return p.moveSelection(keyDelta(k, p.lastVisibleRows))
	case pluginapi.WidgetTree:
		switch k.Name {
		case "enter", "space":
			return p.activate()
		case "left":
			return p.foldFocused(false)
		case "right":
			return p.foldFocused(true)
		}
		return p.moveSelection(keyDelta(k, p.lastVisibleRows))
	}
	if res, ok := p.pageScrollKey(k); ok {
		return res
	}
	return Result{Handled: true}
}

// keyDelta maps a movement key to a selection delta.
func keyDelta(k Key, rows int) int {
	switch k.Name {
	case "up":
		return -1
	case "down":
		return 1
	case "pageup":
		return -max(rows, 1)
	case "pagedown":
		return max(rows, 1)
	case "home":
		return -1 << 30
	case "end":
		return 1 << 30
	}
	return 0
}

// numberKey steps the focused number control and reports the change.
func (p *Panel) numberKey(w pluginapi.Widget, k Key) (Result, bool) {
	step := w.Step
	if step == 0 {
		step = 1
	}
	v := p.numberValue(w)
	switch k.Name {
	case "up":
		v += step
	case "down":
		v -= step
	case "pageup":
		v += step * 10
	case "pagedown":
		v -= step * 10
	case "home":
		v = w.Min
	case "end":
		if w.Max != 0 {
			v = w.Max
		}
	case "enter", "space":
		return Result{Handled: true, Events: []pluginapi.WidgetEvent{p.evInteractive("activate", w)}}, true
	default:
		return Result{}, false
	}
	p.setNumber(w, v)
	return Result{Handled: true, Events: []pluginapi.WidgetEvent{p.evInteractive("change", w)}}, true
}

// dropdownKey handles the focused dropdown: Enter opens or chooses, arrows change the value
// when closed and move the highlight when open, Escape closes.
func (p *Panel) dropdownKey(w pluginapi.Widget, k Key) (Result, bool) {
	open := p.dropdownOpen(w)
	switch k.Name {
	case "enter", "space":
		if open {
			p.setDropdownOpen(w, false)
			return Result{Handled: true, Events: []pluginapi.WidgetEvent{p.changeOption(w, p.optionIndex(w))}}, true
		}
		p.setDropdownOpen(w, true)
		return Result{Handled: true}, true
	case "escape":
		if open {
			p.setDropdownOpen(w, false)
			return Result{Handled: true}, true
		}
		return Result{}, false
	case "up":
		if open {
			p.cycleOption(w, -1)
			return Result{Handled: true}, true
		}
		return Result{Handled: true, Events: []pluginapi.WidgetEvent{p.changeOption(w, p.optionIndex(w)-1)}}, true
	case "down":
		if open {
			p.cycleOption(w, 1)
			return Result{Handled: true}, true
		}
		return Result{Handled: true, Events: []pluginapi.WidgetEvent{p.changeOption(w, p.optionIndex(w)+1)}}, true
	case "left", "home":
		return Result{Handled: true, Events: []pluginapi.WidgetEvent{p.changeOption(w, 0)}}, true
	case "right", "end":
		return Result{Handled: true, Events: []pluginapi.WidgetEvent{p.changeOption(w, len(w.Options)-1)}}, true
	}
	return Result{}, false
}

// radioKey moves the focused radio group's selection, which reports a change at once.
func (p *Panel) radioKey(w pluginapi.Widget, k Key) (Result, bool) {
	switch k.Name {
	case "up", "left":
		return Result{Handled: true, Events: []pluginapi.WidgetEvent{p.changeOption(w, p.optionIndex(w)-1)}}, true
	case "down", "right":
		return Result{Handled: true, Events: []pluginapi.WidgetEvent{p.changeOption(w, p.optionIndex(w)+1)}}, true
	case "home":
		return Result{Handled: true, Events: []pluginapi.WidgetEvent{p.changeOption(w, 0)}}, true
	case "end":
		return Result{Handled: true, Events: []pluginapi.WidgetEvent{p.changeOption(w, len(w.Options)-1)}}, true
	case "enter", "space":
		return Result{Handled: true, Events: []pluginapi.WidgetEvent{p.evInteractive("activate", w)}}, true
	}
	return Result{}, false
}

// dualKey moves the focused dual list's cursor and toggles inclusion with Space/Enter.
func (p *Panel) dualKey(w pluginapi.Widget, k Key) (Result, bool) {
	sel := p.selected(w)
	if sel < 0 {
		sel = 0
	}
	switch k.Name {
	case "up":
		p.setSelected(w, space.Clamp(sel-1, 0, max(len(w.Options)-1, 0)))
		return Result{Handled: true}, true
	case "down":
		p.setSelected(w, space.Clamp(sel+1, 0, max(len(w.Options)-1, 0)))
		return Result{Handled: true}, true
	case "space", "enter":
		if len(w.Options) == 0 {
			return Result{Handled: true}, true
		}
		return Result{Handled: true, Events: []pluginapi.WidgetEvent{p.toggleIncluded(w, w.Options[space.Clamp(sel, 0, len(w.Options)-1)])}}, true
	}
	return Result{}, false
}

// cycleOption moves the dropdown highlight without changing the value.
func (p *Panel) cycleOption(w pluginapi.Widget, delta int) {
	p.setSelected(w, space.Clamp(p.optionIndex(w)+delta, 0, max(len(w.Options)-1, 0)))
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
	case "textInputChar":
		if w, ok := p.focusedWidget(); ok && w.Kind == pluginapi.WidgetText {
			p.insertText(w, a.Text)
			return Result{Handled: true, Events: []pluginapi.WidgetEvent{p.evInteractive("change", w)}}
		}
	case "textInputKey":
		if w, ok := p.focusedWidget(); ok && w.Kind == pluginapi.WidgetText {
			if res, ok := p.editFocusedText(w, Key{Name: a.Key}); ok {
				return res
			}
		}
	case "key":
		if w, ok := p.focusedWidget(); ok {
			switch w.Kind {
			case pluginapi.WidgetText:
				if res, ok := p.editFocusedText(w, Key{Name: a.Key}); ok {
					return res
				}
			case pluginapi.WidgetDropdown:
				if res, ok := p.dropdownKey(w, Key{Name: a.Key}); ok {
					return res
				}
			case pluginapi.WidgetNumber:
				if res, ok := p.numberKey(w, Key{Name: a.Key}); ok {
					return res
				}
			case pluginapi.WidgetRadio:
				if res, ok := p.radioKey(w, Key{Name: a.Key}); ok {
					return res
				}
			case pluginapi.WidgetDualList:
				if res, ok := p.dualKey(w, Key{Name: a.Key}); ok {
					return res
				}
			}
		}
	}
	return Result{Handled: true}
}

// activate activates the focused widget: a button reports it, a toggle flips, a dropdown
// opens or chooses, a text field reports it, and a list or tree reports its selected row.
func (p *Panel) activate() Result {
	w, ok := p.focusedWidget()
	if !ok {
		return Result{Handled: true}
	}
	e := p.ev("activate")
	e.Widget = w.Key
	switch w.Kind {
	case pluginapi.WidgetButton:
	case pluginapi.WidgetToggle:
		v := !p.checked(w)
		p.setChecked(w, v)
		e.Event, e.Checked = "toggle", v
	case pluginapi.WidgetDropdown:
		if p.dropdownOpen(w) {
			p.setDropdownOpen(w, false)
			return Result{Handled: true, Events: []pluginapi.WidgetEvent{p.changeOption(w, p.optionIndex(w))}}
		}
		p.setDropdownOpen(w, true)
		return Result{Handled: true}
	case pluginapi.WidgetRadio, pluginapi.WidgetNumber, pluginapi.WidgetText:
	case pluginapi.WidgetList:
		items := p.listItems(w)
		sel := p.selected(w)
		if sel < 0 && len(items) > 0 {
			sel = 0
			p.setSelected(w, sel)
		}
		if sel < 0 || sel >= len(items) {
			return Result{Handled: true}
		}
		e.Widget, e.Index = items[sel].Key, sel
		if e.Widget == "" {
			e.Widget = w.Key
		}
	case pluginapi.WidgetTree:
		nodes := p.treeNodes(w)
		sel := p.selected(w)
		if sel < 0 || sel >= len(nodes) {
			return Result{Handled: true}
		}
		e.Widget, e.Index = nodes[sel].Key, sel
	}
	return Result{Handled: true, Events: []pluginapi.WidgetEvent{e}}
}

// moveSelection moves the selection of the focused list, tree, dropdown, radio or dual list
// by delta rows and reports the new selection.
func (p *Panel) moveSelection(delta int) Result {
	w, ok := p.focusedWidget()
	if !ok {
		return Result{Handled: true}
	}
	switch w.Kind {
	case pluginapi.WidgetList:
		items := p.listItems(w)
		if len(items) == 0 {
			return Result{Handled: true}
		}
		sel := p.selected(w)
		if sel < 0 {
			sel = 0
		}
		sel = space.Clamp(sel+delta, 0, len(items)-1)
		p.setSelected(w, sel)
		e := p.ev("select")
		e.Index = sel
		if it := items[sel]; it.Key != "" {
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
		pos = space.Clamp(pos+delta, 0, len(vis)-1)
		p.setSelected(w, vis[pos].index)
		e := p.ev("select")
		e.Index = vis[pos].index
		e.Widget = vis[pos].node.Key
		return Result{Handled: true, Events: []pluginapi.WidgetEvent{e}}
	case pluginapi.WidgetDropdown:
		return Result{Handled: true, Events: []pluginapi.WidgetEvent{p.changeOption(w, p.optionIndex(w)+sign(delta))}}
	case pluginapi.WidgetRadio:
		return Result{Handled: true, Events: []pluginapi.WidgetEvent{p.changeOption(w, p.optionIndex(w)+sign(delta))}}
	case pluginapi.WidgetDualList:
		sel := p.selected(w)
		if sel < 0 {
			sel = 0
		}
		p.setSelected(w, space.Clamp(sel+sign(delta), 0, max(len(w.Options)-1, 0)))
		return Result{Handled: true}
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
	return Result{Handled: true, Events: []pluginapi.WidgetEvent{p.expandEvent(w, vn.node, expand)}}
}

// expandEvent is the expand/collapse widget event of a tree node.
func (p *Panel) expandEvent(w pluginapi.Widget, nd pluginapi.WidgetNode, expanded bool) pluginapi.WidgetEvent {
	e := p.ev("expand")
	e.Widget = nd.Key
	e.Checked = expanded
	return e
}

// HandleMouse handles one mouse message. It reports whether the pointer was inside the
// panel's rectangle; outside, the root routes the message to the frame.
func (p *Panel) HandleMouse(m Mouse) (Result, bool) {
	if !p.rect.Contains(m.X, m.Y) {
		return Result{}, false
	}
	if m.Release {
		if p.drag == nil {
			return Result{Handled: true}, true
		}
		return p.endTreeDrag(m.X, m.Y), true
	}
	// A motion during a row drag updates the drag target instead of only reporting a move.
	if p.drag != nil && m.Button == "" && m.Wheel == 0 {
		return p.treeDragMotion(m.X, m.Y), true
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
		if h, ok := p.hitAt(m.X, m.Y); ok && h.focusIdx >= 0 {
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
	if h.kind == hitOverlayItem {
		w, ok := findWidget(p.spec.Root, h.overlayKey)
		if !ok {
			return res, true
		}
		switch h.overlay {
		case "dropdown":
			if w.Kind == pluginapi.WidgetDropdown {
				p.setDropdownOpen(w, false)
				res.Events = append(res.Events, p.changeOption(w, h.index))
			}
		case "completion":
			if w.Kind == pluginapi.WidgetText {
				res.Events = append(res.Events, p.acceptCompletionIndex(w, h.index).Events...)
			}
		}
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
		if w.Kind == pluginapi.WidgetList {
			items := p.listItems(w)
			if h.index >= 0 && h.index < len(items) {
				p.setSelected(w, h.index)
				sel := p.ev("select")
				sel.Index = h.index
				sel.Widget = items[h.index].Key
				res.Events = append(res.Events, sel)
			}
		}
	case hitTreeRow:
		nodes := p.treeNodes(w)
		if w.Kind == pluginapi.WidgetTree && h.index >= 0 && h.index < len(nodes) {
			p.setSelected(w, h.index)
			sel := p.ev("select")
			sel.Index, sel.Widget = h.index, nodes[h.index].Key
			res.Events = append(res.Events, sel)
			if nodes[h.index].Draggable && nodes[h.index].Key != "" {
				p.drag = &treeDrag{widget: w.Key, source: nodes[h.index].Key, targetIndex: -1}
			}
		}
	case hitTreeDisclosure:
		nodes := p.treeNodes(w)
		if w.Kind == pluginapi.WidgetTree && h.index >= 0 && h.index < len(nodes) {
			p.setSelected(w, h.index)
			cur := false
			for _, vn := range p.visible(w) {
				if vn.index == h.index {
					cur = vn.expanded
					break
				}
			}
			p.setExpanded(w, nodes[h.index], !cur)
			res.Events = append(res.Events, p.expandEvent(w, nodes[h.index], !cur))
		}
	case hitText:
		if w.Kind == pluginapi.WidgetText {
			text := p.textValue(w)
			inner := max(h.r.W-2, 1)
			if w.Rows > 1 {
				line := space.Clamp(m.Y-h.r.Y, 0, lineCount(text)-1)
				col := max(m.X-h.r.X, 0)
				p.setText(w, text, offsetOf(text, line, byteColumnAt(text, line, col)))
			} else {
				shown, _ := windowText(text, p.textCursor(w), inner)
				start := len(text) - len(shown)
				col := max(m.X-h.r.X-1, 0)
				line, _ := lineColOf(text, start)
				lineText := text[offsetOf(text, line, 0):]
				if i := strings.IndexByte(lineText, 10); i >= 0 {
					lineText = lineText[:i]
				}
				p.setText(w, text, start+byteColumnAt(lineText, 0, col))
			}
			p.setSelection(w, p.textCursor(w), p.textCursor(w))
		}
	case hitRadio:
		if w.Kind == pluginapi.WidgetRadio && h.index >= 0 && h.index < len(w.Options) {
			res.Events = append(res.Events, p.changeOption(w, h.index))
		}
	case hitDualRow:
		if w.Kind == pluginapi.WidgetDualList && h.index >= 0 && h.index < len(w.Options) {
			p.setSelected(w, h.index)
		}
	case hitDropdown:
		if w.Kind == pluginapi.WidgetDropdown {
			p.setDropdownOpen(w, !p.dropdownOpen(w))
		}
	}
	return res, true
}

// treeDragMotion updates the drag target to the tree row under the pointer and reports a
// drag event when it changes.
func (p *Panel) treeDragMotion(x, y int) Result {
	h, ok := p.hitAt(x, y)
	key, idx := "", -1
	if ok && h.kind == hitTreeRow {
		if w, found := widgetAt(p.spec.Root, h.path); found && w.Kind == pluginapi.WidgetTree && h.index >= 0 {
			nodes := p.treeNodes(w)
			if h.index < len(nodes) {
				key, idx = nodes[h.index].Key, h.index
			}
		}
	}
	res := Result{Handled: true}
	id := y<<20 | x
	if !p.haveMouse || p.lastMouse != id {
		p.lastMouse, p.haveMouse = id, true
		mv := p.ev("mouse_move")
		mv.X, mv.Y = x, y
		res.Events = append(res.Events, mv)
	}
	if p.drag.target == key && p.drag.targetIndex == idx {
		return res
	}
	p.drag.target, p.drag.targetIndex = key, idx
	e := p.ev("drag")
	e.Widget = p.drag.source
	e.TargetKey, e.TargetIndex = key, idx
	res.Events = append(res.Events, e)
	return res
}

// endTreeDrag finishes the drag: a release on another row reports drop and then dragend;
// anywhere else only dragend.
func (p *Panel) endTreeDrag(x, y int) Result {
	d := p.drag
	p.drag = nil
	res := Result{Handled: true}
	if d == nil {
		return res
	}
	if d.target != "" && d.target != d.source {
		drop := p.ev("drop")
		drop.Widget = d.source
		drop.TargetKey = d.target
		drop.TargetIndex = d.targetIndex
		res.Events = append(res.Events, drop)
	}
	end := p.ev("dragend")
	end.Widget = d.source
	res.Events = append(res.Events, end)
	return res
}

// byteColumnAt returns the byte offset within a line of the display column col (a column
// inside a wide rune is the start of that rune).
func byteColumnAt(text string, line, col int) int {
	start := offsetOf(text, line, 0)
	end := strings.IndexByte(text[start:], '\n')
	if end < 0 {
		end = len(text) - start
	}
	lineText := text[start : start+end]
	off := 0
	for cells := 0; off < len(lineText); {
		r, n := utf8.DecodeRuneInString(lineText[off:])
		if cells += ansi.StringWidth(string(r)); cells > col {
			break
		}
		off += n
	}
	return off
}

// Mutate applies a targeted mutation and reports whether the widget it names exists.
func (p *Panel) Mutate(m pluginapi.WidgetMutation) bool {
	if m.Kind == "setFocusKey" {
		key := m.FocusKey
		if key == "" {
			key = m.WidgetKey
		}
		return p.FocusWidget(key)
	}
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
	case "setSelectedIndex", "setDropdown":
		switch w.Kind {
		case pluginapi.WidgetList, pluginapi.WidgetTree, pluginapi.WidgetDropdown, pluginapi.WidgetRadio, pluginapi.WidgetDualList:
			n := len(w.Items)
			if w.Kind == pluginapi.WidgetTree {
				n = len(p.treeNodes(w))
			} else if w.Kind != pluginapi.WidgetList {
				n = len(w.Options)
			}
			p.setSelected(w, space.Clamp(m.Index, 0, max(n-1, 0)))
			return true
		}
	case "setNumber":
		if w.Kind == pluginapi.WidgetNumber {
			p.setNumber(w, m.Number)
			return true
		}
	case "setValue":
		if w.Kind == pluginapi.WidgetText {
			cursor := p.textCursor(w)
			if m.HasCursor {
				cursor = m.CursorByte
			}
			p.setText(w, m.Value, cursor)
			return true
		}
		if in := p.instFor(w.Key); in != nil {
			in.value, in.haveValue = m.Value, true
			return true
		}
	case "setCompletions":
		if w.Kind == pluginapi.WidgetText {
			in := p.instFor(w.Key)
			if in == nil {
				return false
			}
			in.completions = nil
			for _, it := range m.Items {
				if it.Text != "" {
					in.completions = append(in.completions, it.Text)
				}
			}
			in.haveCompletions = true
			in.completionSel = 0
			return true
		}
	case "setDualIncluded":
		if w.Kind == pluginapi.WidgetDualList {
			p.setIncluded(w, m.Included)
			return true
		}
	case "setItems":
		if w.Kind == pluginapi.WidgetList {
			items := append([]pluginapi.WidgetItem(nil), m.Items...)
			for i := range items {
				if i < len(m.ItemKeys) {
					items[i].Key = m.ItemKeys[i]
				}
			}
			in := p.instFor(w.Key)
			if in == nil {
				return false
			}
			in.items, in.haveItems = items, true
			return true
		}
	case "setRawEntries":
		if w.Kind == pluginapi.WidgetRaw {
			in := p.instFor(w.Key)
			if in == nil {
				return false
			}
			in.runs, in.haveRuns = append([]pluginapi.WidgetRun(nil), m.Entries...), true
			return true
		}
	case "setExpandedKeys":
		if w.Kind == pluginapi.WidgetTree {
			in := p.instFor(w.Key)
			if in == nil {
				return false
			}
			in.expanded = map[string]bool{}
			for _, k := range m.Keys {
				in.expanded[k] = true
			}
			return true
		}
	case "setCheckedKeys":
		if w.Kind == pluginapi.WidgetTree {
			in := p.instFor(w.Key)
			if in == nil {
				return false
			}
			if in.checkedKeys == nil {
				in.checkedKeys = map[string]bool{}
			}
			for _, k := range m.Keys {
				in.checkedKeys[k] = m.Checked
			}
			return true
		}
	case "previewTreeMove":
		if w.Kind == pluginapi.WidgetTree {
			in := p.instFor(w.Key)
			if in == nil {
				return false
			}
			in.previewHave = true
			in.previewSrc, in.previewAfter, in.previewLabel = m.SourceKey, m.AfterKey, m.Label
			return true
		}
	case "endTreeMovePreview":
		if w.Kind == pluginapi.WidgetTree {
			if in := p.inst[w.Key]; in != nil {
				in.previewHave = false
			}
			return true
		}
	case "appendTreeNodes":
		if w.Kind == pluginapi.WidgetTree {
			in := p.instFor(w.Key)
			if in == nil {
				return false
			}
			if !in.haveNodes {
				in.nodes = append([]pluginapi.WidgetNode(nil), w.Nodes...)
				in.haveNodes = true
			}
			in.nodes = append(in.nodes, m.NewNodes...)
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

func sign(v int) int {
	switch {
	case v > 0:
		return 1
	case v < 0:
		return -1
	}
	return 0
}
