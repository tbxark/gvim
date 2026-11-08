package pluginpanel

import (
	"github.com/tbxark/gvim/pluginapi"
)

// Host-owned interactive instance state and its helpers: the dropdown open flag, the
// completion list of a text field, the raw rows a mutation replaced, the checked nodes of a
// tree, and the drag preview the tree paints.

// evNamed builds a simple widget event of one keyed widget.
func evNamed(name, key string) pluginapi.WidgetEvent {
	return pluginapi.WidgetEvent{Event: name, Widget: key, Index: -1, CursorByte: -1, TargetIndex: -1}
}

// dropdownOpen reports whether the dropdown is expanded: the host flag once the user
// changed it, else the spec seed.
func (p *Panel) dropdownOpen(w pluginapi.Widget) bool {
	if in := p.inst[w.Key]; in != nil && in.haveOpen {
		return in.open
	}
	return w.Open
}

// setDropdownOpen stores the dropdown open state.
func (p *Panel) setDropdownOpen(w pluginapi.Widget, open bool) {
	if in := p.instFor(w.Key); in != nil {
		in.open, in.haveOpen = open, true
	}
}

// completionsOf returns the candidate list of a text field: the host's list once a mutation
// replaced it, else the spec seed.
func (p *Panel) completionsOf(w pluginapi.Widget) []string {
	if in := p.inst[w.Key]; in != nil && in.haveCompletions {
		return in.completions
	}
	return w.Completions
}

// completionsOpen reports whether the focused text field's candidate list is showing.
func (p *Panel) completionsOpen(w pluginapi.Widget) bool {
	if len(p.completionsOf(w)) == 0 {
		return false
	}
	if in := p.inst[w.Key]; in != nil && in.completionHaveOpen {
		return in.completionOpen
	}
	return true
}

// setCompletionsOpen stores whether the candidate list is showing.
func (p *Panel) setCompletionsOpen(w pluginapi.Widget, open bool) {
	if in := p.instFor(w.Key); in != nil {
		in.completionOpen, in.completionHaveOpen = open, true
	}
}

// completionSelOf returns the highlighted candidate row.
func (p *Panel) completionSelOf(w pluginapi.Widget) int {
	if in := p.inst[w.Key]; in != nil {
		return in.completionSel
	}
	return 0
}

// cycleCompletion moves the highlighted candidate row.
func (p *Panel) cycleCompletion(w pluginapi.Widget, delta int) {
	items := p.completionsOf(w)
	if len(items) == 0 {
		return
	}
	in := p.instFor(w.Key)
	if in == nil {
		return
	}
	in.completionSel = clamp(in.completionSel+delta, 0, len(items)-1)
}

// acceptCompletion fills the highlighted candidate into the text field and reports a change
// and a completion_accept event.
func (p *Panel) acceptCompletion(w pluginapi.Widget) Result {
	return p.acceptCompletionIndex(w, p.completionSelOf(w))
}

// acceptCompletionIndex fills candidate i into the text field.
func (p *Panel) acceptCompletionIndex(w pluginapi.Widget, i int) Result {
	items := p.completionsOf(w)
	if i < 0 || i >= len(items) {
		return Result{Handled: true}
	}
	p.insertText(w, items[i])
	p.setCompletionsOpen(w, false)
	change := p.evInteractive("change", w)
	accept := evNamed("completion_accept", w.Key)
	accept.Value = items[i]
	return Result{Handled: true, Events: []pluginapi.WidgetEvent{change, accept}}
}

// rawRuns returns the rows of a raw widget: the host's rows once a mutation replaced them,
// else the spec's.
func (p *Panel) rawRuns(w pluginapi.Widget) []pluginapi.WidgetRun {
	if in := p.inst[w.Key]; in != nil && in.haveRuns {
		return in.runs
	}
	return w.Runs
}

// treeChecked returns the checked state of a tree node: the host's keyed map once a
// setCheckedKeys mutation set it, else the node's seed.
func (p *Panel) treeChecked(w pluginapi.Widget, nd pluginapi.WidgetNode) bool {
	if in := p.inst[w.Key]; in != nil && in.checkedKeys != nil {
		if v, ok := in.checkedKeys[nd.Key]; ok {
			return v
		}
	}
	return nd.Checked
}

// treeDrag is the drag of one tree row: the widget key, the lifted node key and the row the
// pointer is over (-1 for none).
type treeDrag struct {
	widget      string
	source      string
	target      string
	targetIndex int
}

// previewOrder returns the visible tree nodes with the drag preview applied: the source row
// is moved after the plugin's after_key row and drawn with the preview label. It returns
// (nodes, label) where label is the node key of the moved row (its Text is replaced).
func (p *Panel) previewOrder(w pluginapi.Widget, vis []visNode) ([]visNode, string) {
	in := p.inst[w.Key]
	if in == nil || !in.previewHave {
		return vis, ""
	}
	src, after := in.previewSrc, in.previewAfter
	out := make([]visNode, 0, len(vis))
	var moved *visNode
	for _, vn := range vis {
		if vn.node.Key == src {
			cp := vn
			moved = &cp
			continue
		}
		out = append(out, vn)
	}
	if moved == nil {
		return vis, ""
	}
	if in.previewLabel != "" {
		moved.node.Text = in.previewLabel
	}
	at := len(out)
	for i, vn := range out {
		if vn.node.Key == after {
			at = i + 1
			break
		}
	}
	out = append(out, visNode{})
	copy(out[at+1:], out[at:])
	out[at] = *moved
	return out, moved.node.Key
}
