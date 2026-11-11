package tui

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/buffer"
	"github.com/tbxark/gvim/internal/tui/dialog"
	"github.com/tbxark/gvim/internal/tui/find"
	"github.com/tbxark/gvim/internal/tui/grid"
	"github.com/tbxark/gvim/internal/tui/list"
	"github.com/tbxark/gvim/internal/tui/palette"
	"github.com/tbxark/gvim/internal/tui/prompt"
)

// stackOverlay is the test overlay of the stack policy: it records the cancel path
// (Dismiss) and the Live Grep state a palette would keep.
type stackOverlay struct {
	open      bool
	dismissed bool
	resume    *palette.LiveResume
}

func (o *stackOverlay) Update(tea.Msg) (Overlay, tea.Cmd) { return o, nil }
func (o *stackOverlay) Draw(*grid.Grid)                   {}
func (o *stackOverlay) Active() bool                      { return o.open }
func (o *stackOverlay) Dismiss()                          { o.dismissed = true }
func (o *stackOverlay) LiveResumeState() *palette.LiveResume {
	return o.resume
}

// exclusiveOverlay is an ordinary transient overlay: opening it closes the other
// exclusive overlays.
type exclusiveOverlay struct{ *stackOverlay }

func (*exclusiveOverlay) ExclusiveOverlay() {}

// alertOverlay is a layering overlay that needs an answer (a dialog or a prompt in
// production): it keeps the input, and a new exclusive overlay goes under it.
type alertOverlay struct{ *stackOverlay }

func (*alertOverlay) AlertOverlay() {}

// policyModel returns a model over a plain document, for the stack policy tests.
func policyModel(t *testing.T) *Model {
	t.Helper()
	return newUtilityModel(t, newFakeEditor("one two three"), testKeys(), Settings{}, nil)
}

// TestExclusiveOverlayClosesTheExclusiveLayersBelow checks the one-at-a-time rule
// without touching the alert: the alert between the two exclusive overlays stays on
// top, the new exclusive overlay goes under it, and only the closed exclusive overlay
// runs its cancel path.
func TestExclusiveOverlayClosesTheExclusiveLayersBelow(t *testing.T) {
	t.Parallel()
	m := policyModel(t)
	first := &exclusiveOverlay{&stackOverlay{open: true}}
	alert := &alertOverlay{&stackOverlay{open: true}}
	second := &exclusiveOverlay{&stackOverlay{open: true}}
	m.pushOverlay(first)
	m.pushOverlay(alert)
	m.pushOverlay(second)
	if len(m.overlays) != 2 {
		t.Fatalf("overlays = %d, want the new exclusive overlay and the alert", len(m.overlays))
	}
	if m.overlays[0] != Overlay(second) || m.overlays[1] != Overlay(alert) {
		t.Fatalf("stack = %v, want the new exclusive overlay under the alert", m.overlays)
	}
	if !first.dismissed {
		t.Error("the closed exclusive overlay did not run its cancel path")
	}
	if alert.dismissed {
		t.Error("the alert was closed")
	}
	if second.dismissed {
		t.Error("the new exclusive overlay was closed instead of placed under the alert")
	}
}

// TestAlertOverlayStacksAndEscReturnsToTheExclusiveLayer checks the other half of
// the rule: an alert opens over the transient overlay and closing it gives the
// input back to that overlay instead of closing it.
func TestAlertOverlayStacksAndEscReturnsToTheExclusiveLayer(t *testing.T) {
	t.Parallel()
	m := policyModel(t)
	base := &exclusiveOverlay{&stackOverlay{open: true}}
	alert := &alertOverlay{&stackOverlay{open: true}}
	m.pushOverlay(base)
	m.pushOverlay(alert)
	if len(m.overlays) != 2 {
		t.Fatalf("overlays = %d, want the exclusive overlay and the alert", len(m.overlays))
	}
	press(m, special(tea.KeyEscape))
	if len(m.overlays) != 1 || m.overlays[0] != Overlay(base) {
		t.Fatalf("stack after closing the alert = %v, want the exclusive overlay", m.overlays)
	}
	if base.dismissed {
		t.Error("closing the alert closed the exclusive overlay under it")
	}
}

// TestExclusiveOverlayOpensUnderEveryAlert checks the alert block: with two alerts
// stacked, the new exclusive overlay goes under both, so closing the upper alert
// reveals the lower one, then the new overlay, never the new overlay over the lower
// alert.
func TestExclusiveOverlayOpensUnderEveryAlert(t *testing.T) {
	t.Parallel()
	m := policyModel(t)
	base := &exclusiveOverlay{&stackOverlay{open: true}}
	lower := &alertOverlay{&stackOverlay{open: true}}
	upper := &alertOverlay{&stackOverlay{open: true}}
	m.pushOverlay(base)
	m.pushOverlay(lower)
	m.pushOverlay(upper)
	next := &exclusiveOverlay{&stackOverlay{open: true}}
	m.pushOverlay(next)
	want := []Overlay{next, lower, upper}
	if len(m.overlays) != len(want) {
		t.Fatalf("overlays = %d, want the new overlay under both alerts", len(m.overlays))
	}
	for i := range want {
		if m.overlays[i] != want[i] {
			t.Fatalf("stack[%d] = %v, want %v", i, m.overlays[i], want[i])
		}
	}
	press(m, special(tea.KeyEscape))
	if len(m.overlays) != 2 || m.overlays[1] != Overlay(lower) {
		t.Fatalf("stack after closing the upper alert = %v, want the lower alert on top", m.overlays)
	}
	press(m, special(tea.KeyEscape))
	if len(m.overlays) != 1 || m.overlays[0] != Overlay(next) {
		t.Fatalf("stack after closing both alerts = %v, want the new exclusive overlay", m.overlays)
	}
}

// TestClosingAnExclusiveOverlayRunsCleanupAndKeepsLiveResume checks the cleanup a
// closed palette gets: Dismiss runs, and its Live Grep controls are captured for
// Resume as they are when Esc pops it.
func TestClosingAnExclusiveOverlayRunsCleanupAndKeepsLiveResume(t *testing.T) {
	t.Parallel()
	m := policyModel(t)
	closing := &exclusiveOverlay{&stackOverlay{open: true, resume: &palette.LiveResume{Text: "todo"}}}
	m.pushOverlay(closing)
	m.pushOverlay(&exclusiveOverlay{&stackOverlay{open: true}})
	if !closing.dismissed {
		t.Error("the closed palette did not run Dismiss")
	}
	if m.root.lastGrep == nil || m.root.lastGrep.Text != "todo" {
		t.Fatalf("lastGrep = %+v, want the closed palette's Live Grep controls", m.root.lastGrep)
	}
}

// TestOpeningThePaletteClosesTheFindBar covers the real pair: the find bar is on
// the stack, so opening the palette closes it through Dismiss (the caret and
// selection go back) instead of leaving both open.
func TestOpeningThePaletteClosesTheFindBar(t *testing.T) {
	t.Parallel()
	m := policyModel(t)
	m.openFind(false)
	if len(m.overlays) != 1 || !m.findOpen() {
		t.Fatalf("find bar not open: overlays %v", m.overlays)
	}
	bar := m.findBar
	m.openPalette(">")
	if len(m.overlays) != 1 {
		t.Fatalf("overlays = %d, want the palette alone", len(m.overlays))
	}
	if _, ok := m.overlays[0].(*palette.Palette); !ok {
		t.Fatalf("top overlay = %T, want the palette", m.overlays[0])
	}
	if bar.Active() {
		t.Error("the find bar was left active under the palette")
	}
}

// TestPaletteClosesTheFindBarThroughItsCancelPath checks the cleanup and focus
// restoration of a programmatic close: the bar moved the caret with F3, so closing it
// for the palette must run its Dismiss (the Esc path) and put the caret and selection
// back.
func TestPaletteClosesTheFindBarThroughItsCancelPath(t *testing.T) {
	t.Parallel()
	m, sf := newDefaultSearchModel(t, "foo bar foo")
	sf.doc.cursor = buffer.Pos{Line: 0, Col: 4}
	openBarWith(m, "foo")
	press(m, special(tea.KeyF3))
	if !sf.sel {
		t.Fatal("F3 selected nothing")
	}
	m.openPalette(">")
	if m.findBar.Active() {
		t.Error("the find bar was not dismissed")
	}
	if sf.sel || sf.doc.cursor != (buffer.Pos{Line: 0, Col: 4}) {
		t.Fatalf("caret %v selected %v after the palette closed the bar, want the original caret", sf.doc.cursor, sf.sel)
	}
	if len(m.overlays) != 1 {
		t.Fatalf("overlays = %d, want the palette alone", len(m.overlays))
	}
	if _, ok := m.overlays[0].(*palette.Palette); !ok {
		t.Fatalf("top overlay = %T, want the palette", m.overlays[0])
	}
}

// TestOpeningFindClosesThePalette is the reverse pair: the palette is removed and
// the find bar takes its place.
func TestOpeningFindClosesThePalette(t *testing.T) {
	t.Parallel()
	m := policyModel(t)
	m.openPalette(">")
	if len(m.overlays) != 1 {
		t.Fatalf("palette not open: overlays %d", len(m.overlays))
	}
	m.openFind(false)
	if len(m.overlays) != 1 {
		t.Fatalf("overlays = %d, want the find bar alone", len(m.overlays))
	}
	if _, ok := m.overlays[0].(*find.Bar); !ok {
		t.Fatalf("top overlay = %T, want the find bar", m.overlays[0])
	}
}

// TestPickerListClosesThePalette checks the picker family: a filterable list is an
// exclusive transient overlay too, so it replaces an open palette.
func TestPickerListClosesThePalette(t *testing.T) {
	t.Parallel()
	m := policyModel(t)
	m.openPalette(">")
	m.pushOverlay(list.New("Pick", []list.Row{{Left: "a", Value: "a"}}, m.styles.Overlay, nil))
	if len(m.overlays) != 1 {
		t.Fatalf("overlays = %d, want the list alone", len(m.overlays))
	}
	if _, ok := m.overlays[0].(*list.View); !ok {
		t.Fatalf("top overlay = %T, want the list", m.overlays[0])
	}
}

// TestInfoBoxStacksOverTheListThatOpenedIt pins the exception to the rule: a
// TextBox is an information box, not a transient interaction, so the warning detail
// opens over the warnings list without closing it.
func TestInfoBoxStacksOverTheListThatOpenedIt(t *testing.T) {
	t.Parallel()
	m := policyModel(t)
	m.pushOverlay(list.New("Warnings", []list.Row{{Left: "a"}}, m.listChrome(), nil))
	m.pushOverlay(list.NewTextBox(m.listChrome(), func() (string, []string, string) {
		return "Warning", []string{"a"}, "Enter or Esc close"
	}))
	if len(m.overlays) != 2 {
		t.Fatalf("overlays = %d, want the list and the info box", len(m.overlays))
	}
	if _, ok := m.overlays[0].(*list.View); !ok {
		t.Fatalf("bottom overlay = %T, want the warnings list", m.overlays[0])
	}
	if _, ok := m.overlays[1].(*list.TextBox); !ok {
		t.Fatalf("top overlay = %T, want the info box", m.overlays[1])
	}
}

// TestFindBarOffTheStackIsClosedToo covers the find bar find_selection builds
// without pushing it: the stack walk cannot see it, so the policy dismisses it
// explicitly, exactly as Esc would.
func TestFindBarOffTheStackIsClosedToo(t *testing.T) {
	t.Parallel()
	m := policyModel(t)
	m.findBar = m.newFindBar(false)
	if m.findBar == nil || !m.findBar.Active() {
		t.Fatal("the test model has no active off-stack find bar")
	}
	m.pushOverlay(&exclusiveOverlay{&stackOverlay{open: true}})
	if m.findBar.Active() {
		t.Error("the off-stack find bar was left active")
	}
	if len(m.overlays) != 1 {
		t.Fatalf("overlays = %d, want the pushed overlay", len(m.overlays))
	}
}

// TestPromptStacksOverThePalette covers the alert itself: a real prompt opens over
// the palette and Esc returns to the palette.
func TestPromptStacksOverThePalette(t *testing.T) {
	t.Parallel()
	m := policyModel(t)
	m.openPalette(">")
	m.pushOverlay(prompt.NewConfirm(m.styles, "Save the file?", "policy-test"))
	if len(m.overlays) != 2 {
		t.Fatalf("overlays = %d, want the palette and the prompt", len(m.overlays))
	}
	press(m, special(tea.KeyEscape))
	if len(m.overlays) != 1 {
		t.Fatalf("overlays after Esc = %d, want the palette", len(m.overlays))
	}
	if _, ok := m.overlays[0].(*palette.Palette); !ok {
		t.Fatalf("top overlay = %T, want the palette", m.overlays[0])
	}
}

// TestFindBarOpensUnderAnOpenPrompt covers the real alert type: the find bar opened
// while a prompt asks its question goes under the prompt, and closing the prompt
// gives the find bar the input.
func TestFindBarOpensUnderAnOpenPrompt(t *testing.T) {
	t.Parallel()
	m := policyModel(t)
	m.openPalette(">")
	m.pushOverlay(prompt.NewConfirm(m.styles, "Discard the changes?", "policy-test"))
	m.openFind(false)
	if len(m.overlays) != 2 {
		t.Fatalf("overlays = %d, want the find bar under the prompt", len(m.overlays))
	}
	if _, ok := m.overlays[0].(*find.Bar); !ok {
		t.Fatalf("bottom overlay = %T, want the find bar", m.overlays[0])
	}
	if _, ok := m.overlays[1].(*prompt.Confirm); !ok {
		t.Fatalf("top overlay = %T, want the prompt", m.overlays[1])
	}
	press(m, special(tea.KeyEscape))
	if len(m.overlays) != 1 || !m.findOpen() {
		t.Fatalf("stack after answering the prompt = %v, want the find bar on top", m.overlays)
	}
}

// TestErrorDialogKeepsTheInputOverANewTransient pins the dialog side of the alert
// marker (the method is promoted from the embedded modal): the dialog stays on top and
// the find bar opened meanwhile waits under it.
func TestErrorDialogKeepsTheInputOverANewTransient(t *testing.T) {
	t.Parallel()
	m := policyModel(t)
	m.openPalette(">")
	d := dialog.NewErrorDialog(testClock, "The save failed", "policy-test", m.styles.Overlay)
	m.pushOverlay(d)
	if len(m.overlays) != 2 || m.overlays[1] != Overlay(d) {
		t.Fatalf("stack = %v, want the palette and the error dialog on top", m.overlays)
	}
	m.openFind(false)
	if len(m.overlays) != 2 {
		t.Fatalf("overlays = %d, want the find bar under the dialog", len(m.overlays))
	}
	if _, ok := m.overlays[0].(*find.Bar); !ok {
		t.Fatalf("bottom overlay = %T, want the find bar", m.overlays[0])
	}
	if m.overlays[1] != Overlay(d) {
		t.Fatalf("top overlay = %T, want the error dialog", m.overlays[1])
	}
}

// TestReopenedFindBarStaysOpen guards the exception in the policy: query_replace
// reopens the popped bar and pushes it, and that push must not dismiss the bar it
// is pushing.
func TestReopenedFindBarStaysOpen(t *testing.T) {
	t.Parallel()
	m := policyModel(t)
	m.openFind(false)
	press(m, special(tea.KeyEscape))
	if m.findOpen() {
		t.Fatal("Esc did not close the find bar")
	}
	m.queryReplace()
	if !m.findOpen() {
		t.Fatal("query_replace reopened the find bar but the push closed it again")
	}
}
