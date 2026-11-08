package plugin

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/tbxark/gvim/plugin/bundled"
	"github.com/tbxark/gvim/pluginapi"
)

// Limits of what the loader reads from a plugin directory (AMENDMENTS C20: 256 KiB per
// .lua file, 1 MiB for plugin.json, 64 .lua files and 5 MiB per plugin). Other files of a
// plugin directory are never read by the loader and do not count: a plugin may ship data.
const (
	maxPluginFiles = 64
	maxPluginBytes = 5 << 20
	// maxWalkEntries bounds the directory entries visited while looking for .lua files, so a
	// vendored tree of unrealistic size cannot stall discovery. The walk then stops quietly:
	// the .lua files it has not reached are not loaded.
	maxWalkEntries = 100_000
	maxWalkDepth   = 8
	// maxStateFile caps a state file when it is read. The quota of gvim.state counts keys and
	// encoded values only; the file adds JSON framing and key escapes, so the cap is a multiple.
	maxStateFile = 8 * maxState
)

// compileHook is called in the discovery Work just before a plugin is compiled. Tests use
// it to prove that compiling never happens on the UI goroutine.
var compileHook func(plugin string)

// OpenFunc opens a directory as a file system confined to it (a symlink inside must not
// lead out) and returns the function that releases it. It is only called from Work.
type OpenFunc func(dir string) (fsys fs.FS, closeFn func(), err error)

// LoaderOptions configure plugin discovery (FINAL 4). A nil Options.Loader means the
// runtime discovers nothing: Start returns no Work and plugins are added with LoadPlugin.
type LoaderOptions struct {
	// Bundled holds the embedded plugins under plugins/<name>/ (nil: the binary's own).
	Bundled fs.FS
	// UserDir is the directory of user plugins, config.Dir()/plugins ("" none).
	UserDir string
	// InitFile is config.Dir()/init.lua ("" none).
	InitFile string
	// StateFile is config.Dir()/plugin-state.json, the manager's toggles ("" none).
	StateFile string
	// Paths are the --plugin arguments: a .lua file or a plugin directory each.
	Paths []string
	// Disabled is --no-plugins: neither bundled, user plugins nor init.lua are discovered.
	// Paths still load.
	Disabled bool
	// Enabled holds plugins.<name>.enabled of the configuration. A name present here is
	// locked: the manager cannot toggle it.
	Enabled map[string]bool
	// Settings holds plugins.<name>.settings of the configuration, per plugin name. They
	// reach the plugin as Source.Settings and Source.ProjectSettings.
	Settings map[string]PluginSettings
	// Open opens a directory for reading (nil: os.OpenRoot, which refuses symlinks that
	// leave the directory).
	Open OpenFunc
}

func (l *LoaderOptions) open(dir string) (fs.FS, func(), error) {
	if l.Open != nil {
		return l.Open(dir)
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, nil, err
	}
	return root.FS(), func() { _ = root.Close() }, nil
}

// openParent opens the directory of a single-file plugin or of init.lua. Unlike open it does
// not confine reads: the file may be a symlink into a dotfiles checkout (stow), which the
// user put there. Only directory plugins are confined to their directory.
func (l *LoaderOptions) openParent(dir string) (fs.FS, func(), error) {
	if l.Open != nil {
		return l.Open(dir)
	}
	return os.DirFS(dir), func() {}, nil
}

// PluginSettings are the plugins.<name>.settings objects of the two config layers, kept
// apart because the project layer is untrusted (AMENDMENTS C11).
type PluginSettings struct {
	User, Project map[string]any
}

// pluginRef says where a plugin comes from, so that it can be read again.
type pluginRef struct {
	kind string
	name string
	fsys fs.FS  // bundled: the plugin's own directory in the embedded tree
	dir  string // directory plugin: the plugin directory; single file: its parent
	file string // single file: its name; "" for a directory plugin
}

// rawFile is a file of a plugin as read: its source, or why it could not be read.
type rawFile struct {
	src string
	err error
}

// candidate is one plugin the loader knows: where it is, what its manifest says, whether
// it is enabled, and its compiled files.
type candidate struct {
	name, kind, version string
	man                 manifest
	ref                 pluginRef
	dir                 string // Source.Dir
	main                string
	seq                 int
	settings            PluginSettings

	raw      map[string]rawFile
	compiled map[string]*Compiled
	// problems lists every file that failed to read or compile ("chunk: first line").
	problems []string
	// err is why the plugin cannot load at all: bad name or manifest, API too new,
	// missing or broken entry, too many files.
	err string
	// reason is why an enabled, readable plugin is not running: a dependency problem or a
	// failed main chunk. Set while planning and loading.
	reason    string
	enabled   bool
	locked    string
	overrides []string // kinds of the plugins of the same name this one replaced

	// stateData is the content of the plugin's state file, read in the Work (nil: none or
	// not read). The loader hands it to SeedState before the main chunk runs.
	stateData []byte
}

// note is something discovery wants the user to hear about.
type note struct {
	text string
	warn bool
}

// discovery is the result of the discovery Work: opaque to everyone but the loader.
type discovery struct {
	cands  []*candidate // in source order, shadowed duplicates removed
	legacy []string     // *.js / *.ts files of the user plugin directory
	notes  []note
	state  pluginState
	// unread names the sources that could not be read at all (SourceUser: the plugin
	// directory could not be opened or listed) and why. Their plugins are neither new nor
	// gone: a reload keeps what runs.
	unread map[string]string
}

// ---------- the Work ----------

// discover returns the Work that finds, reads and compiles every plugin; then runs on the
// UI goroutine with the result. st is the toggle state to use (nil: read StateFile). The
// Work goroutine touches no Lua value and no Runtime field: opts, os and st are copies.
func (r *Runtime) discover(st *pluginState, then func(*discovery)) pluginapi.Work {
	opts := *r.ld.opt
	osys := r.os
	cfgDir := osys.Dirs().Config
	var snapshot *pluginState
	if st != nil {
		c := st.clone()
		snapshot = &c
	}
	return r.newWork(func(ctx context.Context) (any, string) {
		return opts.gather(ctx, osys, cfgDir, snapshot), ""
	}, func(v any, errText string) {
		d, _ := v.(*discovery)
		if d == nil {
			r.logf("", levelError, "plugin discovery failed: "+errText)
			d = &discovery{}
		}
		then(d)
	})
}

// gather scans every source, resolves name collisions, decides which plugins are enabled
// and compiles those.
func (l *LoaderOptions) gather(ctx context.Context, osys OS, cfgDir string, st *pluginState) *discovery {
	d := &discovery{}
	if st != nil {
		d.state = *st
	} else {
		d.state = readPluginState(ctx, osys, l.StateFile, &d.notes)
	}
	var all []*candidate
	if !l.Disabled {
		all = append(all, l.scanBundled(ctx)...)
		all = append(all, l.scanUser(ctx, d)...)
	}
	all = append(all, l.scanPaths(ctx)...)
	if !l.Disabled && l.InitFile != "" {
		if ref := l.initRef(); l.hasInit(ref, d) {
			all = append(all, l.scanRef(ref))
		}
	}
	d.cands = shadow(all, &d.notes)
	if !l.Disabled {
		l.noteUnknownConfig(d)
	}
	for i, c := range d.cands {
		c.seq = i
		c.decide(l.Enabled, &d.state)
		c.settings = l.Settings[c.name]
		if ctx.Err() != nil {
			break
		}
		if c.enabled && c.err == "" {
			c.compile()
			c.readState(ctx, osys, cfgDir)
		}
	}
	return d
}

// initRef is the reference of init.lua.
func (l *LoaderOptions) initRef() pluginRef {
	return pluginRef{kind: SourceInit, name: "init", dir: filepath.Dir(l.InitFile), file: filepath.Base(l.InitFile)}
}

// hasInit reports whether init.lua is there. A symlink counts even when its target is
// missing: reading it then fails and shows as a failed "init" row. A file that exists but
// cannot be inspected is reported in the notes.
func (l *LoaderOptions) hasInit(ref pluginRef, d *discovery) bool {
	fsys, closeFn, err := l.openParent(ref.dir)
	if err == nil {
		defer closeFn()
		_, err = fs.Lstat(fsys, ref.file)
	}
	switch {
	case err == nil:
		return true
	case errors.Is(err, fs.ErrNotExist):
		return false
	}
	d.notes = append(d.notes, note{text: fmt.Sprintf("cannot read %s: %v", l.InitFile, err), warn: true})
	return false
}

func (l *LoaderOptions) scanBundled(ctx context.Context) []*candidate {
	root := l.Bundled
	if root == nil {
		root = bundled.FS
	}
	sub, err := fs.Sub(root, "plugins")
	if err != nil {
		return nil
	}
	entries, err := fs.ReadDir(sub, ".")
	if err != nil {
		return nil
	}
	var out []*candidate
	for _, e := range entries {
		if !e.IsDir() || ctx.Err() != nil {
			continue
		}
		dsub, err := fs.Sub(sub, e.Name())
		if err != nil {
			continue
		}
		out = append(out, l.scanRef(pluginRef{kind: SourceBundled, name: e.Name(), fsys: dsub}))
	}
	return out
}

func (d *discovery) unreadUser(text string) {
	d.notes = append(d.notes, note{text: text, warn: true})
	if d.unread == nil {
		d.unread = map[string]string{}
	}
	d.unread[SourceUser] = text
}

func (l *LoaderOptions) scanUser(ctx context.Context, d *discovery) []*candidate {
	if l.UserDir == "" {
		return nil
	}
	fsys, closeFn, err := l.open(l.UserDir)
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			d.unreadUser(fmt.Sprintf("cannot read the plugin directory %s: %v", l.UserDir, err))
		}
		return nil
	}
	entries, err := fs.ReadDir(fsys, ".")
	closeFn()
	if err != nil {
		d.unreadUser(fmt.Sprintf("cannot list the plugin directory %s: %v", l.UserDir, err))
		return nil
	}
	var out []*candidate
	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, ".") || ctx.Err() != nil {
			continue
		}
		full := filepath.Join(l.UserDir, name)
		isDir := e.IsDir()
		if !isDir && e.Type()&fs.ModeSymlink != 0 {
			// A symlink to a directory is a directory plugin (a development checkout).
			if _, c, err := l.open(full); err == nil {
				c()
				isDir = true
			}
		}
		switch {
		case isDir:
			out = append(out, l.scanRef(pluginRef{kind: SourceUser, name: name, dir: full}))
		case strings.HasSuffix(name, ".lua"):
			out = append(out, l.scanRef(pluginRef{kind: SourceUser, name: strings.TrimSuffix(name, ".lua"), dir: l.UserDir, file: name}))
		case (strings.HasSuffix(name, ".js") || strings.HasSuffix(name, ".ts")) && !strings.HasSuffix(name, ".d.ts"):
			d.legacy = append(d.legacy, name)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].name < out[j].name })
	return out
}

func (l *LoaderOptions) scanPaths(ctx context.Context) []*candidate {
	var out []*candidate
	for _, p := range l.Paths {
		if ctx.Err() != nil {
			break
		}
		abs, err := filepath.Abs(p)
		if err != nil {
			abs = p
		}
		if _, closeFn, err := l.open(abs); err == nil {
			closeFn()
			out = append(out, l.scanRef(pluginRef{kind: SourceCLI, name: filepath.Base(abs), dir: abs}))
			continue
		}
		base := filepath.Base(abs)
		ref := pluginRef{kind: SourceCLI, name: strings.TrimSuffix(base, ".lua"), dir: filepath.Dir(abs), file: base}
		c := l.scanRef(ref)
		if !strings.HasSuffix(base, ".lua") {
			c.err = fmt.Sprintf("--plugin %s: not a .lua file or a plugin directory", p)
		}
		out = append(out, c)
	}
	return out
}

// shadow removes candidates that another candidate of the same name replaces: cli over
// user over bundled; of two user entries, the directory wins over the file; of two cli
// entries the later wins. init is reserved.
func shadow(all []*candidate, notes *[]note) []*candidate {
	rank := map[string]int{SourceBundled: 1, SourceUser: 2, SourceCLI: 3}
	winner := map[string]*candidate{}
	drop := map[*candidate]bool{}
	for _, c := range all {
		if c.kind != SourceInit && c.name == "init" {
			*notes = append(*notes, note{text: `"init" is a reserved plugin name (use init.lua in the configuration directory)`, warn: true})
			drop[c] = true
			continue
		}
		prev := winner[c.name]
		if prev == nil || c.kind == SourceInit {
			winner[c.name] = c
			continue
		}
		var keep, lose *candidate
		switch {
		case rank[c.kind] > rank[prev.kind]:
			keep, lose = c, prev
		case rank[c.kind] < rank[prev.kind]:
			keep, lose = prev, c
		case c.kind == SourceUser:
			keep, lose = prev, c
			if prev.ref.file != "" && c.ref.file == "" {
				keep, lose = c, prev
			}
			*notes = append(*notes, note{text: fmt.Sprintf("two plugins named %q in the plugin directory: the directory is used, %s is ignored", c.name, lose.ref.file), warn: true})
		default: // two --plugin paths of one name: the later one
			keep, lose = c, prev
		}
		keep.overrides = append(keep.overrides, lose.overrides...)
		keep.overrides = append(keep.overrides, lose.kind)
		drop[lose] = true
		winner[c.name] = keep
	}
	out := make([]*candidate, 0, len(all))
	for _, c := range all {
		if !drop[c] {
			out = append(out, c)
		}
	}
	return out
}

// ---------- scanning one plugin ----------

// scanRef reads the files of one plugin. It never fails: a plugin that cannot be read is
// a candidate with err set.
func (l *LoaderOptions) scanRef(ref pluginRef) *candidate {
	c := &candidate{ref: ref, kind: ref.kind, name: ref.name, main: "init.lua", raw: map[string]rawFile{}}
	if ref.file != "" {
		l.scanFile(c)
	} else {
		l.scanDir(c)
	}
	if c.err == "" && !nameRE.MatchString(c.name) {
		c.err = fmt.Sprintf("invalid plugin name %q (want [a-z][a-z0-9_]*, at most 40 characters)", c.name)
	}
	if c.err == "" {
		for _, r := range c.man.Requires {
			if r == c.name {
				c.err = "plugin.json: a plugin cannot require itself"
			}
		}
	}
	return c
}

func (l *LoaderOptions) scanFile(c *candidate) {
	c.main = c.ref.file
	fsys, closeFn, err := l.openParent(c.ref.dir)
	if err != nil {
		c.err = fmt.Sprintf("cannot read %s: %v", filepath.Join(c.ref.dir, c.ref.file), err)
		return
	}
	defer closeFn()
	c.raw[c.main] = readLua(fsys, c.ref.file, c.name)
}

func (l *LoaderOptions) scanDir(c *candidate) {
	fsys, closeFn := c.ref.fsys, func() {}
	if fsys == nil {
		var err error
		fsys, closeFn, err = l.open(c.ref.dir)
		if err != nil {
			c.err = fmt.Sprintf("cannot read the plugin directory %s: %v", c.ref.dir, err)
			return
		}
		c.dir = c.ref.dir
	}
	defer closeFn()

	data, err := readCapped(fsys, "plugin.json", MaxManifest)
	switch {
	case err == nil:
		m, merr := parseManifest(data)
		if merr != nil {
			c.err = merr.Error()
			return
		}
		c.man, c.version = m, m.Version
		if m.Name != "" {
			if c.kind == SourceCLI {
				c.name = m.Name
			} else if m.Name != c.name {
				c.err = fmt.Sprintf("plugin.json: name %q does not match the directory name %q", m.Name, c.name)
				return
			}
		}
		if m.Main != "" {
			c.main = m.Main
		}
	case errors.Is(err, fs.ErrNotExist):
	default:
		c.err = fmt.Sprintf("plugin.json: %v", err)
		return
	}

	var total int64
	entries, truncated := 0, false
	werr := fs.WalkDir(fsys, ".", func(p string, de fs.DirEntry, err error) error {
		if err != nil {
			if p == "." {
				return err
			}
			return fs.SkipDir
		}
		entries++
		if entries > maxWalkEntries {
			truncated = true
			return fs.SkipAll
		}
		if de.IsDir() {
			if p != "." && (strings.HasPrefix(de.Name(), ".") || strings.Count(p, "/") >= maxWalkDepth) {
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(p, ".lua") {
			return nil
		}
		if len(c.raw) >= maxPluginFiles {
			c.err = fmt.Sprintf("too many .lua files (more than %d)", maxPluginFiles)
			return fs.SkipAll
		}
		f := readLua(fsys, p, c.name)
		total += int64(len(f.src))
		if total > maxPluginBytes {
			c.err = fmt.Sprintf("the .lua files are larger than %d MiB", maxPluginBytes>>20)
			return fs.SkipAll
		}
		c.raw[p] = f
		return nil
	})
	if c.err != "" {
		return
	}
	if werr != nil && !errors.Is(werr, fs.SkipAll) {
		c.err = fmt.Sprintf("cannot read the plugin files: %v", werr)
		return
	}
	if _, ok := c.raw[c.main]; !ok {
		c.err = fmt.Sprintf("main file %q not found", c.main)
		if truncated {
			c.err += fmt.Sprintf(" (the plugin directory has more than %d entries; the search stopped)", maxWalkEntries)
		}
	}
}

// readLua reads one .lua file; a file over the cap is not read at all.
func readLua(fsys fs.FS, name, plugin string) rawFile {
	if fi, err := fs.Stat(fsys, name); err == nil && fi.Size() > MaxLuaFile {
		return rawFile{err: pluginapi.Errf(pluginapi.CodeLimit, "%s/%s: source too large (%d bytes, max %d)", plugin, name, fi.Size(), MaxLuaFile)}
	}
	data, err := readCapped(fsys, name, MaxLuaFile)
	if err != nil {
		var pe *pluginapi.Error
		if errors.As(err, &pe) {
			return rawFile{err: pluginapi.Errf(pe.Code, "%s/%s: %s", plugin, name, pe.Msg)}
		}
		return rawFile{err: pluginapi.Errf(pluginapi.CodeIO, "%s/%s: %v", plugin, name, err)}
	}
	return rawFile{src: string(data)}
}

// readCapped reads a file of at most max bytes; a larger one is a CodeTooLarge error.
// Anything but a regular file is refused before it is opened: open(2) of a FIFO waits for a
// writer and would hold the whole discovery Work forever.
func readCapped(fsys fs.FS, name string, max int64) ([]byte, error) {
	fi, err := fs.Stat(fsys, name)
	if err != nil {
		return nil, err
	}
	if !fi.Mode().IsRegular() {
		return nil, pluginapi.Errf(pluginapi.CodeIO, "not a regular file")
	}
	f, err := fsys.Open(name)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	data, err := io.ReadAll(io.LimitReader(f, max+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > max {
		return nil, pluginapi.Errf(pluginapi.CodeTooLarge, "file is larger than %d bytes", max)
	}
	return data, nil
}

// ---------- enablement and compiling ----------

// decide sets enabled and locked: plugins.<name>.enabled of the configuration wins, then
// the manager's toggles, then default_enabled (bundled plugins), then true. Plugins given
// with --plugin and init.lua are always enabled.
func (c *candidate) decide(cfg map[string]bool, st *pluginState) {
	c.enabled, c.locked = true, ""
	if c.kind != SourceBundled && c.kind != SourceUser {
		return
	}
	switch v, ok := cfg[c.name]; {
	case ok:
		c.enabled, c.locked = v, "set in config"
	case st.disabled[c.name]:
		c.enabled = false
	case st.enabled[c.name]:
	case c.kind == SourceBundled && c.man.DefaultEnabled != nil:
		c.enabled = *c.man.DefaultEnabled
	}
}

// compile compiles every readable .lua file (AMENDMENTS C20). It runs in Work. The
// entry file's failure becomes err; failures of other files are kept in problems and
// surface when the file is required, or abort a reload.
func (c *candidate) compile() {
	if compileHook != nil {
		compileHook(c.name)
	}
	files := make(map[string]string, len(c.raw))
	c.compiled = make(map[string]*Compiled, len(c.raw))
	for rel, f := range c.raw {
		if f.err != nil {
			c.compiled[rel] = &Compiled{Err: f.err}
			continue
		}
		files[rel] = f.src
	}
	for rel, cc := range Compile(c.name, files) {
		c.compiled[rel] = cc
	}
	c.raw = nil
	rels := make([]string, 0, len(c.compiled))
	for rel := range c.compiled {
		rels = append(rels, rel)
	}
	sort.Strings(rels)
	for _, rel := range rels {
		if cc := c.compiled[rel]; cc.Err != nil {
			c.problems = append(c.problems, firstLine(cc.Err.Error()))
		}
	}
	if cc := c.compiled[c.main]; cc == nil {
		c.err = fmt.Sprintf("main file %q not found", c.main)
	} else if cc.Err != nil {
		c.err = firstLine(cc.Err.Error())
	}
}

// entry is the absolute path of the file the plugin runs from, "" when it has none on disk
// (a bundled plugin lives in the embedded tree).
func (c *candidate) entry() string {
	switch {
	case c.ref.file != "":
		return filepath.Join(c.ref.dir, c.ref.file)
	case c.dir != "":
		return filepath.Join(c.dir, filepath.FromSlash(c.main))
	}
	return ""
}

// source returns what LoadPlugin needs.
func (c *candidate) source() Source {
	return Source{Name: c.name, Version: c.version, Kind: c.kind, Dir: c.dir, File: c.entry(), Main: c.main, Compiled: c.compiled,
		Settings: c.settings.User, ProjectSettings: c.settings.Project}
}

// scanOne reads one plugin again for reload and enable: scan and compile, no shadowing.
func (l *LoaderOptions) scanOne(ctx context.Context, osys OS, cfgDir string, ref pluginRef, cfg map[string]bool, st *pluginState) *candidate {
	c := l.scanRef(ref)
	c.decide(cfg, st)
	c.settings = l.Settings[c.name]
	if c.err == "" {
		c.compile()
		if c.enabled {
			c.readState(ctx, osys, cfgDir)
		}
	}
	return c
}

// readState reads the plugin's state file (see Runtime.StateFile). A missing or unreadable
// file is no state; one over the cap is ignored.
func (c *candidate) readState(ctx context.Context, osys OS, cfgDir string) {
	path := stateFilePath(cfgDir, c.name)
	if path == "" {
		return
	}
	if data, err := osys.ReadFile(ctx, path, maxStateFile); err == nil {
		c.stateData = data
	}
}

// compileAbandon is how long the Work of a buffer load waits for the compiler.
const compileAbandon = 2 * time.Second

// compileText compiles a buffer's text, giving up after compileAbandon (the compile's CPU
// is bounded by the size cap; an abandoned result is ignored).
func compileText(ctx context.Context, name, src string) *candidate {
	c := &candidate{name: name, kind: SourceBuffer, main: "init.lua", raw: map[string]rawFile{"init.lua": {src: src}}, enabled: true}
	done := make(chan struct{})
	go func() {
		c.compile()
		close(done)
	}()
	select {
	case <-done:
		return c
	case <-time.After(compileAbandon):
	case <-ctx.Done():
	}
	return &candidate{name: name, kind: SourceBuffer, err: "timeout: compiling took too long, abandoned"}
}

// noteUnknownConfig adds a notice for each plugin the configuration names (plugins.<name>)
// that is not installed: a misspelt name would otherwise configure nothing and say nothing.
func (l *LoaderOptions) noteUnknownConfig(d *discovery) {
	known := make(map[string]bool, len(d.cands))
	for _, c := range d.cands {
		known[c.name] = true
	}
	var bad []string
	add := func(name string) {
		if !known[name] && !slices.Contains(bad, name) && !d.state.warnedUnknown[name] {
			bad = append(bad, name)
		}
	}
	for name := range l.Enabled {
		add(name)
	}
	for name := range l.Settings {
		add(name)
	}
	sort.Strings(bad)
	if d.state.warnedUnknown == nil {
		d.state.warnedUnknown = map[string]bool{}
	}
	for _, name := range bad {
		d.state.warnedUnknown[name] = true
		d.notes = append(d.notes, note{text: fmt.Sprintf("config plugins.%s: no plugin of that name is installed (a typo?)", name), warn: true})
	}
}
