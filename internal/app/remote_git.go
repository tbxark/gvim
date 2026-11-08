package app

// Remote git status for the explorer's status letters (task-116). The command runs on the
// remote host through the workspace authority's one-shot surface (with the remote
// environment delta and the spawn/trust gate); nothing here consults the local git state.
// A short-lived cache coalesces the save/watch/reload bursts the root asks during.

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/tbxark/gvim/authority"
	"github.com/tbxark/gvim/plugin/hostos"
)

const (
	// remoteGitStatusTimeout bounds one remote git status read.
	remoteGitStatusTimeout = 8 * time.Second
	// remoteGitStatusTTL is how long a read answers the root's repeated asks. The root
	// re-reads on save, watch reloads and reconnect; the cache only absorbs bursts.
	remoteGitStatusTTL = 2 * time.Second
	// remoteGitStatusMaxOutput bounds the porcelain output kept.
	remoteGitStatusMaxOutput = 8 << 20
)

// remoteGitStatus implements tui.GitStatus over a remote workspace: git status runs with
// the remote workspace as cwd and its normalized porcelain entries become the same
// A/M/D/U/R/C letters the local explorer shows, keyed by absolute remote path.
type remoteGitStatus struct {
	one      authority.OneShot
	root     string           // absolute remote workspace root
	spawnEnv func() []string  // remote captured environment delta; nil applies none
	now      func() time.Time // test clock; nil uses time.Now
	timeout  time.Duration    // read deadline; remoteGitStatusTimeout unless a test says otherwise

	mu       sync.Mutex
	cachedAt time.Time
	marks    map[string]rune
	err      error
	ttl      time.Duration
}

// newRemoteGitStatus returns the remote git-status port over root.
func newRemoteGitStatus(one authority.OneShot, root string, spawnEnv func() []string) *remoteGitStatus {
	return &remoteGitStatus{one: one, root: root, spawnEnv: spawnEnv, ttl: remoteGitStatusTTL, timeout: remoteGitStatusTimeout}
}

// Marks returns the status letters of the remote work tree that holds dir (the remote
// workspace root when dir is empty), read through the authority unless a fresh cached read
// exists.
func (g *remoteGitStatus) Marks(dir string) (map[string]rune, error) {
	g.mu.Lock()
	if g.ttl > 0 && !g.cachedAt.IsZero() && g.clock().Sub(g.cachedAt) < g.ttl {
		marks, err := cloneMarks(g.marks), g.err
		g.mu.Unlock()
		return marks, err
	}
	g.mu.Unlock()
	// The read always uses this slot's remote workspace root: the port is only installed
	// for a remote tree, and a caller passing a local-looking dir must never turn this into
	// a local git read.
	marks, err := g.read(g.root)
	g.mu.Lock()
	g.cachedAt = g.clock()
	g.marks = marks
	g.err = err
	g.mu.Unlock()
	if marks != nil {
		marks = cloneMarks(marks)
	}
	return marks, err
}

// Invalidate drops the cache, so the next Marks reads the host again (reconnect path).
func (g *remoteGitStatus) Invalidate() {
	g.mu.Lock()
	g.cachedAt = time.Time{}
	g.marks = nil
	g.err = nil
	g.mu.Unlock()
}

// clock reads the cache clock.
func (g *remoteGitStatus) clock() time.Time {
	if g.now != nil {
		return g.now()
	}
	return time.Now()
}

// read runs one bounded git status on the remote host and parses its porcelain-v1 -z
// output. A non-zero exit is an error (a workspace outside a repository included), never a
// silent empty status; the root turns it into the 'git status: ...' notice.
func (g *remoteGitStatus) read(root string) (map[string]rune, error) {
	deadline := g.timeout
	if deadline <= 0 {
		deadline = remoteGitStatusTimeout
	}
	ctx, cancel := context.WithTimeout(context.Background(), deadline)
	defer cancel()
	var env map[string]string
	if g.spawnEnv != nil {
		env = remoteEnvMap(g.spawnEnv())
	}
	res, err := g.one.Run(ctx, []string{"git", "status", "--porcelain=v1", "-z"},
		hostos.ProcOptions{Cwd: root, Env: env, MaxOutput: remoteGitStatusMaxOutput})
	if err != nil {
		if ctx.Err() != nil {
			return nil, fmt.Errorf("git status timed out after %s", deadline)
		}
		return nil, err
	}
	if res.Code != 0 {
		msg := strings.TrimSpace(string(res.Stderr))
		if len(msg) > 200 {
			msg = msg[:200]
		}
		if msg != "" {
			return nil, fmt.Errorf("git status failed (exit %d): %s", res.Code, msg)
		}
		return nil, fmt.Errorf("git status failed (exit %d)", res.Code)
	}
	return parseRemoteGitStatus(res.Stdout, root), nil
}

// parseRemoteGitStatus parses git status --porcelain=v1 -z into the explorer's letters,
// keyed by absolute remote path. The -z form separates entries with NUL and puts a rename
// or copy's original path in the following field. Approximations, matching the letters the
// local port exposes: a rename/copy shows R at its new path (the original is not marked),
// an unmerged pair shows C, a typechange shows M, and any other combination takes X then Y.
func parseRemoteGitStatus(data []byte, root string) map[string]rune {
	fields := strings.Split(string(data), "\x00")
	marks := make(map[string]rune, len(fields))
	for i := 0; i < len(fields); i++ {
		entry := fields[i]
		if len(entry) < 4 {
			continue
		}
		x, y, path := entry[0], entry[1], entry[3:]
		if x == '!' && y == '!' {
			continue // ignored entries are not shown
		}
		mark, ok := remoteGitMark(x, y)
		if !ok {
			continue
		}
		marks[joinRemotePath(root, path)] = mark
		if x == 'R' || x == 'C' || y == 'R' || y == 'C' {
			i++ // the original path of a rename/copy follows in its own NUL field
		}
	}
	return marks
}

// remoteGitMark maps one porcelain XY pair to the explorer letter. ok is false for a pair
// the panel should not show.
func remoteGitMark(x, y byte) (rune, bool) {
	switch {
	case x == '?' && y == '?':
		return 'U', true
	case x == 'U' || y == 'U', x == 'A' && y == 'A', x == 'D' && y == 'D', x == 'A' && y == 'D', x == 'D' && y == 'A':
		return 'C', true
	case x == 'R' || y == 'R':
		return 'R', true
	case x == 'C' || y == 'C':
		return 'R', true
	case x == 'A' || y == 'A':
		return 'A', true
	case x == 'D' || y == 'D':
		return 'D', true
	case x == 'M' || y == 'M', x == 'T' || y == 'T':
		return 'M', true
	case x == ' ' && y == ' ':
		return 0, false
	default:
		if x != ' ' {
			if mark, ok := porcelainLetter(x); ok {
				return mark, true
			}
		}
		return porcelainLetter(y)
	}
}

// porcelainLetter maps a single porcelain status char to the explorer letter.
func porcelainLetter(c byte) (rune, bool) {
	switch c {
	case 'A':
		return 'A', true
	case 'M', 'T':
		return 'M', true
	case 'D':
		return 'D', true
	case 'R', 'C':
		return 'R', true
	case 'U':
		return 'C', true
	default:
		return 0, false
	}
}

// cloneMarks copies a mark map so a caller cannot mutate the cache.
func cloneMarks(in map[string]rune) map[string]rune {
	if in == nil {
		return nil
	}
	out := make(map[string]rune, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
