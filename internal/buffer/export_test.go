package buffer

import (
	"slices"
	"testing"
)

// Hooks for the black-box tests in package buffer_test.

// SetMaterializeCap lowers the Text/Range cap for the duration of the test.
func SetMaterializeCap(t testing.TB, n int64) {
	t.Helper()
	old := materializeCap
	materializeCap = n
	t.Cleanup(func() { materializeCap = old })
}

// SetIntSize makes OpenLarge believe int has n bits for the test.
func SetIntSize(t testing.TB, n int) {
	t.Helper()
	old := intSize
	intSize = n
	t.Cleanup(func() { intSize = old })
}

// JumpScanned marks the first n chunks as indexed, each holding perChunk
// newlines, without reading the source. It lets a test address offsets far
// into a huge generated source.
func (b *Buffer) JumpScanned(n int, perChunk int64) {
	g := &b.big.g
	for c := 1; c <= n; c++ {
		g.nl[c] = int64(c) * perChunk
	}
	g.scanned = n
}

// PageCount is the number of cached source pages.
func (b *Buffer) PageCount() int { return b.big.pages.len() }

// PageCap is the number of pages the buffer may keep right now.
func (b *Buffer) PageCap() int { return b.big.budget.pages(b.big.g.chunk, b.big.pages.want) }

// DecodedCount is the number of decoded lines in the cache.
func (b *Buffer) DecodedCount() int { return len(b.big.dec.byLine) }

// TxnRefused reports the refusal mark of the open Group.
func (b *Buffer) TxnRefused() bool { return b.txnRefused }

// SetWriteStep lowers the WriteTo copy and context-check step for the test.
func SetWriteStep(t testing.TB, n int64) {
	t.Helper()
	old := writeStep
	writeStep = n
	t.Cleanup(func() { writeStep = old })
}

// GridNL is a copy of the cumulative newline counts of the line index.
func (b *Buffer) GridNL() []int64 { return slices.Clone(b.big.g.nl) }

// GridSkip is the number of leading source bytes that are not text.
func (b *Buffer) GridSkip() int64 { return b.big.g.skip }

// Stores is the number of open large buffers counted by the budget.
func (p *PageBudget) Stores() int { return int(p.stores.Load()) }
