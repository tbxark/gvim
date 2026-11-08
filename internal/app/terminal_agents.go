package app

import (
	"github.com/tbxark/gvim/config"
	"github.com/tbxark/gvim/internal/tui/dock"
)

// dockAgents converts the config's terminal.agents list into the dock's port type:
// the name, the new-session argv and the resume argv, each passed to the PTY as
// argv elements (never through a shell).
func dockAgents(cfg config.TerminalConfig) []dock.Agent {
	if len(cfg.Agents) == 0 {
		return nil
	}
	out := make([]dock.Agent, 0, len(cfg.Agents))
	for _, a := range cfg.Agents {
		argv := make([]string, 0, 1+len(a.Args))
		argv = append(argv, a.Command)
		argv = append(argv, a.Args...)
		var resume []string
		if len(a.ResumeArgs) > 0 {
			resume = make([]string, 0, 1+len(a.ResumeArgs))
			resume = append(resume, a.Command)
			resume = append(resume, a.ResumeArgs...)
		}
		out = append(out, dock.Agent{Name: a.Name, Argv: argv, ResumeArgv: resume})
	}
	return out
}
