package pluginhost

import (
	"context"
	"strings"
	"testing"

	"github.com/tbxark/gvim/plugin/hostos"
	"github.com/tbxark/gvim/plugin/plugintest"
	"github.com/tbxark/gvim/pluginapi"
)

// walkFixture builds a remote tree with hidden entries, a skipped directory, nested
// directories and a symlink.
func walkFixture() *fakeRemoteFiles {
	remote := newFakeRemoteFiles()
	remote.dirs = map[string][]hostos.DirEntry{
		"/srv/proj": {
			{Name: "z.txt", Kind: "file", Size: 1},
			{Name: "src", Kind: "dir"},
			{Name: ".hidden", Kind: "file"},
			{Name: "node_modules", Kind: "dir"},
			{Name: "link", Kind: "symlink"},
			{Name: "a.txt", Kind: "file"},
		},
		"/srv/proj/src": {
			{Name: "sub", Kind: "dir"},
			{Name: "b.go", Kind: "file", Size: 2},
		},
		"/srv/proj/src/sub": {
			{Name: "c.go", Kind: "file", Size: 3},
		},
		"/srv/proj/node_modules": {
			{Name: "ignored.js", Kind: "file"},
		},
	}
	remote.links = map[string]string{"/srv/proj/link": "a.txt"}
	return remote
}

func authorityOSFor(t *testing.T, files *fakeMutableFiles) (*authorityOS, *plugintest.FakeOS, *fakeMutableFiles) {
	t.Helper()
	slot, _, _ := authorityTestSlot(files)
	local := plugintest.NewFakeOS()
	osys := NewAuthorityOS(slot, local, nil).(*authorityOS)
	return osys, local, files
}

func TestAuthorityOSWalkRemote(t *testing.T) {
	remote := &fakeMutableFiles{fakeRemoteFiles: walkFixture()}
	osys, local, _ := authorityOSFor(t, remote)
	ctx := context.Background()

	res, err := osys.Walk(ctx, ".", hostos.WalkOptions{SkipDirs: []string{"node_modules"}})
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, e := range res.Entries {
		got = append(got, e.Rel)
		if !strings.HasPrefix(e.Path, "/srv/proj/") {
			t.Fatalf("entry path = %q; want the remote absolute path", e.Path)
		}
	}
	want := []string{"a.txt", "link", "src", "src/b.go", "src/sub", "src/sub/c.go", "z.txt"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("walk = %v, want %v", got, want)
	}
	for _, e := range res.Entries {
		if e.Rel == "src/b.go" && (e.Size != 2 || e.Path != "/srv/proj/src/b.go") {
			t.Fatalf("src/b.go = %+v", e)
		}
	}

	// Hidden entries are opt-in; a depth cap is entries-only, no descent beyond it.
	res, err = osys.Walk(ctx, ".", hostos.WalkOptions{Hidden: true, SkipDirs: []string{"node_modules"}})
	if err != nil || len(res.Entries) == 0 || res.Entries[0].Rel != ".hidden" {
		t.Fatalf("hidden walk = %+v, %v", res, err)
	}
	res, err = osys.Walk(ctx, ".", hostos.WalkOptions{SkipDirs: []string{"node_modules"}, MaxDepth: 1})
	if err != nil {
		t.Fatal(err)
	}
	got = nil
	for _, e := range res.Entries {
		got = append(got, e.Rel)
	}
	if strings.Join(got, ",") != "a.txt,link,src,z.txt" {
		t.Fatalf("max-depth walk = %v", got)
	}

	// The entry cap truncates instead of returning an unbounded list.
	res, err = osys.Walk(ctx, ".", hostos.WalkOptions{MaxEntries: 2})
	if err != nil || len(res.Entries) != 2 || !res.Truncated {
		t.Fatalf("capped walk = %+v, %v", res, err)
	}

	// Cancellation is checked and reported with the plugin cancelled code.
	cancelCtx, cancel := context.WithCancel(ctx)
	remote.beforeReadDir = func(string) { cancel() }
	_, err = osys.Walk(cancelCtx, ".", hostos.WalkOptions{})
	remote.beforeReadDir = nil
	if err == nil || pluginapi.CodeOf(err) != pluginapi.CodeCancelled {
		t.Fatalf("cancelled walk error = %v, code %q", err, pluginapi.CodeOf(err))
	}

	// Nothing went to the local machine.
	if calls := local.Calls(); len(calls) != 0 {
		t.Fatalf("walk touched the local OS: %v", calls)
	}
}

func TestAuthorityOSWalkRootError(t *testing.T) {
	remote := newFakeRemoteFiles()
	osys, local, _ := authorityOSFor(t, &fakeMutableFiles{fakeRemoteFiles: remote})
	if _, err := osys.Walk(context.Background(), "missing", hostos.WalkOptions{}); err == nil || pluginapi.CodeOf(err) != pluginapi.CodeNotFound {
		t.Fatalf("missing-root walk error = %v, code %q", err, pluginapi.CodeOf(err))
	}
	if calls := local.Calls(); len(calls) != 0 {
		t.Fatalf("walk touched the local OS: %v", calls)
	}
}

func TestAuthorityOSCopyFileRemote(t *testing.T) {
	remote := walkFixture()
	remote.files["/srv/proj/a.txt"] = "payload"
	stream := &fakeStreamFiles{fakeMutableFiles: &fakeMutableFiles{fakeRemoteFiles: remote}}
	slot, _, _ := authorityTestSlot(stream)
	local := plugintest.NewFakeOS()
	osys := NewAuthorityOS(slot, local, nil)
	if err := osys.Copy(context.Background(), "a.txt", "b.txt"); err != nil {
		t.Fatal(err)
	}
	if got := stream.files["/srv/proj/b.txt"]; got != "payload" {
		t.Fatalf("copied file = %q", got)
	}
	stream.mu.Lock()
	commits := stream.commits
	stream.mu.Unlock()
	if commits != 1 {
		t.Fatalf("streamed file commits = %d, want 1", commits)
	}
	if calls := local.Calls(); len(calls) != 0 {
		t.Fatalf("copy touched the local OS: %v", calls)
	}

	// A destination directory is a conflict, never a recursive overwrite.
	if err := osys.Copy(context.Background(), "a.txt", "src"); err == nil || pluginapi.CodeOf(err) != pluginapi.CodeConflict {
		t.Fatalf("copy onto a directory = %v, code %q", err, pluginapi.CodeOf(err))
	}
	// A symlink source is not a regular file.
	if err := osys.Copy(context.Background(), "link", "link-copy"); err == nil || pluginapi.CodeOf(err) != pluginapi.CodeInvalid {
		t.Fatalf("symlink copy = %v, code %q", err, pluginapi.CodeOf(err))
	}
	if calls := local.Calls(); len(calls) != 0 {
		t.Fatalf("copy touched the local OS: %v", calls)
	}
}

func TestAuthorityOSCopyTreeRemote(t *testing.T) {
	remote := walkFixture()
	remote.files["/srv/proj/src/b.go"] = "bb"
	remote.files["/srv/proj/src/sub/c.go"] = "ccc"
	stream := &fakeStreamFiles{fakeMutableFiles: &fakeMutableFiles{fakeRemoteFiles: remote}}
	slot, _, _ := authorityTestSlot(stream)
	local := plugintest.NewFakeOS()
	osys := NewAuthorityOS(slot, local, nil)
	ctx := context.Background()
	if err := osys.Copy(ctx, "src", "dst"); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(stream.mkdirs, ","); got != "/srv/proj,/srv/proj/dst/sub" {
		t.Fatalf("copied dirs = %q", got)
	}
	if stream.files["/srv/proj/dst/b.go"] != "bb" || stream.files["/srv/proj/dst/sub/c.go"] != "ccc" {
		t.Fatalf("copied files = %v", stream.files)
	}
	if calls := local.Calls(); len(calls) != 0 {
		t.Fatalf("tree copy touched the local OS: %v", calls)
	}
	// Destination must not exist, and a copy into itself is refused.
	if err := osys.Copy(ctx, "src", "src"); err == nil || pluginapi.CodeOf(err) != pluginapi.CodeInvalid {
		t.Fatalf("copy into itself = %v, code %q", err, pluginapi.CodeOf(err))
	}
	if err := osys.Copy(ctx, "src", "src/sub"); err == nil || pluginapi.CodeOf(err) != pluginapi.CodeInvalid {
		t.Fatalf("copy into child = %v, code %q", err, pluginapi.CodeOf(err))
	}
}

func TestAuthorityOSCopyTreeSymlinkRefused(t *testing.T) {
	remote := &fakeMutableFiles{fakeRemoteFiles: walkFixture()}
	remote.dirs["/srv/proj/src"] = append(remote.dirs["/srv/proj/src"], hostos.DirEntry{Name: "link", Kind: "symlink"})
	osys, local, mutable := authorityOSFor(t, remote)
	err := osys.Copy(context.Background(), "src", "dst")
	if err == nil || pluginapi.CodeOf(err) != pluginapi.CodeUnsupported {
		t.Fatalf("symlink tree copy = %v, code %q", err, pluginapi.CodeOf(err))
	}
	if len(mutable.mkdirs) != 0 {
		t.Fatalf("a refused symlink copy created directories: %v", mutable.mkdirs)
	}
	if calls := local.Calls(); len(calls) != 0 {
		t.Fatalf("tree copy touched the local OS: %v", calls)
	}
}

func TestAuthorityOSCopyCancellation(t *testing.T) {
	remote := &fakeMutableFiles{fakeRemoteFiles: walkFixture()}
	osys, local, mutable := authorityOSFor(t, remote)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := osys.Copy(ctx, "src", "dst"); err == nil || pluginapi.CodeOf(err) != pluginapi.CodeCancelled {
		t.Fatalf("cancelled copy = %v, code %q", err, pluginapi.CodeOf(err))
	}
	if len(mutable.mkdirs) != 0 {
		t.Fatalf("a cancelled copy created directories: %v", mutable.mkdirs)
	}
	if calls := local.Calls(); len(calls) != 0 {
		t.Fatalf("cancelled copy touched the local OS: %v", calls)
	}
}
