package tui

import (
	"testing"

	"github.com/tbxark/gvim/pluginapi"
)

// TestBufferGroupHost checks the group-lite host: a two-panel group builds two splits with
// the panel buffers, panel content/title/visibility/focus work, and closing it restores the
// split tree and closes the panel buffers.
func TestBufferGroupHost(t *testing.T) {
	t.Parallel()
	rt := newFakeRuntime(t)
	m, _, pe := newPluginEditorModel(t, rt)
	h := m.plug.host
	if got := len(m.windowLeaves()); got != 1 {
		t.Fatalf("leaves at start = %d", got)
	}
	layout := pluginapi.BufferGroupLayout{Dir: "h", Ratio: 0.5,
		First:  &pluginapi.BufferGroupLayout{Leaf: &pluginapi.BufferGroupLeaf{Name: "files"}},
		Second: &pluginapi.BufferGroupLayout{Leaf: &pluginapi.BufferGroupLeaf{Name: "preview"}},
	}
	if err := h.CreateBufferGroup("p", 5, pluginapi.BufferGroupSpec{Name: "Search", Layout: layout}); err != nil {
		t.Fatal(err)
	}
	if got := len(m.windowLeaves()); got != 2 {
		t.Fatalf("leaves after the group = %d, want 2", got)
	}
	panels := h.BufferGroupPanels(5)
	if len(panels) != 2 || panels[0].Name != "files" || panels[1].Name != "preview" || !panels[0].Visible {
		t.Fatalf("panels = %+v", panels)
	}
	if len(pe.created) != 2 {
		t.Fatalf("created buffers = %v", pe.created)
	}
	// Panel content lands on the panel buffer.
	entries := []pluginapi.ContentEntry{{Text: "main.go\n", Props: map[string]any{"file": "main.go"}}}
	if err := h.SetPanelContent("p", 5, "files", entries); err != nil {
		t.Fatal(err)
	}
	if got := pe.contents[panels[0].Buffer]; len(got) != 1 || got[0].Props["file"] != "main.go" {
		t.Fatalf("content = %+v", got)
	}
	if err := h.SetPanelContent("q", 5, "files", entries); err == nil {
		t.Fatal("another plugin set the group's panel content")
	}
	// Title segments are validated against the panels.
	if err := h.SetBufferGroupTitle(5, []pluginapi.BufferGroupTitleSegment{{Panel: "nope"}}); err == nil {
		t.Fatal("an unknown title panel was accepted")
	}
	if err := h.SetBufferGroupTitle(5, []pluginapi.BufferGroupTitleSegment{{Text: "Search: "}, {Panel: "files"}}); err != nil {
		t.Fatal(err)
	}
	if err := h.FocusBufferGroupPanel(5, "files"); err != nil {
		t.Fatal(err)
	}
	// Hide and re-show a panel.
	if err := h.SetBufferGroupPanelVisible(5, "preview", false); err != nil {
		t.Fatal(err)
	}
	if got := len(m.windowLeaves()); got != 1 {
		t.Fatalf("leaves after hiding = %d, want 1", got)
	}
	if err := h.SetBufferGroupPanelVisible(5, "preview", true); err != nil {
		t.Fatal(err)
	}
	if got := len(m.windowLeaves()); got != 2 {
		t.Fatalf("leaves after showing = %d, want 2", got)
	}
	// A second mount of the same id is busy.
	if err := h.CreateBufferGroup("p", 5, pluginapi.BufferGroupSpec{Name: "x", Layout: layout}); err == nil {
		t.Fatal("a duplicate group id mounted")
	}
	// Close restores the single-window frame and closes the panel buffers it made.
	h.CloseBufferGroup(5)
	if got := len(m.windowLeaves()); got != 1 {
		t.Fatalf("leaves after closing = %d, want 1", got)
	}
	for _, id := range pe.created {
		if _, ok := pe.Buffer(id); ok {
			t.Fatalf("panel buffer %d survived the group", id)
		}
	}
	if h.BufferGroupPanels(5) != nil {
		t.Fatal("a closed group still lists panels")
	}
}

// TestScrollFollowMapping checks the anchor interpolation of the scroll-sync follower.
func TestScrollFollowMapping(t *testing.T) {
	t.Parallel()
	anchors := []pluginapi.ScrollAnchor{{Left: 0, Right: 0}, {Left: 10, Right: 20}, {Left: 20, Right: 40}}
	cases := []struct{ line, want int }{{0, 0}, {5, 10}, {10, 20}, {15, 30}, {20, 40}, {25, 50}}
	for _, c := range cases {
		if got := scrollFollow(anchors, c.line, true); got != c.want {
			t.Errorf("follow(%d) = %d, want %d", c.line, got, c.want)
		}
	}
	// The reverse direction uses the right lines to map to the left.
	if got := scrollFollow(anchors, 20, false); got != 10 {
		t.Errorf("reverse follow(20) = %d, want 10", got)
	}
	// No anchors is the identity.
	if got := scrollFollow(nil, 7, true); got != 7 {
		t.Errorf("identity = %d", got)
	}
}
