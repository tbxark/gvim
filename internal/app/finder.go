package app

import (
	"context"

	"github.com/tbxark/gvim/internal/finder"
	"github.com/tbxark/gvim/internal/ui/palette"
)

// Compile-time checks: the file index and the ranker satisfy the ports the
// palette defines.
var (
	_ palette.Files  = (*finder.Index)(nil)
	_ palette.Ranker = fileRanker{}
)

// startFiles starts the file index for root in the background and returns at
// once. The palette reads it through Snapshot. The caller closes the index when
// the program ends, which stops enumeration.
func startFiles(ctx context.Context, root string) *finder.Index {
	return finder.Start(ctx, root)
}

// fileRanker is the palette's Ranker over internal/finder. It holds the frecency
// of the files opened through the picker. It is safe for concurrent use, since
// the palette ranks on a command goroutine.
type fileRanker struct {
	r *finder.Ranker
}

// newFileRanker returns a ranker that measures frecency on the wall clock.
func newFileRanker() fileRanker {
	return fileRanker{r: finder.NewRanker(nil)}
}

// Rank scores files against query and returns at most limit matches, best
// first. The finder caps the result at finder.MaxResults.
func (f fileRanker) Rank(query string, files []string, limit int) []palette.Ranked {
	matches := f.r.Rank(query, files, limit)
	out := make([]palette.Ranked, len(matches))
	for i, m := range matches {
		out[i] = palette.Ranked{Path: m.Path, Indexes: m.Positions}
	}
	return out
}

// Record notes that the file at rel, relative to the root, was opened from the
// picker. The root calls it when an OpenFileMsg is handled.
func (f fileRanker) Record(rel string) {
	f.r.Record(rel)
}
