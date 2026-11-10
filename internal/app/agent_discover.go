package app

import (
	"context"
	"os"

	"github.com/tbxark/gvim/config"
	"github.com/tbxark/gvim/internal/agentdiscover"
	"github.com/tbxark/gvim/pluginapi"
	"github.com/tbxark/gvim/trust"
)

// agentDiscoverer is the app's agent-discovery port: it scans for tmux sessions
// off the update loop and applies the workspace trust spawn gate to every tmux
// command the scan wants to run. Discovery is read-only (list-sessions and
// list-panes, no shell), but it is still a spawn, so it obeys the same gate as
// the terminal and plugins: Trusted runs it, Blocked refuses it, Restricted only
// refuses an executable that resolves inside the workspace (tmux is a bare name
// resolved through the user's PATH, so it normally runs). A refusal becomes a
// scan problem, which the picker shows with the socket-only rows.
type agentDiscoverer struct {
	root  func() string
	gate  func(context.Context, []string, string, map[string]string) error
	known []string // pane command basenames of the configured terminal.agents
}

// newAgentDiscoverer returns the discovery port over the live workspace root and
// the session's spawn gate (the same one the terminal and plugins consult). root
// reports the current project root, not the launch directory, so a project switch
// is seen by the next scan.
func newAgentDiscoverer(root func() string, gate func(context.Context, []string, string, map[string]string) error, agents []config.TerminalAgent) *agentDiscoverer {
	known := make([]string, 0, len(agents))
	for _, agent := range agents {
		if agent.Command != "" {
			known = append(known, agent.Command)
		}
	}
	return &agentDiscoverer{root: root, gate: gate, known: known}
}

// Discover runs one scan, in the caller's command goroutine.
func (d *agentDiscoverer) Discover(ctx context.Context) agentdiscover.Report {
	report := agentdiscover.Scan(ctx, agentdiscover.Config{
		Runner:        gatedScanRunner{d: d},
		AllowCommands: true,
		KnownAgents:   d.known,
	})
	// The transcript stores are read-only: no command, no shell, nothing beyond
	// the user's own Claude Code/Codex files. It runs in the same effect command as
	// the tmux scan, never on the update loop.
	report.External = agentdiscover.ScanExternal(ctx, agentdiscover.ExternalOptions{})
	return report
}

// gatedScanRunner runs every tmux command through the session's spawn gate and
// then ExecRunner. The gate is the same closure the terminal and plugins use, so
// discovery cannot bypass the trust rule (Blocked denies, Restricted only refuses
// a project-resolved executable). No child environment is supplied: discovery must
// resolve tmux through the inherited PATH, not a project environment overlay.
type gatedScanRunner struct {
	d *agentDiscoverer
}

// Run implements agentdiscover.Runner.
func (g gatedScanRunner) Run(ctx context.Context, name string, args ...string) agentdiscover.Result {
	root := g.d.root()
	argv := append([]string{name}, args...)
	if err := g.d.gate(ctx, argv, root, nil); err != nil {
		return agentdiscover.Result{Code: -1, Err: err}
	}
	// The gate resolved the command against the workspace root with the inherited
	// PATH. Resolve it once more the same way and execute that absolute path:
	// os/exec's LookPath runs in the parent process before cmd.Dir applies, so
	// letting exec resolve a relative PATH entry would run a different executable
	// than the gate judged (the editor's cwd instead of the workspace root).
	return agentdiscover.ExecRunner{
		Dir: root,
		Resolve: func(name string) (string, bool) {
			return trust.ResolveSpawnPath(root, name, os.Getenv("PATH"))
		},
	}.Run(ctx, name, args...)
}

// pluginAgentReport flattens one native discovery report into the plugin API shape. The
// scan has already run off the update loop, with the same trust gate; this conversion only
// copies bounded fields and never exposes a resume/attach argv or a process handle.
func pluginAgentReport(report agentdiscover.Report) pluginapi.AgentReport {
	out := pluginapi.AgentReport{
		Supported: true,
		Installed: report.Installed || report.External.Installed,
		Truncated: report.Truncated || report.External.Truncated,
		Problems:  append(append([]string(nil), report.Problems...), report.External.Problems...),
		Sessions:  make([]pluginapi.AgentSession, 0, len(report.Sessions)+len(report.External.Sessions)),
	}
	for _, s := range report.Sessions {
		out.Sessions = append(out.Sessions, pluginapi.AgentSession{
			Kind: "tmux", Tool: "tmux", ID: s.ID, Title: s.Name,
			Path: s.Path, Dir: s.Path, Agents: append([]string(nil), s.Agents...),
			Attached: s.Attached, SocketOnly: s.SocketOnly,
		})
	}
	for _, s := range report.External.Sessions {
		out.Sessions = append(out.Sessions, pluginapi.AgentSession{
			Kind: "history", Tool: s.Tool, ID: s.ID, Title: s.Title,
			Path: s.Path, Dir: s.CWD, Agent: s.Agent, Exact: s.Exact,
			ModTime: s.ModTime.Unix(),
		})
	}
	return out
}
