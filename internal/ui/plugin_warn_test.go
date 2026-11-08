package ui

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/internal/editor"
	"github.com/tbxark/gvim/internal/pluginapi"
	"github.com/tbxark/gvim/internal/theme"
)

// A name in a when-clause that no condition answers is a typo: one warning, naming it.
func TestWhenTyposAreReportedOnce(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		when []string
		set  bool // a plugin sets the context first
		warn bool
	}{
		{"typo", []string{"has_bufferr"}, false, true},
		{"negated typo", []string{"!read_onlyy"}, false, true},
		{"built-in condition", []string{"has_buffer", "!read_only"}, false, false},
		{"language", []string{"language:go"}, false, false},
		{"mode", []string{"mode:p:m"}, false, false},
		{"a context a plugin set", []string{"busy"}, true, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			e := newKeyEnv(t)
			if tt.set {
				e.m.plug.host.SetContext("p", "busy", false)
			}
			e.m.plug.host.SetCommands("p", []pluginapi.CommandInfo{{ID: "p.typo", Plugin: "p", Title: "Typo", When: tt.when}})
			e.m.notice = ""
			e.m.allCommands()
			e.m.allCommands()
			got := strings.Contains(e.m.notice, "is not a known condition")
			if got != tt.warn {
				t.Fatalf("notice = %q, want a warning: %v", e.m.notice, tt.warn)
			}
			if tt.warn {
				bad := strings.TrimPrefix(tt.when[0], "!")
				if !strings.Contains(e.m.notice, "'"+bad+"'") || !strings.HasPrefix(e.m.notice, "p: ") {
					t.Fatalf("the warning does not name the plugin and the key: %q", e.m.notice)
				}
				e.m.notice = ""
				e.m.allCommands()
				if e.m.notice != "" {
					t.Fatalf("the warning was shown twice: %q", e.m.notice)
				}
			}
		})
	}
}

// A key a plugin bound to an action that does not exist says so once, when it is pressed.
func TestUnknownActionOfAPluginKeyIsReported(t *testing.T) {
	t.Parallel()
	rt := newFakeRuntime(t)
	ed := newFakeEditor("x")
	unknown := &errEditor{fakeEditor: ed, err: fmt.Errorf("%w: nonexistent.action", editor.ErrUnknownAction)}
	m := New(Deps{Editor: unknown, Keys: testKeys(), Theme: theme.Theme{}, Clock: testClock, Root: RootPorts{Plugins: rt}})
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	useHost(rt, m)
	m.plug.host.SetBindings("p", []pluginapi.Binding{{Chord: "ctrl+alt+z", Action: "nonexistent.action", Plugin: "p"}})

	m.dispatch(Command{Action: "nonexistent.action"})
	want := "p: key ctrl+alt+z is bound to 'nonexistent.action', which is not an action or a command"
	if m.notice != want {
		t.Fatalf("notice = %q, want %q", m.notice, want)
	}
	m.notice = ""
	m.dispatch(Command{Action: "nonexistent.action"})
	if m.notice != "" {
		t.Fatalf("the warning was shown twice: %q", m.notice)
	}
	// an action nobody bound is not the plugin's doing
	m.dispatch(Command{Action: "something.else"})
	if m.notice != "" {
		t.Fatalf("a warning for an action no plugin bound: %q", m.notice)
	}
}
