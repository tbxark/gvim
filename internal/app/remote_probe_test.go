package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/tbxark/gvim/authority"
	"github.com/tbxark/gvim/plugin/hostos"
)

// remoteProbeFS is an authority.FileSystem with the optional RealPath capability: the
// maps are the remote host, so a local decoy path with the same name must never appear.
type remoteProbeFS struct {
	*remoteFinderFake
	mu    sync.Mutex
	stats map[string]hostos.FileStat
	reals map[string]string
}

func newRemoteProbeFS() *remoteProbeFS {
	return &remoteProbeFS{
		remoteFinderFake: newRemoteFinderFake(),
		stats:            map[string]hostos.FileStat{},
		reals:            map[string]string{},
	}
}

func (f *remoteProbeFS) Stat(_ context.Context, path string) (hostos.FileStat, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	st, ok := f.stats[path]
	if !ok {
		return hostos.FileStat{}, errors.New("not found")
	}
	return st, nil
}

func (f *remoteProbeFS) RealPath(_ context.Context, path string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	real, ok := f.reals[path]
	if !ok {
		return "", errors.New("not found")
	}
	return real, nil
}

func remoteProbeSlot(fs authority.FileSystem) authority.Slot {
	return authority.New(authority.Parts{Files: fs})
}

// TestRemotePrefixProbeUsesAuthorityRealPath: the remote probe resolves both the root
// and the candidate through the authority's RealPath capability and stats the remote
// file; a space in the path is legal.
func TestRemotePrefixProbeUsesAuthorityRealPath(t *testing.T) {
	fs := newRemoteProbeFS()
	fs.reals["/remote/w"] = "/remote/w"
	fs.reals["/remote/w/a.go"] = "/remote/w/a.go"
	fs.reals["/remote/w/my file.txt"] = "/remote/w/my file.txt"
	fs.stats["/remote/w/a.go"] = hostos.FileStat{Kind: "file"}
	fs.stats["/remote/w/my file.txt"] = hostos.FileStat{Kind: "file"}
	probe := newRemotePrefixProbe(remoteProbeSlot(fs), "/remote/w")
	for _, tt := range []struct {
		query string
		want  []string
	}{
		{"a.go", []string{"a.go"}},
		{"my file.txt", []string{"my file.txt"}},
		{"missing.go", nil},
		{"../secret", nil},
	} {
		got, err := probe(context.Background(), tt.query)
		if err != nil || len(got) != len(tt.want) || (len(got) > 0 && got[0] != tt.want[0]) {
			t.Errorf("probe(%q) = %v, %v; want %v", tt.query, got, err, tt.want)
		}
	}
}

// TestRemotePrefixProbeRejectsSymlinkEscape: a candidate whose canonical remote path is
// outside the canonical root is refused, even though it stats as a file.
func TestRemotePrefixProbeRejectsSymlinkEscape(t *testing.T) {
	fs := newRemoteProbeFS()
	fs.reals["/remote/w"] = "/remote/w"
	fs.reals["/remote/w/link.go"] = "/etc/passwd"
	fs.reals["/remote/w/out/secret.go"] = "/tmp/out/secret.go"
	fs.stats["/etc/passwd"] = hostos.FileStat{Kind: "file"}
	fs.stats["/tmp/out/secret.go"] = hostos.FileStat{Kind: "file"}
	probe := newRemotePrefixProbe(remoteProbeSlot(fs), "/remote/w")
	for _, query := range []string{"link.go", "out/secret.go"} {
		got, err := probe(context.Background(), query)
		if err != nil || len(got) != 0 {
			t.Errorf("probe(%q) = %v, %v; want no result for a symlink escape", query, got, err)
		}
	}
}

// TestRemotePrefixProbeNeedsRealPath: a backend that cannot resolve symlinks cannot
// prove containment, so it yields no result instead of guessing or reading locally.
func TestRemotePrefixProbeNeedsRealPath(t *testing.T) {
	// The plain fake has no RealPath method; its Stat also refuses.
	probe := newRemotePrefixProbe(remoteProbeSlot(newRemoteFinderFake()), "/remote/w")
	if got, err := probe(context.Background(), "a.go"); err != nil || len(got) != 0 {
		t.Fatalf("probe without realpath = %v, %v; want no result", got, err)
	}
}

// TestRemotePrefixProbeNeverFallsBackLocal: a local decoy with the query's name is not
// consulted when the remote host does not have the file.
func TestRemotePrefixProbeNeverFallsBackLocal(t *testing.T) {
	local := t.TempDir()
	if err := os.WriteFile(filepath.Join(local, "a.go"), []byte("local"), 0o600); err != nil {
		t.Fatal(err)
	}
	fs := newRemoteProbeFS()
	fs.reals["/remote/w"] = "/remote/w"
	probe := newRemotePrefixProbe(remoteProbeSlot(fs), "/remote/w")
	if got, err := probe(context.Background(), "a.go"); err != nil || len(got) != 0 {
		t.Fatalf("remote probe returned %v, %v for a local-only file", got, err)
	}
}

// TestRemotePrefixProbeHonorsCancellation: the authority calls receive the palette's
// context, and a canceled probe is an error rather than a guessed result.
func TestRemotePrefixProbeHonorsCancellation(t *testing.T) {
	fs := newRemoteProbeFS()
	fs.reals["/remote/w"] = "/remote/w"
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := newRemotePrefixProbe(remoteProbeSlot(fs), "/remote/w")(ctx, "a.go"); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled remote probe error = %v, want context.Canceled", err)
	}
}

// TestRemoteFileIndexInstallsTheProbe: the remote index owns the authority-only probe.
func TestRemoteFileIndexInstallsTheProbe(t *testing.T) {
	fs := newRemoteProbeFS()
	fs.dirs["/remote/w"] = []hostos.DirEntry{{Name: "a.go", Kind: "file"}}
	fs.reals["/remote/w"] = "/remote/w"
	fs.reals["/remote/w/a.go"] = "/remote/w/a.go"
	fs.stats["/remote/w/a.go"] = hostos.FileStat{Kind: "file"}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	idx, root := newRemoteFileIndex(ctx, remoteProbeSlot(fs), "/remote/w")
	if root != "/remote/w" {
		t.Fatalf("root = %q, want /remote/w", root)
	}
	if got, err := idx.Probe(context.Background(), "a.go"); err != nil || len(got) != 1 || got[0] != "a.go" {
		t.Fatalf("index probe = %v, %v; want a.go", got, err)
	}
}
