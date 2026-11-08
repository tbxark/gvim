// Package pluginpanel lays out, paints and hit-tests the declarative widget panel of the
// plugin host (plugin API tier 1a). It is a component of the TUI tree: the root model owns
// the placement, the focus hand-off and the routing, and this package answers "what does
// this spec look like in this rectangle" and "what did the user just do to it".
//
// The panel keeps the instance state of keyed widgets (checked, selected, expanded, text)
// across spec updates, so a plugin can re-send a spec without losing the user's place.
// Everything a plugin supplies is validated against the caps of Validate and sanitised
// before it reaches the grid: control characters become spaces, because the grid writes
// its cells to the terminal as they are.
package pluginpanel

import (
	"image/color"
	"strings"
	"unicode"

	"github.com/charmbracelet/x/ansi"

	"github.com/tbxark/gvim/internal/tui/style"
	"github.com/tbxark/gvim/pluginapi"
)

// Caps of one panel. The plugin Lua API checks the same numbers before it calls the host,
// and Validate checks them again host-side: a plugin cannot get an unbounded tree, list or
// string into the paint loop.
const (
	MaxDepth   = 8    // nesting of row/col containers
	MaxWidgets = 2000 // nodes in one widget tree
	MaxItems   = 2000 // rows of one list
	MaxNodes   = 5000 // nodes of one tree
	MaxString  = 4 << 10
	MaxKey     = 128
	MaxTitle   = 120

	// DefaultVisibleRows is the list/tree window of a widget without visible_rows.
	DefaultVisibleRows = 8
)

// Rect is a block of frame cells: X, Y is the top-left corner, W and H the size.
type Rect struct {
	X, Y, W, H int
}

// Contains reports whether the cell (x, y) lies inside r.
func (r Rect) Contains(x, y int) bool {
	return r.W > 0 && r.H > 0 && x >= r.X && x < r.X+r.W && y >= r.Y && y < r.Y+r.H
}

// Key is one decoded key press in the form the panel handles. Name is the canonical key
// name of the key alone: "tab", "enter", "space", "escape", "up", "down", "left", "right",
// "home", "end", "pageup", "pagedown". Text is the printable text, when there is one.
type Key struct {
	Name             string
	Text             string
	Ctrl, Alt, Shift bool
}

// Mouse is one mouse message the panel takes: the frame cell, the button ("left", "right",
// "middle") or the wheel delta in rows (positive down).
type Mouse struct {
	X, Y   int
	Button string
	Wheel  int
	// Release marks the release of the left button after a press, for a tree drag.
	Release bool
}

// Result is what one input did to the panel: whether the panel took it, the widget events
// the root reports to the plugin, and whether the input asked to close the panel.
type Result struct {
	Handled bool
	Events  []pluginapi.WidgetEvent
	Close   bool
}

// Options describe how a panel is presented.
type Options struct {
	// Chrome is the overlay frame of a floating panel, and the fill and text styles of
	// every placement.
	Chrome style.Overlay
	// Resolve turns a plugin color (a literal RGB value, a theme key or a semantic name)
	// into a terminal color. nil keeps the role's own color.
	Resolve func(pluginapi.Color) color.Color
	Title   string
	// Framed paints the rounded card frame (a floating panel). A sidebar section and a
	// buffer placement are painted inside a rectangle the root already framed.
	Framed bool
	// Closable makes Escape (and a close click, when the root routes one) unmount the
	// panel: the root raises a cancel widget_event.
	Closable bool
	// Page makes the whole panel content one scrolling window when no focused widget
	// takes a movement key.
	Page bool
	// AutoFocusFirst (nil: true) starts the panel on its first tabbable widget when the
	// spec has no FocusKey. A page panel usually sets false so nothing starts focused.
	AutoFocusFirst *bool
}

// instance is the state of one keyed widget. A value is set only once the host owns it: a
// spec update does not overwrite what the user did. The tier-1b fields carry the interactive
// state: a text field's content, caret and selection, a number, the included set of a dual
// list, the items of a list rebuilt by a mutation, a tree's appended nodes and its checked
// keys.
type instance struct {
	checked     bool
	haveChecked bool
	selected    int
	haveSel     bool
	value       string
	haveValue   bool
	expanded    map[string]bool // node key -> expanded, for a tree
	// text
	text     string
	haveText bool
	cursor   int // byte offset into text
	textTop  int // first visible line of a multiline field
	selStart int // byte offsets; selStart == selEnd means no selection
	selEnd   int
	// number
	number     float64
	haveNumber bool
	// dualList
	included     []string
	haveIncluded bool
	// list/tree rebuilds and tree checks
	items       []pluginapi.WidgetItem
	haveItems   bool
	nodes       []pluginapi.WidgetNode
	haveNodes   bool
	runs        []pluginapi.WidgetRun
	haveRuns    bool
	checkedKeys map[string]bool // tree node key -> checked
	// dropdown open state
	open     bool
	haveOpen bool
	// text completion list (host-owned while the popup is open)
	completions        []string
	haveCompletions    bool
	completionSel      int
	completionOpen     bool
	completionHaveOpen bool
	// tree drag preview (increment B: recorded here, painted by the tree)
	previewHave  bool
	previewSrc   string
	previewAfter string
	previewLabel string
}

// focusRef identifies one focusable widget of the spec, in depth-first order.
type focusRef struct {
	key  string
	path string
	kind pluginapi.WidgetKind
}

// Panel is one mounted declarative widget panel.
type Panel struct {
	spec pluginapi.WidgetPanel

	chrome   style.Overlay
	resolve  func(pluginapi.Color) color.Color
	title    string
	framed   bool
	closable bool
	page     bool
	// autoFocus says whether a panel without a FocusKey seeds its first tabbable widget.
	autoFocus bool

	inst map[string]*instance

	focusables []focusRef
	focus      int // index into focusables; -1 for none
	focused    bool

	hits []hit
	rect Rect
	// lastMouse is the cell of the latest mouse_move, so a move within a cell is not
	// reported twice; haveMouse says it was set.
	lastMouse       int
	haveMouse       bool
	lastVisibleRows int // rows of the last list/tree window, for page moves

	// page mode: pageTop is the first content row shown, contentRows the content height of
	// the last draw.
	pageTop     int
	contentRows int
	// overlays are the transient paint-on-top regions of the last draw (the option list of
	// an open dropdown, the completion candidates of a focused text field).
	overlays []panelOverlay
	// drag is the tree row drag in progress: the source widget and node key.
	drag *treeDrag
}

// New returns a panel for spec. The state of keyed widgets starts empty: the spec's seeds
// (checked, selected, expanded) are drawn until the host owns a value.
func New(spec pluginapi.WidgetPanel, o Options) *Panel {
	p := &Panel{
		chrome:    o.Chrome,
		resolve:   o.Resolve,
		title:     o.Title,
		framed:    o.Framed,
		closable:  o.Closable,
		page:      o.Page,
		autoFocus: o.AutoFocusFirst == nil || *o.AutoFocusFirst,
		inst:      map[string]*instance{},
		focus:     -1,
		lastMouse: -1,
	}
	p.setSpec(spec)
	return p
}

// setSpec replaces the spec and rebuilds the focus ring, keeping the focus on the same keyed
// widget when it still exists.
func (p *Panel) setSpec(spec pluginapi.WidgetPanel) {
	old := p.focusKey()
	p.spec = spec
	p.focusables = p.focusables[:0]
	p.collect(spec.Root, "0", 0)
	p.focus = -1
	if len(p.focusables) == 0 {
		return
	}
	if old != "" {
		for i, f := range p.focusables {
			if f.key != "" && f.key == old {
				p.focus = i
				return
			}
		}
	}
	// The spec's own focus hint, then the first focusable.
	if spec.FocusKey != "" {
		for i, f := range p.focusables {
			if f.key == spec.FocusKey {
				p.focus = i
				return
			}
		}
	}
	for i, f := range p.focusables {
		if w, ok := widgetAt(spec.Root, f.path); ok && w.Focused {
			p.focus = i
			return
		}
	}
	if p.autoFocus {
		p.focus = 0
	}
}

// focusKey returns the key of the focused widget, "" when none.
func (p *Panel) focusKey() string {
	if p.focus < 0 || p.focus >= len(p.focusables) {
		return ""
	}
	return p.focusables[p.focus].key
}

// FocusedKey is the key of the widget with the focus, "" when none.
func (p *Panel) FocusedKey() string { return p.focusKey() }

// Spec returns the current spec.
func (p *Panel) Spec() pluginapi.WidgetPanel { return p.spec }

// Title is the panel title.
func (p *Panel) Title() string { return p.title }

// Framed reports whether the panel paints its own card frame.
func (p *Panel) Framed() bool { return p.framed }

// Closable reports whether Escape closes the panel.
func (p *Panel) Closable() bool { return p.closable }

// SetFocused gives the panel the keyboard (or takes it away). Taking it away keeps the
// widget focus, so coming back lands where the user left.
func (p *Panel) SetFocused(f bool) {
	p.focused = f
	if f && p.focus < 0 && len(p.focusables) > 0 {
		p.focus = 0
	}
}

// Focused reports whether the panel has the keyboard.
func (p *Panel) Focused() bool { return p.focused }

// FocusWidget moves the focus to the keyed widget key and reports whether it exists.
func (p *Panel) FocusWidget(key string) bool {
	if key == "" {
		return false
	}
	for i, f := range p.focusables {
		if f.key == key {
			p.focus = i
			return true
		}
	}
	return false
}

// Update replaces the panel content. The instance state of matching keys, the focus and the
// scroll position are kept.
func (p *Panel) Update(spec pluginapi.WidgetPanel) {
	p.setSpec(spec)
}

// Dragging reports whether a tree row drag is in progress in this panel.
func (p *Panel) Dragging() bool { return p.drag != nil }

// Contains reports whether the panel's last draw covers the cell (x, y).
func (p *Panel) Contains(x, y int) bool { return p.rect.Contains(x, y) }

// Rect returns the block the panel was last painted in.
func (p *Panel) Rect() Rect { return p.rect }

// ---- validation ----

// Validate checks a panel against the caps and the kind rules of v1. It returns a
// pluginapi error: CodeLimit for a cap, CodeInvalid for a malformed node.
func Validate(panel pluginapi.WidgetPanel) error {
	if panel.FocusKey != "" && len(panel.FocusKey) > MaxKey {
		return pluginapi.Errf(pluginapi.CodeLimit, "focus_key is longer than %d bytes", MaxKey)
	}
	n := 0
	if err := validateWidget(panel.Root, 0, &n); err != nil {
		return err
	}
	return nil
}

func validateWidget(w pluginapi.Widget, depth int, n *int) error {
	*n++
	if *n > MaxWidgets {
		return pluginapi.Errf(pluginapi.CodeLimit, "a panel takes at most %d widgets", MaxWidgets)
	}
	if depth > MaxDepth {
		return pluginapi.Errf(pluginapi.CodeLimit, "widget nesting is deeper than %d", MaxDepth)
	}
	if len(w.Key) > MaxKey {
		return pluginapi.Errf(pluginapi.CodeLimit, "a widget key is longer than %d bytes", MaxKey)
	}
	for _, s := range []string{w.Text, w.Label} {
		if len(s) > MaxString {
			return pluginapi.Errf(pluginapi.CodeLimit, "widget text is longer than %d bytes", MaxString)
		}
	}
	for _, r := range w.Runs {
		if len(r.Text) > MaxString {
			return pluginapi.Errf(pluginapi.CodeLimit, "a styled run is longer than %d bytes", MaxString)
		}
	}
	switch w.Kind {
	case pluginapi.WidgetRow, pluginapi.WidgetCol:
		for _, c := range w.Children {
			if err := validateWidget(c, depth+1, n); err != nil {
				return err
			}
		}
	case pluginapi.WidgetLabel, pluginapi.WidgetRaw, pluginapi.WidgetHintBar,
		pluginapi.WidgetSpacer, pluginapi.WidgetDivider, pluginapi.WidgetButton, pluginapi.WidgetToggle,
		pluginapi.WidgetWindowEmbed:
	case pluginapi.WidgetText:
		if w.Key == "" {
			return pluginapi.Errf(pluginapi.CodeInvalid, "an editable text field needs a key")
		}
		if w.Rows < 0 || w.Rows > 200 || w.MinRows < 0 || w.MaxRows < 0 {
			return pluginapi.Errf(pluginapi.CodeInvalid, "a text field's row counts are outside 0..200")
		}
		if len(w.Completions) > MaxItems {
			return pluginapi.Errf(pluginapi.CodeLimit, "a text field takes at most %d completions", MaxItems)
		}
	case pluginapi.WidgetNumber, pluginapi.WidgetDropdown, pluginapi.WidgetRadio, pluginapi.WidgetDualList:
		if w.Key == "" {
			return pluginapi.Errf(pluginapi.CodeInvalid, "an interactive widget needs a key")
		}
		if len(w.Options) > MaxItems {
			return pluginapi.Errf(pluginapi.CodeLimit, "a control takes at most %d options", MaxItems)
		}
	case pluginapi.WidgetLabeledSection, pluginapi.WidgetComponent, pluginapi.WidgetOverlay, pluginapi.WidgetPopup:
		if len(w.Children) > 1 {
			return pluginapi.Errf(pluginapi.CodeInvalid, "a %s wraps at most one widget", string(w.Kind))
		}
		for _, c := range w.Children {
			if err := validateWidget(c, depth+1, n); err != nil {
				return err
			}
		}
	case pluginapi.WidgetList:
		if len(w.Items) > MaxItems {
			return pluginapi.Errf(pluginapi.CodeLimit, "a list takes at most %d items", MaxItems)
		}
		if w.Selected < -1 || w.Selected >= len(w.Items) {
			return pluginapi.Errf(pluginapi.CodeInvalid, "selected index %d is outside the list", w.Selected)
		}
		for _, it := range w.Items {
			if len(it.Key) > MaxKey || len(it.Text) > MaxString || len(it.Detail) > MaxString {
				return pluginapi.Errf(pluginapi.CodeLimit, "a list item is too large")
			}
		}
	case pluginapi.WidgetTree:
		if len(w.Nodes) > MaxNodes {
			return pluginapi.Errf(pluginapi.CodeLimit, "a tree takes at most %d nodes", MaxNodes)
		}
		if w.Selected < -1 || w.Selected >= len(w.Nodes) {
			return pluginapi.Errf(pluginapi.CodeInvalid, "selected index %d is outside the tree", w.Selected)
		}
		for _, nd := range w.Nodes {
			if nd.Depth < 0 || nd.Depth > MaxDepth {
				return pluginapi.Errf(pluginapi.CodeInvalid, "tree node depth %d is outside 0..%d", nd.Depth, MaxDepth)
			}
			if len(nd.Key) > MaxKey || len(nd.Text) > MaxString || len(nd.Detail) > MaxString {
				return pluginapi.Errf(pluginapi.CodeLimit, "a tree node is too large")
			}
		}
	default:
		return pluginapi.Errf(pluginapi.CodeInvalid, "unknown widget kind %q", string(w.Kind))
	}
	if w.Kind == pluginapi.WidgetDivider {
		if w.Text != "" && ansi.StringWidth(w.Text) != 1 {
			return pluginapi.Errf(pluginapi.CodeInvalid, "a divider glyph is one cell wide")
		}
	}
	return nil
}

// Sanitize replaces the control characters of a plugin string with spaces: a cell written
// to the terminal must not carry an escape sequence.
func Sanitize(s string) string {
	if !strings.ContainsFunc(s, unicode.IsControl) {
		return s
	}
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, s)
}

// ---- instance helpers ----

// instFor returns the instance state of key, creating it. A keyless widget has none: the
// plugin should key a widget whose state it wants kept.
func (p *Panel) instFor(key string) *instance {
	if key == "" {
		return nil
	}
	in := p.inst[key]
	if in == nil {
		in = &instance{}
		p.inst[key] = in
	}
	return in
}

// checked returns the effective checked state of a toggle.
func (p *Panel) checked(w pluginapi.Widget) bool {
	if in := p.inst[w.Key]; in != nil && in.haveChecked {
		return in.checked
	}
	return w.Checked
}

// setChecked writes the checked state and stores it for a keyed widget.
func (p *Panel) setChecked(w pluginapi.Widget, v bool) {
	if in := p.instFor(w.Key); in != nil {
		in.checked, in.haveChecked = v, true
	}
}

// selected returns the effective selected index of a list or tree.
func (p *Panel) selected(w pluginapi.Widget) int {
	if in := p.inst[w.Key]; in != nil && in.haveSel {
		return in.selected
	}
	return w.Selected
}

func (p *Panel) setSelected(w pluginapi.Widget, i int) {
	if in := p.instFor(w.Key); in != nil {
		in.selected, in.haveSel = i, true
	}
}

// expanded returns the effective expanded state of a tree node. A keyed tree stores the
// set of expanded node keys once the user changed it; before that the node's seed applies.
func (p *Panel) expanded(w pluginapi.Widget, nd pluginapi.WidgetNode) bool {
	if in := p.inst[w.Key]; in != nil && in.expanded != nil {
		if v, ok := in.expanded[nd.Key]; ok {
			return v
		}
		return false
	}
	return nd.Expanded || nd.Key == ""
}

// setExpanded stores the expanded state of a tree node.
func (p *Panel) setExpanded(w pluginapi.Widget, nd pluginapi.WidgetNode, v bool) {
	in := p.instFor(w.Key)
	if in == nil {
		return
	}
	if in.expanded == nil {
		in.expanded = map[string]bool{}
		// Seed the map from the spec so the other nodes keep their state.
		for _, n := range w.Nodes {
			if n.Key != "" {
				in.expanded[n.Key] = n.Expanded
			}
		}
	}
	in.expanded[nd.Key] = v
}

// value returns the text of a label or text block, when a setValue mutation replaced it.
func (p *Panel) value(w pluginapi.Widget) string {
	if in := p.inst[w.Key]; in != nil && in.haveValue {
		return in.value
	}
	return w.Text
}

// ---- interactive instance state ----

// textValue returns a text field's content: the host's edited text once the user typed,
// else the spec seed.
func (p *Panel) textValue(w pluginapi.Widget) string {
	if in := p.inst[w.Key]; in != nil && in.haveText {
		return in.text
	}
	return w.Text
}

// setText stores a text field's content and caret, clamping the caret to the content.
func (p *Panel) setText(w pluginapi.Widget, text string, cursor int) {
	in := p.instFor(w.Key)
	if in == nil {
		return
	}
	in.text, in.haveText = text, true
	in.cursor = clamp(cursor, 0, len(text))
	if in.selStart > len(text) {
		in.selStart = len(text)
	}
	if in.selEnd > len(text) {
		in.selEnd = len(text)
	}
}

// textCursor returns the caret byte offset of a text field, clamped to its content.
func (p *Panel) textCursor(w pluginapi.Widget) int {
	if in := p.inst[w.Key]; in != nil && in.haveText {
		return clamp(in.cursor, 0, len(in.text))
	}
	return len(w.Text)
}

// textSelection returns the selected byte range of a text field, (0,0) for none.
func (p *Panel) textSelection(w pluginapi.Widget) (int, int) {
	in := p.inst[w.Key]
	if in == nil || in.selStart == in.selEnd {
		return 0, 0
	}
	return min(in.selStart, in.selEnd), max(in.selStart, in.selEnd)
}

// setSelection stores a text field's selection normalized.
func (p *Panel) setSelection(w pluginapi.Widget, start, end int) {
	in := p.instFor(w.Key)
	if in == nil {
		return
	}
	in.selStart, in.selEnd = min(start, end), max(start, end)
}

// numberValue returns a number widget's value.
func (p *Panel) numberValue(w pluginapi.Widget) float64 {
	if in := p.inst[w.Key]; in != nil && in.haveNumber {
		return in.number
	}
	return w.Number
}

// setNumber stores a number widget's value, clamped to its bounds.
func (p *Panel) setNumber(w pluginapi.Widget, v float64) {
	if in := p.instFor(w.Key); in != nil {
		in.number, in.haveNumber = clampNumber(w, v), true
	}
}

// clampNumber clamps v to the number widget's min/max.
func clampNumber(w pluginapi.Widget, v float64) float64 {
	if w.Min != 0 || w.Max != 0 {
		if v < w.Min {
			v = w.Min
		}
		if w.Max != 0 && v > w.Max {
			v = w.Max
		}
	}
	return v
}

// includedSet returns a dual list's included values as a set.
func (p *Panel) includedSet(w pluginapi.Widget) map[string]bool {
	set := map[string]bool{}
	src := w.Included
	if in := p.inst[w.Key]; in != nil && in.haveIncluded {
		src = in.included
	}
	for _, v := range src {
		set[v] = true
	}
	return set
}

// setIncluded stores a dual list's included values.
func (p *Panel) setIncluded(w pluginapi.Widget, included []string) {
	if in := p.instFor(w.Key); in != nil {
		in.included, in.haveIncluded = append([]string(nil), included...), true
	}
}

// listItems returns a list's rows: the host's rebuilt rows once a mutation replaced them,
// else the spec's.
func (p *Panel) listItems(w pluginapi.Widget) []pluginapi.WidgetItem {
	if in := p.inst[w.Key]; in != nil && in.haveItems {
		return in.items
	}
	return w.Items
}

// treeNodes returns a tree's nodes: appended/updated rows once a mutation changed them, else
// the spec's.
func (p *Panel) treeNodes(w pluginapi.Widget) []pluginapi.WidgetNode {
	if in := p.inst[w.Key]; in != nil && in.haveNodes {
		return in.nodes
	}
	return w.Nodes
}

// ---- focus ----

// collect appends the focusable widgets of the subtree in depth-first order.
func (p *Panel) collect(w pluginapi.Widget, path string, depth int) {
	if isFocusable(w.Kind) && (w.Kind != pluginapi.WidgetButton || !w.Disabled) {
		p.focusables = append(p.focusables, focusRef{key: w.Key, path: path, kind: w.Kind})
	}
	for i, c := range w.Children {
		child := path + "/" + itoa(i)
		p.collect(c, child, depth+1)
	}
}

// isFocusable reports whether a widget kind joins the Tab ring.
func isFocusable(k pluginapi.WidgetKind) bool {
	switch k {
	case pluginapi.WidgetButton, pluginapi.WidgetToggle, pluginapi.WidgetList, pluginapi.WidgetTree,
		pluginapi.WidgetText, pluginapi.WidgetNumber, pluginapi.WidgetDropdown, pluginapi.WidgetRadio,
		pluginapi.WidgetDualList:
		return true
	}
	return false
}

// moveFocus advances the focus by delta, wrapping around the ring.
func (p *Panel) moveFocus(delta int) Result {
	if len(p.focusables) == 0 {
		return Result{Handled: true}
	}
	if delta == 0 {
		delta = 1
	}
	if p.focus < 0 {
		p.focus = 0
	} else {
		p.focus = ((p.focus+delta)%len(p.focusables) + len(p.focusables)) % len(p.focusables)
	}
	p.focused = true
	return Result{Handled: true, Events: []pluginapi.WidgetEvent{p.focusEvent()}}
}

// focusEvent is the "focus" widget_event for the current focus.
func (p *Panel) focusEvent() pluginapi.WidgetEvent {
	return pluginapi.WidgetEvent{Event: "focus", Widget: p.focusKey(), Index: -1}
}

// focusedWidget returns the spec node of the focused focusable.
func (p *Panel) focusedWidget() (pluginapi.Widget, bool) {
	if p.focus < 0 || p.focus >= len(p.focusables) {
		return pluginapi.Widget{}, false
	}
	return widgetAt(p.spec.Root, p.focusables[p.focus].path)
}

// widgetAt returns the widget at a path built by collect ("0", "0/1", ...).
func widgetAt(root pluginapi.Widget, path string) (pluginapi.Widget, bool) {
	cur := root
	rest := strings.TrimPrefix(path, "0")
	for rest != "" {
		rest = strings.TrimPrefix(rest, "/")
		i := 0
		for i < len(rest) && rest[i] >= '0' && rest[i] <= '9' {
			i++
		}
		if i == 0 {
			return pluginapi.Widget{}, false
		}
		n := atoi(rest[:i])
		if n < 0 || n >= len(cur.Children) {
			return pluginapi.Widget{}, false
		}
		cur = cur.Children[n]
		rest = rest[i:]
	}
	return cur, true
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}

func atoi(s string) int {
	n := 0
	for i := 0; i < len(s); i++ {
		n = n*10 + int(s[i]-'0')
	}
	return n
}
