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
// that gvim uses, gvim's scrollback_lines, and gvim's agents list (a gvim
// extension). Fresh's skip_app_execution_alias is accepted and ignored.
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
	// JumpToEndOnOutput is terminal.jump_to_end_on_output: new output while the
	// dock is in scrollback returns to the live screen, unless a selection is
	// active. Default true, as Fresh's is.
	JumpToEndOnOutput bool
	// MouseDragSelects is terminal.mouse_drag_selects: a drag on the live grid
	// selects text by dropping into scrollback. False makes live-grid drags
	// inert; scrollback drags still select. Default true, as Fresh's is.
	MouseDragSelects bool
	// PassCtrlVWithoutText is terminal.pass_ctrl_v_without_text: when the system
	// clipboard can be read but holds no text (an image, say), Ctrl+V goes to the
	// shell as the key itself instead of pasting the last yank, so an agent CLI
	// can read the clipboard itself. False pastes the last yank instead. A
	// clipboard that cannot be read at all pastes the last yank either way.
	// Default true, as Fresh's is.
	PassCtrlVWithoutText bool
	// ResumeAgents is terminal.resume_agents: a restart of an exited agent
	// terminal re-runs the agent's resume argv instead of its launch argv. Default
	// true, as Fresh's is.
	ResumeAgents bool
	// Agents is gvim's terminal.agents list: the agent presets the Run Agent
	// command offers and the metadata that turns a plain launch into a resumable
	// agent session. It is a gvim extension: Fresh's agent data comes from plugins.
	Agents []TerminalAgent
}

// TerminalAgent is one terminal.agents entry: a named agent command.
type TerminalAgent struct {
	// Name is the id the Run Agent picker shows and the record stores.
	Name string
	// Command is the program to run, looked up on PATH when it is not absolute.
	Command string
	// Args are the arguments of a new session.
	Args []string
	// ResumeArgs are the arguments that rejoin a previous session (for example
	// ["--continue"] or ["--resume", "id"]); Run Agent and a restart with
	// terminal.resume_agents on run Command + ResumeArgs.
	ResumeArgs []string
}

// Bounds of the agent list and its strings, so a config cannot hand the dock an
// unbounded command.
const (
	TerminalAgentsMax    = 32
	TerminalAgentTextMax = 1024
)

// DefaultTerminalOptions returns the terminal defaults: the user's shell, the
// dock kept after exit, ten thousand scrollback lines, mouse forwarding on
// request.
func DefaultTerminalOptions() TerminalConfig {
	return TerminalConfig{
		CloseOnExit:          false,
		ScrollbackLines:      TerminalScrollbackDefault,
		MouseForwarding:      MouseForwardRequested,
		JumpToEndOnOutput:    true,
		MouseDragSelects:     true,
		PassCtrlVWithoutText: true,
		ResumeAgents:         true,
	}
}

// terminalKeysIgnored are Fresh's terminal keys that gvim accepts and ignores.
var terminalKeysIgnored = []string{
	"skip_app_execution_alias",
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
		case "jump_to_end_on_output":
			var b bool
			if err := json.Unmarshal(v, &b); err != nil {
				diag(k, "must be true or false")
				continue
			}
			c.Terminal.JumpToEndOnOutput = b
		case "mouse_drag_selects":
			var b bool
			if err := json.Unmarshal(v, &b); err != nil {
				diag(k, "must be true or false")
				continue
			}
			c.Terminal.MouseDragSelects = b
		case "pass_ctrl_v_without_text":
			var b bool
			if err := json.Unmarshal(v, &b); err != nil {
				diag(k, "must be true or false")
				continue
			}
			c.Terminal.PassCtrlVWithoutText = b
		case "resume_agents":
			var b bool
			if err := json.Unmarshal(v, &b); err != nil {
				diag(k, "must be true or false")
				continue
			}
			c.Terminal.ResumeAgents = b
		case "agents":
			if l == layerProject {
				diag("agents", "project config cannot set terminal.agents: a repository must not choose programs gvim runs")
				continue
			}
			agents, err := decodeTerminalAgents(v)
			if err != nil {
				diag("agents", err.Error())
				continue
			}
			c.Terminal.Agents = agents
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

// decodeTerminalAgents reads terminal.agents: an array of named agent commands.
// Names must be unique and non-empty; the command and every argument are bounded.
func decodeTerminalAgents(raw json.RawMessage) ([]TerminalAgent, error) {
	var items []json.RawMessage
	if err := json.Unmarshal(raw, &items); err != nil {
		return nil, errors.New("must be an array of agent objects")
	}
	if len(items) > TerminalAgentsMax {
		return nil, fmt.Errorf("takes at most %d agents, got %d", TerminalAgentsMax, len(items))
	}
	out := make([]TerminalAgent, 0, len(items))
	seen := make(map[string]bool, len(items))
	for i, item := range items {
		var v struct {
			Name       *string   `json:"name"`
			Command    *string   `json:"command"`
			Args       *[]string `json:"args"`
			ResumeArgs *[]string `json:"resume_args"`
		}
		if trimmed := bytes.TrimSpace(item); len(trimmed) == 0 || trimmed[0] != '{' {
			return nil, fmt.Errorf("agent %d must be an object", i+1)
		}
		if err := json.Unmarshal(item, &v); err != nil {
			return nil, fmt.Errorf("agent %d must be an object", i+1)
		}
		name := ""
		if v.Name != nil {
			name = strings.TrimSpace(*v.Name)
		}
		if name == "" {
			return nil, fmt.Errorf("agent %d has no name", i+1)
		}
		if len(name) > 64 {
			return nil, fmt.Errorf("agent name %q is longer than 64 characters", name)
		}
		if seen[name] {
			return nil, fmt.Errorf("agent name %q is defined twice", name)
		}
		seen[name] = true
		command := ""
		if v.Command != nil {
			command = strings.TrimSpace(*v.Command)
		}
		if command == "" {
			return nil, fmt.Errorf("agent %q has no command", name)
		}
		if err := checkAgentText("agent command", command); err != nil {
			return nil, err
		}
		agent := TerminalAgent{Name: name, Command: command}
		if v.Args != nil {
			if len(*v.Args) > 32 {
				return nil, fmt.Errorf("agent %q takes at most 32 args", name)
			}
			agent.Args = slices.Clone(*v.Args)
			for _, a := range agent.Args {
				if err := checkAgentText("agent arg", a); err != nil {
					return nil, err
				}
			}
		}
		if v.ResumeArgs != nil {
			if len(*v.ResumeArgs) > 32 {
				return nil, fmt.Errorf("agent %q takes at most 32 resume_args", name)
			}
			agent.ResumeArgs = slices.Clone(*v.ResumeArgs)
			for _, a := range agent.ResumeArgs {
				if err := checkAgentText("agent resume arg", a); err != nil {
					return nil, err
				}
			}
		}
		out = append(out, agent)
	}
	return out, nil
}

// checkAgentText rejects an agent string that cannot be passed as one argv
// element: empty after trimming, too long, or holding a NUL, CR or newline.
func checkAgentText(what, text string) error {
	if strings.TrimSpace(text) == "" {
		return fmt.Errorf("%s must not be empty", what)
	}
	if len(text) > TerminalAgentTextMax {
		return fmt.Errorf("%s is longer than %d bytes", what, TerminalAgentTextMax)
	}
	if strings.IndexByte(text, 0) >= 0 || strings.ContainsAny(text, "\n\r") {
		return fmt.Errorf("%s must be one line without NUL bytes", what)
	}
	return nil
}

// TerminalShellOptions returns the shell command and arguments a terminal
// starts: the configured override, or empty for the user's shell.
func (t TerminalConfig) TerminalShellOptions() (command string, args []string) {
	if t.Shell.Command == "" {
		return "", nil
	}
	return t.Shell.Command, slices.Clone(t.Shell.Args)
}
