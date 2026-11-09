package app

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/tbxark/gvim/internal/apptest"
	"github.com/tbxark/gvim/session"
)

// TestMergeExtractMru: moved documents go in front (newest first), an old ref for a
// moved path cannot duplicate it, and unrelated existing refs keep their order; nil on
// either side keeps the other side.
func TestMergeExtractMru(t *testing.T) {
	cases := []struct {
		name     string
		existing []session.MruFile
		moved    []session.MruFile
		want     []session.MruFile
	}{
		{
			name:     "moved in front, stale ref dropped",
			existing: []session.MruFile{{Path: "a"}, {Path: "c"}},
			moved:    []session.MruFile{{Path: "b"}, {Path: "a"}},
			want:     []session.MruFile{{Path: "b"}, {Path: "a"}, {Path: "c"}},
		},
		{name: "no existing MRU", moved: []session.MruFile{{Path: "b"}}, want: []session.MruFile{{Path: "b"}}},
		{name: "no moved files", existing: []session.MruFile{{Path: "a"}}, want: []session.MruFile{{Path: "a"}}},
		{
			name:     "pathless refs are kept",
			existing: []session.MruFile{{Label: "scratch"}, {Path: "a"}},
			moved:    []session.MruFile{{Path: "b"}},
			want:     []session.MruFile{{Path: "b"}, {Label: "scratch"}, {Path: "a"}},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := mergeExtractMru(tc.existing, tc.moved)
			if !slices.Equal(got, tc.want) {
				t.Fatalf("mergeExtractMru = %+v, want %+v", got, tc.want)
			}
		})
	}
}

// TestExtractMruUsesTheSourceRecency: the refs carried to a new workspace are the source
// editor's MRU order, newest first, not the extraction's path order.
func TestExtractMruUsesTheSourceRecency(t *testing.T) {
	apptest.IsolateConfig(t)
	dir := t.TempDir()
	a := filepath.Join(dir, "a.txt")
	b := filepath.Join(dir, "b.txt")
	c := filepath.Join(dir, "c.txt")
	for _, p := range []string{a, b, c} {
		apptest.Must(t, os.WriteFile(p, []byte("x\n"), 0o644))
	}
	ed := apptest.NewSessionEditor()
	for _, p := range []string{a, b, c} {
		apptest.Must(t, ed.OpenFile(p))
	}
	ed.ActivateTab(0)
	ed.ActivateTab(1)
	got := extractMru(ed, map[string]bool{a: true, b: true, c: true})
	want := []session.MruFile{{Path: b}, {Path: a}, {Path: c}}
	if !slices.Equal(got, want) {
		t.Fatalf("extractMru = %+v, want %+v", got, want)
	}
	// A subset keeps the relative recency order and ignores unknown paths.
	got = extractMru(ed, map[string]bool{a: true, c: true, filepath.Join(dir, "missing"): true})
	want = []session.MruFile{{Path: a}, {Path: c}}
	if !slices.Equal(got, want) {
		t.Fatalf("subset extractMru = %+v, want %+v", got, want)
	}
}
