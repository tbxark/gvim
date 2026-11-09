package app

// Remote file preview for the quick-open palette (task-116): the head of a remote file is
// read through the workspace authority (bounded, no local fallback), so the palette's
// existing LRU, stale-sequence and "reader is called from a command" machinery works
// unchanged.

import (
	"context"
	"errors"
	"io"
	"strings"
	"time"

	"github.com/tbxark/gvim/authority"
	"github.com/tbxark/gvim/internal/tui/palette"
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

// ReadWindow is the remote Live Grep preview: it reads the remote file through the
// authority in bounded ReadRange chunks, stops at a scan budget, and returns the complete
// lines around the 1-based match line (five lines before, twelve lines maximum). It is
// cancellable through ctx (the palette cancels it when the selection moves), never reads the
// local disk, and falls back to a bounded whole-file ReadFile only when the backend has no
// windowed primitive.
func (p *remotePreviewer) ReadWindow(ctx context.Context, path string, line, maxBytes int) ([]byte, int, bool, error) {
	if p.slot == nil {
		return nil, 0, false, errors.New("remote workspace is disconnected")
	}
	if !p.slot.Connected() {
		return nil, 0, false, errors.New("remote workspace is disconnected")
	}
	files := p.slot.Files()
	if files == nil {
		return nil, 0, false, errors.New("remote workspace has no file system")
	}
	if line < 1 {
		line = 1
	}
	if maxBytes <= 0 {
		maxBytes = remotePreviewMinBytes
	}
	if maxBytes > remotePreviewMaxBytes {
		maxBytes = remotePreviewMaxBytes
	}
	const (
		chunk      = 64 << 10
		scanBudget = 8 << 20
		before     = 5
		maxLines   = 12
	)
	runCtx, cancel := context.WithTimeout(ctx, remotePreviewTimeout)
	defer cancel()
	var data []byte
	eof := false
	for len(data) < scanBudget {
		if err := runCtx.Err(); err != nil {
			return nil, 0, false, err
		}
		part, err := files.ReadRange(runCtx, path, int64(len(data)), chunk)
		if len(part) > 0 {
			data = append(data, part...)
		}
		if err == nil {
			if len(part) == 0 {
				eof = true
				break
			}
			if strings.Count(string(data), "\n") >= line+maxLines {
				break
			}
			continue
		}
		if errors.Is(err, io.EOF) {
			eof = true
			break
		}
		if errors.Is(err, authority.ErrUnsupportedFileIO) {
			data, err = files.ReadFile(runCtx, path, scanBudget)
			if err != nil {
				return nil, 0, false, err
			}
			eof = true
			break
		}
		return nil, 0, false, err
	}
	if len(data) == 0 {
		return nil, 0, false, nil
	}
	lines := strings.Split(string(data), "\n")
	if n := len(lines); n > 0 && lines[n-1] == "" {
		lines = lines[:n-1]
	} else if !eof && len(lines) > 0 {
		// The scan stopped on a budget/line bound, not EOF: the last line may be partial.
		lines = lines[:len(lines)-1]
	}
	first := line - before
	if first < 1 {
		first = 1
	}
	if first > len(lines) {
		return nil, 0, false, nil
	}
	last := min(first+maxLines-1, len(lines))
	if last < first {
		return nil, 0, false, nil
	}
	out := strings.Join(lines[first-1:last], "\n") + "\n"
	truncated := last < len(lines) || len(data) >= scanBudget
	if len(out) > maxBytes {
		cut := maxBytes
		if i := strings.LastIndexByte(out[:maxBytes], '\n'); i >= 0 {
			cut = i + 1
		}
		out = out[:cut]
		truncated = true
	}
	return []byte(out), first, truncated, nil
}

var _ palette.WindowPreviewer = (*remotePreviewer)(nil)
