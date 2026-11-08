package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
)

// Values of terminal.mouse_forwarding, Fresh's names for when the mouse reaches
// the program inside a terminal. MouseForwardRequested is Fresh's "requested"
// (the default), MouseForwardAltScreen its "alt_screen".
const (
	MouseForwardRequested = "requested"
	MouseForwardAltScreen = "alt_screen"
)

// TerminalScrollbackDefault is terminal.scrollback_lines when no file sets it:
// gvim keeps the same history x/vt does by default.
const TerminalScrollbackDefault = 10000

// TerminalScrollbackMax bounds terminal.scrollback_lines. A larger history costs
// memory in the emulator with no way to read it faster.
const TerminalScrollbackMax = 1000000

// TerminalShell is terminal.shell: the program a terminal starts instead of the
// user's shell. An empty Command means $SHELL, and then /bin/sh.
type TerminalShell struct {
	// Command is the program, looked up on PATH when it is not absolute.
	Command string
	// Args are the arguments passed to Command.
	Args []string
}

// TerminalConfig is the terminal section of the config: Fresh's terminal keys
// that gvim uses, plus gvim's scrollback_lines. The other Fresh terminal keys
// (jump_to_end_on_output, resume_agents, mouse_drag_selects,
// pass_ctrl_v_without_text, skip_app_execution_alias) are accepted and ignored:
// the features they tune are not built yet, and a Fresh config must load without
// diagnostics.
type TerminalConfig struct {
	// Shell is the terminal.shell override. An empty Command keeps the behaviour
	// of always starting $SHELL.
	Shell TerminalShell
	// CloseOnExit is terminal.close_on_exit: close the terminal dock when its
	// program exits instead of keeping the last screen as read-only scrollback.
	CloseOnExit bool
	// ScrollbackLines is gvim's terminal.scrollback_lines (not a Fresh key): the
	// rows of history the emulator keeps. Zero disables the history.
	ScrollbackLines int
	// MouseForwarding is terminal.mouse_forwarding: "requested" (default) sends
	// mouse events only to programs that asked for them, "alt_screen" sends them
	// to every alternate-screen program.
	MouseForwarding string
}

// DefaultTerminalOptions returns the terminal defaults: the user's shell, the
// dock kept after exit, ten thousand scrollback lines, mouse forwarding on
// request.
func DefaultTerminalOptions() TerminalConfig {
	return TerminalConfig{
		CloseOnExit:     false,
		ScrollbackLines: TerminalScrollbackDefault,
		MouseForwarding: MouseForwardRequested,
	}
}

// terminalKeysIgnored are Fresh's terminal keys that gvim accepts and ignores.
var terminalKeysIgnored = []string{
	"jump_to_end_on_output", "resume_agents", "mouse_drag_selects",
	"pass_ctrl_v_without_text", "skip_app_execution_alias",
}

// setTerminal applies the terminal object of one config layer; path names the
// file it came from, for diagnostics. The project layer may tune the dock's
// behaviour but may not set terminal.shell: a repository must not choose the
// program gvim executes (the same rule as lsp).
func (c *Config) setTerminal(raw json.RawMessage, l layer, path string) (diags []Diagnostic) {
	diag := func(key, message string) {
		diags = append(diags, Diagnostic{Path: path, Key: "terminal." + key, Message: message})
	}
	entries, err := decodeObject(raw)
	if err != nil {
		return []Diagnostic{{Path: path, Key: "terminal", Message: err.Error()}}
	}
	for _, k := range sortedKeys(entries) {
		v := entries[k]
		if isNull(v) {
			diag(k, "null is not a valid value")
			continue
		}
		switch k {
		case "shell":
			if l == layerProject {
				diag("shell", "project config cannot set terminal.shell: a repository must not choose the program gvim executes")
				continue
			}
			shell, err := decodeTerminalShell(v)
			if err != nil {
				diag("shell", err.Error())
				continue
			}
			c.Terminal.Shell = shell
		case "close_on_exit":
			var b bool
			if err := json.Unmarshal(v, &b); err != nil {
				diag("close_on_exit", "must be true or false")
				continue
			}
			c.Terminal.CloseOnExit = b
		case "scrollback_lines":
			var n int
			if err := json.Unmarshal(v, &n); err != nil {
				diag("scrollback_lines", "must be an integer")
				continue
			}
			if n < 0 || n > TerminalScrollbackMax {
				diag("scrollback_lines", fmt.Sprintf("must be between 0 and %d, got %d", TerminalScrollbackMax, n))
				continue
			}
			c.Terminal.ScrollbackLines = n
		case "mouse_forwarding":
			var s string
			if err := json.Unmarshal(v, &s); err != nil {
				diag("mouse_forwarding", "must be a string")
				continue
			}
			if !slices.Contains([]string{MouseForwardRequested, MouseForwardAltScreen}, s) {
				diag("mouse_forwarding", fmt.Sprintf("must be %q or %q, got %q", MouseForwardRequested, MouseForwardAltScreen, s))
				continue
			}
			c.Terminal.MouseForwarding = s
		default:
			if slices.Contains(terminalKeysIgnored, k) {
				continue // a Fresh terminal key gvim accepts and ignores
			}
			diag(k, "unknown setting")
		}
	}
	return diags
}

// decodeTerminalShell reads a terminal.shell object: a command and its
// arguments. An empty command is allowed and means the user's shell.
func decodeTerminalShell(raw json.RawMessage) (TerminalShell, error) {
	var v struct {
		Command *string   `json:"command"`
		Args    *[]string `json:"args"`
	}
	if trimmed := bytes.TrimSpace(raw); len(trimmed) == 0 || trimmed[0] != '{' {
		return TerminalShell{}, errors.New("must be an object with a command and args")
	}
	if err := json.Unmarshal(raw, &v); err != nil {
		return TerminalShell{}, errors.New("must be an object with a command and args")
	}
	out := TerminalShell{}
	if v.Command != nil {
		out.Command = strings.TrimSpace(*v.Command)
	}
	if v.Args != nil {
		out.Args = slices.Clone(*v.Args)
	}
	return out, nil
}

// TerminalShellOptions returns the shell command and arguments a terminal
// starts: the configured override, or empty for the user's shell.
func (t TerminalConfig) TerminalShellOptions() (command string, args []string) {
	if t.Shell.Command == "" {
		return "", nil
	}
	return t.Shell.Command, slices.Clone(t.Shell.Args)
}
