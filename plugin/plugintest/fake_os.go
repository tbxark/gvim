package plugintest

import (
	"context"
	"io/fs"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/tbxark/gvim/plugin/hostos"
	"github.com/tbxark/gvim/pluginapi"
)

// FakeOS is a hostos.OS for tests: an in-memory filesystem, scripted processes and
// scripted watchers. It is safe for concurrent use, like every OS. A process or watcher
// that would block waits on the fake's own wake-up channel; BlockedWaiters tells the
// harness how many goroutines are parked there, so it can tell "idle" from "still
// producing" without sleeping.
type FakeOS struct {
	mu sync.Mutex

	// Dir is what Dirs returns; Env is what Getenv reads; Bin maps program names to the
	// path LookPath finds.
	Dir hostos.Dirs
	Env map[string]string
	Bin map[string]string

	nodes   map[string]*fakeNode
	clock   int64
	scripts []procScript
	runs    []Run
	procs   []*FakeProc
	watches []*FakeWatch
	calls   []string
	ui      uint64 // the goroutine that created the fake: the ui goroutine of a test
	uiCalls int    // IO calls made on it

	failWatch map[string]error
	shutdown  bool

	wake    chan struct{} // closed and replaced on every state change a waiter may care about
	blocked int           // goroutines parked on wake
}

type fakeNode struct {
	dir   bool
	data  []byte
	mode  fs.FileMode
	mtime time.Time
}

// Run is one recorded Run or Start call.
type Run struct {
	Argv    []string
	Opts    hostos.ProcOptions
	Started bool // Start rather than Run
}

// ProcScript scripts the processes whose argv begins with a prefix.
type ProcScript struct {
	Stdout, Stderr string // Run: the result; Start: delivered as output unless Chunks is set
	Chunks         []hostos.ProcChunk
	Code           int
	Signal         string
	TimedOut       bool
	Truncated      bool
	Err            error // the process cannot start
	// Hold keeps a started process running until Exit, Kill or Terminate.
	Hold bool
	// Echo makes a started process drain its stdin and send it back as stdout. Any other
	// process never reads stdin: writes queue up to the 8 MiB bound, then fail with busy.
	Echo bool
}

type procScript struct {
	prefix []string
	s      ProcScript
}

var _ hostos.OS = (*FakeOS)(nil)

// NewFakeOS returns a fake with the directories /home/u (home), /home/u/.config/gvim,
// /tmp and /work (cwd and root) present.
func NewFakeOS() *FakeOS {
	f := &FakeOS{
		Dir:       hostos.Dirs{Config: "/home/u/.config/gvim", Home: "/home/u", Temp: "/tmp", Cwd: "/work", Root: "/work"},
		Env:       map[string]string{},
		Bin:       map[string]string{},
		nodes:     map[string]*fakeNode{},
		failWatch: map[string]error{},
		wake:      make(chan struct{}),
		ui:        goid(),
	}
	for _, d := range []string{"/home/u", "/home/u/.config/gvim", "/tmp", "/work"} {
		f.mkdirAll(d)
	}
	return f
}

// ---------- scripting and inspection ----------

// SetFile creates or replaces a file (and its parent directories).
func (f *FakeOS) SetFile(path, content string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	p := f.abs(path)
	f.mkdirAll(filepath.Dir(p))
	f.nodes[p] = &fakeNode{data: []byte(content), mode: 0o644, mtime: f.tick()}
}

// SetDir creates a directory (and its parents).
func (f *FakeOS) SetDir(path string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.mkdirAll(f.abs(path))
}

// File returns the content of a file.
func (f *FakeOS) File(path string) (string, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := f.nodes[f.abs(path)]
	if n == nil || n.dir {
		return "", false
	}
	return string(n.data), true
}

// Exists reports whether path exists.
func (f *FakeOS) Exists(path string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.nodes[f.abs(path)] != nil
}

// Script makes processes whose argv starts with prefix behave as s. The first script
// that matches wins; an argv no script matches fails to start with CodeExec.
func (f *FakeOS) Script(prefix []string, s ProcScript) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.scripts = append(f.scripts, procScript{prefix: prefix, s: s})
}

// Rescript changes what the processes whose argv starts with exactly prefix do from now on:
// it replaces the script registered with that prefix in place (keeping its rank), or adds
// one when there is none. A test uses it to change an answer in the middle of a run.
func (f *FakeOS) Rescript(prefix []string, s ProcScript) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := range f.scripts {
		if slices.Equal(f.scripts[i].prefix, prefix) {
			f.scripts[i].s = s
			return
		}
	}
	f.scripts = append(f.scripts, procScript{prefix: prefix, s: s})
}

// Runs returns the recorded Run and Start calls.
func (f *FakeOS) Runs() []Run {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]Run(nil), f.runs...)
}

// Procs returns the started processes in start order.
func (f *FakeOS) Procs() []*FakeProc {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]*FakeProc(nil), f.procs...)
}

// Calls returns the file system calls made, as "Name path" strings.
func (f *FakeOS) Calls() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.calls...)
}

// FailWatch makes Watch of path fail with err.
func (f *FakeOS) FailWatch(path string, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.failWatch[f.abs(path)] = err
}

// Watches returns the watchers started so far.
func (f *FakeOS) Watches() []*FakeWatch {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]*FakeWatch(nil), f.watches...)
}

// Emit delivers a change to every live watcher of path (or of its parent directory).
func (f *FakeOS) Emit(path, kind string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	p := f.abs(path)
	for _, w := range f.watches {
		if !w.stopped && (w.path == p || w.path == filepath.Dir(p)) {
			w.queue = append(w.queue, hostos.WatchEvent{Path: p, Kind: kind})
		}
	}
	f.broadcast()
}

// ShutdownCalled reports whether Shutdown ran.
func (f *FakeOS) ShutdownCalled() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.shutdown
}

// BlockedWaiters is the number of goroutines parked in Output, Wait or Next.
func (f *FakeOS) BlockedWaiters() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.blocked
}

// ---------- blocking ----------

// broadcast wakes every parked goroutine. Called with mu held.
func (f *FakeOS) broadcast() {
	close(f.wake)
	f.wake = make(chan struct{})
	f.blocked = 0
}

// await parks until ready() holds (checked with mu held) or ctx ends. mu must be held;
// it is held again on return. It reports whether ready() holds.
func (f *FakeOS) await(ctx context.Context, ready func() bool) bool {
	for !ready() {
		if ctx.Err() != nil {
			return false
		}
		ch := f.wake
		f.blocked++
		f.mu.Unlock()
		select {
		case <-ch:
			f.mu.Lock() // broadcast reset the count
		case <-ctx.Done():
			f.mu.Lock()
			if f.wake == ch { // not woken by a broadcast: still counted
				f.blocked--
			}
		}
	}
	return true
}

// ---------- files ----------

func (f *FakeOS) tick() time.Time {
	f.clock++
	return time.Unix(1_700_000_000+f.clock, 0)
}

func (f *FakeOS) abs(p string) string {
	if !filepath.IsAbs(p) {
		p = filepath.Join(f.Dir.Cwd, p)
	}
	return filepath.Clean(p)
}

func (f *FakeOS) mkdirAll(p string) {
	for ; ; p = filepath.Dir(p) {
		if n := f.nodes[p]; n != nil {
			return
		}
		f.nodes[p] = &fakeNode{dir: true, mode: 0o755, mtime: f.tick()}
		if filepath.Dir(p) == p {
			return
		}
	}
}

func notFound(p string) error {
	return pluginapi.Errf(pluginapi.CodeNotFound, "%s: no such file or directory", p)
}

func (f *FakeOS) rec(name, path string) {
	f.calls = append(f.calls, name+" "+path)
	if goid() == f.ui {
		f.uiCalls++
	}
}

// UICalls is the number of IO calls (files, processes, watchers) made on the goroutine that
// created the fake. The runtime must make none: IO belongs to Work goroutines.
func (f *FakeOS) UICalls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.uiCalls
}

// ReadFile implements hostos.OS.
func (f *FakeOS) ReadFile(_ context.Context, path string, max int64) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	p := f.abs(path)
	f.rec("ReadFile", p)
	n := f.nodes[p]
	switch {
	case n == nil:
		return nil, notFound(p)
	case n.dir:
		return nil, pluginapi.Errf(pluginapi.CodeIO, "%s: is a directory", p)
	case int64(len(n.data)) > max:
		return nil, pluginapi.Errf(pluginapi.CodeTooLarge, "%s: larger than %d bytes", p, max)
	}
	return append([]byte(nil), n.data...), nil
}

// WriteFile implements hostos.OS.
func (f *FakeOS) WriteFile(_ context.Context, path string, data []byte, o hostos.WriteOptions) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	p := f.abs(path)
	f.rec("WriteFile", p)
	if parent := f.nodes[filepath.Dir(p)]; parent == nil {
		if !o.Parents {
			return notFound(filepath.Dir(p))
		}
		f.mkdirAll(filepath.Dir(p))
	}
	old := f.nodes[p]
	if old != nil && old.dir {
		return pluginapi.Errf(pluginapi.CodeIO, "%s: is a directory", p)
	}
	mode := o.Mode
	if mode == 0 {
		mode = 0o644
		if old != nil {
			mode = old.mode
		}
	}
	buf := append([]byte(nil), data...)
	if o.Append && old != nil {
		buf = append(append([]byte(nil), old.data...), data...)
	}
	f.nodes[p] = &fakeNode{data: buf, mode: mode, mtime: f.tick()}
	return nil
}

func kindOf(n *fakeNode) string {
	if n.dir {
		return "dir"
	}
	return "file"
}

// ReadDir implements hostos.OS.
func (f *FakeOS) ReadDir(_ context.Context, path string, hidden bool) ([]hostos.DirEntry, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	p := f.abs(path)
	f.rec("ReadDir", p)
	if n := f.nodes[p]; n == nil {
		return nil, notFound(p)
	} else if !n.dir {
		return nil, pluginapi.Errf(pluginapi.CodeIO, "%s: not a directory", p)
	}
	var out []hostos.DirEntry
	for q, n := range f.nodes {
		name := filepath.Base(q)
		if q == p || filepath.Dir(q) != p || (!hidden && strings.HasPrefix(name, ".")) {
			continue
		}
		out = append(out, hostos.DirEntry{Name: name, Kind: kindOf(n), Size: int64(len(n.data)), MTime: n.mtime})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// Stat implements hostos.OS.
func (f *FakeOS) Stat(_ context.Context, path string) (hostos.FileStat, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	p := f.abs(path)
	f.rec("Stat", p)
	n := f.nodes[p]
	if n == nil {
		return hostos.FileStat{}, notFound(p)
	}
	return hostos.FileStat{Kind: kindOf(n), Size: int64(len(n.data)), MTime: n.mtime, Mode: n.mode}, nil
}

// Mkdir implements hostos.OS.
func (f *FakeOS) Mkdir(_ context.Context, path string, parents bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	p := f.abs(path)
	f.rec("Mkdir", p)
	if n := f.nodes[p]; n != nil {
		if n.dir && parents {
			return nil
		}
		return pluginapi.Errf(pluginapi.CodeConflict, "%s: already exists", p)
	}
	if f.nodes[filepath.Dir(p)] == nil && !parents {
		return notFound(filepath.Dir(p))
	}
	f.mkdirAll(p)
	return nil
}

func (f *FakeOS) below(p string) []string {
	var out []string
	for q := range f.nodes {
		if strings.HasPrefix(q, p+string(filepath.Separator)) {
			out = append(out, q)
		}
	}
	sort.Strings(out)
	return out
}

// Remove implements hostos.OS.
func (f *FakeOS) Remove(_ context.Context, path string, recursive bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	p := f.abs(path)
	f.rec("Remove", p)
	n := f.nodes[p]
	if n == nil {
		return notFound(p)
	}
	kids := f.below(p)
	if n.dir && len(kids) > 0 && !recursive {
		return pluginapi.Errf(pluginapi.CodeIO, "%s: directory not empty", p)
	}
	for _, q := range kids {
		delete(f.nodes, q)
	}
	delete(f.nodes, p)
	return nil
}

// Rename implements hostos.OS.
func (f *FakeOS) Rename(_ context.Context, from, to string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	a, b := f.abs(from), f.abs(to)
	f.rec("Rename", a+" "+b)
	if f.nodes[a] == nil {
		return notFound(a)
	}
	if f.nodes[filepath.Dir(b)] == nil {
		return notFound(filepath.Dir(b))
	}
	moves := append([]string{a}, f.below(a)...)
	for _, q := range moves {
		f.nodes[b+strings.TrimPrefix(q, a)] = f.nodes[q]
		delete(f.nodes, q)
	}
	return nil
}

// Copy implements hostos.OS.
func (f *FakeOS) Copy(_ context.Context, from, to string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	a, b := f.abs(from), f.abs(to)
	f.rec("Copy", a+" "+b)
	if f.nodes[a] == nil {
		return notFound(a)
	}
	if f.nodes[filepath.Dir(b)] == nil {
		return notFound(filepath.Dir(b))
	}
	for _, q := range append([]string{a}, f.below(a)...) {
		n := *f.nodes[q]
		n.data = append([]byte(nil), n.data...)
		f.nodes[b+strings.TrimPrefix(q, a)] = &n
	}
	return nil
}

// RealPath implements hostos.OS.
func (f *FakeOS) RealPath(_ context.Context, path string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	p := f.abs(path)
	f.rec("RealPath", p)
	if f.nodes[p] == nil {
		return "", notFound(p)
	}
	return p, nil
}

// Walk implements hostos.OS.
func (f *FakeOS) Walk(_ context.Context, root string, o hostos.WalkOptions) (hostos.WalkResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	r := f.abs(root)
	f.rec("Walk", r)
	if n := f.nodes[r]; n == nil {
		return hostos.WalkResult{}, notFound(r)
	} else if !n.dir {
		return hostos.WalkResult{}, pluginapi.Errf(pluginapi.CodeIO, "%s: not a directory", r)
	}
	limit := o.MaxEntries
	if limit <= 0 {
		limit = 50_000
	}
	skip := map[string]bool{}
	for _, s := range o.SkipDirs {
		skip[s] = true
	}
	var res hostos.WalkResult
	for _, q := range f.below(r) {
		rel := strings.TrimPrefix(q, r+string(filepath.Separator))
		parts := strings.Split(rel, string(filepath.Separator))
		hide := false
		for i, part := range parts {
			if (!o.Hidden && strings.HasPrefix(part, ".")) || (skip[part] && (i < len(parts)-1 || f.nodes[q].dir)) {
				hide = true
			}
		}
		if hide || (o.MaxDepth > 0 && len(parts) > o.MaxDepth) {
			continue
		}
		if len(res.Entries) >= limit {
			res.Truncated = true
			break
		}
		n := f.nodes[q]
		res.Entries = append(res.Entries, hostos.WalkEntry{Path: q, Rel: rel, Kind: kindOf(n), Size: int64(len(n.data)), MTime: n.mtime})
	}
	return res, nil
}

// ---------- environment ----------

// LookPath implements hostos.OS.
func (f *FakeOS) LookPath(name string) (string, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	p, ok := f.Bin[name]
	return p, ok
}

// Getenv implements hostos.OS.
func (f *FakeOS) Getenv(name string) (string, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	v, ok := f.Env[name]
	return v, ok
}

// Dirs implements hostos.OS.
func (f *FakeOS) Dirs() hostos.Dirs {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.Dir
}

// Now implements hostos.OS.
func (f *FakeOS) Now() time.Time { return time.Unix(1_700_000_000, 0) }

// Shutdown implements hostos.OS: it kills every process and stops every watcher.
func (f *FakeOS) Shutdown() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.shutdown = true
	for _, p := range f.procs {
		p.exit(hostos.ProcResult{Code: -1, Signal: "KILL"})
	}
	for _, w := range f.watches {
		w.stopped = true
	}
	f.broadcast()
}

// ---------- processes ----------

func (f *FakeOS) script(argv []string) (ProcScript, bool) {
	for _, s := range f.scripts {
		if len(argv) >= len(s.prefix) {
			match := true
			for i, a := range s.prefix {
				if argv[i] != a {
					match = false
					break
				}
			}
			if match {
				return s.s, true
			}
		}
	}
	return ProcScript{}, false
}

func noScript(argv []string) error {
	return pluginapi.Errf(pluginapi.CodeExec, "%s: executable file not found", argv[0])
}

// Run implements hostos.OS.
func (f *FakeOS) Run(ctx context.Context, argv []string, o hostos.ProcOptions) (hostos.ProcResult, error) {
	f.mu.Lock()
	f.runs = append(f.runs, Run{Argv: append([]string(nil), argv...), Opts: o})
	f.rec("Run", argv[0])
	s, ok := f.script(argv)
	f.mu.Unlock()
	if !ok {
		return hostos.ProcResult{}, noScript(argv)
	}
	if s.Err != nil {
		return hostos.ProcResult{}, s.Err
	}
	if ctx.Err() != nil {
		return hostos.ProcResult{}, pluginapi.Errf(pluginapi.CodeCancelled, "cancelled")
	}
	r := hostos.ProcResult{Code: s.Code, Signal: s.Signal, TimedOut: s.TimedOut, Truncated: s.Truncated,
		Stdout: []byte(s.Stdout), Stderr: []byte(s.Stderr)}
	if o.MergeStderr {
		r.Stdout, r.Stderr = append(r.Stdout, r.Stderr...), nil
	}
	return r, nil
}

// Start implements hostos.OS.
func (f *FakeOS) Start(_ context.Context, argv []string, o hostos.ProcOptions) (hostos.Proc, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.runs = append(f.runs, Run{Argv: append([]string(nil), argv...), Opts: o, Started: true})
	f.rec("Start", argv[0])
	s, ok := f.script(argv)
	if !ok {
		return nil, noScript(argv)
	}
	if s.Err != nil {
		return nil, s.Err
	}
	p := &FakeProc{f: f, pid: 1000 + len(f.procs), script: s, stdinOpen: o.OpenStdin}
	p.chunks = s.Chunks
	if p.chunks == nil {
		if s.Stdout != "" {
			p.chunks = append(p.chunks, hostos.ProcChunk{Data: []byte(s.Stdout)})
		}
		if s.Stderr != "" {
			p.chunks = append(p.chunks, hostos.ProcChunk{Stderr: true, Data: []byte(s.Stderr)})
		}
	}
	if !s.Hold && !s.Echo {
		p.done, p.res = true, hostos.ProcResult{Code: s.Code, Signal: s.Signal, TimedOut: s.TimedOut}
	}
	f.procs = append(f.procs, p)
	return p, nil
}

// FakeProc is a scripted running process.
type FakeProc struct {
	f      *FakeOS
	pid    int
	script ProcScript

	chunks    []hostos.ProcChunk
	done      bool
	res       hostos.ProcResult
	stdinOpen bool
	stdin     []byte // bytes written and not drained
	written   []byte // everything written
	closed    bool   // CloseStdin was called
	signals   []string
	terminate time.Duration
}

var _ hostos.Proc = (*FakeProc)(nil)

// PID implements hostos.Proc.
func (p *FakeProc) PID() int { return p.pid }

// Written is everything written to stdin so far.
func (p *FakeProc) Written() []byte {
	p.f.mu.Lock()
	defer p.f.mu.Unlock()
	return append([]byte(nil), p.written...)
}

// Signals are the signals sent with Kill, in order.
func (p *FakeProc) Signals() []string {
	p.f.mu.Lock()
	defer p.f.mu.Unlock()
	return append([]string(nil), p.signals...)
}

// StdinClosed reports whether CloseStdin was called.
func (p *FakeProc) StdinClosed() bool {
	p.f.mu.Lock()
	defer p.f.mu.Unlock()
	return p.closed
}

// Terminated is the grace of the last Terminate call (0: never called).
func (p *FakeProc) Terminated() time.Duration {
	p.f.mu.Lock()
	defer p.f.mu.Unlock()
	return p.terminate
}

// exit ends the process with r unless it ended already. mu must be held.
func (p *FakeProc) exit(r hostos.ProcResult) {
	if !p.done {
		p.done, p.res = true, r
		p.f.broadcast()
	}
}

// Exit ends a held process with the given code.
func (p *FakeProc) Exit(code int) {
	p.f.mu.Lock()
	defer p.f.mu.Unlock()
	p.exit(hostos.ProcResult{Code: code})
}

// Write implements hostos.Proc: a bounded queue (8 MiB) that only an Echo script drains.
func (p *FakeProc) Write(data []byte) error {
	p.f.mu.Lock()
	defer p.f.mu.Unlock()
	switch {
	case p.done:
		return pluginapi.Errf(pluginapi.CodeClosed, "process has exited")
	case !p.stdinOpen || p.closed:
		return pluginapi.Errf(pluginapi.CodeClosed, "stdin is closed")
	case len(p.stdin)+len(data) > 8<<20:
		return pluginapi.Errf(pluginapi.CodeBusy, "stdin buffer full")
	}
	p.written = append(p.written, data...)
	if p.script.Echo {
		p.chunks = append(p.chunks, hostos.ProcChunk{Data: append([]byte(nil), data...)})
		p.f.broadcast()
	} else {
		p.stdin = append(p.stdin, data...)
	}
	return nil
}

// CloseStdin implements hostos.Proc. An Echo process ends when its stdin is closed.
func (p *FakeProc) CloseStdin() {
	p.f.mu.Lock()
	defer p.f.mu.Unlock()
	p.closed = true
	if p.script.Echo {
		p.exit(hostos.ProcResult{Code: p.script.Code})
	}
}

// Kill implements hostos.Proc.
func (p *FakeProc) Kill(signal string) error {
	p.f.mu.Lock()
	defer p.f.mu.Unlock()
	if p.done {
		return pluginapi.Errf(pluginapi.CodeClosed, "process has exited")
	}
	if signal == "" {
		signal = "TERM"
	}
	p.signals = append(p.signals, signal)
	p.exit(hostos.ProcResult{Code: -1, Signal: signal})
	return nil
}

// Terminate implements hostos.Proc.
func (p *FakeProc) Terminate(grace time.Duration) {
	p.f.mu.Lock()
	defer p.f.mu.Unlock()
	p.terminate = grace
	p.exit(hostos.ProcResult{Code: -1, Signal: "TERM"})
}

// Output implements hostos.Proc.
func (p *FakeProc) Output(ctx context.Context) (hostos.ProcChunk, bool) {
	p.f.mu.Lock()
	defer p.f.mu.Unlock()
	if !p.f.await(ctx, func() bool { return len(p.chunks) > 0 || p.done }) {
		return hostos.ProcChunk{}, false
	}
	if len(p.chunks) == 0 {
		return hostos.ProcChunk{}, false
	}
	c := p.chunks[0]
	p.chunks = p.chunks[1:]
	return c, true
}

// Wait implements hostos.Proc.
func (p *FakeProc) Wait(ctx context.Context) (hostos.ProcResult, error) {
	p.f.mu.Lock()
	defer p.f.mu.Unlock()
	if !p.f.await(ctx, func() bool { return p.done }) {
		return hostos.ProcResult{}, pluginapi.Errf(pluginapi.CodeCancelled, "cancelled")
	}
	return p.res, nil
}

// ---------- watchers ----------

// FakeWatch is a running scripted watcher.
type FakeWatch struct {
	f       *FakeOS
	path    string
	opts    hostos.WatchOptions
	queue   []hostos.WatchEvent
	stopped bool
}

var _ hostos.Watch = (*FakeWatch)(nil)

// Path is the absolute path being watched.
func (w *FakeWatch) Path() string { return w.path }

// Options are the options the watch was started with.
func (w *FakeWatch) Options() hostos.WatchOptions { return w.opts }

// Stopped reports whether Stop was called.
func (w *FakeWatch) Stopped() bool {
	w.f.mu.Lock()
	defer w.f.mu.Unlock()
	return w.stopped
}

// Watch implements hostos.OS.
func (f *FakeOS) Watch(_ context.Context, path string, o hostos.WatchOptions) (hostos.Watch, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	p := f.abs(path)
	f.rec("Watch", p)
	if err := f.failWatch[p]; err != nil {
		return nil, err
	}
	if f.nodes[p] == nil {
		return nil, notFound(p)
	}
	w := &FakeWatch{f: f, path: p, opts: o}
	f.watches = append(f.watches, w)
	return w, nil
}

// Next implements hostos.Watch.
func (w *FakeWatch) Next(ctx context.Context) (hostos.WatchEvent, bool) {
	w.f.mu.Lock()
	defer w.f.mu.Unlock()
	if !w.f.await(ctx, func() bool { return len(w.queue) > 0 || w.stopped }) || (len(w.queue) == 0) {
		return hostos.WatchEvent{}, false
	}
	e := w.queue[0]
	w.queue = w.queue[1:]
	return e, true
}

// Stop implements hostos.Watch.
func (w *FakeWatch) Stop() {
	w.f.mu.Lock()
	defer w.f.mu.Unlock()
	w.stopped = true
	w.f.broadcast()
}
