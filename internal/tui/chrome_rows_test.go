package tui

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"

	"github.com/tbxark/gvim/internal/tui/menubar"
	"github.com/tbxark/gvim/internal/tui/prompt"
	"github.com/tbxark/gvim/internal/tui/style"
)

// TestMenuBandPaddingRowTakesTheMenuStyle checks the menu band above the classic frame.
func TestMenuBandPaddingRowTakesTheMenuStyle(t *testing.T) {
	m := newTestModel(t, newFakeEditor("hello"), newFakeTree(3), 80, 24)
	m.styles.Menu = lipgloss.NewStyle().Background(lipgloss.Color("#123456"))
	m.menu = menubar.New(m, m.styles)
	l := m.layout()
	if l.MenuTop != 0 || l.MenuBottom != 0 || l.MenuLabelY != 0 || l.StripY != 2 || l.BodyTop != 3 {
		t.Fatalf("menu band %d..%d label %d strip %d body %d, want 0..0, 0, 2 and 3",
			l.MenuTop, l.MenuBottom, l.MenuLabelY, l.StripY, l.BodyTop)
	}
	g := m.paint()
	want := style.TaggedCell(m.styles.Menu, "ui", "menu_fg", "ui", "menu_bg")
	for x := range g.Width() {
		if got := g.At(x, l.MenuBottom).Style; got.BG != want.BG {
			t.Fatalf("menu cell (%d, %d) bg %+v, want %+v", x, l.MenuBottom, got.BG, want.BG)
		}
	}
	if got := m.menu.SetSize(m.width, m.height).SetRow(l.MenuLabelY).Hit(1, l.MenuBottom); got.Kind != menubar.TargetLabel {
		t.Errorf("menu row hits %v, want a menu label", got.Kind)
	}
	for _, label := range []string{"File", "Edit", "View", "Help"} {
		if !strings.Contains(g.Plain()[l.MenuLabelY], label) {
			t.Errorf("label row %q has no %s label", g.Plain()[l.MenuLabelY], label)
		}
	}
}

// TestOpenPromptDoesNotChangeTheLayout checks the prompts are centred cards: opening
// one reserves no row, the status bar stays the last row of the frame, and the card
// box floats over the middle of the screen.
func TestOpenPromptDoesNotChangeTheLayout(t *testing.T) {
	m, _ := viewModel(t, DefaultViewOptions(), "hello\n", 60, 12)
	const last = 11
	idle := m.layout()
	if idle.StatusY != last {
		t.Fatalf("idle layout: status %d, want %d", idle.StatusY, last)
	}
	m.pushOverlay(prompt.NewLine(m.styles, prompt.LineOptions{Label: "Go to line:"}))
	l := m.layout()
	if l.StatusY != idle.StatusY || l.BodyBottom != idle.BodyBottom {
		t.Fatalf("prompt open changed the layout: %+v, was %+v", l, idle)
	}
	if row := viewRow(m, last); !strings.Contains(row, "Ln 1") {
		t.Fatalf("an open prompt moved the status bar off the last row: %q", row)
	}
	if !frameHas(m, "Go to line:") {
		t.Fatal("the prompt card is not painted")
	}
}

// TestPromptCardIsCentred checks a confirm's card paints in the middle of the frame,
// not on its last row.
func TestPromptCardIsCentred(t *testing.T) {
	m, _ := viewModel(t, DefaultViewOptions(), "hello\n", 60, 12)
	m.pushOverlay(prompt.NewConfirmWith(m.styles, "Delete file?", nil, prompt.ConfirmOptions{}))
	last := viewRow(m, 11)
	if strings.Contains(last, "Delete file?") {
		t.Fatalf("the confirm painted on the last row: %q", last)
	}
	if !frameHas(m, "Delete file?") || !frameHas(m, "Yes") {
		t.Fatal("the confirm card is not painted")
	}
}
