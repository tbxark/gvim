package app

import (
	"fmt"
	"os"
	"strings"

	"github.com/tbxark/gvim/commands"
	"github.com/tbxark/gvim/config"
	"github.com/tbxark/gvim/explorer"
	"github.com/tbxark/gvim/finder"
	"github.com/tbxark/gvim/internal/keybindings"
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
	ctx, opts, cfg, deps, pw, keys := b.ctx, b.opts, b.cfg, &b.deps, b.pw, b.keys
	levelOf := makeLevelOf(b.trustCtl, b.trustState.Level)
	trustedForSettings := makeTrustedForSettings(b.trustCtl)
	var lspBase *lspBaselineUpdater
	// The Settings page writes the config files and reapplies the live values. The
	// project layer is offered only while the workspace is trusted, so the check
	// reads the trust controller every time.
	b.settingsSvc = newSettingsService(b.root, opts, b.ed, trustedForSettings, cfg, b.themeCat, settingsRuntime{
		lsp:         deps.LSP,
		localeApply: func(locale string) { pw.Runtime.SetLocale(locale) },
		lspReload: func(next config.Config) {
			// The worker serializes this with live trust changes and always applies the
			// newest config+trust snapshot under the live gate; it never blocks the caller.
			if lspBase != nil {
				lspBase.UpdateConfig(next, trust.State{Level: levelOf()})
			}
		},
		plugins: b.pluginInv,
		keymapOf: func(cfg config.Config) tui.Keymap {
			return keybindings.NewKeymap(keybindings.ApplyOverrides(presetKeymap(opts.Keymap, cfg), cfg))
		},
		presetName:        b.choice.name,
		iconsMode:         b.icon.Mode.String(),
		iconsWhy:          b.icon.Why,
		version:           Version(),
		initialFormatters: b.initFormatters,
		keysReload: func(next config.Config) {
			if keys != nil {
				keys.Reload(next)
			}
		},
		withKeybindings: func(cur config.Config) config.Config {
			if keys != nil {
				return keys.Current(cur)
			}
			return cur
		},
	})
	svc := b.settingsSvc
	if b.liveCfg != nil {
		b.liveCfg.set(svc.currentConfig)
	}
	svc.env = b.envProvider
	svc.setEnvWatch(b.envWatch)
	svc.setRootOf(b.rootNow)
	proj := b.proj
	if proj != nil {
		// Switch Project builds the new root's tree and index from the live config, not
		// from the defaults.
		proj.SetOptions(
			func() explorer.Options { return explorerOptions(svc.currentConfig()) },
			func() finder.Options { return finderOptions(svc.currentConfig()) },
		)
	}
	projSearch, grep, isRemote, remoteLoader := b.projSearch, b.grep, opts.Remote != nil, b.remoteExplorerLoader
	svc.setConfigReload(func(next config.Config) {
		if projSearch != nil {
			projSearch.SetCustomIgnorePatterns(next.FileExplorer.CustomIgnorePatterns)
		}
		if grep != nil {
			grep.SetCustomIgnorePatterns(next.FileExplorer.CustomIgnorePatterns)
		}
		if !isRemote && proj != nil {
			// The index and root of the folder that is open now, not the startup ones.
			proj.Index().SetOptions(proj.RootDir(), finderOptions(next))
		}
		if grep != nil {
			grep.SetScanFilter(next.FileScanExclusions, next.FileScanInclusions)
		}
		if isRemote && remoteLoader != nil {
			remoteLoader.SetIgnoreOptions(next.FileExplorer.RespectGitignore, next.FileExplorer.CustomIgnorePatterns, next.FileScanExclusions, next.FileScanInclusions)
		}
	})
	if b.localWatchRetryObj != nil {
		// Install after the settings service exists; SetInstall synchronizes with a retry
		// that may already have recovered a watcher while the runtime was being built.
		b.localWatchRetryObj.SetInstall(func(lw *localEnvWatch) {
			svc.setEnvWatch(lw)
		})
	}
	deps.SettingsPort = svc
	lspBase = newLSPBaselineUpdater(ctx, deps.LSP, svc.currentConfig(), trust.State{Level: levelOf()})
	b.lspBase = lspBase
	svc.lspBase = lspBase
	if setter, ok := deps.LSP.(interface{ SetBaselineWait(func()) }); ok && lspBase != nil {
		setter.SetBaselineWait(lspBase.Wait)
	}
}

// wireTrustLSP re-gates the LSP baseline at once on a live trust change
// (revoke/restore), so a revoke cannot leave an auto-starting configuration behind even
// without a Settings reload. The updater keeps the latest settings config; a stale
// reload snapshot cannot overwrite a newer baseline. Manual Start/Restart remains
// available.
func (b *runtimeBuild) wireTrustLSP() {
	trustCtl, lspBase, lsp, svc, isRemote := b.trustCtl, b.lspBase, b.deps.LSP, b.settingsSvc, b.opts.Remote != nil
	if trustCtl == nil {
		return
	}
	if !isRemote && lspBase != nil {
		trustCtl.prepareTrust = func(state trust.State) error {
			cfg, err := svc.loadTrustedLSP(state.Root)
			if err != nil {
				return err
			}
			lspBase.UpdateConfig(cfg, state)
			lspBase.Wait()
			if refresher, ok := lsp.(interface{ RefreshServerPlans() }); ok {
				refresher.RefreshServerPlans()
			}
			return nil
		}
	}
	trustCtl.SetChangeListener(func(state trust.State) {
		// A manual start performed while Trusted must not survive as a forced
		// automatic=false bypass after a revoke. Clear it before any new manager or
		// Open can consult it; running servers are not stopped.
		if setter, ok := lsp.(interface{ ClearForced() }); ok {
			setter.ClearForced()
		}
		if lspBase != nil {
			lspBase.UpdateTrust(trust.State{Level: state.Level})
		}
	})
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
	trustedLive := makeTrustedLive(makeLevelOf(b.trustCtl, b.trustState.Level))
	model.SetDock(tui.DockPorts{
		Starter: uiadapter.NewTermStarter(b.ctx, uiadapter.TermStarterOptions{
			Shell: shellCommand, Args: shellArgs, ScrollbackLines: cfg.Terminal.ScrollbackLines,
			SpawnEnv:  b.envProvider.Overlay,
			Authority: b.authSlot,
			// A child OSC 52 writes the system clipboard only when the config asks
			// for it and the workspace is trusted (output is untrusted data, and
			// the default is off); the session parser is inert otherwise.
			OSC52: func(selection, text string) bool {
				if !cfg.Clipboard.UseOSC52 || !trustedLive() {
					return false
				}
				return uiadapter.SystemClipboard{}.Write(text) == nil
			},
			// A child OSC 52 read request is answered with the system clipboard only
			// when the gvim extension is on and the workspace is trusted. The answer
			// is bounded and queued as terminal input; no reply on failure/empty/too
			// large, so an unavailable clipboard never blocks the child's paste.
			OSC52Query: dockOSC52Query(cfg, trustedLive, systemClipboardRead),
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
	proj := b.proj
	spawnGate := makeSpawnGate(b.opts.Remote != nil, b.rootNow, makeLevelOf(b.trustCtl, b.trustState.Level))
	b.model.SetAgentDiscoverer(newAgentDiscoverer(func() string { return proj.RootDir() }, spawnGate, b.cfg.Terminal.Agents))
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
		cancel:           b.cancel,
		localWatchRetry:  b.localWatchRetryObj,
		remoteEnv:        b.remoteEnv,
		auth:             b.authSlot,
		remoteWatch:      b.remoteFileWatch,
		remoteFiles:      b.remoteFiles,
		keepSavedSession: b.remoteRestoreSkipped,
	}
	b.model.SetQuitStash(rt.stashForQuit)
	return rt
}
