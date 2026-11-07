package plugin_test

import (
	"context"
	"errors"
	"io/fs"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/tbxark/gvim/internal/plugin"
	"github.com/tbxark/gvim/internal/plugin/plugintest"
	"github.com/tbxark/gvim/internal/pluginapi"
)

// disk is a fake file system for the loader: one MapFS holds every path without its leading
// slash ("cfg/plugins/hello.lua" is /cfg/plugins/hello.lua).
type disk struct{ fstest.MapFS }

func (d disk) put(path, src string) {
	d.MapFS[strings.TrimPrefix(path, "/")] = &fstest.MapFile{Data: []byte(src)}
}

func (d disk) remove(path string) { delete(d.MapFS, strings.TrimPrefix(path, "/")) }

// open is plugin.OpenFunc over the MapFS.
func (d disk) open(dir string) (fs.FS, func(), error) {
	rel := strings.TrimPrefix(filepath.ToSlash(filepath.Clean(dir)), "/")
	if rel == "" {
		rel = "."
	}
	fi, err := fs.Stat(d.MapFS, rel)
	if err != nil {
		return nil, nil, err
	}
	if !fi.IsDir() {
		return nil, nil, errors.New("not a directory")
	}
	sub, err := fs.Sub(d.MapFS, rel)
	return sub, func() {}, err
}

// memOS is an OS that keeps files in memory (plugin-state.json) and counts writes.
type memOS struct {
	plugin.NopOS
	mu       sync.Mutex
	files    map[string][]byte
	writes   int
	inflight int
	maxIn    int
	delay    time.Duration
	failNext error
	config   string // Dirs().Config: where plugin-data/<name>/state.json lives ("" none)
}

func (m *memOS) Dirs() plugin.Dirs { return plugin.Dirs{Config: m.config} }

func newMemOS() *memOS { return &memOS{files: map[string][]byte{}} }

func (m *memOS) ReadFile(_ context.Context, path string, max int64) ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	data, ok := m.files[path]
	if !ok {
		return nil, pluginapi.Errf(pluginapi.CodeNotFound, "%s does not exist", path)
	}
	if max > 0 && int64(len(data)) > max {
		return nil, pluginapi.Errf(pluginapi.CodeTooLarge, "%s is larger than %d bytes", path, max)
	}
	return append([]byte(nil), data...), nil
}

func (m *memOS) WriteFile(_ context.Context, path string, data []byte, o plugin.WriteOptions) error {
	m.mu.Lock()
	m.inflight++
	m.maxIn = max(m.maxIn, m.inflight)
	delay, fail := m.delay, m.failNext
	m.failNext = nil
	m.mu.Unlock()
	time.Sleep(delay)
	m.mu.Lock()
	defer m.mu.Unlock()
	m.inflight--
	if fail != nil {
		return fail
	}
	if !o.Atomic || !o.Parents {
		return errors.New("plugin-state.json must be written atomically with parents")
	}
	m.writes++
	m.files[path] = append([]byte(nil), data...)
	return nil
}

func (m *memOS) file(path string) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return string(m.files[path])
}

func (m *memOS) writeCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.writes
}

const (
	userDir   = "/cfg/plugins"
	initFile  = "/cfg/init.lua"
	stateFile = "/cfg/plugin-state.json"
)

// env is a loader test fixture: the fake disk, the bundled tree, the OS and the options.
type env struct {
	t       *testing.T
	disk    disk
	bundled fstest.MapFS
	os      *memOS
	opts    plugin.LoaderOptions
	h       *plugintest.Harness
}

func newEnv(t *testing.T) *env {
	t.Helper()
	e := &env{t: t, disk: disk{fstest.MapFS{}}, bundled: fstest.MapFS{}, os: newMemOS()}
	e.opts = plugin.LoaderOptions{Bundled: e.bundled, UserDir: userDir, InitFile: initFile, StateFile: stateFile, Open: e.disk.open}
	return e
}

// bundle adds a bundled plugin: dir files as name -> source.
func (e *env) bundle(name string, files map[string]string) {
	for rel, src := range files {
		e.bundled["plugins/"+name+"/"+rel] = &fstest.MapFile{Data: []byte(src)}
	}
}

// user adds a user plugin: a single file when files has one key ending in .lua at the top
// level and name is "x.lua", otherwise a directory.
func (e *env) user(path, src string) { e.disk.put(userDir+"/"+path, src) }

// build creates the harness and the runtime with the loader; nothing is started.
func (e *env) build(o ...plugintest.Options) *plugintest.Harness {
	e.t.Helper()
	var opts plugintest.Options
	if len(o) > 0 {
		opts = o[0]
	}
	if opts.Timeout == 0 {
		opts.Timeout = 100 * time.Millisecond
	}
	opts.OS = e.os
	h := plugintest.New(e.t, nil, opts)
	h.RT = plugin.New(plugin.Options{
		Timeout: opts.Timeout, OS: e.os, Services: &plugintest.FakeServices{}, Version: "test", Platform: "test",
		CheckGoroutine: true, Now: h.Now, Loader: &e.opts,
	})
	e.t.Cleanup(h.RT.Stop)
	e.h = h
	return h
}

// start builds the runtime, starts it and settles.
func (e *env) start(o ...plugintest.Options) *plugintest.Harness {
	e.t.Helper()
	h := e.build(o...)
	h.Start()
	h.Settle()
	return h
}

// spyLines is the spy as a slice.
func (e *env) spyLines() []string { return plugin.Spy(e.h.RT) }

func (e *env) names() []string {
	var out []string
	for _, p := range e.h.RT.Plugins() {
		out = append(out, p.Name+":"+p.Status)
	}
	return out
}

func (e *env) info(name string) pluginapi.PluginInfo {
	e.t.Helper()
	p, ok := e.h.Plugin(name)
	if !ok {
		e.t.Fatalf("plugin %q is not listed: %v", name, e.names())
	}
	return p
}

func (e *env) notices() []string { return e.h.NoticeTexts() }

func (e *env) noticeHas(sub string) bool {
	for _, n := range e.notices() {
		if strings.Contains(n, sub) {
			return true
		}
	}
	return false
}

// marker is a plugin source that records its own load in the spy.
func marker(name string) string { return `T.spy("load ` + name + `")` }
