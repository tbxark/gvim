package sidebar

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/tbxark/gvim/internal/tui/explorer"
	"github.com/tbxark/gvim/internal/tui/grid"
	"github.com/tbxark/gvim/internal/tui/icons"
	"github.com/tbxark/gvim/internal/tui/style"
)

func TestHeaderExplorerAloneKeepsTheOldCells(t *testing.T) {
	t.Parallel()
	for width := 0; width <= 60; width++ {
		want := explorer.HeaderFor(width, icons.Plain)
		got := HeaderFor(width, icons.Plain, HeaderSpec{Kind: KindExplorer, First: true})
		if got.Text != want.Text || got.Reload != want.Reload || got.Close != want.Close ||
			got.TitleX != want.TitleX || got.TitleW != want.TitleW {
			t.Errorf("width %d: %+v, want the explorer header %+v", width, got, want)
		}
	}
}

// TestHeaderTitleStaysInsideTheRow sweeps every header width: the bold title range
// never leaves the row or the text, and the corner stays regular.
func TestHeaderTitleStaysInsideTheRow(t *testing.T) {
	t.Parallel()
	for width := 1; width <= 60; width++ {
		h := HeaderFor(width, icons.Plain, HeaderSpec{Kind: KindExplorer, First: true, Several: true})
		if h.TitleW < 0 || (h.TitleW > 0 && h.TitleX+h.TitleW > width) {
			t.Fatalf("width %d: title %d..%d leaves the row", width, h.TitleX, h.TitleX+h.TitleW)
		}
		if got := ansi.StringWidth(h.Text); got != width {
			t.Fatalf("width %d: text is %d cells", width, got)
		}
		g := grid.New(width, 1)
		PaintHeader(g, 0, width, 0, h, style.Styles{})
		if width > 1 && g.At(0, 0).Style.Bold {
			t.Errorf("width %d: the corner is bold", width)
		}
		if h.TitleW > 0 && !g.At(h.TitleX, 0).Style.Bold {
			t.Errorf("width %d: the title's first cell is not bold", width)
		}
	}
}

// TestHeaderTitleTakesTheBoldWeight checks the row's type hierarchy: the title
// cells are bold while the corner, the rule and the buttons stay regular, and the
// title range moves right of the chevron.
func TestHeaderTitleTakesTheBoldWeight(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name   string
		spec   HeaderSpec
		titleX int
	}{
		{"the explorer alone", HeaderSpec{Kind: KindExplorer, First: true}, 2},
		{"the explorer with a chevron", HeaderSpec{Kind: KindExplorer, First: true, Several: true}, 4},
		{"a contents section", HeaderSpec{Kind: KindContents, Several: true}, 4},
	} {
		t.Run(tt.name, func(t *testing.T) {
			h := HeaderFor(32, icons.Plain, tt.spec)
			if h.TitleX != tt.titleX || h.TitleW == 0 {
				t.Fatalf("title at %d for %d cells, want it at %d", h.TitleX, h.TitleW, tt.titleX)
			}
			g := grid.New(32, 1)
			PaintHeader(g, 0, 32, 0, h, style.Styles{})
			if !g.At(h.TitleX, 0).Style.Bold {
				t.Errorf("title cell is not bold: %+v", g.At(h.TitleX, 0).Style)
			}
			if !g.At(h.TitleX+h.TitleW-1, 0).Style.Bold {
				t.Errorf("last title cell is not bold")
			}
			if g.At(0, 0).Style.Bold {
				t.Error("the corner is bold")
			}
			if h.TitleX > 2 && g.At(2, 0).Style.Bold {
				t.Error("the chevron is bold")
			}
		})
	}
	// A row too narrow for a title has no title range, and its corner stays regular.
	narrow := HeaderFor(4, icons.Plain, HeaderSpec{Kind: KindExplorer, First: true})
	if narrow.TitleW != 2 || narrow.TitleX != 2 {
		t.Fatalf("narrow title range = %d/%d, want the two visible title cells", narrow.TitleX, narrow.TitleW)
	}
	if empty := HeaderFor(1, icons.Plain, HeaderSpec{Kind: KindExplorer, First: true}); empty.TitleW != 0 {
		t.Errorf("a one-cell header has a %d-cell title", empty.TitleW)
	}
}

func TestHeaderContents(t *testing.T) {
	t.Parallel()
	tests := []struct {
		width   int
		text    string
		closeAt int
	}{
		{10, "├ ▼ Conten", -1},
		{13, "├ ▼ Contents ", -1},
		{14, "├ ▼ Contents ×", 13},
		{15, "├ ▼ Contents ×┤", 13},
		{16, "├ ▼ Contents ×─┤", 13},
		{32, "├ ▼ Contents " + "────────────────" + "×─┤", 29},
	}
	for _, tt := range tests {
		h := HeaderFor(tt.width, icons.Plain, HeaderSpec{Kind: KindContents, Several: true})
		if h.Text != tt.text {
			t.Errorf("width %d: text %q, want %q", tt.width, h.Text, tt.text)
		}
		if got := ansi.StringWidth(h.Text); got != tt.width {
			t.Errorf("width %d: text is %d cells", tt.width, got)
		}
		if h.Reload != -1 {
			t.Errorf("width %d: the contents header has a refresh button", tt.width)
		}
		if h.Close != tt.closeAt {
			t.Errorf("width %d: close at %d, want %d", tt.width, h.Close, tt.closeAt)
		}
	}
}

func TestHeaderExplorerChevron(t *testing.T) {
	t.Parallel()
	h := HeaderFor(32, icons.Plain, HeaderSpec{Kind: KindExplorer, First: true, Several: true})
	if want := "┌ ▼ File Explorer "; len(h.Text) < len(want) || h.Text[:len(want)] != want {
		t.Fatalf("header = %q, want it to open with %q", h.Text, want)
	}
	if got := ansi.StringWidth(h.Text); got != 32 {
		t.Fatalf("header is %d cells, want 32", got)
	}
	if h.Reload != 27 || h.Close != 29 {
		t.Fatalf("buttons at %d and %d, want 27 and 29", h.Reload, h.Close)
	}
	collapsed := HeaderFor(32, icons.Plain, HeaderSpec{Kind: KindExplorer, First: true, Several: true, Collapsed: true})
	if collapsed.Text != "┌ ▶ File Explorer ─────────⟳─×─┐" {
		t.Fatalf("collapsed header = %q", collapsed.Text)
	}
}

func TestHeaderSharedRowUsesTheNotchCorners(t *testing.T) {
	t.Parallel()
	got := HeaderFor(20, icons.Plain, HeaderSpec{Kind: KindContents, Several: true})
	if !strings.HasPrefix(got.Text, "├") || !strings.HasSuffix(got.Text, "┤") {
		t.Fatalf("shared header = %q, want ├ .. ┤", got.Text)
	}
	top := HeaderFor(20, icons.Plain, HeaderSpec{Kind: KindExplorer, First: true})
	if !strings.HasPrefix(top.Text, "┌") || !strings.HasSuffix(top.Text, "┐") {
		t.Fatalf("top header = %q, want ┌ .. ┐", top.Text)
	}
}
