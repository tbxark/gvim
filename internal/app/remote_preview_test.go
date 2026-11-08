package app

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"

	"github.com/tbxark/gvim/authority"
	"github.com/tbxark/gvim/authority/authoritytest"
)

// previewFS is the in-memory remote file system of the preview tests: ReadFile enforces the
// authority's read cap ("larger than N"), so the retry/truncation path is exercised. It
// embeds UnsupportedFiles so the remaining FileSystem surface stays a refusal.
type previewFS struct {
	authority.UnsupportedFiles
	mu    sync.Mutex
	data  []byte
	err   error
	reads []int64
}

func (f *previewFS) ReadFile(_ context.Context, _ string, max int64) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reads = append(f.reads, max)
	if f.err != nil {
		return nil, f.err
	}
	if int64(len(f.data)) > max {
		return nil, errors.New("preview.txt: larger than N bytes")
	}
	return append([]byte(nil), f.data...), nil
}

// ReadRange returns the windowed head, with io.EOF on a short answer, so the preview's
// exact-head and truncation path is exercised.
func (f *previewFS) ReadRange(_ context.Context, _ string, off, n int64) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reads = append(f.reads, n)
	if f.err != nil {
		return nil, f.err
	}
	if off >= int64(len(f.data)) {
		return nil, io.EOF
	}
	end := min(off+n, int64(len(f.data)))
	out := append([]byte(nil), f.data[off:end]...)
	if end < int64(len(f.data)) {
		return out, nil
	}
	return out, io.EOF
}

// noRangeFS is a backend without the windowed primitive: its ReadRange refuses and only
// the bounded whole-file read remains.
type noRangeFS struct {
	authority.UnsupportedFiles
	data []byte
}

func (f *noRangeFS) ReadFile(context.Context, string, int64) ([]byte, error) {
	if len(f.data) > 1<<10 {
		return nil, errors.New("large.txt: larger than 1024 bytes")
	}
	return append([]byte(nil), f.data...), nil
}

// TestRemotePreviewFallbackWithoutReadRange checks a backend without the windowed read
// shows a complete small file and reports a too-large file as an error (never a silently
// truncated head presented as complete).
func TestRemotePreviewFallbackWithoutReadRange(t *testing.T) {
	small := &noRangeFS{data: []byte("hello\n")}
	data, truncated, err := newRemotePreviewer(previewSlot(small, true)).ReadHead("/remote/w/small.txt", 16)
	if err != nil || string(data) != "hello\n" || truncated {
		t.Fatalf("fallback head = %q truncated %v err %v", data, truncated, err)
	}
	large := &noRangeFS{data: bytes.Repeat([]byte("x"), 2000)}
	if _, _, err := newRemotePreviewer(previewSlot(large, true)).ReadHead("/remote/w/large.txt", 16); err == nil {
		t.Fatal("a too-large file without ReadRange reported no error")
	}
}

func (f *previewFS) readSizes() []int64 {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]int64(nil), f.reads...)
}

func previewSlot(fs authority.FileSystem, connected bool) *authoritytest.Fake {
	slot := authoritytest.New()
	slot.FileSystem = fs
	slot.ConnectedState = connected
	return slot
}

// TestRemotePreviewReaderReadsTheRemoteHead checks the remote preview reads through the
// authority, halves the read window on a too-large file and reports truncation honestly, and
// keeps a complete small file marked complete.
func TestRemotePreviewReaderReadsTheRemoteHead(t *testing.T) {
	big := &previewFS{data: bytes.Repeat([]byte("x"), 100)}
	p := newRemotePreviewer(previewSlot(big, true))
	data, truncated, err := p.ReadHead("/remote/w/big.txt", 16)
	if err != nil {
		t.Fatalf("ReadHead: %v", err)
	}
	if string(data) != strings.Repeat("x", 16) || !truncated {
		t.Fatalf("head = %d bytes, truncated %v; want the 16-byte head and true", len(data), truncated)
	}
	if sizes := big.readSizes(); len(sizes) != 1 || sizes[0] != 17 {
		t.Fatalf("read windows = %v, want the 17-byte range (one past the head)", sizes)
	}
	small := &previewFS{data: []byte("hello\n")}
	data, truncated, err = newRemotePreviewer(previewSlot(small, true)).ReadHead("/remote/w/small.txt", 16)
	if err != nil || string(data) != "hello\n" || truncated {
		t.Fatalf("small head = %q truncated %v err %v", data, truncated, err)
	}
}

// TestRemotePreviewReaderBoundsAndCap checks the read is capped at 64 KiB, a read error is
// reported, and a disconnected authority never reads (nor falls back locally).
func TestRemotePreviewReaderBoundsAndCap(t *testing.T) {
	fs := &previewFS{data: []byte("x")}
	p := newRemotePreviewer(previewSlot(fs, true))
	if _, _, err := p.ReadHead("/remote/w/a.txt", 1<<20); err != nil {
		t.Fatal(err)
	}
	if sizes := fs.readSizes(); len(sizes) != 1 || sizes[0] != remotePreviewMaxBytes+1 {
		t.Fatalf("read sizes = %v, want one past the %d-byte cap", sizes, remotePreviewMaxBytes)
	}
	failing := &previewFS{err: errors.New("permission denied")}
	if _, _, err := newRemotePreviewer(previewSlot(failing, true)).ReadHead("/remote/w/a.txt", 16); err == nil {
		t.Fatal("a failed read reported no error")
	}
	disconnected := &previewFS{data: []byte("secret")}
	if _, _, err := newRemotePreviewer(previewSlot(disconnected, false)).ReadHead("/remote/w/a.txt", 16); err == nil || !strings.Contains(err.Error(), "disconnected") {
		t.Fatalf("disconnected error = %v", err)
	}
	if sizes := disconnected.readSizes(); len(sizes) != 0 {
		t.Fatalf("a disconnected preview read %v", sizes)
	}
}

// TestRemotePreviewNilSlot checks a preview with no slot is an error, never a panic.
func TestRemotePreviewNilSlot(t *testing.T) {
	if _, _, err := newRemotePreviewer(nil).ReadHead("/x", 16); err == nil {
		t.Fatal("nil slot reported no error")
	}
}
