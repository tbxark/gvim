package tui

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/pluginapi"
)

// TestBufferGroupLayoutRatioIsFirstChildShare pins the group-layout ratio semantics: the
// plugin gives the first child's share, the split gives the new window's share. A 0.07
// first child must take 7% (git_log's fixed height-1 toolbar layout), not 93%.
func TestBufferGroupLayoutRatioIsFirstChildShare(t *testing.T) {
	rt := newFakeRuntime(t)
	m, _, _ := newPluginEditorModel(t, rt)
	h := m.plug.host
	layout := pluginapi.BufferGroupLayout{Dir: "v", Ratio: 0.07,
		First:  &pluginapi.BufferGroupLayout{Leaf: &pluginapi.BufferGroupLeaf{Name: "toolbar"}},
		Second: &pluginapi.BufferGroupLayout{Leaf: &pluginapi.BufferGroupLeaf{Name: "body"}},
	}
	if err := h.CreateBufferGroup("p", 7, pluginapi.BufferGroupSpec{Name: "log", Layout: layout}); err != nil {
		t.Fatal(err)
	}
	root := m.splits.Root
	if root == nil || root.Leaf() {
		t.Fatalf("root = %+v, want a split", root)
	}
	if root.Ratio != 7 {
		t.Fatalf("root ratio = %d, want 7 (the first child's share)", root.Ratio)
	}
	h.CloseBufferGroup(7)
}

// TestBufferGroupFixedHeightLeaf checks a fixed layout leaf gets its requested display
// height (git_log's height-1 toolbar) through the split ratio, and keeps it after a
// window resize.
func TestBufferGroupFixedHeightLeaf(t *testing.T) {
	rt := newFakeRuntime(t)
	m, _, _ := newPluginEditorModel(t, rt)
	h := m.plug.host
	layout := pluginapi.BufferGroupLayout{Dir: "v",
		First:  &pluginapi.BufferGroupLayout{Leaf: &pluginapi.BufferGroupLeaf{Name: "toolbar", Fixed: true, Height: 1}},
		Second: &pluginapi.BufferGroupLayout{Leaf: &pluginapi.BufferGroupLeaf{Name: "body"}},
	}
	if err := h.CreateBufferGroup("p", 9, pluginapi.BufferGroupSpec{Name: "log", Layout: layout}); err != nil {
		t.Fatal(err)
	}
	if got := len(m.layout().Panes); got != 2 {
		t.Fatalf("panes = %d", got)
	}
	if got := m.layout().Panes[0].H; got != 1 {
		t.Fatalf("toolbar height = %d, want 1", got)
	}
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	if got := m.layout().Panes[0].H; got != 1 {
		t.Fatalf("toolbar height after resize = %d, want 1", got)
	}
	h.CloseBufferGroup(9)
}
