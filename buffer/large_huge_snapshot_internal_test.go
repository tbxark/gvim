package buffer

import (
	"math/rand/v2"
	"testing"
)

// A snapshot reads a huge line through an index of its own, built with the stride and chunk
// of the buffer, so its windows agree with the runes at every stride, as the buffer's do.
func TestHugeLineSnapshotWindowsAgreeAtEveryStride(t *testing.T) {
	t.Parallel()
	for _, geom := range []struct{ stride, chunk int }{{4096, 4 << 20}, {7, 100}, {1, 64}, {50, 33}} {
		rng := rand.New(rand.NewPCG(uint64(geom.stride), uint64(geom.chunk)))
		runes := []rune(hugeText(rng, 700, []rune("a é 日本 😀 z")))
		b := tinyOpen(t, "head\n"+string(runes)+"\ntail")
		b.big.strideOver, b.big.chunkOver = geom.stride, geom.chunk
		indexFully(t, b)
		snap := b.Snapshot()
		for range 200 {
			col, n := rng.IntN(len(runes)+20)-10, rng.IntN(150)
			lo := min(max(col, 0), len(runes))
			hi := min(lo+n, len(runes))
			if got, want := snap.LineRunes(1, col, n), string(runes[lo:hi]); got != want {
				t.Fatalf("stride %d: LineRunes(%d, %d) = %q, want %q", geom.stride, col, n, got, want)
			}
		}
		if err := snap.Err(); err != nil {
			t.Fatalf("stride %d: Err = %v", geom.stride, err)
		}
		snap.Release()
	}
}
