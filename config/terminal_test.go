package config

import (
	"reflect"
	"strings"
	"testing"
)

func TestTerminalDefaults(t *testing.T) {
	t.Parallel()
	cfg, diags := loadBody(t, "{}")
	if len(diags) != 0 {
		t.Fatalf("diagnostics = %v", diags)
	}
	want := TerminalConfig{CloseOnExit: false, ScrollbackLines: TerminalScrollbackDefault, MouseForwarding: MouseForwardRequested}
	if !reflect.DeepEqual(cfg.Terminal, want) {
		t.Fatalf("Terminal = %+v, want %+v", cfg.Terminal, want)
	}
}

func TestTerminalSection(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		body string
		want TerminalConfig
	}{
		{
			name: "the shell override",
			body: `{"terminal": {"shell": {"command": "/usr/bin/fish", "args": ["-l"]}}}`,
			want: withTerminal(func(c *TerminalConfig) { c.Shell = TerminalShell{Command: "/usr/bin/fish", Args: []string{"-l"}} }),
		},
		{
			name: "an empty shell object is the user's shell",
			body: `{"terminal": {"shell": {}}}`,
			want: DefaultTerminalOptions(),
		},
		{
			name: "close on exit",
			body: `{"terminal": {"close_on_exit": true}}`,
			want: withTerminal(func(c *TerminalConfig) { c.CloseOnExit = true }),
		},
		{
			name: "scrollback lines",
			body: `{"terminal": {"scrollback_lines": 500}}`,
			want: withTerminal(func(c *TerminalConfig) { c.ScrollbackLines = 500 }),
		},
		{
			name: "scrollback disabled",
			body: `{"terminal": {"scrollback_lines": 0}}`,
			want: withTerminal(func(c *TerminalConfig) { c.ScrollbackLines = 0 }),
		},
		{
			name: "the legacy mouse rule",
			body: `{"terminal": {"mouse_forwarding": "alt_screen"}}`,
			want: withTerminal(func(c *TerminalConfig) { c.MouseForwarding = MouseForwardAltScreen }),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cfg, diags := loadBody(t, tt.body)
			if len(diags) != 0 {
				t.Fatalf("diagnostics = %v", diags)
			}
			if !reflect.DeepEqual(cfg.Terminal, tt.want) {
				t.Fatalf("Terminal = %+v, want %+v", cfg.Terminal, tt.want)
			}
		})
	}
}

// withTerminal returns the terminal defaults after change.
func withTerminal(change func(*TerminalConfig)) TerminalConfig {
	c := DefaultTerminalOptions()
	change(&c)
	return c
}

// TestTerminalFreshKeysAccepted checks that the Fresh terminal keys gvim does not
// implement load without diagnostics: a Fresh config file opens clean.
func TestTerminalFreshKeysAccepted(t *testing.T) {
	t.Parallel()
	body := `{"terminal": {
  "jump_to_end_on_output": false,
  "resume_agents": false,
  "mouse_drag_selects": false,
  "pass_ctrl_v_without_text": false,
  "skip_app_execution_alias": false
}}`
	cfg, diags := loadBody(t, body)
	if len(diags) != 0 {
		t.Fatalf("diagnostics for Fresh's terminal keys = %v", diags)
	}
	if !reflect.DeepEqual(cfg.Terminal, DefaultTerminalOptions()) {
		t.Fatalf("Terminal = %+v, want the defaults", cfg.Terminal)
	}
}

func TestTerminalInvalidValues(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		body string
		key  string
	}{
		{"a negative history", `{"terminal": {"scrollback_lines": -1}}`, "terminal.scrollback_lines"},
		{"an oversized history", `{"terminal": {"scrollback_lines": 2000000}}`, "terminal.scrollback_lines"},
		{"a history that is not a number", `{"terminal": {"scrollback_lines": "many"}}`, "terminal.scrollback_lines"},
		{"an unknown mouse rule", `{"terminal": {"mouse_forwarding": "always"}}`, "terminal.mouse_forwarding"},
		{"a mouse rule that is not a string", `{"terminal": {"mouse_forwarding": 1}}`, "terminal.mouse_forwarding"},
		{"close on exit that is not a boolean", `{"terminal": {"close_on_exit": "yes"}}`, "terminal.close_on_exit"},
		{"a shell that is not an object", `{"terminal": {"shell": "fish"}}`, "terminal.shell"},
		{"a null value", `{"terminal": {"close_on_exit": null}}`, "terminal.close_on_exit"},
		{"an unknown key", `{"terminal": {"always_use_mouse": true}}`, "terminal.always_use_mouse"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cfg, diags := loadBody(t, tt.body)
			if len(diags) != 1 || diags[0].Key != tt.key {
				t.Fatalf("diagnostics = %+v, want one for %s", diags, tt.key)
			}
			if diags[0].Path == "" {
				t.Fatalf("the diagnostic does not name the file: %+v", diags[0])
			}
			if !reflect.DeepEqual(cfg.Terminal, DefaultTerminalOptions()) {
				t.Fatalf("a rejected value changed the config: %+v", cfg.Terminal)
			}
		})
	}
}

// TestProjectLayerCannotSetTerminalShell checks the trust rule: a repository can
// tune the dock, but must not choose the program gvim executes.
func TestProjectLayerCannotSetTerminalShell(t *testing.T) {
	t.Parallel()
	cfg, diags := loadLayers(t, `{"terminal": {"shell": {"command": "/usr/bin/fish"}}}`, `{"terminal": {"shell": {"command": "/tmp/evil"}, "close_on_exit": true}}`)
	if len(diags) != 1 || !strings.Contains(diags[0].Key, "terminal.shell") || !strings.Contains(diags[0].Message, "project config cannot set terminal.shell") {
		t.Fatalf("diagnostics = %+v, want the terminal.shell refusal", diags)
	}
	if diags[0].Path == "" {
		t.Fatalf("the refusal does not name the project file: %+v", diags[0])
	}
	if got, _ := cfg.Terminal.TerminalShellOptions(); got != "/usr/bin/fish" {
		t.Fatalf("shell = %q, want the user's /usr/bin/fish", got)
	}
	if !cfg.Terminal.CloseOnExit {
		t.Fatal("the project layer could not tune close_on_exit, which is allowed")
	}
}

// TestTerminalSource checks that the config records which file set the section.
func TestTerminalSource(t *testing.T) {
	t.Parallel()
	cfg, diags := loadBody(t, `{"terminal": {"close_on_exit": true}}`)
	if len(diags) != 0 {
		t.Fatalf("diagnostics = %v", diags)
	}
	if got := cfg.Source("terminal"); got == "" {
		t.Fatal("the source of terminal is not recorded")
	}
}

func TestTerminalShellOptions(t *testing.T) {
	t.Parallel()
	empty := Defaults()
	if cmd, args := empty.Terminal.TerminalShellOptions(); cmd != "" || args != nil {
		t.Fatalf("an empty shell override = %q, %v; want the user's shell", cmd, args)
	}
	full := withTerminal(func(c *TerminalConfig) { c.Shell = TerminalShell{Command: "fish", Args: []string{"-l"}} })
	cmd, args := full.TerminalShellOptions()
	if cmd != "fish" || !reflect.DeepEqual(args, []string{"-l"}) {
		t.Fatalf("TerminalShellOptions = %q, %v; want fish and [-l]", cmd, args)
	}
}
