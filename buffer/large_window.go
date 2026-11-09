package buffer

import (
	"context"
	"sort"
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

// WindowAvailability is implemented by a WindowSource that can report which byte
// ranges are cached, without recording a miss. A source that does not implement it
// keeps the conservative answer that every line is available (local files and memory
// buffers are).
type WindowAvailability interface {
	// Loaded reports whether [off, off+n) is cached.
	Loaded(off, n int64) bool
}

// LineLoaded reports whether line i can be read without a source read or recording a
// pending window. It is true for memory buffers, local large files, and the edited
// overlay lines of a large buffer. For an asynchronous window source that reports
// availability, a line in an unloaded window is false: reading it would return only
// the blank placeholder and make the caller load that window. Syntax highlighting and
// other read-only consumers use it to avoid I/O beyond what the paint already needs.
func (b *Buffer) LineLoaded(i int) bool {
	if b.big == nil {
		return true
	}
	avail, ok := b.big.src.src.(WindowAvailability)
	if !ok {
		return true
	}
	r := b.big.ref(b.big.clampLine(i))
	if r.isLit {
		return true // the edit overlay is always in memory
	}
	// Find the chunks holding the line's bytes from the newline counts alone. Asking
	// lineRange here would read the page cache and turn the availability check itself
	// into a source miss.
	g := b.big.g
	base := int64(r.base)
	firstChunk := int64(0)
	if base > 0 {
		firstChunk = chunkOfNewline(g, base)
	}
	var lastChunk int64
	switch {
	case base < g.nl[g.scanned]:
		lastChunk = chunkOfNewline(g, base+1)
	case g.complete():
		lastChunk = int64(g.nChunks() - 1)
	default:
		return false // the line's terminator is not indexed yet: do not read a prefix
	}
	if firstChunk < 0 || lastChunk < firstChunk {
		return false
	}
	start := g.skip + firstChunk*g.chunk
	end := g.skip + min((lastChunk+1)*g.chunk, g.size)
	return avail.Loaded(start, max(end-start, 1))
}

// chunkOfNewline returns the index of the chunk holding the k-th '\n' (k counts
// from 1). It uses only the scanned newline counts, so it reads no page and can never
// turn into a source miss.
func chunkOfNewline(g gridView, k int64) int64 {
	return int64(sort.Search(g.scanned+1, func(i int) bool { return g.nl[i] >= k }) - 1)
}

// isWindowSource reports whether src loads asynchronously.
func isWindowSource(src Source) bool {
	_, ok := src.(WindowSource)
	return ok
}
