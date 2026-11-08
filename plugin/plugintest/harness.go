package plugintest

import (
	"bytes"
	"context"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/tbxark/gvim/plugin"
	"github.com/tbxark/gvim/plugin/hostos"
	"github.com/tbxark/gvim/pluginapi"
)

// Files are the plugin sources of a harness, keyed by path. "hello.lua" is the single-file
// plugin hello; "git_blame/init.lua" and "git_blame/lua/util.lua" are files of the directory
// plugin git_blame (entry init.lua).
type Files map[string]string

// Options configure a Harness.
type Options struct {
	Timeout time.Duration // plugin_timeout_ms (0: 500 ms)
	// EventBudget raises the 50 ms cap of event and timer slices (0: the spec's 50 ms), for
	// tests that run real Lua and must not time out on a busy machine.
	EventBudget time.Duration
	// HeapLimit is the heap guard (0: 4 GiB). The guard measures the whole process, and the
	// tests of a package share one while they run in parallel, so the limit of the editor
	// would cancel an innocent plugin because of the allocations of another test. The tests
	// of the guard itself set their own limit.
	HeapLimit int64
	Services  pluginapi.Services // nil: a *FakeServices
	OS        hostos.OS          // nil: a fresh *FakeOS
	Host      *FakeHost          // nil: a fresh one
	Now       func() time.Time   // clock of the runtime (nil: the harness's manual clock)
	Locale    string             // gvim.i18n locale ("" means "en")
	// Settings are the plugins.<name>.settings layers of the config, per plugin name.
	Settings map[string]plugin.PluginSettings
}

// Harness plays the ui for a runtime: it starts it, calls Run/Emit/Resolve, pumps Drain
// until nothing is left, runs the Work on goroutines like a tea.Cmd would and answers timers
// from a manual clock.
type Harness struct {
	T    *testing.T
	RT   *plugin.Runtime
	Host *FakeHost
	// OS is the FakeOS of the runtime (nil when Options.OS was another implementation).
	OS       *FakeOS
	Files    Files
	Sources  []plugin.Source
	settings map[string]plugin.PluginSettings

	// CompileOnUIGoroutine reports whether compiling a plugin ever ran on the goroutine of
	// the test (the UI goroutine). Compiling is Work: it must be false.
	CompileOnUIGoroutine bool

	now     time.Time
	timers  []timer
	gid     uint64
	streams []*stream
	active  atomic.Int32 // stream goroutines that have not finished (see startStream)
}

type timer struct {
	due time.Time
	tok pluginapi.Token
}

// New builds a harness and the runtime; nothing is loaded until Start.
func New(t *testing.T, files Files, o Options) *Harness {
	t.Helper()
	h := &Harness{T: t, Files: files, Host: o.Host, now: time.Unix(1_700_000_000, 0), gid: goid()}
	if h.Host == nil {
		h.Host = NewFakeHost()
	}
	osImpl := o.OS
	if osImpl == nil {
		osImpl = NewFakeOS()
	}
	h.OS, _ = osImpl.(*FakeOS)
	svc := o.Services
	if svc == nil {
		svc = &FakeServices{}
	}
	now := o.Now
	if now == nil {
		now = h.Now
	}
	heap := o.HeapLimit
	if heap == 0 {
		heap = 4 << 30
	}
	h.RT = plugin.New(plugin.Options{
		Timeout: o.Timeout, EventBudget: o.EventBudget, HeapLimit: heap, OS: osImpl, Services: svc,
		Version: "test", Platform: "test", Locale: o.Locale, CheckGoroutine: true, Now: now,
	})
	h.settings = o.Settings
	h.Sources = h.sources()
	t.Cleanup(h.RT.Stop)
	return h
}

// sources groups the files into plugins, in alphabetical order.
func (h *Harness) sources() []plugin.Source {
	type group struct {
		files map[string]string
		dir   bool
		i18n  string
	}
	groups := map[string]*group{}
	for path, src := range h.Files {
		// "<name>.i18n.json" (single-file plugin) and "<dir>/<name>.i18n.json" (directory
		// plugin) are the translation files of the plugin, not Lua sources.
		if strings.HasSuffix(path, ".i18n.json") {
			name, _, isDir := strings.Cut(path, "/")
			if !isDir {
				name = strings.TrimSuffix(path, ".i18n.json")
			}
			g := groups[name]
			if g == nil {
				g = &group{files: map[string]string{}}
				groups[name] = g
			}
			g.i18n = src
			continue
		}
		name, rest, isDir := strings.Cut(path, "/")
		if !isDir {
			name = strings.TrimSuffix(path, ".lua")
			rest = "init.lua"
		}
		g := groups[name]
		if g == nil {
			g = &group{files: map[string]string{}}
			groups[name] = g
		}
		g.dir = g.dir || isDir
		g.files[rest] = src
	}
	var names []string
	for n := range groups {
		names = append(names, n)
	}
	sort.Strings(names)
	var out []plugin.Source
	for _, n := range names {
		g := groups[n]
		s := plugin.Source{Name: n, Kind: plugin.SourceUser, I18nJSON: g.i18n}
		if st, ok := h.settings[n]; ok {
			s.Settings, s.ProjectSettings = st.User, st.Project
		}
		if g.dir {
			s.Dir = "/plugins/" + n
		}
		// Compile in Work, on another goroutine, like the loader does.
		done := make(chan map[string]*plugin.Compiled)
		go func() {
			if goid() == h.gid {
				h.CompileOnUIGoroutine = true
			}
			done <- plugin.Compile(n, g.files)
		}()
		s.Compiled = <-done
		out = append(out, s)
	}
	return out
}

// Start starts the runtime and loads every plugin, then settles.
func (h *Harness) Start() {
	h.T.Helper()
	h.runWork(h.RT.Start(h.Host))
	for _, s := range h.Sources {
		if err := h.RT.LoadPlugin(s); err != nil {
			h.T.Logf("LoadPlugin(%s): %v", s.Name, err)
		}
		h.Settle()
	}
}

// Load compiles a single-file plugin in Work (another goroutine) and loads it, then settles.
func (h *Harness) Load(name, src string) error {
	h.T.Helper()
	done := make(chan map[string]*plugin.Compiled)
	go func() { done <- plugin.Compile(name, map[string]string{"init.lua": src}) }()
	err := h.RT.LoadPlugin(plugin.Source{Name: name, Kind: plugin.SourceUser, Compiled: <-done})
	h.Settle()
	return err
}

// Unload unloads a plugin completely and settles.
func (h *Harness) Unload(name string) {
	h.T.Helper()
	h.RT.UnloadPlugin(name)
	h.Settle()
}

// Run starts a plugin command and settles. The error is Runtime.Run's.
func (h *Harness) Run(action string, args map[string]any) error {
	h.T.Helper()
	err := h.RT.Run(action, args, pluginapi.SrcAPI)
	h.Settle()
	return err
}

// Emit queues an event and settles.
func (h *Harness) Emit(e pluginapi.Event) {
	h.T.Helper()
	h.RT.Emit(e)
	h.Settle()
}

// Resolve completes a wait and settles.
func (h *Harness) Resolve(c pluginapi.Completion) {
	h.T.Helper()
	h.RT.Resolve(c)
	h.Settle()
}

// Control runs a lifecycle request and settles.
func (h *Harness) Control(c pluginapi.Control) []pluginapi.Notice {
	h.T.Helper()
	work, notices := h.RT.Control(c)
	h.runWork(work)
	h.Settle()
	return notices
}

// Settle pumps the runtime like the ui does after every call: Drain, run the Work it
// returns, resolve it, repeat until nothing is queued.
func (h *Harness) Settle() {
	h.T.Helper()
	for i := 0; i < 10_000; i++ {
		work, more := h.RT.Drain(100 * time.Millisecond)
		ran := h.runWork(work)
		if h.pollStreams() {
			ran = true
		}
		if !more && !ran && len(work) == 0 {
			return
		}
	}
	h.T.Fatal("plugintest: Settle did not reach quiescence")
}

// Advance moves the manual clock forward and resolves the timers that became due, then
// settles. Timers are Work with After set.
func (h *Harness) Advance(d time.Duration) {
	h.T.Helper()
	h.now = h.now.Add(d)
	sort.SliceStable(h.timers, func(i, j int) bool { return h.timers[i].due.Before(h.timers[j].due) })
	for len(h.timers) > 0 && !h.timers[0].due.After(h.now) {
		t := h.timers[0]
		h.timers = h.timers[1:]
		h.RT.Resolve(pluginapi.Completion{Token: t.tok})
		h.Settle()
	}
	h.Settle()
}

// Now is the manual clock.
func (h *Harness) Now() time.Time { return h.now }

// RunWork runs Work like the ui would (see Settle) and reports whether anything ran.
func (h *Harness) RunWork(work []pluginapi.Work) bool { return h.runWork(work) }

// PendingTimers is the number of Work{After} timers that have not fired yet.
func (h *Harness) PendingTimers() int { return len(h.timers) }

// runWork runs Work like tea.Cmds: Run and Stream on their own goroutines, with the result
// resolved here; After on the manual clock. It reports whether anything ran.
func (h *Harness) runWork(work []pluginapi.Work) bool {
	ran := false
	for _, w := range work {
		ctx := w.Ctx
		if ctx == nil {
			ctx = context.Background()
		}
		switch {
		case w.Run != nil:
			ran = true
			ch := make(chan pluginapi.Completion, 1)
			go func() { ch <- w.Run(ctx) }()
			select {
			case c := <-ch:
				h.RT.Resolve(c)
			case <-time.After(10 * time.Second):
				h.T.Fatalf("plugintest: Work for token %d did not finish", w.Token)
			}
		case w.Stream != nil:
			ran = true
			h.startStream(w, ctx)
		case w.After > 0:
			ran = true
			h.timers = append(h.timers, timer{due: h.now.Add(w.After), tok: w.Token})
		}
	}
	return ran
}

// Notices returns the Notify calls of the host.
func (h *Harness) Notices() []Notice { return h.Host.Notices }

// NoticeTexts returns just the messages.
func (h *Harness) NoticeTexts() []string {
	out := make([]string, len(h.Host.Notices))
	for i, n := range h.Host.Notices {
		out[i] = n.Msg
	}
	return out
}

// Log returns the plugin log.
func (h *Harness) Log() []pluginapi.LogEntry { return h.RT.Log() }

// LogText returns the log messages of one plugin ("" for all), one string per entry.
func (h *Harness) LogText(plugin string) []string {
	var out []string
	for _, e := range h.RT.Log() {
		if plugin == "" || e.Plugin == plugin {
			out = append(out, e.Level+": "+e.Msg)
		}
	}
	return out
}

// Plugin returns the manager row of a plugin.
func (h *Harness) Plugin(name string) (pluginapi.PluginInfo, bool) {
	for _, p := range h.RT.Plugins() {
		if p.Name == name {
			return p, true
		}
	}
	return pluginapi.PluginInfo{}, false
}

func goid() uint64 {
	var buf [64]byte
	b := buf[:runtime.Stack(buf[:], false)]
	b = bytes.TrimPrefix(b, []byte("goroutine "))
	if i := bytes.IndexByte(b, ' '); i >= 0 {
		b = b[:i]
	}
	n, _ := strconv.ParseUint(string(b), 10, 64)
	return n
}
