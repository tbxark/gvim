package plugin_test

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/tbxark/gvim/internal/plugin"
	"github.com/tbxark/gvim/internal/plugin/plugintest"
	"github.com/tbxark/gvim/internal/pluginapi"
)

// uiStart starts a plugin "t" whose main chunk is body (with `gvim` in scope) and whose
// command t.go runs go. It returns the harness with the command not yet run.
func uiStart(t *testing.T, main, goBody string, o ...plugintest.Options) *plugintest.Harness {
	t.Helper()
	return start(t, `
local gvim = require "gvim"
`+main+`
T.command("go", function(ctx)
`+goBody+`
end)`, o...)
}

// answer resolves the wait of an overlay the way the ui does when the user answers.
func answer(h *plugintest.Harness, o *plugintest.FakeOverlay, values ...any) {
	h.Resolve(pluginapi.Completion{Token: o.Token, Values: values})
}

func mustRun(t *testing.T, h *plugintest.Harness) {
	t.Helper()
	if err := h.Run("t.go", nil); err != nil {
		t.Fatalf("Run: %v", err)
	}
}

func TestUIInput(t *testing.T) {
	rows := []struct {
		name   string
		body   string
		answer func(h *plugintest.Harness, o *plugintest.FakeOverlay)
		want   string
		check  func(t *testing.T, o *plugintest.FakeOverlay)
	}{
		{
			name: "text answer, options reach the host",
			body: `T.spy(gvim.ui.input{prompt = "Name:", default = "abc", select_all = false})`,
			answer: func(h *plugintest.Harness, o *plugintest.FakeOverlay) {
				answer(h, o, "hello")
			},
			want: "hello",
			check: func(t *testing.T, o *plugintest.FakeOverlay) {
				if o.Input.Prompt != "Name:" || o.Input.Default != "abc" || o.Input.SelectAll {
					t.Fatalf("spec = %+v", o.Input)
				}
			},
		},
		{
			name: "select_all defaults to true",
			body: `T.spy(gvim.ui.input{prompt = "P"})`,
			answer: func(h *plugintest.Harness, o *plugintest.FakeOverlay) {
				answer(h, o, "")
			},
			want: "",
			check: func(t *testing.T, o *plugintest.FakeOverlay) {
				if !o.Input.SelectAll {
					t.Fatal("select_all should default to true")
				}
			},
		},
		{
			name: "no options at all",
			body: `T.spy(tostring(gvim.ui.input()))`,
			answer: func(h *plugintest.Harness, o *plugintest.FakeOverlay) {
				answer(h, o, nil)
			},
			want: "nil",
		},
		{
			name: "cancelled with Esc is nil",
			body: `local v, e = gvim.ui.input{prompt = "P"} T.spy(tostring(v), tostring(e))`,
			answer: func(h *plugintest.Harness, o *plugintest.FakeOverlay) {
				answer(h, o, nil)
			},
			want: "nil nil",
		},
		{
			name: "cancelled by the panic chord is an error",
			body: `local v, e = gvim.ui.input{prompt = "P"} T.spy(tostring(v), gvim.err_code(e))`,
			answer: func(h *plugintest.Harness, o *plugintest.FakeOverlay) {
				h.Resolve(pluginapi.Completion{Token: o.Token, Err: "cancelled: plugin input cleared"})
			},
			want: "nil cancelled",
		},
	}
	for _, tt := range rows {
		t.Run(tt.name, func(t *testing.T) {
			h := uiStart(t, "", tt.body)
			mustRun(t, h)
			o := h.Host.UI().Last("input")
			if o == nil || !o.Open {
				t.Fatalf("no open prompt; calls %v", h.Host.Calls())
			}
			if tt.check != nil {
				tt.check(t, o)
			}
			if got := spyText(h); got != "" {
				t.Fatalf("the task did not wait: spy %q", got)
			}
			tt.answer(h, o)
			if got := spyText(h); got != tt.want {
				t.Fatalf("spy = %q, want %q; log %v", got, tt.want, h.LogText(""))
			}
		})
	}
}

func TestUIInputBadArguments(t *testing.T) {
	rows := []struct{ name, body, want string }{
		{"options not a table", `gvim.ui.input("x")`, "table expected"},
		{"prompt not a string", `gvim.ui.input{prompt = 1.5}`, "option 'prompt' must be a string"},
		{"validate not a function", `gvim.ui.input{validate = 3}`, "option 'validate' must be a function"},
		{"confirm needs a string", `gvim.ui.confirm(1)`, "string expected"},
		{"choose needs a table", `gvim.ui.choose("m", 1)`, "table expected"},
		{"select needs a table", `gvim.ui.select("x")`, "table expected"},
		{"select bad filter", `gvim.ui.select({"a"}, {filter = "weird"})`, "'filter' must be"},
		{"select bad format", `gvim.ui.select({"a"}, {format = 1})`, "'format' must be a function"},
		{"picker bad on_query", `gvim.ui.picker{on_query = 1}`, "option 'on_query' must be a function"},
		{"popup both lines and items", `gvim.ui.popup{lines = {"a"}, items = {"b"}}`, "'lines' or 'items'"},
		{"popup bad anchor", `gvim.ui.popup{anchor = "left"}`, "option 'anchor'"},
	}
	for _, tt := range rows {
		t.Run(tt.name, func(t *testing.T) {
			h := uiStart(t, "", tt.body)
			mustRun(t, h)
			if !logHas(h, "t", tt.want) {
				t.Fatalf("log %v does not mention %q", h.LogText("t"), tt.want)
			}
			if len(h.Host.UI().Overlays) != 0 {
				t.Fatalf("a bad call reached the host: %+v", h.Host.UI().Overlays)
			}
		})
	}
}

func TestUIInputCallbacks(t *testing.T) {
	h := uiStart(t, "", `
local text = gvim.ui.input{
  prompt = "P",
  validate = function(s) if s == "bad" then return "no way" end end,
  on_change = function(s) T.spy("change " .. s) end,
}
T.spy("done " .. tostring(text))`)
	mustRun(t, h)
	o := h.Host.UI().Last("input")
	spec := o.Input
	if spec.Validate == nil || spec.OnChange == nil {
		t.Fatalf("closures missing: %+v", spec)
	}
	if err := spec.Validate("bad"); err == nil || err.Error() != "no way" {
		t.Fatalf("Validate(bad) = %v", err)
	}
	if err := spec.Validate("fine"); err != nil {
		t.Fatalf("Validate(fine) = %v", err)
	}
	// OnChange only queues a task; the ui drains afterwards.
	spec.OnChange("ab")
	if got := spyText(h); got != "" {
		t.Fatalf("OnChange ran Lua synchronously: %q", got)
	}
	h.Settle()
	answer(h, o, "ab")
	if got := spyText(h); got != "change ab|done ab" {
		t.Fatalf("spy = %q", got)
	}
}

func TestUIInputValidateFailureAcceptsText(t *testing.T) {
	h := uiStart(t, "", `gvim.ui.input{validate = function(s) error("boom") end}`)
	mustRun(t, h)
	spec := h.Host.UI().Last("input").Input
	if err := spec.Validate("x"); err != nil {
		t.Fatalf("a failing validator must not trap the user: %v", err)
	}
	if !logHas(h, "t", "boom") {
		t.Fatalf("the failure was not logged: %v", h.LogText("t"))
	}
}

func TestUIInputValidateMustNotYield(t *testing.T) {
	h := uiStart(t, "", `gvim.ui.input{validate = function(s) T.echo(s) return "never" end}`)
	mustRun(t, h)
	spec := h.Host.UI().Last("input").Input
	if err := spec.Validate("x"); err != nil {
		t.Fatalf("a yielding validator is a failure, not an error message: %v", err)
	}
	if !logHas(h, "t", "must not yield") {
		t.Fatalf("log %v", h.LogText("t"))
	}
}

func TestUIConfirm(t *testing.T) {
	rows := []struct {
		name   string
		body   string
		answer func(h *plugintest.Harness, o *plugintest.FakeOverlay)
		want   string
	}{
		{"yes", `T.spy(gvim.ui.confirm("Sure?", {yes = "Do it", no = "Keep", default = true}))`,
			func(h *plugintest.Harness, o *plugintest.FakeOverlay) { answer(h, o, true) }, "true"},
		{"no", `T.spy(gvim.ui.confirm("Sure?"))`,
			func(h *plugintest.Harness, o *plugintest.FakeOverlay) { answer(h, o, false) }, "false"},
		{"cancelled by the panic chord", `local v, e = gvim.ui.confirm("Sure?") T.spy(tostring(v), gvim.err_code(e))`,
			func(h *plugintest.Harness, o *plugintest.FakeOverlay) {
				h.Resolve(pluginapi.Completion{Token: o.Token, Err: "cancelled: x"})
			}, "nil cancelled"},
	}
	for _, tt := range rows {
		t.Run(tt.name, func(t *testing.T) {
			h := uiStart(t, "", tt.body)
			mustRun(t, h)
			o := h.Host.UI().Last("confirm")
			if o == nil || o.Confirm.Message != "Sure?" {
				t.Fatalf("overlay = %+v", o)
			}
			if tt.name == "yes" && (o.Confirm.Yes != "Do it" || o.Confirm.No != "Keep" || !o.Confirm.Default) {
				t.Fatalf("spec = %+v", o.Confirm)
			}
			tt.answer(h, o)
			if got := spyText(h); got != tt.want {
				t.Fatalf("spy = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestUIChoose(t *testing.T) {
	h := uiStart(t, "", `
local id = gvim.ui.choose("Pick one", { {id = "open", label = "Open"}, {id = 7, label = "Seven", detail = "d"}, "plain" })
T.spy(tostring(id))`)
	mustRun(t, h)
	o := h.Host.UI().Last("picker")
	if o.Picker.Title != "Pick one" || o.Picker.Fuzzy || len(o.Picker.Items) != 3 {
		t.Fatalf("spec = %+v", o.Picker)
	}
	if it := o.Picker.Items[1]; it.Text != "Seven" || it.Detail != "d" || it.ID != 1 {
		t.Fatalf("item = %+v", it)
	}
	answer(h, o, 1)
	if got := spyText(h); got != "7" {
		t.Fatalf("spy = %q", got)
	}
}

func TestUIChooseRows(t *testing.T) {
	rows := []struct {
		name, body string
		answer     any
		want       string
	}{
		{"first", `T.spy(gvim.ui.choose("m", {{id = "a", label = "A"}, {id = "b", label = "B"}}))`, 0, "a"},
		{"string choices answer with themselves", `T.spy(gvim.ui.choose("m", {"x", "y"}))`, 1, "y"},
		{"cancel", `T.spy(tostring(gvim.ui.choose("m", {{id = "a", label = "A"}})))`, nil, "nil"},
		{"out of range answer is a cancel", `T.spy(tostring(gvim.ui.choose("m", {{id = "a", label = "A"}})))`, 9, "nil"},
		{"no choices", `local v, e = gvim.ui.choose("m", {}) T.spy(tostring(v), gvim.err_code(e))`, nil, "nil invalid"},
	}
	for _, tt := range rows {
		t.Run(tt.name, func(t *testing.T) {
			h := uiStart(t, "", tt.body)
			mustRun(t, h)
			if o := h.Host.UI().Last("picker"); o != nil {
				answer(h, o, tt.answer)
			}
			if got := spyText(h); got != tt.want {
				t.Fatalf("spy = %q, want %q; log %v", got, tt.want, h.LogText(""))
			}
		})
	}
}

// TestSelectIndexIsOneBased is binding (AMENDMENTS C24): the host item ID is the 0-based
// Go index, and the second result of gvim.ui.select is the 1-based position in the array.
func TestSelectIndexIsOneBased(t *testing.T) {
	rows := []struct {
		name, body string
		hostID     any
		want       string
		wantTexts  []string
	}{
		{"first item", `local it, i = gvim.ui.select({"a", "b", "c"}) T.spy(it, i)`, 0, "a 1", []string{"a", "b", "c"}},
		{"last item", `local it, i = gvim.ui.select({"a", "b", "c"}) T.spy(it, i)`, 2, "c 3", []string{"a", "b", "c"}},
		{"table item comes back as the same table", `
local items = { {text = "x", n = 10}, {text = "y", n = 20} }
local it, i = gvim.ui.select(items)
T.spy(it == items[2], it.n, i)`, 1, "true 20 2", []string{"x", "y"}},
		{"format maps the display text", `
local it, i = gvim.ui.select({ {name = "p"}, {name = "q"} }, {format = function(x) return "<" .. x.name .. ">" end})
T.spy(it.name, i)`, 1, "q 2", []string{"<p>", "<q>"}},
		{"cancel", `local it, i = gvim.ui.select({"a"}) T.spy(tostring(it), tostring(i))`, nil, "nil nil", []string{"a"}},
	}
	for _, tt := range rows {
		t.Run(tt.name, func(t *testing.T) {
			h := uiStart(t, "", tt.body)
			mustRun(t, h)
			o := h.Host.UI().Last("picker")
			var texts []string
			for i, it := range o.Picker.Items {
				texts = append(texts, it.Text)
				if it.ID != i {
					t.Fatalf("host item %d has ID %d; the host ID is the 0-based index", i, it.ID)
				}
			}
			if !slices.Equal(texts, tt.wantTexts) || !o.Picker.Fuzzy {
				t.Fatalf("texts = %v fuzzy %v", texts, o.Picker.Fuzzy)
			}
			answer(h, o, tt.hostID)
			if got := spyText(h); got != tt.want {
				t.Fatalf("spy = %q, want %q; log %v", got, tt.want, h.LogText(""))
			}
		})
	}
}

func TestUISelectOptions(t *testing.T) {
	h := uiStart(t, "", `gvim.ui.select({{text = "a", detail = "d", keybinding = "ctrl+a", disabled = true}, "b"}, {title = "T", filter = "none", initial_query = "q", placeholder = "ph", footer = "ft"})`)
	mustRun(t, h)
	p := h.Host.UI().Last("picker").Picker
	if p.Title != "T" || p.Fuzzy || p.InitialQuery != "q" || p.Placeholder != "ph" || p.Footer != "ft" {
		t.Fatalf("spec = %+v", p)
	}
	if it := p.Items[0]; it.Detail != "d" || it.Keybinding != "ctrl+a" || !it.Disabled {
		t.Fatalf("item = %+v", it)
	}
}

func TestUIHostRefusal(t *testing.T) {
	h := uiStart(t, "", `
local v, e = gvim.ui.input{}
T.spy(tostring(v), gvim.err_code(e))
local c, ce = gvim.ui.confirm("x")
T.spy(tostring(c), gvim.err_code(ce))
local s, se = gvim.ui.select({"a"})
T.spy(tostring(s), gvim.err_code(se))`)
	h.Host.UI().Reject = pluginapi.Errf(pluginapi.CodeUnsupported, "no ui")
	mustRun(t, h)
	if got := spyText(h); got != "nil unsupported|nil unsupported|nil unsupported" {
		t.Fatalf("spy = %q", got)
	}
	// Nothing is parked: a refused call does not consume the wait.
	if n := len(plugin.ParkedTokens(h.RT)); n != 0 {
		t.Fatalf("%d waits left", n)
	}
}

const pickerMain = `
local picker
picker = gvim.ui.picker{
  title = "Files",
  items = { "alpha", {text = "beta", detail = "d"}, "gamma" },
  on_query = function(q, p)
    T.spy("query " .. q)
    if q == "x" then p:set_items({"from-query"}) end
  end,
  on_move = function(item, p) T.spy("move " .. (type(item) == "table" and item.text or item)) end,
  on_select = function(item, i, p) T.spy("select " .. tostring(i) .. " " .. (type(item) == "table" and item.text or item) .. " " .. tostring(p == picker)) end,
  on_close = function() T.spy("close") end,
  placeholder = "type",
  footer = "foot",
  initial_query = "ini",
}
T.command("open", function() T.spy(tostring(picker:open())) T.spy(tostring(picker:is_open())) end)
T.command("again", function() local ok, e = picker:open() T.spy(tostring(ok), gvim.err_code(e)) end)
T.command("shut", function() T.spy(tostring(picker:close()), tostring(picker:is_open())) end)
T.command("set", function()
  T.spy(tostring(picker:set_items({"one", "two"})))
  picker:set_title("T2") picker:set_footer("F2") picker:set_status("S2")
end)
T.command("state", function() T.spy(picker:query(), tostring(picker:selected())) end)
`

func TestUIPickerLifecycle(t *testing.T) {
	h := start(t, "local gvim = require \"gvim\"\n"+pickerMain)
	if err := h.Run("t.open", nil); err != nil {
		t.Fatal(err)
	}
	o := h.Host.UI().Last("picker")
	if o == nil || !o.Open {
		t.Fatalf("picker not opened: %v", h.Host.Calls())
	}
	ps := o.Picker
	if ps.Title != "Files" || !ps.Fuzzy || !ps.Live || !ps.Preview || ps.InitialQuery != "ini" || ps.Placeholder != "type" || ps.Footer != "foot" {
		t.Fatalf("spec = %+v", ps)
	}
	if len(ps.Items) != 3 || ps.Items[1].Text != "beta" || ps.Items[1].Detail != "d" || ps.Items[2].ID != 2 {
		t.Fatalf("items = %+v", ps.Items)
	}
	// Opening a live picker asks on_query for the first items at once.
	if got := spyText(h); got != "true|true|query ini" {
		t.Fatalf("spy = %q", got)
	}
	plugin.ResetSpy(h.RT)

	// A second open is refused.
	if err := h.Run("t.again", nil); err != nil {
		t.Fatal(err)
	}
	if got := spyText(h); got != "nil busy" {
		t.Fatalf("second open: %q", got)
	}
	plugin.ResetSpy(h.RT)

	// The highlight moves: on_move runs with the item; selected() follows.
	h.Resolve(pluginapi.Completion{Token: o.Token, Kind: "move", Values: []any{1}, More: true})
	h.Resolve(pluginapi.Completion{Token: o.Token, Kind: "move", Values: []any{0}, More: true})
	if got := spyText(h); got != "move beta|move alpha" {
		t.Fatalf("moves: %q", got)
	}
	plugin.ResetSpy(h.RT)
	if err := h.Run("t.state", nil); err != nil {
		t.Fatal(err)
	}
	if got := spyText(h); got != "ini alpha" {
		t.Fatalf("state: %q", got)
	}
	plugin.ResetSpy(h.RT)

	// A query change is debounced, then on_query runs with the text; a newer query wins.
	h.Resolve(pluginapi.Completion{Token: o.Token, Kind: "query", Values: []any{"a"}, More: true})
	h.Resolve(pluginapi.Completion{Token: o.Token, Kind: "query", Values: []any{"x"}, More: true})
	if got := spyText(h); got != "" {
		t.Fatalf("on_query ran before the debounce: %q", got)
	}
	h.Advance(70 * time.Millisecond)
	if got := spyText(h); got != "query x" {
		t.Fatalf("after the debounce: %q", got)
	}
	// on_query called set_items on its own handle: the host got the new items.
	if last := o.Updates[len(o.Updates)-1]; last.Items == nil || len(*last.Items) != 1 || (*last.Items)[0].Text != "from-query" {
		t.Fatalf("updates = %+v", o.Updates)
	}
	plugin.ResetSpy(h.RT)

	// Choosing: the final completion carries the 0-based item id; on_select gets the
	// 1-based index and the owner's handle.
	h.Resolve(pluginapi.Completion{Token: o.Token, Values: []any{0}})
	if got := spyText(h); got != "select 1 from-query true" {
		t.Fatalf("select: %q", got)
	}
}

func TestUIPickerCloseCallbacks(t *testing.T) {
	rows := []struct {
		name  string
		final pluginapi.Completion
		want  string
	}{
		{"esc", pluginapi.Completion{Values: []any{nil}}, "close"},
		{"panic chord", pluginapi.Completion{Err: "cancelled: plugin input cleared"}, "close"},
		{"choice", pluginapi.Completion{Values: []any{2}}, "select 3 gamma true"},
	}
	for _, tt := range rows {
		t.Run(tt.name, func(t *testing.T) {
			h := start(t, "local gvim = require \"gvim\"\n"+pickerMain)
			if err := h.Run("t.open", nil); err != nil {
				t.Fatal(err)
			}
			plugin.ResetSpy(h.RT)
			o := h.Host.UI().Last("picker")
			c := tt.final
			c.Token = o.Token
			h.Resolve(c)
			if got := spyText(h); got != tt.want {
				t.Fatalf("spy = %q, want %q", got, tt.want)
			}
			// The picker can be opened again afterwards.
			plugin.ResetSpy(h.RT)
			if err := h.Run("t.open", nil); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(spyText(h), "true|true") {
				t.Fatalf("reopen: %q", spyText(h))
			}
		})
	}
}

func TestUIPickerCloseAndUpdates(t *testing.T) {
	h := start(t, "local gvim = require \"gvim\"\n"+pickerMain)
	if err := h.Run("t.set", nil); err != nil {
		t.Fatal(err)
	}
	if len(h.Host.UI().Overlays) != 0 {
		t.Fatal("set_items on a closed picker reached the host")
	}
	if err := h.Run("t.open", nil); err != nil {
		t.Fatal(err)
	}
	o := h.Host.UI().Last("picker")
	plugin.ResetSpy(h.RT)
	if err := h.Run("t.set", nil); err != nil {
		t.Fatal(err)
	}
	if len(o.Updates) != 4 {
		t.Fatalf("updates = %+v", o.Updates)
	}
	if o.Picker.Title != "T2" || o.Picker.Footer != "F2" || len(o.Picker.Items) != 2 {
		t.Fatalf("picker = %+v", o.Picker)
	}
	if st := o.Updates[3].Status; st == nil || *st != "S2" {
		t.Fatalf("status update = %+v", o.Updates[3])
	}
	plugin.ResetSpy(h.RT)
	// p:close() closes on the host and runs no callback.
	if err := h.Run("t.shut", nil); err != nil {
		t.Fatal(err)
	}
	if got := spyText(h); got != "true false" || o.Open {
		t.Fatalf("spy = %q open = %v", got, o.Open)
	}
	// A late answer to the closed picker is ignored.
	plugin.ResetSpy(h.RT)
	h.Resolve(pluginapi.Completion{Token: o.Token, Values: []any{0}})
	if got := spyText(h); got != "" {
		t.Fatalf("late answer ran a callback: %q", got)
	}
}

func TestUIPickerStaleQueryItems(t *testing.T) {
	h := start(t, `
local gvim = require "gvim"
local first
local picker = gvim.ui.picker{
  on_query = function(q, p)
    if first == nil then first = p return end
    T.spy("second", tostring(p:set_items({"fresh"})))
    local ok, e = first:set_items({"stale"})
    T.spy("stale call", tostring(ok), gvim.err_code(e))
  end,
}
T.command("open", function() picker:open() end)
`)
	if err := h.Run("t.open", nil); err != nil {
		t.Fatal(err)
	}
	o := h.Host.UI().Last("picker")
	// Opening ran on_query once (first); a new query runs it again (second).
	h.Resolve(pluginapi.Completion{Token: o.Token, Kind: "query", Values: []any{"two"}, More: true})
	h.Advance(70 * time.Millisecond)
	if got := spyText(h); got != "second true|stale call nil stale" {
		t.Fatalf("spy = %q; log %v", got, h.LogText("t"))
	}
	if n := len(o.Updates); n != 1 {
		t.Fatalf("the stale items reached the host: %+v", o.Updates)
	}
}

func TestUIPopup(t *testing.T) {
	h := start(t, `
local gvim = require "gvim"
local lines, choice
T.command("lines", function()
  lines = gvim.ui.popup{title = "Commit", lines = {"a", "b"}, anchor = "center", width = 40, on_close = function() T.spy("closed") end}
  T.spy(tostring(lines:is_open()))
end)
T.command("update", function() T.spy(tostring(lines:set_lines({"c"}))) end)
T.command("choice", function()
  choice = gvim.ui.popup{items = {"x", {text = "y", detail = "dd"}}, on_select = function(it, i, p) T.spy("sel", type(it) == "table" and it.text or it, i, tostring(p == choice)) end,
    on_close = function() T.spy("closed2") end}
end)
T.command("shut", function() T.spy(tostring(lines:close()), tostring(lines:is_open())) end)
`)
	if err := h.Run("t.lines", nil); err != nil {
		t.Fatal(err)
	}
	o := h.Host.UI().Last("popup")
	if o == nil || !o.Open || o.Popup.Title != "Commit" || !slices.Equal(o.Popup.Lines, []string{"a", "b"}) || o.Popup.Anchor != "center" || o.Popup.Width != 40 {
		t.Fatalf("popup = %+v", o)
	}
	if spyText(h) != "true" {
		t.Fatalf("spy %q", spyText(h))
	}
	// set_lines shows the popup again under a new wait.
	if err := h.Run("t.update", nil); err != nil {
		t.Fatal(err)
	}
	o2 := h.Host.UI().Last("popup")
	if o2 == o || o.Open || !o2.Open || o2.ID != o.ID || o2.Token == o.Token || !slices.Equal(o2.Popup.Lines, []string{"c"}) {
		t.Fatalf("reopen: old %+v new %+v", o, o2)
	}
	// Closing it from the ui runs on_close.
	plugin.ResetSpy(h.RT)
	h.Resolve(pluginapi.Completion{Token: o2.Token, Values: []any{nil}})
	if got := spyText(h); got != "closed" {
		t.Fatalf("spy = %q", got)
	}
	// The stale first wait does nothing.
	plugin.ResetSpy(h.RT)
	h.Resolve(pluginapi.Completion{Token: o.Token, Values: []any{nil}})
	if spyText(h) != "" {
		t.Fatalf("old wait delivered: %q", spyText(h))
	}

	// A choice popup reports the row.
	if err := h.Run("t.choice", nil); err != nil {
		t.Fatal(err)
	}
	c := h.Host.UI().Last("popup")
	if len(c.Popup.Items) != 2 || c.Popup.Items[1].Detail != "dd" || c.Popup.Items[1].ID != 1 {
		t.Fatalf("choice popup = %+v", c.Popup)
	}
	h.Resolve(pluginapi.Completion{Token: c.Token, Values: []any{1}})
	if got := spyText(h); got != "sel y 2 true" {
		t.Fatalf("spy = %q", got)
	}
}

func TestUIPopupSetLinesRefusals(t *testing.T) {
	h := start(t, `
local gvim = require "gvim"
T.command("go", function()
  local p = gvim.ui.popup{items = {"a"}}
  local ok, e = p:set_lines({"x"})
  T.spy(tostring(ok), gvim.err_code(e))
  local q = gvim.ui.popup{lines = {"a"}}
  q:close()
  T.spy(tostring(q:set_lines({"y"})), tostring(q:is_open()))
end)`)
	mustRun(t, h)
	if got := spyText(h); got != "nil invalid|true false" {
		t.Fatalf("spy = %q", got)
	}
}

func TestUIOverlayQuota(t *testing.T) {
	h := uiStart(t, "", `
local made = 0
for i = 1, 10 do
  local p = gvim.ui.picker{items = {"a"}}
  local ok, e = p:open()
  if ok then made = made + 1 else T.spy(gvim.err_code(e)) break end
end
local pp, e = gvim.ui.popup{lines = {"x"}}
T.spy(made, tostring(pp), gvim.err_code(e))`)
	mustRun(t, h)
	if got := spyText(h); got != "limit|8 nil limit" {
		t.Fatalf("spy = %q", got)
	}
}

// TestUIOverlaysGoneOnUnload is the leak test of this area: whatever a plugin had open
// when it unloads is closed on the host, and a late answer is ignored.
func TestUIOverlaysGoneOnUnload(t *testing.T) {
	h := plugintest.New(t, nil, plugintest.Options{Timeout: 100 * time.Millisecond})
	h.Start()
	before := h.Host.Resources()
	regBefore := plugin.RegistryOf(h.RT)
	if err := h.Load("t", `
local gvim = require "gvim"
local keep = {}
T.command("open", function()
  keep.picker = gvim.ui.picker{items = {"a", "b"}, on_select = function() T.spy("select") end, on_close = function() T.spy("close") end}
  keep.picker:open()
  keep.popup = gvim.ui.popup{lines = {"hi"}, on_close = function() T.spy("popup close") end}
end)
T.command("input", function() T.spy(tostring(gvim.ui.input{prompt = "P"})) end)
T.command("confirm", function() T.spy(tostring(gvim.ui.confirm("Q"))) end)
T.command("select", function() T.spy(tostring(gvim.ui.select({"a"}))) end)
T.command("choose", function() T.spy(tostring(gvim.ui.choose("c", {"a"}))) end)
`); err != nil {
		t.Fatal(err)
	}
	for _, c := range []string{"t.open", "t.input", "t.confirm", "t.select", "t.choose"} {
		if err := h.Run(c, nil); err != nil {
			t.Fatal(err)
		}
	}
	opened := h.Host.UI().Opened()
	if len(opened) != 6 {
		t.Fatalf("%d overlays open, want 6: %v", len(opened), h.Host.Resources())
	}
	h.Unload("t")
	if got := h.Host.Resources(); !slices.Equal(got, before) {
		t.Fatalf("host resources after unload = %v, before load %v", got, before)
	}
	if got := plugin.RegistryOf(h.RT); got != regBefore {
		t.Fatalf("registry after unload = %+v, before load %+v", got, regBefore)
	}
	// Late answers to the dead plugin's overlays change nothing.
	for _, o := range opened {
		h.Resolve(pluginapi.Completion{Token: o.Token, Values: []any{0}})
	}
	if got := spyText(h); got != "" {
		t.Fatalf("a late answer ran plugin code: %q", got)
	}
}

func TestUITaskCancelClosesPrompt(t *testing.T) {
	h := start(t, `
local gvim = require "gvim"
local task
T.command("go", function()
  task = gvim.async.spawn(function() T.spy(tostring(gvim.ui.input{prompt = "P"})) end)
end)
T.command("stop", function() task:cancel() end)`)
	mustRun(t, h)
	o := h.Host.UI().Last("input")
	if o == nil || !o.Open {
		t.Fatal("prompt not open")
	}
	if err := h.Run("t.stop", nil); err != nil {
		t.Fatal(err)
	}
	if o.Open || !slices.Contains(h.Host.UI().Cancelled, o.Token) {
		t.Fatalf("cancelling the task left the prompt open: open=%v cancelled=%v", o.Open, h.Host.UI().Cancelled)
	}
}

func TestUIClipboardFocusSize(t *testing.T) {
	h := uiStart(t, "", `
T.spy(tostring(gvim.clipboard.set("copied")))
local s = gvim.ui.size()
T.spy(s.width, s.height)
local ok, e = gvim.ui.focus("sidebar")
T.spy(tostring(ok), gvim.err_code(e))
local big = ("x"):rep(9 * 1024 * 1024)
local ok2, e2 = gvim.clipboard.set(big)
T.spy(tostring(ok2), gvim.err_code(e2))`)
	h.Host.Screen = [2]int{100, 30}
	mustRun(t, h)
	if h.Host.Clipboard != "copied" {
		t.Fatalf("clipboard = %q", h.Host.Clipboard)
	}
	if got := spyText(h); got != "true|100 30|nil unsupported|nil limit" {
		t.Fatalf("spy = %q; log %v", got, h.LogText("t"))
	}
}

func TestPickItemsParse(t *testing.T) {
	rows := []struct {
		name, body, want string
	}{
		{"number item", `gvim.ui.select({1, 2.5})`, ""},
		{"bad item type", `gvim.ui.select({true})`, "item 1: string or table expected, got boolean"},
		{"table without text", `gvim.ui.select({{}})`, "item 1: text must be a string"},
		{"bad detail", `gvim.ui.select({{text = "a", detail = 1}})`, "item 1: detail must be a string"},
		{"too many items", `local t = {} for i = 1, 50001 do t[i] = "x" end gvim.ui.select(t)`, "too many items"},
	}
	for _, tt := range rows {
		t.Run(tt.name, func(t *testing.T) {
			h := uiStart(t, "", tt.body)
			mustRun(t, h)
			if tt.want == "" {
				if h.Host.UI().Last("picker") == nil {
					t.Fatalf("no picker; log %v", h.LogText("t"))
				}
				return
			}
			// Bad items are an expected failure: the call returns nil, err and logs nothing.
			if h.Host.UI().Last("picker") != nil {
				t.Fatal("a bad item list reached the host")
			}
		})
	}
}

func TestUIHostErrorsAreStrings(t *testing.T) {
	h := uiStart(t, "", `local it, e = gvim.ui.select({true}) T.spy(tostring(it), tostring(e))`)
	mustRun(t, h)
	if got := spyText(h); !strings.HasPrefix(got, "nil invalid: item 1:") {
		t.Fatalf("spy = %q", got)
	}
}

// TestUIPickerSelectedIsNilWhenNothingIsHighlighted checks S13-4: the host reports a move to
// no item (the filter matched nothing) and selected() stops returning the old item. on_move
// does not run for it.
func TestUIPickerSelectedIsNilWhenNothingIsHighlighted(t *testing.T) {
	h := start(t, "local gvim = require \"gvim\"\n"+pickerMain)
	if err := h.Run("t.open", nil); err != nil {
		t.Fatal(err)
	}
	o := h.Host.UI().Last("picker")
	plugin.ResetSpy(h.RT)
	h.Resolve(pluginapi.Completion{Token: o.Token, Kind: "move", Values: []any{1}, More: true})
	h.Resolve(pluginapi.Completion{Token: o.Token, Kind: "move", Values: []any{-1}, More: true})
	if got := spyText(h); got != "move beta" {
		t.Fatalf("on_move ran for no item: %q", got)
	}
	plugin.ResetSpy(h.RT)
	if err := h.Run("t.state", nil); err != nil {
		t.Fatal(err)
	}
	if got := spyText(h); got != "ini nil" {
		t.Fatalf("state = %q, want no selection", got)
	}
}
