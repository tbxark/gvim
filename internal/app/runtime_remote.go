package app

import (
	"context"

	"github.com/tbxark/gvim/authority"
	"github.com/tbxark/gvim/internal/restore"
	"github.com/tbxark/gvim/internal/tui"
	"github.com/tbxark/gvim/lsp"
)

// wireRemote installs a remote launch's file access, recovery, language servers, file
// index, environment, search, explorer and git ports on the connected authority. It
// does nothing for a local launch.
func (b *runtimeBuild) wireRemote() {
	opts, deps, authSlot := &b.opts, &b.deps, b.authSlot
	if opts.Remote == nil {
		return
	}
	ctx, ed, cfg := b.ctx, b.ed, b.cfg
	// The slot now has the real remote FileSystem: give the editor the file access and
	// park the remote file for the root's first command. Installing the access is
	// local bookkeeping only; the load itself runs in that command and a failure is a
	// retryable notice, never an empty local buffer.
	deps.StartupOpen = setupRemoteEditing(ed, authSlot, opts.Remote)
	deps.StartupLine, deps.StartupCol = opts.Remote.Line, opts.Remote.Col
	// Remote hot-exit/recovery: stage the unsaved text of remote documents under the
	// remote workspace's local session directory (never named by a local file) and
	// recover abandoned copies now that the remote file access is installed, so the
	// recorded remote paths reopen through the authority. It is a deliberate local
	// staging divergence from Fresh, whose recovery routes through the authority.
	// Remote workspaces stage their session under the local cache keyed by the
	// remote target; the remote project path is never used as a local path.
	recoverySession := remoteRecoverySessionDir(opts.Remote)
	if b.deferRemoteRestore {
		recoverySession = opts.SessionDir
	}
	b.recPort, b.recEvery, b.recNotice = setupRemoteRecovery(ed, cfg, recoverySession, "SSH:"+remoteTrustTarget(opts.Remote))
	deps.Root.Recovery, deps.Root.RecoveryEvery = b.recPort, b.recEvery
	b.remoteRecoveryPath = restore.RecoveryDirFor(recoverySession, true)
	// LSP over SSH: the servers run on the remote host through the slot's
	// LongRunning surface; a local server must never start for a remote buffer.
	if deps.LSP != nil {
		if setter, ok := deps.LSP.(interface {
			SetSpawner(func(context.Context, lsp.Config) (*lsp.Client, error))
		}); ok {
			setter.SetSpawner(remoteLSPSpawner(authSlot, opts.Remote.Workspace(), b.envProvider.Overlay))
		}
		if setter, ok := deps.LSP.(interface {
			SetRemoteMarkerChecker(func(ctx context.Context, dir string, markers []string) (bool, error))
		}); ok {
			setter.SetRemoteMarkerChecker(remoteLSMarkerChecker(authSlot))
		}
	}
	// The quick-open file list comes from the remote workspace through the
	// authority file system (streaming, bounded, no local fallback); the palette
	// root is that remote workspace and a local project switch would enumerate
	// the local disk, so it is refused for a remote launch.
	var remoteRoot string
	b.remoteFiles, remoteRoot = newRemoteFileIndex(ctx, authSlot, opts.Remote.Workspace())
	b.denoRoot = remoteRoot
	deps.Files = b.remoteFiles
	deps.FileRoot = remoteRoot
	remoteRootPorts(&deps.Root)
	// The remote root is now resolved (a ~ root is expanded on the remote host):
	// point the remote environment provider at it and warm its marker probe/capture
	// in the background, so the first remote spawn reads a captured remote delta
	// and no UI update ever waits on the remote host.
	if b.envRemote != nil {
		b.envRemote.SetRoot(remoteRoot)
		// Probe the interactive login shell synchronously (one bounded remote
		// command) so a fallback notice can join the startup notices; detection and
		// capture themselves stay off the UI update loop.
		if b.envShell != nil {
			b.envShell.Prepare(ctx)
			if notice := b.envShell.Notice(); notice != "" {
				b.openNotices = append(b.openNotices, notice)
			}
		}
		go b.envRemote.Warm(ctx)
		// Env-input changes on the remote host invalidate detection/capture through
		// the authority's own watch (no local fallback); the watcher degrades to a
		// Settings notice + manual Env: Reload when the backend cannot watch.
		b.remoteEnv = startRemoteEnvWatch(ctx, authSlot, b.envRemote, remoteRoot)
		b.envWatch = b.remoteEnv
	}
	// Project search and Git Grep run on the remote host through the authority's
	// one-shot surface (the gate still applies); their results carry absolute remote
	// paths, so selecting one opens through the remote document access. Project
	// replacement reads and writes each candidate through the authority file system
	// with the same trust gate: bounded reads, atomic remote writes, stale/changed
	// files skipped, cancellation and a partial-write report. The local ports stay
	// cleared by remoteRootPorts for every other caller.
	installRemoteSearchPorts(&deps.Root, authSlot.OneShot(), authSlot.Files(), b.authGate, remoteRoot, authSlot.Label(), b.envProvider.Overlay)
	// The read-only remote explorer: the tree lists the remote directories through
	// the authority file system, one command at a time, so the update loop never
	// waits on the remote host. No local fallback: every listing is remote, the
	// workspace trust gate runs before them, and a disconnected authority says so
	// (the tree is reloaded after a reconnect). New File, New Directory, Rename,
	// Delete, and the Copy and Move of subtrees (copy/cut/paste and drag/move) run
	// through the authority's FileMutations surface; the local file-operation adapter
	// is never used for remote paths.
	b.remoteExplorerLoader = installRemoteExplorer(deps, authSlot, remoteRoot, b.authGate, ed)
	// Remote git marks and the quick-open file preview run through the same authority
	// (remote cwd, remote env delta, spawn/trust gate), never through the local git or
	// the local file system. The git read is briefly cached so save/watch/reload bursts
	// share one remote command; the reconnect hook invalidates it and restarts the
	// remote file index.
	b.remoteGit = installRemoteDataPorts(&deps.Root, authSlot, remoteRoot, b.envProvider.Overlay)
}

// restoreRemoteSession restores the saved session of a daemon-hosted remote workspace
// only after the authority connects and the remote document access is installed (the
// files open through the remote file access, never the local disk). A failed
// reconnect leaves the session file and stashes untouched for the next start: this
// runtime restores nothing and the capture path skips it, so an empty editor can never
// overwrite the saved session.
func (b *runtimeBuild) restoreRemoteSession() {
	if !b.deferRemoteRestore || !b.restored {
		return
	}
	opts, deps, authSlot, sess := &b.opts, &b.deps, b.authSlot, b.sess
	if authSlot == nil || !authSlot.Connected() {
		b.remoteRestoreSkipped = true
		deps.ShowExplorer = false
		deps.StartupOpen, deps.StartupLine, deps.StartupCol = "", 0, 0
		b.openNotices = append(b.openNotices, "SSH workspace disconnected: the saved remote session is kept for the next start")
		return
	}
	// The saved paths are remote: probe their existence through the authority,
	// then let the editor open them through its installed remote file access.
	exists := remoteRestoreExists(authSlot)
	switch {
	case b.plan.tabs:
		b.failed = restore.RestoreSessionAccess(b.ed, opts.SessionDir, sess, exists)
		b.tree.SetShowHidden(sess.ShowHidden)
		b.tree.SetShowIgnored(sess.ShowIgnored)
		if deps.Explorer != nil {
			deps.Explorer.SetShowHidden(sess.ShowHidden)
			deps.Explorer.SetShowIgnored(sess.ShowIgnored)
		}
	case b.plan.hot:
		b.failed = restore.RestoreHotExitAccess(b.ed, opts.SessionDir, sess, exists)
	}
	// The restored tabs own the start: do not reopen the startup target file.
	deps.StartupOpen, deps.StartupLine, deps.StartupCol = "", 0, 0
}

// wireRemoteWatch bridges the authority's remote watch into the root's existing watch
// pipeline (RootPorts.Watch/WatchDirs). A local launch gets nil here and keeps its
// fsnotify watcher; the remote writer never touches the local filesystem.
func (b *runtimeBuild) wireRemoteWatch() {
	b.remoteFileWatch = setupRemoteWatch(b.ctx, &b.deps, b.authSlot, b.opts.Remote)
	if b.remoteExplorerLoader != nil {
		b.remoteExplorerLoader.setWatcher(b.remoteFileWatch)
	}
}

// wireRemoteHooks gives the model the status, reconnect and credential-prompt hooks of
// the remote authority and opens the SSH terminal at the remote workspace.
func (b *runtimeBuild) wireRemoteHooks() {
	ctx, slot := b.ctx, b.authSlot
	needsPassword := func() bool {
		p, ok := slot.(interface{ NeedsPassword() bool })
		return ok && p.NeedsPassword()
	}
	reconnect := func(ctx context.Context, password string) error {
		if p, ok := slot.(interface {
			ReconnectWithPassword(context.Context, string) error
		}); ok {
			return p.ReconnectWithPassword(ctx, password)
		}
		return slot.Reconnect(ctx)
	}
	b.model.SetAuthorityHooks(tui.AuthorityHooks{
		Status: func() tui.AuthorityStatus {
			st := slot.State()
			return tui.AuthorityStatus{Text: st.Label, Connected: st.Connected, Reason: st.Reason, Connecting: st.Connecting}
		},
		NeedsPassword: needsPassword,
		Reconnect:     reconnect,
		// Encrypted identity passphrase and keyboard-interactive (2FA) prompts:
		// the authority records the request and this hook asks for it once the
		// terminal window is active or the background poll sees it.
		NeedsPassphrase: func() bool {
			p, ok := slot.(interface{ NeedsPassphrase() bool })
			return ok && p.NeedsPassphrase()
		},
		ReconnectWithPassphrase: func(ctx context.Context, passphrase string) error {
			if p, ok := slot.(interface {
				ReconnectWithPassphrase(context.Context, string) error
			}); ok {
				return p.ReconnectWithPassphrase(ctx, passphrase)
			}
			return slot.Reconnect(ctx)
		},
		KeyboardPrompt: func() ([]string, []bool, bool) {
			p, ok := slot.(interface {
				KeyboardInteractiveChallenge() (authority.KeyboardPrompt, bool)
			})
			if !ok {
				return nil, nil, false
			}
			challenge, pending := p.KeyboardInteractiveChallenge()
			if !pending {
				return nil, nil, false
			}
			return challenge.Questions, challenge.Echos, true
		},
		ReconnectWithAnswers: func(ctx context.Context, answers []string) error {
			if p, ok := slot.(interface {
				ReconnectWithAnswers(context.Context, []string) error
			}); ok {
				return p.ReconnectWithAnswers(ctx, answers)
			}
			return slot.Reconnect(ctx)
		},
		// LSP over SSH: after the connection is back, restart the remote servers
		// that were running before the drop (never on the update goroutine; the
		// manager honours trust and cancellation and starts no duplicate).
		// External-change watching restarts on the new carrier too; the generation
		// bump there drops events from the dead one before they can reach the UI.
		AfterReconnect: func() (int, error) {
			// The remote interactive-login-shell probe is connection-scoped: re-probe
			// after a reconnect off the UI goroutine and refresh the fallback notice
			// (a probe that failed at startup must not pin the session to sh -c). The
			// activation capture itself stays trust-gated and the probe is a fixed
			// pass-through command, never a project snippet.
			if b.envShell != nil {
				go b.envShell.Reprobes(ctx)
			}
			b.remoteFileWatch.Reconnected()
			b.remoteEnv.Reconnected()
			// The remote git markers and the file list were read from the carrier
			// that just dropped: invalidate the cached status (the TUI re-reads the
			// marks in its reconnect command) and re-enumerate the remote files on
			// the restored carrier (the palette's loading poll picks the new list up).
			if b.remoteGit != nil {
				b.remoteGit.Invalidate()
			}
			if b.remoteFiles != nil {
				b.remoteFiles.Restart()
			}
			if b.denoAuto != nil {
				// New carrier: clear the old decision and probe again; a late answer from
				// the dropped carrier cannot apply because the generation moves.
				b.denoAuto.Start(ctx)
			}
			return restartLSPAfterReconnect(slot, b.deps.LSP)
		},
	})
	b.model.OpenAuthorityTerminalAtStart()
}
