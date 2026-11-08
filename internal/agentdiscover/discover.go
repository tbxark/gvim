// Package agentdiscover finds terminal sessions that already exist outside gvim,
// so the editor can offer to rejoin them. The first scanner is tmux, level 0 in
// Fresh's terms (docs/internal/tmux-native-sessions.md): it finds a running tmux
// server's sessions, reports their working directory, attachment state and
// probable agent panes, and builds the attach argv qualified by the server's
// socket. Importing a session as a workspace or a dock row is a later layer; this
// package never runs anything for the user and never resumes a session.
//
// Trust: listing sessions runs tmux commands. The caller decides whether that may
// happen (an explicit user command in a trusted workspace) and passes
// AllowCommands; with it off the scan is socket-only, which says a server exists
// but not what it holds. The package itself is TUI-free and runs nothing else: no
// shell, no writes, only tmux list-sessions/list-panes through the injected
// Runner.
package agentdiscover

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

// Defaults bound one scan, so a huge or wedged tmux server cannot hold the UI or
// allocate without end.
const (
	defaultCommandTimeout  = 2 * time.Second
	defaultMaxCommandBytes = 256 << 10
	defaultMaxSessions     = 200
	defaultMaxPanes        = 2000
	defaultMaxSockets      = 500
	defaultMaxSocketDirs   = 2000
	defaultMaxProblems     = 20
	maxProblemText         = 512
)

// Config is one scan's inputs.
type Config struct {
	// TmpDir is where tmux keeps its socket directories; empty means
	// $TMUX_TMPDIR, then /tmp.
	TmpDir string
	// Getenv reads TMUX_TMPDIR; nil means os.Getenv.
	Getenv func(string) string
	// Runner runs tmux; nil means an ExecRunner with CommandTimeout.
	Runner Runner
	// AllowCommands permits the tmux list-sessions/list-panes calls. The caller
	// gates it on trust and on this being an explicit user scan; false gives
	// socket-only findings.
	AllowCommands bool
	// KnownAgents are extra pane command basenames reported as probable agents
	// (the config's terminal.agents commands, for example). The built-in set is
	// Fresh's (claude, codex, ...).
	KnownAgents []string
	// CommandTimeout bounds one tmux command; zero means 2s.
	CommandTimeout time.Duration
	// MaxSessions, MaxPanes, MaxSockets and MaxSocketDirs bound the scan; zero
	// means the package defaults.
	MaxSessions, MaxPanes, MaxSockets, MaxSocketDirs int
}

// Session is one discovered sessions. SocketOnly rows come from a socket whose
// server was not queried (commands off) or is gone: the session list is unknown.
type Session struct {
	Socket  string // the server socket path
	Server  string // the socket's base name, unique per server
	ID      string // "<server>/<name>" for live sessions, the socket path otherwise
	Name    string
	Path    string // the session's session_path, or a pane's path when that is empty
	Created int64
	// Attached says a tmux client is attached to the session. Panes is how many
	// panes it has. Agents lists the foreground command names that look like
	// agents: a name match, not an identity.
	Attached bool
	Panes    int
	Agents   []string
	// AttachArgv opens the session: tmux -S <socket> attach -t <name>. It is
	// qualified by the socket because a bare "tmux attach" cannot see a server on
	// a non-default socket.
	AttachArgv []string
	SocketOnly bool
}

// Report is one scan's result.
type Report struct {
	// Installed says a tmux server (at least one socket) was found.
	Installed bool
	// AllowCommands is whether the scan was allowed to query the servers.
	AllowCommands bool
	Sessions      []Session
	// Problems are bounded descriptions of servers that could not be queried.
	Problems []string
	// Truncated says a scan bound stopped the walk.
	Truncated bool
}

// agentSet returns the probable-agent command names: the built-in set plus the
// config's extra basenames.
func agentSet(extra []string) map[string]bool {
	out := make(map[string]bool, len(agentCommands)+len(extra))
	for name := range agentCommands {
		out[name] = true
	}
	for _, name := range extra {
		if base := baseName(strings.TrimSpace(name)); base != "" {
			out[base] = true
		}
	}
	return out
}

// Scan finds the tmux sessions of the local machine. It never returns an error: a
// missing tmux, a stale socket or a command failure is a bounded Problem (or an
// Installed false), so the caller can always show a picker with a stated reason.
func Scan(ctx context.Context, cfg Config) Report {
	getenv := cfg.Getenv
	if getenv == nil {
		getenv = os.Getenv
	}
	runner := cfg.Runner
	if runner == nil {
		runner = ExecRunner{Timeout: cfg.CommandTimeout}
	}
	maxSessions := cfg.MaxSessions
	if maxSessions <= 0 {
		maxSessions = defaultMaxSessions
	}
	maxPanes := cfg.MaxPanes
	if maxPanes <= 0 {
		maxPanes = defaultMaxPanes
	}
	maxSockets := cfg.MaxSockets
	if maxSockets <= 0 {
		maxSockets = defaultMaxSockets
	}
	maxDirs := cfg.MaxSocketDirs
	if maxDirs <= 0 {
		maxDirs = defaultMaxSocketDirs
	}
	report := Report{AllowCommands: cfg.AllowCommands}
	sockets, installed, truncated := socketPaths(cfg.TmpDir, getenv, maxDirs, maxSockets)
	report.Installed, report.Truncated = installed, truncated
	if len(sockets) == 0 {
		return report
	}
	agents := agentSet(cfg.KnownAgents)
	problems := newProblems()
	panesSeen := 0
	for _, socket := range sockets {
		if len(report.Sessions) >= maxSessions {
			report.Truncated = true
			break
		}
		server := baseName(socket)
		if !cfg.AllowCommands {
			report.Sessions = append(report.Sessions, socketOnlySession(socket, server))
			continue
		}
		list := runner.Run(ctx, "tmux", "-S", socket, "list-sessions", "-F", sessionFmt)
		if list.Code != 0 || list.Err != nil {
			reason := strings.TrimSpace(list.Stderr)
			if reason == "" && list.Err != nil {
				reason = list.Err.Error()
			}
			if !isNoServer(reason) {
				problems.add("tmux -S " + socket + " list-sessions: " + reasonOrCode(reason, list.Code))
			}
			report.Sessions = append(report.Sessions, socketOnlySession(socket, server))
			continue
		}
		rows := parseSessions(list.Stdout)
		if len(rows) == 0 {
			// A live server with no sessions (all just killed): nothing to offer.
			continue
		}
		// Ask the same server for its panes; a failure only costs the agent
		// hints and the pane counts, not the session rows.
		var panes []paneRow
		panesOut := runner.Run(ctx, "tmux", "-S", socket, "list-panes", "-a", "-F", paneFmt)
		if panesOut.Code != 0 || panesOut.Err != nil {
			problems.add("tmux -S " + socket + " list-panes: " + reasonOrCode(strings.TrimSpace(panesOut.Stderr), panesOut.Code))
		} else {
			panes = parsePanes(panesOut.Stdout)
		}
		bySession := make(map[string]*Session, len(rows))
		for _, row := range rows {
			name := row.Name
			if name == "" {
				name = row.ID
			}
			rec := Session{
				Socket: socket, Server: server, ID: server + "/" + name,
				Name: name, Path: row.Path, Created: row.Created, Attached: row.Attached,
				AttachArgv: []string{"tmux", "-S", socket, "attach", "-t", name},
			}
			bySession[row.ID] = &rec
		}
		agentHits := make(map[string]map[string]bool, len(rows))
		for _, pane := range panes {
			if panesSeen >= maxPanes {
				report.Truncated = true
				break
			}
			panesSeen++
			rec := bySession[pane.SessionID]
			if rec == nil {
				continue
			}
			rec.Panes++
			if rec.Path == "" && pane.Path != "" {
				rec.Path = pane.Path
			}
			command := baseName(strings.TrimSpace(pane.Command))
			if command != "" && agents[command] {
				hits := agentHits[pane.SessionID]
				if hits == nil {
					hits = make(map[string]bool, 2)
					agentHits[pane.SessionID] = hits
				}
				hits[command] = true
			}
		}
		for id, rec := range bySession {
			if hits := agentHits[id]; len(hits) > 0 {
				names := make([]string, 0, len(hits))
				for name := range hits {
					names = append(names, name)
				}
				sort.Strings(names)
				rec.Agents = names
			}
		}
		ids := make([]string, 0, len(bySession))
		for id := range bySession {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		for _, id := range ids {
			if len(report.Sessions) >= maxSessions {
				report.Truncated = true
				break
			}
			report.Sessions = append(report.Sessions, *bySession[id])
		}
	}
	report.Problems = problems.list()
	return report
}

// socketOnlySession is the fallback row of a socket whose server was not asked.
func socketOnlySession(socket, server string) Session {
	return Session{
		Socket: socket, Server: server, ID: socket, Name: server,
		AttachArgv: []string{"tmux", "-S", socket, "attach"}, SocketOnly: true,
	}
}

// socketPaths walks the tmux socket directories under tmpDir (or $TMUX_TMPDIR,
// then /tmp) and returns the sockets. tmux-<uid> directories are found by
// listing rather than by uid: the directory is mode 0700, so this sees only what
// it may read.
func socketPaths(tmpDir string, getenv func(string) string, maxDirs, maxSockets int) (sockets []string, installed, truncated bool) {
	if tmpDir == "" {
		tmpDir = getenv("TMUX_TMPDIR")
		if tmpDir == "" {
			tmpDir = "/tmp"
		}
	}
	entries, err := os.ReadDir(tmpDir)
	if err != nil {
		return nil, false, false
	}
	var dirs []string
	for _, entry := range entries {
		if maxDirs > 0 && len(dirs) >= maxDirs {
			truncated = true
			break
		}
		if !entry.IsDir() || !isTmuxDir(entry.Name()) {
			continue
		}
		dirs = append(dirs, filepath.Join(tmpDir, entry.Name()))
	}
	for _, dir := range dirs {
		inner, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, entry := range inner {
			if len(sockets) >= maxSockets {
				truncated = true
				break
			}
			// Everything inside a tmux-<uid> directory is a socket. Some
			// transports report it as a file or a symlink; a directory is not one.
			if entry.IsDir() {
				continue
			}
			sockets = append(sockets, filepath.Join(dir, entry.Name()))
		}
	}
	sort.Strings(sockets)
	return sockets, len(sockets) > 0, truncated
}

// isTmuxDir reports whether name is tmux-<uid>.
func isTmuxDir(name string) bool {
	if !strings.HasPrefix(name, "tmux-") || len(name) == len("tmux-") {
		return false
	}
	for _, r := range name[len("tmux-"):] {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// isNoServer reports a stale socket, which is normal in /tmp and not a problem.
func isNoServer(stderr string) bool {
	lower := strings.ToLower(stderr)
	return strings.Contains(lower, "no server running") || strings.Contains(lower, "error connecting to")
}

// reasonOrCode is the problem text of a failed command.
func reasonOrCode(reason string, code int) string {
	if reason != "" {
		return clipText(reason, maxProblemText)
	}
	return fmt.Sprintf("exit %d", code)
}

// problemList bounds and clips the scan's problem list.
type problemList struct {
	items []string
}

func newProblems() *problemList { return &problemList{items: make([]string, 0, 2)} }

func (p *problemList) add(text string) {
	if len(p.items) >= defaultMaxProblems {
		return
	}
	p.items = append(p.items, clipText(text, maxProblemText))
}

// list returns the bounded problems.
func (p *problemList) list() []string { return p.items }

// clipText bounds one text to max bytes, on a rune boundary: a split rune is
// dropped rather than half-written.
func clipText(text string, max int) string {
	if len(text) <= max {
		return text
	}
	clipped := text[:max]
	for len(clipped) > 0 && !utf8.ValidString(clipped) {
		clipped = clipped[:len(clipped)-1]
	}
	return clipped
}
