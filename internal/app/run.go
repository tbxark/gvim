package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/x/term"

	"github.com/tbxark/gvim/authority"
	"github.com/tbxark/gvim/commands"
	"github.com/tbxark/gvim/config"
	"github.com/tbxark/gvim/editor"
	"github.com/tbxark/gvim/env"
	"github.com/tbxark/gvim/explorer"
	"github.com/tbxark/gvim/finder"
	"github.com/tbxark/gvim/i18n"
	"github.com/tbxark/gvim/internal/configview"
	"github.com/tbxark/gvim/internal/daemon"
	"github.com/tbxark/gvim/internal/filewatch"
	"github.com/tbxark/gvim/internal/highlight"
	"github.com/tbxark/gvim/internal/keybindings"
	"github.com/tbxark/gvim/internal/lsphost"
	"github.com/tbxark/gvim/internal/pluginhost"
	"github.com/tbxark/gvim/internal/restore"
	"github.com/tbxark/gvim/internal/tui"
	"github.com/tbxark/gvim/internal/tui/find"
	"github.com/tbxark/gvim/internal/tui/icons"
	"github.com/tbxark/gvim/internal/uiadapter"
	"github.com/tbxark/gvim/keymap"
	"github.com/tbxark/gvim/lsp"
	"github.com/tbxark/gvim/plugin/hostos"
	"github.com/tbxark/gvim/pluginapi"
	"github.com/tbxark/gvim/session"
	"github.com/tbxark/gvim/syntax"
	"github.com/tbxark/gvim/theme"
	"github.com/tbxark/gvim/trust"
	"github.com/tbxark/gvim/workspace"
)

// Constructors that buildRuntime calls. Tests replace them to see the values the
// config produced. In production they are the real constructors.
var (
	newEditor = editor.New
	newUI     = tui.New
	newLSP    = lsphost.NewFor
)

// Run validates opts, builds the terminal UI and runs it in the alternate
// screen until the user quits. Canceling ctx stops the program, and Run then
// returns the context's error. When the launch is a session launch (see
// planSession) the session is saved when the program ends, to Options.SessionDir,
// or to the default directory when that is empty.
func Run(ctx context.Context, opts Options) error {
	if err := opts.Validate(); err != nil {
		return err
	}
	// An attach is a terminal proxy: it builds no editor and touches no session.
	if opts.Daemon != nil && opts.Daemon.Mode == DaemonAttach {
		return attachToDaemon(ctx, opts)
	}
	// A bare launch may join a daemon that already hosts this workspace when the user
	// config turns on auto_attach (default off). It never starts a daemon, and it runs
	// before the terminal check because the attach proxy owns the terminal.
	if opts.Daemon == nil {
		if attached, err := maybeAutoAttach(ctx, opts); attached {
			return err
		}
	}
	serveDaemon := opts.Daemon != nil && opts.Daemon.Mode == DaemonServe
	if !serveDaemon {
		if err := requireTerminal(os.Stdout); err != nil {
			return err
		}
		// The daemon owns the workspace's session: a bare launch refuses instead of
		// becoming a second editor of the same workspace.
		if err := refuseIfDaemonRunning(opts); err != nil {
			return err
		}
	}
	// A pipe on standard input with nothing named to open is buffer content, as in
	// Fresh ("git show | gvim"): consume it even without --stdin. A folder, a remote
	// target or --restore names what to open, as files do, so their stdin stays unread.
	// A launch that asks for stdin explicitly must have a pipe: a terminal would block
	// forever on a read.
	if !serveDaemon && !opts.StdinLaunch() && len(opts.Files) == 0 && opts.Root == "" &&
		opts.Remote == nil && !opts.Restore && !term.IsTerminal(os.Stdin.Fd()) {
		opts.ReadStdin = true
		opts.ReadOnly = true
	}
	var stdinInput *os.File
	if opts.ReadStdin {
		if term.IsTerminal(os.Stdin.Fd()) {
			return errors.New("stdin: no data piped to standard input (--stdin or - needs a pipe)")
		}
		data, err := readStdinContext(ctx, os.Stdin, MaxStdinBytes)
		if err != nil {
			return err
		}
		opts.Stdin = data
		if stdinHasNUL(data) {
			_, _ = fmt.Fprintln(os.Stderr, "gvim: warning: stdin input contains NUL bytes; opened as text")
		}
		in, err := stdinInputTTY()
		if err != nil {
			return err
		}
		defer func() { _ = in.Close() }()
		stdinInput = in
	}
	var srv *daemon.Server
	if serveDaemon {
		// The daemon's one shared plugin runtime and host mux: every local workspace model
		// is built against it and binds itself on a switch (#19b shared-runtime slice).
		opts.PluginDaemon = pluginhost.NewDaemon()
		opts.PluginShared = tui.NewPluginShared()
		namedRoot, err := resolveRoot(opts.Root)
		if err != nil {
			return err
		}
		name := opts.Daemon.Name
		if name == "" {
			name = daemon.DefaultName(namedRoot)
		}
		ln, path, err := daemon.Listen(name)
		if err != nil {
			return err
		}
		// Claim the workspace for this daemon until the checked exit save is done: a
		// direct launch reads the mark (daemon_hosts.go) and refuses instead of becoming
		// a second writer of the daemon's unsaved buffers. The mark covers an explicitly
		// named daemon too. A failed registration refuses to start (and closes the
		// listener): without the mark a direct launch could miss this daemon and race it,
		// which is the data loss this daemon mode rule exists to prevent.
		canonicalRoot := namedRoot
		if resolved, cerr := daemon.CanonicalRoot(namedRoot); cerr == nil {
			canonicalRoot = resolved
		}
		if err := writeHostMark(canonicalRoot, name); err != nil {
			_ = ln.Close()
			return fmt.Errorf("daemon: cannot record the workspace host mark (a direct launch would miss this daemon, so it refuses to start): %w", err)
		}
		srv = daemon.NewServer(ln, path)
		defer func() { _ = srv.Close() }()
		opts.Daemon.Detach = srv.Detach
		defer clearHostMark(canonicalRoot, name)
		_, _ = fmt.Fprintf(os.Stderr, "gvim: daemon listening on %s\n", path)
	}
	if srv != nil {
		// The daemon serves the workspace layer: one program over a host model that
		// swaps workspace models, one runtime per materialized workspace.
		return runDaemonHost(ctx, srv, opts)
	}
	rt, err := buildRuntime(ctx, opts, true)
	if err != nil {
		return err
	}
	defer rt.close()
	var runErr error
	switch {
	case stdinInput != nil:
		runErr = tui.RunWithInput(ctx, rt.model, stdinInput)
	default:
		runErr = tui.Run(ctx, rt.model)
	}
	if runErr != nil {
		rt.log.write("error", runErr.Error())
	}
	// An error ends the program with the user's text unsettled: a signal, not a quit.
	rt.interrupted = runErr != nil
	rt.exit()
	return runErr
}

// errNotTerminal is the error of a run whose standard output is not a terminal. The
// terminal UI would write its escape sequences into the file or pipe and draw no frame,
// while the keyboard still reads from the terminal, so the program would be invisible and
// could not be told to quit.
var errNotTerminal = errors.New("standard output is not a terminal")

// requireTerminal returns errNotTerminal when f is not a terminal.
func requireTerminal(f *os.File) error {
	if f == nil || !term.IsTerminal(f.Fd()) {
		return errNotTerminal
	}
	return nil
}

// exit ends the session in order. The session is saved first, because it is local
// and fast. Then the language servers stop and the dock closes, each with its own
// deadline, so a hung server or shell delays only its own step and never the save.
func (r *runtime) exit() {
	if r.skipSave {
		// No clean save happened, so this exit must not delete the recovery copy.
		r.interrupted = true
	}
	if err := r.saveSearch(); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, "gvim: warning: search history:", err)
	}
	if r.interrupted && r.sessionDir == "" && r.recoveryWriter != nil {
		// No session is saved, so the recovery copy is the only one of the unsaved text.
		// Write it once more, as it stands now, and keep it.
		_ = r.recoveryWriter.Plan()()
	}
	if err := r.save(); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, "gvim: warning: session:", err)
	} else if r.recovery != "" && (!r.interrupted || r.sessionDir != "") && !r.skipSave {
		if restore.ClearRecovery(r.recovery) == nil {
			_ = os.RemoveAll(r.recovery)
		}
	}
	// The model closes first: the plugins' shutdown handlers may still read the documents.
	// The files of large documents are closed after the session is saved: the save reads
	// the documents.
	r.model.Close()
	r.ed.Close()
}

// shutdown is exit followed by close: the end of a daemon workspace's runtime, which
// no deferred close in Run covers.
func (r *runtime) shutdown() {
	r.exit()
	r.close()
}

// runtime is a built session: the UI model, the editor it shows, and the resources
// that run until the program ends. Run saves the session, stops the servers, closes
// the dock and then closes the resources.
type runtime struct {
	model      *tui.Model
	ed         *editor.Editor
	projects   *uiadapter.Projects
	watcher    *filewatch.Watcher // nil when no watch was started
	sessionDir string             // empty when the session is neither restored nor saved
	// keepSavedSession marks a daemon-hosted remote workspace whose authority was
	// disconnected at start: its saved session (on disk) must survive this runtime, so
	// the capture/exit path writes nothing for it.
	keepSavedSession bool
	// pluginStateFile is the workspace-scoped gvim.state file of a daemon workspace
	// (sessions/<stable-id>/plugin-state.json), or "" for a direct launch's editor-wide
	// per-plugin state files. Observability/test seam; the plugin runtime owns the writes.
	pluginStateFile string
	// daemonMaterializedCap is daemon.materialized_workspaces of this session's config:
	// the daemon's cap on warm local workspaces, read by the workspace manager. It is
	// only meaningful for a daemon host.
	daemonMaterializedCap int
	// pluginWS is this LOCAL daemon workspace's plugin parts (its OS/root/env/gate, editor
	// adapter, keymap builder and state namespace) bound to the daemon's one shared plugin
	// runtime (#19b shared-runtime slice). Nil for a direct launch and for remote/authority
	// workspaces (which keep their own runtime this slice).
	pluginWS  *pluginhost.Workspace
	searchDir string // where the find history and toggles are kept; empty for nowhere
	// stashDir is the session directory even when the session is not saved. A stash of
	// the unsaved edits of a large file can be written there without a session file.
	stashDir string
	// kept are the saved files whose unsaved text could not be restored. The session
	// save lists them, so their stash files survive until the next start.
	kept []session.FileState
	// recovery is the crash-recovery directory, which a clean exit empties. It is empty
	// when there is none.
	recovery string
	// recoveryWriter writes the recovery copy; it is nil when there is none.
	recoveryWriter tui.RecoveryPort
	// interrupted is set when the program ended by a signal and not a quit. The recovery
	// copy then stays when no session is saved to hold the unsaved text.
	interrupted bool
	// skipSave is set by the daemon exit path when a capture write is still in flight at
	// the exit deadline: that write holds the newest snapshot and owns the workspace lock,
	// so the exit must not write the session itself (it would race it) and must keep the
	// crash-recovery copy instead of clearing it.
	skipSave bool
	// log is the --log-file writer; nil when there is none.
	log *fileLog
	// auth is the workspace authority slot of the session (docs/authority.md): the
	// local backend, or the SSH backend of a remote launch.
	auth authority.Slot
	// remoteWatch is the remote external-change bridge of a remote launch; nil for a
	// local launch, whose filewatch.Watcher is in watcher.
	remoteWatch *remoteWatch
	// remoteFiles is the remote workspace's file index of a remote launch; nil for a
	// local launch, whose index is owned by Projects.
	remoteFiles *finder.Index
}

// close stops the file index of the current root, the watcher and the remote watch.
func (r *runtime) close() {
	r.projects.Close()
	if r.watcher != nil {
		_ = r.watcher.Close()
	}
	if r.remoteWatch != nil {
		_ = r.remoteWatch.Close()
	}
	if r.remoteFiles != nil {
		r.remoteFiles.Close()
	}
	if r.auth != nil {
		_ = r.auth.Close()
	}
	r.log.Close()
}

// save writes the session, when a session directory is set.
func (r *runtime) save() error {
	if r.sessionDir == "" || r.skipSave {
		return nil
	}
	// The pane layout is captured with the session; SplitState reads the model
	// without switching tabs, and a stale layout is dropped by the writer.
	if err := restore.WriteSessionWithLayout(r.ed, r.sessionDir, r.model.ExplorerState(), r.kept, r.model.SplitState()); err != nil {
		return err
	}
	// The terminal transcript rides next to the session file: a restored session
	// brings it back read-only, with no process running.
	return restore.SaveTerminal(r.sessionDir, r.model.TerminalState())
}

// saveSearch writes the find histories and toggles to the session directory. They
// are kept after a start with file arguments too, which saves no session.
func (r *runtime) saveSearch() error {
	st, ok := r.model.SearchState()
	if !ok || r.searchDir == "" {
		return nil
	}
	return session.SaveSearch(r.searchDir, session.Search{
		Find: st.Find, Replace: st.Replace,
		Case: st.Case, Word: st.Word, Regex: st.Regex, Confirm: st.ConfirmEach,
	})
}

// loadSearch gives the model the find state saved by an earlier run.
func loadSearch(model *tui.Model, dir string) {
	if dir == "" {
		return
	}
	s, ok, err := session.LoadSearch(dir)
	if err != nil {
		_, _ = fmt.Fprintln(os.Stderr, "gvim: warning: search history:", err)
		return
	}
	if ok {
		model.SetSearchState(find.State{
			Find: s.Find, Replace: s.Replace,
			Case: s.Case, Word: s.Word, Regex: s.Regex, ConfirmEach: s.Confirm,
		})
	}
}

// stashForQuit writes the unsaved edits of the large documents before the program
// ends, and returns the error that stops the quit when it cannot. Such a document is
// not held whole, so its edits survive only as a stash. With a saved session the
// stash is part of the session. Without one, when files were given on the command
// line, each stash is a file of its own in the session directory.
func (r *runtime) stashForQuit() error {
	var docs []*editor.Document
	for _, d := range r.ed.UnsavedDocs() {
		if d.Large() {
			docs = append(docs, d)
		}
	}
	if len(docs) == 0 {
		return nil
	}
	if r.sessionDir != "" {
		return r.save()
	}
	if r.stashDir == "" {
		return errors.New("there is no session directory to keep them in")
	}
	for _, d := range docs {
		st, ok := d.OverlayStash()
		if !ok {
			continue
		}
		if _, err := session.StashOverlay(r.stashDir, st); err != nil {
			return err
		}
		if failed, ok := d.FailedStash(); ok {
			if _, err := session.StashOverlay(r.stashDir, failed); err != nil {
				return err
			}
		}
	}
	return nil
}

// buildModel builds the UI model and returns it with the file index of the root. It
// starts no watcher and saves nothing, so it is for the callers that only need the
// model. The caller closes the index.
func buildModel(ctx context.Context, opts Options) (*tui.Model, *finder.Index, error) {
	rt, err := buildRuntime(ctx, opts, false)
	if err != nil {
		return nil, nil, err
	}
	return rt.model, rt.projects.Index(), nil
}

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
func buildRuntime(ctx context.Context, opts Options, watching bool) (rt *runtime, err error) {
	if err := configview.CheckFile(opts.Config); err != nil {
		return nil, err
	}
	root, err := resolveRoot(opts.Root)
	if err != nil {
		return nil, err
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
		watching = false
	}
	// The Deno auto-selection workspace root: the local root, replaced by the expanded
	// remote root below for a remote launch.
	denoRoot := root
	// The trust decision gates the project config layer, so it is resolved
	// before the config is loaded: an untrusted workspace is configured by the
	// user's own file only.
	var trustStore *trust.Store
	var trustState trust.State
	var trustWarning string
	if opts.Remote != nil {
		// A remote workspace's decision is keyed by the remote target, not by the local
		// process root: the same target keeps its level across restarts and reconnects.
		trustStore, trustState, trustWarning = openRemoteTrust(opts.Remote)
	} else {
		trustStore, trustState, trustWarning = openTrust(root)
	}
	cfg, diags, err := config.Load(config.Options{
		Root:      root,
		UserFile:  opts.Config,
		Untrusted: !trustState.AllowsProjectConfig(),
	})
	if err != nil {
		return nil, err
	}
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
	if trustWarning != "" {
		_, _ = fmt.Fprintln(os.Stderr, "gvim: warning: "+trustWarning)
	}
	// What this launch does with the saved session: restored, restored without
	// its layout (the unsaved text only), or not at all, and whether it is saved
	// and hot-exited when the program ends.
	plan := planSession(opts, cfg)
	// fileLaunch is the invocation property plugins read; it is fixed here, before the
	// remote launch's session plan is reset, so an explicit --simple on a remote target
	// still reports a file launch while a normal remote target does not name files.
	fileLaunch := plan.fileLaunch
	// A direct remote launch keeps no local session (the local session must not come back
	// under an SSH workspace). A daemon-hosted remote workspace is different: its session
	// lives in the workspace session directory and is restored after the authority
	// connects (the files open through the remote document access), then captured back on
	// exit through the workspace capture path.
	deferRemoteRestore := opts.Remote != nil && opts.Daemon != nil
	if opts.Remote != nil && !deferRemoteRestore {
		plan = sessionPlan{}
	}
	tree, err := explorer.Open(root)
	if err != nil {
		return nil, err
	}
	tree.SetShowHidden(cfg.ExplorerShowHidden)
	tree.SetShowIgnored(cfg.ExplorerShowIgnored)
	// Without files the editor starts empty, and the welcome screen shows.
	edOpts := editorOptions(cfg, len(opts.Files) == 0 && !opts.StdinLaunch())
	// Daemon-hosted local workspaces share one cross-workspace buffer registry (#19b
	// bounded slice). A direct launch and a remote workspace get nil, so their editors
	// never share (remote files live on another host).
	edOpts.Shared = sharedBuffersFor(opts)
	interval := autoSaveInterval(opts.AutoSaveInterval, cfg)
	edOpts.AutoSaveInterval = interval
	edOpts.Root = root
	edOpts.SessionDir = opts.SessionDir
	edOpts.ReadOnly = opts.ReadOnly
	ed := newEditor(edOpts)
	ed.SetFormatters(formattersFor(cfg, lookPath))
	ed.SetLanguageNamer(syntax.CachedFileLanguage())
	ed.SetLanguageSettings(languageSettings(cfg))
	ed.SetStringContext(highlight.NewStringContext())
	ed.SetNonCodeContext(highlight.NewNonCodeContext())
	ed.SetEphemeralFilePatterns(cfg.EphemeralFilePatterns)
	ed.SetRulers(cfg.Rulers)
	var sess session.Session
	restored := false
	remoteRestoreSkipped := false
	var failed []restore.Unrestored
	var sidebarState *tui.SidebarState
	if plan.load {
		s, ok, err := session.Load(opts.SessionDir)
		switch {
		case err != nil:
			_, _ = fmt.Fprintln(os.Stderr, "gvim: warning: session:", err)
		case ok:
			sess, restored = s, true
			switch {
			case deferRemoteRestore:
				// The files are remote: restore them after the authority connects and
				// the remote file access is installed, below. The pure layout/sidebar
				// state is available now for the model options.
				sidebarState = restore.SidebarState(s)
			case plan.tabs:
				failed = restore.RestoreSession(ed, opts.SessionDir, s)
				tree.SetShowHidden(s.ShowHidden)
				tree.SetShowIgnored(s.ShowIgnored)
				// The sidebar UI state is restored with the saved layout; a hot-exit
				// restore and a file launch keep the config defaults.
				sidebarState = restore.SidebarState(s)
			case plan.hot:
				// restore_previous_session off (or --no-restore): the layout and
				// the clean tabs stay out, but the unsaved text comes back.
				failed = restore.RestoreHotExit(ed, opts.SessionDir, s)
			}
		}
	}
	// The piped content is a [stdin] buffer, opened first like Fresh's file-born stdin
	// buffer. It has no path (Save goes through Save As) and is unmodified on open.
	if opts.StdinLaunch() {
		ed.OpenStdinText(string(opts.Stdin), editor.StdinOptions{Label: "[stdin]", Language: stdinLanguage(opts)})
	}
	openNotices, fullLoads, err := openTargets(ed, opts.Files, plan.tabs)
	if err != nil {
		return nil, err
	}
	// Counted before crash recovery, which adds tabs of its own after the two files.
	if opts.Diff && len(ed.Tabs()) != 2 {
		return nil, errors.New("--diff needs two different files")
	}
	// Local crash recovery runs here as always. A remote launch gets its own staged
	// recovery copy after the remote file access is installed (see the remote block):
	// remote buffer text is staged under the remote workspace's local session directory
	// with the remote paths recorded, never as a local file the workspace names.
	var recPort tui.RecoveryPort
	var recEvery time.Duration
	var recNotice string
	if opts.Remote == nil {
		recPort, recEvery = restore.RecoveryPort(ed, cfg, opts.SessionDir, watching)
		recNotice = restore.RecoverFromCrash(ed, opts.SessionDir, watching, recPort)
	}
	choice, err := runKeymap(opts.Keymap, cfg)
	if err != nil {
		return nil, err
	}
	km := choice.km
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
	themeCat := newThemeCatalog(themeReg)
	th, themeRef, themeWarn, err := runTheme(ctx, opts.Theme, cfg, themeReg)
	if err != nil {
		// A URL theme that cannot be fetched or read from its cache falls back to the
		// default theme with a clear notice instead of failing the start (an offline
		// first run must still open the editor); other theme errors stay fatal as
		// before.
		if theme.IsURLForm(effectiveThemeRef(opts.Theme, cfg)) {
			themeWarn = fmt.Sprintf("%v; using %q", err, "dark")
			th, err = themeReg.Load("dark")
			themeRef = "dark"
		}
		if err != nil {
			return nil, err
		}
	}
	if themeWarn != "" {
		_, _ = fmt.Fprintln(os.Stderr, "gvim: warning: "+themeWarn)
	}
	var files *finder.Index
	if opts.Remote != nil {
		// A remote launch never enumerates the local disk: the real remote index is
		// created once the authority slot is connected, below, and this idle index is
		// the placeholder until then.
		files = finder.Idle()
	} else {
		files = uiadapter.StartFiles(ctx, root)
	}
	watcher, events := startWatcher(watching)
	proj := uiadapter.NewProjects(ctx, watcher, root, files)
	logFile, logNotice := startLogFile(opts.LogFile, diags)
	if logFile != nil {
		opts.pluginLog = logFile.Plugin
	}
	deps := uiadapter.NewDeps(ed, km, th, tree, files)
	// Expose the startup invocation property to plugins (gvim.workspace.file_launch).
	deps.FileLaunch = fileLaunch
	// Remote large files read windows through the app's authority-backed loader in
	// commands; a local session reports no async large document and arms no polling.
	deps.LargeWindows = newLargeWindows(ed)
	// The release check behind the status row element {update}: one bounded
	// background request at startup, a daily debounce, and no error path the user
	// must handle (docs/settings.md).
	deps.Updates = newUpdateHooks(&cfg)
	var trustCtl *trustController
	if trustStore != nil {
		trustCtl = newTrustController(trustStore, trustState)
		if opts.Remote != nil {
			trustCtl.recordKey = remoteTrustKey(opts.Remote)
		}
		deps.Trust = trustCtl
	}
	// The project environment is detected once at workspace open (shallow, no
	// execution) and its activation is applied to every process the editor spawns.
	// Activation itself is host-gated on the live trust level.
	levelOf := func() trust.Level {
		if trustCtl != nil {
			return trustCtl.State().Level
		}
		return trustState.Level
	}
	trustedLive := func() bool { return levelOf() == trust.Trusted }
	// The project environment: a local launch detects and activates locally; a remote
	// launch uses env.Remote, whose marker probe and activation run on the REMOTE host
	// through the authority's one-shot surface, so the local overlay (and any secret in
	// it) never reaches the remote host (#R13-13, Fresh's backend-agnostic provider).
	var authSlot authority.Slot
	var envRemote *env.Remote
	var envShell *remoteEnvShell
	var envWatch envWatchReplanner
	var envProvider workspaceEnv
	var localWatchRetry func()
	var localWatchRetryObj *localEnvWatchRetry
	var settingsSvc *settingsService
	if opts.Remote != nil {
		rootOf := func() string {
			if envRemote == nil {
				return ""
			}
			return envRemote.Root()
		}
		envShell = newRemoteEnvShell(func() authority.Slot { return authSlot }, rootOf, func(msg string) {
			if envRemote != nil {
				envRemote.SetNotice(msg)
			}
		})
		envRemote = env.NewRemote(opts.Remote.Workspace(), cfg.EnvDetectors(), trustedLive,
			envShell.RunDetect, envShell.RunCapture)
		// Before trust the authority command gate refuses the shell probe; the marker
		// detector then uses shell-free authority Stat calls so an untrusted workspace can
		// still show the remote detection result (activation remains trust-gated).
		envRemote.SetMarkerProber(newRemoteMarkerProber(func() authority.Slot { return authSlot }))
		envProvider = envRemote
	} else {
		localProvider := env.New(root, cfg.EnvDetectors(), trustedLive)
		envProvider = localProvider
		if watching {
			// Local detector markers are watched with the editor's fsnotify watcher so a
			// created/removed .envrc/.nvmrc/.venv refreshes detection off the UI loop. If
			// fsnotify is unavailable the provider carries an Env: Reload degradation
			// notice instead of blocking or polling the tree. Env: Reload retries a
			// failed start (e.g. the root was not watchable yet) in the background.
			localWatch := startLocalEnvWatch(ctx, localProvider, root)
			envWatch = localWatch
			retry := &localEnvWatchRetry{
				ctx:     ctx,
				current: localWatch,
				start: func() *localEnvWatch {
					return startLocalEnvWatch(ctx, localProvider, root)
				},
			}
			localWatchRetryObj = retry
			localWatchRetry = retry.Retry
		} else {
			localProvider.SetWatchNotice(appText("gvim.notice.local_env_watch_off", "Local environment: file watching is off; changed detector markers are picked up on Env: Reload"))
		}
	}
	levelLive := func() string { return string(levelOf()) }
	// The workspace spawn gate (docs/trust.md): Fresh's executable-path rule with the
	// child's effective PATH resolved, read live so a decision applies to the next
	// launch. Authority one-shots and the terminal, plugin processes and local
	// language-server starts all consult it. A remote launch keeps the coarse rule: its
	// commands run on another machine, whose trust store is deferred (#21).
	spawnGate := func(_ context.Context, argv []string, cwd string, childEnv map[string]string) error {
		if opts.Remote != nil {
			if !trustedLive() {
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
	// The authority's plain Gate (no environment) is the remote/fallback check; the local
	// backend prefers the environment-aware form below.
	authGate := func(ctx context.Context, argv []string, cwd string) error {
		return spawnGate(ctx, argv, cwd, nil)
	}
	// A remote launch mounts its authority before the plugin runtime is built: the
	// runtime's OS is the authority-backed one (see po.WrapOS below), so gvim.fs and
	// gvim.proc from the first plugin Work go to the remote workspace and never to the
	// local machine. A local launch keeps the plugin OS and mounts after the runtime,
	// exactly as before.
	if opts.Remote != nil {
		authSlot = mountAuthority(opts, nil, authGate, spawnGate, localEnvOverlay(envProvider, true))
	}
	// The language-server gate has the manager's shape (it computes the child PATH).
	lspGate := func(_ context.Context, argv []string, cwd, pathValue string) error {
		if opts.Remote != nil {
			if !trustedLive() {
				return errors.New("workspace is not trusted: language servers cannot start")
			}
			return nil
		}
		return trust.DecideSpawn(levelOf(), root, cwd, argv, pathValue)
	}
	deps.Env = envPort{provider: envProvider, level: levelLive, retryWatch: localWatchRetry}
	if logFile != nil {
		deps.NoticeSink = logFile.Notice
	}
	adapter, _ := deps.Editor.(*uiadapter.Editor)
	if adapter != nil {
		adapter.SetContext(cfg.HighlightContextBytes)
		// The hot-exit quit policy is a wrapper over the adapter, so the session
		// save and the recovery copy keep using the real adapter and runtime.ed.
		deps.Editor = hotExitEditor{Editor: adapter, hot: plan.hotExit}
	}
	ed.SetShellEnv(envProvider.Overlay)
	deps.Settings = settingsFor(opts, cfg)
	deps.Settings.Theme = th.Name
	deps.ThemeCatalog = themeCat
	deps.ThemeRef = themeRef
	icon := icons.Resolve(cfg.UIIcons, cfg.UIIconsFont, os.Getenv)
	deps.Settings.IconsMode, deps.Settings.IconsWhy = icon.Mode.String(), icon.Why
	deps.Settings.Keymap = choice.name
	keys := keybindings.NewStore(deps.Settings.ConfigFile, cfg)
	liveCfg := func() config.Config { return keys.Current(cfg) }
	deps.SwitchKeymap = switchKeymapWith(liveCfg)
	deps.LSP = newLSP(lspConfig(cfg, trustState))
	if setter, ok := deps.LSP.(interface{ SetSpawnEnv(func() []string) }); ok {
		setter.SetSpawnEnv(envProvider.Overlay)
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
	// docs/authority.md §7) once the slot exists, below. Until then the port is built as
	// for a local launch; the manager never starts a local server for a remote workspace
	// after the spawner is installed, and the spawn gate still runs before every start.
	// A folder argument shows the explorer at startup. Without one the explorer
	// stays hidden until Ctrl+B, unless the session had it open. --simple opens
	// neither.
	deps.ShowExplorer = plan.explorer && (opts.Root != "" || (plan.tabs && restored && sess.Explorer))
	deps.Sidebar = sidebarState
	deps.HideLineNumbers = !cfg.LineNumbers
	deps.HideMenuBar = !cfg.ShowMenuBar
	deps.TabSize = cfg.TabSize
	deps.View = viewOptions(cfg, icon.Mode.String())
	deps.Languages = languageSettings(cfg)
	deps.MarkdownTOC = markdownTOCConfig(cfg)
	// The Settings page writes the config files and reapplies the live values. The
	// project layer is offered only while the workspace is trusted, so the check
	// reads the trust controller every time.
	trustedForSettings := func() bool {
		if trustCtl == nil {
			return true
		}
		return trustCtl.State().AllowsProjectConfig()
	}
	projSearch := newProjectSearch(edOpts)
	deps.Root = tui.RootPorts{
		Projects:       proj,
		Grep:           grepFor(edOpts),
		Preview:        previewReader{},
		ProjectSearch:  projSearch,
		ProjectReplace: projSearch,
		GitGrep:        gitGrep{spawnEnv: envProvider.Overlay},
		Git:            uiadapter.GitStatus{},
		OpenURL:        openURL,
		Watch:          events,
		AutoSave:       interval,
		Stashes:        uiadapter.StashesFor(opts.SessionDir),
	}
	deps.Root.Keybindings = keys
	deps.Root.AutoRevertPoll = autoRevertPoll(cfg)
	deps.Root.Recovery, deps.Root.RecoveryEvery = recPort, recEvery
	deps.Root.FullLoadPrompts = fullLoads
	if watcher != nil {
		// Assign only a running watcher: a nil *filewatch.Watcher in the interface is not nil.
		deps.Root.WatchDirs = watcher
	}
	// The plugin runtime is always wired. With --no-plugins its loader is disabled.
	var markEdited func(*editor.Document)
	if adapter != nil {
		markEdited = adapter.MarkEdited
	}
	// A daemon workspace scopes gvim.state to its own workspace session directory
	// (sessions/<stable-id>/plugin-state.json); a direct launch keeps the editor-wide
	// per-plugin state files. This read happens here, off the update loop: buildRuntime
	// runs before the program starts, or in the workspace build goroutine. The plugin
	// runtime seeds the parsed state when it starts and writes it back coalesced.
	var pluginStateFile string
	var pluginStateData []byte
	if opts.Daemon != nil && opts.SessionDir != "" {
		pluginStateFile = filepath.Join(opts.SessionDir, "plugin-state.json")
		if data, err := os.ReadFile(pluginStateFile); err == nil {
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
		Root:               proj.RootDir,
		SpawnEnv:           localEnvOverlay(envProvider, opts.Remote != nil),
		Gate:               spawnGate,
		WorkspaceStateFile: pluginStateFile,
		WorkspaceStateData: pluginStateData,
	}
	// A shared daemon runtime spans workspaces but its plugin services are session-wide;
	// its per-workspace scanner/trust root cannot be represented safely. A remote launch's
	// local tmux/transcript stores are not the remote workspace's either. Only a local
	// direct (non-shared) launch exposes agent discovery to plugins; the plugin reports
	// its unsupported state in the other cases.
	if opts.Remote == nil && (opts.Daemon == nil || opts.PluginDaemon == nil) {
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
		po.WrapOS = func(local hostos.OS) hostos.OS { return pluginhost.NewAuthorityOS(authSlot, local, spawnGate) }
	}
	var pw *pluginhost.Wiring
	var pluginWS *pluginhost.Workspace
	if opts.Daemon != nil && opts.PluginDaemon != nil && opts.Remote == nil {
		// One plugin runtime for the whole daemon: this workspace contributes its own OS
		// (root/env/gate forwarded through the runtime's OS mux), editor adapter, keymap
		// builder and gvim.state namespace, and the switch rebinds them.
		pluginWS = pluginhost.NewWorkspace(cfg, ed, markEdited, po)
		if pluginWS.StateNote != "" {
			_, _ = fmt.Fprintln(os.Stderr, "gvim: warning: "+pluginWS.StateNote)
		}
		sharedRuntime, sharedKeys := opts.PluginDaemon.Ensure(cfg, po)
		pw = &pluginhost.Wiring{OS: pluginWS.OS, Runtime: sharedRuntime, Editor: pluginWS.Editor, Keys: sharedKeys}
	} else {
		pw = pluginhost.New(cfg, ed, markEdited, po)
	}
	// The workspace authority: the local backend over the plugin OS (the same
	// instance the plugin runtime uses) or the SSH backend of a remote launch. Every
	// launch and one-shot goes through it; nothing downstream looks at the kind. A
	// remote launch mounted it before the plugin runtime, so its plugin OS can point at
	// it; this call is for the local session.
	if authSlot == nil {
		authSlot = mountAuthority(opts, pw, authGate, spawnGate, localEnvOverlay(envProvider, opts.Remote != nil))
	}
	if connectNotice, _ := attemptAuthorityConnect(ctx, authSlot); connectNotice != "" {
		openNotices = append(openNotices, connectNotice)
	}
	if opts.Remote != nil && !opts.NoPlugins {
		// Honest startup note for plugin authors and users: the plugin OS follows the
		// workspace for files and processes, but these are local-only and remote file
		// watching is the authority's polling watch, not the local fsnotify.
		openNotices = append(openNotices, remotePluginAuthorityNotice)
	}
	var remoteFiles *finder.Index
	var remoteGit *remoteGitStatus
	var remoteExplorerLoader *authorityDirLoader
	var remoteRecoveryPath string
	if opts.Remote != nil {
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
		if deferRemoteRestore {
			recoverySession = opts.SessionDir
		}
		recPort, recEvery, recNotice = setupRemoteRecovery(ed, cfg, recoverySession, "SSH:"+remoteTrustTarget(opts.Remote))
		deps.Root.Recovery, deps.Root.RecoveryEvery = recPort, recEvery
		remoteRecoveryPath = restore.RecoveryDirFor(recoverySession, true)
		// LSP over SSH: the servers run on the remote host through the slot's
		// LongRunning surface; a local server must never start for a remote buffer.
		if deps.LSP != nil {
			if setter, ok := deps.LSP.(interface {
				SetSpawner(func(context.Context, lsp.Config) (*lsp.Client, error))
			}); ok {
				setter.SetSpawner(remoteLSPSpawner(authSlot, opts.Remote.Workspace(), envProvider.Overlay))
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
		remoteFiles, remoteRoot = newRemoteFileIndex(ctx, authSlot, opts.Remote.Workspace())
		denoRoot = remoteRoot
		deps.Files = remoteFiles
		deps.FileRoot = remoteRoot
		remoteRootPorts(&deps.Root)
		// The remote root is now resolved (a ~ root is expanded on the remote host):
		// point the remote environment provider at it and warm its marker probe/capture
		// in the background, so the first remote spawn reads a captured remote delta
		// and no UI update ever waits on the remote host.
		if envRemote != nil {
			envRemote.SetRoot(remoteRoot)
			// Probe the interactive login shell synchronously (one bounded remote
			// command) so a fallback notice can join the startup notices; detection and
			// capture themselves stay off the UI update loop.
			if envShell != nil {
				envShell.Prepare(ctx)
				if notice := envShell.Notice(); notice != "" {
					openNotices = append(openNotices, notice)
				}
			}
			go envRemote.Warm(ctx)
			// Env-input changes on the remote host invalidate detection/capture through
			// the authority's own watch (no local fallback); the watcher degrades to a
			// Settings notice + manual Env: Reload when the backend cannot watch.
			envWatch = startRemoteEnvWatch(ctx, authSlot, envRemote, remoteRoot)
		}
		// Project search and Git Grep run on the remote host through the authority's
		// one-shot surface (the gate still applies); their results carry absolute remote
		// paths, so selecting one opens through the remote document access. Project
		// replacement reads and writes each candidate through the authority file system
		// with the same trust gate: bounded reads, atomic remote writes, stale/changed
		// files skipped, cancellation and a partial-write report. The local ports stay
		// cleared by remoteRootPorts for every other caller.
		installRemoteSearchPorts(&deps.Root, authSlot.OneShot(), authSlot.Files(), authGate, remoteRoot, authSlot.Label(), envProvider.Overlay)
		// The read-only remote explorer: the tree lists the remote directories through
		// the authority file system, one command at a time, so the update loop never
		// waits on the remote host. No local fallback: every listing is remote, the
		// workspace trust gate runs before them, and a disconnected authority says so
		// (the tree is reloaded after a reconnect). New File, New Directory, Rename and
		// Delete run through the authority's FileMutations surface; the local file-operation
		// adapter is never used for remote paths. Cut/paste of subtrees and drag/move stay
		// unwired (recorded gaps): the explorer refuses them before the adapter.
		remoteExplorerLoader = installRemoteExplorer(&deps, authSlot, remoteRoot, authGate, ed)
		// Remote git marks and the quick-open file preview run through the same authority
		// (remote cwd, remote env delta, spawn/trust gate), never through the local git or
		// the local file system. The git read is briefly cached so save/watch/reload bursts
		// share one remote command; the reconnect hook invalidates it and restarts the
		// remote file index.
		remoteGit = installRemoteDataPorts(&deps.Root, authSlot, remoteRoot, envProvider.Overlay)
	}
	// A daemon-hosted remote workspace restores its saved session only after the authority
	// connects and the remote document access is installed (the files open through the
	// remote file access, never the local disk). A failed reconnect leaves the session file
	// and stashes untouched for the next start: this runtime restores nothing and the
	// capture path skips it, so an empty editor can never overwrite the saved session.
	if deferRemoteRestore && restored {
		if authSlot == nil || !authSlot.Connected() {
			remoteRestoreSkipped = true
			deps.ShowExplorer = false
			deps.StartupOpen, deps.StartupLine, deps.StartupCol = "", 0, 0
			openNotices = append(openNotices, "SSH workspace disconnected: the saved remote session is kept for the next start")
		} else {
			// The saved paths are remote: probe their existence through the authority,
			// then let the editor open them through its installed remote file access.
			exists := remoteRestoreExists(authSlot)
			switch {
			case plan.tabs:
				failed = restore.RestoreSessionAccess(ed, opts.SessionDir, sess, exists)
				tree.SetShowHidden(sess.ShowHidden)
				tree.SetShowIgnored(sess.ShowIgnored)
				if deps.Explorer != nil {
					deps.Explorer.SetShowHidden(sess.ShowHidden)
					deps.Explorer.SetShowIgnored(sess.ShowIgnored)
				}
			case plan.hot:
				failed = restore.RestoreHotExitAccess(ed, opts.SessionDir, sess, exists)
			}
			// The restored tabs own the start: do not reopen the startup target file.
			deps.StartupOpen, deps.StartupLine, deps.StartupCol = "", 0, 0
		}
	}
	// Remote external-change watching: bridge the authority's remote watch into the root's
	// existing watch pipeline (RootPorts.Watch/WatchDirs). A local launch gets nil here and
	// keeps its fsnotify watcher; the remote writer never touches the local filesystem.
	remoteFileWatch := setupRemoteWatch(ctx, &deps, authSlot, opts.Remote)
	if remoteExplorerLoader != nil {
		remoteExplorerLoader.setWatcher(remoteFileWatch)
	}
	// The one-shots of the session go through the slot: the shell and formatter runner
	// below, the dock terminal through its starter, and the local git grep here. A remote
	// launch already installed the authority-backed Git Grep port in its block above; do
	// not overwrite it with the local adapter (which would resolve the local work tree).
	if opts.Remote == nil {
		deps.Root.GitGrep = gitGrep{spawnEnv: envProvider.Overlay, oneShot: authSlot.OneShot()}
	}
	deps.Root.Plugins = pw.Runtime
	deps.Root.PluginEditor = pw.Editor
	if pluginWS != nil {
		// The daemon-wide host mux: the model's host calls route to whichever workspace is
		// active, and editor-wide registrations follow a switch.
		deps.Root.PluginShared = opts.PluginShared
	}
	deps.Root.PluginKeys = func(name string, bs []pluginapi.Binding) (tui.Keymap, error) {
		km, err := pw.Keys(name, bs)
		if km == nil {
			return nil, err
		}
		return keybindings.NewKeymap(km), err
	}
	// The Settings page's live parts: the LSP manager for its Language Servers
	// page statuses and actions, and the plugin runtime for its Plugins page. They
	// are built after the page's config snapshot and before the model.
	var lspBase *lspBaselineUpdater
	settingsSvc = newSettingsService(root, opts, ed, trustedForSettings, cfg, themeCat, settingsRuntime{
		lsp:         deps.LSP,
		localeApply: func(locale string) { pw.Runtime.SetLocale(locale) },
		lspReload: func(next config.Config) {
			// The worker serializes this with live trust changes and always applies the
			// newest config+trust snapshot under the live gate; it never blocks the caller.
			if lspBase != nil {
				lspBase.UpdateConfig(next, trust.State{Level: levelOf()})
			}
		},
		plugins:    pw.Runtime,
		pluginsCfg: cfg,
		keymapOf: func(cfg config.Config) tui.Keymap {
			return keybindings.NewKeymap(keybindings.ApplyOverrides(presetKeymap(opts.Keymap, cfg), cfg))
		},
		presetName: choice.name,
		iconsMode:  icon.Mode.String(),
		iconsWhy:   icon.Why,
		version:    gvimVersion(),
	})
	settingsSvc.env = envProvider
	settingsSvc.setEnvWatch(envWatch)
	if localWatchRetryObj != nil {
		// Install after the settings service exists; SetInstall synchronizes with a retry
		// that may already have recovered a watcher while the runtime was being built.
		localWatchRetryObj.SetInstall(func(lw *localEnvWatch) {
			settingsSvc.setEnvWatch(lw)
		})
	}
	deps.SettingsPort = settingsSvc
	lspBase = newLSPBaselineUpdater(ctx, deps.LSP, settingsSvc.currentConfig(), trust.State{Level: levelOf()})
	settingsSvc.lspBase = lspBase
	if setter, ok := deps.LSP.(interface{ SetBaselineWait(func()) }); ok && lspBase != nil {
		setter.SetBaselineWait(lspBase.Wait)
	}

	// A live trust change (revoke/restore) re-gates the LSP baseline at once, so a
	// revoke cannot leave an auto-starting configuration behind even without a
	// Settings reload. The updater keeps the latest settings config; a stale reload
	// snapshot cannot overwrite a newer baseline. Manual Start/Restart remains available.
	if trustCtl != nil {
		if opts.Remote == nil && lspBase != nil {
			trustCtl.prepareTrust = func(state trust.State) error {
				cfg, err := settingsSvc.loadTrustedLSP(state.Root)
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

	// Project-root Deno auto-selection (Fresh deno_lsp parity with authority-only checks):
	// one bounded background probe against the workspace root; it never overrides an explicit
	// or disabled user LSP config and is re-run after a reconnect or project-root switch. The
	// provider is evaluated when a manager for exactly this root is created, re-reading the
	// settings service's live config (a settings reload thus invalidates a stale decision).
	var denoAuto *denoAutoSelector
	if deps.LSP != nil && authSlot != nil && denoRoot != "" {
		denoCfg := func() config.Config {
			// The Deno selection is an auto-start choice, so evaluate it against the same
			// live-trust-gated config the port uses, never the raw reloaded settings.
			return lspConfig(settingsSvc.currentConfig(), trust.State{Level: levelOf()})
		}
		denoAuto = newDenoAutoSelector(deps.LSP, authSlot, denoRoot, opts.Remote != nil, authGate, envProvider.Overlay, denoCfg)
		if denoAuto != nil {
			if setter, ok := deps.LSP.(interface{ SetRootChangeListener(func(string)) }); ok {
				setter.SetRootChangeListener(func(root string) {
					denoAuto.Rebind(ctx, root)
				})
			}
			denoAuto.Start(ctx)
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
			setter.SetExternalLocationOpener(newSlangSynthOpener(opts.Remote != nil, authSlot, authGate, envProvider.Overlay, root))
		}
	}
	// Notice is the first config problem, then any session file whose unsaved
	// text was kept, for the status row. Every warning still goes to stderr,
	// before the alternate screen starts.
	// Every config problem is kept for Show Warnings. The status row shows the first one, with
	// how many there are and where to read them when there are more.
	problems := configProblems(diags, choice.warnings)
	var notices []string
	if n := keybindings.Notice(problems); n != "" {
		notices = append(notices, n)
	}
	deps.Warnings = append(problems, choice.replaced...)
	notices = append(notices, openNotices...)
	if trustWarning != "" {
		notices = append(notices, trustWarning)
	}
	if logNotice != "" {
		notices = append(notices, logNotice)
	}
	if n := restore.RestoreNotice(failed); n != "" {
		notices = append(notices, n)
	}
	if recNotice != "" {
		notices = append(notices, recNotice)
	}
	if opts.Remote != nil {
		notices = append(notices, remoteEditingNotice(opts.Remote, deps.StartupOpen))
	}
	if themeWarn != "" {
		notices = append(notices, themeWarn)
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
	model := newUI(deps)
	// Restore the saved pane layout over the files the session just opened. A
	// remote launch has no local session layout, and a hot-exit restore skips the
	// layout with the clean tabs; a stale or invalid record is ignored by
	// RestoreSplits, which keeps the one-pane start.
	if restored && plan.tabs && (opts.Remote == nil || (deferRemoteRestore && !remoteRestoreSkipped)) {
		model.RestoreSplits(sess.Layout, sess.Files)
	}
	loadSearch(model, opts.SessionDir)
	shellCommand, shellArgs := cfg.Terminal.TerminalShellOptions()
	model.SetDock(tui.DockPorts{
		Starter: uiadapter.NewTermStarter(ctx, uiadapter.TermStarterOptions{
			Shell: shellCommand, Args: shellArgs, ScrollbackLines: cfg.Terminal.ScrollbackLines,
			SpawnEnv:  envProvider.Overlay,
			Authority: authSlot,
			// A refused remote cwd falls back to the authority's default directory and
			// says so in the status row (the terminal port has no notice channel).
			Notice: model.Notify,
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
	// Agent discovery scans the local machine through the same spawn gate as the
	// terminal and plugins; the picker runs the scan off the update loop.
	model.SetAgentDiscoverer(newAgentDiscoverer(func() string { return proj.RootDir() }, spawnGate, cfg.Terminal.Agents))
	// The workspace authority of the session: the shell and formatter runner go
	// through the slot, and a remote launch shows its segment and opens the SSH
	// terminal at the remote workspace.
	if run := authorityShellRunner(authSlot); run != nil {
		model.SetShellRunner(run)
	}
	if opts.Remote != nil {
		slot := authSlot
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
		model.SetAuthorityHooks(tui.AuthorityHooks{
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
				if envShell != nil {
					go envShell.Reprobes(ctx)
				}
				remoteFileWatch.Reconnected()
				// The remote git markers and the file list were read from the carrier
				// that just dropped: invalidate the cached status (the TUI re-reads the
				// marks in its reconnect command) and re-enumerate the remote files on
				// the restored carrier (the palette's loading poll picks the new list up).
				if remoteGit != nil {
					remoteGit.Invalidate()
				}
				if remoteFiles != nil {
					remoteFiles.Restart()
				}
				if denoAuto != nil {
					// New carrier: clear the old decision and probe again; a late answer from
					// the dropped carrier cannot apply because the generation moves.
					denoAuto.Start(ctx)
				}
				return restartLSPAfterReconnect(authSlot, deps.LSP)
			},
		})
		model.OpenAuthorityTerminalAtStart()
	}

	// A restored session brings the terminal transcript back, dead: nothing is
	// re-run automatically, and Restart starts a shell below the transcript.
	if plan.load && plan.tabs && opts.SessionDir != "" && !remoteRestoreSkipped {
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
	// A launch that does not restore the session does not save it either: a save
	// would replace the saved session and remove the stash files that it names.
	// A file launch and --simple are those launches.
	sessionDir := ""
	if plan.save && !remoteRestoreSkipped {
		sessionDir = opts.SessionDir
	}
	rt = &runtime{
		model:                 model,
		ed:                    ed,
		projects:              proj,
		watcher:               watcher,
		sessionDir:            sessionDir,
		pluginStateFile:       pluginStateFile,
		daemonMaterializedCap: cfg.Daemon.MaterializedWorkspaces,
		pluginWS:              pluginWS,
		searchDir:             opts.SessionDir,
		stashDir:              opts.SessionDir,
		kept:                  keptFiles(failed),
		recovery:              recoveryDirForRun(opts.SessionDir, watching, remoteRecoveryPath),

		recoveryWriter:   deps.Root.Recovery,
		log:              logFile,
		auth:             authSlot,
		remoteWatch:      remoteFileWatch,
		remoteFiles:      remoteFiles,
		keepSavedSession: remoteRestoreSkipped,
	}
	model.SetQuitStash(rt.stashForQuit)
	return rt, nil
}

// recoveryDirForRun is the recovery directory this run clears on a clean exit: the
// remote staging copy for a remote launch, otherwise the local session's own copy.
func recoveryDirForRun(sessionDir string, watching bool, remoteRecoveryPath string) string {
	if remoteRecoveryPath != "" {
		return remoteRecoveryPath
	}
	return restore.RecoveryDirFor(sessionDir, watching)
}

// keptFiles returns the files of failed, in order, for the session save to list.
func keptFiles(failed []restore.Unrestored) []session.FileState {
	files := make([]session.FileState, len(failed))
	for i, u := range failed {
		files[i] = u.File
	}
	return files
}

// startWatcher starts a file watcher when watching is set. It returns the watcher
// and its event channel, and both are nil when no watcher runs. The watcher is
// pointed at the tree root by NewProjects. A watcher that cannot start is a
// warning on stderr, and the tree then reloads only on Ctrl+R.
func startWatcher(watching bool) (*filewatch.Watcher, <-chan filewatch.Changed) {
	if !watching {
		return nil, nil
	}
	w, err := filewatch.New(filewatch.Options{})
	if err != nil {
		_, _ = fmt.Fprintln(os.Stderr, "gvim: warning: file watcher:", err)
		return nil, nil
	}
	return w, w.Events()
}

// editorOptions returns the editor settings that cfg gives. empty is true when
// gvim starts without files.
func editorOptions(cfg config.Config, empty bool) editor.Options {
	ending := "LF"
	if cfg.LineEnding == config.LineEndingCRLF {
		ending = "CRLF"
	}
	return editor.Options{
		Empty:                  empty,
		UseTabs:                !cfg.InsertSpaces,
		TabSize:                cfg.TabSize,
		AutoIndent:             cfg.AutoIndent,
		TrimTrailingWhitespace: cfg.TrimTrailingWhitespace,
		EnsureFinalNewline:     cfg.EnsureFinalNewline,
		DefaultLineEnding:      ending,

		AutoClose:                              cfg.AutoClose,
		AutoSurround:                           cfg.AutoSurround,
		VirtualSpace:                           cfg.VirtualSpace,
		ScrollOffset:                           cfg.ScrollOffset,
		AutoReadOnly:                           cfg.AutoReadOnly,
		SnapshotInterval:                       cfg.SnapshotInterval,
		ConfirmQuit:                            cfg.ConfirmQuit,
		AutoCreateEmptyBufferOnLastBufferClose: cfg.AutoCreateEmptyBufferOnLastBufferClose,
		RecoveryEnabled:                        cfg.RecoveryEnabled,
		AutoRecoverySaveIntervalSecs:           cfg.AutoRecoverySaveIntervalSecs,
		AutoRevertPollIntervalMS:               cfg.AutoRevertPollIntervalMS,
		LargeFileThresholdBytes:                cfg.LargeFileThresholdBytes,
		LargeFileMode:                          true,
		EstimatedLineLength:                    cfg.EstimatedLineLength,
	}
}

// keymapChoice is the keymap a session starts with: the keymap, the name of the preset it
// is built on, and what is wrong with the config that chose it.
type keymapChoice struct {
	km       *keymap.Map
	name     string
	warnings []string
	// replaced are the preset bindings the config's keybindings take over (replacedBindings).
	replaced []string
}

// runKeymap returns the keymap a session uses. The flag names a built-in keymap,
// and when it is empty the config keymap is used, and the platform default when
// the config sets none. The config keybindings are applied on top of that keymap.
// A bad flag stops startup. A config keymap that does not exist, and a keybinding that
// cannot be applied, do not: they are warnings, the default keymap stands for the first,
// and the binding is left out for the second.
func runKeymap(flag string, cfg config.Config) (keymapChoice, error) {
	name := flag
	if name == "" {
		name = cfg.Keymap
	}
	var warnings []string
	km, err := keybindings.Keymap(name)
	if err != nil {
		if flag != "" {
			return keymapChoice{}, err
		}
		err = fmt.Errorf("%w (valid keymaps: %s); using the %s keymap", err, strings.Join(keybindings.BuiltinKeymaps, ", "), keymap.DefaultName())
		warnings = append(warnings, inConfigFile(cfg, "keymap", err).Error())
		km, name = keymap.Default(), ""
	}
	if name == "" {
		name = keymap.DefaultName()
	}
	warnings = append(warnings, keybindings.Warnings(cfg)...)
	return keymapChoice{km: keybindings.ApplyOverrides(km, cfg), name: name, warnings: warnings, replaced: keybindings.Replaced(km, cfg)}, nil
}

// presetKeymap returns the built-in keymap a session starts from, before the user's
// keybindings: the flag, else the config keymap, else the platform default. runKeymap has
// checked the name by then, so a bad one falls back to the default here.
func presetKeymap(flag string, cfg config.Config) *keymap.Map {
	name := flag
	if name == "" {
		name = cfg.Keymap
	}
	if km, err := keybindings.Keymap(name); err == nil {
		return km
	}
	return keymap.Default()
}

// switchKeymapFor returns the SwitchKeymap port of a session. It builds the named
// built-in keymap with the config keybindings applied, as runKeymap does, so an
// override survives a switch.
func switchKeymapFor(cfg config.Config) func(name string) (tui.Keymap, error) {
	return switchKeymapWith(func() config.Config { return cfg })
}

// switchKeymapWith is switchKeymapFor over the config as it is when the switch happens:
// the keybinding editor changes the keybindings during the session.
func switchKeymapWith(cfgNow func() config.Config) func(name string) (tui.Keymap, error) {
	return func(name string) (tui.Keymap, error) {
		km, err := keybindings.Keymap(name)
		if err != nil {
			return nil, err
		}
		return keybindings.NewKeymap(keybindings.ApplyOverrides(km, cfgNow())), nil
	}
}

// settingsFor returns the effective settings that open_settings lists. A flag wins
// over the config, as it does for the session. The keymap falls back to the platform
// default when neither names one. ConfigFile is the user file the session reads: the
// one named by --config, else the default user file, or empty when that is unknown.
func settingsFor(opts Options, cfg config.Config) tui.Settings {
	s := tui.Settings{
		Theme:               cfg.Theme,
		Keymap:              cfg.Keymap,
		TabSize:             cfg.TabSize,
		InsertSpaces:        cfg.InsertSpaces,
		AutoIndent:          cfg.AutoIndent,
		LineNumbers:         cfg.LineNumbers,
		MenuBar:             cfg.ShowMenuBar,
		ExplorerShowHidden:  cfg.ExplorerShowHidden,
		ExplorerShowIgnored: cfg.ExplorerShowIgnored,
		UIIcons:             cfg.UIIcons,
		UIIconsFont:         cfg.UIIconsFont,
		UIImageProtocol:     cfg.UIImageProtocol,
		ConfigFile:          opts.Config,
		FoldBuffer:          cfg.ContentsFoldBuffer(),
		ContentsFollow:      cfg.ContentsFollow(),
		ContentsAutoOpen:    cfg.ContentsAutoOpen(),
		ContentsRows:        cfg.ContentsRows(),

		LineEnding:             cfg.LineEnding,
		TrimTrailingWhitespace: cfg.TrimTrailingWhitespace,
		EnsureFinalNewline:     cfg.EnsureFinalNewline,
	}
	if opts.Theme != "" {
		s.Theme = opts.Theme
	}
	if opts.Keymap != "" {
		s.Keymap = opts.Keymap
	}
	if s.Keymap == "" {
		s.Keymap = keymap.DefaultName()
	}
	if s.ConfigFile == "" {
		if files, err := config.Paths(); err == nil {
			s.ConfigFile = files.User
		}
	}
	return s
}

// runTheme returns the theme a session uses: the flag, else the config theme.
// Both are config theme values (a bundled name, builtin://name, a path relative
// to the user theme dir, file://, or an http(s) URL to a theme JSON file) and
// resolve through the registry/URL fetcher. The config theme is "dark" unless a
// file sets another. The second result is the portable ref the TUI keeps so it
// can apply the theme again; the third is a non-fatal warning (a URL theme served
// from the last-known-good cache, or a cache that could not be written).
func runTheme(ctx context.Context, flag string, cfg config.Config, reg *theme.Registry) (theme.Theme, string, string, error) {
	ref := effectiveThemeRef(flag, cfg)
	if err := theme.ValidateForm(ref); err != nil {
		return theme.Theme{}, "", "", err
	}
	if theme.IsURLForm(ref) {
		th, warning, err := fetchURLTheme(ctx, ref, reg)
		if err != nil {
			return theme.Theme{}, "", "", err
		}
		return th, strings.TrimSpace(ref), warning, nil
	}
	th, err := reg.Load(ref)
	if err != nil {
		if flag == "" {
			err = inConfigFile(cfg, "theme", err)
		}
		return theme.Theme{}, "", "", err
	}
	canonical, err := reg.Canonical(ref)
	if err != nil {
		canonical = ref
	}
	return th, canonical, "", nil
}

// configProblems lists the problems of loading the config: the diagnostics of the files, then the
// keymap and keybinding warnings, one line each.
func configProblems(diags []config.Diagnostic, warnings []string) []string {
	out := make([]string, 0, len(diags)+len(warnings))
	for _, d := range diags {
		out = append(out, d.String())
	}
	return append(out, warnings...)
}

// inConfigFile prefixes err with the config file that set key. When no file set
// it, err is returned unchanged.
func inConfigFile(cfg config.Config, key string, err error) error {
	if path := cfg.Source(key); path != "" {
		return fmt.Errorf("%s: %w", path, err)
	}
	return err
}

// resolveRoot returns the absolute explorer root. An empty dir means the
// current directory. Otherwise dir is opened as a workspace and recorded in
// the recent list. A failure to record it is a warning on stderr, and the
// root is still used.
func resolveRoot(dir string) (string, error) {
	if dir == "" {
		return os.Getwd()
	}
	r, err := workspace.Switch(dir)
	if r.Path() == "" {
		if err == nil {
			err = fmt.Errorf("open workspace %s: no root", dir)
		}
		return "", err
	}
	if err != nil {
		_, _ = fmt.Fprintln(os.Stderr, "gvim: warning:", err)
	}
	return r.Path(), nil
}

// printDiagnostics writes one warning line to w for each config problem. Loading
// does not stop at a problem, so these lines are the only report of a skipped
// setting.
func printDiagnostics(w io.Writer, diags []config.Diagnostic) {
	for _, d := range diags {
		_, _ = fmt.Fprintln(w, "gvim: warning: "+d.String())
	}
}
