package pluginpanel

import (
	"testing"

	"github.com/tbxark/gvim/internal/tui/grid"
	"github.com/tbxark/gvim/pluginapi"
)

// interactivePanel draws a panel with one interactive widget focused and returns it.
func interactivePanel(w pluginapi.Widget, o ...func(*Options)) (*Panel, *grid.Grid) {
	root := pluginapi.Widget{Kind: pluginapi.WidgetCol, Children: []pluginapi.Widget{w}}
	opts := Options{}
	for _, f := range o {
		f(&opts)
	}
	p := New(pluginapi.WidgetPanel{Root: root, FocusKey: w.Key}, opts)
	p.SetFocused(true)
	g := grid.New(40, 10)
	p.Draw(g, Rect{X: 0, Y: 0, W: 40, H: 10})
	return p, g
}

func widgetEventOf(t *testing.T, res Result, kind string) pluginapi.WidgetEvent {
	t.Helper()
	for _, e := range res.Events {
		if e.Event == kind {
			return e
		}
	}
	t.Fatalf("no %q event in %+v", kind, res.Events)
	return pluginapi.WidgetEvent{}
}

func TestTextFieldEditing(t *testing.T) {
	w := pluginapi.Widget{Kind: pluginapi.WidgetText, Key: "name", Text: "hello"}
	p, _ := interactivePanel(w)
	if got := p.FocusedKey(); got != "name" {
		t.Fatalf("focus = %q", got)
	}
	// Typing inserts at the caret (the seed's end) and reports a change.
	res := p.HandleKey(Key{Name: "x", Text: "x"})
	ev := widgetEventOf(t, res, "change")
	if ev.Value != "hellox" || ev.CursorByte != 6 {
		t.Fatalf("change = %+v, want hellox at 6", ev)
	}
	// Backspace removes it.
	res = p.HandleKey(Key{Name: "backspace"})
	if ev := widgetEventOf(t, res, "change"); ev.Value != "hello" {
		t.Fatalf("backspace = %+v", ev)
	}
	// A caret move does not report a change but keeps the state.
	res = p.HandleKey(Key{Name: "left"})
	if len(res.Events) != 0 {
		t.Fatalf("left reported %+v", res.Events)
	}
	if got := p.textCursor(w); got != 4 {
		t.Fatalf("cursor = %d, want 4", got)
	}
	// setValue with a cursor mutation repositions the caret.
	if !p.Mutate(pluginapi.WidgetMutation{Kind: "setValue", WidgetKey: "name", Value: "abc", CursorByte: 1, HasCursor: true}) {
		t.Fatal("setValue not applied")
	}
	if got, cur := p.textValue(w), p.textCursor(w); got != "abc" || cur != 1 {
		t.Fatalf("value/cursor = %q/%d", got, cur)
	}
	// Typing replaces the selection when one is set.
	p.setSelection(w, 0, 2)
	p.HandleKey(Key{Name: "z", Text: "z"})
	if got := p.textValue(w); got != "zc" {
		t.Fatalf("replacement = %q, want zc", got)
	}
}

func TestTextReadOnlyNeverEdits(t *testing.T) {
	w := pluginapi.Widget{Kind: pluginapi.WidgetText, Key: "out", Text: "fixed", ReadOnly: true}
	p, _ := interactivePanel(w)
	if res := p.HandleKey(Key{Name: "x", Text: "x"}); len(res.Events) != 0 {
		t.Fatalf("a read-only field emitted %+v", res.Events)
	}
	if res := p.HandleKey(Key{Name: "backspace"}); len(res.Events) != 0 {
		t.Fatalf("backspace on read-only emitted %+v", res.Events)
	}
	if got := p.textValue(w); got != "fixed" {
		t.Fatalf("value changed to %q", got)
	}
}

func TestMultilineTextRows(t *testing.T) {
	w := pluginapi.Widget{Kind: pluginapi.WidgetText, Key: "body", Text: "one\ntwo", Rows: 3}
	p, g := interactivePanel(w)
	p.HandleKey(Key{Name: "down"})
	p.HandleKey(Key{Name: "x", Text: "x"})
	if got := p.textValue(w); got != "one\ntwox" {
		t.Fatalf("value = %q", got)
	}
	rows := g.Plain()
	if !containsCell(rows, 0, "one") || !containsCell(rows, 1, "two") {
		t.Fatalf("painted rows = %q", rows[:4])
	}
}

func containsCell(rows []string, i int, sub string) bool {
	if i >= len(rows) {
		return false
	}
	return stringsContains(rows[i], sub)
}

func stringsContains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func TestNumberControl(t *testing.T) {
	w := pluginapi.Widget{Kind: pluginapi.WidgetNumber, Key: "n", Number: 5, Min: 0, Max: 10, Step: 2}
	p, _ := interactivePanel(w)
	ev := widgetEventOf(t, p.HandleKey(Key{Name: "up"}), "change")
	if ev.Value != "7" {
		t.Fatalf("up = %q, want 7", ev.Value)
	}
	// The upper bound clamps.
	p.HandleKey(Key{Name: "up"})
	p.HandleKey(Key{Name: "up"})
	ev = widgetEventOf(t, p.HandleKey(Key{Name: "up"}), "change")
	if ev.Value != "10" {
		t.Fatalf("clamped = %q, want 10", ev.Value)
	}
	if !p.Mutate(pluginapi.WidgetMutation{Kind: "setNumber", WidgetKey: "n", Number: 42}) {
		t.Fatal("setNumber not applied")
	}
	if got := p.numberValue(w); got != 10 {
		t.Fatalf("mutation bypassed the clamp: %v", got)
	}
}

func TestDropdownOpenChoose(t *testing.T) {
	w := pluginapi.Widget{Kind: pluginapi.WidgetDropdown, Key: "d", Options: []string{"a", "b", "c"}, Selected: 0}
	p, g := interactivePanel(w)
	// Closed: Down changes the value directly.
	ev := widgetEventOf(t, p.HandleKey(Key{Name: "down"}), "change")
	if ev.Index != 1 || ev.Value != "b" {
		t.Fatalf("down = %+v", ev)
	}
	// Enter opens the list; the options are painted over the content.
	p.HandleKey(Key{Name: "enter"})
	if !p.dropdownOpen(w) {
		t.Fatal("the dropdown did not open")
	}
	p.Draw(g, Rect{X: 0, Y: 0, W: 40, H: 10})
	plain := g.Plain()
	if !stringsContains(plain[2], "a") || !stringsContains(plain[3], "b") || !stringsContains(plain[4], "c") {
		t.Fatalf("option list not painted: %q", plain[:5])
	}
	// Down moves the highlight, Enter chooses and closes.
	p.HandleKey(Key{Name: "down"})
	ev = widgetEventOf(t, p.HandleKey(Key{Name: "enter"}), "change")
	if ev.Index != 2 || ev.Value != "c" || p.dropdownOpen(w) {
		t.Fatalf("enter = %+v open=%v", ev, p.dropdownOpen(w))
	}
}

func TestRadioGroup(t *testing.T) {
	w := pluginapi.Widget{Kind: pluginapi.WidgetRadio, Key: "r", Options: []string{"x", "y", "z"}, Selected: 0}
	p, g := interactivePanel(w)
	ev := widgetEventOf(t, p.HandleKey(Key{Name: "down"}), "change")
	if ev.Index != 1 || ev.Value != "y" {
		t.Fatalf("down = %+v", ev)
	}
	p.Draw(g, Rect{X: 0, Y: 0, W: 40, H: 10})
	plain := g.Plain()
	if !stringsContains(plain[1], "(o) y") {
		t.Fatalf("radio rows = %q", plain[:3])
	}
}

func TestDualListIncluded(t *testing.T) {
	w := pluginapi.Widget{Kind: pluginapi.WidgetDualList, Key: "dl", Options: []string{"a", "b"}, Included: []string{"a"}}
	p, g := interactivePanel(w)
	plain := g.Plain()
	if !stringsContains(plain[0], "[x] a") || !stringsContains(plain[1], "[ ] b") {
		t.Fatalf("dual rows = %q", plain[:3])
	}
	p.HandleKey(Key{Name: "down"})
	ev := widgetEventOf(t, p.HandleKey(Key{Name: "space"}), "change")
	if len(ev.Included) != 2 || ev.Included[0] != "a" || ev.Included[1] != "b" {
		t.Fatalf("included = %+v", ev.Included)
	}
	if !p.Mutate(pluginapi.WidgetMutation{Kind: "setDualIncluded", WidgetKey: "dl", Included: []string{"b"}}) {
		t.Fatal("setDualIncluded not applied")
	}
	if set := p.includedSet(w); !set["b"] || set["a"] {
		t.Fatalf("included set = %+v", set)
	}
}

func TestCompletionList(t *testing.T) {
	w := pluginapi.Widget{Kind: pluginapi.WidgetText, Key: "f", Text: "src/"}
	p, g := interactivePanel(w)
	if !p.Mutate(pluginapi.WidgetMutation{Kind: "setCompletions", WidgetKey: "f", Items: []pluginapi.WidgetItem{{Text: "src/a.go"}, {Text: "src/b.go"}}}) {
		t.Fatal("setCompletions not applied")
	}
	p.Draw(g, Rect{X: 0, Y: 0, W: 40, H: 10})
	if !p.completionsOpen(w) {
		t.Fatal("completions not open")
	}
	// Down moves the highlight, Tab accepts into the field.
	p.HandleKey(Key{Name: "down"})
	res := p.HandleKey(Key{Name: "tab"})
	if got := widgetEventOf(t, res, "completion_accept").Value; got != "src/b.go" {
		t.Fatalf("accept = %q", got)
	}
	if got := p.textValue(w); got != "src/src/b.go" {
		t.Fatalf("value = %q, want the accepted candidate inserted", got)
	}
	// Esc dismissed the list earlier: set it again and dismiss.
	p.Mutate(pluginapi.WidgetMutation{Kind: "setCompletions", WidgetKey: "f", Items: []pluginapi.WidgetItem{{Text: "x"}}})
	p.HandleKey(Key{Name: "escape"})
	if p.completionsOpen(w) {
		t.Fatal("escape did not dismiss the list")
	}
}

func TestInteractiveMutations(t *testing.T) {
	list := pluginapi.Widget{Kind: pluginapi.WidgetList, Key: "l", Items: []pluginapi.WidgetItem{{Text: "a"}, {Text: "b"}}}
	tree := pluginapi.Widget{Kind: pluginapi.WidgetTree, Key: "t", Nodes: []pluginapi.WidgetNode{{Key: "a", Text: "a", Depth: 0}, {Key: "b", Text: "b", Depth: 1}}}
	raw := pluginapi.Widget{Kind: pluginapi.WidgetRaw, Key: "raw", Runs: []pluginapi.WidgetRun{{Text: "one"}}}
	root := pluginapi.Widget{Kind: pluginapi.WidgetCol, Children: []pluginapi.Widget{list, tree, raw,
		{Kind: pluginapi.WidgetButton, Key: "ok", Label: "OK"}}}
	p := New(pluginapi.WidgetPanel{Root: root, FocusKey: "ok"}, Options{})
	p.SetFocused(true)

	if !p.Mutate(pluginapi.WidgetMutation{Kind: "setItems", WidgetKey: "l", Items: []pluginapi.WidgetItem{{Text: "x"}, {Text: "y"}}, ItemKeys: []string{"kx", "ky"}}) {
		t.Fatal("setItems not applied")
	}
	if items := p.listItems(list); len(items) != 2 || items[1].Key != "ky" {
		t.Fatalf("items = %+v", items)
	}
	if !p.Mutate(pluginapi.WidgetMutation{Kind: "setExpandedKeys", WidgetKey: "t", Keys: []string{"a"}}) {
		t.Fatal("setExpandedKeys not applied")
	}
	if vis := p.visible(tree); !vis[0].expanded {
		t.Fatalf("node a is not expanded: %+v", vis)
	}
	if !p.Mutate(pluginapi.WidgetMutation{Kind: "setCheckedKeys", WidgetKey: "t", Keys: []string{"a"}, Checked: true}) {
		t.Fatal("setCheckedKeys not applied")
	}
	if !p.treeChecked(tree, tree.Nodes[0]) || p.treeChecked(tree, tree.Nodes[1]) {
		t.Fatal("checked keys not applied")
	}
	if !p.Mutate(pluginapi.WidgetMutation{Kind: "appendTreeNodes", WidgetKey: "t", NewNodes: []pluginapi.WidgetNode{{Key: "c", Text: "c", Depth: 0}}}) {
		t.Fatal("appendTreeNodes not applied")
	}
	if got := len(p.treeNodes(tree)); got != 3 {
		t.Fatalf("nodes = %d, want 3", got)
	}
	if !p.Mutate(pluginapi.WidgetMutation{Kind: "setRawEntries", WidgetKey: "raw", Entries: []pluginapi.WidgetRun{{Text: "two"}}}) {
		t.Fatal("setRawEntries not applied")
	}
	if got := p.rawRuns(raw)[0].Text; got != "two" {
		t.Fatalf("raw = %q", got)
	}
	if !p.Mutate(pluginapi.WidgetMutation{Kind: "setFocusKey", FocusKey: "l"}) {
		t.Fatal("setFocusKey not applied")
	}
	if got := p.FocusedKey(); got != "l" {
		t.Fatalf("focus = %q", got)
	}
	// An unknown widget key is reported as not applied.
	if p.Mutate(pluginapi.WidgetMutation{Kind: "setChecked", WidgetKey: "nope", Checked: true}) {
		t.Fatal("unknown key applied")
	}
}

func TestWrapperKinds(t *testing.T) {
	root := pluginapi.Widget{Kind: pluginapi.WidgetCol, Children: []pluginapi.Widget{
		{Kind: pluginapi.WidgetLabeledSection, Title: "Legend", Children: []pluginapi.Widget{
			{Kind: pluginapi.WidgetButton, Key: "ok", Label: "OK"},
		}},
		{Kind: pluginapi.WidgetComponent, Children: []pluginapi.Widget{{Kind: pluginapi.WidgetLabel, Text: "inside"}}},
		{Kind: pluginapi.WidgetPopup, Title: "Pop", Children: []pluginapi.Widget{{Kind: pluginapi.WidgetLabel, Text: "popped"}}},
		{Kind: pluginapi.WidgetWindowEmbed, Window: 3, Rows: 3},
	}}
	p := New(pluginapi.WidgetPanel{Root: root, FocusKey: "ok"}, Options{})
	p.SetFocused(true)
	g := grid.New(40, 16)
	p.Draw(g, Rect{X: 0, Y: 0, W: 40, H: 16})
	plain := g.Plain()
	all := ""
	for _, row := range plain {
		all += row + "\n"
	}
	for _, want := range []string{"Legend", "[OK]", "inside", "popped", "window 3"} {
		if !stringsContains(all, want) {
			t.Fatalf("painting lacks %q:\n%s", want, all)
		}
	}
	// The wrapped button joins the focus ring.
	if len(p.focusables) != 1 || p.focusables[0].key != "ok" {
		t.Fatalf("focusables = %+v", p.focusables)
	}
}

func TestPageModeScrollsContent(t *testing.T) {
	children := make([]pluginapi.Widget, 0, 12)
	for i := 0; i < 12; i++ {
		children = append(children, pluginapi.Widget{Kind: pluginapi.WidgetLabel, Text: "row" + itoa(i)})
	}
	root := pluginapi.Widget{Kind: pluginapi.WidgetCol, Children: children}
	no := false
	p := New(pluginapi.WidgetPanel{Root: root}, Options{Page: true, AutoFocusFirst: &no})
	p.SetFocused(true)
	g := grid.New(20, 6)
	p.Draw(g, Rect{X: 0, Y: 0, W: 20, H: 6})
	if !stringsContains(g.Plain()[0], "row0") {
		t.Fatalf("first page row = %q", g.Plain()[0])
	}
	// Down scrolls the page when no widget has the focus.
	p.HandleKey(Key{Name: "down"})
	p.HandleKey(Key{Name: "down"})
	p.Draw(g, Rect{X: 0, Y: 0, W: 20, H: 6})
	if !stringsContains(g.Plain()[0], "row2") {
		t.Fatalf("after scrolling first row = %q, want row2", g.Plain()[0])
	}
	if p.FocusedKey() != "" {
		t.Fatalf("auto_focus_first=false still focused %q", p.FocusedKey())
	}
}

func TestValidateInteractiveKinds(t *testing.T) {
	cases := []struct {
		name string
		w    pluginapi.Widget
		ok   bool
	}{
		{"text without key", pluginapi.Widget{Kind: pluginapi.WidgetText}, false},
		{"number without key", pluginapi.Widget{Kind: pluginapi.WidgetNumber}, false},
		{"dropdown without key", pluginapi.Widget{Kind: pluginapi.WidgetDropdown}, false},
		{"radio without key", pluginapi.Widget{Kind: pluginapi.WidgetRadio}, false},
		{"dual without key", pluginapi.Widget{Kind: pluginapi.WidgetDualList}, false},
		{"text with key", pluginapi.Widget{Kind: pluginapi.WidgetText, Key: "t"}, true},
		{"wrapper with two children", pluginapi.Widget{Kind: pluginapi.WidgetComponent, Children: []pluginapi.Widget{
			{Kind: pluginapi.WidgetLabel}, {Kind: pluginapi.WidgetLabel}}}, false},
		{"wrapper with one child", pluginapi.Widget{Kind: pluginapi.WidgetPopup, Children: []pluginapi.Widget{
			{Kind: pluginapi.WidgetLabel}}}, true},
	}
	for _, c := range cases {
		err := Validate(pluginapi.WidgetPanel{Root: pluginapi.Widget{Kind: pluginapi.WidgetCol, Children: []pluginapi.Widget{c.w}}})
		if (err == nil) != c.ok {
			t.Errorf("%s: err = %v, want ok=%v", c.name, err, c.ok)
		}
	}
}
