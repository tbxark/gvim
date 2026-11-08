package buffer

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"
)

// Defaults of large file mode.
const (
	// DefaultCachePages is the number of source pages one buffer may cache.
	DefaultCachePages = 64
	// DefaultInitialBytes is how much of the source OpenLarge indexes before
	// it returns.
	DefaultInitialBytes = 4 << 20
	// DefaultDecodedBudget is the byte budget of decoded lines (4 bytes per
	// rune).
	DefaultDecodedBudget = 32 << 20

	maxChunk = 1 << 30
)

// intSize is the width of int in bits; tests override it.
var intSize = strconv.IntSize

// LargeOptions tune a large buffer. The zero value means the defaults; tests
// use tiny values to exercise chunk edges.
type LargeOptions struct {
	// Chunk is the byte size of one index chunk and of one cached page.
	Chunk int
	// CachePages is the number of pages kept in memory.
	CachePages int
	// MaxLine is the longest line, in bytes, that is read whole.
	MaxLine int
	// InitialBytes is the prefix OpenLarge indexes before it returns.
	InitialBytes int
	// Skip is the number of leading bytes that are not text (a BOM).
	Skip int64
	// Budget bounds the page memory shared with other large buffers. Nil
	// means the process-wide budget.
	Budget *PageBudget
	// DecodedBudget is the byte budget of the decoded line cache.
	DecodedBudget int64
	// MaxOverlayBytes caps the estimated memory of unsaved edits. Past it
	// edits are refused (ErrOverlayFull).
	MaxOverlayBytes int64
	// MaxHistoryBytes caps the estimated memory the undo history keeps alive;
	// the oldest groups are dropped first.
	MaxHistoryBytes int64
	// MaxHistoryGroups caps the number of undo groups kept.
	MaxHistoryGroups int
}

func (o LargeOptions) normalized() LargeOptions {
	if o.Chunk <= 0 {
		o.Chunk = DefaultChunk
	}
	o.Chunk = min(o.Chunk, maxChunk)
	if o.CachePages <= 0 {
		o.CachePages = DefaultCachePages
	}
	if o.MaxLine <= 0 {
		o.MaxLine = DefaultMaxLine
	}
	if o.InitialBytes <= 0 {
		o.InitialBytes = DefaultInitialBytes
	}
	if o.Budget == nil {
		o.Budget = defaultBudget
	}
	if o.DecodedBudget <= 0 {
		o.DecodedBudget = DefaultDecodedBudget
	}
	if o.MaxOverlayBytes <= 0 {
		o.MaxOverlayBytes = DefaultMaxOverlayBytes
	}
	if o.MaxHistoryBytes <= 0 {
		o.MaxHistoryBytes = DefaultMaxHistoryBytes
	}
	if o.MaxHistoryGroups <= 0 {
		o.MaxHistoryGroups = MaxHistoryGroups
	}
	return o
}

// OpenLarge returns a buffer that reads its text from src instead of holding
// it in memory. It indexes at most InitialBytes of the source (plus what a
// first line needs, up to MaxLine) and returns; the rest is indexed by
// BeginIndexScan batches. Until then Lines() counts the lines found so far.
//
// Edits go to an in-memory overlay of literal lines over the source's base
// lines, so they cost memory only for the lines that were changed. An edit
// that cannot be made (a line that is too long or not valid UTF-8, too many
// unsaved bytes, a changed source) does nothing and reports why through
// Refusal.
//
// On success the buffer owns src and closes it when it, and every index scan
// started from it, are closed. On error the caller still owns src.
func OpenLarge(src Source, o LargeOptions) (*Buffer, error) {
	if intSize < 64 {
		return nil, ErrUnsupportedPlatform
	}
	o = o.normalized()
	total := src.Size()
	if o.Skip < 0 || o.Skip > total {
		return nil, fmt.Errorf("buffer: skip %d is outside the %d byte source", o.Skip, total)
	}
	size := total - o.Skip
	chunk := int64(o.Chunk)
	stats := new(statCounters)
	s := &bigStore{
		baseReader: baseReader{
			src: newSrcRef(src),
			g: gridView{
				chunk: chunk, skip: o.Skip, size: size,
				nl: make([]int64, (size+chunk-1)/chunk+1),
			},
			pages:   newPageCache(chunk, o.CachePages, o.Budget, stats),
			ranges:  newRangeMemo(),
			huges:   newHugeMemo(),
			stats:   stats,
			maxLine: int64(o.MaxLine),
		},
		budget:     o.Budget,
		dec:        newLineCache(o.DecodedBudget),
		maxOverlay: o.MaxOverlayBytes,
		maxHistory: o.MaxHistoryBytes,
		maxGroups:  o.MaxHistoryGroups,
	}
	s.budget.stores.Add(1)

	// Index the prefix: at least InitialBytes, and then on until a first line
	// is known or MaxLine bytes show that it is a very long one.
	scan := s.beginScan()
	defer scan.Close()
	for !s.g.complete() {
		done := int64(s.g.scanned) * chunk
		if done >= int64(o.InitialBytes) && (s.g.baseLines() > 0 || done >= int64(o.MaxLine)) {
			break
		}
		batch, err := scan.Next(context.Background(), chunk)
		s.apply(batch)
		if err != nil {
			s.budget.stores.Add(-1)
			return nil, err
		}
	}
	return &Buffer{clock: time.Now, big: s}, nil
}

// Large reports whether the buffer is backed by a file source instead of
// memory.
func (b *Buffer) Large() bool { return b.big != nil }

// refuse records why the last read or edit was refused. Inside Group it also
// marks the group as refused.
func (b *Buffer) refuse(err error) {
	b.big.refusal = err
	if b.depth > 0 {
		b.txnRefused = true
	}
}

// bigText turns the result of a large read into the plain Text/Range result:
// an over-cap read is refused and returns no text.
func (b *Buffer) bigText(text string, err error) string {
	if errors.Is(err, ErrOverCap) {
		b.refuse(err)
	}
	return text
}

// CheckSource compares the source with its state at open time. A source that
// shrank or was rewritten in place (ErrSourceChanged) freezes the buffer:
// cached lines keep displaying, nothing more is read. A source that only grew
// (ErrSourceGrew) is noted and keeps working. A memory buffer returns nil.
func (b *Buffer) CheckSource() error {
	s := b.big
	if s == nil {
		return nil
	}
	if s.err != nil {
		return s.err
	}
	switch err := s.src.src.Check(); err {
	case nil:
		return nil
	case ErrSourceGrew:
		s.grew = true
		return err
	default:
		s.fail(ErrSourceChanged)
		return ErrSourceChanged
	}
}
