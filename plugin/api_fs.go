package plugin

import (
	"context"
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"
	"time"

	lua "github.com/yuin/gopher-lua"

	"github.com/tbxark/gvim/plugin/hostos"
	"github.com/tbxark/gvim/pluginapi"
)

// gvim.fs (FINAL 5.8, AMENDMENTS C16). Every call except watch is awaitable: its Work runs
// the OS call off the UI goroutine. The limits of the spec are enforced here, so they hold
// for every OS implementation.

const (
	maxFSRead      = 16 << 20 // fs.read
	maxFSWrite     = MaxString
	maxPathLen     = 4096
	defaultWalk    = 50_000
	maxWalk        = 100_000 // also the most entries fs.list returns: the result is converted to Lua on the UI goroutine
	quotaWatchers  = 32
	defaultWatchMS = 50
	minWatchMS     = 10
	maxWatchMS     = 5000
)

func init() {
	registerIO("fs_read", prepFSRead)
	registerIO("fs_write", prepFSWrite)
	registerIO("fs_list", prepFSList)
	registerIO("fs_stat", prepFSStat)
	registerIO("fs_exists", prepFSExists)
	registerIO("fs_mkdir", prepFSMkdir)
	registerIO("fs_remove", prepFSRemove)
	registerIO("fs_rename", prepFSTwoPaths("rename", func(ctx context.Context, o hostos.OS, a, b string) error { return o.Rename(ctx, a, b) }))
	registerIO("fs_copy", prepFSTwoPaths("copy", func(ctx context.Context, o hostos.OS, a, b string) error { return o.Copy(ctx, a, b) }))
	registerIO("fs_realpath", prepFSRealpath)
	registerIO("fs_walk", prepFSWalk)
}

// ---------- the generic IO await ----------

// ioJob is the body of an IO Work: plain Go in, plain Go out, on a Work goroutine. The
// returned values are what the awaiting Lua call returns.
type ioJob func(ctx context.Context, o hostos.OS) ([]any, error)

// registerIO registers an await kind whose handler validates the arguments (prep calls
// c.Bad or c.Fail and returns nil on a bad call) and runs the returned job in a Work.
func registerIO(kind string, prep func(c *awaitCall) ioJob) {
	registerAwait(kind, func(c *awaitCall) {
		job := prep(c)
		if job == nil {
			return
		}
		if err := c.CheckPark(true); err != nil {
			c.Fail(err)
			return
		}
		o := c.r.os
		c.Park(&parkSpec{Work: &pluginapi.Work{Run: func(ctx context.Context) pluginapi.Completion {
			vals, err := job(ctx, o)
			if err != nil {
				return pluginapi.Completion{Err: errString(err)}
			}
			return pluginapi.Completion{Values: vals}
		}}})
	})
}

// ---------- argument helpers ----------

// optReader reads an options table; the first problem is kept in bad.
type optReader struct {
	t   *lua.LTable
	bad string
}

func (o *optReader) get(name string) lua.LValue {
	if o.t == nil {
		return lua.LNil
	}
	return o.t.RawGetString(name)
}

func (o *optReader) fail(format string, a ...any) {
	if o.bad == "" {
		o.bad = fmt.Sprintf(format, a...)
	}
}

func (o *optReader) boolean(name string, def bool) bool {
	switch v := o.get(name).(type) {
	case lua.LBool:
		return bool(v)
	case *lua.LNilType:
		return def
	}
	o.fail("option %s must be a boolean", name)
	return def
}

// number reads an optional number, clamped to [lo, hi].
func (o *optReader) number(name string, def, lo, hi float64) float64 {
	switch v := o.get(name).(type) {
	case lua.LNumber:
		f := float64(v)
		if f != f {
			o.fail("option %s must be a number", name)
			return def
		}
		return min(max(f, lo), hi)
	case *lua.LNilType:
		return def
	}
	o.fail("option %s must be a number", name)
	return def
}

func (o *optReader) str(name string) string {
	switch v := o.get(name).(type) {
	case lua.LString:
		return string(v)
	case *lua.LNilType:
		return ""
	}
	o.fail("option %s must be a string", name)
	return ""
}

func (o *optReader) strings(name string, limit int) []string {
	switch v := o.get(name).(type) {
	case *lua.LTable:
		var out []string
		for i := 1; i <= v.Len(); i++ {
			s, ok := v.RawGetInt(i).(lua.LString)
			if !ok {
				o.fail("option %s must be a list of strings", name)
				return nil
			}
			if len(out) >= limit {
				o.fail("option %s has more than %d entries", name, limit)
				return nil
			}
			out = append(out, string(s))
		}
		return out
	case *lua.LNilType:
		return nil
	}
	o.fail("option %s must be a list of strings", name)
	return nil
}

// optionsArg reads the optional options table at args[i].
func optionsArg(args []lua.LValue, i int) *optReader {
	o := &optReader{}
	if i >= len(args) || args[i] == lua.LNil {
		return o
	}
	t, ok := args[i].(*lua.LTable)
	if !ok {
		o.bad = "table expected"
		return o
	}
	o.t = t
	return o
}

// stringArg returns args[i] as a string (a number is converted), or "" and false.
func stringArg(args []lua.LValue, i int) (string, bool) {
	if i >= len(args) {
		return "", false
	}
	switch v := args[i].(type) {
	case lua.LString:
		return string(v), true
	case lua.LNumber:
		return v.String(), true
	}
	return "", false
}

// checkPathText rejects paths no OS call could meaningfully take.
func checkPathText(p string) error {
	switch {
	case p == "":
		return pluginapi.Errf(pluginapi.CodeInvalid, "empty path")
	case len(p) > maxPathLen:
		return pluginapi.Errf(pluginapi.CodeInvalid, "path longer than %d bytes", maxPathLen)
	case strings.ContainsRune(p, 0):
		return pluginapi.Errf(pluginapi.CodeInvalid, "path contains a NUL byte")
	}
	return nil
}

// pathArg returns the path argument i (0-based). On a bad call it finishes c and
// reports false.
func (c *awaitCall) pathArg(i int, fname string) (string, bool) {
	s, ok := stringArg(c.args, i)
	if !ok {
		c.Bad(i+1, fname, "string expected")
		return "", false
	}
	if err := checkPathText(s); err != nil {
		c.Fail(err)
		return "", false
	}
	return s, true
}

// options returns the options reader at argument i; a bad table finishes c.
func (c *awaitCall) options(i int, fname string) (*optReader, bool) {
	o := optionsArg(c.args, i)
	if o.bad != "" {
		c.Bad(i+1, fname, o.bad)
		return nil, false
	}
	return o, true
}

// optionsDone finishes c with a bad-argument error when reading the options went wrong.
func (c *awaitCall) optionsDone(i int, fname string, o *optReader) bool {
	if o.bad != "" {
		c.Bad(i+1, fname, o.bad)
		return false
	}
	return true
}

// unixSeconds is a time as seconds since the epoch with sub-second precision (0 for the
// zero time).
func unixSeconds(t time.Time) float64 {
	if t.IsZero() {
		return 0
	}
	return float64(t.UnixNano()) / 1e9
}

// ---------- the calls ----------

func prepFSRead(c *awaitCall) ioJob {
	path, ok := c.pathArg(0, "read")
	if !ok {
		return nil
	}
	o, ok := c.options(1, "read")
	if !ok {
		return nil
	}
	limit := int64(o.number("max", maxFSRead, 1, maxFSRead))
	if !c.optionsDone(1, "read", o) {
		return nil
	}
	return func(ctx context.Context, os hostos.OS) ([]any, error) {
		data, err := os.ReadFile(ctx, path, limit)
		if err != nil {
			return nil, err
		}
		return []any{string(data)}, nil
	}
}

func prepFSWrite(c *awaitCall) ioJob {
	path, ok := c.pathArg(0, "write")
	if !ok {
		return nil
	}
	data, ok := stringArg(c.args, 1)
	if !ok {
		c.Bad(2, "write", "string expected")
		return nil
	}
	if len(data) > maxFSWrite {
		c.Fail(pluginapi.Errf(pluginapi.CodeTooLarge, "data is larger than %d bytes", maxFSWrite))
		return nil
	}
	o, ok := c.options(2, "write")
	if !ok {
		return nil
	}
	wo := hostos.WriteOptions{
		Atomic:    o.boolean("atomic", true),
		Append:    o.boolean("append", false),
		Mode:      fs.FileMode(o.number("mode", 0, 0, 0o7777)),
		Parents:   o.boolean("parents", false),
		Exclusive: o.boolean("exclusive", false),
	}
	if !c.optionsDone(2, "write", o) {
		return nil
	}
	if wo.Append {
		wo.Atomic = false // appending to a copy and renaming would lose concurrent appends
	}
	if wo.Exclusive {
		if wo.Append {
			c.Fail(pluginapi.Errf(pluginapi.CodeInvalid, "write: a create-new (exclusive) cannot append"))
			return nil
		}
		if wo.Parents {
			c.Fail(pluginapi.Errf(pluginapi.CodeInvalid, "write: a create-new (exclusive) does not create parent directories"))
			return nil
		}
		// A create-new is not a replace: writing through a temp file and renaming would
		// defeat the exclusive claim, so the backends create the target directly.
		wo.Atomic = false
	}
	return func(ctx context.Context, os hostos.OS) ([]any, error) {
		if err := os.WriteFile(ctx, path, []byte(data), wo); err != nil {
			return nil, err
		}
		return []any{true}, nil
	}
}

func prepFSList(c *awaitCall) ioJob {
	path, ok := c.pathArg(0, "list")
	if !ok {
		return nil
	}
	o, ok := c.options(1, "list")
	if !ok {
		return nil
	}
	hidden := o.boolean("hidden", false)
	if !c.optionsDone(1, "list", o) {
		return nil
	}
	return func(ctx context.Context, os hostos.OS) ([]any, error) {
		ents, err := os.ReadDir(ctx, path, hidden)
		if err != nil {
			return nil, err
		}
		if len(ents) > maxWalk {
			return nil, pluginapi.Errf(pluginapi.CodeTooLarge, "directory has more than %d entries; use fs.walk with max_entries", maxWalk)
		}
		out := make([]any, len(ents))
		for i, e := range ents {
			m := map[string]any{"name": e.Name, "kind": e.Kind, "size": e.Size, "mtime": unixSeconds(e.MTime)}
			if e.TargetKind != "" {
				m["target_kind"] = e.TargetKind
			}
			out[i] = m
		}
		return []any{out}, nil
	}
}

func prepFSStat(c *awaitCall) ioJob {
	path, ok := c.pathArg(0, "stat")
	if !ok {
		return nil
	}
	return func(ctx context.Context, os hostos.OS) ([]any, error) {
		st, err := os.Stat(ctx, path)
		if err != nil {
			return nil, err
		}
		return []any{map[string]any{"kind": st.Kind, "size": st.Size, "mtime": unixSeconds(st.MTime), "mode": int64(st.Mode.Perm())}}, nil
	}
}

func prepFSExists(c *awaitCall) ioJob {
	path, ok := c.pathArg(0, "exists")
	if !ok {
		return nil
	}
	return func(ctx context.Context, os hostos.OS) ([]any, error) {
		if _, err := os.Stat(ctx, path); err != nil {
			if pluginapi.CodeOf(err) == pluginapi.CodeNotFound {
				return []any{false}, nil
			}
			return nil, err
		}
		return []any{true}, nil
	}
}

func prepFSMkdir(c *awaitCall) ioJob {
	path, ok := c.pathArg(0, "mkdir")
	if !ok {
		return nil
	}
	o, ok := c.options(1, "mkdir")
	if !ok {
		return nil
	}
	parents := o.boolean("parents", true)
	if !c.optionsDone(1, "mkdir", o) {
		return nil
	}
	return func(ctx context.Context, os hostos.OS) ([]any, error) {
		if err := os.Mkdir(ctx, path, parents); err != nil {
			return nil, err
		}
		return []any{true}, nil
	}
}

func prepFSTwoPaths(fname string, do func(ctx context.Context, o hostos.OS, a, b string) error) func(c *awaitCall) ioJob {
	return func(c *awaitCall) ioJob {
		a, ok := c.pathArg(0, fname)
		if !ok {
			return nil
		}
		b, ok := c.pathArg(1, fname)
		if !ok {
			return nil
		}
		return func(ctx context.Context, os hostos.OS) ([]any, error) {
			if err := do(ctx, os, a, b); err != nil {
				return nil, err
			}
			return []any{true}, nil
		}
	}
}

func prepFSRealpath(c *awaitCall) ioJob {
	path, ok := c.pathArg(0, "realpath")
	if !ok {
		return nil
	}
	return func(ctx context.Context, os hostos.OS) ([]any, error) {
		p, err := os.RealPath(ctx, path)
		if err != nil {
			return nil, err
		}
		return []any{p}, nil
	}
}

func prepFSWalk(c *awaitCall) ioJob {
	root, ok := c.pathArg(0, "walk")
	if !ok {
		return nil
	}
	o, ok := c.options(1, "walk")
	if !ok {
		return nil
	}
	wo := hostos.WalkOptions{
		SkipDirs:   o.strings("skip_dirs", 1000),
		Hidden:     o.boolean("hidden", false),
		MaxDepth:   int(o.number("max_depth", 0, 0, 1<<20)),
		MaxEntries: int(o.number("max_entries", defaultWalk, 1, maxWalk)),
	}
	if !c.optionsDone(1, "walk", o) {
		return nil
	}
	return func(ctx context.Context, os hostos.OS) ([]any, error) {
		res, err := os.Walk(ctx, root, wo)
		if err != nil {
			return nil, err
		}
		ents := make([]any, len(res.Entries))
		for i, e := range res.Entries {
			ents[i] = map[string]any{"path": e.Path, "rel": e.Rel, "kind": e.Kind, "size": e.Size, "mtime": unixSeconds(e.MTime)}
		}
		return []any{map[string]any{"entries": ents, "truncated": res.Truncated}}, nil
	}
}

// ---------- remove: the protected directories ----------

func prepFSRemove(c *awaitCall) ioJob {
	path, ok := c.pathArg(0, "remove")
	if !ok {
		return nil
	}
	o, ok := c.options(1, "remove")
	if !ok {
		return nil
	}
	recursive := o.boolean("recursive", false)
	if !c.optionsDone(1, "remove", o) {
		return nil
	}
	dirs := c.r.os.Dirs()
	abs := absPath(dirs.Cwd, path)
	protected := []string{dirs.Home, dirs.Root, dirs.Config, dirs.Cwd}
	return func(ctx context.Context, os hostos.OS) ([]any, error) {
		// What is checked is what is removed: the OS gets the cleaned absolute path.
		if err := refuseProtected(ctx, os, abs, protected); err != nil {
			return nil, err
		}
		if err := os.Remove(ctx, abs, recursive); err != nil {
			return nil, err
		}
		return []any{true}, nil
	}
}

// absPath makes p absolute against cwd and cleans it.
func absPath(cwd, p string) string {
	if !filepath.IsAbs(p) && cwd != "" {
		p = filepath.Join(cwd, p)
	}
	if abs, err := filepath.Abs(p); err == nil {
		p = abs
	}
	return filepath.Clean(p)
}

// refuseProtected is the rule of fs.remove: the filesystem root, the home directory, the
// workspace root, the working directory, the config directory and every directory that
// contains one of them are never removed, even recursively. Symbolic links in the parent
// of the target are resolved first, so a link cannot name a protected directory.
func refuseProtected(ctx context.Context, o hostos.OS, abs string, protected []string) error {
	targets := []string{abs}
	if parent, err := o.RealPath(ctx, filepath.Dir(abs)); err == nil {
		if real := filepath.Join(parent, filepath.Base(abs)); real != abs {
			targets = append(targets, real)
		}
	}
	var guard []string
	for _, p := range protected {
		if p == "" {
			continue
		}
		p = filepath.Clean(p)
		guard = append(guard, p)
		if real, err := o.RealPath(ctx, p); err == nil && real != p {
			guard = append(guard, real)
		}
	}
	for _, t := range targets {
		if t == filepath.VolumeName(t)+string(filepath.Separator) {
			return pluginapi.Errf(pluginapi.CodeInvalid, "refusing to remove the filesystem root")
		}
		for _, g := range guard {
			if t == g || strings.HasPrefix(g, t+string(filepath.Separator)) {
				return pluginapi.Errf(pluginapi.CodeInvalid, "refusing to remove %s: it is or contains the home, workspace or config directory", abs)
			}
		}
	}
	return nil
}

// ---------- watchers ----------

const kindWatcher = "Watcher"

// watcher is the Watcher handle: one fsnotify subscription whose events run fn.
type watcher struct {
	p      *plugin
	id     int
	fn     *lua.LFunction
	active bool
	cancel context.CancelFunc
	w      *wait
	live   *int
}

func (w *watcher) handleString() string { return fmt.Sprint(w.id) }

func (w *watcher) stop() {
	if !w.active {
		return
	}
	w.active = false
	*w.live--
	w.cancel()
	w.p.r.dropWait(w.w)
}

// onItem handles one item of the watch stream on the UI goroutine.
func (w *watcher) onItem(c pluginapi.Completion) {
	if !w.active {
		return
	}
	var ev map[string]any
	switch {
	case c.Kind == "error" || c.Err != "":
		ev = map[string]any{"kind": "error", "err": c.Err}
	case len(c.Values) > 0:
		ev, _ = c.Values[0].(map[string]any)
	}
	if ev != nil {
		if _, err := w.p.timerTask("watch event", w.fn, w.p.toLua(ev)); err != nil {
			w.p.r.logf(w.p.name, levelWarn, "dropped watch event: "+err.Error())
		}
	}
	if !c.More {
		w.active = false
		*w.live--
		w.cancel()
		w.p.r.dropWait(w.w)
	}
}

func installFS(e *apiEnv) {
	p, r := e.p, e.r
	area := e.area("fs")
	for _, k := range []string{"read", "write", "list", "stat", "exists", "mkdir", "remove", "rename", "copy", "realpath", "walk"} {
		area.RawSetString(k, e.async("fs_"+k))
	}

	live, seq := 0, 0
	methods := e.kind(kindWatcher).methods
	e.set(methods, "stop", func(L *lua.LState) int {
		checkObject(L, 1, kindWatcher).(*watcher).stop()
		L.Push(lua.LTrue)
		return 1
	})
	e.set(methods, "active", func(L *lua.LState) int {
		L.Push(lua.LBool(checkObject(L, 1, kindWatcher).(*watcher).active))
		return 1
	})

	e.set(area, "watch", func(L *lua.LState) int {
		path := L.CheckString(1)
		fn := L.CheckFunction(2)
		o := optionsArg(argsFrom(L, 1), 2)
		debounce := time.Duration(o.number("debounce_ms", defaultWatchMS, minWatchMS, maxWatchMS)) * time.Millisecond
		if o.bad != "" {
			L.ArgError(3, o.bad)
		}
		if err := checkPathText(path); err != nil {
			v, s := errTuple(err)
			L.Push(v)
			L.Push(s)
			return 2
		}
		if live >= quotaWatchers {
			v, s := errTuple(pluginapi.Errf(pluginapi.CodeLimit, "watcher quota (%d)", quotaWatchers))
			L.Push(v)
			L.Push(s)
			return 2
		}
		live++
		seq++
		wctx, cancel := context.WithCancel(p.pctx)
		w := &watcher{p: p, id: seq, fn: fn, active: true, cancel: cancel, live: &live}
		tok, wt := p.newWait(w.onItem, func() { // unload
			if w.active {
				w.active = false
				live--
			}
			cancel()
		})
		w.w = wt
		p.queueWork(tok, pluginapi.Work{Ctx: wctx, Stream: watchStream(r.os, path, hostos.WatchOptions{Debounce: debounce})})
		L.Push(p.newObject(kindWatcher, w))
		return 1
	})
}

// watchStream is the Work of a watcher: it adds the OS watch (a failure is the final item,
// delivered once), then hands out events until the watch ends or ctx is cancelled. Every
// item, final ones included, is returned with ok = true; the final one has More unset.
func watchStream(o hostos.OS, path string, wo hostos.WatchOptions) func(context.Context) (pluginapi.Completion, bool) {
	var w hostos.Watch
	started := false
	end := func() (pluginapi.Completion, bool) {
		if w != nil {
			w.Stop()
		}
		return pluginapi.Completion{Kind: "end"}, true
	}
	return func(ctx context.Context) (pluginapi.Completion, bool) {
		if !started {
			started = true
			var err error
			if w, err = o.Watch(ctx, path, wo); err != nil {
				return pluginapi.Completion{Kind: "error", Err: errString(err)}, true
			}
		}
		if ctx.Err() != nil {
			return end()
		}
		ev, ok := w.Next(ctx)
		if !ok {
			return end()
		}
		m := map[string]any{"path": ev.Path, "kind": ev.Kind}
		if ev.Err != "" {
			m["err"] = ev.Err
		}
		return pluginapi.Completion{Kind: "event", More: true, Values: []any{m}}, true
	}
}
