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
type Index struct {
	cancel context.CancelFunc
	done   chan struct{}

	mu        sync.Mutex
	files     []string
	truncated bool
	err       error
}

// Start begins enumerating root in a goroutine and returns at once. The index
// stops when ctx is done or Close is called. Enumeration errors are kept for
// Err; a partial list is still usable.
func Start(ctx context.Context, root string) *Index {
	return startWith(ctx, func(ctx context.Context, emit func(string) bool) error {
		return Enumerate(ctx, root, emit)
	})
}

// startWith is Start over any enumeration. Tests use it to feed a fake list.
// enum must return when ctx is done and must stop when emit returns false.
func startWith(ctx context.Context, enum func(context.Context, func(string) bool) error) *Index {
	ctx, cancel := context.WithCancel(ctx)
	x := &Index{cancel: cancel, done: make(chan struct{})}
	go func() {
		defer close(x.done)
		err := enum(ctx, x.add)
		x.mu.Lock()
		x.err = err
		x.mu.Unlock()
	}()
	return x
}

// add appends one file. It returns false once MaxFiles files are held.
func (x *Index) add(rel string) bool {
	x.mu.Lock()
	defer x.mu.Unlock()
	if len(x.files) >= MaxFiles {
		x.truncated = true
		return false
	}
	x.files = append(x.files, rel)
	return true
}

// Snapshot returns the files found so far and whether enumeration is still
// running. The slice is never modified after it is returned, so the caller may
// read it while enumeration continues. The state is read before the list: a
// false loading then means the list holds every file, as add ran before done.
func (x *Index) Snapshot() (files []string, loading bool) {
	loading = !x.finished()
	x.mu.Lock()
	files = x.files
	x.mu.Unlock()
	return files, loading
}

// Loading reports whether enumeration is still running.
func (x *Index) Loading() bool { return !x.finished() }

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

// Done returns a channel that is closed when enumeration has finished.
func (x *Index) Done() <-chan struct{} { return x.done }

// Close stops enumeration and waits for the goroutine to exit.
func (x *Index) Close() {
	x.cancel()
	<-x.done
}

func (x *Index) finished() bool {
	select {
	case <-x.done:
		return true
	default:
		return false
	}
}
