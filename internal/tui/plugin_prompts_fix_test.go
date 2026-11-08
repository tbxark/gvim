package tui

import (
	"errors"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/internal/tui/grid"
	"github.com/tbxark/gvim/internal/tui/picker"
	"github.com/tbxark/gvim/pluginapi"
)

// moveItems are the "move" items the runtime received for the wait of token tok.
func moveItems(rt *fakeRuntime, tok pluginapi.Token) []any {
	var out []any
	for _, c := range rt.resolved {
		if c.Token == tok && c.Kind == "move" {
			out = append(out, c.Values[0])
		}
	}
	return out
}

// TestPluginPickerUpdateBeforeTheOpenIsPumped checks S13-1: `p:open(); p:set_items(...)` in
// one slice reaches the host before the pump has opened the overlay. The update must not be
// lost and must not fail.
func TestPluginPickerUpdateBeforeTheOpenIsPumped(t *testing.T) {
	t.Parallel()
	m, rt := newPromptModel(t)
	title, footer, status := "New", "Foot", "loading"
	items := []pluginapi.PickItem{{ID: 20, Text: "one"}, {ID: 13, Text: "two"}}
	sel := 13
	openVia(t, m, rt, func(h pluginapi.Host) {
		if err := h.OpenPicker(5, 42, pluginapi.PickerSpec{Title: "T", Items: testItems(), Preview: true}); err != nil {
			t.Error(err)
		}
		u := pluginapi.PickerUpdate{Title: &title, Footer: &footer, Status: &status, Items: &items, Selected: &sel}
		if err := h.UpdatePicker(42, u); err != nil {
			t.Errorf("UpdatePicker before the open was pumped: %v", err)
		}
		if err := h.OpenPicker(6, 42, pluginapi.PickerSpec{}); pluginapi.CodeOf(err) != pluginapi.CodeInvalid {
			t.Errorf("a second open of a pending picker = %v, want invalid", err)
		}
		tooMany := make([]pluginapi.PickItem, picker.MaxItems+1)
		if err := h.UpdatePicker(42, pluginapi.PickerUpdate{Items: &tooMany}); pluginapi.CodeOf(err) != pluginapi.CodeLimit {
			t.Errorf("too many items for a pending picker = %v, want limit", err)
		}
	})
	ov := m.plugPicker(42)
	if ov == nil {
		t.Fatal("the picker did not open")
	}
	if got := ov.p.RowIDs(); !slices.Equal(got, []int{20, 13}) {
		t.Fatalf("rows = %v, want the updated items", got)
	}
	if id, _ := ov.p.SelectedID(); id != 13 {
		t.Fatalf("selected = %d, want 13", id)
	}
	g := grid.New(m.width, m.height)
	for _, o := range m.overlays {
		o.Draw(g)
	}
	rows := strings.Join(g.Plain(), "\n")
	for _, want := range []string{"New", "Foot", "loading", "two"} {
		if !strings.Contains(rows, want) {
			t.Errorf("the frame does not show %q:\n%s", want, rows)
		}
	}
	if strings.Contains(rows, "alpha") {
		t.Error("the frame still shows the item of the first spec")
	}
	if got := moveItems(rt, 5); !slices.Equal(got, []any{20, 13}) {
		t.Fatalf("moves = %v, want the updated first row, then the selection", got)
	}
}

// TestPluginPickerClosedBeforeTheOpenIsPumped checks that closing or cancelling a picker
// whose opening is still queued leaves no overlay and no pending entry, and that the id can
// be used again.
func TestPluginPickerClosedBeforeTheOpenIsPumped(t *testing.T) {
	t.Parallel()
	rows := []struct {
		name  string
		close func(h pluginapi.Host)
	}{
		{"ClosePicker", func(h pluginapi.Host) { h.ClosePicker(42) }},
		{"CancelWait", func(h pluginapi.Host) { h.CancelWait(5) }},
	}
	for _, tt := range rows {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			m, rt := newPromptModel(t)
			openVia(t, m, rt, func(h pluginapi.Host) {
				_ = h.OpenPicker(5, 42, pluginapi.PickerSpec{Items: testItems()})
				tt.close(h)
				if err := h.UpdatePicker(42, pluginapi.PickerUpdate{}); pluginapi.CodeOf(err) != pluginapi.CodeClosed {
					t.Errorf("UpdatePicker after the close = %v, want closed", err)
				}
			})
			if len(m.overlays) != 0 || len(m.plug.waits) != 0 || len(m.plug.ui.opening) != 0 {
				t.Fatalf("overlays %d waits %v opening %v", len(m.overlays), m.plug.waits, m.plug.ui.opening)
			}
			openVia(t, m, rt, func(h pluginapi.Host) {
				if err := h.OpenPicker(6, 42, pluginapi.PickerSpec{Items: testItems()}); err != nil {
					t.Error(err)
				}
			})
			if m.plugPicker(42) == nil {
				t.Fatal("the id could not be opened again")
			}
		})
	}
}

// TestPluginValidateErrorIsCleaned checks S13-2: the text a validate callback returns is
// shown in the prompt, so it must not carry control characters into a cell.
func TestPluginValidateErrorIsCleaned(t *testing.T) {
	t.Parallel()
	m, rt := newPromptModel(t)
	openVia(t, m, rt, func(h pluginapi.Host) {
		_ = h.Input(7, pluginapi.InputSpec{Prompt: "Name:", Validate: func(string) error {
			return errors.New("bad\x1b]0;owned\x07!")
		}})
	})
	typeInto(m, "a")
	feed(m, runKey(m, special(tea.KeyEnter)))
	if len(m.overlays) != 1 {
		t.Fatalf("%d overlays: the rejected text closed the prompt", len(m.overlays))
	}
	g := grid.New(m.width, m.height)
	for _, o := range m.overlays {
		o.Draw(g)
	}
	for y := range m.height {
		for x := range m.width {
			if text := g.At(x, y).Text; strings.ContainsFunc(text, func(r rune) bool { return r < 0x20 || r == 0x7f }) {
				t.Fatalf("cell (%d,%d) holds %q", x, y, text)
			}
		}
	}
	if row := frameRow(m, m.height-1); !strings.Contains(row, "bad") {
		t.Fatalf("prompt row = %q, want the rejection text", row)
	}
}

// clipRecorder is a clipboard that records what is written to it.
type clipRecorder struct{ writes *[]string }

func (clipRecorder) Read() (string, error)  { return "", nil }
func (c clipRecorder) Write(s string) error { *c.writes = append(*c.writes, s); return nil }

// TestPluginClipboardSetIsCoalesced checks S13-3: any number of gvim.clipboard.set calls in a
// slice make one clipboard command, carrying the last text.
func TestPluginClipboardSetIsCoalesced(t *testing.T) {
	t.Parallel()
	m, rt := newPromptModel(t)
	var writes []string
	m.dock.clipboard = clipRecorder{writes: &writes}
	openVia(t, m, rt, func(h pluginapi.Host) {
		for i := range 1000 {
			h.SetClipboard(strings.Repeat("x", i+1))
		}
		if m.pending != nil {
			t.Error("SetClipboard collected a command per call")
		}
	})
	if len(writes) != 1 || len(writes[0]) != 1000 {
		t.Fatalf("%d clipboard writes %v, want one with the last text", len(writes), len(writes))
	}
	// A later slice writes again.
	openVia(t, m, rt, func(h pluginapi.Host) { h.SetClipboard("again") })
	if !slices.Equal(writes[1:], []string{"again"}) {
		t.Fatalf("writes after the second slice = %q", writes[1:])
	}
}

// TestPluginValidateThatRunsAnActionKeepsTheStack checks S13-5: an action a validator runs
// (goto_line opens a prompt) must not be run while routeOverlay owns the overlay stack, or the
// prompt it pushed would be overwritten by the write-back.
func TestPluginValidateThatRunsAnActionKeepsTheStack(t *testing.T) {
	t.Parallel()
	for _, text := range []string{"no", "yes"} {
		t.Run(text, func(t *testing.T) {
			t.Parallel()
			m, rt := newPromptModel(t)
			openVia(t, m, rt, func(h pluginapi.Host) {
				_ = h.Input(7, pluginapi.InputSpec{Prompt: "Name:", Validate: func(s string) error {
					_ = h.Do("goto_line", nil, 1)
					if s == "no" {
						return errors.New("refused")
					}
					return nil
				}})
			})
			typeInto(m, text)
			feed(m, runKey(m, special(tea.KeyEnter)))
			var kinds []string
			for _, o := range m.overlays {
				if po, ok := o.(*pluginLineOverlay); ok {
					kinds = append(kinds, map[bool]string{true: "input(closed)", false: "input"}[po.closed])
				} else {
					kinds = append(kinds, "other")
				}
			}
			want := []string{"input", "other"}
			if text == "yes" {
				want = []string{"other"} // the answer is given; the action's prompt stays
			}
			if !slices.Equal(kinds, want) {
				t.Fatalf("overlays = %v, want %v", kinds, want)
			}
			if text == "yes" {
				if got := lastResolved(t, rt); got.Values[0] != "yes" {
					t.Fatalf("resolved %+v", got)
				}
			}
		})
	}
}

// TestPluginPickerWithNoRowsReportsNoSelection checks S13-4 end to end: filtering a picker
// down to nothing tells the runtime that nothing is highlighted.
func TestPluginPickerWithNoRowsReportsNoSelection(t *testing.T) {
	t.Parallel()
	m, rt := newPromptModel(t)
	openVia(t, m, rt, func(h pluginapi.Host) {
		_ = h.OpenPicker(5, 42, pluginapi.PickerSpec{Items: testItems(), Fuzzy: true, Preview: true})
	})
	typeInto(m, "qqq")
	if got := moveItems(rt, 5); !slices.Equal(got, []any{10, picker.NoItem}) {
		t.Fatalf("moves = %v, want the first row, then no item", got)
	}
}
