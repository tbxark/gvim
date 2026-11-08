package plugin_test

import (
	"slices"
	"strconv"
	"strings"
	"testing"
)

// gvim.ui.input takes the earlier answers as a list: oldest first, at most the last 100, strings only.
func TestUIInputHistory(t *testing.T) {
	t.Parallel()
	h := uiStart(t, "", `
local many = {}
for i = 1, 150 do many[i] = "h" .. i end
many[#many + 1] = 7
gvim.ui.input{prompt = ":", history = {"s/a/b/", "w", 3, "q"}}
gvim.ui.input{prompt = "/", history = many}`)
	mustRun(t, h)
	var got [][]string
	for _, o := range h.Host.UI().Overlays {
		if o.Kind == "input" {
			got = append(got, o.Input.History)
		}
	}
	if len(got) != 1 {
		// the second prompt waits for the first
		t.Fatalf("overlays: %d", len(got))
	}
	if want := []string{"s/a/b/", "w", "q"}; !slices.Equal(got[0], want) {
		t.Fatalf("history = %q, want %q", got[0], want)
	}
	answer(h, h.Host.UI().Last("input"), "x")
	h.Settle()
	o := h.Host.UI().Last("input")
	if o == nil || !o.Open {
		t.Fatal("the second prompt did not open")
	}
	hist := o.Input.History
	if len(hist) != 99 || hist[0] != "h"+strconv.Itoa(52) || hist[98] != "h150" {
		t.Fatalf("a long history is cut to the last 100 items (the 7 is not a string): %d entries, first %q last %q", len(hist), hist[0], hist[len(hist)-1])
	}
}

func TestUIInputHistoryMustBeATable(t *testing.T) {
	t.Parallel()
	h := uiStart(t, "", `gvim.ui.input{history = "w"}`)
	mustRun(t, h)
	if !logHas(h, "t", "option 'history'") || !strings.Contains(strings.Join(h.LogText("t"), "\n"), "table of strings") {
		t.Fatalf("log %v", h.LogText("t"))
	}
	if len(h.Host.UI().Overlays) != 0 {
		t.Fatal("a bad call reached the host")
	}
}
