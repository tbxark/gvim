package app

import (
	"fmt"
	"os"
	"strings"

	"github.com/tbxark/gvim/commands"
	"github.com/tbxark/gvim/config"
	"github.com/tbxark/gvim/internal/keybindings"
	"github.com/tbxark/gvim/internal/lsphost"
	"github.com/tbxark/gvim/internal/restore"
	"github.com/tbxark/gvim/internal/tui"
	"github.com/tbxark/gvim/internal/uiadapter"
	"github.com/tbxark/gvim/trust"
)

// wireSettingsService builds the Settings page's service with its live parts: the LSP
// manager for its Language Servers page statuses and actions, and the plugin runtime
// for its Plugins page. They are built after the page's config snapshot and before the
// model.
func (b *runtimeBuild) wireSettingsService() {
	ctx, opts, cfg, deps, pw := b.ctx, &b.opts, b.cfg, &b.deps, b.pw
	// The Settings page writes the config files and reapplies the live values. The
	// project layer is offered only while the workspace is trusted, so the check
	// reads the trust controller every time.
	b.settingsSvc = newSettingsService(b.root, *opts, b.ed, b.trustedForSettings, cfg, b.themeCat, settingsRuntime{
		lsp:         deps.LSP,
		localeApply: func(locale string) { pw.Runtime.SetLocale(locale) },
		lspReload: func(next config.Config) {
			// The worker serializes this with live trust changes and always applies the
			// newest config+trust snapshot under the live gate; it never blocks the caller.
			if b.lspBase != nil {
				b.lspBase.UpdateConfig(next, trust.State{Level: b.levelOf()})
			}
		},
		plugins:    pw.Runtime,
		pluginsCfg: cfg,
		keymapOf: func(cfg config.Config) tui.Keymap {
			return keybindings.NewKeymap(keybindings.ApplyOverrides(presetKeymap(opts.Keymap, cfg), cfg))
		},
		presetName: b.choice.name,
		iconsMode:  b.icon.Mode.String(),
		iconsWhy:   b.icon.Why,
		version:    gvimVersion(),
	})
	svc := b.settingsSvc
	svc.env = b.envProvider
	svc.setEnvWatch(b.envWatch)
	if b.localWatchRetryObj != nil {
		// Install after the settings service exists; SetInstall synchronizes with a retry
		// that may already have recovered a watcher while the runtime was being built.
		b.localWatchRetryObj.SetInstall(func(lw *localEnvWatch) {
			svc.setEnvWatch(lw)
		})
	}
	deps.SettingsPort = svc
	b.lspBase = newLSPBaselineUpdater(ctx, deps.LSP, svc.currentConfig(), trust.State{Level: b.levelOf()})
	svc.lspBase = b.lspBase
	if setter, ok := deps.LSP.(interface{ SetBaselineWait(func()) }); ok && b.lspBase != nil {
		setter.SetBaselineWait(b.lspBase.Wait)
	}
}

// wireTrustLSP re-gates the LSP baseline at once on a live trust change
// (revoke/restore), so a revoke cannot leave an auto-starting configuration behind even
// without a Settings reload. The updater keeps the latest settings config; a stale
// reload snapshot cannot overwrite a newer baseline. Manual Start/Restart remains
// available.
func (b *runtimeBuild) wireTrustLSP() {
	trustCtl, lspBase, deps := b.trustCtl, b.lspBase, &b.deps
	if trustCtl == nil {
		return
	}
	if b.opts.Remote == nil && lspBase != nil {
		trustCtl.prepareTrust = func(state trust.State) error {
			cfg, err := b.settingsSvc.loadTrustedLSP(state.Root)
			if err != nil {
				return err
			}
			lspBase.UpdateConfig(cfg, state)
			lspBase.Wait()
			if refresher, ok := deps.LSP.(interface{ RefreshServerPlans() }); ok {
				refresher.RefreshServerPlans()
			}
			return nil
		}
	}
	trustCtl.SetChangeListener(func(state trust.State) {
		// A manual start performed while Trusted must not survive as a forced
		// automatic=false bypass after a revoke. Clear it before any new manager or
		// Open can consult it; running servers are not stopped.
		if setter, ok := deps.LSP.(interface{ ClearForced() }); ok {
			setter.ClearForced()
		}
		if lspBase != nil {
			lspBase.UpdateTrust(trust.State{Level: state.Level})
		}
	})
}

// wireLSPExtras starts the Deno auto-selection and installs the external-location
// opener on the LSP manager.
func (b *runtimeBuild) wireLSPExtras() {
	ctx, opts, deps, authSlot := b.ctx, &b.opts, &b.deps, b.authSlot
	// Project-root Deno auto-selection (Fresh deno_lsp parity with authority-only checks):
	// one bounded background probe against the workspace root; it never overrides an explicit
	// or disabled user LSP config and is re-run after a reconnect or project-root switch. The
	// provider is evaluated when a manager for exactly this root is created, re-reading the
	// settings service's live config (a settings reload thus invalidates a stale decision).
	if deps.LSP != nil && authSlot != nil && b.denoRoot != "" {
		denoCfg := func() config.Config {
			// The Deno selection is an auto-start choice, so evaluate it against the same
			// live-trust-gated config the port uses, never the raw reloaded settings.
			return lspConfig(b.settingsSvc.currentConfig(), trust.State{Level: b.levelOf()})
		}
		b.denoAuto = newDenoAutoSelector(deps.LSP, authSlot, b.denoRoot, opts.Remote != nil, b.authGate, b.envProvider.Overlay, denoCfg)
		if b.denoAuto != nil {
			if setter, ok := deps.LSP.(interface{ SetRootChangeListener(func(string)) }); ok {
				setter.SetRootChangeListener(func(root string) {
					b.denoAuto.Rebind(ctx, root)
				})
			}
			b.denoAuto.Start(ctx)
		}
	}
	// Native external locations: Fresh's slang-lsp slang-synth builtin modules.
	// The opener validates the URI, refuses remote authorities, runs the fixed slangd
	// command through authority.OneShot under the trust gate with bounded output, and
	// returns locally generated read-only source for the TUI to open.
	if deps.LSP != nil && authSlot != nil {
		if setter, ok := deps.LSP.(interface {
			SetExternalLocationOpener(lsphost.ExternalLocationOpener)
		}); ok {
			setter.SetExternalLocationOpener(newSlangSynthOpener(opts.Remote != nil, authSlot, b.authGate, b.envProvider.Overlay, b.root))
		}
	}
}

// collectNotices sets the status row notice, the Show Warnings list and, for a daemon
// host, the Detach command.
func (b *runtimeBuild) collectNotices() {
	opts, deps := &b.opts, &b.deps
	// Notice is the first config problem, then any session file whose unsaved
	// text was kept, for the status row. Every warning still goes to stderr,
	// before the alternate screen starts.
	// Every config problem is kept for Show Warnings. The status row shows the first one, with
	// how many there are and where to read them when there are more.
	problems := configProblems(b.diags, b.choice.warnings)
	var notices []string
	if n := keybindings.Notice(problems); n != "" {
		notices = append(notices, n)
	}
	deps.Warnings = append(problems, b.choice.replaced...)
	notices = append(notices, b.openNotices...)
	if b.trustWarning != "" {
		notices = append(notices, b.trustWarning)
	}
	if b.logNotice != "" {
		notices = append(notices, b.logNotice)
	}
	if n := restore.RestoreNotice(b.failed); n != "" {
		notices = append(notices, n)
	}
	if b.recNotice != "" {
		notices = append(notices, b.recNotice)
	}
	if opts.Remote != nil {
		notices = append(notices, remoteEditingNotice(opts.Remote, deps.StartupOpen))
	}
	if b.themeWarn != "" {
		notices = append(notices, b.themeWarn)
	}
	deps.Notice = strings.Join(notices, "; ")
	if opts.Daemon != nil && opts.Daemon.Mode == DaemonServe {
		// A hosted editor offers Detach: the client goes away, the editor keeps running.
		deps.Detach = opts.Daemon.Detach
		rows := make([]commands.Entry, 0, len(deps.Commands)+1)
		rows = append(rows, deps.Commands...)
		rows = append(rows, commands.Entry{
			Name:        "Detach from Daemon",
			Description: "Detach this client and leave the editor running in the daemon",
			Action:      "detach_daemon",
			Source:      commands.SourceBuiltin,
		})
		deps.Commands = rows
	}
}

// newModel builds the UI model and restores the saved pane layout and find history.
func (b *runtimeBuild) newModel() {
	b.model = newUI(b.deps)
	// Restore the saved pane layout over the files the session just opened. A
	// remote launch has no local session layout, and a hot-exit restore skips the
	// layout with the clean tabs; a stale or invalid record is ignored by
	// RestoreSplits, which keeps the one-pane start.
	if b.restored && b.plan.tabs && (b.opts.Remote == nil || (b.deferRemoteRestore && !b.remoteRestoreSkipped)) {
		b.model.RestoreSplits(b.sess.Layout, b.sess.Files)
	}
	loadSearch(b.model, b.opts.SessionDir)
}

// wireDock gives the model its terminal dock: the shell starter and the clipboard and
// display options from the terminal config.
func (b *runtimeBuild) wireDock() {
	cfg, model := b.cfg, b.model
	shellCommand, shellArgs := cfg.Terminal.TerminalShellOptions()
	model.SetDock(tui.DockPorts{
		Starter: uiadapter.NewTermStarter(b.ctx, uiadapter.TermStarterOptions{
			Shell: shellCommand, Args: shellArgs, ScrollbackLines: cfg.Terminal.ScrollbackLines,
			SpawnEnv:  b.envProvider.Overlay,
			Authority: b.authSlot,
			// A refused remote cwd falls back to the authority's default directory and
			// says so in the status row (the terminal port has no notice channel).
			Notice: model.Notify,
			// A child OSC 52 writes the system clipboard only when the config asks
			// for it and the workspace is trusted (output is untrusted data, and
			// the default is off); the session parser is inert otherwise.
			OSC52: func(selection, text string) bool {
				if !cfg.Clipboard.UseOSC52 || !b.trustedLive() {
					return false
				}
				return uiadapter.SystemClipboard{}.Write(text) == nil
			},
			// A child OSC 52 read request is answered with the system clipboard only
			// when the gvim extension is on and the workspace is trusted. The answer
			// is bounded and queued as terminal input; no reply on failure/empty/too
			// large, so an unavailable clipboard never blocks the child's paste.
			OSC52Query: dockOSC52Query(cfg, b.trustedLive, systemClipboardRead),
		}),
		Clipboard: uiadapter.SystemClipboard{},
		Effects: tui.DockEffects{
			CloseOnExit:          cfg.Terminal.CloseOnExit,
			MouseForwarding:      cfg.Terminal.MouseForwarding,
			StayInScrollback:     !cfg.Terminal.JumpToEndOnOutput,
			NoMouseDragSelects:   !cfg.Terminal.MouseDragSelects,
			PassCtrlVWithoutText: cfg.Terminal.PassCtrlVWithoutText,
			ResumeAgents:         cfg.Terminal.ResumeAgents,
			// terminal.restore_live (a gvim extension, default true = Fresh's
			// behavior): a live-at-capture tab revives with a fresh PTY on the
			// user's first dive into it; exited tabs stay read-only.
			RestoreLive: cfg.Terminal.RestoreLive,
			Agents:      dockAgents(cfg.Terminal),
		},
	})
}

// wireAuthorityUI gives the model the agent discoverer and the authority's shell
// runner, and a remote launch its connection hooks and SSH terminal.
func (b *runtimeBuild) wireAuthorityUI() {
	// Agent discovery scans the local machine through the same spawn gate as the
	// terminal and plugins; the picker runs the scan off the update loop.
	b.model.SetAgentDiscoverer(newAgentDiscoverer(func() string { return b.proj.RootDir() }, b.spawnGate, b.cfg.Terminal.Agents))
	// The workspace authority of the session: the shell and formatter runner go
	// through the slot, and a remote launch shows its segment and opens the SSH
	// terminal at the remote workspace.
	if run := authorityShellRunner(b.authSlot); run != nil {
		b.model.SetShellRunner(run)
	}
	if b.opts.Remote != nil {
		b.wireRemoteHooks()
	}
}

// restoreTerminal brings back the saved terminal transcript and opens the --diff
// comparison.
func (b *runtimeBuild) restoreTerminal() {
	opts, model := &b.opts, b.model
	// A restored session brings the terminal transcript back, dead: nothing is
	// re-run automatically, and Restart starts a shell below the transcript.
	if b.plan.load && b.plan.tabs && opts.SessionDir != "" && !b.remoteRestoreSkipped {
		if st, ok, err := restore.LoadTerminalForRestore(opts.SessionDir); err != nil {
			// An unusable record is quarantined (or protected by a marker) before the
			// session continues, so the normal-exit save cannot silently replace it.
			// Warn on both the log stream and the status row.
			_, _ = fmt.Fprintln(os.Stderr, "gvim: warning: terminal:", err)
			model.Notify(err.Error())
		} else if ok {
			model.RestoreTerminal(st)
		}
	}
	if opts.Diff {
		// The two files are in tabs 0 and 1: the tab count was checked before recovery.
		model.OpenCompare(0, 1)
	}
}

// newRuntime assembles the built session and installs the quit stash hook.
func (b *runtimeBuild) newRuntime() *runtime {
	opts := &b.opts
	// A launch that does not restore the session does not save it either: a save
	// would replace the saved session and remove the stash files that it names.
	// A file launch and --simple are those launches.
	sessionDir := ""
	if b.plan.save && !b.remoteRestoreSkipped {
		sessionDir = opts.SessionDir
	}
	rt := &runtime{
		model:                 b.model,
		ed:                    b.ed,
		projects:              b.proj,
		watcher:               b.watcher,
		sessionDir:            sessionDir,
		pluginStateFile:       b.pluginStateFile,
		daemonMaterializedCap: b.cfg.Daemon.MaterializedWorkspaces,
		pluginWS:              b.pluginWS,
		searchDir:             opts.SessionDir,
		stashDir:              opts.SessionDir,
		kept:                  keptFiles(b.failed),
		recovery:              recoveryDirForRun(opts.SessionDir, b.watching, b.remoteRecoveryPath),

		recoveryWriter:   b.deps.Root.Recovery,
		log:              b.logFile,
		auth:             b.authSlot,
		remoteWatch:      b.remoteFileWatch,
		remoteFiles:      b.remoteFiles,
		keepSavedSession: b.remoteRestoreSkipped,
	}
	b.model.SetQuitStash(rt.stashForQuit)
	return rt
}
