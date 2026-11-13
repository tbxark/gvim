package tui

import (
	"image/color"
	"path/filepath"
	"sort"
	"strings"

	"github.com/tbxark/gvim/internal/tui/explorer"
	"github.com/tbxark/gvim/internal/tui/grid"
	"github.com/tbxark/gvim/internal/tui/style"
	"github.com/tbxark/gvim/pluginapi"
)

// File explorer decorations and slots: the host store behind Fresh's
// setFileExplorerDecorations/setFileExplorerSlots. Plugins set them per namespace; the
// root resolves, per painted row, the best decoration and slot, and hands the explorer a
// plugin-agnostic explorer.RowDecor through the Geometry callback. Painting reads the
// store every frame (no cache invalidation), OwnerGone drops a plugin's namespaces, and a
// path outside the workspace root is ignored.

// explorerDeco is one decoration entry of a namespace.
type explorerDeco struct {
	symbol   string
	color    pluginapi.Color
	priority int
	order    int64
}

// explorerSlot is one slot override entry of a namespace.
type explorerSlot struct {
	slot  pluginapi.FileExplorerSlot
	order int64
}

// explorerNamespace is one (plugin, namespace) set of decorations and slots.
type explorerNamespace struct {
	plugin, ns string
	order      int64
	decos      map[string]explorerDeco // by normalized absolute path
	slots      map[string]explorerSlot
}

// pluginExplorer is the explorer surface state of the plugin host.
type pluginExplorer struct {
	sets  map[string]*explorerNamespace // "plugin\x00namespace"
	next  int64
	dirty bool
	// The resolved index, rebuilt lazily after a change.
	deco  map[string]explorerDeco
	slots map[string]explorerSlot
}

// set returns (creating when create) the namespace of a plugin.
func (pe *pluginExplorer) set(plugin, ns string, create bool) *explorerNamespace {
	if pe.sets == nil {
		if !create {
			return nil
		}
		pe.sets = map[string]*explorerNamespace{}
	}
	key := plugin + "\x00" + ns
	s := pe.sets[key]
	if s == nil && create {
		pe.next++
		s = &explorerNamespace{plugin: plugin, ns: ns, order: pe.next}
		pe.sets[key] = s
	}
	return s
}

// countPlugin is how many namespaces a plugin has.
func (pe *pluginExplorer) countPlugin(plugin string) int {
	n := 0
	for _, s := range pe.sets {
		if s.plugin == plugin {
			n++
		}
	}
	return n
}

// drop forgets one namespace's decorations or its slots, and the key when it has
// nothing left.
func (pe *pluginExplorer) drop(plugin, ns string, decos bool) {
	key := plugin + "\x00" + ns
	s := pe.sets[key]
	if s == nil {
		return
	}
	if decos {
		s.decos = nil
	} else {
		s.slots = nil
	}
	if len(s.decos) == 0 && len(s.slots) == 0 {
		delete(pe.sets, key)
	}
	pe.dirty = true
}

// dropOwner forgets everything a plugin set (unload, reload).
func (pe *pluginExplorer) dropOwner(plugin string) {
	changed := false
	for key, s := range pe.sets {
		if s.plugin == plugin {
			delete(pe.sets, key)
			changed = true
		}
	}
	if changed {
		pe.dirty = true
	}
}

// explorerPath normalizes a plugin path against the workspace root. Relative paths
// resolve against the root; an empty root accepts absolute paths only. Paths outside the
// root are ignored.
func (m *Model) explorerPath(p string) (string, bool) {
	p = strings.TrimSpace(p)
	if p == "" {
		return "", false
	}
	root := m.rootDir()
	if !filepath.IsAbs(p) {
		if root == "" {
			return "", false
		}
		p = filepath.Join(root, p)
	}
	p = filepath.Clean(p)
	if root != "" {
		root = filepath.Clean(root)
		if p != root && !strings.HasPrefix(p, root+string(filepath.Separator)) {
			return "", false
		}
	}
	return p, true
}

// SetExplorerDecorations replaces one namespace's decorated rows.
func (h *pluginHost) SetExplorerDecorations(plugin, ns string, list []pluginapi.FileExplorerDecoration) error {
	m := h.model()
	if ns == "" || len(ns) > 128 {
		return pluginapi.Errf(pluginapi.CodeInvalid, "a namespace name is 1 to 128 bytes")
	}
	if len(list) > pluginapi.MaxExplorerEntries {
		return pluginapi.Errf(pluginapi.CodeLimit, "a namespace takes at most %d decorations", pluginapi.MaxExplorerEntries)
	}
	pe := &m.plug.explorer
	s := pe.set(plugin, ns, true)
	if len(s.decos) == 0 && len(s.slots) == 0 && pe.countPlugin(plugin) > pluginapi.MaxExplorerNamespaces {
		delete(pe.sets, plugin+"\x00"+ns)
		return pluginapi.Errf(pluginapi.CodeLimit, "a plugin uses at most %d explorer namespaces", pluginapi.MaxExplorerNamespaces)
	}
	if s.decos == nil {
		s.decos = map[string]explorerDeco{}
	}
	clean := make(map[string]explorerDeco, len(list))
	for _, d := range list {
		path, ok := m.explorerPath(d.Path)
		if !ok {
			continue
		}
		symbol := cleanText(d.Symbol)
		if len(symbol) > pluginapi.MaxExplorerSymbol {
			symbol = symbol[:pluginapi.MaxExplorerSymbol]
		}
		if symbol == "" {
			continue
		}
		prio := clampExplorerInt(d.Priority)
		entry := explorerDeco{symbol: symbol, color: d.Color, priority: prio, order: pe.next}
		pe.next++
		if old, ok := clean[path]; !ok || betterDeco(entry, old, s.ns, s.ns) {
			clean[path] = entry
		}
	}
	s.decos = clean
	if len(s.decos) == 0 && len(s.slots) == 0 {
		delete(pe.sets, plugin+"\x00"+ns)
	}
	pe.dirty = true
	return nil
}

// ClearExplorerDecorations removes a namespace's decorations (its slots stay).
func (h *pluginHost) ClearExplorerDecorations(plugin, ns string) {
	h.model().plug.explorer.drop(plugin, ns, true)
}

// SetExplorerSlots replaces one namespace's slot overrides.
func (h *pluginHost) SetExplorerSlots(plugin, ns string, list []pluginapi.FileExplorerSlot) error {
	m := h.model()
	if ns == "" || len(ns) > 128 {
		return pluginapi.Errf(pluginapi.CodeInvalid, "a namespace name is 1 to 128 bytes")
	}
	if len(list) > pluginapi.MaxExplorerEntries {
		return pluginapi.Errf(pluginapi.CodeLimit, "a namespace takes at most %d slot entries", pluginapi.MaxExplorerEntries)
	}
	pe := &m.plug.explorer
	s := pe.set(plugin, ns, true)
	if len(s.decos) == 0 && len(s.slots) == 0 && pe.countPlugin(plugin) > pluginapi.MaxExplorerNamespaces {
		delete(pe.sets, plugin+"\x00"+ns)
		return pluginapi.Errf(pluginapi.CodeLimit, "a plugin uses at most %d explorer namespaces", pluginapi.MaxExplorerNamespaces)
	}
	if s.slots == nil {
		s.slots = map[string]explorerSlot{}
	}
	clean := make(map[string]explorerSlot, len(list))
	for _, slot := range list {
		path, ok := m.explorerPath(slot.Path)
		if !ok {
			continue
		}
		slot.Priority = clampExplorerInt(slot.Priority)
		if slot.Leading != nil {
			slot.Leading = cleanSlotText(slot.Leading)
		}
		if slot.Trailing != nil {
			slot.Trailing = cleanSlotText(slot.Trailing)
		}
		entry := explorerSlot{slot: slot, order: pe.next}
		pe.next++
		if old, ok := clean[path]; !ok || betterSlot(entry, old, s.ns, s.ns) {
			clean[path] = entry
		}
	}
	s.slots = clean
	if len(s.decos) == 0 && len(s.slots) == 0 {
		delete(pe.sets, plugin+"\x00"+ns)
	}
	pe.dirty = true
	return nil
}

// ClearExplorerSlots removes a namespace's slot overrides (its decorations stay).
func (h *pluginHost) ClearExplorerSlots(plugin, ns string) {
	h.model().plug.explorer.drop(plugin, ns, false)
}

// cleanSlotText sanitizes a slot text and reserves its caps.
func cleanSlotText(t *pluginapi.FileExplorerSlotText) *pluginapi.FileExplorerSlotText {
	out := *t
	out.Text = cleanText(out.Text)
	if len(out.Text) > pluginapi.MaxExplorerSlotText {
		out.Text = out.Text[:pluginapi.MaxExplorerSlotText]
	}
	out.MinWidth = min(max(out.MinWidth, 0), pluginapi.MaxExplorerMinWidth)
	return &out
}

// betterDeco reports whether a wins over b: higher priority, then the alphabetically
// earlier namespace, then the earlier inserted entry.
func betterDeco(a, b explorerDeco, ans, bns string) bool {
	if a.priority != b.priority {
		return a.priority > b.priority
	}
	if ans != bns {
		return ans < bns
	}
	return a.order < b.order
}

// betterSlot is betterDeco for slot entries.
func betterSlot(a, b explorerSlot, ans, bns string) bool {
	if a.slot.Priority != b.slot.Priority {
		return a.slot.Priority > b.slot.Priority
	}
	if ans != bns {
		return ans < bns
	}
	return a.order < b.order
}

// bestDeco is the winning decoration of a path plus the namespace that supplied it.
type bestDeco struct {
	deco explorerDeco
	ns   string
}

// bestSlot is the winning slot entry of a path plus its namespace.
type bestSlot struct {
	entry explorerSlot
	ns    string
}

// rebuild refreshes the resolved index after a change.
func (pe *pluginExplorer) rebuild() {
	pe.deco = map[string]explorerDeco{}
	pe.slots = map[string]explorerSlot{}
	pe.dirty = false
	// Walk in key order so the result does not depend on map iteration order.
	keys := make([]string, 0, len(pe.sets))
	for key := range pe.sets {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	dec := map[string]bestDeco{}
	slots := map[string]bestSlot{}
	for _, key := range keys {
		s := pe.sets[key]
		for path, d := range s.decos {
			old, ok := dec[path]
			if !ok || betterDeco(d, old.deco, s.ns, old.ns) {
				dec[path] = bestDeco{deco: d, ns: s.ns}
				pe.deco[path] = d
			}
		}
		for path, e := range s.slots {
			old, ok := slots[path]
			if !ok || betterSlot(e, old.entry, s.ns, old.ns) {
				slots[path] = bestSlot{entry: e, ns: s.ns}
				pe.slots[path] = e
			}
		}
	}
}

// clampExplorerInt keeps a priority in a sane range.
func clampExplorerInt(n int) int {
	if n > 1_000_000 {
		return 1_000_000
	}
	if n < -1_000_000 {
		return -1_000_000
	}
	return n
}

// rowDecor resolves the plugin decoration of one explorer row. It rebuilds the index when
// a plugin changed the store since the previous frame.
func (m *Model) explorerRowDecor(path string) explorer.RowDecor {
	pe := &m.plug.explorer
	if len(pe.sets) == 0 {
		return explorer.RowDecor{}
	}
	if pe.dirty {
		pe.rebuild()
	}
	var rd explorer.RowDecor
	if e, ok := pe.slots[path]; ok {
		s := e.slot
		rd.SuppressLeading = s.SuppressLeading
		rd.SuppressTrailing = s.SuppressTrailing
		if s.Leading != nil {
			rd.Leading = s.Leading.Text
			rd.LeadingStyle = grid.Style{FG: m.pluginExplorerColor(s.Leading.Color)}
			rd.LeadingMinWidth = s.Leading.MinWidth
		}
		if s.Trailing != nil {
			rd.Trailing = s.Trailing.Text
			rd.TrailingStyle = grid.Style{FG: m.pluginExplorerColor(s.Trailing.Color)}
		}
		if !s.SuppressNameColor {
			rd.NameColor = m.pluginExplorerColor(s.NameColor)
		}
	}
	if !rd.SuppressTrailing && rd.Trailing == "" {
		if d, ok := pe.deco[path]; ok {
			rd.Trailing = d.symbol
			rd.TrailingStyle = grid.Style{FG: m.pluginExplorerColor(d.color)}
		}
	}
	return rd
}

// pluginExplorerColor resolves a plugin colour through the theme; unset is nil.
func (m *Model) pluginExplorerColor(c pluginapi.Color) (col color.Color) {
	return style.ResolveColor(m.theme, decorColor(c))
}
