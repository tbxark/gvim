// Package finder lists the project's files for the quick-open picker and ranks
// them against a query. It follows Fresh's file provider
// (crates/fresh-editor/src/input/quick_open/providers.rs and
// crates/fresh-editor/src/services/project_files.rs).
//
// Enumeration prefers git: inside a work tree, "git ls-files" lists tracked and
// untracked files that .gitignore does not exclude. Elsewhere, or when git is
// missing, a walk of the directory honours .gitignore through explorer.
// Start runs enumeration in the background so the picker can open at once, with
// the status "Loading files…" until the list is complete.
package finder

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"sync"

	"github.com/tbxark/gvim/explorer"
)

// MaxFiles caps the list. Enumeration stops at this many files, as Fresh's
// MAX_FILES does.
const MaxFiles = 50000

// LoadingStatus is the status line shown while enumeration is running.
const LoadingStatus = "Loading files…"

// skipDirs are the directories the walk never enters. The list is Fresh's
// IGNORED_DIRS; the git path does not need it, because git's own exclusions
// already keep build trees out.
var skipDirs = []string{".git", "node_modules", "target", "__pycache__", ".hg", ".svn", ".DS_Store"}

// Enumerate calls emit with each project file under root, as a slash-separated
// path relative to root, until emit returns false or ctx is done. It uses git
// when root is inside a work tree and the directory walk otherwise. A file
// listed by git but deleted from disk is still emitted; git lists the index.
func Enumerate(ctx context.Context, root string, emit func(rel string) bool) error {
	done, err := gitFiles(ctx, root, emit)
	if done {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return explorer.WalkFiles(ctx, root, explorer.WalkOptions{SkipDirs: skipDirs}, emit)
}

// Options selects extra files for a listing.
type Options struct {
	// ShowHidden visits names starting with "." (the walk still skips the skipDirs
	// such as .git).
	ShowHidden bool
	// ShowIgnored visits files matched by .gitignore.
	ShowIgnored bool
}

// EnumerateAll calls emit with each project file under root like Enumerate, but
// includes the hidden and .gitignore-matched files opts asks for. It always walks the
// tree when it has to include them: git's ls-files cannot list ignored files alongside
// the tracked ones. With the zero Options it is Enumerate.
func EnumerateAll(ctx context.Context, root string, opts Options, emit func(rel string) bool) error {
	if !opts.ShowHidden && !opts.ShowIgnored {
		return Enumerate(ctx, root, emit)
	}
	return explorer.WalkFiles(ctx, root, explorer.WalkOptions{
		ShowHidden:  opts.ShowHidden,
		ShowIgnored: opts.ShowIgnored,
		SkipDirs:    skipDirs,
	}, emit)
}

// gitFiles runs "git ls-files" in root. done is false when git cannot list the
// directory (no git binary, or root is outside a work tree), so the caller
// falls back to the walk. Entries that git lists by name only, such as nested
// repositories, are left out.
func gitFiles(ctx context.Context, root string, emit func(rel string) bool) (done bool, err error) {
	if _, err := exec.LookPath("git"); err != nil {
		return false, nil
	}
	cctx, cancel := context.WithCancel(ctx)
	defer cancel()
	cmd := exec.CommandContext(cctx, "git", "-C", root, "ls-files", "-z", "-c", "-o", "--exclude-standard")
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return false, nil
	}
	if err := cmd.Start(); err != nil {
		return false, nil
	}

	sc := bufio.NewScanner(stdout)
	sc.Split(splitNUL)
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	emitted, stopped := 0, false
	for sc.Scan() {
		p := sc.Text()
		if p == "" || strings.HasSuffix(p, "/") {
			continue
		}
		emitted++
		if !emit(p) {
			stopped = true
			break
		}
	}
	if stopped {
		cancel() // kill git; its exit status no longer matters
	}
	waitErr := cmd.Wait()
	switch {
	case stopped:
		return true, nil
	case ctx.Err() != nil:
		return true, ctx.Err()
	case sc.Err() != nil:
		return true, fmt.Errorf("finder: git ls-files: %w", sc.Err())
	case waitErr != nil && emitted == 0:
		return false, nil // not a work tree, so the walk takes over
	case waitErr != nil:
		return true, fmt.Errorf("finder: git ls-files: %w", waitErr)
	}
	return true, nil
}

// splitNUL is a bufio.SplitFunc for the NUL-separated output of ls-files -z.
func splitNUL(data []byte, atEOF bool) (advance int, token []byte, err error) {
	if i := bytes.IndexByte(data, 0); i >= 0 {
		return i + 1, data[:i], nil
	}
	if atEOF && len(data) > 0 {
		return len(data), data, nil
	}
	return 0, nil, nil
}

// Index is the file list of one root, filled in the background. It is safe for
// concurrent use by the picker and the goroutine that fills it.
//
// Restart re-enumerates in place: the same *Index stays the palette's Files port, the old
// run is cancelled and waited for, the collected list is cleared, and a new run starts.
// Snapshot reports loading from the moment Restart begins, so a palette polling while the
// index loads sees a coherent (empty or partial) new list instead of a stale complete one.
type Index struct {
	parent context.Context
	enum   func(context.Context, func(string) bool, func(string)) error

	// lifecycle serializes Start/Close/Restart; mu guards the state readers use.
	lifecycle sync.Mutex
	cancel    context.CancelFunc
	closed    bool

	mu        sync.Mutex
	done      chan struct{}
	gen       uint64
	files     []string
	truncated bool
	err       error
	// notice is a non-fatal message the enumeration attached to this run (for example
	// "used a directory walk because the git file list was unavailable"). It is cleared
	// when a run starts, so a reconnect/refresh does not keep a stale notice.
	notice string
	// probe is the optional literal-path probe (Fresh's fast prefix-probe slice).
	// It performs no enumeration and is set before the palette uses the index; the
	// lock only protects a late SetProbe from a reader.
	probeMu sync.RWMutex
	probe   ProbeFunc
}

// Start begins enumerating root in a goroutine and returns at once. The index
// stops when ctx is done or Close is called. Enumeration errors are kept for
// Err; a partial list is still usable.
func Start(ctx context.Context, root string) *Index {
	x := startWith(ctx, func(ctx context.Context, emit func(string) bool) error {
		return Enumerate(ctx, root, emit)
	})
	x.SetProbe(LocalPrefixProbe(root))
	return x
}

// SetProbe installs the literal-path probe used by the quick-open file mode while
// the index is loading. A nil probe disables it.
func (x *Index) SetProbe(probe ProbeFunc) {
	if x == nil {
		return
	}
	x.probeMu.Lock()
	x.probe = probe
	x.probeMu.Unlock()
}

// Probe asks the installed literal-path probe for the exact file query names. It
// returns nil for an index without a probe and does not touch the enumerated list.
// The palette calls it from a tea.Cmd, never from Update/Draw.
func (x *Index) Probe(ctx context.Context, query string) ([]string, error) {
	if x == nil {
		return nil, nil
	}
	x.probeMu.RLock()
	probe := x.probe
	x.probeMu.RUnlock()
	if probe == nil {
		return nil, nil
	}
	return probe(ctx, query)
}

// StartFunc begins an index over any enumeration function and returns at once. enum is
// called on every run (initial and after Restart) with a context and the emit callback;
// it must return when ctx is done and stop when emit returns false. It is the seam a
// backend with a flat file list (for example a remote `git ls-files` over the authority)
// uses to feed the same Index the local walk fills. Start uses StartFunc internally.
func StartFunc(ctx context.Context, enum func(context.Context, func(string) bool) error) *Index {
	return startNotifyWith(ctx, func(ctx context.Context, emit func(string) bool, _ func(string)) error {
		return enum(ctx, emit)
	})
}

// StartFuncNotify is StartFunc with a non-fatal notice: enum may call notify(text) to
// attach a message to this run (the last non-empty notice of the current run is kept; an
// empty text is ignored). Restart clears it, so a reconnect or manual refresh does not
// show a stale degradation. A run superseded by Restart cannot set its notice.
func StartFuncNotify(ctx context.Context, enum func(context.Context, func(string) bool, func(string)) error) *Index {
	return startNotifyWith(ctx, enum)
}

// startWith is Start over an enumeration without a notice; tests use it to feed a fake
// list. enum must return when ctx is done and must stop when emit returns false.
func startWith(ctx context.Context, enum func(context.Context, func(string) bool) error) *Index {
	return startNotifyWith(ctx, func(ctx context.Context, emit func(string) bool, _ func(string)) error {
		return enum(ctx, emit)
	})
}

// startNotifyWith is the shared constructor of every Index start.
func startNotifyWith(ctx context.Context, enum func(context.Context, func(string) bool, func(string)) error) *Index {
	if ctx == nil {
		ctx = context.Background()
	}
	x := &Index{parent: ctx, enum: enum}
	x.lifecycle.Lock()
	x.startLocked()
	x.lifecycle.Unlock()
	return x
}

// startLocked starts one enumeration run: a fresh done channel and generation, then the
// goroutine. The caller holds lifecycle.
func (x *Index) startLocked() {
	x.mu.Lock()
	x.gen++
	gen := x.gen
	done := make(chan struct{})
	x.done = done
	x.notice = ""
	x.mu.Unlock()
	ctx, cancel := context.WithCancel(x.parent)
	x.cancel = cancel
	go x.run(ctx, gen, done)
}

// run enumerates once. Its result is stored only when it is still the current run, so a
// run superseded by Restart cannot overwrite the fresh state; its own done channel is
// always closed.
func (x *Index) run(ctx context.Context, gen uint64, done chan struct{}) {
	err := x.enum(ctx, func(rel string) bool { return x.addFor(gen, rel) }, func(text string) { x.notifyFor(gen, text) })
	x.mu.Lock()
	if x.gen == gen {
		x.err = err
	}
	close(done)
	x.mu.Unlock()
}

// Restart cancels the current run, waits for it to stop, discards the list and starts a new
// enumeration with the same function and parent context. A closed or enum-less index
// (Idle) ignores it. It is safe to call while Snapshot is read: Snapshot reports loading
// from the restart on.
func (x *Index) Restart() {
	x.lifecycle.Lock()
	defer x.lifecycle.Unlock()
	if x.enum == nil || x.closed {
		return
	}
	x.mu.Lock()
	oldDone := x.done
	old := x.cancel
	x.gen++
	gen := x.gen
	done := make(chan struct{})
	x.done = done
	x.files = nil
	x.truncated = false
	x.err = nil
	x.notice = ""
	x.mu.Unlock()
	if old != nil {
		old()
	}
	<-oldDone
	ctx, cancel := context.WithCancel(x.parent)
	x.cancel = cancel
	go x.run(ctx, gen, done)
}

// add appends one file to the current run. It is the one-file form the older tests use;
// the running enumeration uses addFor with its own generation so a superseded run cannot
// inject files after a Restart.
func (x *Index) add(rel string) bool {
	x.mu.Lock()
	gen := x.gen
	x.mu.Unlock()
	return x.addFor(gen, rel)
}

// addFor appends one file of run gen. It returns false when the run was superseded or
// MaxFiles files are held.
func (x *Index) addFor(gen uint64, rel string) bool {
	x.mu.Lock()
	defer x.mu.Unlock()
	if x.gen != gen {
		return false
	}
	if len(x.files) >= MaxFiles {
		x.truncated = true
		return false
	}
	x.files = append(x.files, rel)
	return true
}

// Snapshot returns the files found so far and whether enumeration is still
// running. The slice is never modified after it is returned, so the caller may
// read it while enumeration continues. The state is read with the list: a false
// loading then means the list holds every file of that run.
func (x *Index) Snapshot() (files []string, loading bool) {
	x.mu.Lock()
	defer x.mu.Unlock()
	return x.files, !closedDone(x.done)
}

// Loading reports whether enumeration is still running.
func (x *Index) Loading() bool {
	x.mu.Lock()
	defer x.mu.Unlock()
	return !closedDone(x.done)
}

// Notice returns the non-fatal message the current run attached, or "". It is cleared
// when a run starts; a superseded run cannot change it.
func (x *Index) Notice() string {
	x.mu.Lock()
	defer x.mu.Unlock()
	return x.notice
}

// notifyFor records one notice of run gen if it is still current.
func (x *Index) notifyFor(gen uint64, text string) {
	if text == "" {
		return
	}
	x.mu.Lock()
	defer x.mu.Unlock()
	if x.gen == gen {
		x.notice = text
	}
}

// Truncated reports whether enumeration stopped at MaxFiles.
func (x *Index) Truncated() bool {
	x.mu.Lock()
	defer x.mu.Unlock()
	return x.truncated
}

// Err returns the enumeration error, or nil. It is nil while loading and after
// a complete run; after ctx is cancelled it is context.Canceled.
func (x *Index) Err() error {
	x.mu.Lock()
	defer x.mu.Unlock()
	return x.err
}

// Done returns a channel that is closed when the current enumeration finished. A Restart
// replaces the channel; a caller that must see every run should call Done again after a
// restart.
func (x *Index) Done() <-chan struct{} {
	x.mu.Lock()
	defer x.mu.Unlock()
	return x.done
}

// Close stops enumeration and waits for the goroutine to exit, then makes Restart a
// no-op.
func (x *Index) Close() {
	x.lifecycle.Lock()
	defer x.lifecycle.Unlock()
	if x.closed {
		return
	}
	x.closed = true
	x.mu.Lock()
	oldDone := x.done
	x.mu.Unlock()
	if x.cancel != nil {
		x.cancel()
	}
	<-oldDone
}

// closedDone reports whether done is closed; done is never nil for an index built here.
func closedDone(done chan struct{}) bool {
	if done == nil {
		return true
	}
	select {
	case <-done:
		return true
	default:
		return false
	}
}
