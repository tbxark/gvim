package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/charmbracelet/x/term"

	"github.com/tbxark/gvim/authority"
	"github.com/tbxark/gvim/config"
	"github.com/tbxark/gvim/editor"
	"github.com/tbxark/gvim/finder"
	"github.com/tbxark/gvim/internal/daemon"
	"github.com/tbxark/gvim/internal/filewatch"
	"github.com/tbxark/gvim/internal/keybindings"
	"github.com/tbxark/gvim/internal/lsphost"
	"github.com/tbxark/gvim/internal/pluginhost"
	"github.com/tbxark/gvim/internal/restore"
	"github.com/tbxark/gvim/internal/tui"
	"github.com/tbxark/gvim/internal/tui/find"
	"github.com/tbxark/gvim/internal/uiadapter"
	"github.com/tbxark/gvim/keymap"
	"github.com/tbxark/gvim/session"
	"github.com/tbxark/gvim/theme"
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
	if err := restore.WriteSession(r.ed, r.sessionDir, r.model.ExplorerState(), r.kept, r.model.SplitState()); err != nil {
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

// switchKeymapWith returns the SwitchKeymap port of a session. It builds the named
// built-in keymap with the config keybindings applied, as runKeymap does, so an
// override survives a switch. The config is read when the switch happens: the
// keybinding editor changes the keybindings during the session.
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
