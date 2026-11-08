package app

// Daemon launch helpers: attaching this terminal to a running daemon, and the
// single-writer refusal of a bare launch while a daemon hosts the workspace.

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/tbxark/gvim/internal/daemon"
)

// attachToDaemon forwards this terminal to the daemon of the workspace (or the named
// one) and opens the named files in the hosted editor. It returns when the client
// detaches, the daemon exits or the connection drops.
func attachToDaemon(ctx context.Context, opts Options) error {
	name, err := attachDaemonName(opts)
	if err != nil {
		return err
	}
	targets := make([]daemon.OpenFileTarget, 0, len(opts.Files))
	for _, f := range opts.Files {
		targets = append(targets, daemon.OpenFileTarget{Path: f.Path, Line: f.Line, Col: f.Col})
	}
	cwd, err := os.Getwd()
	if err != nil {
		cwd = ""
	}
	return daemon.AttachClientTargets(ctx, name, cwd, targets)
}

// attachDaemonName uses the same resolved root as daemon startup. An explicit
// name does not depend on the client workspace.
func attachDaemonName(opts Options) (string, error) {
	if opts.Daemon != nil && opts.Daemon.Name != "" {
		return opts.Daemon.Name, nil
	}
	root, err := resolveRoot(opts.Root)
	if err != nil {
		return "", err
	}
	return daemon.DefaultName(root), nil
}

// refuseIfDaemonRunning keeps the single-writer rule for a direct launch. While a daemon
// (the per-root default or any explicitly named one, discovered through the daemon-host
// marks) hosts this workspace, EVERY direct editor mode is refused: bare, file launches,
// --diff, --simple and stdin. Fresh can let a direct launch through safely because it
// enforces one buffer per file across editors (a second editor gets a read-only buffer);
// gvim has no cross-process per-file ownership, so a direct file launch could race the
// daemon's unsaved buffer both ways (the direct editor writes the file under the daemon's
// buffer, then the daemon's save overwrites the direct edits). Refusing with an attach
// instruction is the safe behavior until that ownership feature exists; this is a
// deliberate divergence from Fresh (docs/architecture.md §8).
func refuseIfDaemonRunning(opts Options) error {
	if opts.Daemon != nil {
		// The daemon host itself and the attaching client are not direct editors.
		return nil
	}
	root, err := resolveRoot(opts.Root)
	if err != nil {
		// buildRuntime reports the root error with its own wording.
		return nil
	}
	canonical := root
	if resolved, cerr := daemon.CanonicalRoot(root); cerr == nil {
		canonical = resolved
	}
	// Any live daemon that recorded this root, named or default. The default socket probe
	// stays as the fallback for a daemon whose mark could not be written.
	hosts := liveHostsFor(canonical)
	defaultName := daemon.DefaultName(root)
	if path, perr := daemon.SocketPath(defaultName); perr == nil && daemon.IsLive(path) {
		found := false
		for _, h := range hosts {
			if h.Name == defaultName {
				found = true
				break
			}
		}
		if !found {
			hosts = append([]hostMark{{Name: defaultName, Root: canonical, Socket: path}}, hosts...)
		}
	}
	if len(hosts) == 0 {
		return nil
	}
	return daemonHostsRefusal(hosts, defaultName)
}

// daemonHostsRefusal is the direct launch's error while one or more daemons host the
// workspace: which daemons, why a direct editor is refused, and the two ways out
// (attach with the files, or quit the daemon first).
func daemonHostsRefusal(hosts []hostMark, defaultName string) error {
	var who []string
	var attach []string
	for _, h := range hosts {
		who = append(who, fmt.Sprintf("%q (socket %s)", h.Name, h.Socket))
		if h.Name == defaultName {
			attach = append(attach, "\"gvim -a\" (then the files, e.g. gvim -a FILE...)")
		} else {
			// The optional NAME is only accepted in the = form (pflag NoOptDefVal):
			// "gvim -a NAME" would open a file called NAME, not attach to that daemon.
			attach = append(attach, fmt.Sprintf("\"gvim --attach=%s\" (then the files, e.g. gvim --attach=%s FILE...)", h.Name, h.Name))
		}
	}
	return fmt.Errorf(
		"a daemon hosts this workspace: %s. A direct editor would be a second writer of the daemon's unsaved buffers, so the launch is refused; attach with %s, or quit the daemon first (attach, then Ctrl+Q)",
		strings.Join(who, ", "), strings.Join(attach, " or "))
}
