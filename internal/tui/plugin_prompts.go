package tui

import (
	"errors"
	"slices"
	"strings"
	"unicode"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/tbxark/gvim/internal/tui/grid"
	"github.com/tbxark/gvim/internal/tui/picker"
	"github.com/tbxark/gvim/internal/tui/popup"
	"github.com/tbxark/gvim/internal/tui/prompt"
	"github.com/tbxark/gvim/pluginapi"
)

// The prompts, pickers and popups of the plugin API: gvim.ui.input, confirm, choose,
// select, picker and popup (FINAL X6).
//
// A plugin asks through an async Host call (Input, Confirm, OpenPicker, ShowPopup). The
// call validates, records the wait token in m.plug.waits and defers the opening to the
// pump (the Lua slice may be running inside an overlay's Update, and routeOverlay owns
// the overlay stack while it does). The answer ends in Runtime.Resolve:
//
//   - a prompt submits as prompt.Done with a plugin tag; Model.answer hands it to
//     pluginAnswer first;
//   - a picker never talks to the runtime from inside its Update: it returns events, and
//     the overlay turns them into a prompt.Done with pluginPickTag, which the root
//     handles outside routeOverlay, like any answer. The same goes for the ranking
//     result;
//   - a popup row arrives as popup.Chosen with a pluginPopupData payload (lsp.go gives
//     it to pluginPopupChosen), and a popup that closes without a choice as prompt.Done;
//   - Esc never reaches an overlay: routeOverlay calls Dismiss on it, and the Done that
//     Dismiss queues is returned by plugDismissed.
//
// Owning a wait. The Host calls carry no plugin, so the root does not know whose overlay
// is whose. The runtime ties every wait to its plugin and runs the wait's cancel (this
// file's CancelWait, ClosePicker, ClosePopup) when the plugin unloads or its task ends, so
// OwnerGone has nothing left to close here (plugPromptsGone). The panic chord uses
// plugOverlaysCancel.

// pluginUI is the root's state for the overlays that plugins open.
type pluginUI struct {
	// out holds the commands that Dismiss queued; routeOverlay takes them.
	out []tea.Cmd
	// busy is above zero while a plugin overlay's Update runs. A close that happens then
	// only marks the overlay, because routeOverlay writes the stack back when Update returns.
	busy int
	// opening holds the pickers whose opening is queued, by picker id. Updates that arrive
	// before the pump opens the overlay change the pending spec (S13-1: `p:open();
	// p:set_status(...)` must not be lost).
	opening map[uint64]*pendingPicker
	// clip is the text of the last gvim.clipboard.set of the slice; clipQueued says a
	// closure that emits it is in m.plug.later. Many calls make one clipboard write.
	clip       string
	clipQueued bool
	// updates counts the calls of Update. notices are the plugin notices that showed in
	// update noticeAt, which are joined on the status row (see pluginHost.Notify).
	updates  uint64
	noticeAt uint64
	notices  []string
}

// pendingPicker is a picker that has been asked for and is not on the stack yet.
type pendingPicker struct {
	tok      pluginapi.Token
	spec     pluginapi.PickerSpec
	status   *string
	selected *int
}

// Wait kinds in m.plug.waits that belong to this file.
const (
	waitInput   = "input"
	waitConfirm = "confirm"
	waitPicker  = "picker"
	waitPopup   = "popup"
)

// Tags of the prompt.Done messages this file sends and receives.
type (
	// pluginInputTag tags the text prompt of wait tok. esc is set for the Done that
	// Dismiss queued.
	pluginInputTag struct {
		tok pluginapi.Token
		esc bool
	}
	pluginConfirmTag struct {
		tok pluginapi.Token
		esc bool
	}
	// pluginPopupTag says the popup of wait tok closed without a choice.
	pluginPopupTag struct{ tok pluginapi.Token }
	// pluginPickTag carries the events a picker returned.
	pluginPickTag struct {
		tok    pluginapi.Token
		events []picker.Event
		seq    int // the order the overlay produced it in (pickerOverlay.tag)
	}
	// pluginPickRankTag carries a ranking result back to the picker with this id.
	pluginPickRankTag struct {
		id  uint64
		msg tea.Msg
	}
)

// pluginPopupData is the payload of a popup row (popup.Item.Data).
type pluginPopupData struct {
	tok  pluginapi.Token
	item int  // the PickItem ID
	line bool // a text line: choosing it only closes the popup
}

// cleanText replaces the control characters of text that a plugin supplied with spaces: the
// grid writes its cells to the terminal as they are, so an escape sequence in a label or a
// file name would otherwise reach the terminal.
func cleanText(s string) string {
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

// doneCmd returns the command that reports d.
func doneCmd(d prompt.Done) tea.Cmd { return func() tea.Msg { return d } }

// ---- overlays

// pluginOverlay is an overlay a plugin opened.
type pluginOverlay interface {
	Overlay
	plugToken() pluginapi.Token
	plugShut()
}

// plugBase is what the overlays of a plugin share.
type plugBase struct {
	m      *Model
	tok    pluginapi.Token
	closed bool
}

func (b *plugBase) plugToken() pluginapi.Token { return b.tok }

// pluginOverlay marks the overlay for plugOverlay (plugin_keys.go): it counts as plugin
// input for the panic chords.
func (b *plugBase) pluginOverlay() {}
func (b *plugBase) plugShut()      { b.closed = true }

// dismissed records the Esc of the user: the overlay is done, and d reaches the root after
// routeOverlay has returned.
func (b *plugBase) dismissed(d prompt.Done) {
	b.closed = true
	b.m.plug.ui.out = append(b.m.plug.ui.out, doneCmd(d))
}

// pluginLineOverlay is a text prompt of a plugin.
type pluginLineOverlay struct {
	plugBase
	l       *prompt.Line
	changed bool // OnChange ran: the runtime has a callback to run
}

func (o *pluginLineOverlay) Update(msg tea.Msg) (Overlay, tea.Cmd) {
	if o.closed {
		return nil, nil
	}
	ui := &o.m.plug.ui
	ui.busy++
	l, cmd := o.l.Update(msg)
	ui.busy--
	// A validator or OnChange ran Lua outside a pump: what it deferred (an action, a
	// clipboard write) and the callback task it queued wait for a drain.
	if o.changed || len(o.m.plug.later) > 0 {
		o.changed = false
		cmd = tea.Batch(cmd, o.m.drainTick())
	}
	if !l.Active() || o.closed {
		return nil, cmd
	}
	return o, cmd
}

func (o *pluginLineOverlay) Draw(g *grid.Grid) {
	if !o.closed {
		o.l.Draw(g)
	}
}
func (o *pluginLineOverlay) Active() bool { return !o.closed && o.l.Active() }

// UsesPromptRow marks the overlay as a prompt on the bottom row, so the root reserves
// that row for it while it is open.
func (o *pluginLineOverlay) UsesPromptRow() {}
func (o *pluginLineOverlay) Dismiss() {
	o.dismissed(prompt.Done{Tag: pluginInputTag{tok: o.tok, esc: true}})
}
func (o *pluginLineOverlay) Cursor(w, h int) (int, int, bool) {
	return o.l.Cursor(w, h)
}

// The plugin prompts draw on the bottom prompt row, like the built-in ones, so the
// root reserves the row for them while they are open.
var (
	_ prompt.RowOwner = (*pluginLineOverlay)(nil)
	_ prompt.RowOwner = (*pluginConfirmOverlay)(nil)
)

// pluginConfirmOverlay is a Yes/No question of a plugin.
type pluginConfirmOverlay struct {
	plugBase
	c *prompt.Confirm
}

func (o *pluginConfirmOverlay) Update(msg tea.Msg) (Overlay, tea.Cmd) {
	if o.closed {
		return nil, nil
	}
	c, cmd := o.c.Update(msg)
	if !c.Active() {
		return nil, cmd
	}
	return o, cmd
}

func (o *pluginConfirmOverlay) Draw(g *grid.Grid) {
	if !o.closed {
		o.c.Draw(g)
	}
}
func (o *pluginConfirmOverlay) Active() bool { return !o.closed && o.c.Active() }

// UsesPromptRow marks the overlay as a prompt on the bottom row, so the root reserves
// that row for it while it is open.
func (o *pluginConfirmOverlay) UsesPromptRow() {}
func (o *pluginConfirmOverlay) Dismiss() {
	o.dismissed(prompt.Done{Tag: pluginConfirmTag{tok: o.tok, esc: true}})
}

// pluginPickerOverlay is a picker of a plugin.
type pluginPickerOverlay struct {
	plugBase
	id uint64
	p  *picker.Picker
	// Each batch of events travels in a command of its own, and commands run on separate
	// goroutines, so the messages may arrive in any order: sent counts the batches made,
	// next is the one to deliver, and held keeps the ones that came early (pickOrdered).
	sent, next int
	held       map[int]pluginPickTag
}

// tag wraps a batch of events for delivery, numbered in the order it was produced.
func (o *pluginPickerOverlay) tag(events []picker.Event) pluginPickTag {
	t := pluginPickTag{tok: o.tok, events: events, seq: o.sent}
	o.sent++
	return t
}

func (o *pluginPickerOverlay) Update(msg tea.Msg) (Overlay, tea.Cmd) {
	if o.closed {
		return nil, nil
	}
	cmd := o.cmdFor(o.p.Update(msg))
	if !o.p.Active() {
		return nil, cmd
	}
	return o, cmd
}

func (o *pluginPickerOverlay) Draw(g *grid.Grid) {
	if !o.closed {
		o.p.Draw(g)
	}
}
func (o *pluginPickerOverlay) Active() bool { return !o.closed && o.p.Active() }

// Dismiss is the Esc of the user: the picker closes without a choice.
func (o *pluginPickerOverlay) Dismiss() {
	o.closed = true
	ev := o.p.Close()
	d := prompt.Done{Tag: o.tag([]picker.Event{ev})}
	o.m.plug.ui.out = append(o.m.plug.ui.out, doneCmd(d))
}

// cmdFor turns what the picker asks of its owner into commands: the events and the
// ranking come back to the root as prompt.Done, outside the overlay's Update.
func (o *pluginPickerOverlay) cmdFor(r picker.Result) tea.Cmd {
	var cmds []tea.Cmd
	if r.Cmd != nil {
		cmds = append(cmds, r.Cmd)
	}
	if r.Rank != nil {
		rank, id := r.Rank, o.id
		cmds = append(cmds, func() tea.Msg {
			return prompt.Done{Tag: pluginPickRankTag{id: id, msg: rank()}}
		})
	}
	if len(r.Events) > 0 {
		cmds = append(cmds, doneCmd(prompt.Done{Tag: o.tag(r.Events)}))
	}
	return tea.Batch(cmds...)
}

// pluginPopupOverlay is a popup of a plugin.
type pluginPopupOverlay struct {
	plugBase
	id uint64
	p  *popup.Popup
}

func (o *pluginPopupOverlay) Update(msg tea.Msg) (Overlay, tea.Cmd) {
	if o.closed {
		return nil, nil
	}
	_, cmd := o.p.Update(msg)
	if o.p.Active() {
		return o, cmd
	}
	if cmd == nil {
		// A click outside closed it: no row was chosen.
		cmd = doneCmd(prompt.Done{Tag: pluginPopupTag{tok: o.tok}})
	}
	return nil, cmd
}

func (o *pluginPopupOverlay) Draw(g *grid.Grid) {
	if !o.closed {
		o.p.Draw(g)
	}
}
func (o *pluginPopupOverlay) Active() bool { return !o.closed && o.p.Active() }
func (o *pluginPopupOverlay) Dismiss()     { o.dismissed(prompt.Done{Tag: pluginPopupTag{tok: o.tok}}) }

// ---- the stack

// plugRemove closes a plugin overlay: it is marked, and leaves the stack unless an
// overlay's Update is running (routeOverlay writes the stack back after it).
func (m *Model) plugRemove(o pluginOverlay) {
	o.plugShut()
	if m.plug.ui.busy > 0 {
		return
	}
	m.overlays = slices.DeleteFunc(m.overlays, func(x Overlay) bool { return x == Overlay(o) })
}

// plugShutWhere closes the plugin overlays that match.
func (m *Model) plugShutWhere(match func(o pluginOverlay) bool) {
	for _, o := range slices.Clone(m.overlays) {
		if po, ok := o.(pluginOverlay); ok && match(po) {
			m.plugRemove(po)
		}
	}
}

// plugPicker returns the open picker with this id.
func (m *Model) plugPicker(id uint64) *pluginPickerOverlay {
	for _, o := range m.overlays {
		if po, ok := o.(*pluginPickerOverlay); ok && po.id == id && !po.closed {
			return po
		}
	}
	return nil
}

// plugDismissed returns the commands that the Esc of a plugin overlay queued. routeOverlay
// returns them: Dismiss must not run the plugin, because routeOverlay pops the overlay
// right after it.
func (m *Model) plugDismissed() tea.Cmd {
	out := m.plug.ui.out
	m.plug.ui.out = nil
	return tea.Batch(out...)
}

// plugCancelWait forgets the wait t and closes the overlay that shows it. It is
// Host.CancelWait.
func (m *Model) plugCancelWait(t pluginapi.Token) {
	delete(m.plug.waits, t)
	for id, pp := range m.plug.ui.opening {
		if pp.tok == t {
			delete(m.plug.ui.opening, id)
		}
	}
	m.plugShutWhere(func(o pluginOverlay) bool { return o.plugToken() == t })
}

// plugOverlaysCancel closes every prompt, picker and popup that plugins opened, and
// resolves their waits with a "cancelled" error carrying why. The panic chord calls it.
func (m *Model) plugOverlaysCancel(why string) tea.Cmd {
	p := &m.plug
	var toks []pluginapi.Token
	for t, kind := range p.waits {
		switch kind {
		case waitInput, waitConfirm, waitPicker, waitPopup:
			toks = append(toks, t)
		}
	}
	slices.Sort(toks)
	clear(p.ui.opening)
	m.plugShutWhere(func(pluginOverlay) bool { return true })
	cmds := make([]tea.Cmd, 0, len(toks))
	for _, t := range toks {
		cmds = append(cmds, m.plugFinish(pluginapi.Completion{Token: t, Err: string(pluginapi.CodeCancelled) + ": " + why}))
	}
	return tea.Batch(cmds...)
}

// plugPromptsGone is OwnerGone for this area. There is nothing to do: the runtime cancels
// the waits of an unloading plugin before it calls OwnerGone, and CancelWait, ClosePicker
// and ClosePopup have closed the overlays by then.
func (m *Model) plugPromptsGone(plugin string, gen uint64) { _, _ = plugin, gen }

// plugItem delivers an intermediate item (a query change, a highlight move) of the wait
// c.Token to the runtime. The wait stays open.
func (m *Model) plugItem(c pluginapi.Completion) tea.Cmd {
	p := &m.plug
	if _, ok := p.waits[c.Token]; !ok || p.rt == nil {
		return nil
	}
	c.More = true
	return m.pumpAfter(func() { p.rt.Resolve(c) })
}

// ---- Host: the async prompt calls

// Input asks for a line of text. The result is Values{string}, or Values{nil} when the user
// pressed Esc.
func (h *pluginHost) Input(t pluginapi.Token, o pluginapi.InputSpec) error {
	m := h.m
	if err := m.plugWait(t, waitInput); err != nil {
		return err
	}
	m.plug.later = append(m.plug.later, func() tea.Cmd { return m.openInput(t, o) })
	return nil
}

func (m *Model) openInput(t pluginapi.Token, o pluginapi.InputSpec) tea.Cmd {
	if _, ok := m.plug.waits[t]; !ok {
		return nil
	}
	ov := &pluginLineOverlay{plugBase: plugBase{m: m, tok: t}}
	opts := prompt.LineOptions{
		Label:     cleanText(o.Prompt),
		Input:     cleanText(o.Default),
		SelectAll: o.SelectAll,
		Tag:       pluginInputTag{tok: t},
	}
	for _, h := range o.History {
		opts.History = append(opts.History, cleanText(h))
	}
	if o.Validate != nil {
		// The text a validator returns is shown in the prompt: it is cleaned like the rest.
		opts.Validate = func(text string) error {
			if err := o.Validate(text); err != nil {
				return errors.New(cleanText(err.Error()))
			}
			return nil
		}
	}
	if o.OnChange != nil {
		opts.OnChange = func(text string) {
			o.OnChange(text)
			ov.changed = true
		}
	}
	ov.l = prompt.NewLine(m.styles, opts)
	m.pushOverlay(ov)
	return nil
}

// Confirm asks a yes/no question. The result is Values{bool}; Esc answers false.
func (h *pluginHost) Confirm(t pluginapi.Token, o pluginapi.ConfirmSpec) error {
	m := h.m
	if err := m.plugWait(t, waitConfirm); err != nil {
		return err
	}
	m.plug.later = append(m.plug.later, func() tea.Cmd { return m.openConfirm(t, o) })
	return nil
}

func (m *Model) openConfirm(t pluginapi.Token, o pluginapi.ConfirmSpec) tea.Cmd {
	if _, ok := m.plug.waits[t]; !ok {
		return nil
	}
	c := prompt.NewConfirmWith(m.styles, cleanText(o.Message), pluginConfirmTag{tok: t},
		prompt.ConfirmOptions{Yes: cleanText(o.Yes), No: cleanText(o.No), DefaultYes: o.Default})
	m.pushOverlay(&pluginConfirmOverlay{plugBase: plugBase{m: m, tok: t}, c: c})
	return nil
}

// ---- Host: pickers

// pickerItems converts the host items.
func pickerItems(in []pluginapi.PickItem) []picker.Item {
	out := make([]picker.Item, len(in))
	for i, it := range in {
		out[i] = picker.Item{ID: it.ID, Text: cleanText(it.Text), Detail: cleanText(it.Detail), Keybinding: cleanText(it.Keybinding), Disabled: it.Disabled}
	}
	return out
}

// OpenPicker opens a picker. The result is Values{id} of the chosen item, or Values{nil}
// when the user closed it. A Live picker also reports each query change (Kind "query",
// More) and a Preview picker each highlight (Kind "move", More).
func (h *pluginHost) OpenPicker(t pluginapi.Token, id uint64, o pluginapi.PickerSpec) error {
	m := h.m
	if len(o.Items) > picker.MaxItems {
		return pluginapi.Errf(pluginapi.CodeLimit, "a picker takes at most %d items", picker.MaxItems)
	}
	if m.plugPicker(id) != nil || m.plug.ui.opening[id] != nil {
		return pluginapi.Errf(pluginapi.CodeInvalid, "picker %d is already open", id)
	}
	if err := m.plugWait(t, waitPicker); err != nil {
		return err
	}
	pp := &pendingPicker{tok: t, spec: o}
	if m.plug.ui.opening == nil {
		m.plug.ui.opening = map[uint64]*pendingPicker{}
	}
	m.plug.ui.opening[id] = pp
	m.plug.later = append(m.plug.later, func() tea.Cmd { return m.openPicker(id, pp) })
	return nil
}

func (m *Model) openPicker(id uint64, pp *pendingPicker) tea.Cmd {
	ui := &m.plug.ui
	if ui.opening[id] != pp {
		return nil // closed or cancelled before the pump got here
	}
	delete(ui.opening, id)
	t, o := pp.tok, pp.spec
	if _, ok := m.plug.waits[t]; !ok {
		return nil
	}
	p := picker.New(m.styles, picker.Options{
		Title: cleanText(o.Title), Footer: cleanText(o.Footer), Placeholder: cleanText(o.Placeholder), InitialQuery: cleanText(o.InitialQuery),
		Items: pickerItems(o.Items), Fuzzy: o.Fuzzy, Live: o.Live, Preview: o.Preview,
	})
	ov := &pluginPickerOverlay{plugBase: plugBase{m: m, tok: t}, id: id, p: p}
	m.pushOverlay(ov)
	cmds := []tea.Cmd{ov.cmdFor(p.Init())}
	if pp.status != nil {
		p.SetStatus(cleanText(*pp.status))
	}
	if pp.selected != nil {
		cmds = append(cmds, ov.cmdFor(p.Select(*pp.selected)))
	}
	return tea.Batch(cmds...)
}

// UpdatePicker changes the open picker. The commands it causes (a new ranking) are
// collected like those of Host.Do. A picker that is still waiting for the pump to open it
// takes the change into its spec.
func (h *pluginHost) UpdatePicker(id uint64, u pluginapi.PickerUpdate) error {
	m := h.m
	if u.Items != nil && len(*u.Items) > picker.MaxItems {
		return pluginapi.Errf(pluginapi.CodeLimit, "a picker takes at most %d items", picker.MaxItems)
	}
	if pp := m.plug.ui.opening[id]; pp != nil {
		if u.Title != nil {
			pp.spec.Title = *u.Title
		}
		if u.Footer != nil {
			pp.spec.Footer = *u.Footer
		}
		if u.Status != nil {
			pp.status = u.Status
		}
		if u.Items != nil {
			pp.spec.Items = *u.Items
		}
		if u.Selected != nil {
			pp.selected = u.Selected
		}
		return nil
	}
	ov := m.plugPicker(id)
	if ov == nil {
		return pluginapi.Errf(pluginapi.CodeClosed, "picker %d is not open", id)
	}
	if u.Title != nil {
		ov.p.SetTitle(cleanText(*u.Title))
	}
	if u.Footer != nil {
		ov.p.SetFooter(cleanText(*u.Footer))
	}
	if u.Status != nil {
		ov.p.SetStatus(cleanText(*u.Status))
	}
	if u.Items != nil {
		m.pending = tea.Batch(m.pending, ov.cmdFor(ov.p.SetItems(pickerItems(*u.Items))))
	}
	if u.Selected != nil {
		m.pending = tea.Batch(m.pending, ov.cmdFor(ov.p.Select(*u.Selected)))
	}
	return nil
}

// ClosePicker closes the picker the plugin asked to close. Its wait is forgotten, not
// resolved: the runtime knows it closed it.
func (h *pluginHost) ClosePicker(id uint64) {
	m := h.m
	if pp := m.plug.ui.opening[id]; pp != nil {
		delete(m.plug.ui.opening, id)
		delete(m.plug.waits, pp.tok)
	}
	if ov := m.plugPicker(id); ov != nil {
		delete(m.plug.waits, ov.tok)
		m.plugRemove(ov)
	}
}

// ---- Host: popups

// ShowPopup shows a popup. The result is Values{item ID} of the chosen row, or Values{nil}
// when it closed without one.
func (h *pluginHost) ShowPopup(t pluginapi.Token, id uint64, o pluginapi.PopupSpec) error {
	m := h.m
	if len(o.Items) > picker.MaxItems {
		return pluginapi.Errf(pluginapi.CodeLimit, "a popup takes at most %d items", picker.MaxItems)
	}
	if err := m.plugWait(t, waitPopup); err != nil {
		return err
	}
	m.plug.later = append(m.plug.later, func() tea.Cmd { return m.openPopup(t, id, o) })
	return nil
}

func (m *Model) openPopup(t pluginapi.Token, id uint64, o pluginapi.PopupSpec) tea.Cmd {
	if _, ok := m.plug.waits[t]; !ok {
		return nil
	}
	var items []popup.Item
	if len(o.Items) > 0 {
		items = make([]popup.Item, len(o.Items))
		for i, it := range o.Items {
			items[i] = popup.Item{Label: cleanText(it.Text), Detail: cleanText(it.Detail), Data: pluginPopupData{tok: t, item: it.ID}}
		}
	} else {
		items = make([]popup.Item, len(o.Lines))
		for i, l := range o.Lines {
			items[i] = popup.Item{Label: cleanText(l), Data: pluginPopupData{tok: t, line: true}}
		}
	}
	title := cleanText(o.Title)
	if pad := o.Width - 2 - ansi.StringWidth(title); pad > 0 {
		title += strings.Repeat(" ", pad) // the box is as wide as its widest line
	}
	p := popup.New(popupStyles(m.styles))
	p.Show(m.popupAnchor(o.Anchor, title, items, o.Width), title, items)
	m.pushOverlay(&pluginPopupOverlay{plugBase: plugBase{m: m, tok: t}, id: id, p: p})
	return nil
}

// popupAnchor is where a plugin popup goes: under the caret, or in the middle of the frame
// (also when the caret is not on screen).
func (m *Model) popupAnchor(anchor, title string, items []popup.Item, width int) popup.Point {
	if anchor != "center" {
		if x, y, ok := m.caret(); ok {
			return popup.Point{X: x, Y: y}
		}
	}
	w := max(ansi.StringWidth(title), width-2)
	for _, it := range items {
		n := ansi.StringWidth(it.Label)
		if it.Detail != "" {
			n += 2 + ansi.StringWidth(it.Detail)
		}
		w = max(w, n)
	}
	w = min(w+2, m.width)
	rows := min(max(len(items), 1), 10)
	return popup.Point{X: max((m.width-w)/2, 0), Y: max((m.height-rows-1)/2-1, 0)}
}

// ClosePopup closes the popup the plugin asked to close; its wait is forgotten.
func (h *pluginHost) ClosePopup(id uint64) {
	m := h.m
	for _, o := range slices.Clone(m.overlays) {
		if po, ok := o.(*pluginPopupOverlay); ok && po.id == id && !po.closed {
			delete(m.plug.waits, po.tok)
			m.plugRemove(po)
		}
	}
}

// pluginPopupChosen resolves the wait of a plugin popup whose row was chosen. lspMessage
// calls it for every popup.Chosen; it reports whether the row was a plugin's.
func (m *Model) pluginPopupChosen(c popup.Chosen) (tea.Cmd, bool) {
	d, ok := c.Item.Data.(pluginPopupData)
	if !ok {
		return nil, false
	}
	var v any
	if !d.line {
		v = d.item
	}
	return m.plugFinish(pluginapi.Completion{Token: d.tok, Values: []any{v}}), true
}

// ---- answers

// pluginAnswer handles the answer of a prompt that a plugin opened, by its tag. answer
// calls it first. It reports whether the answer was a plugin's.
func (m *Model) pluginAnswer(d prompt.Done) (tea.Cmd, bool) {
	switch tag := d.Tag.(type) {
	case pluginInputTag:
		var v any
		if !tag.esc {
			v = d.Text
		}
		return m.plugFinish(pluginapi.Completion{Token: tag.tok, Values: []any{v}}), true
	case pluginConfirmTag:
		yes := !tag.esc && d.Outcome == prompt.Yes
		return m.plugFinish(pluginapi.Completion{Token: tag.tok, Values: []any{yes}}), true
	case pluginPopupTag:
		return m.plugFinish(pluginapi.Completion{Token: tag.tok, Values: []any{nil}}), true
	case pluginPickTag:
		return m.pickOrdered(tag), true
	case quitDiscardTag:
		return m.quitDiscardAnswer(d), true
	case pluginPickRankTag:
		ov := m.plugPicker(tag.id)
		if ov == nil {
			return nil, true
		}
		return ov.cmdFor(ov.p.Update(tag.msg)), true
	}
	return nil, false
}

// pickOrdered delivers the batches of events of an open picker in the order the overlay
// produced them: a batch that arrives before an earlier one waits for it. A query that
// reached the plugin out of order would leave it with the older text. Once the overlay is
// closed the rest has nothing to wait for (the wait is over or ending).
func (m *Model) pickOrdered(tag pluginPickTag) tea.Cmd {
	var ov *pluginPickerOverlay
	for _, o := range m.overlays {
		if po, ok := o.(*pluginPickerOverlay); ok && po.tok == tag.tok && !po.closed {
			ov = po
		}
	}
	if ov == nil {
		return m.pickEvents(tag)
	}
	if tag.seq != ov.next {
		if ov.held == nil {
			ov.held = make(map[int]pluginPickTag)
		}
		ov.held[tag.seq] = tag
		return nil
	}
	var cmds []tea.Cmd
	for {
		cmds = append(cmds, m.pickEvents(tag))
		ov.next++
		held, ok := ov.held[ov.next]
		if !ok {
			return tea.Batch(cmds...)
		}
		delete(ov.held, ov.next)
		tag = held
	}
}

// pickEvents delivers the events of a picker to the runtime, in order.
func (m *Model) pickEvents(tag pluginPickTag) tea.Cmd {
	cmds := make([]tea.Cmd, 0, len(tag.events))
	for _, e := range tag.events {
		switch e := e.(type) {
		case picker.Changed:
			cmds = append(cmds, m.plugItem(pluginapi.Completion{Token: tag.tok, Kind: "query", Values: []any{e.Query}}))
		case picker.Moved:
			cmds = append(cmds, m.plugItem(pluginapi.Completion{Token: tag.tok, Kind: "move", Values: []any{e.ID}}))
		case picker.Chosen:
			cmds = append(cmds, m.plugFinish(pluginapi.Completion{Token: tag.tok, Values: []any{e.ID}}))
		case picker.Closed:
			cmds = append(cmds, m.plugFinish(pluginapi.Completion{Token: tag.tok, Values: []any{nil}}))
		}
	}
	return tea.Batch(cmds...)
}
