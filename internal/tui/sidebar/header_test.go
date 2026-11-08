package sidebar

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/tbxark/gvim/internal/tui/explorer"
	"github.com/tbxark/gvim/internal/tui/icons"
)

func TestHeaderExplorerAloneKeepsTheOldCells(t *testing.T) {
	t.Parallel()
	for width := 0; width <= 60; width++ {
		want := explorer.HeaderFor(width, icons.Plain)
		got := HeaderFor(width, icons.Plain, HeaderSpec{Kind: KindExplorer, First: true})
		if got.Text != want.Text || got.Reload != want.Reload || got.Close != want.Close {
			t.Errorf("width %d: %+v, want the explorer header %+v", width, got, want)
		}
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
