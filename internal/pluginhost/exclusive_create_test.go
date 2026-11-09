package pluginhost

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/tbxark/gvim/plugin/hostos"
	"github.com/tbxark/gvim/plugin/plugintest"
	"github.com/tbxark/gvim/pluginapi"
)

func exclusiveCodeOf(err error) pluginapi.Code {
	var pe *pluginapi.Error
	if errors.As(err, &pe) {
		return pe.Code
	}
	return ""
}

// TestPluginOSExclusiveCreate: the local plugin OS creates a file exclusively and leaves
// an existing one untouched with a conflict.
func TestPluginOSExclusiveCreate(t *testing.T) {
	osys := newPluginOS(nil)
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	if err := osys.WriteFile(context.Background(), path, []byte("mine"), hostos.WriteOptions{Exclusive: true, Mode: 0o600}); err != nil {
		t.Fatalf("exclusive create: %v", err)
	}
	if got, err := os.ReadFile(path); err != nil || string(got) != "mine" {
		t.Fatalf("file = %q, %v", got, err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %v, %v, want 0600", info, err)
	}
	err = osys.WriteFile(context.Background(), path, []byte("theirs"), hostos.WriteOptions{Exclusive: true})
	if exclusiveCodeOf(err) != pluginapi.CodeConflict {
		t.Fatalf("second exclusive create = %v, want conflict", err)
	}
	if got, _ := os.ReadFile(path); string(got) != "mine" {
		t.Fatalf("a refused create-new changed the file: %q", got)
	}
	for _, opts := range []hostos.WriteOptions{{Exclusive: true, Parents: true}, {Exclusive: true, Append: true}} {
		if err := osys.WriteFile(context.Background(), filepath.Join(dir, "x"), []byte("x"), opts); codeOf(err) != pluginapi.CodeInvalid {
			t.Errorf("WriteFile(%+v) = %v, want invalid", opts, err)
		}
	}
}

// fakeCreatorFiles is a remote file system with the authority FileCreator capability.
type fakeCreatorFiles struct{ *fakeRemoteFiles }

func (f *fakeCreatorFiles) CreateNew(_ context.Context, p string, _ hostos.WriteOptions) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.files[p]; ok {
		return fs.ErrExist
	}
	if _, ok := f.dirs[p]; ok {
		return fs.ErrExist
	}
	f.files[p] = ""
	return nil
}

// CreateNewWithContent is the single-op content capability authorityOS now requires for
// exclusive writes.
func (f *fakeCreatorFiles) CreateNewWithContent(_ context.Context, p string, data []byte, _ hostos.WriteOptions) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.files[p]; ok {
		return fs.ErrExist
	}
	if _, ok := f.dirs[p]; ok {
		return fs.ErrExist
	}
	f.files[p] = string(data)
	return nil
}

// fakeEmptyCreatorFiles has only the empty-file FileCreator surface: authorityOS must
// refuse an exclusive content write instead of falling back to CreateNew+WriteFile.
type fakeEmptyCreatorFiles struct{ *fakeRemoteFiles }

func (f *fakeEmptyCreatorFiles) CreateNew(_ context.Context, p string, _ hostos.WriteOptions) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.files[p]; ok {
		return fs.ErrExist
	}
	f.files[p] = ""
	return nil
}

// TestAuthorityOSExclusiveCreateUsesContentCreator: an exclusive write on a remote
// workspace claims and writes through authority.ContentCreators in one capability call; a
// second create-new is refused with the existing content untouched.
func TestAuthorityOSExclusiveCreateUsesContentCreator(t *testing.T) {
	remote := &fakeCreatorFiles{fakeRemoteFiles: newFakeRemoteFiles()}
	slot, _, _ := authorityTestSlot(remote)
	osys := NewAuthorityOS(slot, plugintest.NewFakeOS(), nil)
	path := "/srv/proj/config.toml"
	if err := osys.WriteFile(context.Background(), path, []byte("mine"), hostos.WriteOptions{Exclusive: true}); err != nil {
		t.Fatalf("exclusive create: %v", err)
	}
	remote.mu.Lock()
	got := remote.files[path]
	remote.mu.Unlock()
	if got != "mine" {
		t.Fatalf("remote file = %q", got)
	}
	err := osys.WriteFile(context.Background(), path, []byte("theirs"), hostos.WriteOptions{Exclusive: true})
	if exclusiveCodeOf(err) != pluginapi.CodeConflict {
		t.Fatalf("second remote create-new = %v, want conflict", err)
	}
	remote.mu.Lock()
	got = remote.files[path]
	remote.mu.Unlock()
	if got != "mine" {
		t.Fatalf("a refused remote create-new changed the file: %q", got)
	}
}

// TestAuthorityOSExclusiveCreateUnsupported: a backend without FileCreator is refused
// explicitly instead of falling back to a racy existence check.
func TestAuthorityOSExclusiveCreateUnsupported(t *testing.T) {
	remote := newFakeRemoteFiles()
	slot, _, _ := authorityTestSlot(remote)
	osys := NewAuthorityOS(slot, plugintest.NewFakeOS(), nil)
	err := osys.WriteFile(context.Background(), "/srv/proj/config.toml", []byte("mine"), hostos.WriteOptions{Exclusive: true})
	var pe *pluginapi.Error
	if !errors.As(err, &pe) || pe.Code != pluginapi.CodeUnsupported {
		t.Fatalf("exclusive create on a backend without FileCreator = %v, want unsupported", err)
	}
}

// TestAuthorityOSExclusiveCreateNoContentCapability: a backend that only has the
// empty-file FileCreator surface must be refused; authorityOS must not call CreateNew and
// then WriteFile, which is the replaced-path race.
func TestAuthorityOSExclusiveCreateNoContentCapability(t *testing.T) {
	remote := &fakeEmptyCreatorFiles{fakeRemoteFiles: newFakeRemoteFiles()}
	slot, _, _ := authorityTestSlot(remote)
	osys := NewAuthorityOS(slot, plugintest.NewFakeOS(), nil)
	path := "/srv/proj/config.toml"
	err := osys.WriteFile(context.Background(), path, []byte("mine"), hostos.WriteOptions{Exclusive: true})
	if pluginapi.CodeOf(err) != pluginapi.CodeUnsupported {
		t.Fatalf("exclusive write without content capability = %v, code %q; want unsupported", err, pluginapi.CodeOf(err))
	}
	remote.mu.Lock()
	_, exists := remote.files[path]
	remote.mu.Unlock()
	if exists {
		t.Fatal("a refused exclusive write created the target (fallback happened)")
	}
}
