package tui

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

// remoteMenuSetup is one focus scene from which Fresh's global F6 must open the
// Remote Indicator menu.
type remoteMenuSetup struct {
	name  string
	setup func(t *testing.T) *Model
}

// TestF6OpensRemoteIndicatorMenuEverywhere checks Fresh's F6 semantics on the real
// key path: the global Remote Indicator menu opens from the editor and every chrome
// focus scene (tab strip, explorer, Contents, Diagnostics, dock, focused plugin
// section, open menu bar), reports the local workspace and that this session has no
// remote authority (the remote wording is covered by
// TestF6RemoteMenuShowsTheRemoteAuthority), and does not move the chrome focus.
func TestF6OpensRemoteIndicatorMenuEverywhere(t *testing.T) {
	t.Parallel()
	base := func(t *testing.T) *Model {
		return newTOCModel(t, newTOCEditor("notes.md", "# One"), nil)
	}
	scenes := []remoteMenuSetup{
		{name: "editor", setup: base},
		{name: "tab strip", setup: func(t *testing.T) *Model {
			m := base(t)
			m.tabFocus = true
			return m
		}},
		{name: "explorer", setup: func(t *testing.T) *Model {
			m := base(t)
			m.takeExplorerFocus()
			return m
		}},
		{name: "contents", setup: func(t *testing.T) *Model {
			m := base(t)
			m.takeContentsFocus()
			return m
		}},
		{name: "diagnostics", setup: func(t *testing.T) *Model {
			m := base(t)
			m.diag.open = true
			m.takeDiagFocus()
			return m
		}},
		{name: "dock", setup: func(t *testing.T) *Model {
			m := base(t)
			m.dock.SetFocus(true)
			return m
		}},
		{name: "focused plugin section", setup: func(t *testing.T) *Model {
			m := base(t)
			mp := mountFocusSection(m, 31)
			m.focusPluginSection(mp)
			return m
		}},
		{name: "open menu bar", setup: func(t *testing.T) *Model {
			m := base(t)
			m.menu = m.menu.Open(0)
			return m
		}},
	}
	for _, scene := range scenes {
		t.Run(scene.name, func(t *testing.T) {
			t.Parallel()
			m := scene.setup(t)
			stageBefore := m.stage()
			press(m, special(tea.KeyF6))
			if len(m.overlays) != 1 {
				t.Fatalf("F6 opened %d overlays, want the Remote Indicator menu", len(m.overlays))
			}
			if !frameHas(m, "Remote") || !frameRowHas(m, "Workspace", "Local") {
				t.Fatal("F6 menu does not report the local workspace")
			}
			if !frameHas(m, "No remote authority in this session") {
				t.Fatal("F6 local menu does not say this session has no remote authority")
			}
			if frameHas(m, "not supported") {
				t.Fatal("F6 local menu still claims remote workspaces are unsupported")
			}
			if got := m.stage(); got != stageBefore {
				t.Fatalf("F6 moved the chrome focus: stage %v -> %v", stageBefore, got)
			}
			// The menu is modal: Escape closes it and the previous focus remains.
			press(m, special(tea.KeyEscape))
			if len(m.overlays) != 0 {
				t.Fatalf("Escape left %d overlays open", len(m.overlays))
			}
		})
	}
}

// TestF6RemoteMenuLeavesAPluginSectionFocused checks F6 does not blur a focused
// plugin section: the Remote Indicator menu is a global popup, not a chrome focus
// move (the F7 chrome cycle is the key that leaves the section).
func TestF6RemoteMenuLeavesAPluginSectionFocused(t *testing.T) {
	t.Parallel()
	m := newTOCModel(t, newTOCEditor("notes.md", "# One"), nil)
	mp := mountFocusSection(m, 32)
	m.focusPluginSection(mp)
	press(m, special(tea.KeyF6))
	if m.plug.panels.focus != mp.id || !mp.panel.Focused() {
		t.Fatalf("F6 blurred the plugin section: %v/%v", m.plug.panels.focus, mp.panel.Focused())
	}
	press(m, special(tea.KeyEscape))
	if m.plug.panels.focus != mp.id || !mp.panel.Focused() {
		t.Fatalf("closing the F6 menu blurred the plugin section: %v/%v", m.plug.panels.focus, mp.panel.Focused())
	}
}

// TestF6RemoteMenuShowsTheRemoteAuthority checks F6 from a remote workspace: the
// menu shows the actual SSH authority, its state and the remote root (and the local
// wording is not used).
func TestF6RemoteMenuShowsTheRemoteAuthority(t *testing.T) {
	t.Parallel()
	m := newTOCModel(t, newTOCEditor("notes.md", "# One"), nil)
	m.fileRoot = "/remote/w"
	m.SetAuthorityHooks(AuthorityHooks{
		Status: func() AuthorityStatus {
			return AuthorityStatus{Text: "SSH:alice@build-host", Connected: true}
		},
	})
	m.takeExplorerFocus()
	press(m, special(tea.KeyF6))
	if len(m.overlays) != 1 {
		t.Fatal("F6 did not open the Remote Indicator menu")
	}
	if !frameHas(m, "Workspace: Remote") || !frameHas(m, "alice@build-host") {
		t.Fatal("the remote F6 menu does not show the SSH authority")
	}
	if !frameHas(m, "Connected") || !frameHas(m, "Remote root") || !frameHas(m, "/remote/w") {
		t.Fatal("the remote F6 menu does not show the state and remote root")
	}
	if frameHas(m, "Workspace: Local") {
		t.Fatal("the remote F6 menu calls the workspace local")
	}
}
