package tui

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/internal/tui/ctxmenu"
	"github.com/tbxark/gvim/internal/tui/list"
	"github.com/tbxark/gvim/internal/tui/prompt"
	"github.com/tbxark/gvim/theme"
)

// overlayModel is a model over one document with the bindings of an overlay context.
func overlayModel(t *testing.T, bindings fakeKeymap) *Model {
	t.Helper()
	m := New(Deps{
		Editor: newFakeEditor("hello"), Explorer: newFakeTree(1), Keys: bindings,
		Theme: theme.Theme{}, Clock: testClock,
	})
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	return m
}

// pushPrompt pushes a one-line prompt and returns it.
func pushPrompt(m *Model, input string) *prompt.Line {
	p := prompt.NewLine(m.styles, prompt.LineOptions{Label: "Value:", Input: input, Tag: "test"})
	m.pushOverlay(p)
	return p
}

func TestOverlayContextsMapToTheirFreshNames(t *testing.T) {
	t.Parallel()
	m := overlayModel(t, fakeKeymap{})
	p := pushPrompt(m, "")
	if ctx, ok := overlayContextOf(p); !ok || ctx != KeyContextPrompt {
		t.Fatalf("prompt context = %q, %v", ctx, ok)
	}
	c := prompt.NewConfirm(m.styles, "Sure?", "tag")
	if ctx, ok := overlayContextOf(c); !ok || ctx != KeyContextPrompt {
		t.Fatalf("confirm context = %q, %v", ctx, ok)
	}
	v := list.New("Pick", []list.Row{{Left: "a", Value: "a"}}, m.styles.Overlay, nil)
	if ctx, ok := overlayContextOf(v); !ok || ctx != KeyContextPopup {
		t.Fatalf("list context = %q, %v", ctx, ok)
	}
	menu := ctxmenu.New(m.styles.Overlay)
	if ctx, ok := overlayContextOf(menu); !ok || ctx != KeyContextMenu {
		t.Fatalf("context menu context = %q, %v", ctx, ok)
	}
	m.openFind(false)
	if ctx, ok := overlayContextOf(m.overlays[len(m.overlays)-1]); !ok || ctx != KeyContextSearchPrompt {
		t.Fatalf("find bar context = %q, %v", ctx, ok)
	}
}

func TestPromptContextBindings(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		bindings fakeKeymap
		open     func(m *Model) *prompt.Line
		key      tea.KeyPressMsg
		check    func(t *testing.T, m *Model, p *prompt.Line)
	}{
		{
			name:     "a bound cancel chord closes the prompt",
			bindings: fakeKeymap{"prompt|t|ctrl": {Action: "prompt_cancel"}},
			open:     func(m *Model) *prompt.Line { return pushPrompt(m, "x") },
			key:      ctrl('t'),
			check: func(t *testing.T, m *Model, _ *prompt.Line) {
				t.Helper()
				if len(m.overlays) != 0 {
					t.Fatal("the bound cancel chord left the prompt open")
				}
			},
		},
		{
			name:     "the default cancel chord still works after a rebind",
			bindings: fakeKeymap{"prompt|t|ctrl": {Action: "prompt_cancel"}},
			open:     func(m *Model) *prompt.Line { return pushPrompt(m, "x") },
			key:      special(tea.KeyEscape),
			check: func(t *testing.T, m *Model, _ *prompt.Line) {
				t.Helper()
				if len(m.overlays) != 0 {
					t.Fatal("Esc left the prompt open")
				}
			},
		},
		{
			name:     "a bound edit action edits the field",
			bindings: fakeKeymap{"prompt|u|ctrl": {Action: "prompt_delete_word_backward"}},
			open:     func(m *Model) *prompt.Line { return pushPrompt(m, "one two") },
			key:      ctrl('u'),
			check: func(t *testing.T, _ *Model, p *prompt.Line) {
				t.Helper()
				if got := p.Value(); got != "one " {
					t.Fatalf("value after the bound delete-word = %q, want %q", got, "one ")
				}
			},
		},
		{
			name:     "an unbound key falls through to the field",
			bindings: fakeKeymap{"prompt|t|ctrl": {Action: "prompt_cancel"}},
			open:     func(m *Model) *prompt.Line { return pushPrompt(m, "one two") },
			key:      ctrl('u'),
			check: func(t *testing.T, _ *Model, p *prompt.Line) {
				t.Helper()
				if got := p.Value(); got != "" {
					t.Fatalf("value after the unbound Ctrl+U = %q, want the field's own delete", got)
				}
			},
		},
		{
			name:     "the default confirm chord still submits",
			bindings: fakeKeymap{"prompt|t|ctrl": {Action: "prompt_paste"}},
			open:     func(m *Model) *prompt.Line { return pushPrompt(m, "42") },
			key:      special(tea.KeyEnter),
			check: func(t *testing.T, m *Model, _ *prompt.Line) {
				t.Helper()
				if len(m.overlays) != 0 {
					t.Fatal("Enter left the prompt open")
				}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := overlayModel(t, tt.bindings)
			p := tt.open(m)
			_, cmd := m.Update(tt.key)
			runCmds(m, cmd)
			tt.check(t, m, p)
		})
	}
}

func TestPopupContextBindings(t *testing.T) {
	t.Parallel()
	t.Run("a bound confirm chord chooses the highlighted row", func(t *testing.T) {
		m := overlayModel(t, fakeKeymap{"popup|s|ctrl": {Action: "popup_confirm"}})
		chosen := ""
		v := list.New("Pick", []list.Row{{Left: "a", Value: "a"}, {Left: "b", Value: "b"}}, m.styles.Overlay, func(_ *list.View, r list.Row) tea.Cmd {
			chosen = r.Value
			return nil
		})
		m.pushOverlay(v)
		_, cmd := m.Update(ctrl('s'))
		runCmds(m, cmd)
		if chosen != "a" {
			t.Fatalf("chosen = %q, want the highlighted row", chosen)
		}
	})
	t.Run("a bound cancel chord closes the list", func(t *testing.T) {
		m := overlayModel(t, fakeKeymap{"popup|q|ctrl": {Action: "popup_cancel"}})
		m.pushOverlay(list.New("Pick", []list.Row{{Left: "a"}}, m.styles.Overlay, nil))
		m.Update(ctrl('q'))
		if len(m.overlays) != 0 {
			t.Fatal("the bound cancel chord left the list open")
		}
	})
	t.Run("an unbound key falls through to the list", func(t *testing.T) {
		m := overlayModel(t, fakeKeymap{"popup|s|ctrl": {Action: "popup_confirm"}})
		m.pushOverlay(list.New("Pick", []list.Row{{Left: "a"}}, m.styles.Overlay, nil))
		if _, cmd := m.Update(ctrl('x')); cmd != nil || len(m.overlays) != 1 {
			t.Fatalf("the unbound key = %v, overlays %d", cmd, len(m.overlays))
		}
	})
}

func TestMenuContextBindings(t *testing.T) {
	t.Parallel()
	m := menuModel(t, false)
	keys := menuKeys()
	keys["menu|j|"] = Command{Action: "menu_down"}
	keys["menu|q|ctrl"] = Command{Action: "menu_close"}
	m.keys = keys
	runAction(t, m, "menu_activate")
	if !m.menu.IsOpen() {
		t.Fatal("the menu did not open")
	}
	before := m.menu.Selected()
	m.Update(typed("j"))
	if got := m.menu.Selected(); got == before {
		t.Fatalf("the bound menu_down chord left the highlight at %d", got)
	}
	m.Update(ctrl('q'))
	if m.menu.IsOpen() {
		t.Fatal("the bound menu_close chord left the menu open")
	}
}

func TestSettingsContextBindingClosesThePage(t *testing.T) {
	t.Parallel()
	m := overlayModel(t, fakeKeymap{"settings|t|ctrl": {Action: "close_settings"}})
	page := &settingsPage{m: m, active: true}
	m.pushOverlay(page)
	m.Update(ctrl('t'))
	if len(m.overlays) != 0 {
		t.Fatal("close_settings left the settings page open")
	}
}

func TestDockContextBindingRunsTheActionNotTheShell(t *testing.T) {
	t.Parallel()
	starter := newFakeStarter()
	m := newDockModel(t, newFakeEditor("hello"), DockPorts{Starter: starter})
	openDock(t, m)
	keys := dockKeys()
	keys["dock|n|alt"] = Command{Action: "focus_editor"}
	m.keys = keys
	// With capture off the dock's own context runs first: the key must not reach the shell.
	m.Update(altKey('n'))
	if got := starter.term.writes; len(got) != 0 {
		t.Fatalf("the dock context binding wrote to the shell: %q", got)
	}
}

func TestSearchPromptContextBindingStepsTheMatch(t *testing.T) {
	t.Parallel()
	m, sf := newDefaultSearchModel(t, "foo bar foo")
	keys := testKeys()
	keys["normal|f|ctrl"] = Command{Action: "search"}
	keys["searchPrompt|z|ctrl"] = Command{Action: "find_next"}
	m.keys = keys
	// Open the bar, search, and submit: the caret lands on the first match.
	press(m, ctrl('f'))
	for _, r := range "foo" {
		press(m, typed(string(r)))
	}
	press(m, special(tea.KeyEnter))
	// Reopen the bar and press the bound step chord: it runs find_next, not the bar's own key.
	press(m, ctrl('f'))
	press(m, ctrl('z'))
	if sf.anchor.Col != 8 {
		t.Fatalf("the bound find_next chord left the anchor at %+v, want the second match", sf.anchor)
	}
}
