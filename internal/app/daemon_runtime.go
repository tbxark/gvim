package app

// Daemon launch helpers: attaching this terminal to a running daemon, and the
// single-writer refusal of a bare launch while a daemon hosts the workspace.

import (
	"context"
	"fmt"

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
	files := make([]string, 0, len(opts.Files))
	for _, f := range opts.Files {
		files = append(files, f.Path)
	}
	return daemon.AttachClient(ctx, name, files)
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

// refuseIfDaemonRunning keeps the single-writer rule for a bare (session) launch: when
// a daemon already hosts this workspace's editor, a second direct editor would write the
// same session. File launches, --simple and a stdin launch are direct file launches and
// stay allowed; the user attaches with -a for the daemon.
func refuseIfDaemonRunning(opts Options) error {
	if opts.Daemon != nil || opts.Simple || opts.Diff || opts.StdinLaunch() || len(opts.Files) > 0 {
		return nil
	}
	root, err := resolveRoot(opts.Root)
	if err != nil {
		// buildRuntime reports the root error with its own wording.
		return nil
	}
	path, err := daemon.SocketPath(daemon.DefaultName(root))
	if err != nil {
		return nil
	}
	if daemon.IsLive(path) {
		return fmt.Errorf("a daemon is already running for this workspace (socket %s); attach with gvim -a or quit the daemon first", path)
	}
	return nil
}
