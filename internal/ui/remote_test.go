package ui

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestRemoteMenuSaysLocalAndRemoteIsNotSupported(t *testing.T) {
	ed := newFakeEditor("hello")
	m := newUtilityModel(t, ed, testKeys(), Settings{}, nil)
	if err := m.Run("show_remote_indicator_menu"); err != nil {
		t.Fatalf("Run = %v, want nil", err)
	}
	if len(m.overlays) != 1 {
		t.Fatalf("overlays = %d, want the menu", len(m.overlays))
	}
	if !frameHas(m, "Remote") || !frameRowHas(m, "Workspace", "Local") {
		t.Fatal("the menu does not say the workspace is local")
	}
	if !frameHas(m, "Remote workspaces are not supported") {
		t.Fatal("the menu does not say remote workspaces are not supported")
	}
}

func TestRemoteMenuEnterAndEscClose(t *testing.T) {
	for _, k := range []tea.KeyPressMsg{special(tea.KeyEnter), special(tea.KeyEscape)} {
		ed := newFakeEditor("hello")
		m := newUtilityModel(t, ed, testKeys(), Settings{}, nil)
		runAction(t, m, "show_remote_indicator_menu")
		press(m, k)
		if len(m.overlays) != 0 {
			t.Fatalf("key %v left %d overlays open", k, len(m.overlays))
		}
	}
}
