package pluginhost

import (
	"context"
	"path"
	"testing"

	"github.com/tbxark/gvim/plugin/hostos"
	"github.com/tbxark/gvim/plugin/plugintest"
	"github.com/tbxark/gvim/pluginapi"
)

// fakeRealPathOnlyFiles has the realpath capability but no mutations or atomic directory
// claim: a tree copy must return unsupported before writing.
type fakeRealPathOnlyFiles struct{ *fakeRemoteFiles }

func (f *fakeRealPathOnlyFiles) RealPath(ctx context.Context, p string) (string, error) {
	if _, err := f.Stat(ctx, p); err != nil {
		return "", err
	}
	return path.Clean(p), nil
}

// fakeAliasFiles resolves the configured alias paths through realpath, proving whether the
// copy safety check uses canonical paths rather than spelling.
type fakeAliasFiles struct {
	*fakeMutableFiles
	real map[string]string
}

func (f *fakeAliasFiles) RealPath(ctx context.Context, p string) (string, error) {
	if real, ok := f.real[path.Clean(p)]; ok {
		return real, nil
	}
	if _, err := f.Stat(ctx, p); err != nil {
		return "", err
	}
	return path.Clean(p), nil
}

// TestAuthorityOSCopyRequiresRealPathCapability: a backend that cannot resolve symbolic
// links cannot prove a destination is not inside the source (or an alias), so copy returns
// unsupported before any stat/write of the destination and never falls back locally.
func TestAuthorityOSCopyRequiresRealPathCapability(t *testing.T) {
	files := newFakeRemoteFiles()
	slot, _, _ := authorityTestSlot(files)
	local := plugintest.NewFakeOS()
	osys := NewAuthorityOS(slot, local, nil).(*authorityOS)
	err := osys.Copy(context.Background(), "remote.txt", "copy.txt")
	if err == nil || pluginapi.CodeOf(err) != pluginapi.CodeUnsupported {
		t.Fatalf("copy without realpath = %v, code %q", err, pluginapi.CodeOf(err))
	}
	if _, ok := files.files["/srv/proj/copy.txt"]; ok {
		t.Fatal("unsupported copy wrote a destination file")
	}
	if calls := local.Calls(); len(calls) != 0 {
		t.Fatalf("unsupported copy touched the local OS: %v", calls)
	}
}

// TestAuthorityOSCopyRequiresAtomicClaim: realpath alone is not enough for a tree copy;
// without the atomic destination claim the check-then-mkdir fallback could merge into a
// concurrently created directory, so the copy returns unsupported before writing.
func TestAuthorityOSCopyRequiresAtomicClaim(t *testing.T) {
	files := &fakeRealPathOnlyFiles{fakeRemoteFiles: walkFixture()}
	slot, _, _ := authorityTestSlot(files)
	local := plugintest.NewFakeOS()
	osys := NewAuthorityOS(slot, local, nil).(*authorityOS)
	err := osys.Copy(context.Background(), "src", "dst")
	if err == nil || pluginapi.CodeOf(err) != pluginapi.CodeUnsupported {
		t.Fatalf("copy without atomic claim = %v, code %q", err, pluginapi.CodeOf(err))
	}
	if _, ok := files.files["/srv/proj/dst/b.go"]; ok {
		t.Fatal("unsupported tree copy wrote a destination file")
	}
	if calls := local.Calls(); len(calls) != 0 {
		t.Fatalf("unsupported copy touched the local OS: %v", calls)
	}
}

// TestAuthorityOSCopyCanonicalAliasesRefused: the source through a symlink alias and a
// destination through a symlink into the source are both refused before the tree walk or
// any write, not merely by comparing the original path strings.
func TestAuthorityOSCopyCanonicalAliasesRefused(t *testing.T) {
	remote := &fakeMutableFiles{fakeRemoteFiles: walkFixture()}
	remote.dirs["/srv/proj/alias"] = remote.dirs["/srv/proj/src"]
	remote.dirs["/srv/proj/parentalias"] = remote.dirs["/srv/proj/src"]
	files := &fakeAliasFiles{fakeMutableFiles: remote, real: map[string]string{
		"/srv/proj/alias":       "/srv/proj/src",
		"/srv/proj/parentalias": "/srv/proj/src",
	}}
	slot, _, _ := authorityTestSlot(files)
	local := plugintest.NewFakeOS()
	osys := NewAuthorityOS(slot, local, nil).(*authorityOS)
	for _, tc := range []struct{ from, to string }{
		{"alias", "src/new"},
		{"src", "parentalias/new"},
		{"src", "src/sub"},
		{"src", "src"},
	} {
		err := osys.Copy(context.Background(), tc.from, tc.to)
		if err == nil || pluginapi.CodeOf(err) != pluginapi.CodeInvalid {
			t.Fatalf("Copy(%q, %q) = %v, code %q; want invalid before write", tc.from, tc.to, err, pluginapi.CodeOf(err))
		}
	}
	if len(remote.mkdirs) != 0 || len(remote.reads) != 0 {
		t.Fatalf("a refused canonical copy did work, mkdirs=%v reads=%v", remote.mkdirs, remote.reads)
	}
	if calls := local.Calls(); len(calls) != 0 {
		t.Fatalf("a refused copy touched the local OS: %v", calls)
	}
}

// TestAuthorityOSCopyWithoutStreamerRefusesExistingDestination: on a backend with no
// atomic WriteStream the whole-file fallback truncates the destination. A destination
// hard-linked to the source would therefore destroy the source, and realpath cannot
// identify hard links; refuse an existing destination instead. A missing destination
// still takes the bounded fallback (no pre-existing data to lose).
func TestAuthorityOSCopyWithoutStreamerRefusesExistingDestination(t *testing.T) {
	remote := &fakeMutableFiles{fakeRemoteFiles: walkFixture()}
	remote.files["/srv/proj/a.txt"] = "source data"
	remote.files["/srv/proj/b.txt"] = "keep me"
	slot, _, _ := authorityTestSlot(remote)
	local := plugintest.NewFakeOS()
	osys := NewAuthorityOS(slot, local, nil).(*authorityOS)
	err := osys.Copy(context.Background(), "a.txt", "b.txt")
	if err == nil || pluginapi.CodeOf(err) != pluginapi.CodeUnsupported {
		t.Fatalf("copy over an existing destination without a streamer = %v, code %q", err, pluginapi.CodeOf(err))
	}
	if got := remote.files["/srv/proj/b.txt"]; got != "keep me" {
		t.Fatalf("refused copy changed the destination: %q", got)
	}
	if got := remote.files["/srv/proj/a.txt"]; got != "source data" {
		t.Fatalf("refused copy changed the source: %q", got)
	}
	if calls := local.Calls(); len(calls) != 0 {
		t.Fatalf("refused copy touched the local OS: %v", calls)
	}
}

// TestAuthorityOSCopyRootIntoChildRefused: the filesystem root is a prefix of every
// absolute path, including a destination that does not exist, so the usual "source + /"
// test must not let a root tree copy recurse into its own filesystem. The refusal must
// happen before any mkdir/write.
func TestAuthorityOSCopyRootIntoChildRefused(t *testing.T) {
	remote := &fakeMutableFiles{fakeRemoteFiles: &fakeRemoteFiles{
		files: map[string]string{},
		dirs: map[string][]hostos.DirEntry{
			"/":    {{Name: "tmp", Kind: "dir"}},
			"/tmp": {},
		},
	}}
	slot, _, _ := authorityTestSlot(remote)
	local := plugintest.NewFakeOS()
	osys := NewAuthorityOS(slot, local, nil).(*authorityOS)
	err := osys.Copy(context.Background(), "/", "/tmp/root-copy")
	if err == nil || pluginapi.CodeOf(err) != pluginapi.CodeInvalid {
		t.Fatalf("root tree copy into child = %v, code %q", err, pluginapi.CodeOf(err))
	}
	if len(remote.mkdirs) != 0 {
		t.Fatalf("refused root copy created directories: %v", remote.mkdirs)
	}
	if calls := local.Calls(); len(calls) != 0 {
		t.Fatalf("refused root copy touched the local OS: %v", calls)
	}
}
