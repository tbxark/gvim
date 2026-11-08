package plugin_test

import (
	"io/fs"
	"sync/atomic"
	"testing"
	"testing/fstest"
	"time"
)

// pipeFS makes Open of a named pipe block like the real open(2) of a FIFO without a writer.
// A real loader must check the file type with Stat first; blocked opens are released after a
// second so that a failing test reports instead of hanging.
type pipeFS struct {
	fs.FS
	opened *atomic.Bool
	gate   <-chan time.Time
}

func (p pipeFS) Stat(name string) (fs.FileInfo, error) { return fs.Stat(p.FS, name) }

func (p pipeFS) Open(name string) (fs.File, error) {
	if fi, err := fs.Stat(p.FS, name); err == nil && fi.Mode()&fs.ModeNamedPipe != 0 {
		p.opened.Store(true)
		<-p.gate
	}
	return p.FS.Open(name)
}

func TestDiscoveryNeverOpensANamedPipe(t *testing.T) {
	tests := []struct {
		name string
		pipe string // path under the fake disk
	}{
		{name: "single file plugin", pipe: userDir + "/pipe.lua"},
		{name: "entry of a directory plugin", pipe: userDir + "/dir/init.lua"},
		{name: "manifest of a directory plugin", pipe: userDir + "/dir/plugin.json"},
		{name: "module of a directory plugin", pipe: userDir + "/dir/lua/m.lua"},
		{name: "init.lua", pipe: initFile},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newEnv(t)
			e.bundle("b_one", map[string]string{"init.lua": marker("b_one")})
			e.user("ok.lua", marker("ok"))
			e.user("dir/init.lua", marker("dir"))
			e.disk.MapFS[tt.pipe[1:]] = &fstest.MapFile{Mode: fs.ModeNamedPipe}
			var opened atomic.Bool
			gate := time.After(time.Second)
			open := e.opts.Open
			e.opts.Open = func(dir string) (fs.FS, func(), error) {
				fsys, closeFn, err := open(dir)
				if err != nil {
					return nil, nil, err
				}
				return pipeFS{FS: fsys, opened: &opened, gate: gate}, closeFn, nil
			}
			e.start()
			if opened.Load() {
				t.Fatalf("the loader opened the named pipe %s", tt.pipe)
			}
			for _, name := range []string{"b_one", "ok"} {
				if p := e.info(name); p.Status != "loaded" {
					t.Errorf("%s = %+v, want loaded", name, p)
				}
			}
		})
	}
}
