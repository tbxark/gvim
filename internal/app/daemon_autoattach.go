package app

// Auto attach (task-146): a bare launch may join a daemon that already hosts this
// workspace instead of being refused as a second writer. The knob is
// config.auto_attach, default off (Fresh defaults it on; gvim deliberately keeps the
// explicit -a/--attach surface the default). No daemon is started when none is running:
// this is only "attach if already there". The decision reads the user config only
// (a project config cannot switch it on) and runs before the terminal check, because the
// attach path is a terminal proxy.

import (
	"context"
	"fmt"
	"os"

	"github.com/charmbracelet/x/term"

	"github.com/tbxark/gvim/config"
	"github.com/tbxark/gvim/internal/daemon"
)

// autoAttachEligible reports whether the launch is the shape auto_attach applies to: no
// explicit daemon mode, no files, no direct-mode flags and a terminal on stdin. Any flag
// that selects a direct editor keeps its old behaviour.
func autoAttachEligible(opts Options) bool {
	if opts.Daemon != nil || opts.Remote != nil {
		return false
	}
	if len(opts.Files) > 0 || opts.Diff || opts.Simple || opts.ReadStdin || opts.Restore || opts.NoRestore {
		return false
	}
	return true
}

// autoAttachUserConfig loads only the user layer for the auto_attach preference. Any
// error or diagnostic leaves the preference off (the default); it never fails a launch.
func autoAttachUserConfig(opts Options) config.Config {
	cfg, _, err := config.Load(config.Options{UserFile: opts.Config})
	if err != nil {
		return config.Defaults()
	}
	return cfg
}

// autoAttachPlan decides whether a bare launch should join an existing daemon. It returns
// the daemon name and the notice to print when it should. It never dials.
func autoAttachPlan(opts Options) (name, notice string, ok bool) {
	if cfg := autoAttachUserConfig(opts); !cfg.AutoAttach {
		return "", "", false
	}
	root, err := resolveRoot(opts.Root)
	if err != nil {
		return "", "", false
	}
	canonical := root
	if resolved, cerr := daemon.CanonicalRoot(root); cerr == nil {
		canonical = resolved
	}
	hosts := liveHostsFor(canonical)
	if len(hosts) == 0 {
		return "", "", false
	}
	// Prefer this root's default daemon; a single explicitly named daemon is unambiguous.
	// Several named daemons for one root and no default: the launch keeps the refusal
	// (never guess which session to join).
	defaultName := daemon.DefaultName(root)
	name = ""
	for _, h := range hosts {
		if h.Name == defaultName {
			name = h.Name
			break
		}
	}
	if name == "" {
		if len(hosts) != 1 {
			return "", "", false
		}
		name = hosts[0].Name
	}
	notice = fmt.Sprintf("gvim: auto-attach: daemon %q hosts this workspace; attaching (turn it off with \"auto_attach\": false)", name)
	return name, notice, true
}

// maybeAutoAttach performs the auto attach when autoAttachPlan says so. The caller returns
// its result directly: the attach proxy owns the terminal and no editor/session is built.
func maybeAutoAttach(ctx context.Context, opts Options) (attached bool, err error) {
	if !autoAttachEligible(opts) || !term.IsTerminal(os.Stdin.Fd()) {
		return false, nil
	}
	name, notice, ok := autoAttachPlan(opts)
	if !ok {
		return false, nil
	}
	_, _ = fmt.Fprintln(os.Stderr, notice)
	attachOpts := opts
	attachOpts.Daemon = &DaemonOptions{Mode: DaemonAttach, Name: name}
	return true, attachToDaemon(ctx, attachOpts)
}
