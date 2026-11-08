package pluginpanel

import (
	"strings"
	"testing"

	"github.com/tbxark/gvim/internal/tui/grid"
	"github.com/tbxark/gvim/pluginapi"
)

func label(text, key string) pluginapi.Widget {
	return pluginapi.Widget{Kind: pluginapi.WidgetLabel, Text: text, Key: key}
}

func panel(root pluginapi.Widget, focus string) pluginapi.WidgetPanel {
	return pluginapi.WidgetPanel{Root: root, FocusKey: focus}
}

func draw(p *Panel, w, h int) (*grid.Grid, []string) {
	g := grid.New(w, h)
	p.Draw(g, Rect{X: 0, Y: 0, W: w, H: h})
	return g, g.Plain()
}

func TestDrawColumnOfWidgets(t *testing.T) {
	p := New(panel(pluginapi.Widget{Kind: pluginapi.WidgetCol, Children: []pluginapi.Widget{
		label("heading", "h"),
		{Kind: pluginapi.WidgetRow, Children: []pluginapi.Widget{
			{Kind: pluginapi.WidgetButton, Key: "ok", Label: "OK"},
			{Kind: pluginapi.WidgetSpacer, Columns: 1},
			{Kind: pluginapi.WidgetToggle, Key: "wrap", Label: "Wrap", Checked: true},
		}},
		{Kind: pluginapi.WidgetDivider},
		{Kind: pluginapi.WidgetList, Key: "files", Items: []pluginapi.WidgetItem{
			{Text: "one"}, {Text: "two", Detail: "2"},
		}},
	}}, "ok"), Options{})
	g, rows := draw(p, 30, 10)
	for i := range rows {
		rows[i] = strings.TrimRight(rows[i], " ")
	}
	if rows[0] != "heading" {
		t.Errorf("row 0 = %q, want the label", rows[0])
	}
	if got := rows[1]; got != "[OK] [x] Wrap" {
		t.Errorf("row 1 = %q, want the button, spacer and toggle", got)
	}
	if rows[2][0] == 0 {
		t.Errorf("the divider row is empty")
	}
	if !contains(rows[3], "one") || !contains(rows[4], "two") {
		t.Errorf("list rows = %q", rows[3:5])
	}
	// The focus ring is the button and the toggle, and the list: depth-first order.
	if len(p.focusables) != 3 {
		t.Fatalf("focusables = %+v, want three", p.focusables)
	}
	if _, ok := p.hitAt(1, 1); !ok {
		t.Errorf("no hit on the button cell")
	}
	_ = g
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func TestKeyFocusAndActivation(t *testing.T) {
	root := pluginapi.Widget{Kind: pluginapi.WidgetCol, Children: []pluginapi.Widget{
		{Kind: pluginapi.WidgetButton, Key: "ok", Label: "OK"},
		{Kind: pluginapi.WidgetToggle, Key: "wrap", Label: "Wrap"},
		{Kind: pluginapi.WidgetList, Key: "items", Items: []pluginapi.WidgetItem{{Key: "a", Text: "a"}, {Key: "b", Text: "b"}}},
	}}
	p := New(panel(root, "ok"), Options{})
	p.SetFocused(true)
	draw(p, 20, 8)

	if p.FocusedKey() != "ok" {
		t.Fatalf("focus = %q, want ok", p.FocusedKey())
	}
	r := p.HandleKey(Key{Name: "tab"})
	if !r.Handled || len(r.Events) != 1 || r.Events[0].Event != "focus" || r.Events[0].Widget != "wrap" {
		t.Fatalf("tab = %+v, want focus wrap", r)
	}
	r = p.HandleKey(Key{Name: "enter"})
	if len(r.Events) != 1 || r.Events[0].Event != "toggle" || r.Events[0].Checked != true {
		t.Fatalf("enter on a toggle = %+v, want toggle true", r)
	}
	// The instance state keeps the flip across an update that re-sends checked=false.
	p.Update(panel(root, "wrap"))
	if !p.checked(root.Children[1]) {
		t.Fatalf("the checked state was lost on update")
	}
	r = p.HandleKey(Key{Name: "tab"})
	if r.Events[0].Widget != "items" {
		t.Fatalf("second tab = %+v, want the list", r)
	}
	r = p.HandleKey(Key{Name: "down"})
	if len(r.Events) != 1 || r.Events[0].Event != "select" || r.Events[0].Index != 1 {
		t.Fatalf("down on a list = %+v, want select 1", r)
	}
	r = p.HandleKey(Key{Name: "enter"})
	if len(r.Events) != 1 || r.Events[0].Event != "activate" || r.Events[0].Widget != "b" || r.Events[0].Index != 1 {
		t.Fatalf("enter on a list = %+v, want activate b", r)
	}
}

func TestMouseClickHits(t *testing.T) {
	root := pluginapi.Widget{Kind: pluginapi.WidgetCol, Children: []pluginapi.Widget{
		{Kind: pluginapi.WidgetButton, Key: "ok", Label: "OK"},
		{Kind: pluginapi.WidgetToggle, Key: "wrap", Label: "Wrap", Checked: true},
	}}
	p := New(panel(root, ""), Options{})
	p.SetFocused(true)
	draw(p, 20, 5)
	if p.FocusedKey() != "ok" {
		t.Fatalf("focus = %q", p.FocusedKey())
	}
	// A click on the toggle row flips it and reports the click first.
	res, inside := p.HandleMouse(Mouse{X: 0, Y: 1, Button: "left"})
	if !inside || !res.Handled {
		t.Fatalf("click was not taken: %+v", res)
	}
	if len(res.Events) != 3 {
		t.Fatalf("events = %+v, want mouse_click, focus and toggle", res.Events)
	}
	if res.Events[0].Event != "mouse_click" || res.Events[2].Event != "toggle" || res.Events[2].Checked != false {
		t.Fatalf("events = %+v", res.Events)
	}
	// Outside the rectangle the panel refuses the message.
	if _, inside := p.HandleMouse(Mouse{X: 30, Y: 1, Button: "left"}); inside {
		t.Fatal("a click outside the panel was taken")
	}
	// Wheel events carry the delta and move a list selection.
	if res, inside := p.HandleMouse(Mouse{X: 0, Y: 1, Wheel: 1}); !inside || len(res.Events) != 1 || res.Events[0].Event != "mouse_wheel" {
		t.Fatalf("wheel = %+v", res)
	}
}

func TestMouseMoveIsCoalescedPerCell(t *testing.T) {
	p := New(panel(label("x", ""), ""), Options{})
	p.SetFocused(true)
	draw(p, 10, 3)
	if res, _ := p.HandleMouse(Mouse{X: 1, Y: 1}); len(res.Events) != 1 || res.Events[0].Event != "mouse_move" {
		t.Fatalf("first move = %+v", res)
	}
	if res, _ := p.HandleMouse(Mouse{X: 1, Y: 1}); len(res.Events) != 0 {
		t.Fatalf("a move within the same cell was reported again: %+v", res)
	}
	if res, _ := p.HandleMouse(Mouse{X: 2, Y: 1}); len(res.Events) != 1 {
		t.Fatalf("the next cell was not reported")
	}
}

func TestTreeVisibilityAndExpansion(t *testing.T) {
	w := pluginapi.Widget{Kind: pluginapi.WidgetTree, Key: "tree", Nodes: []pluginapi.WidgetNode{
		{Key: "a", Text: "a", Depth: 0, Expanded: false},
		{Key: "a1", Text: "a1", Depth: 1},
		{Key: "b", Text: "b", Depth: 0},
	}}
	p := New(panel(w, ""), Options{})
	p.SetFocused(true)
	vis := p.visible(w)
	if len(vis) != 2 || vis[0].node.Key != "a" || vis[1].node.Key != "b" {
		t.Fatalf("visible = %+v, want a and b", vis)
	}
	if !vis[0].children {
		t.Fatal("a is not a parent")
	}
	// Expanding the parent shows the child.
	p.setExpanded(w, w.Nodes[0], true)
	vis = p.visible(w)
	if len(vis) != 3 || vis[1].node.Key != "a1" {
		t.Fatalf("visible after expanding = %+v", vis)
	}
	// The tree is focusable: Home/End move the selection among visible nodes.
	draw(p, 20, 6)
	r := p.HandleKey(Key{Name: "end"})
	if len(r.Events) != 1 || r.Events[0].Event != "select" || r.Events[0].Index != 2 {
		t.Fatalf("end = %+v, want select node b", r)
	}
}

func TestMutations(t *testing.T) {
	root := pluginapi.Widget{Kind: pluginapi.WidgetCol, Children: []pluginapi.Widget{
		{Kind: pluginapi.WidgetToggle, Key: "wrap", Label: "Wrap"},
		{Kind: pluginapi.WidgetList, Key: "files", Items: []pluginapi.WidgetItem{{Text: "a"}, {Text: "b"}, {Text: "c"}}},
		label("old", "title"),
	}}
	p := New(panel(root, ""), Options{})
	if !p.Mutate(pluginapi.WidgetMutation{Kind: "setChecked", WidgetKey: "wrap", Checked: true}) {
		t.Fatal("setChecked was not applied")
	}
	if !p.checked(root.Children[0]) {
		t.Fatal("the toggle was not checked")
	}
	if !p.Mutate(pluginapi.WidgetMutation{Kind: "setSelectedIndex", WidgetKey: "files", Index: 2}) {
		t.Fatal("setSelectedIndex was not applied")
	}
	if got := p.selected(root.Children[1]); got != 2 {
		t.Fatalf("selected = %d, want 2", got)
	}
	if !p.Mutate(pluginapi.WidgetMutation{Kind: "setValue", WidgetKey: "title", Value: "new"}) {
		t.Fatal("setValue was not applied")
	}
	if got := p.value(root.Children[2]); got != "new" {
		t.Fatalf("value = %q, want new", got)
	}
	// A mutation of a key the spec does not have is reported as not applied.
	if p.Mutate(pluginapi.WidgetMutation{Kind: "setChecked", WidgetKey: "nope", Checked: true}) {
		t.Fatal("an unknown key was applied")
	}
	// Out-of-range selections clamp.
	p.Mutate(pluginapi.WidgetMutation{Kind: "setSelectedIndex", WidgetKey: "files", Index: 99})
	if got := p.selected(root.Children[1]); got != 2 {
		t.Fatalf("clamped selected = %d, want 2", got)
	}
}

func TestClosableEscape(t *testing.T) {
	p := New(panel(label("x", ""), ""), Options{Closable: true})
	p.SetFocused(true)
	if r := p.HandleKey(Key{Name: "escape"}); !r.Handled || !r.Close {
		t.Fatalf("escape = %+v, want close", r)
	}
	q := New(panel(label("x", ""), ""), Options{})
	q.SetFocused(true)
	if r := q.HandleKey(Key{Name: "escape"}); !r.Handled || r.Close {
		t.Fatalf("escape of a non-closable panel = %+v, want handled without close", r)
	}
	// A panel without the keyboard takes no key.
	u := New(panel(label("x", ""), ""), Options{})
	if r := u.HandleKey(Key{Name: "enter"}); r.Handled {
		t.Fatalf("an unfocused panel took a key: %+v", r)
	}
	// A focused panel with no focusable widget still takes the activation key.
	if r := q.HandleKey(Key{Name: "enter"}); !r.Handled {
		t.Fatalf("an empty focused panel refused enter: %+v", r)
	}
}

func TestWidgetCommand(t *testing.T) {
	root := pluginapi.Widget{Kind: pluginapi.WidgetCol, Children: []pluginapi.Widget{
		{Kind: pluginapi.WidgetButton, Key: "a", Label: "A"},
		{Kind: pluginapi.WidgetButton, Key: "b", Label: "B"},
	}}
	p := New(panel(root, "a"), Options{})
	p.SetFocused(true)
	r := p.HandleAction(pluginapi.WidgetAction{Kind: "focusAdvance", Delta: 1})
	if len(r.Events) != 1 || r.Events[0].Widget != "b" {
		t.Fatalf("focusAdvance = %+v, want b", r)
	}
	r = p.HandleAction(pluginapi.WidgetAction{Kind: "activate"})
	if len(r.Events) != 1 || r.Events[0].Event != "activate" || r.Events[0].Widget != "b" {
		t.Fatalf("activate = %+v, want b", r)
	}
	// selectMove on a button is handled and does nothing.
	if r := p.HandleAction(pluginapi.WidgetAction{Kind: "selectMove", Delta: 1}); !r.Handled {
		t.Fatal("selectMove was not handled")
	}
}

func TestValidateCaps(t *testing.T) {
	deep := pluginapi.Widget{Kind: pluginapi.WidgetCol}
	cur := &deep
	for i := 0; i < MaxDepth+1; i++ {
		child := pluginapi.Widget{Kind: pluginapi.WidgetCol}
		cur.Children = []pluginapi.Widget{child}
		cur = &cur.Children[0]
	}
	if err := Validate(panel(deep, "")); err == nil || pluginapi.CodeOf(err) != pluginapi.CodeLimit {
		t.Fatalf("depth %d: err = %v, want limit", MaxDepth+1, err)
	}
	items := make([]pluginapi.WidgetItem, MaxItems+1)
	if err := Validate(panel(pluginapi.Widget{Kind: pluginapi.WidgetList, Items: items}, "")); err == nil || pluginapi.CodeOf(err) != pluginapi.CodeLimit {
		t.Fatalf("items: err = %v, want limit", err)
	}
	nodes := make([]pluginapi.WidgetNode, MaxNodes+1)
	if err := Validate(panel(pluginapi.Widget{Kind: pluginapi.WidgetTree, Nodes: nodes}, "")); err == nil || pluginapi.CodeOf(err) != pluginapi.CodeLimit {
		t.Fatalf("nodes: err = %v, want limit", err)
	}
	long := make([]byte, MaxString+1)
	for i := range long {
		long[i] = 'x'
	}
	if err := Validate(panel(label(string(long), ""), "")); err == nil || pluginapi.CodeOf(err) != pluginapi.CodeLimit {
		t.Fatalf("string: err = %v, want limit", err)
	}
	if err := Validate(panel(pluginapi.Widget{Kind: "nope"}, "")); err == nil || pluginapi.CodeOf(err) != pluginapi.CodeInvalid {
		t.Fatalf("kind: err = %v, want invalid", err)
	}
	if err := Validate(panel(pluginapi.Widget{Kind: pluginapi.WidgetDivider, Text: "ab"}, "")); err == nil || pluginapi.CodeOf(err) != pluginapi.CodeInvalid {
		t.Fatalf("divider: err = %v, want invalid", err)
	}
	// The boundary is allowed.
	if err := Validate(panel(pluginapi.Widget{Kind: pluginapi.WidgetList, Items: make([]pluginapi.WidgetItem, MaxItems)}, "")); err != nil {
		t.Fatalf("the item cap boundary was refused: %v", err)
	}
}

func TestValidateWidgetCount(t *testing.T) {
	children := make([]pluginapi.Widget, MaxWidgets)
	for i := range children {
		children[i] = pluginapi.Widget{Kind: pluginapi.WidgetLabel, Text: "x"}
	}
	err := Validate(panel(pluginapi.Widget{Kind: pluginapi.WidgetCol, Children: children}, ""))
	if err == nil || pluginapi.CodeOf(err) != pluginapi.CodeLimit {
		t.Fatalf("widget count: err = %v, want limit", err)
	}
}

func TestSanitize(t *testing.T) {
	if got := Sanitize("a\x1b[31mb"); got != "a [31mb" {
		t.Fatalf("Sanitize = %q", got)
	}
	if got := Sanitize("plain text"); got != "plain text" {
		t.Fatalf("Sanitize changed a clean string: %q", got)
	}
}

func TestEmptyPanel(t *testing.T) {
	p := New(pluginapi.WidgetPanel{}, Options{})
	p.SetFocused(true)
	draw(p, 10, 3)
	if r := p.HandleKey(Key{Name: "tab"}); !r.Handled {
		t.Fatalf("tab on an empty panel = %+v, want handled", r)
	}
	if r := p.HandleKey(Key{Name: "down"}); !r.Handled {
		t.Fatalf("down on an empty panel = %+v", r)
	}
}
