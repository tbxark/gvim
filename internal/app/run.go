package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/tbxark/gvim/internal/config"
	"github.com/tbxark/gvim/internal/editor"
	"github.com/tbxark/gvim/internal/explorer"
	"github.com/tbxark/gvim/internal/explorer/watch"
	"github.com/tbxark/gvim/internal/finder"
	"github.com/tbxark/gvim/internal/keymap"
	"github.com/tbxark/gvim/internal/session"
	"github.com/tbxark/gvim/internal/theme"
	"github.com/tbxark/gvim/internal/ui"
	"github.com/tbxark/gvim/internal/ui/icons"
	"github.com/tbxark/gvim/internal/workspace"
)

// Constructors that buildRuntime calls. Tests replace them to see the values the
// config produced. In production they are the real constructors.
var (
	newEditor = editor.New
	newUI     = ui.New
)

// Run validates opts, builds the terminal UI and runs it in the alternate
// screen until the user quits. Canceling ctx stops the program, and Run then
// returns the context's error. The session is saved when the program ends, to
// Options.SessionDir, or to the default directory when that is empty.
func Run(ctx context.Context, opts Options) error {
	if err := opts.Validate(); err != nil {
		return err
	}
	if opts.SessionDir == "" {
		if dir, err := defaultSessionDir(); err == nil {
			opts.SessionDir = dir
		}
	}
	rt, err := buildRuntime(ctx, opts, true)
	if err != nil {
		return err
	}
	defer rt.close()
	runErr := ui.Run(ctx, rt.model)
	rt.exit()
	return runErr
}

// exit ends the session in order. The session is saved first, because it is local
// and fast. Then the language servers stop and the dock closes, each with its own
// deadline, so a hung server or shell delays only its own step and never the save.
func (r *runtime) exit() {
	if err := r.save(); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, "gvim: warning: session:", err)
	}
	r.model.Close()
}

// runtime is a built session: the UI model, the editor it shows, and the resources
// that run until the program ends. Run saves the session, stops the servers, closes
// the dock and then closes the resources.
type runtime struct {
	model      *ui.Model
	ed         *editor.Editor
	projects   *projectOpener
	watcher    *watch.Watcher // nil when no watch was started
	sessionDir string         // empty when the session is neither restored nor saved
	// kept are the saved files whose unsaved text could not be restored. The session
	// save lists them, so their stash files survive until the next start.
	kept []session.FileState
}

// close stops the file index of the current root and the watcher.
func (r *runtime) close() {
	r.projects.close()
	if r.watcher != nil {
		_ = r.watcher.Close()
	}
}

// save writes the session, when a session directory is set.
func (r *runtime) save() error {
	if r.sessionDir == "" {
		return nil
	}
	return writeSession(r.ed, r.sessionDir, r.model.ExplorerState(), r.kept)
}

// buildModel builds the UI model and returns it with the file index of the root. It
// starts no watcher and saves nothing, so it is for the callers that only need the
// model. The caller closes the index.
func buildModel(ctx context.Context, opts Options) (*ui.Model, *finder.Index, error) {
	rt, err := buildRuntime(ctx, opts, false)
	if err != nil {
		return nil, nil, err
	}
	return rt.model, rt.projects.index, nil
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
func buildRuntime(ctx context.Context, opts Options, watching bool) (*runtime, error) {
	if err := checkConfigFile(opts.Config); err != nil {
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
	edOpts.AutoSaveInterval = opts.AutoSaveInterval
	edOpts.Root = root
	edOpts.SessionDir = opts.SessionDir
	ed := newEditor(edOpts)
	ed.SetFormatters(cfg.Formatters)
	var sess session.Session
	restored := false
	var failed []unrestored
	if len(opts.Files) == 0 && opts.SessionDir != "" {
		s, ok, err := session.Load(opts.SessionDir)
		switch {
		case err != nil:
			_, _ = fmt.Fprintln(os.Stderr, "gvim: warning: session:", err)
		case ok:
			sess, restored = s, true
			failed = restoreSession(ed, opts.SessionDir, s)
			tree.SetShowHidden(s.ShowHidden)
			tree.SetShowIgnored(s.ShowIgnored)
		}
	}
	for _, f := range opts.Files {
		if err := ed.OpenFile(f.Path); err != nil {
			return nil, err
		}
		if f.Line > 0 {
			if err := ed.GotoLine(f.Line, f.Col); err != nil {
				return nil, err
			}
		}
	}
	if opts.Diff && len(ed.Tabs()) != 2 {
		return nil, errors.New("--diff needs two different files")
	}
	km, err := runKeymap(opts.Keymap, cfg)
	if err != nil {
		return nil, err
	}
	th, err := runTheme(opts.Theme, cfg)
	if err != nil {
		return nil, err
	}
	files := startFiles(ctx, root)
	watcher, events := startWatcher(watching)
	proj := newProjectOpener(ctx, watcher, root, files)
	deps := newUIDeps(ed, km, th, tree, files)
	deps.Settings = settingsFor(opts, cfg)
	icon := icons.Resolve(cfg.UIIcons, cfg.UIIconsFont, os.Getenv)
	deps.Settings.IconsMode, deps.Settings.IconsWhy = icon.Mode.String(), icon.Why
	deps.SwitchKeymap = switchKeymapFor(cfg)
	deps.LSP = newLSPPort(cfg.LSP)
	// A folder argument shows the explorer at startup. Without one the explorer
	// stays hidden until Ctrl+B, unless the session had it open.
	deps.ShowExplorer = opts.Root != "" || (restored && sess.Explorer)
	deps.HideLineNumbers = !cfg.LineNumbers
	deps.HideMenuBar = !cfg.ShowMenuBar
	deps.TabSize = cfg.TabSize
	deps.View = &ui.ViewOptions{
		ShowTabBar:              cfg.ShowTabBar,
		ShowStatusBar:           cfg.ShowStatusBar,
		ShowPromptLine:          cfg.ShowPromptLine,
		ShowVerticalScrollbar:   cfg.ShowVerticalScrollbar,
		ShowHorizontalScrollbar: cfg.ShowHorizontalScrollbar,
		LineWrap:                cfg.LineWrap,
		HighlightCurrentLine:    cfg.HighlightCurrentLine,
		HighlightOccurrences:    cfg.HighlightOccurrences,
		ShowWhitespace:          cfg.ShowWhitespace,
		CursorStyle:             cfg.CursorStyle,
		MouseCapture:            cfg.MouseCapture,
		MouseHover:              cfg.MouseHover,
		ScrollSync:              cfg.ScrollSync,
		UIIcons:                 icon.Mode.String(),
	}
	plugins, pluginNotices := loadPlugins(ed, deps.Editor, cfg.PluginTimeout)
	deps.Root = ui.RootPorts{
		Projects: proj,
		Grep:     liveGrep{},
		Git:      gitStatus{},
		Watch:    events,
		AutoSave: opts.AutoSaveInterval,
		Plugins:  plugins,
		Stashes:  stashesFor(opts.SessionDir),
	}
	if watcher != nil {
		// Assign only a running watcher: a nil *watch.Watcher in the interface is not nil.
		deps.Root.WatchDirs = watcher
	}
	// Notice is the first config problem, then the plugin load notices, then any
	// session file whose unsaved text was kept, for the status row. Every warning
	// still goes to stderr, before the alternate screen starts.
	var notices []string
	if len(diags) > 0 {
		notices = append(notices, diags[0].String())
	}
	notices = append(notices, pluginNotices...)
	if n := restoreNotice(failed); n != "" {
		notices = append(notices, n)
	}
	deps.Notice = strings.Join(notices, "; ")
	model := newUI(deps)
	model.SetDock(ui.DockPorts{Starter: termStarter{ctx: ctx}, Clipboard: systemClipboard{}})
	if opts.Diff {
		// The two files are in tabs 0 and 1: the tab count was checked above.
		model.OpenCompare(0, 1)
	}
	// File arguments skip the restore, so the session is not saved either. A save
	// would replace the saved session and remove the stash files that it names.
	sessionDir := opts.SessionDir
	if len(opts.Files) > 0 {
		sessionDir = ""
	}
	return &runtime{
		model:      model,
		ed:         ed,
		projects:   proj,
		watcher:    watcher,
		sessionDir: sessionDir,
		kept:       keptFiles(failed),
	}, nil
}

// keptFiles returns the files of failed, in order, for the session save to list.
func keptFiles(failed []unrestored) []session.FileState {
	files := make([]session.FileState, len(failed))
	for i, u := range failed {
		files[i] = u.file
	}
	return files
}

// startWatcher starts a file watcher when watching is set. It returns the watcher
// and its event channel, and both are nil when no watcher runs. The watcher is
// pointed at the tree root by newProjectOpener. A watcher that cannot start is a
// warning on stderr, and the tree then reloads only on Ctrl+R.
func startWatcher(watching bool) (*watch.Watcher, <-chan watch.Changed) {
	if !watching {
		return nil, nil
	}
	w, err := watch.New(watch.Options{})
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
	}
}

// runKeymap returns the keymap a session uses. The flag names a built-in keymap,
// and when it is empty the config keymap is used, and the platform default when
// the config sets none. The config keybindings are applied on top of that
// keymap. A bad name or a bad override stops startup with an error that names
// the config file when the config is the source.
func runKeymap(flag string, cfg config.Config) (*keymap.Map, error) {
	name := flag
	if name == "" {
		name = cfg.Keymap
	}
	km, err := keymapFor(name)
	if err != nil {
		if flag == "" {
			err = inConfigFile(cfg, "keymap", err)
		}
		return nil, err
	}
	return keymapWithOverrides(km, cfg)
}

// keymapWithOverrides applies the config keybindings to km. Without them km is
// returned unchanged. A bad override names the config file that set it.
func keymapWithOverrides(km *keymap.Map, cfg config.Config) (*keymap.Map, error) {
	if cfg.Keybindings == nil {
		return km, nil
	}
	out, err := km.WithOverrides(cfg.Keybindings)
	if err != nil {
		return nil, inConfigFile(cfg, "keybindings", err)
	}
	return out, nil
}

// switchKeymapFor returns the SwitchKeymap port of a session. It builds the named
// built-in keymap with the config keybindings applied, as runKeymap does, so an
// override survives a switch.
func switchKeymapFor(cfg config.Config) func(name string) (ui.Keymap, error) {
	return func(name string) (ui.Keymap, error) {
		km, err := keymapFor(name)
		if err != nil {
			return nil, err
		}
		km, err = keymapWithOverrides(km, cfg)
		if err != nil {
			return nil, err
		}
		return keymapAdapter{km: km}, nil
	}
}

// settingsFor returns the effective settings that open_settings lists. A flag wins
// over the config, as it does for the session. The keymap falls back to the platform
// default when neither names one. ConfigFile is the user file the session reads: the
// one named by --config, else the default user file, or empty when that is unknown.
func settingsFor(opts Options, cfg config.Config) ui.Settings {
	s := ui.Settings{
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

// keymapFor returns the named built-in keymap. An empty name is the platform
// default.
func keymapFor(name string) (*keymap.Map, error) {
	if name == "" {
		return keymap.Default(), nil
	}
	return keymap.ByName(name)
}

// printDiagnostics writes one warning line to w for each config problem. Loading
// does not stop at a problem, so these lines are the only report of a skipped
// setting.
func printDiagnostics(w io.Writer, diags []config.Diagnostic) {
	for _, d := range diags {
		_, _ = fmt.Fprintln(w, "gvim: warning: "+d.String())
	}
}
