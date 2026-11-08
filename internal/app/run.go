package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/charmbracelet/x/term"

	"github.com/tbxark/gvim/config"
	"github.com/tbxark/gvim/editor"
	"github.com/tbxark/gvim/explorer"
	"github.com/tbxark/gvim/finder"
	"github.com/tbxark/gvim/internal/configview"
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
	"github.com/tbxark/gvim/pluginapi"
	"github.com/tbxark/gvim/session"
	"github.com/tbxark/gvim/syntax"
	"github.com/tbxark/gvim/theme"
	"github.com/tbxark/gvim/workspace"
)

// Constructors that buildRuntime calls. Tests replace them to see the values the
// config produced. In production they are the real constructors.
var (
	newEditor = editor.New
	newUI     = tui.New
)

// Run validates opts, builds the terminal UI and runs it in the alternate
// screen until the user quits. Canceling ctx stops the program, and Run then
// returns the context's error. The session is saved when the program ends, to
// Options.SessionDir, or to the default directory when that is empty.
func Run(ctx context.Context, opts Options) error {
	if err := opts.Validate(); err != nil {
		return err
	}
	if err := requireTerminal(os.Stdout); err != nil {
		return err
	}
	if opts.SessionDir == "" {
		if dir, err := restore.DefaultSessionDir(); err == nil {
			opts.SessionDir = dir
		}
	}
	rt, err := buildRuntime(ctx, opts, true)
	if err != nil {
		return err
	}
	defer rt.close()
	runErr := tui.Run(ctx, rt.model)
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
	} else if r.recovery != "" && (!r.interrupted || r.sessionDir != "") {
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

// runtime is a built session: the UI model, the editor it shows, and the resources
// that run until the program ends. Run saves the session, stops the servers, closes
// the dock and then closes the resources.
type runtime struct {
	model      *tui.Model
	ed         *editor.Editor
	projects   *uiadapter.Projects
	watcher    *filewatch.Watcher // nil when no watch was started
	sessionDir string             // empty when the session is neither restored nor saved
	searchDir  string             // where the find history and toggles are kept; empty for nowhere
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
	// log is the --log-file writer; nil when there is none.
	log *fileLog
}

// close stops the file index of the current root and the watcher.
func (r *runtime) close() {
	r.projects.Close()
	if r.watcher != nil {
		_ = r.watcher.Close()
	}
	r.log.Close()
}

// save writes the session, when a session directory is set.
func (r *runtime) save() error {
	if r.sessionDir == "" {
		return nil
	}
	return restore.WriteSession(r.ed, r.sessionDir, r.model.ExplorerState(), r.kept)
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
// With no file arguments and a session directory, the saved session is restored
// first. With file arguments the session is neither restored nor saved, so the saved
// session stays as it is. When watching is set, the tree root is watched for changes.
func buildRuntime(ctx context.Context, opts Options, watching bool) (rt *runtime, err error) {
	if err := configview.CheckFile(opts.Config); err != nil {
		return nil, err
	}
	root, err := resolveRoot(opts.Root)
	if err != nil {
		return nil, err
	}
	cfg, diags, err := config.Load(config.Options{Root: root, UserFile: opts.Config})
	if err != nil {
		return nil, err
	}
	printDiagnostics(os.Stderr, diags)
	tree, err := explorer.Open(root)
	if err != nil {
		return nil, err
	}
	tree.SetShowHidden(cfg.ExplorerShowHidden)
	tree.SetShowIgnored(cfg.ExplorerShowIgnored)
	// Without files the editor starts empty, and the welcome screen shows.
	edOpts := editorOptions(cfg, len(opts.Files) == 0)
	interval := autoSaveInterval(opts.AutoSaveInterval, cfg)
	edOpts.AutoSaveInterval = interval
	edOpts.Root = root
	edOpts.SessionDir = opts.SessionDir
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
	var failed []restore.Unrestored
	if len(opts.Files) == 0 && opts.SessionDir != "" {
		s, ok, err := session.Load(opts.SessionDir)
		switch {
		case err != nil:
			_, _ = fmt.Fprintln(os.Stderr, "gvim: warning: session:", err)
		case ok:
			sess, restored = s, true
			failed = restore.RestoreSession(ed, opts.SessionDir, s)
			tree.SetShowHidden(s.ShowHidden)
			tree.SetShowIgnored(s.ShowIgnored)
		}
	}
	var openNotices []string
	var fullLoads []editor.ErrLargeNeedsFullLoad
	for _, f := range opts.Files {
		if err := ed.OpenFile(f.Path); err != nil {
			var need editor.ErrLargeNeedsFullLoad
			if errors.As(err, &need) {
				// Decoding it whole would hold the first frame back. The UI asks.
				fullLoads = append(fullLoads, need)
				continue
			}
			if errors.Is(err, editor.ErrTooLargeToTranscode) {
				// Only this file is lost: the others open, and the status row says why.
				openNotices = append(openNotices, err.Error())
				continue
			}
			return nil, err
		}
		if f.Line > 0 {
			if err := ed.GotoLine(f.Line, f.Col); err != nil {
				return nil, err
			}
		}
	}
	// Counted before crash recovery, which adds tabs of its own after the two files.
	if opts.Diff && len(ed.Tabs()) != 2 {
		return nil, errors.New("--diff needs two different files")
	}
	recPort, recEvery := restore.RecoveryPort(ed, cfg, opts.SessionDir, watching)
	recNotice := restore.RecoverFromCrash(ed, opts.SessionDir, watching, recPort)
	choice, err := runKeymap(opts.Keymap, cfg)
	if err != nil {
		return nil, err
	}
	km := choice.km
	th, err := runTheme(opts.Theme, cfg)
	if err != nil {
		return nil, err
	}
	files := uiadapter.StartFiles(ctx, root)
	watcher, events := startWatcher(watching)
	proj := uiadapter.NewProjects(ctx, watcher, root, files)
	logFile, logNotice := startLogFile(opts.LogFile, diags)
	if logFile != nil {
		opts.pluginLog = logFile.Plugin
	}
	deps := uiadapter.NewDeps(ed, km, th, tree, files)
	if logFile != nil {
		deps.NoticeSink = logFile.Notice
	}
	adapter, _ := deps.Editor.(*uiadapter.Editor)
	if adapter != nil {
		adapter.SetContext(cfg.HighlightContextBytes)
	}
	deps.Settings = settingsFor(opts, cfg)
	icon := icons.Resolve(cfg.UIIcons, cfg.UIIconsFont, os.Getenv)
	deps.Settings.IconsMode, deps.Settings.IconsWhy = icon.Mode.String(), icon.Why
	deps.Settings.Keymap = choice.name
	keys := keybindings.NewStore(deps.Settings.ConfigFile, cfg)
	liveCfg := func() config.Config { return keys.Current(cfg) }
	deps.SwitchKeymap = switchKeymapWith(liveCfg)
	deps.LSP = lsphost.NewFor(cfg)
	// A folder argument shows the explorer at startup. Without one the explorer
	// stays hidden until Ctrl+B, unless the session had it open.
	deps.ShowExplorer = opts.Root != "" || (restored && sess.Explorer)
	deps.HideLineNumbers = !cfg.LineNumbers
	deps.HideMenuBar = !cfg.ShowMenuBar
	deps.TabSize = cfg.TabSize
	deps.View = viewOptions(cfg, icon.Mode.String())
	deps.Languages = languageSettings(cfg)
	deps.Root = tui.RootPorts{
		Projects: proj,
		Grep:     grepFor(edOpts),
		Git:      uiadapter.GitStatus{},
		Watch:    events,
		AutoSave: interval,
		Stashes:  uiadapter.StashesFor(opts.SessionDir),
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
	pw := pluginhost.New(cfg, ed, markEdited, pluginhost.Options{
		Paths:    opts.PluginPaths,
		Disabled: opts.NoPlugins,
		LogSink:  opts.pluginLog,
		Version:  gvimVersion(),
		Preset:   presetKeymap(opts.Keymap, cfg),
		Root:     proj.RootDir,
	})
	deps.Root.Plugins = pw.Runtime
	deps.Root.PluginEditor = pw.Editor
	deps.Root.PluginKeys = func(name string, bs []pluginapi.Binding) (tui.Keymap, error) {
		km, err := pw.Keys(name, bs)
		if km == nil {
			return nil, err
		}
		return keybindings.NewKeymap(km), err
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
	if logNotice != "" {
		notices = append(notices, logNotice)
	}
	if n := restore.RestoreNotice(failed); n != "" {
		notices = append(notices, n)
	}
	if recNotice != "" {
		notices = append(notices, recNotice)
	}
	if logNotice != "" {
		notices = append(notices, logNotice)
	}
	deps.Notice = strings.Join(notices, "; ")
	model := newUI(deps)
	loadSearch(model, opts.SessionDir)
	model.SetDock(tui.DockPorts{Starter: uiadapter.NewTermStarter(ctx), Clipboard: uiadapter.SystemClipboard{}})
	if opts.Diff {
		// The two files are in tabs 0 and 1: the tab count was checked before recovery.
		model.OpenCompare(0, 1)
	}
	// File arguments skip the restore, so the session is not saved either. A save
	// would replace the saved session and remove the stash files that it names.
	sessionDir := opts.SessionDir
	if len(opts.Files) > 0 {
		sessionDir = ""
	}
	rt = &runtime{
		model:      model,
		ed:         ed,
		projects:   proj,
		watcher:    watcher,
		sessionDir: sessionDir,
		searchDir:  opts.SessionDir,
		stashDir:   opts.SessionDir,
		kept:       keptFiles(failed),
		recovery:   restore.RecoveryDirFor(opts.SessionDir, watching),

		recoveryWriter: deps.Root.Recovery,
		log:            logFile,
	}
	model.SetQuitStash(rt.stashForQuit)
	return rt, nil
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
		ConfigFile:          opts.Config,

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

// runTheme returns the bundled theme a session uses: the flag, else the config
// theme. The config theme is "dark" unless a file sets another.
func runTheme(flag string, cfg config.Config) (theme.Theme, error) {
	name := flag
	if name == "" {
		name = cfg.Theme
	}
	th, err := theme.Load(name)
	if err != nil {
		if flag == "" {
			err = inConfigFile(cfg, "theme", err)
		}
		return theme.Theme{}, err
	}
	return th, nil
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
