package pluginhost

import (
	"context"
	"io/fs"
	"strings"
	"testing"

	"github.com/tbxark/gvim/plugin/plugintest"
	"github.com/tbxark/gvim/pluginapi"
)

// fakeRealPathFiles is the remote file surface with the optional RealPather.
type fakeRealPathFiles struct {
	*fakeRemoteFiles
	real  map[string]string
	calls []string
	err   error
}

func (f *fakeRealPathFiles) RealPath(_ context.Context, p string) (string, error) {
	f.calls = append(f.calls, p)
	if f.err != nil {
		return "", f.err
	}
	if real, ok := f.real[p]; ok {
		return real, nil
	}
	return "", pluginapi.Errf(pluginapi.CodeNotFound, "%s: no such path", p)
}

func TestAuthorityOSRealPathRemote(t *testing.T) {
	files := &fakeRealPathFiles{fakeRemoteFiles: newFakeRemoteFiles(), real: map[string]string{
		"/srv/proj/link": "/srv/proj/target",
		"/srv/proj/ ":    "/srv/proj/space-target",
	}}
	slot, _, _ := authorityTestSlot(files)
	local := plugintest.NewFakeOS()
	osys := NewAuthorityOS(slot, local, nil)
	got, err := osys.RealPath(context.Background(), "link")
	if err != nil {
		t.Fatal(err)
	}
	if got != "/srv/proj/target" {
		t.Fatalf("realpath = %q", got)
	}
	if len(files.calls) != 1 || files.calls[0] != "/srv/proj/link" {
		t.Fatalf("remote realpath calls = %v (relative paths must resolve against the remote root)", files.calls)
	}
	if calls := local.Calls(); len(calls) != 0 {
		t.Fatalf("realpath touched the local OS: %v", calls)
	}
	// A whitespace-only path is a legal file name and must resolve relative to the
	// workspace root unchanged (no TrimSpace in the bridge).
	if got, err := osys.RealPath(context.Background(), " "); err != nil || got != "/srv/proj/space-target" {
		t.Fatalf("space realpath = %q, %v", got, err)
	}
	// Missing path maps to the plugin API's not_found code.
	if _, err := osys.RealPath(context.Background(), "missing"); pluginapi.CodeOf(err) != pluginapi.CodeNotFound {
		t.Fatalf("missing realpath = %v, code %q", err, pluginapi.CodeOf(err))
	}
	// Cancellation maps to cancelled, before the authority call.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := osys.RealPath(ctx, "link"); pluginapi.CodeOf(err) != pluginapi.CodeCancelled {
		t.Fatalf("cancelled realpath = %v, code %q", err, pluginapi.CodeOf(err))
	}
}

func TestAuthorityOSRealPathUnsupportedWithoutCapability(t *testing.T) {
	// The plain fake remote file surface has no RealPather: a clear unsupported error,
	// not a local realpath.
	slot, _, _ := authorityTestSlot(newFakeRemoteFiles())
	local := plugintest.NewFakeOS()
	osys := NewAuthorityOS(slot, local, nil)
	_, err := osys.RealPath(context.Background(), "link")
	if err == nil || pluginapi.CodeOf(err) != pluginapi.CodeUnsupported {
		t.Fatalf("realpath without capability = %v, code %q", err, pluginapi.CodeOf(err))
	}
	if calls := local.Calls(); len(calls) != 0 {
		t.Fatalf("unsupported realpath touched the local OS: %v", calls)
	}
}

func TestAuthorityOSRealPathMapsBackendError(t *testing.T) {
	files := &fakeRealPathFiles{fakeRemoteFiles: newFakeRemoteFiles(), err: fs.ErrNotExist}
	slot, _, _ := authorityTestSlot(files)
	osys := NewAuthorityOS(slot, plugintest.NewFakeOS(), nil)
	_, err := osys.RealPath(context.Background(), "x")
	if !strings.Contains(err.Error(), "no such") && pluginapi.CodeOf(err) != pluginapi.CodeNotFound {
		t.Fatalf("backend realpath error = %v, code %q", err, pluginapi.CodeOf(err))
	}
}
