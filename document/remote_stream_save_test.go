package document

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/tbxark/gvim/buffer"
)

// fakeSaveStream records the chunks of one streamed save and materialises them into the
// fake access only at Commit, so a failed/aborted save leaves the "remote file" untouched.
type fakeSaveStream struct {
	access *fakeStreamAccess

	mu        sync.Mutex
	chunks    [][]byte
	failAt    int
	failErr   error
	commitErr error
	aborted   bool
	committed bool
	chunkMax  int
}

func (s *fakeSaveStream) Write(ctx context.Context, p []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.aborted || s.committed {
		return errors.New("stream is closed")
	}
	if s.failErr != nil && s.failAt > 0 && len(s.chunks)+1 == s.failAt {
		return s.failErr
	}
	s.chunks = append(s.chunks, append([]byte(nil), p...))
	if len(p) > s.chunkMax {
		s.chunkMax = len(p)
	}
	return nil
}

func (s *fakeSaveStream) Commit(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.aborted || s.committed {
		return errors.New("stream is closed")
	}
	if s.commitErr != nil {
		return s.commitErr
	}
	var data []byte
	for _, chunk := range s.chunks {
		data = append(data, chunk...)
	}
	s.access.mu.Lock()
	s.access.data = data
	s.access.mtime = s.access.mtime.Add(1)
	s.access.mu.Unlock()
	s.committed = true
	return nil
}

func (s *fakeSaveStream) Abort(context.Context) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.aborted = true
}

func (s *fakeSaveStream) state() (chunks int, aborted, committed bool, maxChunk int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.chunks), s.aborted, s.committed, s.chunkMax
}

// fakeStreamAccess is the windowed fake access plus the optional streamed-write surface.
type fakeStreamAccess struct {
	*fakeRangeAccess
	mu          sync.Mutex
	stream      *fakeSaveStream
	opens       int
	openErr     error
	unsupported bool
	nextFailAt  int
	nextFailErr error
}

func (a *fakeStreamAccess) WriteStream(ctx context.Context, path string) (WriteStream, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.opens++
	if a.unsupported {
		return nil, ErrStreamUnsupported
	}
	if a.openErr != nil {
		return nil, a.openErr
	}
	a.stream = &fakeSaveStream{access: a, failAt: a.nextFailAt, failErr: a.nextFailErr}
	return a.stream, nil
}

func (a *fakeStreamAccess) openCount() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.opens
}

func (a *fakeStreamAccess) lastStream() *fakeSaveStream {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.stream
}

// openRemoteLargeStreamSave opens content over a streaming fake access and completes the
// line index, so a large save can be planned.
func openRemoteLargeStreamSave(t *testing.T, content []byte) (*Document, *fakeStreamAccess) {
	t.Helper()
	access := &fakeStreamAccess{fakeRangeAccess: &fakeRangeAccess{data: content}}
	d, err := readDocument("/remote/stream-save.txt", loadSpec{
		Access: access,
		LargeConf: largeSpec{
			Threshold: 1,
			Geom:      bufferLargeOpts(),
		},
	})
	if err != nil {
		t.Fatalf("readDocument: %v", err)
	}
	t.Cleanup(func() { _ = d.Buffer.Close() })
	completeRemoteLargeIndex(t, d)
	return d, access
}

// bufferLargeOpts mirrors the existing remote save tests' geometry.
func bufferLargeOpts() buffer.LargeOptions {
	return buffer.LargeOptions{Chunk: 64 << 10, CachePages: 4, InitialBytes: 4096}
}

// TestRemoteLargeStreamedSaveHash checks a multi-window synthetic file saved through the
// streamed path is byte-identical (hash) to the expected merged file, with more than one
// bounded chunk and no whole-file atomic write.
func TestRemoteLargeStreamedSaveHash(t *testing.T) {
	// ~3 MiB: three 1 MiB windows, edits in the first and last.
	content := remoteLargeSaveContent(300000, nil)
	d, access := openRemoteLargeStreamSave(t, content)
	ensureRemoteLine(t, d, 0)
	ensureRemoteLine(t, d, 299999)
	d.Buffer.Insert(buffer.Pos{Line: 0, Col: 0}, "HEAD ")
	d.Buffer.Insert(buffer.Pos{Line: 299999, Col: 0}, "TAIL ")
	if ref := d.Buffer.Refusal(); ref != nil {
		t.Fatalf("edit refused: %v", ref)
	}
	job := d.PlanSave(d.Path, Settings{})
	if job.err != nil {
		t.Fatalf("PlanSave: %v", job.err)
	}
	res := job.Run()
	if err := res.Err(); err != nil {
		t.Fatalf("streamed save: %v", err)
	}
	want := remoteLargeSaveContent(300000, map[int]string{0: "HEAD ", 299999: "TAIL "})
	stream := access.lastStream()
	chunks, aborted, committed, maxChunk := stream.state()
	if aborted || !committed {
		t.Fatalf("stream aborted=%v committed=%v, want a commit", aborted, committed)
	}
	if chunks < 2 {
		t.Fatalf("streamed save used %d chunk(s); want a streamed multi-chunk write", chunks)
	}
	if maxChunk > 1<<20 {
		t.Fatalf("largest streamed chunk = %d bytes, want bounded", maxChunk)
	}
	gotHash := sha256.Sum256(access.data)
	wantHash := sha256.Sum256(want)
	if gotHash != wantHash {
		t.Fatalf("streamed file hash = %x, want %x (%d vs %d bytes)", gotHash, wantHash, len(access.data), len(want))
	}
	if access.writeCount() != 0 {
		t.Fatalf("the whole-file atomic write ran %d time(s) on the streamed path", access.writeCount())
	}
	if res.Source == nil {
		t.Fatal("the streamed save did not carry a fresh windowed source")
	}
	if res.Res.Bytes != int64(len(want)) {
		t.Fatalf("WriteResult bytes = %d, want %d", res.Res.Bytes, len(want))
	}
}

// TestRemoteLargeStreamedSaveMidStreamFailure checks a write failure aborts the stream and
// leaves the original remote file untouched (temp+rename semantics).
func TestRemoteLargeStreamedSaveMidStreamFailure(t *testing.T) {
	content := remoteLargeSaveContent(200000, nil)
	d, access := openRemoteLargeStreamSave(t, content)
	ensureRemoteLine(t, d, 100000)
	d.Buffer.Insert(buffer.Pos{Line: 100000, Col: 0}, "EDIT ")
	job := d.PlanSave(d.Path, Settings{})
	if job.err != nil {
		t.Fatalf("PlanSave: %v", job.err)
	}
	// Fail the second chunk the merged stream sends.
	access.mu.Lock()
	access.nextFailAt, access.nextFailErr = 2, errors.New("carrier dropped")
	access.mu.Unlock()
	res := job.Run()
	if err := res.Err(); err == nil {
		t.Fatal("a mid-stream failure reported a successful save")
	}
	stream := access.lastStream()
	chunks, aborted, committed, _ := stream.state()
	if !aborted || committed {
		t.Fatalf("stream aborted=%v committed=%v, want abort and no commit", aborted, committed)
	}
	if chunks == 0 || chunks >= 2 {
		t.Fatalf("chunks = %d, want the failure on the second chunk", chunks)
	}
	// The original remote bytes are unchanged; no temp/partial data was materialised.
	if !bytes.Equal(access.data, content) {
		t.Fatalf("the remote file changed after a failed streamed save")
	}
	if access.writeCount() != 0 {
		t.Fatalf("a fallback whole-file write ran after the stream failure")
	}
}

// TestRemoteLargeStreamedSaveCancellationAborts checks a cancelled stream is aborted and
// cleaned up without touching the original file.
func TestRemoteLargeStreamedSaveCancellationAborts(t *testing.T) {
	content := remoteLargeSaveContent(200000, nil)
	d, access := openRemoteLargeStreamSave(t, content)
	ensureRemoteLine(t, d, 50000)
	d.Buffer.Insert(buffer.Pos{Line: 50000, Col: 0}, "EDIT ")
	job := d.PlanSave(d.Path, Settings{})
	if job.err != nil {
		t.Fatalf("PlanSave: %v", job.err)
	}
	access.mu.Lock()
	access.nextFailAt, access.nextFailErr = 1, context.Canceled
	access.mu.Unlock()
	res := job.Run()
	if err := res.Err(); err == nil {
		t.Fatal("a cancelled streamed save reported success")
	}
	stream := access.lastStream()
	_, aborted, committed, _ := stream.state()
	if !aborted || committed {
		t.Fatalf("stream aborted=%v committed=%v, want abort and no commit", aborted, committed)
	}
	if !bytes.Equal(access.data, content) {
		t.Fatal("the remote file changed after a cancelled streamed save")
	}
}

// TestRemoteLargeStreamedSaveChangedRefuses checks a remote file that changed under the
// document still refuses before a stream is opened.
func TestRemoteLargeStreamedSaveChangedRefuses(t *testing.T) {
	content := remoteLargeSaveContent(1000, nil)
	d, access := openRemoteLargeStreamSave(t, content)
	d.Buffer.Insert(buffer.Pos{Line: 0, Col: 0}, "EDIT ")
	access.change(append([]byte(nil), content...))
	job := d.PlanSave(d.Path, Settings{})
	if job.err == nil {
		if err := job.Run().Err(); err == nil {
			t.Fatal("a changed remote file did not refuse the streamed save")
		}
	}
	if access.openCount() != 0 {
		t.Fatalf("a stream was opened for a changed file")
	}
	if !bytes.Equal(access.data, content) {
		t.Fatal("the changed remote file was overwritten")
	}
}

// TestRemoteLargeSaveWithoutStreamKeepsBound checks an access without StreamAccess keeps
// the old bounded whole-file path (and refuses a merge over the bound).
func TestRemoteLargeSaveWithoutStreamKeepsBound(t *testing.T) {
	// No StreamAccess: the plain fakeRangeAccess.
	content := remoteLargeSaveContent(300, nil)
	d, access := openRemoteLargeForSave(t, content)
	d.Buffer.Insert(buffer.Pos{Line: 0, Col: 0}, "EDIT ")
	old := remoteLargeSaveMax
	remoteLargeSaveMax = 64
	t.Cleanup(func() { remoteLargeSaveMax = old })
	job := d.PlanSave(d.Path, Settings{})
	if job.err != nil {
		t.Fatalf("PlanSave: %v", job.err)
	}
	if err := job.Run().Err(); err == nil || !strings.Contains(err.Error(), "over the remote windowed-save bound") {
		t.Fatalf("save without StreamAccess = %v, want the bounded refusal", err)
	}
	if access.writeCount() != 0 || !bytes.Equal(access.data, content) {
		t.Fatal("the bounded fallback wrote despite refusing the merge")
	}
}
