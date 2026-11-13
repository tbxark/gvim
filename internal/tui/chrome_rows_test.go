package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/tbxark/gvim/internal/tui/grid"
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

// TestPromptRowIsReservedOnlyWhileAPromptIsOpen checks the status bar spacing fix:
// with the prompt line setting on but no prompt open, the status bar is the last row
// of the frame; an open prompt moves the status row up and takes the last row.
func TestPromptRowIsReservedOnlyWhileAPromptIsOpen(t *testing.T) {
	o := DefaultViewOptions()
	o.ShowPromptLine = true
	m, _ := viewModel(t, o, "hello\n", 60, 12)
	const last = 11
	if l := m.layout(); l.PromptOn || l.StatusY != last || l.BodyBottom != last-2 {
		t.Fatalf("idle with the setting on: promptOn %v status %d body bottom %d, want off, %d and %d",
			l.PromptOn, l.StatusY, l.BodyBottom, last, last-2)
	}
	if row := viewRow(m, last); !strings.Contains(row, "Ln 1") {
		t.Fatalf("the idle status bar is not on the last row: %q", row)
	}
	m.pushOverlay(prompt.NewLine(m.styles, prompt.LineOptions{Label: "Go to line:"}))
	l := m.layout()
	if !l.PromptOn || l.PromptY != last || l.StatusY != last-1 || l.BodyBottom != last-3 {
		t.Fatalf("prompt open: prompt %d on=%v status %d body bottom %d; want %d on, %d and %d",
			l.PromptY, l.PromptOn, l.StatusY, l.BodyBottom, last, last-1, last-3)
	}
	if row := viewRow(m, last-1); !strings.Contains(row, "Ln 1") {
		t.Errorf("the status bar did not move up: %q", viewRow(m, last-1))
	}
	promptRow := viewRow(m, last)
	if !strings.Contains(promptRow, "Go to line:") || strings.Contains(promptRow, "Ln 1") {
		t.Errorf("the prompt row %q does not replace the status bar", promptRow)
	}
}

// TestConfirmAlsoGetsThePromptRow checks that a confirm reserves the row like the
// one-line prompt does.
func TestConfirmAlsoGetsThePromptRow(t *testing.T) {
	o := DefaultViewOptions()
	o.ShowPromptLine = true
	m, _ := viewModel(t, o, "hello\n", 60, 12)
	m.pushOverlay(prompt.NewConfirm(m.styles, "Delete file?", nil))
	if l := m.layout(); !l.PromptOn || l.PromptY != 11 || l.StatusY != 10 {
		t.Fatalf("confirm open: prompt %d on=%v status %d, want 11 on and 10",
			l.PromptY, l.PromptOn, l.StatusY)
	}
}

// TestPromptWithoutThePromptLineCoversTheStatusRow checks the setting off: no row
// is reserved and the prompt paints over the status bar.
func TestPromptWithoutThePromptLineCoversTheStatusRow(t *testing.T) {
	m, _ := viewModel(t, DefaultViewOptions(), "hello\n", 60, 12)
	m.pushOverlay(prompt.NewLine(m.styles, prompt.LineOptions{Label: "Go to line:"}))
	l := m.layout()
	if l.PromptOn || l.StatusY != 11 {
		t.Fatalf("setting off: promptOn %v status %d, want off and 11", l.PromptOn, l.StatusY)
	}
	row := viewRow(m, 11)
	if !strings.Contains(row, "Go to line:") || strings.Contains(row, "Ln 1") {
		t.Errorf("the prompt does not cover the status row: %q", row)
	}
}

// fakePromptRow is a prompt-shaped overlay from another package: it only proves that
// the row reservation is a marker, so a plugin prompt gets the row too.
type fakePromptRow struct{ active bool }

func (f *fakePromptRow) Update(tea.Msg) (Overlay, tea.Cmd) { return f, nil }
func (f *fakePromptRow) Draw(*grid.Grid)                   {}
func (f *fakePromptRow) Active() bool                      { return f.active }
func (f *fakePromptRow) UsesPromptRow()                    {}

// TestAnyPromptRowOwnerReservesTheRow checks the reservation is a marker interface,
// not a list of built-in types: a prompt from another package (the plugin prompts)
// gets the same reserved row.
func TestAnyPromptRowOwnerReservesTheRow(t *testing.T) {
	o := DefaultViewOptions()
	o.ShowPromptLine = true
	m, _ := viewModel(t, o, "hello\n", 60, 12)
	m.pushOverlay(&fakePromptRow{active: true})
	if l := m.layout(); !l.PromptOn || l.PromptY != 11 || l.StatusY != 10 {
		t.Fatalf("prompt open: prompt %d on=%v status %d, want 11 on and 10", l.PromptY, l.PromptOn, l.StatusY)
	}
}
