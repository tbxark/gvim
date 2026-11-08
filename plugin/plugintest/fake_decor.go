package plugintest

import (
	"fmt"
	"slices"

	"github.com/tbxark/gvim/pluginapi"
)

// The decoration half of the fake host (stage S15). It keeps what plugins set, per plugin,
// namespace and buffer, and checks what the real host checks: the buffer must be open, and
// an expected version must be the buffer's. It does not move anchors when a buffer is
// edited; that is decorations's job, tested there and in the app.

// DecorEntry is one decoration the fake holds, with the id Add gave it.
type DecorEntry struct {
	Plugin, NS string
	Buf        pluginapi.BufferID
	ID         int
	Item       pluginapi.Decoration
}

// FakeDecor is the state of the DecorHost fake.
type FakeDecor struct {
	// Entries are the decorations, in the order they were set.
	Entries []DecorEntry
	// MaxPerNamespace, when positive, makes Set and Add fail with limit beyond it.
	MaxPerNamespace int

	nextID int
}

// Decor returns the decoration state of the fake.
func (f *FakeHost) Decor() *FakeDecor {
	d, ok := f.Ext["decor"].(*FakeDecor)
	if !ok {
		d = &FakeDecor{}
		f.Ext["decor"] = d
	}
	return d
}

// Items returns the decorations of (plugin, ns) on buffer id, in order.
func (d *FakeDecor) Items(plugin, ns string, id pluginapi.BufferID) []pluginapi.Decoration {
	var out []pluginapi.Decoration
	for _, e := range d.Entries {
		if e.Plugin == plugin && e.NS == ns && e.Buf == id {
			out = append(out, e.Item)
		}
	}
	return out
}

func init() {
	RegisterResourceKind(func(f *FakeHost) []string {
		d, ok := f.Ext["decor"].(*FakeDecor)
		if !ok {
			return nil
		}
		seen := map[string]bool{}
		for _, e := range d.Entries {
			seen[fmt.Sprintf("decor:%s/%s@%d", e.Plugin, e.NS, e.Buf)] = true
		}
		out := make([]string, 0, len(seen))
		for k := range seen {
			out = append(out, k)
		}
		return out
	})
	OnOwnerGone(func(f *FakeHost, plugin string, _ uint64) {
		d, ok := f.Ext["decor"].(*FakeDecor)
		if !ok {
			return
		}
		d.Entries = slices.DeleteFunc(d.Entries, func(e DecorEntry) bool { return e.Plugin == plugin })
	})
}

// decorTarget checks the buffer and the expected version, as the real host does.
func (f *FakeHost) decorTarget(id pluginapi.BufferID, expect *pluginapi.Version) error {
	b := f.find(id)
	if b == nil {
		return pluginapi.Errf(pluginapi.CodeClosed, "buffer %d is closed", id)
	}
	if expect != nil && *expect != b.Info.Version {
		return pluginapi.Errf(pluginapi.CodeStale, "buffer %d changed (version %s, expected %s)", id, b.Info.Version, *expect)
	}
	return nil
}

func (d *FakeDecor) count(plugin, ns string, id pluginapi.BufferID) int {
	n := 0
	for _, e := range d.Entries {
		if e.Plugin == plugin && e.NS == ns && e.Buf == id {
			n++
		}
	}
	return n
}

// DecorSet replaces the namespace's decorations on the buffer.
func (f *FakeHost) DecorSet(plugin, ns string, id pluginapi.BufferID, items []pluginapi.Decoration, expect *pluginapi.Version) error {
	f.rec("DecorSet", plugin, ns, id, len(items))
	if err := f.decorTarget(id, expect); err != nil {
		return err
	}
	d := f.Decor()
	if d.MaxPerNamespace > 0 && len(items) > d.MaxPerNamespace {
		return pluginapi.Errf(pluginapi.CodeLimit, "too many decorations (max %d)", d.MaxPerNamespace)
	}
	d.Entries = slices.DeleteFunc(d.Entries, func(e DecorEntry) bool { return e.Plugin == plugin && e.NS == ns && e.Buf == id })
	for _, it := range items {
		d.nextID++
		d.Entries = append(d.Entries, DecorEntry{Plugin: plugin, NS: ns, Buf: id, ID: d.nextID, Item: it})
	}
	return nil
}

// DecorAdd adds one decoration and returns its id.
func (f *FakeHost) DecorAdd(plugin, ns string, id pluginapi.BufferID, item pluginapi.Decoration, expect *pluginapi.Version) (int, error) {
	f.rec("DecorAdd", plugin, ns, id)
	if err := f.decorTarget(id, expect); err != nil {
		return 0, err
	}
	d := f.Decor()
	if d.MaxPerNamespace > 0 && d.count(plugin, ns, id) >= d.MaxPerNamespace {
		return 0, pluginapi.Errf(pluginapi.CodeLimit, "too many decorations (max %d)", d.MaxPerNamespace)
	}
	d.nextID++
	d.Entries = append(d.Entries, DecorEntry{Plugin: plugin, NS: ns, Buf: id, ID: d.nextID, Item: item})
	return d.nextID, nil
}

// DecorRemove removes one decoration by id.
func (f *FakeHost) DecorRemove(plugin, ns string, id pluginapi.BufferID, item int) {
	f.rec("DecorRemove", plugin, ns, id, item)
	d := f.Decor()
	d.Entries = slices.DeleteFunc(d.Entries, func(e DecorEntry) bool {
		return e.Plugin == plugin && e.NS == ns && e.Buf == id && e.ID == item
	})
}

// DecorClear removes the namespace's decorations on the buffer; buffer 0 is every buffer.
func (f *FakeHost) DecorClear(plugin, ns string, id pluginapi.BufferID) {
	f.rec("DecorClear", plugin, ns, id)
	d := f.Decor()
	d.Entries = slices.DeleteFunc(d.Entries, func(e DecorEntry) bool {
		return e.Plugin == plugin && e.NS == ns && (id == 0 || e.Buf == id)
	})
}

// DecorClearRange removes the namespace's items that intersect the line range.
func (f *FakeHost) DecorClearRange(plugin, ns string, id pluginapi.BufferID, fromLine, toLine int) {
	f.rec("DecorClearRange", plugin, ns, id, fromLine, toLine)
	if toLine < fromLine {
		fromLine, toLine = toLine, fromLine
	}
	d := f.Decor()
	d.Entries = slices.DeleteFunc(d.Entries, func(e DecorEntry) bool {
		if e.Plugin != plugin || e.NS != ns || (id != 0 && e.Buf != id) {
			return false
		}
		it := e.Item
		last := it.Line
		switch it.Kind {
		case pluginapi.DecorHighlight, pluginapi.DecorConceal, pluginapi.DecorScroll:
			last = it.Range.To.Line
		}
		return it.Line <= toLine && last >= fromLine
	})
}

// FakeFoldRanges is one recorded folding-range set.
type FakeFoldRanges struct {
	Plugin string
	NS     string
	Buf    pluginapi.BufferID
	Ranges []pluginapi.FoldRange
}

// FoldRanges lists the folding-range sets recorded, in order.
func (f *FakeHost) FoldRanges() []FakeFoldRanges {
	v, _ := f.Ext["foldranges"].([]FakeFoldRanges)
	return append([]FakeFoldRanges(nil), v...)
}

// DecorSetFoldingRanges records the namespace's fold ranges.
func (f *FakeHost) DecorSetFoldingRanges(plugin, ns string, id pluginapi.BufferID, ranges []pluginapi.FoldRange, expect *pluginapi.Version) error {
	f.rec("DecorSetFoldingRanges", plugin, ns, id, len(ranges))
	if err := f.decorTarget(id, expect); err != nil {
		return err
	}
	v, _ := f.Ext["foldranges"].([]FakeFoldRanges)
	kept := v[:0:0]
	for _, fr := range v {
		if fr.Plugin == plugin && fr.NS == ns && fr.Buf == id {
			continue
		}
		kept = append(kept, fr)
	}
	kept = append(kept, FakeFoldRanges{Plugin: plugin, NS: ns, Buf: id, Ranges: append([]pluginapi.FoldRange(nil), ranges...)})
	f.Ext["foldranges"] = kept
	return nil
}

// DecorClearFolds forgets the recorded fold ranges of the namespace (or the plugin).
func (f *FakeHost) DecorClearFolds(plugin, ns string, id pluginapi.BufferID) {
	f.rec("DecorClearFolds", plugin, ns, id)
	v, _ := f.Ext["foldranges"].([]FakeFoldRanges)
	kept := v[:0:0]
	for _, fr := range v {
		if fr.Plugin == plugin && (ns == "" || fr.NS == ns) && (id == 0 || fr.Buf == id) {
			continue
		}
		kept = append(kept, fr)
	}
	f.Ext["foldranges"] = kept
}

// DecorRefreshLines is a no-op that records the call.
func (f *FakeHost) DecorRefreshLines(id pluginapi.BufferID) {
	f.rec("DecorRefreshLines", id)
}
