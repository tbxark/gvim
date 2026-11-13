package terminal

import "unicode/utf8"

// Launch describes the program a dock terminal starts when it is not the plain
// configured shell: an agent session, or a resume of one. The argv is passed to
// the PTY as it is, never through a shell, so an entry is one program and its
// arguments.
//
// A Launch is also the record a terminal keeps so a restart can rejoin where it
// left off: Argv is the launch command, Agent names the configured agent it is
// ("" for a command the user configured without an agent), and ResumeArgv is the
// argv that rejoins the agent's previous session. Ephemeral marks a session
// terminal (an agent started by Run Agent) rather than a user's plain shell.
type Launch struct {
	Argv       []string `json:"argv,omitempty"`
	Agent      string   `json:"agent,omitempty"`
	ResumeArgv []string `json:"resume_argv,omitempty"`
	Ephemeral  bool     `json:"ephemeral,omitempty"`
	// Source is the durable external identity of a discovered session (for example
	// "tmux:default/work"). It lets a repeated import find the tab that already
	// attaches that session instead of opening a duplicate. Empty for a configured
	// agent or shell.
	Source string `json:"source,omitempty"`
}

// Bounds of a Launch, so a saved record or a config cannot hand the dock an
// unbounded command.
const (
	LaunchArgvMax  = 64
	LaunchTextMax  = 1024
	LaunchAgentMax = 64
)

// Clean returns l bounded for a start or a save: argv lists are clipped to
// LaunchArgvMax entries of LaunchTextMax bytes, and the agent name to
// LaunchAgentMax. It never adds data, only drops what is over the bound; a clipped
// string ends at a rune boundary.
func (l Launch) Clean() Launch {
	out := Launch{
		Argv:       cleanArgv(l.Argv),
		ResumeArgv: cleanArgv(l.ResumeArgv),
		Agent:      l.Agent,
		Ephemeral:  l.Ephemeral,
		Source:     l.Source,
	}
	out.Agent = clipBytes(out.Agent, LaunchAgentMax)
	out.Source = clipBytes(out.Source, LaunchTextMax)
	return out
}

// clipBytes cuts s to at most n bytes without splitting a UTF-8 rune.
func clipBytes(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n]
}

// Empty reports whether l is no program at all: the plain shell. A Launch with
// an agent but no argv is not startable and is treated as empty.
func (l Launch) Empty() bool { return len(l.Argv) == 0 }

// cleanArgv clips argv to the bounds of a Launch.
func cleanArgv(argv []string) []string {
	if len(argv) == 0 {
		return nil
	}
	out := make([]string, 0, min(len(argv), LaunchArgvMax))
	for _, a := range argv {
		if len(out) == LaunchArgvMax {
			break
		}
		out = append(out, clipBytes(a, LaunchTextMax))
	}
	return out
}
