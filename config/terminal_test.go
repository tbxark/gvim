package config

import (
	"path/filepath"
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
	want := TerminalConfig{
		CloseOnExit: false, ScrollbackLines: TerminalScrollbackDefault,
		MouseForwarding:      MouseForwardRequested,
		JumpToEndOnOutput:    true,
		MouseDragSelects:     true,
		PassCtrlVWithoutText: true,
		ResumeAgents:         true,
		RestoreLive:          true,
	}
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
		{
			name: "hold the scrollback on new output",
			body: `{"terminal": {"jump_to_end_on_output": false}}`,
			want: withTerminal(func(c *TerminalConfig) { c.JumpToEndOnOutput = false }),
		},
		{
			name: "live drags are inert",
			body: `{"terminal": {"mouse_drag_selects": false}}`,
			want: withTerminal(func(c *TerminalConfig) { c.MouseDragSelects = false }),
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

// TestTerminalPassCtrlVWithoutText checks the Ctrl+V setting Fresh defaults on.
func TestTerminalPassCtrlVWithoutText(t *testing.T) {
	t.Parallel()
	if !DefaultTerminalOptions().PassCtrlVWithoutText {
		t.Fatal("pass_ctrl_v_without_text does not default to true")
	}
	cfg, diags := loadBody(t, `{"terminal": {"pass_ctrl_v_without_text": false}}`)
	if len(diags) != 0 {
		t.Fatalf("diagnostics = %v", diags)
	}
	if cfg.Terminal.PassCtrlVWithoutText {
		t.Fatal("pass_ctrl_v_without_text = false was not applied")
	}
	cfg, diags = loadBody(t, `{"terminal": {"pass_ctrl_v_without_text": true}}`)
	if len(diags) != 0 || !cfg.Terminal.PassCtrlVWithoutText {
		t.Fatalf("pass_ctrl_v_without_text = true: %v, %+v", diags, cfg.Terminal)
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
		{"jump on output that is not a boolean", `{"terminal": {"jump_to_end_on_output": "no"}}`, "terminal.jump_to_end_on_output"},
		{"drag selects that is not a boolean", `{"terminal": {"mouse_drag_selects": 0}}`, "terminal.mouse_drag_selects"},
		{"pass ctrl-v that is not a boolean", `{"terminal": {"pass_ctrl_v_without_text": "no"}}`, "terminal.pass_ctrl_v_without_text"},
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

// TestTerminalResumeAgents checks terminal.resume_agents, Fresh's default-on gate
// for rejoining an agent session on restart.
func TestTerminalResumeAgents(t *testing.T) {
	t.Parallel()
	if !DefaultTerminalOptions().ResumeAgents {
		t.Fatal("resume_agents does not default to true")
	}
	cfg, diags := loadBody(t, `{"terminal": {"resume_agents": false}}`)
	if len(diags) != 0 || cfg.Terminal.ResumeAgents {
		t.Fatalf("resume_agents = %v, diags %v", cfg.Terminal.ResumeAgents, diags)
	}
	if _, diags := loadBody(t, `{"terminal": {"resume_agents": "no"}}`); len(diags) != 1 || diags[0].Key != "terminal.resume_agents" {
		t.Fatalf("invalid resume_agents diagnostics = %v", diags)
	}
}

// TestTerminalAgents checks gvim's terminal.agents list and its bounds.
func TestTerminalAgents(t *testing.T) {
	t.Parallel()
	cfg, diags := loadBody(t, `{"terminal": {"agents": [
		{"name": "claude", "command": "claude", "args": ["--permission-mode", "plan"], "resume_args": ["--continue"]},
		{"name": "codex", "command": "/usr/local/bin/codex"}
	]}}`)
	if len(diags) != 0 {
		t.Fatalf("diagnostics = %v", diags)
	}
	if len(cfg.Terminal.Agents) != 2 || cfg.Terminal.Agents[0].Name != "claude" ||
		cfg.Terminal.Agents[0].Command != "claude" ||
		!reflect.DeepEqual(cfg.Terminal.Agents[0].Args, []string{"--permission-mode", "plan"}) ||
		!reflect.DeepEqual(cfg.Terminal.Agents[0].ResumeArgs, []string{"--continue"}) {
		t.Fatalf("agents = %+v", cfg.Terminal.Agents)
	}
	if cfg.Terminal.Agents[1].Command != "/usr/local/bin/codex" {
		t.Fatalf("second agent = %+v", cfg.Terminal.Agents[1])
	}
	tests := []struct {
		name string
		body string
	}{
		{"an agent without a name", `{"terminal": {"agents": [{"command": "x"}]}}`},
		{"an agent without a command", `{"terminal": {"agents": [{"name": "x"}]}}`},
		{"a duplicate name", `{"terminal": {"agents": [{"name": "x", "command": "a"}, {"name": "x", "command": "b"}]}}`},
		{"an agent that is not an object", `{"terminal": {"agents": ["x"]}}`},
		{"agents that are not an array", `{"terminal": {"agents": {"name": "x"}}}`},
		{"a command with a NUL byte", `{"terminal": {"agents": [{"name": "x", "command": "a\u0000b"}]}}`},
	}
	for _, tt := range tests {
		if _, diags := loadBody(t, tt.body); len(diags) != 1 || diags[0].Key != "terminal.agents" {
			t.Errorf("%s: diagnostics = %v, want one for agents", tt.name, diags)
		}
	}
}

// TestTerminalAgentsProjectLayerRefused checks that a repository cannot choose the
// programs gvim runs as agents (the terminal.shell rule).
func TestTerminalAgentsProjectLayerRefused(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, ".gvim", "config.json"), `{"terminal": {"agents": [{"name": "x", "command": "x"}]}}`)
	cfg, diags, err := Load(Options{Root: root, UserFile: filepath.Join(t.TempDir(), "absent.json")})
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Terminal.Agents) != 0 {
		t.Fatalf("project agents applied: %+v", cfg.Terminal.Agents)
	}
	found := false
	for _, d := range diags {
		if d.Key == "terminal.agents" && strings.Contains(d.Message, "project config cannot set terminal.agents") {
			found = true
		}
	}
	if !found {
		t.Fatalf("diagnostics = %v, want the project agents refusal", diags)
	}
}

// TestTerminalRestoreLive checks terminal.restore_live, the gvim extension that
// gates reviving a live-at-capture tab on the user's first dive after a restore.
// It defaults to true (Fresh restores live terminals Live) and is user-layer only:
// a repository must not make gvim start processes on restore.
func TestTerminalRestoreLive(t *testing.T) {
	t.Parallel()
	if !DefaultTerminalOptions().RestoreLive {
		t.Fatal("restore_live does not default to true")
	}
	cfg, diags := loadBody(t, `{"terminal": {"restore_live": false}}`)
	if len(diags) != 0 || cfg.Terminal.RestoreLive {
		t.Fatalf("restore_live = %v, diags %v", cfg.Terminal.RestoreLive, diags)
	}
	if _, diags := loadBody(t, `{"terminal": {"restore_live": "yes"}}`); len(diags) != 1 || diags[0].Key != "terminal.restore_live" {
		t.Fatalf("invalid restore_live diagnostics = %v", diags)
	}
	// The project layer cannot turn process-starting on restore on, but the user
	// layer can turn it off.
	cfg, diags = loadLayers(t, `{"terminal": {"restore_live": false}}`, `{"terminal": {"restore_live": true}}`)
	if len(diags) != 1 || !strings.Contains(diags[0].Key, "terminal.restore_live") || !strings.Contains(diags[0].Message, "project config cannot set terminal.restore_live") {
		t.Fatalf("project restore_live diagnostics = %v", diags)
	}
	if cfg.Terminal.RestoreLive {
		t.Fatal("the project layer's restore_live reached the config")
	}
}
