package app

import (
	"context"
	"errors"
	"io/fs"
	"strings"
	"testing"

	"github.com/tbxark/gvim/authority"
	"github.com/tbxark/gvim/authority/authoritytest"
	"github.com/tbxark/gvim/plugin/hostos"
)

type markerProbeFS struct {
	*fakeWatchFS
	exists map[string]bool
	calls  []string
	err    error
}

func (f *markerProbeFS) Stat(ctx context.Context, p string) (hostos.FileStat, error) {
	if err := ctx.Err(); err != nil {
		return hostos.FileStat{}, err
	}
	f.calls = append(f.calls, p)
	if f.err != nil {
		return hostos.FileStat{}, f.err
	}
	if f.exists[p] {
		return hostos.FileStat{Kind: "file"}, nil
	}
	if strings.HasSuffix(p, "/broken-link") {
		return hostos.FileStat{Kind: "symlink"}, nil
	}
	return hostos.FileStat{}, fs.ErrNotExist
}

func TestRemoteMarkerProberUsesAuthorityStatOnly(t *testing.T) {
	files := &markerProbeFS{fakeWatchFS: &fakeWatchFS{}, exists: map[string]bool{
		"/remote/proj/.envrc": true,
	}}
	slot := authoritytest.New()
	slot.FileSystem = files
	prober := newRemoteMarkerProber(func() authority.Slot { return slot })
	got, err := prober(context.Background(), "/remote/proj", []string{".envrc", ".venv/bin/python", "broken-link"})
	if err != nil {
		t.Fatal(err)
	}
	if !got[".envrc"] || got[".venv/bin/python"] || got["broken-link"] {
		t.Fatalf("exists = %v (a broken symlink must not count as present)", got)
	}
	wantCalls := "/remote/proj/.envrc,/remote/proj/.venv/bin/python,/remote/proj/broken-link"
	if strings.Join(files.calls, ",") != wantCalls {
		t.Fatalf("Stat calls = %v, want %s", files.calls, wantCalls)
	}
	// The prober never touches the one-shot surface (no shell execution).
	if _, ok := slot.OneShotter.(authority.UnsupportedProcs); !ok {
		t.Fatalf("the fake one-shot was replaced: %T", slot.OneShotter)
	}
}

func TestRemoteMarkerProberErrorsAreClear(t *testing.T) {
	files := &markerProbeFS{fakeWatchFS: &fakeWatchFS{}, err: fs.ErrPermission}
	slot := authoritytest.New()
	slot.FileSystem = files
	prober := newRemoteMarkerProber(func() authority.Slot { return slot })
	if _, err := prober(context.Background(), "/remote/proj", []string{".envrc"}); err == nil || !strings.Contains(err.Error(), ".envrc") {
		t.Fatalf("permission error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := prober(ctx, "/remote/proj", []string{".envrc"}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled prober = %v", err)
	}
	if _, err := newRemoteMarkerProber(func() authority.Slot { return nil })(context.Background(), "/r", []string{"a"}); err == nil {
		t.Fatal("a nil slot produced no error")
	}
}
