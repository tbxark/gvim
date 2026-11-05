package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tbxark/gvim/internal/finder"
	"github.com/tbxark/gvim/internal/theme"
	"github.com/tbxark/gvim/internal/ui/grid"
	"github.com/tbxark/gvim/internal/ui/palette"
)

// fakeIndex is a palette.Files over a fixed list: the finished index of a large
// project.
type fakeIndex struct{ files []string }

func (f fakeIndex) Snapshot() ([]string, bool) { return f.files, false }

// bigProject returns the cap-sized list of paths a finished index would hold.
func bigProject() []string {
	out := make([]string, finder.MaxFiles)
	for i := range out {
		out[i] = fmt.Sprintf("pkg%03d/sub%02d/module_%05d.go", i%400, i%37, i)
	}
	return out
}

// TestFileRankerFiltersFiftyThousandPaths measures the file mode's filter over
// the cap-sized list. The budget is generous, because the race detector slows
// the scorer; the measured times are logged.
func TestFileRankerFiltersFiftyThousandPaths(t *testing.T) {
	const budget = 5 * time.Second
	files := bigProject()
	idx := fakeIndex{files: files}
	rk := newFileRanker()
	var _ palette.Files = idx

	queries := []string{"", "module", "pkg17 sub", "m0042"}
	start := time.Now()
	for _, q := range queries {
		t0 := time.Now()
		got := rk.Rank(q, idx.files, 100)
		d := time.Since(t0)
		t.Logf("Rank(%q) over %d paths: %v, %d matches", q, len(files), d, len(got))
		if d > budget {
			t.Errorf("Rank(%q) took %v, budget %v", q, d, budget)
		}
		if len(got) == 0 || len(got) > 100 {
			t.Errorf("Rank(%q) returned %d matches, want 1..100", q, len(got))
		}
	}
	t.Logf("total for %d queries: %v", len(queries), time.Since(start))
}

func TestFileRankerCapsAndMapsMatches(t *testing.T) {
	rk := newFileRanker()
	got := rk.Rank("go", []string{"a/b.go", "readme.md", "go.mod"}, 1)
	if len(got) != 1 {
		t.Fatalf("limit 1 returned %d matches", len(got))
	}
	if got[0].Path != "go.mod" || len(got[0].Indexes) != 2 || got[0].Indexes[0] != 0 {
		t.Fatalf("match = %+v, want go.mod with indexes starting at 0", got[0])
	}
}

func TestFileRankerRecordedOpensRankFirst(t *testing.T) {
	rk := newFileRanker()
	files := []string{"x/main.go", "y/main.go"}
	rk.Record("y/main.go")
	got := rk.Rank("main", files, 10)
	if len(got) != 2 || got[0].Path != "y/main.go" {
		t.Fatalf("after an open, Rank = %+v, want y/main.go first", got)
	}
}

func TestStartFilesIndexesTheRoot(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"a.txt", filepath.Join("sub", "b.go")} {
		p := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	idx := startFiles(context.Background(), root)
	defer idx.Close()
	<-idx.Done()
	files, loading := idx.Snapshot()
	if loading || len(files) != 2 {
		t.Fatalf("Snapshot = %v, loading %v; want 2 files, done", files, loading)
	}
}

// loadingIndex is a palette.Files whose enumeration has not finished: it holds
// the files it has and reports loading.
type loadingIndex struct{ files []string }

func (l loadingIndex) Snapshot() ([]string, bool) { return l.files, true }

// TestPaletteFileModeRanksAFakeIndex runs the palette's file mode over a fake
// index through the real ranker. The loading row shows while the index runs, and
// the matched paths show once the palette ranks a finished index.
func TestPaletteFileModeRanksAFakeIndex(t *testing.T) {
	screen := func(p *palette.Palette) string {
		g := grid.New(80, 24)
		p.Draw(g)
		return strings.Join(g.Lines(), "\n")
	}
	open := func(files palette.Files) *palette.Palette {
		return palette.New(palette.Options{Root: "/ws", Files: files, Ranker: newFileRanker(), Theme: theme.Theme{}})
	}

	loading := open(loadingIndex{})
	loading.Update(loading.Init()())
	if got := screen(loading); !strings.Contains(got, "Loading files…") {
		t.Fatalf("an unfinished index shows no loading row:\n%s", got)
	}

	idx := fakeIndex{files: []string{"cmd/main.go", "internal/app/ui.go", "README.md"}}
	p := open(idx)
	p.Update(p.Init()())
	got := screen(p)
	if !strings.Contains(got, "internal/app/ui.go") || !strings.Contains(got, "README.md") {
		t.Fatalf("the file rows do not show the fake index:\n%s", got)
	}
	if strings.Contains(got, "Loading files…") {
		t.Errorf("a finished index still shows the loading row:\n%s", got)
	}
}
