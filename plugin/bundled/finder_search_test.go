package bundled_test

import (
	"strings"
	"testing"
	"time"

	"github.com/tbxark/gvim/plugin/plugintest"
	"github.com/tbxark/gvim/pluginapi"
)

// The finder library's live search source (opts.search): min_query, one search call per
// debounced query, max_results capping and the on_rows(shown, total) status hook.

const finderSearchProbe = `
local gvim = require "gvim"
local finder = require "gvim.finder"
gvim.command.register{ name = "search", title = "search", run = function()
  local f = finder.new{
    title = "Search probe",
    min_query = 2,
    max_results = 2,
    search = function(q)
      gvim.notify("search:" .. q)
      return {
        { label = "a-" .. q },
        { label = "b-" .. q },
        { label = "c-" .. q },
      }
    end,
    format = function(it) return { label = it.label } end,
    on_rows = function(shown, total) gvim.notify("rows:" .. shown .. "/" .. tostring(total)) end,
    on_close = function(reason) gvim.notify("close:" .. reason) end,
  }
  f:open()
end }
`

func startFinderSearchProbe(t *testing.T) *plugintest.Harness {
	t.Helper()
	h := plugintest.New(t, plugintest.Files{"probe/init.lua": finderSearchProbe}, plugintest.Options{Timeout: 500 * time.Millisecond, EventBudget: 5 * time.Second})
	h.Start()
	return h
}

func searchLastNotice(h *plugintest.Harness) string {
	texts := h.NoticeTexts()
	if len(texts) == 0 {
		return ""
	}
	return texts[len(texts)-1]
}

func TestFinderSearchSource(t *testing.T) {
	t.Parallel()
	h := startFinderSearchProbe(t)
	if err := h.Run("probe.search", nil); err != nil {
		t.Fatal(err)
	}
	o := h.Host.UI().Last("picker")
	if o == nil {
		t.Fatalf("no picker (log %v)", h.LogText(""))
	}
	// The opening query is empty and shorter than min_query: no search runs, no rows.
	if len(o.Picker.Items) != 0 {
		t.Fatalf("opened rows = %+v", o.Picker.Items)
	}
	if got := searchLastNotice(h); strings.HasPrefix(got, "search:") || !strings.HasPrefix(got, "rows:0/0") {
		t.Fatalf("open notice = %q", got)
	}
	// A one-character query is still below min_query.
	h.Resolve(pluginapi.Completion{Token: o.Token, Kind: "query", Values: []any{"g"}, More: true})
	h.Advance(70 * time.Millisecond)
	if len(o.Picker.Items) != 0 {
		t.Fatalf("short-query rows = %+v", o.Picker.Items)
	}
	// A real query calls the source once and shows at most max_results rows.
	h.Resolve(pluginapi.Completion{Token: o.Token, Kind: "query", Values: []any{"go"}, More: true})
	h.Advance(70 * time.Millisecond)
	if last := o.Updates[len(o.Updates)-1]; last.Items == nil || len(*last.Items) != 2 || (*last.Items)[0].Text != "a-go" || (*last.Items)[1].Text != "b-go" {
		t.Fatalf("search rows = %+v", last.Items)
	}
	if got := searchLastNotice(h); got != "rows:2/3" {
		t.Errorf("on_rows notice = %q, want rows:2/3", got)
	}
	// Choosing reports the item selected and the close reason.
	h.Resolve(pluginapi.Completion{Token: o.Token, Values: []any{nil}})
	if got := searchLastNotice(h); got != "close:cancelled" {
		t.Errorf("cancel notice = %q", got)
	}
}
