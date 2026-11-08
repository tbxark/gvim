package tui

import (
	"math"
	"sort"

	"github.com/tbxark/gvim/pluginapi"
)

// Buffer groups and scroll-sync groups of the plugin host (plugin API tier 1b, "group-lite").
//
// A group is a layout of panels shown as splits of the current tab. Each panel is either a
// read-only virtual buffer the host creates for the group (the plugin sets its styled
// content with SetPanelContent) or an existing buffer shown in its own window. gvim has no
// synthetic group tab: the group owns the split windows it created and the panel buffers it
// made, and CloseBufferGroup / OwnerGone closes them. The plugin's title segments are kept
// for getBufferGroupPanels and the ports but are not painted in a tab (tier 1c).
//
// A scroll-sync group ties two split windows: the follower's top line follows the leader's
// through the anchors the plugin sets (linear interpolation between pairs). The root calls
// syncScrollGroups after every update.

// pluginGroups is the state of the groups and scroll-sync groups of the plugins.
type pluginGroups struct {
	byID   map[pluginapi.BufferGroupID]*mountedGroup
	order  []pluginapi.BufferGroupID
	scroll map[uint64]*scrollGroup
}

// mountedGroup is one mounted buffer group.
type mountedGroup struct {
	id     pluginapi.BufferGroupID
	owner  string
	name   string
	title  []pluginapi.BufferGroupTitleSegment
	panels []*groupPanel
}

// groupPanel is one panel of a mounted group.
type groupPanel struct {
	name    string
	buffer  pluginapi.BufferID
	window  pluginapi.WinID // 0 when hidden
	visible bool
	created bool // the panel buffer is the group's (a virtual buffer it made)
	// fixedHeight asks for a fixed display height (a fixed layout leaf), 0 for none.
	fixedHeight int
}

// scrollGroup is one scroll-sync group.
type scrollGroup struct {
	owner               string
	left                pluginapi.WinID
	right               pluginapi.WinID
	anchors             []pluginapi.ScrollAnchor
	lastLeft, lastRight int
	inited              bool
}

// group returns the mounted group id, nil when none.
func (ps *pluginGroups) group(id pluginapi.BufferGroupID) *mountedGroup {
	return ps.byID[id]
}

// panelOf returns the named panel of a group, nil when it is not in it.
func (g *mountedGroup) panelOf(name string) *groupPanel {
	for _, p := range g.panels {
		if p.name == name {
			return p
		}
	}
	return nil
}

// maxGroupPanels is the most panels one group may have.
const maxGroupPanels = 8

// maxGroupDepth is the deepest layout tree a group may have.
const maxGroupDepth = 4

// flattenLeaves returns the leaves of a layout in painting order and the layout's depth.
func flattenLeaves(node *pluginapi.BufferGroupLayout, out *[]pluginapi.BufferGroupLeaf, depth int) (int, bool) {
	if node == nil {
		return depth, false
	}
	if node.Leaf != nil {
		*out = append(*out, *node.Leaf)
		return depth, true
	}
	d1, ok1 := flattenLeaves(node.First, out, depth+1)
	d2, ok2 := flattenLeaves(node.Second, out, depth+1)
	return max(d1, d2), ok1 && ok2
}

// CreateBufferGroup mounts a group: one split per panel, a panel buffer for each leaf without
// an existing buffer. On a failure it closes what it already created.
func (h *pluginHost) CreateBufferGroup(plugin string, id pluginapi.BufferGroupID, o pluginapi.BufferGroupSpec) error {
	m := h.m
	if id == 0 {
		return pluginapi.Errf(pluginapi.CodeInvalid, "a group id is not 0")
	}
	ps := &m.plug.groups
	if ps.group(id) != nil {
		return pluginapi.Errf(pluginapi.CodeBusy, "group %d is already mounted", id)
	}
	var leaves []pluginapi.BufferGroupLeaf
	depth, ok := flattenLeaves(&o.Layout, &leaves, 0)
	if !ok || len(leaves) == 0 {
		return pluginapi.Errf(pluginapi.CodeInvalid, "a group needs a layout with at least one leaf")
	}
	if depth > maxGroupDepth {
		return pluginapi.Errf(pluginapi.CodeLimit, "a group layout is at most %d deep", maxGroupDepth)
	}
	if len(leaves) > maxGroupPanels {
		return pluginapi.Errf(pluginapi.CodeLimit, "a group has at most %d panels", maxGroupPanels)
	}
	names := map[string]bool{}
	for _, leaf := range leaves {
		if leaf.Name == "" {
			return pluginapi.Errf(pluginapi.CodeInvalid, "a group panel needs a name")
		}
		if names[leaf.Name] {
			return pluginapi.Errf(pluginapi.CodeInvalid, "duplicate group panel %q", leaf.Name)
		}
		names[leaf.Name] = true
		if leaf.Buffer != 0 {
			if _, ok := h.ed().Buffer(leaf.Buffer); !ok {
				return pluginapi.Errf(pluginapi.CodeClosed, "buffer %d is closed", leaf.Buffer)
			}
		}
	}
	if ps.byID == nil {
		ps.byID = map[pluginapi.BufferGroupID]*mountedGroup{}
	}
	g := &mountedGroup{id: id, owner: plugin, name: cleanText(o.Name)}
	start := h.CurrentWindow()
	if start == 0 {
		return pluginapi.Errf(pluginapi.CodeNotFound, "there is no window to build the group in")
	}
	var walk func(node *pluginapi.BufferGroupLayout, win pluginapi.WinID) error
	walk = func(node *pluginapi.BufferGroupLayout, win pluginapi.WinID) error {
		if node.Leaf != nil {
			if err := h.mountGroupLeaf(g, *node.Leaf, win); err != nil {
				return err
			}
			return nil
		}
		dir := "right"
		if node.Dir == "v" {
			dir = "below"
		}
		if node.Dir != "" && node.Dir != "h" && node.Dir != "v" {
			return pluginapi.Errf(pluginapi.CodeInvalid, "layout direction must be \"h\" or \"v\", not %q", node.Dir)
		}
		// BufferGroupLayout.Ratio is the share of the FIRST child; SplitOptions.Ratio is
		// the share of the new (second) window.
		ratio := node.Ratio
		if ratio < 0 || ratio > 1 {
			ratio = 0
		}
		newShare := 0.0
		if ratio > 0 {
			newShare = 1 - ratio
		}
		newWin, err := h.SplitWindow(win, pluginapi.SplitOptions{Dir: dir, Ratio: newShare})
		if err != nil {
			return err
		}
		if err := walk(node.First, win); err != nil {
			return err
		}
		return walk(node.Second, newWin)
	}
	if err := walk(&o.Layout, start); err != nil {
		h.CloseBufferGroup(id)
		return err
	}
	ps.byID[id] = g
	ps.order = append(ps.order, id)
	m.applyGroupFixedHeights()
	return nil
}

// mountGroupLeaf shows one panel in win: an existing buffer, or a panel buffer the group owns.
func (h *pluginHost) mountGroupLeaf(g *mountedGroup, leaf pluginapi.BufferGroupLeaf, win pluginapi.WinID) error {
	buf := leaf.Buffer
	created := false
	if buf == 0 {
		id, err := h.ed().CreateBuffer(pluginapi.CreateBuffer{
			Plugin: g.owner, Name: leaf.Name, ReadOnly: true,
		})
		if err != nil {
			return err
		}
		buf, created = id, true
		if h.m.plug.virtual.owner == nil {
			h.m.plug.virtual.owner = map[pluginapi.BufferID]string{}
		}
		h.m.plug.virtual.owner[id] = g.owner
	}
	if err := h.SetWindowBuffer(win, buf); err != nil {
		if created {
			_ = h.Close(buf, true)
		}
		return err
	}
	p := &groupPanel{name: leaf.Name, buffer: buf, window: win, visible: true, created: created}
	if leaf.Fixed {
		p.fixedHeight = leaf.Height
	}
	g.panels = append(g.panels, p)
	return nil
}

// SetPanelContent replaces the styled content of one panel buffer.
func (h *pluginHost) SetPanelContent(plugin string, id pluginapi.BufferGroupID, panel string, entries []pluginapi.ContentEntry) error {
	g := h.m.plug.groups.group(id)
	if g == nil {
		return pluginapi.Errf(pluginapi.CodeClosed, "group %d is not mounted", id)
	}
	if g.owner != plugin {
		return pluginapi.Errf(pluginapi.CodeInvalid, "group %d belongs to a different plugin", id)
	}
	p := g.panelOf(panel)
	if p == nil {
		return pluginapi.Errf(pluginapi.CodeNotFound, "no panel %q in group %d", panel, id)
	}
	if !p.created {
		return pluginapi.Errf(pluginapi.CodeInvalid, "panel %q shows an ordinary buffer; set its content through the buffer API", panel)
	}
	return h.ed().SetContent(p.buffer, entries)
}

// SetBufferGroupTitle replaces the group's title segments (kept for the panel listing; gvim
// has no group tab to paint them in yet).
func (h *pluginHost) SetBufferGroupTitle(id pluginapi.BufferGroupID, segments []pluginapi.BufferGroupTitleSegment) error {
	g := h.m.plug.groups.group(id)
	if g == nil {
		return pluginapi.Errf(pluginapi.CodeClosed, "group %d is not mounted", id)
	}
	if len(segments) > 16 {
		return pluginapi.Errf(pluginapi.CodeLimit, "a group title has at most 16 segments")
	}
	clean := make([]pluginapi.BufferGroupTitleSegment, 0, len(segments))
	for _, s := range segments {
		if s.Panel != "" && g.panelOf(s.Panel) == nil {
			return pluginapi.Errf(pluginapi.CodeNotFound, "no panel %q in group %d", s.Panel, id)
		}
		clean = append(clean, pluginapi.BufferGroupTitleSegment{Text: cleanText(s.Text), Panel: s.Panel})
	}
	g.title = clean
	return nil
}

// SetBufferGroupPanelVisible hides (closing its window) or re-shows (splitting a visible
// panel's window) one panel, keeping the panel buffer and its content.
func (h *pluginHost) SetBufferGroupPanelVisible(id pluginapi.BufferGroupID, panel string, visible bool) error {
	m := h.m
	g := m.plug.groups.group(id)
	if g == nil {
		return pluginapi.Errf(pluginapi.CodeClosed, "group %d is not mounted", id)
	}
	p := g.panelOf(panel)
	if p == nil {
		return pluginapi.Errf(pluginapi.CodeNotFound, "no panel %q in group %d", panel, id)
	}
	if visible == p.visible {
		return nil
	}
	if !visible {
		if p.window != 0 {
			if err := m.closeSplitByID(uint64(p.window)); err != nil && pluginapi.CodeOf(err) != pluginapi.CodeConflict {
				return err
			}
		}
		p.window, p.visible = 0, false
		return nil
	}
	var host *groupPanel
	for _, other := range g.panels {
		if other.visible && other.window != 0 {
			host = other
			break
		}
	}
	if host == nil {
		return pluginapi.Errf(pluginapi.CodeConflict, "the last visible panel cannot be shown through another")
	}
	newWin, err := h.SplitWindow(host.window, pluginapi.SplitOptions{Dir: "below"})
	if err != nil {
		return err
	}
	if err := h.SetWindowBuffer(newWin, p.buffer); err != nil {
		_ = m.closeSplitByID(uint64(newWin))
		return err
	}
	p.window, p.visible = newWin, true
	return nil
}

// FocusBufferGroupPanel gives the keyboard to a visible panel's window.
func (h *pluginHost) FocusBufferGroupPanel(id pluginapi.BufferGroupID, panel string) error {
	g := h.m.plug.groups.group(id)
	if g == nil {
		return pluginapi.Errf(pluginapi.CodeClosed, "group %d is not mounted", id)
	}
	p := g.panelOf(panel)
	if p == nil || p.window == 0 {
		return pluginapi.Errf(pluginapi.CodeNotFound, "no visible panel %q in group %d", panel, id)
	}
	return h.FocusWindow(p.window)
}

// CloseBufferGroup closes the group's windows and the panel buffers it created.
func (h *pluginHost) CloseBufferGroup(id pluginapi.BufferGroupID) {
	m := h.m
	ps := &m.plug.groups
	g := ps.group(id)
	if g == nil {
		return
	}
	m.plugEmit("buffer_group_closed", map[string]any{"group": uint64(id), "name": g.name})
	for _, p := range g.panels {
		if p.window != 0 {
			_ = m.closeSplitByID(uint64(p.window))
		}
		if p.created {
			delete(m.plug.virtual.owner, p.buffer)
			if _, ok := h.ed().Buffer(p.buffer); ok {
				_ = h.Close(p.buffer, true)
			}
		}
	}
	delete(ps.byID, id)
	for i, got := range ps.order {
		if got == id {
			ps.order = append(ps.order[:i:i], ps.order[i+1:]...)
			break
		}
	}
}

// BufferGroupPanels lists the panels of a group.
func (h *pluginHost) BufferGroupPanels(id pluginapi.BufferGroupID) []pluginapi.BufferGroupPanel {
	g := h.m.plug.groups.group(id)
	if g == nil {
		return nil
	}
	out := make([]pluginapi.BufferGroupPanel, len(g.panels))
	for i, p := range g.panels {
		out[i] = pluginapi.BufferGroupPanel{Name: p.name, Buffer: p.buffer, Window: p.window, Visible: p.visible}
	}
	return out
}

// plugGroupsGone is OwnerGone for the groups and scroll-sync groups.
func (m *Model) plugGroupsGone(plugin string, gen uint64) {
	_ = gen
	h := m.plug.host
	ps := &m.plug.groups
	for _, id := range append([]pluginapi.BufferGroupID(nil), ps.order...) {
		if g := ps.group(id); g != nil && g.owner == plugin {
			if h != nil {
				h.CloseBufferGroup(id)
			} else {
				delete(ps.byID, id)
			}
		}
	}
	for id, sg := range ps.scroll {
		if sg.owner == plugin {
			delete(ps.scroll, id)
		}
	}
}

// ---- scroll sync ----

// CreateScrollSyncGroup ties two split windows together.
func (h *pluginHost) CreateScrollSyncGroup(plugin string, id uint64, left, right pluginapi.WinID) error {
	m := h.m
	if id == 0 {
		return pluginapi.Errf(pluginapi.CodeInvalid, "a scroll sync id is not 0")
	}
	if left == 0 || right == 0 || left == right {
		return pluginapi.Errf(pluginapi.CodeInvalid, "a scroll sync group needs two different windows")
	}
	if m.splitByID(uint64(left)) == nil {
		return closedWindow(left)
	}
	if m.splitByID(uint64(right)) == nil {
		return closedWindow(right)
	}
	ps := &m.plug.groups
	if ps.scroll == nil {
		ps.scroll = map[uint64]*scrollGroup{}
	}
	if _, busy := ps.scroll[id]; busy {
		return pluginapi.Errf(pluginapi.CodeBusy, "scroll sync group %d is already mounted", id)
	}
	ps.scroll[id] = &scrollGroup{owner: plugin, left: left, right: right}
	return nil
}

// SetScrollSyncAnchors replaces a scroll-sync group's anchors.
func (h *pluginHost) SetScrollSyncAnchors(id uint64, anchors []pluginapi.ScrollAnchor) error {
	sg := h.m.plug.groups.scroll[id]
	if sg == nil {
		return pluginapi.Errf(pluginapi.CodeClosed, "scroll sync group %d is not mounted", id)
	}
	if len(anchors) > 10_000 {
		return pluginapi.Errf(pluginapi.CodeLimit, "a scroll sync group takes at most 10000 anchors")
	}
	clean := make([]pluginapi.ScrollAnchor, 0, len(anchors))
	for _, a := range anchors {
		if a.Left < 0 || a.Right < 0 {
			continue
		}
		clean = append(clean, a)
	}
	sort.SliceStable(clean, func(i, j int) bool { return clean[i].Left < clean[j].Left })
	sg.anchors, sg.inited = clean, false
	return nil
}

// RemoveScrollSyncGroup drops a scroll-sync group.
func (h *pluginHost) RemoveScrollSyncGroup(id uint64) {
	delete(h.m.plug.groups.scroll, id)
}

// syncScrollGroups runs after every update: it follows a changed leader top in the follower.
// The leader is whichever window moved while the other did not; a leader that is the focused
// window uses the document's top line.
func (m *Model) syncScrollGroups() {
	if len(m.plug.groups.scroll) == 0 {
		return
	}
	for _, sg := range m.plug.groups.scroll {
		lt, rt := m.leafTopLine(sg.left), m.leafTopLine(sg.right)
		if !sg.inited {
			sg.lastLeft, sg.lastRight, sg.inited = lt, rt, true
			continue
		}
		if lt == sg.lastLeft && rt == sg.lastRight {
			continue
		}
		switch {
		case lt != sg.lastLeft && rt == sg.lastRight:
			m.setLeafTopLine(sg.right, scrollFollow(sg.anchors, lt, true))
		case rt != sg.lastRight && lt == sg.lastLeft:
			m.setLeafTopLine(sg.left, scrollFollow(sg.anchors, rt, false))
		}
		sg.lastLeft, sg.lastRight = m.leafTopLine(sg.left), m.leafTopLine(sg.right)
	}
}

// leafTopLine is the first line window w paints.
func (m *Model) leafTopLine(w pluginapi.WinID) int {
	n := m.splitByID(uint64(w))
	if n == nil {
		return 0
	}
	doc := m.leafDoc(n)
	if n == m.splits.Focus && doc != nil {
		return doc.Top()
	}
	return n.Top
}

// setLeafTopLine scrolls window w to line top.
func (m *Model) setLeafTopLine(w pluginapi.WinID, top int) {
	n := m.splitByID(uint64(w))
	if n == nil {
		return
	}
	doc := m.leafDoc(n)
	top = max(top, 0)
	if doc != nil {
		top = min(top, max(doc.LineCount()-1, 0))
	}
	if n == m.splits.Focus {
		m.ed.ScrollTo(top)
		return
	}
	n.Top = top
}

// scrollFollow maps a leader line to the follower through the anchors (linear interpolation
// between the nearest pairs; the ends extend with the slope of their nearest pair).
func scrollFollow(anchors []pluginapi.ScrollAnchor, line int, fromLeft bool) int {
	if len(anchors) == 0 {
		return line
	}
	get := func(a pluginapi.ScrollAnchor) (int, int) {
		if fromLeft {
			return a.Left, a.Right
		}
		return a.Right, a.Left
	}
	at := func(a pluginapi.ScrollAnchor) (int, int) { return get(a) }
	firstA, firstB := at(anchors[0])
	if line <= firstA {
		return max(firstB-(firstA-line), 0)
	}
	for i := 1; i < len(anchors); i++ {
		aA, aB := at(anchors[i])
		if line <= aA {
			pA, pB := at(anchors[i-1])
			if aA == pA {
				return aB
			}
			return pB + (line-pA)*(aB-pB)/(aA-pA)
		}
	}
	lastA, lastB := at(anchors[len(anchors)-1])
	prevA, prevB := at(anchors[len(anchors)-2])
	slope := 1
	if lastA != prevA {
		slope = (lastB - prevB) / (lastA - prevA)
		if slope == 0 {
			slope = 1
		}
	}
	return max(lastB+(line-lastA)*slope, 0)
}

// applyGroupFixedHeights gives the fixed layout leaves their requested display height by
// setting the ratio of the split above them from the current layout. It runs after every
// update while fixed panels exist, so a window resize re-applies the height. Limits: the
// split border can leave the leaf one row off, a fixed leaf inside a side-by-side ratio
// split is approximate, and a height taller than the body is clamped to fit.
func (m *Model) applyGroupFixedHeights() {
	if len(m.plug.groups.byID) == 0 {
		return
	}
	type fixedPanel struct {
		leaf   *splitNode
		height int
	}
	var fixed []fixedPanel
	for _, g := range m.plug.groups.byID {
		for _, p := range g.panels {
			if p.fixedHeight > 0 && p.window != 0 {
				if n := m.splitByID(uint64(p.window)); n != nil {
					fixed = append(fixed, fixedPanel{leaf: n, height: p.fixedHeight})
				}
			}
		}
	}
	if len(fixed) == 0 {
		return
	}
	panes := m.layout().Panes
	leaves := m.windowLeaves()
	if len(panes) != len(leaves) {
		return
	}
	height := make(map[*splitNode]int, len(leaves))
	for i, n := range leaves {
		height[n] = panes[i].H
	}
	var subtreeHeight func(n *splitNode) int
	subtreeHeight = func(n *splitNode) int {
		if n == nil {
			return 0
		}
		if n.Leaf() {
			return height[n]
		}
		a, b := subtreeHeight(n.A), subtreeHeight(n.B)
		if n.Dir == stacked {
			return a + b + 1 // the split border row
		}
		return max(a, b)
	}
	var parentOf func(root, target *splitNode) *splitNode
	parentOf = func(root, target *splitNode) *splitNode {
		if root == nil || root.Leaf() {
			return nil
		}
		if root.A == target || root.B == target {
			return root
		}
		if p := parentOf(root.A, target); p != nil {
			return p
		}
		return parentOf(root.B, target)
	}
	for _, f := range fixed {
		parent := parentOf(m.splits.Root, f.leaf)
		if parent == nil || parent.Dir != stacked {
			continue
		}
		total := subtreeHeight(parent)
		if total <= 1 {
			continue
		}
		// Split ratios are percentages of the first child. Round up so a one-row leaf is
		// not rounded away, and keep the split usable.
		pct := int(math.Ceil(float64(f.height) * 100 / float64(total)))
		pct = min(max(pct, 1), 99)
		if parent.A != f.leaf {
			pct = 100 - pct
		}
		parent.Ratio = pct
	}
}
