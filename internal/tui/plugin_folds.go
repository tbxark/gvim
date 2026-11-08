package tui

import (
	"sort"

	"github.com/tbxark/gvim/fold"
	"github.com/tbxark/gvim/pluginapi"
)

// Plugin folds (plugin API tier 1c-A): folding ranges a plugin namespace gives for a buffer.
// They are kept in the root plugin state, merged with the language server's and the outline's
// ranges when the pane folds the document, and the user can toggle a togglable range like any
// other fold. A range's Collapsed seed says how it starts; the root remembers what the user
// did after that.

// maxPluginFolds bounds the ranges of one namespace on one buffer.
const maxPluginFolds = 2000

// pluginFolds is the fold state of the plugins, by buffer.
type pluginFolds struct {
	byBuffer map[pluginapi.BufferID][]*foldNamespace
}

// foldNamespace is the ranges of one plugin (owner, namespace) on one buffer.
type foldNamespace struct {
	owner, ns string
	ranges    []pluginapi.FoldRange
	// collapsed is the user's toggle state, by range start line: set means the range was
	// toggled and collapsed is its value.
	collapsed map[int]bool
	order     int
}

// namespace returns the entry of (owner, ns) on the buffer, nil when none.
func (p *pluginFolds) namespace(id pluginapi.BufferID, owner, ns string) *foldNamespace {
	for _, f := range p.byBuffer[id] {
		if f.owner == owner && f.ns == ns {
			return f
		}
	}
	return nil
}

// dropOwner removes everything a plugin has.
func (p *pluginFolds) dropOwner(owner string) {
	for id, list := range p.byBuffer {
		kept := list[:0:0]
		for _, f := range list {
			if f.owner != owner {
				kept = append(kept, f)
			}
		}
		p.byBuffer[id] = kept
		if len(kept) == 0 {
			delete(p.byBuffer, id)
		}
	}
}

// dropBuffer forgets a closed buffer's ranges.
func (p *pluginFolds) dropBuffer(id pluginapi.BufferID) { delete(p.byBuffer, id) }

// DecorSetFoldingRanges replaces one plugin namespace's folding ranges for a buffer.
func (h *pluginHost) DecorSetFoldingRanges(plugin, ns string, id pluginapi.BufferID, ranges []pluginapi.FoldRange, expect *pluginapi.Version) error {
	m := h.m
	if _, err := h.decorTarget(id, expect); err != nil {
		return err
	}
	if len(ranges) > maxPluginFolds {
		return pluginapi.Errf(pluginapi.CodeLimit, "a namespace takes at most %d folding ranges", maxPluginFolds)
	}
	info, _ := h.ed().Buffer(id)
	last := max(info.LineCount-1, 0)
	clean := make([]pluginapi.FoldRange, 0, len(ranges))
	for _, r := range ranges {
		start, end := r.Range.From.Line, r.Range.To.Line
		if end < start {
			start, end = end, start
		}
		start, end = min(max(start, 0), last), min(max(end, 0), last)
		if end <= start {
			continue // a one-line range folds nothing
		}
		placeholder := r.Placeholder
		if len(placeholder) > decorMaxText {
			placeholder = placeholder[:decorMaxText]
		}
		r.Range.From.Line, r.Range.To.Line = start, end
		r.Placeholder = placeholder
		clean = append(clean, r)
	}
	pf := &m.plug.folds
	if pf.byBuffer == nil {
		pf.byBuffer = map[pluginapi.BufferID][]*foldNamespace{}
	}
	fns := pf.namespace(id, plugin, ns)
	if fns == nil {
		m.plug.foldOrder++
		fns = &foldNamespace{owner: plugin, ns: ns, order: m.plug.foldOrder}
		pf.byBuffer[id] = append(pf.byBuffer[id], fns)
	}
	fns.ranges = clean
	if fns.collapsed == nil {
		fns.collapsed = map[int]bool{}
	}
	return nil
}

// DecorClearFolds removes the plugin's folding ranges: for one namespace and buffer id, or
// everything the plugin set when ns is empty, or (id 0) on every buffer.
func (h *pluginHost) DecorClearFolds(plugin, ns string, id pluginapi.BufferID) {
	pf := &h.m.plug.folds
	clear := func(buf pluginapi.BufferID) {
		kept := pf.byBuffer[buf][:0:0]
		for _, f := range pf.byBuffer[buf] {
			if f.owner == plugin && (ns == "" || f.ns == ns) {
				continue
			}
			kept = append(kept, f)
		}
		pf.byBuffer[buf] = kept
		if len(kept) == 0 {
			delete(pf.byBuffer, buf)
		}
	}
	if id != 0 {
		clear(id)
		return
	}
	for buf := range pf.byBuffer {
		clear(buf)
	}
}

// plugFolds gives the pane the plugin folding ranges of the document: the merged ranges and
// their collapsed state (the seed, or the user's toggle), and the placeholder texts. A
// document with no plugin folds gets nil.
func (m *Model) plugFolds(doc Document) ([]fold.Range, map[int]bool, map[int]string) {
	if len(m.plug.folds.byBuffer) == 0 {
		return nil, nil, nil
	}
	bd, ok := doc.(bufferIdentified)
	if !ok {
		return nil, nil, nil
	}
	id := pluginapi.BufferID(bd.BufferID())
	if len(m.plug.folds.byBuffer[id]) == 0 {
		return nil, nil, nil
	}
	var ranges []fold.Range
	folded := map[int]bool{}
	placeholder := map[int]string{}
	for _, f := range m.plug.folds.byBuffer[id] {
		for _, r := range f.ranges {
			fr := fold.Range{Start: r.Range.From.Line, End: r.Range.To.Line}
			collapsed := r.Collapsed
			if v, ok := f.collapsed[fr.Start]; ok {
				collapsed = v
			}
			ranges = append(ranges, fr)
			if collapsed {
				folded[fr.Start] = true
			}
			if r.Placeholder != "" {
				placeholder[fr.Start] = r.Placeholder
			}
		}
	}
	if len(ranges) == 0 {
		return nil, nil, nil
	}
	sort.SliceStable(ranges, func(i, j int) bool {
		if ranges[i].Start != ranges[j].Start {
			return ranges[i].Start < ranges[j].Start
		}
		return ranges[i].End < ranges[j].End
	})
	return ranges, folded, placeholder
}

// togglePluginFold toggles the plugin fold that starts at line, reporting whether one did.
// The root's fold action tries it before the document's own folds.
func (m *Model) togglePluginFold(doc Document, line int) bool {
	bd, ok := doc.(bufferIdentified)
	if !ok {
		return false
	}
	id := pluginapi.BufferID(bd.BufferID())
	for _, f := range m.plug.folds.byBuffer[id] {
		for _, r := range f.ranges {
			if r.Range.From.Line != line {
				continue
			}
			cur := r.Collapsed
			if v, ok := f.collapsed[line]; ok {
				cur = v
			}
			f.collapsed[line] = !cur
			return true
		}
	}
	return false
}
