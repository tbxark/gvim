package app

import (
	"bytes"
	"context"
	"errors"
	"os"
	"sync"
	"testing"

	"github.com/tbxark/gvim/authority"
	"github.com/tbxark/gvim/authority/authoritytest"
	"github.com/tbxark/gvim/document"
	"github.com/tbxark/gvim/plugin/hostos"
)

// fakeAppStream is one streamed write recorded by the app adapter test.
type fakeAppStream struct {
	mu        sync.Mutex
	chunks    [][]byte
	committed bool
	aborted   bool
}

func (s *fakeAppStream) Write(ctx context.Context, p []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.chunks = append(s.chunks, append([]byte(nil), p...))
	return nil
}
func (s *fakeAppStream) Commit(context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.committed = true
	return nil
}
func (s *fakeAppStream) Abort(context.Context) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.aborted = true
}
func (s *fakeAppStream) state() (chunks [][]byte, committed, aborted bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([][]byte(nil), s.chunks...), s.committed, s.aborted
}

// fakeAppStreamFS adds the authority stream surface to the in-memory remote file fake.
type fakeAppStreamFS struct {
	*remoteFS
	mu      sync.Mutex
	opens   []string
	options []hostos.WriteOptions
	stream  *fakeAppStream
	err     error
}

func (f *fakeAppStreamFS) WriteStream(_ context.Context, path string, o hostos.WriteOptions) (authority.WriteStream, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.opens = append(f.opens, path)
	f.options = append(f.options, o)
	if f.err != nil {
		return nil, f.err
	}
	f.stream = &fakeAppStream{}
	return f.stream, nil
}

func (f *fakeAppStreamFS) opened() ([]string, []hostos.WriteOptions) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.opens...), append([]hostos.WriteOptions(nil), f.options...)
}

// TestRemoteFileAccessWriteStream checks the app adapter forwards the document stream to
// the authority's atomic stream (path + atomic option), chunks and commit reach it, and no
// local file is touched.
func TestRemoteFileAccessWriteStream(t *testing.T) {
	fs := &fakeAppStreamFS{remoteFS: newRemoteFS()}
	slot := authoritytest.New()
	slot.FileSystem = fs
	r := newRemoteFileAccess(slot)
	var access document.StreamAccess = r
	ctx := context.Background()
	s, err := access.WriteStream(ctx, "/remote/new.txt")
	if err != nil {
		t.Fatalf("WriteStream: %v", err)
	}
	if err := s.Write(ctx, []byte("hello ")); err != nil {
		t.Fatalf("write 1: %v", err)
	}
	if err := s.Write(ctx, []byte("remote")); err != nil {
		t.Fatalf("write 2: %v", err)
	}
	if err := s.Commit(ctx); err != nil {
		t.Fatalf("commit: %v", err)
	}
	opens, options := fs.opened()
	if len(opens) != 1 || opens[0] != "/remote/new.txt" {
		t.Fatalf("stream opens = %v, want the remote path", opens)
	}
	if len(options) != 1 || !options[0].Atomic {
		t.Fatalf("stream options = %+v, want one atomic write", options)
	}
	chunks, committed, aborted := fs.stream.state()
	if aborted || !committed {
		t.Fatalf("stream committed=%v aborted=%v", committed, aborted)
	}
	if len(chunks) != 2 || !bytes.Equal(bytes.Join(chunks, nil), []byte("hello remote")) {
		t.Fatalf("streamed chunks = %q", chunks)
	}
	if _, err := os.Stat("/remote/new.txt"); err == nil {
		t.Fatal("the remote path exists locally")
	}
	// Abort forwards too.
	s, err = access.WriteStream(ctx, "/remote/other.txt")
	if err != nil {
		t.Fatalf("WriteStream 2: %v", err)
	}
	s.Abort(ctx)
	if _, _, aborted := fs.stream.state(); !aborted {
		t.Fatal("Abort did not reach the authority stream")
	}
}

// TestRemoteFileAccessWithoutStream checks an access whose authority has no stream surface
// reports ErrUnsupportedFileIO, so the document keeps the bounded whole-file fallback.
func TestRemoteFileAccessWithoutStream(t *testing.T) {
	slot := authoritytest.New()
	slot.FileSystem = newRemoteFS()
	r := newRemoteFileAccess(slot)
	if _, err := document.StreamAccess(r).WriteStream(context.Background(), "/remote/x.txt"); err == nil || !errors.Is(err, document.ErrStreamUnsupported) {
		t.Fatalf("WriteStream without a stream backend = %v, want document.ErrStreamUnsupported", err)
	}
}
