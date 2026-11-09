package app

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/tbxark/gvim/editor"
	"github.com/tbxark/gvim/internal/tui/searchpanel"
)

func TestProjectSearchPreviewContextCancellation(t *testing.T) {
	s := newProjectSearch(editor.Options{})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := s.PreviewContext(ctx, searchpanel.PreviewRequest{Path: "/w/a.go", Lines: 3}); !errors.Is(err, context.Canceled) {
		t.Fatalf("PreviewContext error = %v, want context.Canceled", err)
	}
}

// blockingReadRangeFS proves the remote preview passes the caller's context into the
// authority read and stops when it is cancelled.
type blockingReadRangeFS struct {
	*remoteFS
	saw chan context.Context
}

func (f *blockingReadRangeFS) ReadRange(ctx context.Context, _ string, _, _ int64) ([]byte, error) {
	f.saw <- ctx
	<-ctx.Done()
	return nil, ctx.Err()
}

func TestRemoteSearchPreviewContextReachesReadRange(t *testing.T) {
	base := newRemoteFS()
	fs := &blockingReadRangeFS{remoteFS: base, saw: make(chan context.Context, 1)}
	s := newRemoteProjectSearch(&fakeOneShot{}, "/remote/w").withFiles(fs)
	ctx, cancel := context.WithCancel(context.Background())
	var (
		wg  sync.WaitGroup
		err error
	)
	wg.Add(1)
	go func() {
		defer wg.Done()
		_, err = s.PreviewContext(ctx, searchpanel.PreviewRequest{
			Path: "/remote/w/a.txt", Line: 1, EndLine: 1, EndCol: 2, Lines: 3,
		})
	}()
	readCtx := <-fs.saw
	if readCtx.Err() != nil {
		t.Fatal("the read context was already cancelled")
	}
	cancel()
	<-readCtx.Done()
	wg.Wait()
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("PreviewContext error = %v, want context.Canceled", err)
	}
}
