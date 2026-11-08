package tui

import (
	"strings"
	"testing"

	"github.com/tbxark/gvim/internal/tui/grid"
	"github.com/tbxark/gvim/pluginapi"
)

func TestCleanText(t *testing.T) {
	t.Parallel()
	tests := []struct{ name, in, want string }{
		{"plain text is untouched", "héllo wörld", "héllo wörld"},
		{"an escape sequence", "a\x1b[31mred", "a [31mred"},
		{"newline and tab", "a\nb\tc", "a b c"},
		{"carriage return and bell", "a\rb\ac", "a b c"},
		{"delete and C1", "a\x7fb\u009bc", "a b c"},
		{"wide text is kept", "日本語", "日本語"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := cleanText(tt.in); got != tt.want {
				t.Fatalf("cleanText(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// TestPluginOverlaysNeverWriteControlCharacters checks that text a plugin supplied cannot
// put an escape sequence into a cell, whichever overlay shows it.
func TestPluginOverlaysNeverWriteControlCharacters(t *testing.T) {
	t.Parallel()
	evil := "x\x1b]0;owned\x07y\nz"
	rows := []struct {
		name string
		open func(h pluginapi.Host)
	}{
		{"prompt", func(h pluginapi.Host) { _ = h.Input(1, pluginapi.InputSpec{Prompt: evil, Default: evil}) }},
		{"confirm", func(h pluginapi.Host) { _ = h.Confirm(1, pluginapi.ConfirmSpec{Message: evil, Yes: evil, No: evil}) }},
		{"picker", func(h pluginapi.Host) {
			_ = h.OpenPicker(1, 3, pluginapi.PickerSpec{
				Title: evil, Footer: evil, Placeholder: evil, InitialQuery: evil, Fuzzy: true,
				Items: []pluginapi.PickItem{{ID: 1, Text: evil, Detail: evil, Keybinding: evil}},
			})
		}},
		{"popup", func(h pluginapi.Host) {
			_ = h.ShowPopup(1, 3, pluginapi.PopupSpec{Title: evil, Lines: []string{evil}})
		}},
		{"choice popup", func(h pluginapi.Host) {
			_ = h.ShowPopup(1, 3, pluginapi.PopupSpec{Title: evil, Items: []pluginapi.PickItem{{ID: 1, Text: evil, Detail: evil}}})
		}},
	}
	for _, tt := range rows {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			m, rt := newPromptModel(t)
			openVia(t, m, rt, tt.open)
			if p := m.plugPicker(3); p != nil {
				title, status := evil, evil
				if err := m.plug.host.UpdatePicker(3, pluginapi.PickerUpdate{Title: &title, Status: &status, Footer: &title}); err != nil {
					t.Fatal(err)
				}
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
			if len(m.overlays) != 1 {
				t.Fatalf("%d overlays", len(m.overlays))
			}
		})
	}
}

// TestPluginPickerDropsStaleRankingsThroughTheRoot checks the generation rule end to end:
// the rank result of an older query reaches the root last and does not replace the rows.
func TestPluginPickerDropsStaleRankingsThroughTheRoot(t *testing.T) {
	t.Parallel()
	m, rt := newPromptModel(t)
	openVia(t, m, rt, func(h pluginapi.Host) {
		_ = h.OpenPicker(5, 42, pluginapi.PickerSpec{Items: testItems(), Fuzzy: true})
	})
	_, older := m.Update(typed("p")) // ranking for "p"
	_, newer := m.Update(typed("b")) // ranking for "pb"
	feed(m, newer)
	feed(m, older)
	if got := m.plugPicker(42).p.RowIDs(); len(got) != 1 || got[0] != 13 {
		t.Fatalf("rows = %v, want only alphabet: the slow result of the older query replaced the newer rows", got)
	}
}

// TestPluginRankingReachesAPickerBelowAnOverlay checks that a ranking result is routed by
// the picker's id, not by the stack: a prompt on top of the picker does not lose it.
func TestPluginRankingReachesAPickerBelowAnOverlay(t *testing.T) {
	t.Parallel()
	m, rt := newPromptModel(t)
	openVia(t, m, rt, func(h pluginapi.Host) {
		_ = h.OpenPicker(5, 42, pluginapi.PickerSpec{Items: testItems(), Fuzzy: true})
	})
	_, rank := m.Update(typed("b"))
	openVia(t, m, rt, func(h pluginapi.Host) { _ = h.Confirm(6, pluginapi.ConfirmSpec{Message: "Sure?"}) })
	if _, ok := m.overlays[len(m.overlays)-1].(*pluginConfirmOverlay); !ok {
		t.Fatalf("top overlay is %T", m.overlays[len(m.overlays)-1])
	}
	feed(m, rank)
	if got := m.plugPicker(42).p.RowIDs(); len(got) != 2 {
		t.Fatalf("rows = %v, the ranking was lost", got)
	}
}

func TestPluginInputShowsTheCaret(t *testing.T) {
	t.Parallel()
	m, rt := newPromptModel(t)
	openVia(t, m, rt, func(h pluginapi.Host) { _ = h.Input(1, pluginapi.InputSpec{Prompt: "Name:", Default: "ab"}) })
	x, y, ok := m.caret()
	if !ok || y != m.height-1 || x != 1+len("Name: ab") {
		t.Fatalf("caret = %d,%d,%v, want the end of the text on the bottom row", x, y, ok)
	}
	typeInto(m, "c")
	if x, _, _ := m.caret(); x != 1+len("Name: abc") {
		t.Fatalf("caret after typing at column %d", x)
	}
}
