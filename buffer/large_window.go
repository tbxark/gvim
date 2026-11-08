package buffer

import (
	"context"
)

// Windowed large-file sources: an asynchronous source (a remote file read through
// an authority) caches windows in memory, and reads outside the cache return
// ErrSourcePending instead of blocking the paint path. The root loads a window in a
// command and repaints when it arrives. Local files use fileSource, which is not a
// WindowSource and keeps its synchronous behavior unchanged.

// AsyncLarge reports whether a is a large buffer over an asynchronous window
// source.
func (b *Buffer) AsyncLarge() bool {
	return b.big != nil && isWindowSource(b.big.src.src)
}

// TakePendingWindow returns and clears the first window a read missed, for the
// caller to load in a command. ok is false for a memory buffer, a local file
// source, or when nothing missed.
func (b *Buffer) TakePendingWindow() (off, n int64, ok bool) {
	if b.big == nil {
		return 0, 0, false
	}
	ws, ok := b.big.src.src.(WindowSource)
	if !ok {
		return 0, 0, false
	}
	return ws.TakePendingWindow()
}

// LoadWindow loads the windows covering [off, off+n) through the asynchronous
// source. It blocks on I/O and must run in a command, never on the update or paint
// path.
func (b *Buffer) LoadWindow(ctx context.Context, off, n int64) error {
	if b.big == nil {
		return ErrNotWindowed
	}
	ws, ok := b.big.src.src.(WindowSource)
	if !ok {
		return ErrNotWindowed
	}
	return ws.LoadWindow(ctx, off, n)
}

// WindowGeneration returns the asynchronous source's window-arrival counter, or 0
// for a memory buffer or a local file source.
func (b *Buffer) WindowGeneration() uint64 {
	if b.big == nil {
		return 0
	}
	ws, ok := b.big.src.src.(WindowSource)
	if !ok {
		return 0
	}
	return ws.Generation()
}

// isWindowSource reports whether src loads asynchronously.
func isWindowSource(src Source) bool {
	_, ok := src.(WindowSource)
	return ok
}
