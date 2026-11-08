package app

// Remote file preview for the quick-open palette (task-116): the head of a remote file is
// read through the workspace authority (bounded, no local fallback), so the palette's
// existing LRU, stale-sequence and "reader is called from a command" machinery works
// unchanged.

import (
	"context"
	"errors"
	"io"
	"time"

	"github.com/tbxark/gvim/authority"
)

const (
	// remotePreviewMaxBytes caps one remote preview read regardless of what the caller
	// asks for (the palette asks 16 KiB today).
	remotePreviewMaxBytes = 64 << 10
	// remotePreviewMinBytes is the smallest head a too-large file is retried at before the
	// read gives up with an error (never pretending a partial head is complete).
	remotePreviewMinBytes = 1 << 10
	// remotePreviewTimeout bounds one remote preview read.
	remotePreviewTimeout = 6 * time.Second
)

// remotePreviewer is the palette's Previewer over a remote workspace: it reads the head of
// the remote file through authority.Files() and reports truncation honestly. A disconnected
// authority is an error, never a local read.
type remotePreviewer struct {
	slot authority.Slot
}

// newRemotePreviewer returns the remote previewer over slot.
func newRemotePreviewer(slot authority.Slot) *remotePreviewer {
	return &remotePreviewer{slot: slot}
}

// ReadHead returns at most maxBytes of the remote file at path. truncated reports that the
// file continues past the returned bytes. The authority's windowed ReadRange returns a short
// head of a large file (a short answer is io.EOF), so the palette shows the real head with an
// honest truncation flag; a backend without ReadRange (ErrUnsupportedFileIO) falls back to a
// bounded whole-file ReadFile, which can only return a complete small file and reports the
// too-large error for a larger one instead of pretending a head is the whole file.
func (p *remotePreviewer) ReadHead(path string, maxBytes int) ([]byte, bool, error) {
	if p.slot == nil {
		return nil, false, errors.New("remote workspace is disconnected")
	}
	if !p.slot.Connected() {
		return nil, false, errors.New("remote workspace is disconnected")
	}
	files := p.slot.Files()
	if files == nil {
		return nil, false, errors.New("remote workspace has no file system")
	}
	if maxBytes <= 0 {
		maxBytes = remotePreviewMinBytes
	}
	if maxBytes > remotePreviewMaxBytes {
		maxBytes = remotePreviewMaxBytes
	}
	ctx, cancel := context.WithTimeout(context.Background(), remotePreviewTimeout)
	defer cancel()
	// One byte past the window tells whether the file continues; the extra byte is
	// dropped and only reported as truncated.
	data, err := files.ReadRange(ctx, path, 0, int64(maxBytes)+1)
	if err == nil || errors.Is(err, io.EOF) {
		if len(data) > maxBytes {
			return data[:maxBytes], true, nil
		}
		return data, false, nil
	}
	if !errors.Is(err, authority.ErrUnsupportedFileIO) {
		return nil, false, err
	}
	// A backend without the windowed primitive: read the bounded whole file. A file that
	// fits is complete; a larger one is an error, never a silently truncated head.
	data, err = files.ReadFile(ctx, path, int64(maxBytes))
	if err != nil {
		return nil, false, err
	}
	return data, false, nil
}
