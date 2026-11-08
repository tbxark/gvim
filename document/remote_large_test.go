package document

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/tbxark/gvim/buffer"
)

// fakeRangeAccess serves a synthetic remote file through document.RangeAccess and
// records every window read.
type fakeRangeAccess struct {
	data []byte
	mu   sync.Mutex
	err  error
	// reads counts RangeAccess calls and bytes fetched; writes records the atomic
	// writes a save makes and the exact bytes written.
	reads, bytes int64
	writes       int
	written      []byte
	mtime        time.Time
}

func (f *fakeRangeAccess) ReadFile(path string, max int64) ([]byte, error) {
	return nil, errors.New("test: whole-file read must not be used for a windowed large file")
}
func (f *fakeRangeAccess) Stat(path string) (AccessStat, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return AccessStat{Exists: true, Size: int64(len(f.data)), Mtime: f.mtime}, nil
}
func (f *fakeRangeAccess) WriteFileAtomic(path string, data []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return f.err
	}
	f.data = append([]byte(nil), data...)
	f.mtime = f.mtime.Add(time.Second)
	f.writes++
	f.written = append([]byte(nil), data...)
	return nil
}
func (f *fakeRangeAccess) writtenBytes() []byte {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]byte(nil), f.written...)
}
func (f *fakeRangeAccess) writeCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.writes
}
func (f *fakeRangeAccess) change(data []byte) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.data = append([]byte(nil), data...)
	f.mtime = f.mtime.Add(time.Second)
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

func TestReadRemoteLargeDocumentWindowedEditable(t *testing.T) {
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
	if !d.AsyncLarge() || d.readOnly || d.large == nil || !d.large.RemoteWindowed {
		t.Fatalf("document = async %v readOnly %v remote %v", d.AsyncLarge(), d.readOnly, d.large != nil && d.large.RemoteWindowed)
	}
	if status := d.OpenStatus(); !strings.Contains(status, "remote large file (windowed)") || strings.Contains(status, "read-only") {
		t.Fatalf("open status = %q, want the editable windowed reason", status)
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
	// The windowed document is editable; the overlay edit is what a save merges.
	if d.ReadOnly() {
		t.Fatal("a remote large document reports read-only")
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

// remoteLargeSaveContent builds numbered lines; edits maps a line number to a prefix
// inserted before that line, so the same helper builds the expected saved file.
func remoteLargeSaveContent(n int, edits map[int]string) []byte {
	var sb strings.Builder
	for i := 0; i < n; i++ {
		if prefix, ok := edits[i]; ok {
			sb.WriteString(prefix)
		}
		fmt.Fprintf(&sb, "line%06d\n", i)
	}
	return []byte(sb.String())
}

// completeRemoteLargeIndex builds the line index, loading every window the scan
// misses the way the root's window commands do.
func completeRemoteLargeIndex(t *testing.T, d *Document) {
	t.Helper()
	ctx := context.Background()
	for range 2000 {
		scan := d.Buffer.BeginIndexScan()
		if scan == nil {
			return
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
	t.Fatal("remote index did not complete")
}

// ensureRemoteLine loads the window of a line, the way scrolling it into view does,
// because an overlay edit needs the line bytes.
func ensureRemoteLine(t *testing.T, d *Document, line int) {
	t.Helper()
	ctx := context.Background()
	for range 8 {
		if text := d.Buffer.Line(line); text != "" {
			return
		}
		w, ok := d.TakePendingLargeWindow()
		if !ok {
			t.Fatalf("line %d read did not record a window", line)
		}
		if err := d.LoadLargeWindow(ctx, w); err != nil {
			t.Fatalf("LoadLargeWindow: %v", err)
		}
	}
	t.Fatalf("line %d never loaded", line)
}

// openRemoteLargeForSave opens content over the fake authority and completes its
// index, so PlanSave is allowed.
func openRemoteLargeForSave(t *testing.T, content []byte) (*Document, *fakeRangeAccess) {
	t.Helper()
	access := &fakeRangeAccess{data: content}
	d, err := readDocument("/remote/save.txt", loadSpec{
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
	t.Cleanup(func() { _ = d.Buffer.Close() })
	completeRemoteLargeIndex(t, d)
	return d, access
}

// TestRemoteLargeEditInsideWindowSaves checks a normal edit of a windowed remote
// large file is an overlay edit and the save merges it into the whole file through
// the authority's atomic write, with no local file involved.
func TestRemoteLargeEditInsideWindowSaves(t *testing.T) {
	content := remoteLargeSaveContent(50, nil)
	d, access := openRemoteLargeForSave(t, content)
	d.Buffer.Insert(buffer.Pos{Line: 10, Col: 0}, "EDIT ")
	if !d.Buffer.Modified() {
		t.Fatal("an overlay edit did not mark the document modified")
	}
	job := d.PlanSave(d.Path, Settings{})
	res := job.Run()
	if err := res.Err(); err != nil {
		t.Fatalf("save: %v", err)
	}
	want := remoteLargeSaveContent(50, map[int]string{10: "EDIT "})
	if got := access.writtenBytes(); !bytes.Equal(got, want) {
		t.Fatalf("written = %q, want %q", got, want)
	}
	if res.Res.Bytes != int64(len(want)) {
		t.Fatalf("WriteResult bytes = %d, want %d", res.Res.Bytes, len(want))
	}
	if res.Source == nil {
		t.Fatal("the save did not carry a fresh source for the rebase")
	}
	if _, err := os.Stat(d.Path); err == nil {
		t.Fatalf("the local path %q exists after a remote save", d.Path)
	}
	if access.writeCount() != 1 {
		t.Fatalf("remote writes = %d, want 1", access.writeCount())
	}
}

// TestRemoteLargeEditAcrossWindowsSaves checks edits in different windows merge
// with the untouched head/tail, whose windows the save reads back through the
// windowed source (the index scan evicted the early ones from the 8-window cache).
func TestRemoteLargeEditAcrossWindowsSaves(t *testing.T) {
	content := remoteLargeSaveContent(900000, nil) // ~10 MiB, more than 8 windows
	d, access := openRemoteLargeForSave(t, content)
	// Scroll the two edited lines into view first, as the editor does before a user
	// can put the caret on them; the index scan left the early windows evicted.
	ensureRemoteLine(t, d, 0)
	ensureRemoteLine(t, d, 800000)
	d.Buffer.Insert(buffer.Pos{Line: 0, Col: 0}, "HEAD ")
	d.Buffer.Insert(buffer.Pos{Line: 800000, Col: 0}, "TAIL ")
	if ref := d.Buffer.Refusal(); ref != nil {
		t.Fatalf("edit refused: %v", ref)
	}
	job := d.PlanSave(d.Path, Settings{})
	res := job.Run()
	if err := res.Err(); err != nil {
		t.Fatalf("save across windows: %v", err)
	}
	want := remoteLargeSaveContent(900000, map[int]string{0: "HEAD ", 800000: "TAIL "})
	if got := access.writtenBytes(); !bytes.Equal(got, want) {
		t.Fatalf("merged file differs: got %d bytes, want %d", len(got), len(want))
	}
	if res.Source == nil {
		t.Fatal("no fresh source for the rebase")
	}
}

// TestRemoteLargeSaveBoundRefusal checks the merged size bound refuses the save
// before any remote write, with a clear reason.
func TestRemoteLargeSaveBoundRefusal(t *testing.T) {
	content := remoteLargeSaveContent(50, nil)
	d, access := openRemoteLargeForSave(t, content)
	d.Buffer.Insert(buffer.Pos{Line: 1, Col: 0}, "EDIT ")
	old := remoteLargeSaveMax
	remoteLargeSaveMax = int64(len(content) / 2)
	defer func() { remoteLargeSaveMax = old }()
	res := d.PlanSave(d.Path, Settings{}).Run()
	err := res.Err()
	if err == nil || !strings.Contains(err.Error(), "windowed-save bound") {
		t.Fatalf("save over the bound = %v, want the windowed-save bound error", err)
	}
	if access.writeCount() != 0 {
		t.Fatalf("remote writes = %d, want 0", access.writeCount())
	}
}

// TestRemoteLargeSaveRefusesChangedRemote checks a remote file that changed after
// the plan refuses the save instead of overwriting it.
func TestRemoteLargeSaveRefusesChangedRemote(t *testing.T) {
	content := remoteLargeSaveContent(50, nil)
	d, access := openRemoteLargeForSave(t, content)
	d.Buffer.Insert(buffer.Pos{Line: 1, Col: 0}, "EDIT ")
	access.change(append(append([]byte(nil), content...), []byte("changed elsewhere\n")...))
	res := d.PlanSave(d.Path, Settings{}).Run()
	if !errors.Is(res.Err(), ErrChangedOnDisk) {
		t.Fatalf("save over a changed remote file = %v, want ErrChangedOnDisk", res.Err())
	}
	if access.writeCount() != 0 {
		t.Fatalf("remote writes = %d, want 0", access.writeCount())
	}
}

// TestRemoteLargeOverlayUndo checks an overlay edit of a windowed remote file can be
// undone on the update goroutine, like a local large-file edit.
func TestRemoteLargeOverlayUndo(t *testing.T) {
	content := remoteLargeSaveContent(20, nil)
	d, access := openRemoteLargeForSave(t, content)
	before := d.Buffer.Line(3)
	d.Buffer.Insert(buffer.Pos{Line: 3, Col: 0}, "UNDO ")
	if got := d.Buffer.Line(3); got != "UNDO "+before {
		t.Fatalf("edited line = %q, want the overlay edit", got)
	}
	if _, ok := d.Buffer.Undo(); !ok {
		t.Fatal("Undo reported no group")
	}
	if got := d.Buffer.Line(3); got != before {
		t.Fatalf("line after undo = %q, want %q", got, before)
	}
	if access.writeCount() != 0 {
		t.Fatalf("undo wrote to the remote: %d writes", access.writeCount())
	}
}

// TestRemoteLargeSaveAsOtherPath checks Save As builds the same merged bytes and
// still writes only through the authority.
func TestRemoteLargeSaveAsOtherPath(t *testing.T) {
	content := remoteLargeSaveContent(50, nil)
	d, access := openRemoteLargeForSave(t, content)
	d.Buffer.Insert(buffer.Pos{Line: 2, Col: 0}, "AS ")
	other := filepath.Join(t.TempDir(), "copy.txt")
	job := d.PlanSave(other, Settings{})
	job.CheckDisk = false
	res := job.Run()
	if err := res.Err(); err != nil {
		t.Fatalf("save as: %v", err)
	}
	want := remoteLargeSaveContent(50, map[int]string{2: "AS "})
	if got := access.writtenBytes(); !bytes.Equal(got, want) {
		t.Fatalf("save-as written bytes differ")
	}
	if _, err := os.Stat(other); err == nil {
		t.Fatalf("Save As created the local path %q", other)
	}
}
