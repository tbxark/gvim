package pluginpanel

import (
	"testing"

	"github.com/tbxark/gvim/pluginapi"
)

func TestTreeDragEvents(t *testing.T) {
	tree := pluginapi.Widget{Kind: pluginapi.WidgetTree, Key: "tree", Nodes: []pluginapi.WidgetNode{
		{Key: "a", Text: "alpha", Depth: 0, Draggable: true},
		{Key: "b", Text: "beta", Depth: 0},
	}}
	p, _ := interactivePanel(tree)
	if p.FocusedKey() != "tree" {
		t.Fatalf("focus = %q", p.FocusedKey())
	}
	// Press on the draggable row starts a drag.
	_, _ = p.HandleMouse(Mouse{X: 1, Y: 0, Button: "left"})
	if !p.Dragging() {
		t.Fatal("the press did not start a drag")
	}
	// Motion to the second row reports the drag target.
	res, _ := p.HandleMouse(Mouse{X: 1, Y: 1})
	if len(res.Events) != 2 {
		t.Fatalf("motion events = %+v", res.Events)
	}
	drag := res.Events[1]
	if drag.Event != "drag" || drag.Widget != "a" || drag.TargetKey != "b" || drag.TargetIndex != 1 {
		t.Fatalf("drag event = %+v", drag)
	}
	// Repeating the same target reports nothing new (only the mouse_move).
	res, _ = p.HandleMouse(Mouse{X: 2, Y: 1})
	if len(res.Events) != 1 || res.Events[0].Event != "mouse_move" {
		t.Fatalf("same-target motion = %+v", res.Events)
	}
	// Release on the target drops the row and ends the drag.
	res, _ = p.HandleMouse(Mouse{X: 1, Y: 1, Release: true})
	if len(res.Events) != 2 || res.Events[0].Event != "drop" || res.Events[1].Event != "dragend" {
		t.Fatalf("release = %+v", res.Events)
	}
	if res.Events[0].TargetKey != "b" || res.Events[0].Widget != "a" {
		t.Fatalf("drop = %+v", res.Events[0])
	}
	if p.Dragging() {
		t.Fatal("the drag survived the release")
	}
	// A press on a non-draggable row starts no drag.
	_, _ = p.HandleMouse(Mouse{X: 1, Y: 1, Button: "left"})
	if p.Dragging() {
		t.Fatal("a non-draggable row started a drag")
	}
}

func TestTreeDragPreviewMutation(t *testing.T) {
	tree := pluginapi.Widget{Kind: pluginapi.WidgetTree, Key: "tree", Nodes: []pluginapi.WidgetNode{
		{Key: "a", Text: "alpha", Depth: 0},
		{Key: "b", Text: "beta", Depth: 0},
	}}
	p, g := interactivePanel(tree)
	if !p.Mutate(pluginapi.WidgetMutation{Kind: "previewTreeMove", WidgetKey: "tree", SourceKey: "a", AfterKey: "b", Label: "MOVED"}) {
		t.Fatal("previewTreeMove not applied")
	}
	p.Draw(g, Rect{X: 0, Y: 0, W: 40, H: 10})
	plain := g.Plain()
	if !stringsContains(plain[0], "beta") || !stringsContains(plain[1], "MOVED") {
		t.Fatalf("preview rows = %q", plain[:3])
	}
	if !p.Mutate(pluginapi.WidgetMutation{Kind: "endTreeMovePreview", WidgetKey: "tree"}) {
		t.Fatal("endTreeMovePreview not applied")
	}
	p.Draw(g, Rect{X: 0, Y: 0, W: 40, H: 10})
	if !stringsContains(g.Plain()[0], "alpha") {
		t.Fatalf("preview did not end: %q", g.Plain()[:2])
	}
}
