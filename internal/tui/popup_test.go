package tui

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/internal/tui/popup"
)

// samplePopupItems are the rows the root popup tests show.
func samplePopupItems() []popup.Item {
	return []popup.Item{{Label: "alpha", Data: 7}, {Label: "beta"}}
}

func TestShowPopupEnterDeliversTheChosenItem(t *testing.T) {
	ed := newFakeEditor("hello")
	m := newUtilityModel(t, ed, testKeys(), Settings{}, nil)
	m.ShowPopup(popup.Point{X: 2, Y: 3}, "Pick", samplePopupItems())
	if len(m.overlays) != 1 {
		t.Fatalf("overlays = %d, want the popup", len(m.overlays))
	}
	if !frameHas(m, "Pick") || !frameHas(m, "alpha") {
		t.Fatal("the popup does not show its title and items")
	}
	cmd := press(m, special(tea.KeyEnter))
	if cmd == nil {
		t.Fatal("Enter emitted no command")
	}
	got, ok := cmd().(popup.Chosen)
	if !ok || got.Index != 0 || got.Item.Data != 7 {
		t.Fatalf("chosen = %#v, want index 0 with the item data", got)
	}
	if len(m.overlays) != 0 {
		t.Fatal("the popup stayed open after the choice")
	}
	if len(ed.actions) != 0 {
		t.Fatalf("the popup let Enter reach the editor: %v", ed.actions)
	}
}

func TestPopupEscCloses(t *testing.T) {
	ed := newFakeEditor("hello")
	m := newUtilityModel(t, ed, testKeys(), Settings{}, nil)
	m.ShowPopup(popup.Point{}, "Pick", samplePopupItems())
	press(m, special(tea.KeyEscape))
	if len(m.overlays) != 0 {
		t.Fatalf("Esc left %d overlays open", len(m.overlays))
	}
}

func TestPopupFocusMovesTheTopmostPopupToTheTop(t *testing.T) {
	ed := newFakeEditor("hello")
	m := newUtilityModel(t, ed, helpKeys(), Settings{}, nil)
	m.ShowPopup(popup.Point{}, "Pick", samplePopupItems())
	runAction(t, m, "show_help")
	if err := m.Run("popup_focus"); err != nil {
		t.Fatalf("Run = %v, want nil", err)
	}
	if len(m.overlays) != 2 {
		t.Fatalf("overlays = %d, want 2", len(m.overlays))
	}
	if _, ok := m.overlays[len(m.overlays)-1].(*popup.Popup); !ok {
		t.Fatal("popup_focus did not put the popup on top")
	}
	if m.notice != "" {
		t.Fatalf("notice = %q, want none", m.notice)
	}
}

func TestPopupFocusWhenPopupIsAlreadyOnTopChangesNothing(t *testing.T) {
	ed := newFakeEditor("hello")
	m := newUtilityModel(t, ed, testKeys(), Settings{}, nil)
	m.ShowPopup(popup.Point{}, "Pick", samplePopupItems())
	runAction(t, m, "popup_focus")
	if len(m.overlays) != 1 {
		t.Fatalf("overlays = %d, want 1", len(m.overlays))
	}
	if _, ok := m.overlays[0].(*popup.Popup); !ok {
		t.Fatal("the popup is no longer the overlay")
	}
}

func TestPopupFocusWithoutPopupSaysSo(t *testing.T) {
	ed := newFakeEditor("hello")
	m := newUtilityModel(t, ed, testKeys(), Settings{}, nil)
	runAction(t, m, "popup_focus")
	if len(m.overlays) != 0 {
		t.Fatalf("overlays = %d, want none", len(m.overlays))
	}
	if !frameHas(m, "No popup to focus") {
		t.Fatal("the status row does not say there is no popup")
	}
}
