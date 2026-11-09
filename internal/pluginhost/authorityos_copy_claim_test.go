package pluginhost

import (
	"context"
	"io/fs"
	"strings"
	"sync"
	"testing"

	"github.com/tbxark/gvim/authority"
	"github.com/tbxark/gvim/plugin/hostos"
	"github.com/tbxark/gvim/plugin/plugintest"
	"github.com/tbxark/gvim/pluginapi"
)

// fakeAtomicDirFiles adds the authority's atomic directory claim to the mutable fake.
// MkdirNew claims the name under a lock: a second claim fails with fs.ErrExist, which is
// the semantics the plugin copy relies on to close the target-check race.
type fakeAtomicDirFiles struct {
	*fakeMutableFiles
	mu     sync.Mutex
	claims []string
	raceOn string
}

func (f *fakeAtomicDirFiles) WriteStream(ctx context.Context, p string, o hostos.WriteOptions) (authority.WriteStream, error) {
	// A safe tree copy needs the atomic stream surface in addition to the atomic claim.
	return (&fakeStreamFiles{fakeMutableFiles: f.fakeMutableFiles}).WriteStream(ctx, p, o)
}

func (f *fakeAtomicDirFiles) WriteStreamNoFollow(ctx context.Context, p string, o hostos.WriteOptions) (authority.WriteStream, error) {
	return (&fakeStreamFiles{fakeMutableFiles: f.fakeMutableFiles}).WriteStreamNoFollow(ctx, p, o)
}

func (f *fakeAtomicDirFiles) MkdirNew(ctx context.Context, p string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.claims = append(f.claims, p)
	f.fakeRemoteFiles.mu.Lock()
	defer f.fakeRemoteFiles.mu.Unlock()
	if f.raceOn == p {
		// A concurrent creator won the name after the copy's Stat check.
		if _, ok := f.dirs[p]; !ok {
			f.dirs[p] = nil
		}
		return &fs.PathError{Op: "mkdir", Path: p, Err: fs.ErrExist}
	}
	if _, ok := f.dirs[p]; ok {
		return &fs.PathError{Op: "mkdir", Path: p, Err: fs.ErrExist}
	}
	f.dirs[p] = nil
	return nil
}

func copyTreeFixture() *fakeAtomicDirFiles {
	remote := walkFixture()
	remote.files["/srv/proj/src/b.go"] = "bb"
	remote.files["/srv/proj/src/sub/c.go"] = "ccc"
	return &fakeAtomicDirFiles{fakeMutableFiles: &fakeMutableFiles{fakeRemoteFiles: remote}}
}

func authorityOSForAtomic(t *testing.T, files *fakeAtomicDirFiles) (*authorityOS, *plugintest.FakeOS) {
	t.Helper()
	slot, _, _ := authorityTestSlot(files)
	local := plugintest.NewFakeOS()
	return NewAuthorityOS(slot, local, nil).(*authorityOS), local
}

func TestAuthorityOSCopyTreeAtomicClaimRefusesConcurrentDestination(t *testing.T) {
	files := copyTreeFixture()
	files.raceOn = "/srv/proj/dst"
	osys, local := authorityOSForAtomic(t, files)
	err := osys.Copy(context.Background(), "src", "dst")
	if err == nil || pluginapi.CodeOf(err) != pluginapi.CodeConflict || !strings.Contains(err.Error(), "atomic no-overwrite claim") {
		t.Fatalf("racy tree copy = %v, code %q; want an atomic-claim conflict", err, pluginapi.CodeOf(err))
	}
	for name := range files.files {
		if strings.HasPrefix(name, "/srv/proj/dst/") {
			t.Fatalf("a refused racy tree copy wrote into the concurrent destination: %q", name)
		}
	}
	if len(files.claims) != 1 || files.claims[0] != "/srv/proj/dst" {
		t.Fatalf("claims = %v", files.claims)
	}
	if calls := local.Calls(); len(calls) != 0 {
		t.Fatalf("racy copy touched the local OS: %v", calls)
	}
}

func TestAuthorityOSCopyTreeConcurrentSameDestination(t *testing.T) {
	files := copyTreeFixture()
	osys, _ := authorityOSForAtomic(t, files)
	errs := make(chan error, 2)
	for i := 0; i < 2; i++ {
		go func() { errs <- osys.Copy(context.Background(), "src", "dst") }()
	}
	var ok, conflict int
	for i := 0; i < 2; i++ {
		err := <-errs
		switch {
		case err == nil:
			ok++
		case pluginapi.CodeOf(err) == pluginapi.CodeConflict:
			conflict++
		default:
			t.Fatalf("concurrent copy error = %v, code %q", err, pluginapi.CodeOf(err))
		}
	}
	if ok != 1 || conflict != 1 {
		t.Fatalf("concurrent copies: %d succeeded, %d conflicted; want exactly one winner", ok, conflict)
	}
	if files.files["/srv/proj/dst/b.go"] != "bb" || files.files["/srv/proj/dst/sub/c.go"] != "ccc" {
		t.Fatalf("winner's tree = %v", files.files)
	}
}

func TestAuthorityOSCopyTreeCancelledBeforeClaimLeavesNoDestination(t *testing.T) {
	files := copyTreeFixture()
	osys, _ := authorityOSForAtomic(t, files)
	ctx, cancel := context.WithCancel(context.Background())
	files.beforeReadDir = func(string) { cancel() }
	err := osys.Copy(ctx, "src", "dst")
	if pluginapi.CodeOf(err) != pluginapi.CodeCancelled {
		t.Fatalf("cancelled tree copy = %v, code %q", err, pluginapi.CodeOf(err))
	}
	files.mu.Lock()
	claims := append([]string(nil), files.claims...)
	files.mu.Unlock()
	if len(claims) != 0 {
		t.Fatalf("a cancelled copy claimed the destination: %v", claims)
	}
}
