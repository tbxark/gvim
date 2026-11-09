package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/tbxark/gvim/plugin/hostos"
)

// finderGitOneShot answers the remote finder's git probe and listing. It records the
// commands and can simulate a not-a-worktree, a failed/truncated listing or a listing that
// blocks until cancellation.
type finderGitOneShot struct {
	mu        sync.Mutex
	repo      bool
	listing   []byte
	listCode  int
	truncated bool
	failProbe error
	blockList bool
	calls     []string
	listCalls int
}

func (o *finderGitOneShot) Run(ctx context.Context, argv []string, _ hostos.ProcOptions) (hostos.ProcResult, error) {
	o.mu.Lock()
	o.calls = append(o.calls, strings.Join(argv, " "))
	o.mu.Unlock()
	joined := strings.Join(argv, " ")
	if strings.Contains(joined, "rev-parse") {
		if o.failProbe != nil {
			return hostos.ProcResult{}, o.failProbe
		}
		if !o.repo {
			return hostos.ProcResult{Code: 1}, nil
		}
		return hostos.ProcResult{Stdout: []byte("true\n")}, nil
	}
	if strings.Contains(joined, "ls-files") {
		o.mu.Lock()
		o.listCalls++
		block := o.blockList
		listing := o.listing
		code, truncated := o.listCode, o.truncated
		o.mu.Unlock()
		if block {
			<-ctx.Done()
			return hostos.ProcResult{}, ctx.Err()
		}
		return hostos.ProcResult{Stdout: listing, Code: code, Truncated: truncated}, nil
	}
	return hostos.ProcResult{}, errors.New("unexpected remote command: " + joined)
}

func (o *finderGitOneShot) setListing(listing []byte) {
	o.mu.Lock()
	o.listing = listing
	o.mu.Unlock()
}

func (o *finderGitOneShot) counts() (calls, lists int) {
	o.mu.Lock()
	defer o.mu.Unlock()
	return len(o.calls), o.listCalls
}

// TestRemoteFileIndexUsesGitCandidateList checks a git work tree's candidate list fills
// the quick-open index in one traversal: tracked files (including tracked-ignored ones),
// untracked non-ignored files and nested paths are listed; .git internals, hidden paths,
// skip directories, invalid/non-UTF-8 names and paths deeper than the walk bound are not;
// and no recursive remote ReadDir runs.
func TestRemoteFileIndexUsesGitCandidateList(t *testing.T) {
	fs := newRemoteFinderFake() // no directories: a walk would fail
	local := t.TempDir()
	if err := os.WriteFile(filepath.Join(local, "a.go"), []byte("local decoy"), 0o600); err != nil {
		t.Fatal(err)
	}
	deep := strings.Repeat("d/", 65) + "f.go"
	listing := []byte(
		gitLsEntry("100644", "a.go") +
			gitLsEntry("100644", "tracked.log") + // tracked, matches .gitignore: Fresh keeps it
			gitLsEntry("100644", ".config") + // tracked dotfile: hidden in Quick Open
			gitLsEntry("100644", "deep/ok.go") +
			gitLsEntry("100644", ".git/HEAD") +
			"sub/b.go\x00" + // untracked non-ignored
			".hidden/x.go\x00" +
			"node_modules/pkg/x.go\x00" +
			gitLsEntry("100644", deep) +
			"bad\xff.go\x00",
	)
	one := &finderGitOneShot{repo: true, listing: listing}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	idx, root := newRemoteFileIndex(ctx, remoteFinderSlot(fs, one), "/remote/w")
	waitRemoteIndex(t, idx)
	if root != "/remote/w" {
		t.Fatalf("root = %q", root)
	}
	files, loading := idx.Snapshot()
	sort.Strings(files)
	want := "a.go,deep/ok.go,sub/b.go,tracked.log"
	if loading || strings.Join(files, ",") != want {
		t.Fatalf("files = %v (loading %v), want %s", files, loading, want)
	}
	if len(fs.readList()) != 0 {
		t.Fatalf("the git path ran recursive ReadDir calls: %v", fs.readList())
	}
	if calls, lists := one.counts(); lists != 1 || calls != 2 {
		t.Fatalf("commands = %d (listings %d), want one probe and one listing", calls, lists)
	}
	if got, err := os.ReadFile(filepath.Join(local, "a.go")); err != nil || string(got) != "local decoy" {
		t.Fatalf("the local decoy was touched: %q, %v", got, err)
	}
}

// TestRemoteFileIndexGitRestartRelists checks a refresh/Restart re-runs the remote git
// listing (fresh carrier or changed work tree) instead of reusing the first snapshot.
func TestRemoteFileIndexGitRestartRelists(t *testing.T) {
	fs := newRemoteFinderFake()
	one := &finderGitOneShot{repo: true, listing: []byte(gitLsEntry("100644", "a.go"))}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	idx, _ := newRemoteFileIndex(ctx, remoteFinderSlot(fs, one), "/remote/w")
	waitRemoteIndex(t, idx)
	if files, _ := idx.Snapshot(); strings.Join(files, ",") != "a.go" {
		t.Fatalf("first snapshot = %v", files)
	}
	one.setListing([]byte(gitLsEntry("100644", "b.go")))
	idx.Restart()
	waitRemoteIndex(t, idx)
	if files, _ := idx.Snapshot(); strings.Join(files, ",") != "b.go" {
		t.Fatalf("snapshot after refresh = %v, want b.go", files)
	}
	if _, lists := one.counts(); lists != 2 {
		t.Fatalf("git listings = %d, want one per index run", lists)
	}
}

// TestRemoteFileIndexFallsBackToWalk checks the cases where the one-pass git list cannot
// represent the project: a nested repository listed by name (contents are not in the
// output), a non-repository, a failed listing and a truncated listing all fall back to the
// recursive authority walk, which keeps their files and never touches the local disk.
func TestRemoteFileIndexFallsBackToWalk(t *testing.T) {
	cases := map[string]func() *finderGitOneShot{
		"nested repository": func() *finderGitOneShot {
			return &finderGitOneShot{repo: true, listing: []byte(gitLsEntry("160000", "vendor") + gitLsEntry("100644", "a.go"))}
		},
		"untracked nested repository": func() *finderGitOneShot {
			return &finderGitOneShot{repo: true, listing: []byte("nested/\x00" + gitLsEntry("100644", "a.go"))}
		},
		"not a repository": func() *finderGitOneShot { return &finderGitOneShot{repo: false} },
		"listing failed":   func() *finderGitOneShot { return &finderGitOneShot{repo: true, listCode: 128} },
		"listing truncated": func() *finderGitOneShot {
			return &finderGitOneShot{repo: true, truncated: true, listing: []byte(gitLsEntry("100644", "a.go"))}
		},
		"probe failed": func() *finderGitOneShot { return &finderGitOneShot{failProbe: errors.New("no git")} },
	}
	for name, makeOne := range cases {
		t.Run(name, func(t *testing.T) {
			fs := newRemoteFinderFake().dir("/remote/w", "a.go", "src/", "vendor/").dir("/remote/w/src", "b.go").dir("/remote/w/vendor", "inner.go")
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			idx, _ := newRemoteFileIndex(ctx, remoteFinderSlot(fs, makeOne()), "/remote/w")
			waitRemoteIndex(t, idx)
			files, _ := idx.Snapshot()
			sort.Strings(files)
			want := "a.go,src/b.go,vendor/inner.go"
			if got := strings.Join(files, ","); got != want {
				t.Fatalf("files = %v, want %s", files, want)
			}
			if len(fs.readList()) == 0 {
				t.Fatal("the fallback did not walk the remote file system")
			}
		})
	}
}

// TestRemoteFileIndexGitCandidateCancellation checks a cancelled git listing stops the
// index with context.Canceled and never falls back to a local enumeration.
func TestRemoteFileIndexGitCandidateCancellation(t *testing.T) {
	fs := newRemoteFinderFake()
	one := &finderGitOneShot{repo: true, blockList: true}
	ctx, cancel := context.WithCancel(context.Background())
	idx, _ := newRemoteFileIndex(ctx, remoteFinderSlot(fs, one), "/remote/w")
	deadline := time.After(10 * time.Second)
	for {
		_, lists := one.counts()
		if lists == 1 {
			break
		}
		select {
		case <-deadline:
			t.Fatal("the git listing did not start")
		case <-time.After(time.Millisecond):
		}
	}
	cancel()
	waitRemoteIndex(t, idx)
	if !errors.Is(idx.Err(), context.Canceled) {
		t.Fatalf("index error = %v, want context.Canceled", idx.Err())
	}
}
