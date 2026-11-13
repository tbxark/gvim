package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/tbxark/gvim/authority"
	"github.com/tbxark/gvim/config"
	"github.com/tbxark/gvim/editor"
	"github.com/tbxark/gvim/env"
	"github.com/tbxark/gvim/explorer"
	"github.com/tbxark/gvim/finder"
	"github.com/tbxark/gvim/i18n"
	"github.com/tbxark/gvim/internal/configview"
	"github.com/tbxark/gvim/internal/filewatch"
	"github.com/tbxark/gvim/internal/highlight"
	"github.com/tbxark/gvim/internal/keybindings"
	"github.com/tbxark/gvim/internal/pluginhost"
	"github.com/tbxark/gvim/internal/restore"
	"github.com/tbxark/gvim/internal/tui"
	"github.com/tbxark/gvim/internal/tui/icons"
	"github.com/tbxark/gvim/internal/uiadapter"
	"github.com/tbxark/gvim/session"
	"github.com/tbxark/gvim/syntax"
	"github.com/tbxark/gvim/theme"
	"github.com/tbxark/gvim/trust"
)

// buildRuntime opens the files and returns the runtime that shows them, with the
// file index of the root started in the background. It does not start the
// program. A directory root is recorded in the recent list before the files are
// opened. The layered config is loaded first: the file named by opts.Config, or the
// default user file, then the project file for the root. A named config file that
// does not exist is an error. The flags win over the config where both set a value.
//
// What the launch does with the saved session comes from planSession: a session
// launch restores it first (the tabs, or only the unsaved text when the layout
// restore is off) and saves it at exit; a file launch (the default when files are
// given) and --simple restore and save none of it, so the saved session stays as
// it is. Crash recovery and the find history work in every launch. When watching
// is set, the tree root is watched for changes.
//
// The work is a fixed sequence of stages on one runtimeBuild; the order of the
// stages is the order of the side effects, so do not reorder them.
func buildRuntime(ctx context.Context, opts Options, watching bool) (*runtime, error) {
	b := &runtimeBuild{ctx: ctx, opts: opts, watching: watching}
	if err := b.loadConfig(); err != nil {
		return nil, err
	}
	if err := b.openEditor(); err != nil {
		return nil, err
	}
	if err := b.chooseAppearance(); err != nil {
		return nil, err
	}
	b.startWorkspace()
	b.newDeps()
	b.wireEnvironment()
	b.mountRemoteAuthority()
	b.wireSettingsDeps()
	b.wireLSP()
	b.wireRootDeps()
	b.buildPlugins()
	b.connectAuthority()
	b.wireRemote()
	b.restoreRemoteSession()
	b.wireRemoteWatch()
	b.wirePluginPorts()
	b.wireSettingsService()
	b.wireTrustLSP()
	b.wireLSPExtras()
	b.collectNotices()
	b.newModel()
	b.wireDock()
	b.wireAuthorityUI()
	b.restoreTerminal()
	return b.newRuntime(), nil
}

// runtimeBuild is the state the stages of buildRuntime share. A field is set by the
// stage that names it and read by the later ones; opts and watching are the launch
// inputs, which the first stage adjusts for a remote or sessionless launch.
type runtimeBuild struct {
	ctx      context.Context
	opts     Options
	watching bool

	// Config, trust and the session plan (loadConfig).
	root         string
	denoRoot     string // the Deno auto-selection workspace root; the remote root once known
	trustStore   *trust.Store
	trustState   trust.State
	trustWarning string
	cfg          config.Config
	diags        []config.Diagnostic
	plan         sessionPlan
	fileLaunch   bool // the invocation property plugins read; fixed before a remote plan reset
	// deferRemoteRestore marks a daemon-hosted remote workspace: its session is restored
	// after the authority connects.
	deferRemoteRestore bool

	// The editor and what the session restore left behind (openEditor).
	tree                 *explorer.Tree
	edOpts               editor.Options
	interval             time.Duration
	ed                   *editor.Editor
	sess                 session.Session
	restored             bool
	remoteRestoreSkipped bool // the saved remote session was kept because the authority is down
	failed               []restore.Unrestored
	sidebarState         *tui.SidebarState
	openNotices          []string
	fullLoads            []editor.ErrLargeNeedsFullLoad
	recPort              tui.RecoveryPort
	recEvery             time.Duration
	recNotice            string

	// Keymap and theme (chooseAppearance).
	choice    keymapChoice
	themeCat  *themeCatalog
	th        theme.Theme
	themeRef  string
	themeWarn string

	// The file index, watcher, projects and log file (startWorkspace).
	files     *finder.Index
	watcher   *filewatch.Watcher
	events    <-chan filewatch.Changed
	proj      *uiadapter.Projects
	logFile   *fileLog
	logNotice string

	// The UI dependencies the model is built from (newDeps and every wire stage).
	deps     tui.Deps
	trustCtl *trustController

	// The project environment (wireEnvironment). envRemote, envShell and remoteEnv are
	// set for a remote launch only; localWatchRetry for a local launch that watches.
	authSlot           authority.Slot
	envProvider        workspaceEnv
	envRemote          *env.Remote
	envShell           *remoteEnvShell
	envWatch           envWatchReplanner
	remoteEnv          *remoteEnvWatch
	localWatchRetry    func()
	localWatchRetryObj *localEnvWatchRetry

	icon icons.Resolution
	keys *keybindings.Store

	// The plugin wiring (buildPlugins).
	pw              *pluginhost.Wiring
	pluginWS        *pluginhost.Workspace
	pluginStateFile string

	// The remote pieces (wireRemote, wireRemoteWatch); nil for a local launch.
	remoteFiles          *finder.Index
	remoteGit            *remoteGitStatus
	remoteExplorerLoader *authorityDirLoader
	remoteRecoveryPath   string
	remoteFileWatch      *remoteWatch

	// The settings service and the LSP helpers built on it.
	settingsSvc *settingsService
	lspBase     *lspBaselineUpdater
	denoAuto    *denoAutoSelector

	model *tui.Model
}

// loadConfig resolves the root, the trust decision, the layered config and the
// session plan, and applies the config-derived process settings (SSH policy, locale).
func (b *runtimeBuild) loadConfig() error {
	opts := &b.opts
	if err := configview.CheckFile(opts.Config); err != nil {
		return err
	}
	root, err := resolveRoot(opts.Root)
	if err != nil {
		return err
	}
	if opts.Remote != nil {
		// A remote launch keeps a local process root for the local-only pieces
		// (plugin runtime, shells) but opens no local files and no explorer. The remote
		// file access is installed after the SSH slot connects (setupRemoteEditing), and
		// the remote file is opened by the root's first command, off the update loop.
		// Watching and crash recovery are scoped off for a remote launch: a local
		// watcher would watch nothing (the remote paths do not exist locally), and a
		// local recovery copy would write remote text to the local cache.
		root = remoteLocalRoot()
		b.watching = false
	}
	b.root = root
	// The Deno auto-selection workspace root: the local root, replaced by the expanded
	// remote root below for a remote launch.
	b.denoRoot = root
	// The trust decision gates the project config layer, so it is resolved
	// before the config is loaded: an untrusted workspace is configured by the
	// user's own file only.
	if opts.Remote != nil {
		// A remote workspace's decision is keyed by the remote target, not by the local
		// process root: the same target keeps its level across restarts and reconnects.
		b.trustStore, b.trustState, b.trustWarning = openRemoteTrust(opts.Remote)
	} else {
		b.trustStore, b.trustState, b.trustWarning = openTrust(root)
	}
	cfg, diags, err := config.Load(config.Options{
		Root:      root,
		UserFile:  opts.Config,
		Untrusted: !b.trustState.AllowsProjectConfig(),
	})
	if err != nil {
		return err
	}
	b.cfg, b.diags = cfg, diags
	printDiagnostics(os.Stderr, diags)
	if !cfg.SaveSession {
		opts.SessionDir = ""
	} else if opts.SessionDir == "" && opts.Remote == nil && opts.Daemon == nil {
		opts.SessionDir = filepath.Join(root, ".gvim", "session")
	}
	// The SSH policy is config-derived: strict known_hosts, the safe host-key algorithm
	// order and no keyring use unless the user opted in (authority_app.go).
	opts.SSHSecurity = sshSecurityFromConfig(cfg.SSH)
	// Resolve the UI language before anything builds strings or the plugin runtime: the
	// config locale wins, then LC_ALL/LC_MESSAGES/LANG, then English. The Settings row
	// applies later changes live through settingsService.Apply.
	i18n.SetLocale(i18n.Resolve(cfg.Locale, os.Getenv))
	if b.trustWarning != "" {
		_, _ = fmt.Fprintln(os.Stderr, "gvim: warning: "+b.trustWarning)
	}
	// What this launch does with the saved session: restored, restored without
	// its layout (the unsaved text only), or not at all, and whether it is saved
	// and hot-exited when the program ends.
	b.plan = planSession(*opts, cfg)
	// fileLaunch is the invocation property plugins read; it is fixed here, before the
	// remote launch's session plan is reset, so an explicit --simple on a remote target
	// still reports a file launch while a normal remote target does not name files.
	b.fileLaunch = b.plan.fileLaunch
	// A direct remote launch keeps no local session (the local session must not come back
	// under an SSH workspace). A daemon-hosted remote workspace is different: its session
	// lives in the workspace session directory and is restored after the authority
	// connects (the files open through the remote document access), then captured back on
	// exit through the workspace capture path.
	b.deferRemoteRestore = opts.Remote != nil && opts.Daemon != nil
	if opts.Remote != nil && !b.deferRemoteRestore {
		b.plan = sessionPlan{}
	}
	return nil
}

// openEditor builds the explorer tree and the editor, restores the saved session,
// opens the files named on the command line and runs crash recovery.
func (b *runtimeBuild) openEditor() error {
	opts, cfg, root, plan := &b.opts, b.cfg, b.root, b.plan
	tree, err := explorer.Open(root)
	if err != nil {
		return err
	}
	b.tree = tree
	tree.SetShowHidden(cfg.ExplorerShowHidden)
	tree.SetShowIgnored(cfg.ExplorerShowIgnored)
	// Without files the editor starts empty, and the welcome screen shows.
	edOpts := editorOptions(cfg, len(opts.Files) == 0 && !opts.StdinLaunch())
	// Daemon-hosted local workspaces share one cross-workspace buffer registry (#19b
	// bounded slice). A direct launch and a remote workspace get nil, so their editors
	// never share (remote files live on another host).
	edOpts.Shared = sharedBuffersFor(*opts)
	b.interval = autoSaveInterval(opts.AutoSaveInterval, cfg)
	edOpts.AutoSaveInterval = b.interval
	edOpts.Root = root
	edOpts.SessionDir = opts.SessionDir
	edOpts.ReadOnly = opts.ReadOnly
	b.edOpts = edOpts
	ed := newEditor(edOpts)
	b.ed = ed
	ed.SetFormatters(formattersFor(cfg, lookPath))
	ed.SetLanguageNamer(syntax.CachedFileLanguage())
	ed.SetLanguageSettings(languageSettings(cfg))
	ed.SetStringContext(highlight.NewStringContext())
	ed.SetNonCodeContext(highlight.NewNonCodeContext())
	ed.SetEphemeralFilePatterns(cfg.EphemeralFilePatterns)
	ed.SetRulers(cfg.Rulers)
	if plan.load {
		s, ok, err := session.Load(opts.SessionDir)
		switch {
		case err != nil:
			_, _ = fmt.Fprintln(os.Stderr, "gvim: warning: session:", err)
		case ok:
			b.sess, b.restored = s, true
			switch {
			case b.deferRemoteRestore:
				// The files are remote: restore them after the authority connects and
				// the remote file access is installed, below. The pure layout/sidebar
				// state is available now for the model options.
				b.sidebarState = restore.SidebarState(s)
			case plan.tabs:
				b.failed = restore.RestoreSession(ed, opts.SessionDir, s)
				tree.SetShowHidden(s.ShowHidden)
				tree.SetShowIgnored(s.ShowIgnored)
				// The sidebar UI state is restored with the saved layout; a hot-exit
				// restore and a file launch keep the config defaults.
				b.sidebarState = restore.SidebarState(s)
			case plan.hot:
				// restore_previous_session off (or --no-restore): the layout and
				// the clean tabs stay out, but the unsaved text comes back.
				b.failed = restore.RestoreHotExit(ed, opts.SessionDir, s)
			}
		}
	}
	// The piped content is a [stdin] buffer, opened first like Fresh's file-born stdin
	// buffer. It has no path (Save goes through Save As) and is unmodified on open.
	if opts.StdinLaunch() {
		ed.OpenStdinText(string(opts.Stdin), editor.StdinOptions{Label: "[stdin]", Language: stdinLanguage(*opts)})
	}
	b.openNotices, b.fullLoads, err = openTargets(ed, opts.Files, plan.tabs)
	if err != nil {
		return err
	}
	// Counted before crash recovery, which adds tabs of its own after the two files.
	if opts.Diff && len(ed.Tabs()) != 2 {
		return errors.New("--diff needs two different files")
	}
	// Local crash recovery runs here as always. A remote launch gets its own staged
	// recovery copy after the remote file access is installed (see wireRemote):
	// remote buffer text is staged under the remote workspace's local session directory
	// with the remote paths recorded, never as a local file the workspace names.
	if opts.Remote == nil {
		b.recPort, b.recEvery = restore.RecoveryPort(ed, cfg, opts.SessionDir, b.watching)
		b.recNotice = restore.RecoverFromCrash(ed, opts.SessionDir, b.watching, b.recPort)
	}
	return nil
}

// chooseAppearance resolves the keymap and the theme.
func (b *runtimeBuild) chooseAppearance() error {
	opts, cfg := &b.opts, b.cfg
	choice, err := runKeymap(opts.Keymap, cfg)
	if err != nil {
		return err
	}
	b.choice = choice
	// The theme registry owns bundled and user themes, the config theme forms,
	// extends and fallbacks. A bad user theme is a warning, never a startup
	// failure.
	themeReg := theme.NewRegistry(userThemeDir())
	_ = themeReg.Reload()
	// URL themes are fetched at startup only (the bounded request runs before the
	// program exists, never in Update): the registry reads the app-written cache for
	// runtime theme switches, so the Settings path never waits on the network.
	if dir, err := themeURLCacheDir(); err == nil {
		themeReg.SetURLCacheDir(dir)
	}
	b.themeCat = newThemeCatalog(themeReg)
	b.th, b.themeRef, b.themeWarn, err = runTheme(b.ctx, opts.Theme, cfg, themeReg)
	if err != nil {
		// A URL theme that cannot be fetched or read from its cache falls back to the
		// default theme with a clear notice instead of failing the start (an offline
		// first run must still open the editor); other theme errors stay fatal as
		// before.
		if theme.IsURLForm(effectiveThemeRef(opts.Theme, cfg)) {
			b.themeWarn = fmt.Sprintf("%v; using %q", err, "dark")
			b.th, err = themeReg.Load("dark")
			b.themeRef = "dark"
		}
		if err != nil {
			return err
		}
	}
	if b.themeWarn != "" {
		_, _ = fmt.Fprintln(os.Stderr, "gvim: warning: "+b.themeWarn)
	}
	return nil
}

// startWorkspace starts the file index, the file watcher, the projects and the log
// file.
func (b *runtimeBuild) startWorkspace() {
	if b.opts.Remote != nil {
		// A remote launch never enumerates the local disk: the real remote index is
		// created once the authority slot is connected, below, and this idle index is
		// the placeholder until then.
		b.files = finder.Idle()
	} else {
		b.files = uiadapter.StartFiles(b.ctx, b.root)
	}
	b.watcher, b.events = startWatcher(b.watching)
	b.proj = uiadapter.NewProjects(b.ctx, b.watcher, b.root, b.files)
	b.logFile, b.logNotice = startLogFile(b.opts.LogFile, b.diags)
	if b.logFile != nil {
		b.opts.pluginLog = b.logFile.Plugin
	}
}

// newDeps creates the UI dependencies and the trust controller.
func (b *runtimeBuild) newDeps() {
	opts, cfg := &b.opts, b.cfg
	b.deps = uiadapter.NewDeps(b.ed, b.choice.km, b.th, b.tree, b.files)
	// Expose the startup invocation property to plugins (gvim.workspace.file_launch).
	b.deps.FileLaunch = b.fileLaunch
	// Remote large files read windows through the app's authority-backed loader in
	// commands; a local session reports no async large document and arms no polling.
	b.deps.LargeWindows = newLargeWindows(b.ed)
	// The release check behind the status row element {update}: one bounded
	// background request at startup, a daily debounce, and no error path the user
	// must handle (docs/settings.md).
	b.deps.Updates = newUpdateHooks(&cfg)
	if b.trustStore != nil {
		b.trustCtl = newTrustController(b.trustStore, b.trustState)
		if opts.Remote != nil {
			b.trustCtl.recordKey = remoteTrustKey(opts.Remote)
		}
		b.deps.Trust = b.trustCtl
	}
}
