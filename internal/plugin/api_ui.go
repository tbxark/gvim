package plugin

import (
	"errors"
	"strconv"
	"sync/atomic"
	"time"
	"unicode/utf8"

	lua "github.com/yuin/gopher-lua"

	"github.com/tbxark/gvim/internal/pluginapi"
)

// gvim.ui prompts, pickers and popups, and gvim.clipboard (FINAL 5.6, stage S13).
//
//	gvim.ui.input(opts) ⏳ -> string | nil           nil: cancelled with Esc
//	gvim.ui.confirm(msg, opts?) ⏳ -> boolean        Esc answers false
//	gvim.ui.choose(msg, choices) ⏳ -> id | nil
//	gvim.ui.select(items, opts?) ⏳ -> item, index | nil     index is 1-based (AMENDMENTS C24)
//	gvim.ui.picker(spec) -> Picker                   p:open() p:set_items(items) p:set_title(s)
//	                                                 p:set_footer(s) p:set_status(s) p:query()
//	                                                 p:selected() p:close() p:is_open()
//	gvim.ui.popup(spec) -> Popup                     opens at once; p:close() p:set_lines(lines)
//	gvim.ui.focus(area), gvim.ui.size(), gvim.clipboard.set(text)
//
// A prompt that the user cancels with Esc resolves normally (nil, false). One the panic
// chord or an unload cancels resolves `nil, "cancelled: ..."`. A picker's or popup's
// callbacks run as tasks after user interaction (the plugin_timeout_ms budget class);
// on_close runs when the user cancels (Esc, panic chord), not after a selection and not
// after p:close().
//
// Owning a wait. The Host async calls carry no plugin name, so the ui cannot match an
// overlay to its owner. The runtime ties every wait to its plugin instead: each wait it
// registers has a cancel function that tells the host (CancelWait, ClosePicker,
// ClosePopup), and unload, the end of a parked task and p:close() all run it. The host
// therefore never keeps an overlay of a plugin that is gone.

const (
	kindPicker = "Picker"
	kindPopup  = "Popup"

	uiMaxOverlays = 8       // live pickers and popups of one plugin
	uiMaxItems    = 50_000  // items of a picker, as picker.MaxItems
	uiMaxLines    = 1000    // lines of a popup
	uiMaxText     = 4096    // bytes of one item field or popup line
	uiMaxWidth    = 500     // cells of a popup
	uiMaxClip     = 8 << 20 // bytes of gvim.clipboard.set
	uiQueryDelay  = 60 * time.Millisecond
)

// overlaySeq numbers the pickers and popups of all runtimes; the host keys overlays by it.
var overlaySeq atomic.Uint64

func init() {
	registerAwait("ui_input", awaitInput)
	registerAwait("ui_confirm", awaitConfirm)
	registerAwait("ui_choose", awaitChoose)
	registerAwait("ui_pick", awaitPick)
}

// clipText cuts s to at most n bytes on a rune boundary.
func clipText(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n]
}

// parsePickItem reads one item: a string, a number, or a table {text=, detail=,
// keybinding=, disabled=}. "label" is accepted for "text", as choose gives it.
func parsePickItem(v lua.LValue, id int) (pluginapi.PickItem, error) {
	it := pluginapi.PickItem{ID: id}
	switch x := v.(type) {
	case lua.LString:
		it.Text = string(x)
	case lua.LNumber:
		it.Text = x.String()
	case *lua.LTable:
		text := x.RawGetString("text")
		if text == lua.LNil {
			text = x.RawGetString("label")
		}
		if text == lua.LNil {
			text = x.RawGetString("id") // a choice with no label shows its id
		}
		switch t := text.(type) {
		case lua.LString:
			it.Text = string(t)
		case lua.LNumber:
			it.Text = t.String()
		default:
			return it, invalidErr("item %d: text must be a string", id+1)
		}
		for _, f := range []struct {
			key string
			dst *string
		}{{"detail", &it.Detail}, {"keybinding", &it.Keybinding}} {
			switch s := x.RawGetString(f.key).(type) {
			case *lua.LNilType:
			case lua.LString:
				*f.dst = clipText(string(s), uiMaxText)
			default:
				return it, invalidErr("item %d: %s must be a string", id+1, f.key)
			}
		}
		if d, ok := x.RawGetString("disabled").(lua.LBool); ok {
			it.Disabled = bool(d)
		}
	default:
		return it, invalidErr("item %d: string or table expected, got %s", id+1, v.Type())
	}
	it.Text = clipText(it.Text, uiMaxText)
	return it, nil
}

// parsePickItems reads an array of items. It returns the host items (ID is the 0-based
// position) and the original elements, which the callbacks hand back.
func parsePickItems(t *lua.LTable) ([]pluginapi.PickItem, []lua.LValue, error) {
	n := t.Len()
	if n > uiMaxItems {
		return nil, nil, pluginapi.Errf(pluginapi.CodeLimit, "too many items (max %d)", uiMaxItems)
	}
	items := make([]pluginapi.PickItem, 0, n)
	orig := make([]lua.LValue, 0, n)
	for i := 1; i <= n; i++ {
		v := t.RawGetInt(i)
		it, err := parsePickItem(v, i-1)
		if err != nil {
			return nil, nil, err
		}
		items = append(items, it)
		orig = append(orig, v)
	}
	return items, orig, nil
}

// optFunc reads an optional function option.
func optFunc(o *bufTableOpts, key string) *lua.LFunction {
	switch v := o.get(key).(type) {
	case *lua.LNilType:
		return nil
	case *lua.LFunction:
		return v
	}
	o.fail("option '" + key + "' must be a function")
	return nil
}

// optString reads an optional string option, cut to the text cap.
func optString(o *bufTableOpts, key string) string {
	s, _ := o.str(key)
	return clipText(s, uiMaxText)
}

// cancelWaitFn returns the cancel function of a plain wait: tell the host to forget it.
func (p *plugin) cancelWaitFn(tok pluginapi.Token) func() {
	return func() { p.r.guard.CancelWait(tok) }
}

// cancelPickerFn returns the cancel function of a wait that owns a picker.
func (p *plugin) cancelPickerFn(id uint64, tok pluginapi.Token) func() {
	return func() {
		p.r.guard.ClosePicker(id)
		p.r.guard.CancelWait(tok)
	}
}

// callbackAlive reports whether p may start a callback now.
func (p *plugin) callbackAlive(gen uint64) bool { return !p.dead && p.gen == gen && !p.muted() }

// ---------- input ----------

// validator makes the Validate closure of an input prompt: it runs fn(text) synchronously
// (25 ms, no yield) and turns a returned string into the error that keeps the prompt open.
// A failing validator is reported and accepts the text, so a broken plugin cannot trap the
// user in a prompt.
func (p *plugin) validator(fn *lua.LFunction) func(string) error {
	gen := p.gen
	return func(text string) error {
		r := p.r
		if !p.callbackAlive(gen) {
			return nil
		}
		defer r.enter("validate")()
		vals, rerr := r.callSync(p, fn, vetoBudget, "input validate", lua.LString(text))
		if rerr != nil {
			r.noteFailure(p, rerr)
			return nil
		}
		if len(vals) > 0 {
			if s, ok := vals[0].(lua.LString); ok && s != "" {
				return errors.New(string(s))
			}
		}
		return nil
	}
}

// changeCallback makes the OnChange closure of an input prompt: it only queues a callback
// task; the ui drains the runtime afterwards.
func (p *plugin) changeCallback(fn *lua.LFunction) func(string) {
	gen := p.gen
	return func(text string) {
		if p.callbackAlive(gen) {
			p.r.startCallback(p, originCallback, "input on_change", fn, lua.LString(text))
		}
	}
}

// awaitInput is gvim.ui.input(opts).
func awaitInput(c *awaitCall) {
	t, ok := bufOptional(c, 0, "input")
	if !ok {
		return
	}
	o := bufTableOpts{t: t}
	spec := pluginapi.InputSpec{
		Prompt:    optString(&o, "prompt"),
		SelectAll: o.boolean("select_all", true),
	}
	if def, ok := o.str("default"); ok {
		spec.Default = clipText(def, uiMaxText)
	}
	validate, onChange := optFunc(&o, "validate"), optFunc(&o, "on_change")
	if o.err != "" {
		c.Bad(1, "input", o.err)
		return
	}
	if validate != nil {
		spec.Validate = c.p.validator(validate)
	}
	if onChange != nil {
		spec.OnChange = c.p.changeCallback(onChange)
	}
	c.openWait("input", func(tok pluginapi.Token) error { return c.r.guard.Input(tok, spec) }, nil)
}

// openWait asks the host to open a prompt with the token the host will resolve, and parks
// the task. open does the Host call; conv is the optional Convert of the completion.
func (c *awaitCall) openWait(name string, open func(pluginapi.Token) error, conv func(pluginapi.Completion) []lua.LValue) {
	if err := c.CheckPark(false); err != nil {
		c.Fail(err)
		return
	}
	tok := c.NewToken()
	if err := open(tok); err != nil {
		c.Fail(err)
		return
	}
	c.Park(&parkSpec{Token: tok, Convert: conv, Cancel: c.p.cancelWaitFn(tok)})
}

// ---------- confirm ----------

// awaitConfirm is gvim.ui.confirm(msg, opts?).
func awaitConfirm(c *awaitCall) {
	msg, ok := "", false
	if len(c.args) > 0 {
		var s lua.LString
		if s, ok = c.args[0].(lua.LString); ok {
			msg = string(s)
		}
	}
	if !ok {
		c.Bad(1, "confirm", "string expected")
		return
	}
	t, ok := bufOptional(c, 1, "confirm")
	if !ok {
		return
	}
	o := bufTableOpts{t: t}
	spec := pluginapi.ConfirmSpec{Message: clipText(msg, uiMaxText), Default: o.boolean("default", false)}
	spec.Yes, spec.No = optString(&o, "yes"), optString(&o, "no")
	if o.err != "" {
		c.Bad(2, "confirm", o.err)
		return
	}
	c.openWait("confirm", func(tok pluginapi.Token) error { return c.r.guard.Confirm(tok, spec) }, func(cp pluginapi.Completion) []lua.LValue {
		if cp.Err != "" {
			return []lua.LValue{lua.LTrue, lua.LNil, lua.LString(cp.Err)}
		}
		yes := len(cp.Values) > 0 && cp.Values[0] == true
		return []lua.LValue{lua.LTrue, lua.LBool(yes)}
	})
}

// ---------- choose and select ----------

// pickWait opens a picker the task waits on and resumes the task with the chosen index
// (0-based) mapped by pick, or nil when the user cancelled.
func (c *awaitCall) pickWait(spec pluginapi.PickerSpec, pick func(i int) lua.LValue) {
	if err := c.CheckPark(false); err != nil {
		c.Fail(err)
		return
	}
	tok, id := c.NewToken(), overlaySeq.Add(1)
	if err := c.r.guard.OpenPicker(tok, id, spec); err != nil {
		c.Fail(err)
		return
	}
	n := len(spec.Items)
	c.Park(&parkSpec{Token: tok, Cancel: c.p.cancelPickerFn(id, tok), Convert: func(cp pluginapi.Completion) []lua.LValue {
		if cp.Err != "" {
			return []lua.LValue{lua.LTrue, lua.LNil, lua.LString(cp.Err)}
		}
		if len(cp.Values) > 0 {
			if i, ok := wholeNumber(cp.Values[0]); ok && i >= 0 && int(i) < n {
				return []lua.LValue{lua.LTrue, pick(int(i))}
			}
		}
		return []lua.LValue{lua.LTrue, lua.LNil}
	}})
}

// awaitChoose is gvim.ui.choose(msg, choices): a picker without a filter; the result is
// the id of the chosen choice.
func awaitChoose(c *awaitCall) {
	msg, ok := "", false
	if len(c.args) > 0 {
		var s lua.LString
		if s, ok = c.args[0].(lua.LString); ok {
			msg = string(s)
		}
	}
	if !ok {
		c.Bad(1, "choose", "string expected")
		return
	}
	if len(c.args) < 2 {
		c.Bad(2, "choose", "table expected")
		return
	}
	list, ok := c.args[1].(*lua.LTable)
	if !ok {
		c.Bad(2, "choose", "table expected")
		return
	}
	items, orig, err := parsePickItems(list)
	if err != nil {
		c.Fail(err)
		return
	}
	if len(items) == 0 {
		c.Fail(invalidErr("no choices"))
		return
	}
	ids := make([]lua.LValue, len(orig))
	for i, v := range orig {
		ids[i] = v
		if tb, ok := v.(*lua.LTable); ok {
			if id := tb.RawGetString("id"); id != lua.LNil {
				ids[i] = id
			} else {
				ids[i] = lua.LNil
			}
		}
	}
	c.pickWait(pluginapi.PickerSpec{Title: clipText(msg, uiMaxText), Items: items}, func(i int) lua.LValue { return ids[i] })
}

// awaitPick is the Go half of gvim.ui.select: it takes the items (strings or tables) and
// the options, and returns the 1-based index of the chosen one.
func awaitPick(c *awaitCall) {
	list, ok := (*lua.LTable)(nil), false
	if len(c.args) > 0 {
		list, ok = c.args[0].(*lua.LTable)
	}
	if !ok {
		c.Bad(1, "select", "table expected")
		return
	}
	t, ok := bufOptional(c, 1, "select")
	if !ok {
		return
	}
	o := bufTableOpts{t: t}
	spec := pluginapi.PickerSpec{
		Title:        optString(&o, "title"),
		Placeholder:  optString(&o, "placeholder"),
		Footer:       optString(&o, "footer"),
		InitialQuery: optString(&o, "initial_query"),
		Fuzzy:        true,
	}
	if f, ok := o.str("filter"); ok {
		switch f {
		case "fuzzy":
		case "none":
			spec.Fuzzy = false
		default:
			o.fail("option 'filter' must be \"fuzzy\" or \"none\"")
		}
	}
	if o.err != "" {
		c.Bad(2, "select", o.err)
		return
	}
	items, _, err := parsePickItems(list)
	if err != nil {
		c.Fail(err)
		return
	}
	spec.Items = items
	c.pickWait(spec, func(i int) lua.LValue { return lua.LNumber(i + 1) })
}

// selectLua is gvim.ui.select: it applies the `format` option in the task, then asks the
// Go half to pick, and maps the index back to the caller's item.
const selectLua = `
local pick, type, error = ...
return function(items, opts)
  if type(items) ~= "table" then
    error("bad argument #1 to 'select' (table expected, got " .. type(items) .. ")", 2)
  end
  local fmt = opts ~= nil and type(opts) == "table" and opts.format or nil
  local list = items
  if fmt ~= nil then
    if type(fmt) ~= "function" then error("bad argument #2 to 'select' (option 'format' must be a function)", 2) end
    list = {}
    for i = 1, #items do
      local it = items[i]
      local d = { text = fmt(it) }
      if type(it) == "table" then d.detail, d.keybinding, d.disabled = it.detail, it.keybinding, it.disabled end
      list[i] = d
    end
  end
  local idx, err = pick(list, opts)
  if idx == nil then return nil, err end
  return items[idx], idx
end
`

// ---------- the Picker handle ----------

// uiState counts the live pickers and popups of one plugin.
type uiState struct{ live int }

// luaPicker is a picker a plugin created. It lives from p:open() until it is chosen,
// cancelled or closed.
type luaPicker struct {
	p  *plugin
	st *uiState
	id uint64

	title, footer, placeholder, initial string
	fuzzy                               bool

	items []lua.LValue
	pick  []pluginapi.PickItem

	onQuery, onSelect, onMove, onClose *lua.LFunction

	open     bool
	tok      pluginapi.Token
	wait     *wait
	query    string
	qgen     uint64
	stopQ    func()
	selected int // item index of the highlight; -1 when unknown
	handle   *lua.LUserData
}

// pickerRef is the payload of a Picker handle. gen is 0 for the handle the plugin owns; the
// handle given to on_query carries the generation of that call, and its set_items is
// dropped once a newer query started.
type pickerRef struct {
	pk  *luaPicker
	gen uint64
}

func (r *pickerRef) handleString() string { return strconv.FormatUint(r.pk.id, 10) }

func (pk *luaPicker) spec() pluginapi.PickerSpec {
	return pluginapi.PickerSpec{
		Title: pk.title, Footer: pk.footer, Placeholder: pk.placeholder, InitialQuery: pk.initial,
		Items: pk.pick, Fuzzy: pk.fuzzy, Live: pk.onQuery != nil, Preview: true,
	}
}

// shut marks the picker closed and stops its timer. It reports whether it was open.
func (pk *luaPicker) shut() bool {
	if !pk.open {
		return false
	}
	pk.open = false
	pk.st.live--
	if pk.stopQ != nil {
		pk.stopQ()
		pk.stopQ = nil
	}
	return true
}

// openIt opens the picker on the host.
func (pk *luaPicker) openIt() error {
	p := pk.p
	if pk.open {
		return pluginapi.Errf(pluginapi.CodeBusy, "picker is already open")
	}
	if pk.st.live >= uiMaxOverlays {
		return pluginapi.Errf(pluginapi.CodeLimit, "overlay quota (%d)", uiMaxOverlays)
	}
	tok, w := p.newWait(pk.onItem, func() {
		pk.shut()
		p.r.guard.ClosePicker(pk.id)
		p.r.guard.CancelWait(pk.tok)
	})
	pk.tok, pk.wait = tok, w
	if err := p.r.guard.OpenPicker(tok, pk.id, pk.spec()); err != nil {
		p.r.dropWait(w)
		return err
	}
	pk.open = true
	pk.st.live++
	pk.selected = -1
	pk.query = pk.initial
	if pk.onQuery != nil {
		pk.runQuery()
	}
	return nil
}

// closeIt closes the picker from the plugin's side: no callback runs.
func (pk *luaPicker) closeIt() {
	if pk.open && pk.wait != nil {
		pk.p.r.cancelWait(pk.wait)
	}
}

// runQuery starts on_query for the current query, as a callback task.
func (pk *luaPicker) runQuery() {
	p := pk.p
	if !pk.open || pk.onQuery == nil || p.dead || p.muted() {
		return
	}
	pk.qgen++
	ref := p.newObject(kindPicker, &pickerRef{pk: pk, gen: pk.qgen})
	p.r.startCallback(p, originCallback, "picker on_query", pk.onQuery, lua.LString(pk.query), ref)
}

// onItem receives what the ui reports about the open picker: a query change, a highlight
// move, and the final choice or close. It runs on the UI goroutine from Drain.
func (pk *luaPicker) onItem(c pluginapi.Completion) {
	p := pk.p
	if p.dead {
		return
	}
	switch {
	case c.Kind == "query":
		if len(c.Values) > 0 {
			if s, ok := c.Values[0].(string); ok {
				pk.query = s
			}
		}
		if pk.onQuery != nil {
			if pk.stopQ != nil {
				pk.stopQ()
			}
			pk.stopQ = p.afterTimer(uiQueryDelay, pk.runQuery)
		}
	case c.Kind == "move":
		if noItem(c) {
			pk.selected = -1
		} else if i, ok := firstIndex(c, len(pk.items)); ok {
			pk.selected = i
			if pk.onMove != nil && !p.muted() {
				p.r.startCallback(p, originCallback, "picker on_move", pk.onMove, pk.items[i], pk.handle)
			}
		}
	case c.Kind == "" && !c.More:
		pk.end(c)
	}
}

// noItem reports whether a move completion says no row is highlighted (picker.NoItem).
func noItem(c pluginapi.Completion) bool {
	if len(c.Values) == 0 {
		return false
	}
	i, ok := wholeNumber(c.Values[0])
	return ok && i == -1
}

// firstIndex reads the item index a completion carries, if it is a valid one.
func firstIndex(c pluginapi.Completion, n int) (int, bool) {
	if len(c.Values) == 0 {
		return 0, false
	}
	i, ok := wholeNumber(c.Values[0])
	return int(i), ok && i >= 0 && int(i) < n
}

// end handles the final completion: a choice runs on_select, anything else on_close.
func (pk *luaPicker) end(c pluginapi.Completion) {
	p := pk.p
	pk.shut()
	if p.muted() {
		return
	}
	if c.Err == "" {
		if i, ok := firstIndex(c, len(pk.items)); ok {
			if pk.onSelect != nil {
				p.r.startCallback(p, originCallback, "picker on_select", pk.onSelect, pk.items[i], lua.LNumber(i+1), pk.handle)
			}
			return
		}
	}
	if pk.onClose != nil {
		p.r.startCallback(p, originCallback, "picker on_close", pk.onClose)
	}
}

// setItems replaces the items and updates the open picker.
func (pk *luaPicker) setItems(items []pluginapi.PickItem, orig []lua.LValue) error {
	pk.pick, pk.items = items, orig
	if !pk.open {
		return nil
	}
	u := pluginapi.PickerUpdate{Items: &items}
	return pk.p.r.guard.UpdatePicker(pk.id, u)
}

// update sends the changed texts to the open picker.
func (pk *luaPicker) update(u pluginapi.PickerUpdate) error {
	if !pk.open {
		return nil
	}
	return pk.p.r.guard.UpdatePicker(pk.id, u)
}

// installPicker installs gvim.ui.picker and the Picker methods.
func installPicker(e *apiEnv, ui *lua.LTable, st *uiState) {
	p := e.p
	methods := e.kind(kindPicker).methods

	self := func(L *lua.LState) *pickerRef { return checkObject(L, 1, kindPicker).(*pickerRef) }
	ret := func(L *lua.LState, err error) int {
		if err != nil {
			return bufRet(L, err)
		}
		L.Push(lua.LTrue)
		return 1
	}

	e.set(ui, "picker", func(L *lua.LState) int {
		spec := L.CheckTable(1)
		o := bufTableOpts{t: spec}
		pk := &luaPicker{p: p, st: st, id: overlaySeq.Add(1), fuzzy: true, selected: -1}
		pk.title, pk.footer = optString(&o, "title"), optString(&o, "footer")
		pk.placeholder, pk.initial = optString(&o, "placeholder"), optString(&o, "initial_query")
		pk.onQuery, pk.onSelect = optFunc(&o, "on_query"), optFunc(&o, "on_select")
		pk.onMove, pk.onClose = optFunc(&o, "on_move"), optFunc(&o, "on_close")
		if f, ok := o.str("filter"); ok {
			switch f {
			case "fuzzy":
			case "none":
				pk.fuzzy = false
			default:
				o.fail("option 'filter' must be \"fuzzy\" or \"none\"")
			}
		}
		if lv := o.get("items"); lv != lua.LNil {
			tb, ok := lv.(*lua.LTable)
			if !ok {
				o.fail("option 'items' must be a table")
			} else if items, orig, err := parsePickItems(tb); err != nil {
				return bufRet(L, err)
			} else {
				pk.pick, pk.items = items, orig
			}
		}
		if o.err != "" {
			L.ArgError(1, o.err)
		}
		pk.handle = p.newObject(kindPicker, &pickerRef{pk: pk})
		L.Push(pk.handle)
		return 1
	})

	e.set(methods, "open", func(L *lua.LState) int { return ret(L, self(L).pk.openIt()) })
	e.set(methods, "close", func(L *lua.LState) int {
		self(L).pk.closeIt()
		L.Push(lua.LTrue)
		return 1
	})
	e.set(methods, "is_open", func(L *lua.LState) int {
		L.Push(lua.LBool(self(L).pk.open))
		return 1
	})
	e.set(methods, "query", func(L *lua.LState) int {
		L.Push(lua.LString(self(L).pk.query))
		return 1
	})
	e.set(methods, "selected", func(L *lua.LState) int {
		pk := self(L).pk
		if pk.selected >= 0 && pk.selected < len(pk.items) {
			L.Push(pk.items[pk.selected])
		} else {
			L.Push(lua.LNil)
		}
		return 1
	})
	e.set(methods, "set_items", func(L *lua.LState) int {
		ref := self(L)
		list := L.CheckTable(2)
		if ref.gen != 0 && ref.gen != ref.pk.qgen {
			return bufRet(L, pluginapi.Errf(pluginapi.CodeStale, "the query changed; these items are out of date"))
		}
		items, orig, err := parsePickItems(list)
		if err != nil {
			return bufRet(L, err)
		}
		return ret(L, ref.pk.setItems(items, orig))
	})
	text := func(name string, set func(pk *luaPicker, s string) pluginapi.PickerUpdate) {
		e.set(methods, name, func(L *lua.LState) int {
			ref := self(L)
			s := clipText(L.CheckString(2), uiMaxText)
			return ret(L, ref.pk.update(set(ref.pk, s)))
		})
	}
	text("set_title", func(pk *luaPicker, s string) pluginapi.PickerUpdate {
		pk.title = s
		return pluginapi.PickerUpdate{Title: &s}
	})
	text("set_footer", func(pk *luaPicker, s string) pluginapi.PickerUpdate {
		pk.footer = s
		return pluginapi.PickerUpdate{Footer: &s}
	})
	text("set_status", func(pk *luaPicker, s string) pluginapi.PickerUpdate {
		return pluginapi.PickerUpdate{Status: &s}
	})
}

// ---------- the Popup handle ----------

// luaPopup is a popup a plugin opened.
type luaPopup struct {
	p  *plugin
	st *uiState
	id uint64

	spec  pluginapi.PopupSpec
	items []lua.LValue

	onSelect, onClose *lua.LFunction

	open   bool
	tok    pluginapi.Token
	wait   *wait
	handle *lua.LUserData
}

func (pp *luaPopup) handleString() string { return strconv.FormatUint(pp.id, 10) }

func (pp *luaPopup) shut() bool {
	if !pp.open {
		return false
	}
	pp.open = false
	pp.st.live--
	return true
}

// show opens (or reopens) the popup on the host with a fresh wait.
func (pp *luaPopup) show() error {
	p := pp.p
	tok, w := p.newWait(pp.onItem, func() {
		pp.shut()
		p.r.guard.ClosePopup(pp.id)
		p.r.guard.CancelWait(pp.tok)
	})
	pp.tok, pp.wait = tok, w
	if err := p.r.guard.ShowPopup(tok, pp.id, pp.spec); err != nil {
		p.r.dropWait(w)
		return err
	}
	return nil
}

// closeIt closes the popup from the plugin's side: no callback runs.
func (pp *luaPopup) closeIt() {
	if pp.open && pp.wait != nil {
		pp.p.r.cancelWait(pp.wait)
	}
}

// onItem receives the final result of the popup: a chosen row runs on_select, anything
// else on_close.
func (pp *luaPopup) onItem(c pluginapi.Completion) {
	p := pp.p
	if p.dead || c.More || c.Kind != "" {
		return
	}
	pp.shut()
	if p.muted() {
		return
	}
	if c.Err == "" {
		if i, ok := firstIndex(c, len(pp.items)); ok {
			if pp.onSelect != nil {
				p.r.startCallback(p, originCallback, "popup on_select", pp.onSelect, pp.items[i], lua.LNumber(i+1), pp.handle)
			}
			return
		}
	}
	if pp.onClose != nil {
		p.r.startCallback(p, originCallback, "popup on_close", pp.onClose)
	}
}

// popupLines reads an array of strings (numbers are converted).
func popupLines(t *lua.LTable, max int) ([]string, error) {
	n := t.Len()
	if n > max {
		return nil, pluginapi.Errf(pluginapi.CodeLimit, "too many lines (max %d)", max)
	}
	out := make([]string, 0, n)
	for i := 1; i <= n; i++ {
		switch v := t.RawGetInt(i).(type) {
		case lua.LString:
			out = append(out, clipText(string(v), uiMaxText))
		case lua.LNumber:
			out = append(out, v.String())
		default:
			return nil, invalidErr("line %d must be a string", i)
		}
	}
	return out, nil
}

// installPopup installs gvim.ui.popup and the Popup methods.
func installPopup(e *apiEnv, ui *lua.LTable, st *uiState) {
	p := e.p
	methods := e.kind(kindPopup).methods
	self := func(L *lua.LState) *luaPopup { return checkObject(L, 1, kindPopup).(*luaPopup) }

	e.set(ui, "popup", func(L *lua.LState) int {
		spec := L.CheckTable(1)
		o := bufTableOpts{t: spec}
		pp := &luaPopup{p: p, st: st, id: overlaySeq.Add(1)}
		pp.spec.Title = optString(&o, "title")
		pp.spec.Anchor = "cursor"
		if a, ok := o.str("anchor"); ok {
			if a != "cursor" && a != "center" {
				o.fail("option 'anchor' must be \"cursor\" or \"center\"")
			}
			pp.spec.Anchor = a
		}
		if w := o.integer("width", 0); w < 0 || w > uiMaxWidth {
			o.fail("option 'width' must be between 0 and 500")
		} else {
			pp.spec.Width = w
		}
		pp.onSelect, pp.onClose = optFunc(&o, "on_select"), optFunc(&o, "on_close")
		lines, items := o.get("lines"), o.get("items")
		if lines != lua.LNil && items != lua.LNil {
			o.fail("give 'lines' or 'items', not both")
		}
		if lt, ok := lines.(*lua.LTable); ok {
			ls, err := popupLines(lt, uiMaxLines)
			if err != nil {
				return bufRet(L, err)
			}
			pp.spec.Lines = ls
		} else if lines != lua.LNil {
			o.fail("option 'lines' must be a table")
		}
		if it, ok := items.(*lua.LTable); ok {
			pick, orig, err := parsePickItems(it)
			if err != nil {
				return bufRet(L, err)
			}
			if len(pick) > uiMaxLines {
				return bufRet(L, pluginapi.Errf(pluginapi.CodeLimit, "too many items (max %d)", uiMaxLines))
			}
			pp.spec.Items, pp.items = pick, orig
		} else if items != lua.LNil {
			o.fail("option 'items' must be a table")
		}
		if o.err != "" {
			L.ArgError(1, o.err)
		}
		if st.live >= uiMaxOverlays {
			return bufRet(L, pluginapi.Errf(pluginapi.CodeLimit, "overlay quota (%d)", uiMaxOverlays))
		}
		if err := pp.show(); err != nil {
			return bufRet(L, err)
		}
		pp.open = true
		st.live++
		pp.handle = p.newObject(kindPopup, pp)
		L.Push(pp.handle)
		return 1
	})

	e.set(methods, "close", func(L *lua.LState) int {
		self(L).closeIt()
		L.Push(lua.LTrue)
		return 1
	})
	e.set(methods, "is_open", func(L *lua.LState) int {
		L.Push(lua.LBool(self(L).open))
		return 1
	})
	e.set(methods, "set_lines", func(L *lua.LState) int {
		pp := self(L)
		lines, err := popupLines(L.CheckTable(2), uiMaxLines)
		if err != nil {
			return bufRet(L, err)
		}
		if len(pp.spec.Items) > 0 {
			return bufRet(L, invalidErr("set_lines: this popup has items, not lines"))
		}
		pp.spec.Lines = lines
		if !pp.open {
			return bufTrue(L, nil)
		}
		// The host has no update call for a popup: close it and show it again, under a
		// new wait.
		pp.p.r.cancelWait(pp.wait)
		pp.open = true
		pp.st.live++
		if err := pp.show(); err != nil {
			pp.shut()
			return bufRet(L, err)
		}
		return bufTrue(L, nil)
	})
}

// ---------- installUI ----------

// installUI installs gvim.ui (input, confirm, choose, select, picker, popup, focus, size)
// and gvim.clipboard. The status bar items of gvim.ui.statusbar belong to stage S14.
func installUI(e *apiEnv) {
	r := e.r
	ui := e.area("ui")
	st := &uiState{}

	ui.RawSetString("input", e.async("ui_input"))
	ui.RawSetString("confirm", e.async("ui_confirm"))
	ui.RawSetString("choose", e.async("ui_choose"))
	pick := e.async("ui_pick")
	res := e.runLua("gvim/ui_select.lua", selectLua, 1, pick, e.L.GetGlobal("type"), e.L.GetGlobal("error"))
	ui.RawSetString("select", res[0])
	installPicker(e, ui, st)
	installPopup(e, ui, st)

	e.set(ui, "focus", func(L *lua.LState) int {
		return bufTrue(L, r.guard.Focus(L.CheckString(1)))
	})
	e.set(ui, "size", func(L *lua.LState) int {
		w, h := r.guard.ScreenSize()
		t := L.NewTable()
		t.RawSetString("width", lua.LNumber(w))
		t.RawSetString("height", lua.LNumber(h))
		L.Push(t)
		return 1
	})

	clip := e.area("clipboard")
	e.set(clip, "set", func(L *lua.LState) int {
		s := L.CheckString(1)
		if len(s) > uiMaxClip {
			return bufRet(L, pluginapi.Errf(pluginapi.CodeLimit, "clipboard text too large (max %d bytes)", uiMaxClip))
		}
		r.guard.SetClipboard(s)
		L.Push(lua.LTrue)
		return 1
	})
}
