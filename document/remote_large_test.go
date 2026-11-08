package document

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"

	"github.com/tbxark/gvim/buffer"
)

// fakeRangeAccess serves a synthetic remote file through document.RangeAccess and
// records every window read.
type fakeRangeAccess struct {
	data []byte
	mu   sync.Mutex
	err  error
	// reads counts RangeAccess calls and bytes fetched.
	reads, bytes int64
}

func (f *fakeRangeAccess) ReadFile(path string, max int64) ([]byte, error) {
	return nil, errors.New("test: whole-file read must not be used for a windowed large file")
}
func (f *fakeRangeAccess) Stat(path string) (AccessStat, error) {
	return AccessStat{Exists: true, Size: int64(len(f.data))}, nil
}
func (f *fakeRangeAccess) WriteFileAtomic(path string, data []byte) error {
	return errors.New("test: a remote large file is read-only")
}
func (f *fakeRangeAccess) ReadRange(ctx context.Context, path string, off, n int64) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.err; err != nil {
		return nil, err
	}
	f.reads++
	if off < 0 || off > int64(len(f.data)) {
		return nil, io.EOF
	}
	end := min(off+n, int64(len(f.data)))
	f.bytes += end - off
	out := append([]byte(nil), f.data[off:end]...)
	if end < off+n {
		return out, io.EOF
	}
	return out, nil
}
func (f *fakeRangeAccess) counts() (reads, bytes int64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.reads, f.bytes
}

// remoteLargeContent builds a UTF-8 file bigger than several windows: n numbered
// lines of roughly lineWidth bytes.
func remoteLargeContent(lines, lineWidth int) []byte {
	var sb strings.Builder
	for i := 0; i < lines; i++ {
		text := fmt.Sprintf("line%08d", i)
		if len(text) < lineWidth {
			text += strings.Repeat("x", lineWidth-len(text))
		}
		sb.WriteString(text)
		sb.WriteByte('\n')
	}
	return []byte(sb.String())
}

func TestRemoteLargeWindowSourceLRUAndCancellation(t *testing.T) {
	t.Parallel()
	access := &fakeRangeAccess{data: remoteLargeContent(600000, 20)} // ~12 MiB, 12 windows
	src, err := newRemoteWindowSource(context.Background(), access, "/remote/big.txt", int64(len(access.data)))
	if err != nil {
		t.Fatalf("newRemoteWindowSource: %v", err)
	}
	defer func() { _ = src.Close() }()
	// The constructor loads window 0; load windows 1..11, which exceeds the cache.
	for i := int64(0); i < 12; i++ {
		if err := src.LoadWindow(context.Background(), i*RemoteWindowSize, RemoteWindowSize); err != nil {
			t.Fatalf("LoadWindow(%d): %v", i, err)
		}
	}
	buf := make([]byte, 8)
	if _, err := src.ReadAt(buf, 0); !errors.Is(err, buffer.ErrSourcePending) {
		t.Fatalf("ReadAt of the oldest window = %v, want ErrSourcePending (LRU eviction)", err)
	}
	// The missing window is recorded for the root.
	off, n, ok := src.TakePendingWindow()
	if !ok || off != 0 || n != RemoteWindowSize {
		t.Fatalf("pending window = %d %d %v, want window 0", off, n, ok)
	}
	// Cancellation reaches the remote read.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := src.LoadWindow(ctx, 0, RemoteWindowSize); err == nil {
		t.Fatal("LoadWindow with a cancelled context = nil")
	}
	// Close releases the source and refuses later loads.
	if err := src.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := src.LoadWindow(context.Background(), 0, RemoteWindowSize); err == nil {
		t.Fatal("LoadWindow after Close = nil")
	}
}

func TestReadRemoteLargeDocumentIsReadOnlyWindowed(t *testing.T) {
	t.Parallel()
	access := &fakeRangeAccess{data: remoteLargeContent(300000, 20)} // ~6 MiB
	d, err := readDocument("/remote/large.txt", loadSpec{
		Access: access,
		LargeConf: largeSpec{
			Threshold: 1,
			Geom: buffer.LargeOptions{
				Chunk: 64 << 10, CachePages: 4, InitialBytes: 4096,
			},
		},
	})
	if err != nil {
		t.Fatalf("readDocument: %v", err)
	}
	defer func() { _ = d.Buffer.Close() }()
	if !d.AsyncLarge() || !d.readOnly || d.large == nil || !d.large.RemoteWindowed {
		t.Fatalf("document = async %v readOnly %v remote %v", d.AsyncLarge(), d.readOnly, d.large != nil && d.large.RemoteWindowed)
	}
	if status := d.OpenStatus(); !strings.Contains(status, "remote large file (read-only, windowed)") {
		t.Fatalf("open status = %q, want the remote read-only reason", status)
	}
	// Whole-file reads were never used; every byte came from RangeAccess windows.
	if reads, _ := access.counts(); reads == 0 {
		t.Fatal("no windowed read was made")
	}
}

func TestReadRemoteLargeDocumentMissLoadRepaint(t *testing.T) {
	t.Parallel()
	access := &fakeRangeAccess{data: remoteLargeContent(600000, 20)} // 12 MiB
	d, err := readDocument("/remote/big.txt", loadSpec{
		Access: access,
		LargeConf: largeSpec{
			Threshold: 1,
			Geom: buffer.LargeOptions{
				Chunk: 64 << 10, CachePages: 4, InitialBytes: 4096,
			},
		},
	})
	if err != nil {
		t.Fatalf("readDocument: %v", err)
	}
	defer func() { _ = d.Buffer.Close() }()
	// Complete the index, loading every window its scan misses the way the root's
	// window commands do.
	ctx := context.Background()
	for range 400 {
		scan := d.Buffer.BeginIndexScan()
		if scan == nil {
			break
		}
		batch, err := scan.Next(ctx, 1<<20)
		scan.Close()
		d.Buffer.ApplyIndex(batch)
		if errors.Is(err, buffer.ErrSourcePending) {
			w, ok := d.TakePendingLargeWindow()
			if !ok {
				t.Fatal("index missed a window without recording it")
			}
			if err := d.LoadLargeWindow(ctx, w); err != nil {
				t.Fatalf("LoadLargeWindow: %v", err)
			}
			continue
		}
		if err != nil {
			t.Fatalf("index scan: %v", err)
		}
	}
	if !d.Index().Complete {
		t.Fatalf("index not complete: %+v", d.Index())
	}
	// The scan loaded 12 windows; the 8-window cache evicted the first line's
	// window, so the paint read misses and returns at once.
	line := d.Buffer.Line(0)
	if line != "" {
		t.Fatalf("Line(0) before loading = %q, want the blank pending placeholder", line)
	}
	w, ok := d.TakePendingLargeWindow()
	if !ok {
		t.Fatal("the paint miss recorded no pending window")
	}
	if err := d.LoadLargeWindow(ctx, w); err != nil {
		t.Fatalf("LoadLargeWindow: %v", err)
	}
	if got := d.Buffer.Line(0); got != "line00000000xxxxxxxx" {
		t.Fatalf("Line(0) after load = %q", got)
	}
	if d.WindowGeneration() == 0 {
		t.Fatal("window generation did not change")
	}
	// A window belonging to another document is refused (no cross-document load).
	if err := d.LoadLargeWindow(ctx, LargeWindow{Path: "/other.txt", Off: 0, N: 1}); err == nil {
		t.Fatal("LoadLargeWindow accepted another document's path")
	}
	// The read-only document refuses edits.
	if !d.ReadOnly() {
		t.Fatal("a remote large document does not report read-only")
	}
}

func TestReadRemoteLargeDocumentNonUTF8FailsClearly(t *testing.T) {
	t.Parallel()
	access := &fakeRangeAccess{data: []byte{0xff, 0xfe, 0x00, 0x41, 0x00, 0x42}} // UTF-16 BOM
	_, err := readDocument("/remote/bin.txt", loadSpec{
		Access: access,
		LargeConf: largeSpec{
			Threshold: 1,
			Geom:      buffer.LargeOptions{Chunk: 8, CachePages: 2, InitialBytes: 8},
		},
	})
	if !errors.Is(err, ErrTooLargeToTranscode) {
		t.Fatalf("readDocument = %v, want ErrTooLargeToTranscode", err)
	}
}
