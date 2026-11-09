package pluginhost

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"

	"github.com/tbxark/gvim/authority"
	"github.com/tbxark/gvim/plugin/hostos"
	"github.com/tbxark/gvim/plugin/plugintest"
	"github.com/tbxark/gvim/pluginapi"
)

// fakeStreamFiles is the remote file surface plus the authority's streamed-write surface.
// The stream writes into a temp buffer and only Commit publishes it, so a failure/abort
// must leave an existing destination untouched.
type fakeStreamFiles struct {
	*fakeMutableFiles
	mu         sync.Mutex
	chunks     int
	commits    int
	aborts     int
	failChunk  int
	failErr    error
	chunkSizes []int
}

func (f *fakeStreamFiles) WriteStream(ctx context.Context, path string, _ hostos.WriteOptions) (authority.WriteStream, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return &fakeAuthorityWriteStream{parent: f, path: path}, nil
}

type fakeAuthorityWriteStream struct {
	parent *fakeStreamFiles
	path   string
	buf    strings.Builder
	done   bool
}

func (s *fakeAuthorityWriteStream) Write(_ context.Context, p []byte) error {
	if s.done {
		return io.ErrClosedPipe
	}
	s.parent.mu.Lock()
	s.parent.chunks++
	n := s.parent.chunks
	s.parent.chunkSizes = append(s.parent.chunkSizes, len(p))
	fail := s.parent.failChunk > 0 && n >= s.parent.failChunk
	err := s.parent.failErr
	s.parent.mu.Unlock()
	if fail {
		if err == nil {
			err = errors.New("remote stream write failed")
		}
		return err
	}
	s.buf.Write(p)
	return nil
}

func (s *fakeAuthorityWriteStream) Commit(_ context.Context) error {
	if s.done {
		return io.ErrClosedPipe
	}
	s.parent.mu.Lock()
	defer s.parent.mu.Unlock()
	s.parent.mutableFiles().files[s.path] = s.buf.String()
	s.parent.commits++
	s.done = true
	return nil
}

func (s *fakeAuthorityWriteStream) Abort(context.Context) {
	if s.done {
		return
	}
	s.done = true
	s.parent.mu.Lock()
	s.parent.aborts++
	s.parent.mu.Unlock()
}

func (f *fakeStreamFiles) mutableFiles() *fakeMutableFiles { return f.fakeMutableFiles }

func newStreamFiles() *fakeStreamFiles {
	remote := &fakeMutableFiles{fakeRemoteFiles: newFakeRemoteFiles()}
	return &fakeStreamFiles{fakeMutableFiles: remote}
}

func TestAuthorityOSCopyStreamsLargeFile(t *testing.T) {
	stream := newStreamFiles()
	data := strings.Repeat("x", (16<<20)+17)
	stream.files["/srv/proj/big.bin"] = data
	stream.files["/srv/proj/copy.bin"] = "old target must survive until commit"
	slot, _, _ := authorityTestSlot(stream)
	local := plugintest.NewFakeOS()
	osys := NewAuthorityOS(slot, local, nil)
	if err := osys.Copy(context.Background(), "big.bin", "copy.bin"); err != nil {
		t.Fatal(err)
	}
	if got := stream.files["/srv/proj/copy.bin"]; got != data {
		t.Fatalf("copied size = %d, want %d", len(got), len(data))
	}
	stream.mu.Lock()
	chunks, commits, aborts := stream.chunks, stream.commits, stream.aborts
	stream.mu.Unlock()
	if chunks < 17 || commits != 1 || aborts != 0 {
		t.Fatalf("stream: chunks %d commits %d aborts %d (want >16 chunks, exactly one commit)", chunks, commits, aborts)
	}
}

func TestAuthorityOSCopyStreamFailureKeepsExistingTarget(t *testing.T) {
	for _, tc := range []struct {
		name     string
		failErr  error
		wantCode pluginapi.Code
	}{
		{"write failure", errors.New("remote write failed"), pluginapi.CodeIO},
		{"cancellation", context.Canceled, pluginapi.CodeCancelled},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stream := newStreamFiles()
			stream.files["/srv/proj/big.bin"] = strings.Repeat("y", 2*remoteCopyChunk+1)
			stream.files["/srv/proj/copy.bin"] = "keep me"
			stream.failChunk, stream.failErr = 2, tc.failErr
			slot, _, _ := authorityTestSlot(stream)
			osys := NewAuthorityOS(slot, plugintest.NewFakeOS(), nil)
			err := osys.Copy(context.Background(), "big.bin", "copy.bin")
			if err == nil || pluginapi.CodeOf(err) != tc.wantCode {
				t.Fatalf("copy error = %v, code %q; want %q", err, pluginapi.CodeOf(err), tc.wantCode)
			}
			if got := stream.files["/srv/proj/copy.bin"]; got != "keep me" {
				t.Fatalf("a failed streamed copy replaced the existing target: %q", got)
			}
			stream.mu.Lock()
			commits, aborts := stream.commits, stream.aborts
			stream.mu.Unlock()
			if commits != 0 || aborts != 1 {
				t.Fatalf("failed copy: commits %d aborts %d, want 0/1", commits, aborts)
			}
		})
	}
}

func TestAuthorityOSCopyStreamSourceShrinkIsSafe(t *testing.T) {
	stream := newStreamFiles()
	stream.files["/srv/proj/shrink.bin"] = "x"
	stream.sizes = map[string]int64{"/srv/proj/shrink.bin": 100}
	stream.files["/srv/proj/copy.bin"] = "keep me"
	slot, _, _ := authorityTestSlot(stream)
	osys := NewAuthorityOS(slot, plugintest.NewFakeOS(), nil)
	err := osys.Copy(context.Background(), "shrink.bin", "copy.bin")
	if err == nil || pluginapi.CodeOf(err) != pluginapi.CodeIO || !strings.Contains(err.Error(), "source changed during copy") {
		t.Fatalf("shrunken-source copy = %v, code %q", err, pluginapi.CodeOf(err))
	}
	if got := stream.files["/srv/proj/copy.bin"]; got != "keep me" {
		t.Fatalf("a shrunken-source copy replaced the target: %q", got)
	}
	stream.mu.Lock()
	aborts := stream.aborts
	stream.mu.Unlock()
	if aborts != 1 {
		t.Fatalf("shrunken-source copy aborts = %d, want 1", aborts)
	}
}

func TestAuthorityOSCopyStreamsTreeFiles(t *testing.T) {
	remote := walkFixture()
	remote.files["/srv/proj/src/b.go"] = "bb"
	remote.files["/srv/proj/src/sub/c.go"] = "ccc"
	stream := &fakeStreamFiles{fakeMutableFiles: &fakeMutableFiles{fakeRemoteFiles: remote}}
	slot, _, _ := authorityTestSlot(stream)
	local := plugintest.NewFakeOS()
	osys := NewAuthorityOS(slot, local, nil)
	if err := osys.Copy(context.Background(), "src", "dst"); err != nil {
		t.Fatal(err)
	}
	if stream.files["/srv/proj/dst/b.go"] != "bb" || stream.files["/srv/proj/dst/sub/c.go"] != "ccc" {
		t.Fatalf("streamed tree files = %v", stream.files)
	}
	stream.mu.Lock()
	commits, aborts := stream.commits, stream.aborts
	stream.mu.Unlock()
	if commits != 2 || aborts != 0 {
		t.Fatalf("tree stream commits %d aborts %d, want 2/0", commits, aborts)
	}
	if calls := local.Calls(); len(calls) != 0 {
		t.Fatalf("streamed tree copy touched the local OS: %v", calls)
	}
}

func TestAuthorityOSCopyWithoutStreamerIsUnsupported(t *testing.T) {
	// There is no same-handle exclusive create+write primitive and no atomic streamer, so
	// the copy must be refused before it can create or write the destination.
	remote := &fakeMutableFiles{fakeRemoteFiles: walkFixture()}
	remote.files["/srv/proj/big.bin"] = "source"
	slot, _, _ := authorityTestSlot(remote)
	osys := NewAuthorityOS(slot, plugintest.NewFakeOS(), nil)
	err := osys.Copy(context.Background(), "big.bin", "copy.bin")
	if err == nil || pluginapi.CodeOf(err) != pluginapi.CodeUnsupported {
		t.Fatalf("copy without a streamer = %v, code %q", err, pluginapi.CodeOf(err))
	}
	if _, ok := remote.files["/srv/proj/copy.bin"]; ok {
		t.Fatal("unsupported copy created the destination")
	}
}
