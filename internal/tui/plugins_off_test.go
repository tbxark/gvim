package tui

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	commands "github.com/tbxark/gvim/commands"
	"github.com/tbxark/gvim/pluginapi"
	"github.com/tbxark/gvim/theme"
)

// unwiredRuntime stands for a plugin runtime the ui must not call yet: its methods
// are the nil embedded interface's, so any call panics.
type unwiredRuntime struct{ pluginapi.Runtime }

// TestPluginsAreInertUntilTheRuntimeIsWired checks that, with no runtime or with a
// runtime that nothing calls yet, the palette lists only the built-in commands and
// building and sizing the model never reaches the runtime.
func TestPluginsAreInertUntilTheRuntimeIsWired(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		plugins pluginapi.Runtime
	}{
		{"nil runtime", nil},
		{"runtime present but unwired", unwiredRuntime{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ed := newFakeEditor("x")
			ed.quitAction = "quit"
			m := New(Deps{Editor: ed, Keys: testKeys(), Theme: theme.Theme{}, Clock: testClock, Root: RootPorts{Plugins: tt.plugins}})
			m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
			if len(m.commands) == 0 {
				t.Fatal("palette has no commands")
			}
			for _, c := range m.commands {
				if c.Source != commands.SourceBuiltin {
					t.Fatalf("palette lists %q from source %q, want only built-in commands", c.Action, c.Source)
				}
			}
		})
	}
}
