package pluginhost

import (
	"context"
	"fmt"
	"io/fs"
	"path"
	"strings"
	"sync"
	"testing"

	"github.com/tbxark/gvim/authority"
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

// TestAuthorityOSCopyWithoutStreamerRefusesExistingDestination: without an atomic
// streamed-write surface an existing destination cannot be replaced safely (a hard link
// to the source cannot be identified by realpath, and a plain WriteFile truncates), so
// the copy refuses before writing. The destination and source stay unchanged.
func TestAuthorityOSCopyWithoutStreamerRefusesExistingDestination(t *testing.T) {
	remote := &fakeMutableFiles{fakeRemoteFiles: walkFixture()}
	remote.files["/srv/proj/a.txt"] = "source data"
	remote.files["/srv/proj/b.txt"] = "keep me"
	slot, _, _ := authorityTestSlot(remote)
	local := plugintest.NewFakeOS()
	osys := NewAuthorityOS(slot, local, nil).(*authorityOS)
	err := osys.Copy(context.Background(), "a.txt", "b.txt")
	if err == nil || pluginapi.CodeOf(err) != pluginapi.CodeConflict {
		t.Fatalf("copy over an existing destination = %v, code %q", err, pluginapi.CodeOf(err))
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

// fakeNoStreamFiles records any fallback write so a test can prove that a backend without
// an atomic streamer refuses before creating or writing the destination.
type fakeNoStreamFiles struct {
	*fakeMutableFiles
	mu     sync.Mutex
	writes int
}

func (f *fakeNoStreamFiles) WriteFile(ctx context.Context, p string, data []byte, o hostos.WriteOptions) error {
	f.mu.Lock()
	f.writes++
	f.mu.Unlock()
	return f.fakeMutableFiles.WriteFile(ctx, p, data, o)
}

func (f *fakeNoStreamFiles) writeCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.writes
}

// TestAuthorityOSCopyWithoutStreamerRefusesBeforeWrite: there is no same-handle
// create+write or atomic streamer on this backend, so even a new destination must be
// refused before it is created; a concurrent writer's pre-existing target is untouched.
// This is the regression for the rejected create-then-WriteFile scheme.
func TestAuthorityOSCopyWithoutStreamerRefusesBeforeWrite(t *testing.T) {
	for _, tc := range []struct {
		name string
		dst  string
		pre  string
		want pluginapi.Code
	}{
		{name: "new destination", dst: "new.txt", want: pluginapi.CodeUnsupported},
		{name: "concurrent writer target", dst: "new.txt", pre: "winner", want: pluginapi.CodeConflict},
	} {
		t.Run(tc.name, func(t *testing.T) {
			remote := &fakeMutableFiles{fakeRemoteFiles: walkFixture()}
			remote.files["/srv/proj/a.txt"] = "source data"
			if tc.pre != "" {
				remote.files["/srv/proj/"+tc.dst] = tc.pre
			}
			files := &fakeNoStreamFiles{fakeMutableFiles: remote}
			slot, _, _ := authorityTestSlot(files)
			local := plugintest.NewFakeOS()
			osys := NewAuthorityOS(slot, local, nil).(*authorityOS)
			err := osys.Copy(context.Background(), "a.txt", tc.dst)
			if err == nil || pluginapi.CodeOf(err) != tc.want {
				t.Fatalf("copy without a streamer = %v, code %q; want %q", err, pluginapi.CodeOf(err), tc.want)
			}
			if writes := files.writeCount(); writes != 0 {
				t.Fatalf("refused copy called WriteFile %d times", writes)
			}
			if got, ok := remote.files["/srv/proj/"+tc.dst]; ok && tc.pre == "" {
				t.Fatalf("refused copy created destination %q", got)
			} else if tc.pre != "" && got != tc.pre {
				t.Fatalf("refused copy changed the concurrent target: %q", got)
			}
			if calls := local.Calls(); len(calls) != 0 {
				t.Fatalf("refused copy touched the local OS: %v", calls)
			}
		})
	}
}

// fakeFollowerStreamFiles simulates an atomic stream surface that follows a symlink
// swapped in at the destination: its ordinary WriteStream corrupts the source, while the
// no-follow surface writes the real destination. A copy must choose the no-follow method.
type fakeFollowerStreamFiles struct {
	*fakeStreamFiles
	corrupt string
}

type corruptingWriteStream struct {
	files  *fakeStreamFiles
	target string
}

func (s *corruptingWriteStream) Write(context.Context, []byte) error { return nil }
func (s *corruptingWriteStream) Commit(context.Context) error {
	s.files.files[s.target] = "WRONG"
	s.files.commits++
	return nil
}
func (s *corruptingWriteStream) Abort(context.Context) { s.files.aborts++ }

func (f *fakeFollowerStreamFiles) WriteStream(context.Context, string, hostos.WriteOptions) (authority.WriteStream, error) {
	return &corruptingWriteStream{files: f.fakeStreamFiles, target: f.corrupt}, nil
}

func (f *fakeFollowerStreamFiles) WriteStreamNoFollow(ctx context.Context, p string, o hostos.WriteOptions) (authority.WriteStream, error) {
	return f.fakeStreamFiles.WriteStreamNoFollow(ctx, p, o)
}

func (f *fakeFollowerStreamFiles) WriteStreamNewTarget(ctx context.Context, p string, o hostos.WriteOptions) (authority.WriteStream, error) {
	return f.fakeStreamFiles.WriteStreamNewTarget(ctx, p, o)
}

func (f *fakeFollowerStreamFiles) ProveNewTarget(context.Context, string) error { return nil }

// TestAuthorityOSCopyUsesNoFollowStream: a destination symlink that appears between
// canonicalization and stream-open would be followed by the ordinary stream and corrupt
// the source. Copy must use the no-follow surface; the source stays intact and the copy
// lands at the proven destination.
func TestAuthorityOSCopyUsesNoFollowStream(t *testing.T) {
	remote := &fakeMutableFiles{fakeRemoteFiles: walkFixture()}
	remote.files["/srv/proj/a.txt"] = "source data"
	stream := &fakeStreamFiles{fakeMutableFiles: remote}
	files := &fakeFollowerStreamFiles{fakeStreamFiles: stream, corrupt: "/srv/proj/a.txt"}
	slot, _, _ := authorityTestSlot(files)
	local := plugintest.NewFakeOS()
	osys := NewAuthorityOS(slot, local, nil).(*authorityOS)
	if err := osys.Copy(context.Background(), "a.txt", "b.txt"); err != nil {
		t.Fatal(err)
	}
	if got := remote.files["/srv/proj/a.txt"]; got != "source data" {
		t.Fatalf("copy followed a destination symlink and corrupted the source: %q", got)
	}
	if got := remote.files["/srv/proj/b.txt"]; got != "source data" {
		t.Fatalf("no-follow copy destination = %q", got)
	}
	if calls := local.Calls(); len(calls) != 0 {
		t.Fatalf("copy touched the local OS: %v", calls)
	}
}

// fakeFollowOnlyStreamFiles has the ordinary WriteStreamer but not the copy-safe
// no-follow surface. Copy must return unsupported before any destination work.
type fakeFollowOnlyStreamFiles struct {
	*fakeMutableFiles
	stream *fakeStreamFiles
}

func (f *fakeFollowOnlyStreamFiles) WriteStream(ctx context.Context, p string, o hostos.WriteOptions) (authority.WriteStream, error) {
	return f.stream.WriteStream(ctx, p, o)
}

func TestAuthorityOSCopyRequiresNoFollowStream(t *testing.T) {
	remote := &fakeMutableFiles{fakeRemoteFiles: walkFixture()}
	remote.files["/srv/proj/a.txt"] = "source data"
	files := &fakeFollowOnlyStreamFiles{fakeMutableFiles: remote, stream: &fakeStreamFiles{fakeMutableFiles: remote}}
	slot, _, _ := authorityTestSlot(files)
	local := plugintest.NewFakeOS()
	osys := NewAuthorityOS(slot, local, nil).(*authorityOS)
	err := osys.Copy(context.Background(), "a.txt", "b.txt")
	if err == nil || pluginapi.CodeOf(err) != pluginapi.CodeUnsupported {
		t.Fatalf("copy without no-follow stream = %v, code %q", err, pluginapi.CodeOf(err))
	}
	if _, ok := remote.files["/srv/proj/b.txt"]; ok {
		t.Fatal("unsupported copy created the destination")
	}
	if calls := local.Calls(); len(calls) != 0 {
		t.Fatalf("unsupported copy touched the local OS: %v", calls)
	}
}

// fakeNewTargetFiles has only the new-target link-commit stream (no no-follow), modeling
// the SSH capability. It records streams and can simulate a destination created by another
// writer at Commit.
type fakeNewTargetFiles struct {
	*fakeMutableFiles
	mu          sync.Mutex
	opened      int
	unsupported bool
	race        string
	conflicts   int
}

func (f *fakeNewTargetFiles) ProveNewTarget(context.Context, string) error {
	if f.unsupported {
		return fmt.Errorf("%w: hardlink proof unavailable", authority.ErrUnsupportedFileIO)
	}
	return nil
}

func (f *fakeNewTargetFiles) WriteStreamNewTarget(_ context.Context, p string, _ hostos.WriteOptions) (authority.WriteStream, error) {
	if f.unsupported {
		return nil, fmt.Errorf("%w: hardlink proof unavailable", authority.ErrUnsupportedFileIO)
	}
	f.mu.Lock()
	f.opened++
	f.mu.Unlock()
	return &fakeNewTargetStream{parent: f, path: p}, nil
}

type fakeNewTargetStream struct {
	parent *fakeNewTargetFiles
	path   string
	buf    strings.Builder
	done   bool
}

func (s *fakeNewTargetStream) Write(_ context.Context, p []byte) error {
	if s.done {
		return fs.ErrClosed
	}
	s.buf.Write(p)
	return nil
}

func (s *fakeNewTargetStream) Commit(_ context.Context) error {
	if s.done {
		return fs.ErrClosed
	}
	s.parent.mu.Lock()
	defer s.parent.mu.Unlock()
	if s.parent.race != "" && s.path == s.parent.race {
		s.parent.files[s.path] = "winner"
		s.parent.conflicts++
		s.done = true
		return fmt.Errorf("%s: %w: destination already exists", s.path, fs.ErrExist)
	}
	if _, ok := s.parent.files[s.path]; ok {
		return fmt.Errorf("%s: %w: destination already exists", s.path, fs.ErrExist)
	}
	s.parent.files[s.path] = s.buf.String()
	s.done = true
	return nil
}

func (s *fakeNewTargetStream) Abort(context.Context) { s.done = true }

func (f *fakeNewTargetFiles) openCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.opened
}

func TestAuthorityOSCopyNewTargetCapability(t *testing.T) {
	remote := walkFixture()
	remote.files["/srv/proj/a.txt"] = "payload"
	files := &fakeNewTargetFiles{fakeMutableFiles: &fakeMutableFiles{fakeRemoteFiles: remote}}
	slot, _, _ := authorityTestSlot(files)
	local := plugintest.NewFakeOS()
	osys := NewAuthorityOS(slot, local, nil).(*authorityOS)
	if err := osys.Copy(context.Background(), "a.txt", "b.txt"); err != nil {
		t.Fatal(err)
	}
	if got := remote.files["/srv/proj/b.txt"]; got != "payload" {
		t.Fatalf("new-target copy = %q", got)
	}
	if files.openCount() != 1 {
		t.Fatalf("new-target streams = %d, want one", files.openCount())
	}
	if got := remote.files["/srv/proj/a.txt"]; got != "payload" {
		t.Fatalf("source changed: %q", got)
	}
	if calls := local.Calls(); len(calls) != 0 {
		t.Fatalf("new-target copy touched the local OS: %v", calls)
	}
}

func TestAuthorityOSCopyNewTargetExistingDestinationConflict(t *testing.T) {
	remote := walkFixture()
	remote.files["/srv/proj/a.txt"] = "source"
	remote.files["/srv/proj/b.txt"] = "keep me"
	files := &fakeNewTargetFiles{fakeMutableFiles: &fakeMutableFiles{fakeRemoteFiles: remote}}
	slot, _, _ := authorityTestSlot(files)
	osys := NewAuthorityOS(slot, plugintest.NewFakeOS(), nil).(*authorityOS)
	err := osys.Copy(context.Background(), "a.txt", "b.txt")
	if err == nil || pluginapi.CodeOf(err) != pluginapi.CodeConflict {
		t.Fatalf("new-target copy over existing = %v, code %q", err, pluginapi.CodeOf(err))
	}
	if got := remote.files["/srv/proj/b.txt"]; got != "keep me" {
		t.Fatalf("existing destination changed: %q", got)
	}
	if files.openCount() != 0 {
		t.Fatalf("an existing destination opened %d streams", files.openCount())
	}
}

func TestAuthorityOSCopyNewTargetConcurrentCreatorConflict(t *testing.T) {
	remote := walkFixture()
	remote.files["/srv/proj/a.txt"] = "source"
	files := &fakeNewTargetFiles{fakeMutableFiles: &fakeMutableFiles{fakeRemoteFiles: remote}, race: "/srv/proj/b.txt"}
	slot, _, _ := authorityTestSlot(files)
	osys := NewAuthorityOS(slot, plugintest.NewFakeOS(), nil).(*authorityOS)
	err := osys.Copy(context.Background(), "a.txt", "b.txt")
	if err == nil || pluginapi.CodeOf(err) != pluginapi.CodeConflict {
		t.Fatalf("concurrent new-target copy = %v, code %q", err, pluginapi.CodeOf(err))
	}
	if got := remote.files["/srv/proj/b.txt"]; got != "winner" {
		t.Fatalf("concurrent winner changed: %q", got)
	}
	if got := remote.files["/srv/proj/a.txt"]; got != "source" {
		t.Fatalf("source changed: %q", got)
	}
}

func TestAuthorityOSCopyNewTargetUnsupportedProbe(t *testing.T) {
	remote := walkFixture()
	remote.files["/srv/proj/a.txt"] = "source"
	files := &fakeNewTargetFiles{fakeMutableFiles: &fakeMutableFiles{fakeRemoteFiles: remote}, unsupported: true}
	slot, _, _ := authorityTestSlot(files)
	osys := NewAuthorityOS(slot, plugintest.NewFakeOS(), nil).(*authorityOS)
	err := osys.Copy(context.Background(), "a.txt", "b.txt")
	if err == nil || pluginapi.CodeOf(err) != pluginapi.CodeUnsupported {
		t.Fatalf("unproven new-target copy = %v, code %q", err, pluginapi.CodeOf(err))
	}
	if _, ok := remote.files["/srv/proj/b.txt"]; ok {
		t.Fatal("unproven new-target copy created a destination")
	}
}

func TestAuthorityOSCopyNewTargetTree(t *testing.T) {
	remote := walkFixture()
	remote.files["/srv/proj/src/b.go"] = "bb"
	remote.files["/srv/proj/src/sub/c.go"] = "ccc"
	files := &fakeNewTargetFiles{fakeMutableFiles: &fakeMutableFiles{fakeRemoteFiles: remote}}
	slot, _, _ := authorityTestSlot(files)
	osys := NewAuthorityOS(slot, plugintest.NewFakeOS(), nil).(*authorityOS)
	if err := osys.Copy(context.Background(), "src", "dst"); err != nil {
		t.Fatal(err)
	}
	if remote.files["/srv/proj/dst/b.go"] != "bb" || remote.files["/srv/proj/dst/sub/c.go"] != "ccc" {
		t.Fatalf("new-target tree files = %v", remote.files)
	}
	if files.openCount() != 2 {
		t.Fatalf("new-target streams = %d, want 2", files.openCount())
	}
}

func TestAuthorityOSCopyNewTargetTreeUnsupportedProbe(t *testing.T) {
	remote := walkFixture()
	files := &fakeNewTargetFiles{fakeMutableFiles: &fakeMutableFiles{fakeRemoteFiles: remote}, unsupported: true}
	slot, _, _ := authorityTestSlot(files)
	osys := NewAuthorityOS(slot, plugintest.NewFakeOS(), nil).(*authorityOS)
	err := osys.Copy(context.Background(), "src", "dst")
	if err == nil || pluginapi.CodeOf(err) != pluginapi.CodeUnsupported {
		t.Fatalf("unprovable new-target tree = %v, code %q", err, pluginapi.CodeOf(err))
	}
	if len(files.mkdirs) != 0 {
		t.Fatalf("unprovable tree created directories: %v", files.mkdirs)
	}
	if _, ok := remote.files["/srv/proj/dst/b.go"]; ok {
		t.Fatal("unprovable tree wrote a destination file")
	}
}

// fakeNoNewTargetStreamFiles has the ordinary and no-follow atomic replacement streams,
// but not the new-target no-clobber stream required by gvim.fs.copy.
type fakeNoNewTargetStreamFiles struct {
	*fakeMutableFiles
	stream *fakeStreamFiles
}

func (f *fakeNoNewTargetStreamFiles) WriteStream(ctx context.Context, p string, o hostos.WriteOptions) (authority.WriteStream, error) {
	return f.stream.WriteStream(ctx, p, o)
}

func (f *fakeNoNewTargetStreamFiles) WriteStreamNoFollow(ctx context.Context, p string, o hostos.WriteOptions) (authority.WriteStream, error) {
	return f.stream.WriteStreamNoFollow(ctx, p, o)
}

// TestAuthorityOSCopyRefusesNoFollowOnlyStream: a NoFollow/ordinary WriteStream surface
// atomically replaces an existing target, so it is not a no-replace copy capability. Copy
// must return unsupported and touch nothing instead of using that check-then-write fallback.
func TestAuthorityOSCopyRefusesNoFollowOnlyStream(t *testing.T) {
	remote := walkFixture()
	remote.files["/srv/proj/a.txt"] = "source data"
	files := &fakeNoNewTargetStreamFiles{
		fakeMutableFiles: &fakeMutableFiles{fakeRemoteFiles: remote},
		stream:           &fakeStreamFiles{fakeMutableFiles: &fakeMutableFiles{fakeRemoteFiles: remote}},
	}
	slot, _, _ := authorityTestSlot(files)
	osys := NewAuthorityOS(slot, plugintest.NewFakeOS(), nil).(*authorityOS)
	err := osys.Copy(context.Background(), "a.txt", "b.txt")
	if err == nil || pluginapi.CodeOf(err) != pluginapi.CodeUnsupported {
		t.Fatalf("copy with only NoFollow stream = %v, code %q", err, pluginapi.CodeOf(err))
	}
	if _, ok := remote.files["/srv/proj/b.txt"]; ok {
		t.Fatal("a NoFollow-only copy created the destination")
	}
	if got := remote.files["/srv/proj/a.txt"]; got != "source data" {
		t.Fatalf("source changed: %q", got)
	}
}
