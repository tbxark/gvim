package app

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sync/atomic"

	"github.com/tbxark/gvim/authority"
	"github.com/tbxark/gvim/config"
	"github.com/tbxark/gvim/env"
	"github.com/tbxark/gvim/internal/keybindings"
	"github.com/tbxark/gvim/internal/pluginhost"
	"github.com/tbxark/gvim/internal/tui"
	"github.com/tbxark/gvim/internal/tui/icons"
	"github.com/tbxark/gvim/internal/uiadapter"
	"github.com/tbxark/gvim/plugin/hostos"
	"github.com/tbxark/gvim/pluginapi"
	"github.com/tbxark/gvim/trust"
)

type liveConfigRef struct {
	fn       atomic.Pointer[func() config.Config]
	fallback config.Config
}

func (r *liveConfigRef) set(fn func() config.Config) {
	if fn != nil {
		r.fn.Store(&fn)
	}
}

func (r *liveConfigRef) get() config.Config {
	if fn := r.fn.Load(); fn != nil && *fn != nil {
		return (*fn)()
	}
	return r.fallback
}

type authoritySlotRef struct {
	slot authority.Slot
}

func makeLevelOf(ctl *trustController, fallback trust.Level) func() trust.Level {
	if ctl != nil {
		return func() trust.Level { return ctl.State().Level }
	}
	return func() trust.Level { return fallback }
}

func makeTrustedLive(levelOf func() trust.Level) func() bool {
	return func() bool { return levelOf() == trust.Trusted }
}

func makeSpawnGate(remote bool, root string, levelOf func() trust.Level) func(context.Context, []string, string, map[string]string) error {
	return func(_ context.Context, argv []string, cwd string, childEnv map[string]string) error {
		if remote {
			if levelOf() != trust.Trusted {
				return errors.New("workspace is not trusted: remote commands are disabled")
			}
			return nil
		}
		pathValue := childEnv["PATH"]
		if pathValue == "" {
			pathValue = os.Getenv("PATH")
		}
		return trust.DecideSpawn(levelOf(), root, cwd, argv, pathValue)
	}
}

func makeLSPGate(remote bool, root string, levelOf func() trust.Level) func(context.Context, []string, string, string) error {
	return func(_ context.Context, argv []string, cwd, pathValue string) error {
		if remote {
			if levelOf() != trust.Trusted {
				return errors.New("workspace is not trusted: language servers cannot start")
			}
			return nil
		}
		return trust.DecideSpawn(levelOf(), root, cwd, argv, pathValue)
	}
}

func makeTrustedForSettings(ctl *trustController) func() bool {
	if ctl == nil {
		return func() bool { return true }
	}
	return func() bool { return ctl.State().AllowsProjectConfig() }
}

// spawnGate is the workspace spawn gate (docs/trust.md).
func (b *runtimeBuild) spawnGate(ctx context.Context, argv []string, cwd string, childEnv map[string]string) error {
	return makeSpawnGate(b.opts.Remote != nil, b.root, makeLevelOf(b.trustCtl, b.trustState.Level))(ctx, argv, cwd, childEnv)
}

// authGate is the authority's plain Gate (no environment): the remote/fallback check;
// the local backend prefers the environment-aware spawnGate.
func (b *runtimeBuild) authGate(ctx context.Context, argv []string, cwd string) error {
	return b.spawnGate(ctx, argv, cwd, nil)
}

// wireEnvironment builds the project environment provider. It is detected once at
// workspace open (shallow, no execution) and its activation is applied to every
// process the editor spawns. Activation itself is host-gated on the live trust level.
//
// A local launch detects and activates locally; a remote launch uses env.Remote, whose
// marker probe and activation run on the REMOTE host through the authority's one-shot
// surface, so the local overlay (and any secret in it) never reaches the remote host
// (#R13-13, Fresh's backend-agnostic provider).
func (b *runtimeBuild) wireEnvironment() {
	ctx, cfg, root := b.ctx, b.cfg, b.root
	trustedLive := makeTrustedLive(makeLevelOf(b.trustCtl, b.trustState.Level))
	if b.opts.Remote != nil {
		slotRef := &authoritySlotRef{}
		b.slotRef = slotRef
		getSlot := func() authority.Slot { return slotRef.slot }
		var remoteEnv *env.Remote
		rootOf := func() string {
			if remoteEnv == nil {
				return ""
			}
			return remoteEnv.Root()
		}
		b.envShell = newRemoteEnvShell(getSlot, rootOf, func(msg string) {
			if remoteEnv != nil {
				remoteEnv.SetNotice(msg)
			}
		})
		remoteEnv = env.NewRemote(b.opts.Remote.Workspace(), cfg.EnvDetectors(), trustedLive,
			b.envShell.RunDetect, b.envShell.RunCapture)
		// Before trust the authority command gate refuses the shell probe; the marker
		// detector then uses shell-free authority Stat calls so an untrusted workspace can
		// still show the remote detection result (activation remains trust-gated).
		remoteEnv.SetMarkerProber(newRemoteMarkerProber(getSlot))
		b.envRemote = remoteEnv
		b.envProvider = remoteEnv
		return
	}
	localProvider := env.New(root, cfg.EnvDetectors(), trustedLive)
	b.envProvider = localProvider
	if !b.watching {
		localProvider.SetWatchNotice(appText("gvim.notice.local_env_watch_off", "Local environment: file watching is off; changed detector markers are picked up on Env: Reload"))
		return
	}
	// Local detector markers are watched with the editor's fsnotify watcher so a
	// created/removed .envrc/.nvmrc/.venv refreshes detection off the UI loop. If
	// fsnotify is unavailable the provider carries an Env: Reload degradation
	// notice instead of blocking or polling the tree. Env: Reload retries a
	// failed start (e.g. the root was not watchable yet) in the background.
	localWatch := startLocalEnvWatch(ctx, localProvider, root)
	b.envWatch = localWatch
	retry := &localEnvWatchRetry{
		ctx:     ctx,
		current: localWatch,
		start: func() *localEnvWatch {
			return startLocalEnvWatch(ctx, localProvider, root)
		},
	}
	b.localWatchRetryObj = retry
	b.localWatchRetry = retry.Retry
}

// mountRemoteAuthority mounts a remote launch's authority before the plugin runtime is
// built: the runtime's OS is the authority-backed one (see buildPlugins), so gvim.fs
// and gvim.proc from the first plugin Work go to the remote workspace and never to the
// local machine. A local launch keeps the plugin OS and mounts after the runtime
// (connectAuthority).
func (b *runtimeBuild) mountRemoteAuthority() {
	if b.opts.Remote != nil {
		spawnGate := makeSpawnGate(true, b.root, makeLevelOf(b.trustCtl, b.trustState.Level))
		authGate := func(ctx context.Context, argv []string, cwd string) error {
			return spawnGate(ctx, argv, cwd, nil)
		}
		b.authSlot = mountAuthority(b.opts, nil, authGate, spawnGate, localEnvOverlay(b.envProvider, true))
		if b.slotRef != nil {
			b.slotRef.slot = b.authSlot
		}
	}
}

// wireSettingsDeps gives the model the environment, notice sink, editor adapter and the
// settings, theme and keymap ports.
func (b *runtimeBuild) wireSettingsDeps() {
	opts, cfg, deps := &b.opts, b.cfg, &b.deps
	levelOf := makeLevelOf(b.trustCtl, b.trustState.Level)
	deps.Env = envPort{provider: b.envProvider, level: func() string { return string(levelOf()) }, retryWatch: b.localWatchRetry}
	if b.logFile != nil {
		deps.NoticeSink = b.logFile.Notice
	}
	adapter, _ := deps.Editor.(*uiadapter.Editor)
	if adapter != nil {
		adapter.SetContext(cfg.HighlightContextBytes)
		adapter.SetHotExit(b.plan.hotExit)
	}
	b.ed.SetShellEnv(b.envProvider.Overlay)
	deps.Settings = settingsFor(*opts, cfg)
	deps.Settings.Theme = b.th.Name
	deps.ThemeCatalog = b.themeCat
	deps.ThemeRef = b.themeRef
	b.icon = icons.Resolve(effectiveUIIcons(cfg), cfg.UIIconsFont, os.Getenv)
	deps.Settings.IconsMode, deps.Settings.IconsWhy = b.icon.Mode.String(), b.icon.Why
	deps.Settings.Keymap = b.choice.name
	keys := keybindings.NewStore(deps.Settings.ConfigFile, cfg)
	b.keys = keys
	ref := &liveConfigRef{fallback: cfg}
	b.liveCfg = ref
	liveCfg := func() config.Config { return keys.Current(ref.get()) }
	b.liveCfgFn = liveCfg
	deps.SwitchKeymap = switchKeymapWith(liveCfg)
}

// wireLSP builds the language-server port and gives the manager the environment, the
// spawn gates and the config, where it supports them.
func (b *runtimeBuild) wireLSP() {
	cfg, deps := b.cfg, &b.deps
	levelOf := makeLevelOf(b.trustCtl, b.trustState.Level)
	trustedLive := makeTrustedLive(levelOf)
	lspGate := makeLSPGate(b.opts.Remote != nil, b.root, levelOf)
	deps.LSP = newLSP(lspConfig(cfg, b.trustState))
	if setter, ok := deps.LSP.(interface{ SetSpawnEnv(func() []string) }); ok {
		setter.SetSpawnEnv(b.envProvider.Overlay)
	}
	if setter, ok := deps.LSP.(interface {
		SetSpawnGate(func(context.Context, []string, string, string) error)
	}); ok {
		setter.SetSpawnGate(lspGate)
	}
	if setter, ok := deps.LSP.(interface{ SetAutoStartGate(func() bool) }); ok {
		// The live trust check gates automatic starts for managers that already exist
		// (a trust revoke must not leave their old AutoStart configs running new
		// servers); manual StartLanguage/Restart still goes through lspGate.
		setter.SetAutoStartGate(trustedLive)
		// Keep the configured plan in managers; the live gate decides when it may run.
		if configSetter, ok := deps.LSP.(lspConfigSetter); ok {
			configSetter.SetConfig(cfg)
		}
	}
	// A remote launch routes the language servers through the authority (LSP over SSH,
	// docs/authority.md §7) once the slot exists (wireRemote). Until then the port is
	// built as for a local launch; the manager never starts a local server for a remote
	// workspace after the spawner is installed, and the spawn gate still runs before
	// every start.
}

// wireRootDeps fills the view options and the root's ports (projects, grep, preview,
// search, git, watch, recovery).
func (b *runtimeBuild) wireRootDeps() {
	opts, cfg, deps := &b.opts, b.cfg, &b.deps
	// A folder argument shows the explorer at startup. Without one the explorer
	// stays hidden until Ctrl+B, unless the session had it open. --simple opens
	// neither.
	deps.ShowExplorer = b.plan.explorer && (opts.Root != "" || (b.plan.tabs && b.restored && b.sess.Explorer))
	deps.Sidebar = b.sidebarState
	deps.HideLineNumbers = !cfg.LineNumbers
	deps.HideMenuBar = !cfg.ShowMenuBar
	deps.TabSize = cfg.TabSize
	deps.View = viewOptions(cfg, b.icon.Mode.String())
	deps.Languages = languageSettings(cfg)
	deps.MarkdownTOC = markdownTOCConfig(cfg)
	b.projSearch = newProjectSearch(b.edOpts, cfg.FileExplorer.CustomIgnorePatterns)
	b.grep = grepFor(b.edOpts, cfg.FileExplorer.CustomIgnorePatterns)
	deps.Root = tui.RootPorts{
		Projects:       b.proj,
		Grep:           b.grep,
		Preview:        previewReader{},
		ProjectSearch:  b.projSearch,
		ProjectReplace: b.projSearch,
		GitGrep:        gitGrep{spawnEnv: b.envProvider.Overlay},
		Git:            uiadapter.GitStatus{},
		OpenURL:        openURL,
		Watch:          b.events,
		AutoSave:       b.interval,
		Stashes:        uiadapter.StashesFor(opts.SessionDir),
	}
	deps.Root.Keybindings = b.keys
	deps.Root.AutoRevertPoll = autoRevertPoll(cfg)
	deps.Root.Recovery, deps.Root.RecoveryEvery = b.recPort, b.recEvery
	deps.Root.FullLoadPrompts = b.fullLoads
	if b.watcher != nil {
		// Assign only a running watcher: a nil *filewatch.Watcher in the interface is not nil.
		deps.Root.WatchDirs = b.watcher
	}
}

// buildPlugins builds the plugin runtime. It is always wired; with --no-plugins its
// loader is disabled.
func (b *runtimeBuild) buildPlugins() {
	opts, cfg := &b.opts, b.cfg
	// A daemon workspace scopes gvim.state to its own workspace session directory
	// (sessions/<stable-id>/plugin-state.json); a direct launch keeps the editor-wide
	// per-plugin state files. This read happens here, off the update loop: buildRuntime
	// runs before the program starts, or in the workspace build goroutine. The plugin
	// runtime seeds the parsed state when it starts and writes it back coalesced.
	var pluginStateData []byte
	if opts.Daemon != nil && opts.SessionDir != "" {
		b.pluginStateFile = filepath.Join(opts.SessionDir, "plugin-state.json")
		if data, err := os.ReadFile(b.pluginStateFile); err == nil {
			pluginStateData = data
		} else if !errors.Is(err, fs.ErrNotExist) {
			_, _ = fmt.Fprintf(os.Stderr, "gvim: warning: plugin state: %v\n", err)
		}
	}
	po := pluginhost.Options{
		Paths:              opts.PluginPaths,
		Disabled:           opts.NoPlugins,
		LogSink:            opts.pluginLog,
		Version:            gvimVersion(),
		Preset:             presetKeymap(opts.Keymap, cfg),
		Root:               b.proj.RootDir,
		SpawnEnv:           localEnvOverlay(b.envProvider, opts.Remote != nil),
		Gate:               b.spawnGate,
		WorkspaceStateFile: b.pluginStateFile,
		WorkspaceStateData: pluginStateData,
		Remote:             opts.Remote != nil,
		Config:             b.liveCfgFn,
	}
	// A shared daemon runtime spans workspaces but its plugin services are session-wide;
	// its per-workspace scanner/trust root cannot be represented safely. A remote launch's
	// local tmux/transcript stores are not the remote workspace's either. Only a local
	// direct (non-shared) launch exposes agent discovery to plugins; the plugin reports
	// its unsupported state in the other cases.
	if opts.Remote == nil && (opts.Daemon == nil || opts.PluginDaemon == nil) {
		proj := b.proj
		spawnGate := makeSpawnGate(false, b.root, makeLevelOf(b.trustCtl, b.trustState.Level))
		discoverer := newAgentDiscoverer(func() string { return proj.RootDir() }, spawnGate, cfg.Terminal.Agents)
		po.AgentDiscover = func(ctx context.Context) (pluginapi.AgentReport, error) {
			return pluginAgentReport(discoverer.Discover(ctx)), nil
		}
	}
	if opts.Remote != nil {
		// The plugin runtime receives the authority-backed OS: gvim.fs and gvim.proc
		// follow the workspace to the remote host (the authority applies its trust gate
		// and environment), while the documented local-only APIs stay local. A local
		// launch leaves this nil.
		authSlot := b.authSlot
		spawnGate := makeSpawnGate(true, b.root, makeLevelOf(b.trustCtl, b.trustState.Level))
		po.WrapOS = func(local hostos.OS) hostos.OS { return pluginhost.NewAuthorityOS(authSlot, local, spawnGate) }
	}
	if opts.Daemon != nil && opts.PluginDaemon != nil && opts.Remote == nil {
		// One plugin runtime for the whole daemon: this workspace contributes its own OS
		// (root/env/gate forwarded through the runtime's OS mux), editor adapter, keymap
		// builder and gvim.state namespace, and the switch rebinds them.
		b.pluginWS = pluginhost.NewWorkspace(cfg, b.ed, po)
		if b.pluginWS.StateNote != "" {
			_, _ = fmt.Fprintln(os.Stderr, "gvim: warning: "+b.pluginWS.StateNote)
		}
		_, _ = opts.PluginDaemon.Ensure(cfg, po)
		b.pw = opts.PluginDaemon.Wired(b.pluginWS)
	} else {
		b.pw = pluginhost.New(cfg, b.ed, po)
	}
}

// connectAuthority mounts the local session's authority and starts the startup connect
// attempt. The workspace authority is the local backend over the plugin OS (the same
// instance the plugin runtime uses) or the SSH backend of a remote launch. Every
// launch and one-shot goes through it; nothing downstream looks at the kind. A remote
// launch mounted it before the plugin runtime (mountRemoteAuthority), so its plugin
// OS can point at it.
func (b *runtimeBuild) connectAuthority() {
	opts := &b.opts
	if b.authSlot == nil {
		spawnGate := makeSpawnGate(opts.Remote != nil, b.root, makeLevelOf(b.trustCtl, b.trustState.Level))
		authGate := func(ctx context.Context, argv []string, cwd string) error {
			return spawnGate(ctx, argv, cwd, nil)
		}
		b.authSlot = mountAuthority(*opts, b.pw, authGate, spawnGate, localEnvOverlay(b.envProvider, opts.Remote != nil))
	}
	if connectNotice, _ := attemptAuthorityConnect(b.ctx, b.authSlot); connectNotice != "" {
		b.openNotices = append(b.openNotices, connectNotice)
	}
	if opts.Remote != nil && !opts.NoPlugins {
		// Honest startup note for plugin authors and users: the plugin OS follows the
		// workspace for files and processes, but these are local-only and remote file
		// watching is the authority's polling watch, not the local fsnotify.
		b.openNotices = append(b.openNotices, remotePluginAuthorityNotice)
	}
}

// wirePluginPorts gives the root the plugin runtime, editor adapter, daemon-wide host
// mux and keymap builder, and routes the local Git Grep through the authority.
func (b *runtimeBuild) wirePluginPorts() {
	deps, pw := &b.deps, b.pw
	// The one-shots of the session go through the slot: the shell and formatter runner
	// (wireAuthorityUI), the dock terminal through its starter, and the local git grep
	// here. A remote launch already installed the authority-backed Git Grep port in
	// wireRemote; do not overwrite it with the local adapter (which would resolve the
	// local work tree).
	if b.opts.Remote == nil {
		deps.Root.GitGrep = gitGrep{spawnEnv: b.envProvider.Overlay, oneShot: b.authSlot.OneShot()}
	}
	deps.Root.Plugins = pw.Runtime
	deps.Root.PluginEditor = pw.Editor
	if b.pluginWS != nil {
		// The daemon-wide host mux: the model's host calls route to whichever workspace is
		// active, and editor-wide registrations follow a switch.
		deps.Root.PluginShared = b.opts.PluginShared
	}
	deps.Root.PluginKeys = func(name string, bs []pluginapi.Binding) (tui.Keymap, error) {
		km, err := pw.Keys(name, bs)
		if km == nil {
			return nil, err
		}
		return keybindings.NewKeymap(km), err
	}
}
