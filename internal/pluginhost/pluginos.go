package pluginhost

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/tbxark/gvim/plugin/hostos"
	"github.com/tbxark/gvim/pluginapi"
)

// pluginOS is the real hostos.OS: files through the os package (os.OpenRoot for every
// traversal, so a symbolic link swapped in mid-walk cannot lead out of the tree), processes
// in their own process groups, fsnotify watchers (pluginos_proc.go, pluginos_watch.go).
//
// Every method is safe for concurrent use (AMENDMENTS C16): the plugin runtime calls the IO
// methods from Work goroutines, so the process and watcher tables sit behind mu, which is
// never held across an IO call.
type pluginOS struct {
	// gate is the workspace trust spawn gate (nil allows); it runs before every
	// process launch, with the child's effective environment for PATH resolution.
	gate SpawnGate
	dirs func() hostos.Dirs // evaluated at every call: the workspace root can change
	// spawnEnv is the active project environment's set-only overlay for child
	// processes (nil when none is wired).
	spawnEnv func() []string

	mu      sync.Mutex
	procs   map[*osProc]struct{}
	watches map[*osWatch]struct{}
	closed  bool
}

var _ hostos.OS = (*pluginOS)(nil)

// newPluginOS returns the OS for the plugin runtime. dirs reports the well-known
// directories; a nil dirs gives the process's own home, temp and working directory.
func newPluginOS(dirs func() hostos.Dirs, spawnEnv ...func() []string) *pluginOS {
	var overlay func() []string
	if len(spawnEnv) > 0 {
		overlay = spawnEnv[0]
	}
	if dirs == nil {
		dirs = func() hostos.Dirs {
			home, _ := os.UserHomeDir()
			cwd, _ := os.Getwd()
			return hostos.Dirs{Home: home, Temp: os.TempDir(), Cwd: cwd, Root: cwd}
		}
	}
	return &pluginOS{dirs: dirs, spawnEnv: overlay, procs: map[*osProc]struct{}{}, watches: map[*osWatch]struct{}{}}
}

const (
	copyTreeMax   = 100_000 // entries of one fs.copy of a directory
	copyChunk     = 1 << 20
	walkCtxEvery  = 256
	killGraceTime = 2 * time.Second
)

// ---------- paths and errors ----------

// abs makes p absolute against the working directory of the plugins and cleans it.
func (o *pluginOS) abs(p string) string {
	if !filepath.IsAbs(p) {
		if cwd := o.dirs().Cwd; cwd != "" {
			p = filepath.Join(cwd, p)
		}
	}
	if a, err := filepath.Abs(p); err == nil {
		p = a
	}
	return filepath.Clean(p)
}

// mapErr turns an error of the os package into the plugin error with the code the Lua
// side documents.
func mapErr(err error, path string) error {
	if err == nil {
		return nil
	}
	var pe *pluginapi.Error
	if errors.As(err, &pe) {
		return err
	}
	msg := err.Error()
	var perr *fs.PathError
	if errors.As(err, &perr) {
		msg = fmt.Sprintf("%s: %v", perr.Path, perr.Err)
	} else if path != "" {
		msg = path + ": " + msg
	}
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return pluginapi.Errf(pluginapi.CodeNotFound, "%s", msg)
	case errors.Is(err, fs.ErrExist):
		return pluginapi.Errf(pluginapi.CodeConflict, "%s", msg)
	case errors.Is(err, context.Canceled):
		return pluginapi.Errf(pluginapi.CodeCancelled, "%s", msg)
	case errors.Is(err, context.DeadlineExceeded):
		return pluginapi.Errf(pluginapi.CodeTimeout, "%s", msg)
	case errors.Is(err, syscall.EMFILE), errors.Is(err, syscall.ENFILE), errors.Is(err, syscall.ENOSPC):
		return pluginapi.Errf(pluginapi.CodeLimit, "%s", msg)
	}
	return pluginapi.Errf(pluginapi.CodeIO, "%s", msg)
}

func ctxErr(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return mapErr(err, "")
	}
	return nil
}

func kindOfMode(m fs.FileMode) string {
	switch {
	case m&fs.ModeSymlink != 0:
		return "symlink"
	case m.IsDir():
		return "dir"
	}
	return "file"
}

// ---------- files ----------

// ReadFile implements hostos.OS.
func (o *pluginOS) ReadFile(ctx context.Context, path string, max int64) ([]byte, error) {
	if err := ctxErr(ctx); err != nil {
		return nil, err
	}
	p := o.abs(path)
	// O_NONBLOCK: opening a FIFO must not wait for a writer (ctx cannot interrupt open).
	f, err := os.OpenFile(p, os.O_RDONLY|openNoBlock, 0)
	if err != nil {
		return nil, mapErr(err, p)
	}
	defer func() { _ = f.Close() }()
	st, err := f.Stat()
	if err != nil {
		return nil, mapErr(err, p)
	}
	if st.IsDir() {
		return nil, pluginapi.Errf(pluginapi.CodeIO, "%s: is a directory", p)
	}
	if !st.Mode().IsRegular() {
		return nil, pluginapi.Errf(pluginapi.CodeInvalid, "%s: not a regular file", p)
	}
	if st.Size() > max {
		return nil, pluginapi.Errf(pluginapi.CodeTooLarge, "%s: larger than %d bytes", p, max)
	}
	data, err := io.ReadAll(io.LimitReader(&ctxReader{ctx: ctx, r: f}, max+1))
	if err != nil {
		return nil, mapErr(err, p)
	}
	if int64(len(data)) > max {
		return nil, pluginapi.Errf(pluginapi.CodeTooLarge, "%s: larger than %d bytes", p, max)
	}
	return data, nil
}

// ctxReader stops a copy when ctx ends.
type ctxReader struct {
	ctx context.Context
	r   io.Reader
}

func (c *ctxReader) Read(b []byte) (int, error) {
	if err := c.ctx.Err(); err != nil {
		return 0, err
	}
	return c.r.Read(b)
}

// WriteFile implements hostos.OS.
func (o *pluginOS) WriteFile(ctx context.Context, path string, data []byte, wo hostos.WriteOptions) error {
	if err := ctxErr(ctx); err != nil {
		return err
	}
	p := o.abs(path)
	if wo.Parents {
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return mapErr(err, p)
		}
	}
	mode := wo.Mode.Perm()
	existing, serr := os.Stat(p)
	if mode == 0 {
		mode = 0o644
		if serr == nil {
			mode = existing.Mode().Perm()
		}
	}
	if serr == nil && existing.IsDir() {
		return pluginapi.Errf(pluginapi.CodeIO, "%s: is a directory", p)
	}
	if wo.Append {
		f, err := os.OpenFile(p, os.O_WRONLY|os.O_CREATE|os.O_APPEND, mode)
		if err != nil {
			return mapErr(err, p)
		}
		_, err = f.Write(data)
		return mapErr(errors.Join(err, f.Close()), p)
	}
	if !wo.Atomic {
		f, err := os.OpenFile(p, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode)
		if err != nil {
			return mapErr(err, p)
		}
		_, err = f.Write(data)
		return mapErr(errors.Join(err, f.Close()), p)
	}
	// Atomic: a temporary file in the same directory, then rename over the target. A
	// symbolic link is written through, not replaced.
	target := p
	if real, err := filepath.EvalSymlinks(p); err == nil {
		target = real
	}
	tmp, err := os.CreateTemp(filepath.Dir(target), ".gvim-*.tmp")
	if err != nil {
		return mapErr(ontarget(err, p), p)
	}
	name := tmp.Name()
	fail := func(err error) error {
		_ = tmp.Close()
		_ = os.Remove(name)
		return mapErr(ontarget(err, p), p)
	}
	if _, err := tmp.Write(data); err != nil {
		return fail(err)
	}
	if err := tmp.Chmod(mode); err != nil && goruntime.GOOS != "windows" {
		return fail(err)
	}
	if err := tmp.Sync(); err != nil {
		return fail(err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(name)
		return mapErr(ontarget(err, p), p)
	}
	if err := os.Rename(name, target); err != nil {
		_ = os.Remove(name)
		return mapErr(ontarget(err, p), p)
	}
	return nil
}

// ontarget rewrites an error of the temporary file of an atomic write so that it names the
// file the plugin asked for, which is what the plugin knows: ".gvim-123.tmp" is not its file.
func ontarget(err error, target string) error {
	var perr *fs.PathError
	var lerr *os.LinkError
	switch {
	case errors.As(err, &perr):
		return &fs.PathError{Op: perr.Op, Path: target, Err: perr.Err}
	case errors.As(err, &lerr):
		return &fs.PathError{Op: lerr.Op, Path: target, Err: lerr.Err}
	}
	return err
}

// ReadDir implements hostos.OS.
func (o *pluginOS) ReadDir(ctx context.Context, path string, hidden bool) ([]hostos.DirEntry, error) {
	if err := ctxErr(ctx); err != nil {
		return nil, err
	}
	p := o.abs(path)
	ents, err := os.ReadDir(p)
	if err != nil {
		return nil, mapErr(err, p)
	}
	out := make([]hostos.DirEntry, 0, len(ents))
	for _, e := range ents {
		if !hidden && strings.HasPrefix(e.Name(), ".") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue // removed meanwhile
		}
		de := hostos.DirEntry{Name: e.Name(), Kind: kindOfMode(info.Mode()), Size: info.Size(), MTime: info.ModTime()}
		if info.Mode()&fs.ModeSymlink != 0 {
			if st, err := os.Stat(filepath.Join(p, e.Name())); err == nil {
				de.TargetKind = kindOfMode(st.Mode())
			}
		}
		out = append(out, de)
	}
	return out, nil
}

// Stat implements hostos.OS. It follows symbolic links; a link whose target is gone is
// reported as a "symlink".
func (o *pluginOS) Stat(ctx context.Context, path string) (hostos.FileStat, error) {
	if err := ctxErr(ctx); err != nil {
		return hostos.FileStat{}, err
	}
	p := o.abs(path)
	st, err := os.Stat(p)
	if err != nil {
		if l, lerr := os.Lstat(p); lerr == nil {
			st = l
		} else {
			return hostos.FileStat{}, mapErr(err, p)
		}
	}
	return hostos.FileStat{Kind: kindOfMode(st.Mode()), Size: st.Size(), MTime: st.ModTime(), Mode: st.Mode()}, nil
}

// Mkdir implements hostos.OS.
func (o *pluginOS) Mkdir(ctx context.Context, path string, parents bool) error {
	if err := ctxErr(ctx); err != nil {
		return err
	}
	p := o.abs(path)
	if parents {
		return mapErr(os.MkdirAll(p, 0o755), p)
	}
	return mapErr(os.Mkdir(p, 0o755), p)
}

// Remove implements hostos.OS. A recursive removal goes through os.Root, so it cannot
// follow a link out of the directory being removed. Removing a filesystem root is refused
// here as well, whatever the caller checked.
func (o *pluginOS) Remove(ctx context.Context, path string, recursive bool) error {
	if err := ctxErr(ctx); err != nil {
		return err
	}
	p := o.abs(path)
	if filepath.Dir(p) == p {
		return pluginapi.Errf(pluginapi.CodeInvalid, "refusing to remove the filesystem root")
	}
	if err := o.refuseProtected(p); err != nil {
		return err
	}
	if !recursive {
		return mapErr(os.Remove(p), p)
	}
	if _, err := os.Lstat(p); err != nil {
		return mapErr(err, p)
	}
	root, err := os.OpenRoot(filepath.Dir(p))
	if err != nil {
		return mapErr(err, p)
	}
	defer func() { _ = root.Close() }()
	return mapErr(root.RemoveAll(filepath.Base(p)), p)
}

// refuseProtected refuses to remove the home, workspace, working or config directory, or a
// directory that contains one, by file identity: a case variant of the name on a
// case-insensitive file system, or a spelling through a link, is the same directory. A
// symbolic link at p is only a link and may go.
func (o *pluginOS) refuseProtected(p string) error {
	target, err := os.Lstat(p)
	if err != nil || target.Mode()&fs.ModeSymlink != 0 {
		return nil // the removal reports a missing target; a link removes only itself
	}
	d := o.dirs()
	for _, g := range []string{d.Home, d.Root, d.Cwd, d.Config} {
		if g == "" {
			continue
		}
		for cur := filepath.Clean(g); ; {
			if st, err := os.Stat(cur); err == nil && os.SameFile(target, st) {
				return pluginapi.Errf(pluginapi.CodeInvalid, "refusing to remove %s: it is or contains the home, workspace or config directory", p)
			}
			parent := filepath.Dir(cur)
			if parent == cur {
				break
			}
			cur = parent
		}
	}
	return nil
}

// Rename implements hostos.OS. Across file systems it copies and removes.
func (o *pluginOS) Rename(ctx context.Context, from, to string) error {
	if err := ctxErr(ctx); err != nil {
		return err
	}
	a, b := o.abs(from), o.abs(to)
	err := os.Rename(a, b)
	if err == nil || !errors.Is(err, syscall.EXDEV) {
		return mapErr(err, a)
	}
	if err := o.Copy(ctx, a, b); err != nil {
		return err
	}
	return o.Remove(ctx, a, true)
}

// RealPath implements hostos.OS.
func (o *pluginOS) RealPath(ctx context.Context, path string) (string, error) {
	if err := ctxErr(ctx); err != nil {
		return "", err
	}
	p := o.abs(path)
	real, err := filepath.EvalSymlinks(p)
	if err != nil {
		return "", mapErr(err, p)
	}
	return filepath.Clean(real), nil
}

// Copy implements hostos.OS: a file, or a directory tree (symbolic links are copied as
// links, never followed). The destination must not be an existing directory.
func (o *pluginOS) Copy(ctx context.Context, from, to string) error {
	if err := ctxErr(ctx); err != nil {
		return err
	}
	a, b := o.abs(from), o.abs(to)
	st, err := os.Stat(a)
	if err != nil {
		return mapErr(err, a)
	}
	if d, err := os.Stat(b); err == nil && d.IsDir() {
		return pluginapi.Errf(pluginapi.CodeConflict, "%s: the destination is a directory", b)
	}
	if dst, err := os.Stat(b); err == nil && os.SameFile(st, dst) {
		return pluginapi.Errf(pluginapi.CodeInvalid, "%s: source and destination are the same file", a)
	}
	if !st.IsDir() {
		if !st.Mode().IsRegular() {
			return pluginapi.Errf(pluginapi.CodeInvalid, "%s: not a regular file", a)
		}
		return mapErr(copyFile(ctx, a, b, st.Mode().Perm()), a)
	}
	if rel, err := filepath.Rel(a, b); err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return pluginapi.Errf(pluginapi.CodeInvalid, "cannot copy %s into itself", a)
	}
	root, err := os.OpenRoot(a)
	if err != nil {
		return mapErr(err, a)
	}
	defer func() { _ = root.Close() }()
	if err := os.MkdirAll(b, st.Mode().Perm()|0o700); err != nil {
		return mapErr(err, b)
	}
	n := 0
	err = fs.WalkDir(root.FS(), ".", func(rel string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if n++; n > copyTreeMax {
			return pluginapi.Errf(pluginapi.CodeLimit, "more than %d entries to copy", copyTreeMax)
		}
		if n%walkCtxEvery == 0 {
			if err := ctx.Err(); err != nil {
				return err
			}
		}
		if rel == "." {
			return nil
		}
		dst := filepath.Join(b, filepath.FromSlash(rel))
		switch typ := d.Type(); {
		case typ&fs.ModeSymlink != 0:
			target, err := fs.ReadLink(root.FS(), rel)
			if err != nil {
				return err
			}
			return os.Symlink(target, dst)
		case d.IsDir():
			info, err := d.Info()
			if err != nil {
				return err
			}
			return os.MkdirAll(dst, info.Mode().Perm()|0o700)
		case typ.IsRegular():
			info, err := d.Info()
			if err != nil {
				return err
			}
			src, err := root.Open(rel)
			if err != nil {
				return err
			}
			defer func() { _ = src.Close() }()
			return writeStream(ctx, src, dst, info.Mode().Perm())
		}
		return nil // sockets, devices and the like are not copied
	})
	return mapErr(err, a)
}

func copyFile(ctx context.Context, from, to string, mode fs.FileMode) error {
	// O_NONBLOCK: a file swapped for a FIFO after Copy checked it must not park the
	// goroutine in open (ctx cannot interrupt open).
	src, err := os.OpenFile(from, os.O_RDONLY|openNoBlock, 0)
	if err != nil {
		return err
	}
	defer func() { _ = src.Close() }()
	if st, err := src.Stat(); err != nil {
		return err
	} else if !st.Mode().IsRegular() {
		return pluginapi.Errf(pluginapi.CodeInvalid, "%s: not a regular file", from)
	}
	return writeStream(ctx, src, to, mode)
}

func writeStream(ctx context.Context, src io.Reader, to string, mode fs.FileMode) error {
	dst, err := os.OpenFile(to, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode|0o200)
	if err != nil {
		return err
	}
	buf := make([]byte, copyChunk)
	_, err = io.CopyBuffer(dst, &ctxReader{ctx: ctx, r: src}, buf)
	return errors.Join(err, dst.Close())
}

// Walk implements hostos.OS: a depth-first listing in lexical order below root, not
// following symbolic links, skipping the named directories and (unless Hidden) dotfiles.
// Unreadable subdirectories are skipped.
func (o *pluginOS) Walk(ctx context.Context, root string, wo hostos.WalkOptions) (hostos.WalkResult, error) {
	if err := ctxErr(ctx); err != nil {
		return hostos.WalkResult{}, err
	}
	base := o.abs(root)
	r, err := os.OpenRoot(base)
	if err != nil {
		return hostos.WalkResult{}, mapErr(err, base)
	}
	defer func() { _ = r.Close() }()
	limit := wo.MaxEntries
	if limit <= 0 {
		limit = 50_000
	}
	skip := make(map[string]bool, len(wo.SkipDirs))
	for _, s := range wo.SkipDirs {
		skip[s] = true
	}
	var res hostos.WalkResult
	n := 0
	err = fs.WalkDir(r.FS(), ".", func(rel string, d fs.DirEntry, err error) error {
		if n++; n%walkCtxEvery == 0 {
			if cerr := ctx.Err(); cerr != nil {
				return cerr
			}
		}
		if rel == "." {
			return err
		}
		if err != nil {
			if d != nil && d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		name := d.Name()
		if (!wo.Hidden && strings.HasPrefix(name, ".")) || (d.IsDir() && skip[name]) {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		depth := strings.Count(rel, "/") + 1
		if wo.MaxDepth > 0 && depth > wo.MaxDepth {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if len(res.Entries) >= limit {
			res.Truncated = true
			return fs.SkipAll
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		res.Entries = append(res.Entries, hostos.WalkEntry{
			Path: filepath.Join(base, filepath.FromSlash(rel)), Rel: rel,
			Kind: kindOfMode(info.Mode()), Size: info.Size(), MTime: info.ModTime(),
		})
		return nil
	})
	if err != nil {
		return hostos.WalkResult{}, mapErr(err, base)
	}
	return res, nil
}

// ---------- environment ----------

// LookPath implements hostos.OS.
func (o *pluginOS) LookPath(name string) (string, bool) {
	p, err := exec.LookPath(name)
	return p, err == nil
}

// Getenv implements hostos.OS.
func (o *pluginOS) Getenv(name string) (string, bool) { return os.LookupEnv(name) }

// Dirs implements hostos.OS.
func (o *pluginOS) Dirs() hostos.Dirs { return o.dirs() }

// Now implements hostos.OS.
func (o *pluginOS) Now() time.Time { return time.Now() }

// Shutdown implements hostos.OS: it kills every live process group with SIGKILL at once and
// stops every watcher; later Run, Start and Watch calls fail.
func (o *pluginOS) Shutdown() {
	o.mu.Lock()
	o.closed = true
	procs := make([]*osProc, 0, len(o.procs))
	for p := range o.procs {
		procs = append(procs, p)
	}
	watches := make([]*osWatch, 0, len(o.watches))
	for w := range o.watches {
		watches = append(watches, w)
	}
	o.mu.Unlock()
	for _, p := range procs {
		p.killGroup("KILL")
	}
	for _, w := range watches {
		w.Stop()
	}
}

func errShutdown() error {
	return pluginapi.Errf(pluginapi.CodeClosed, "the editor is shutting down")
}
