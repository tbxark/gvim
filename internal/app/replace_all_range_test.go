package app

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/tbxark/gvim/buffer"
	"github.com/tbxark/gvim/editor"
	"github.com/tbxark/gvim/search"
)

// TestReplaceAllThroughTheAdapterRespectsFindInSelection checks the path the find bar
// takes: the adapter's ReplaceMatches replaces only the matches inside the find range,
// which are the matches the bar counts, and the text outside the selection is unchanged.
func TestReplaceAllThroughTheAdapterRespectsFindInSelection(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "a.txt")
	must(t, os.WriteFile(path, []byte("a\na\na\n"), 0o644))
	ed := editor.New(editor.Options{})
	must(t, ed.OpenFile(path))
	a := editorAdapter{ed: ed, spans: &spanCache{}, others: map[*editor.Document]*spanCache{}}

	a.SetCursor(buffer.Pos{Line: 1}, false)
	a.SetCursor(buffer.Pos{Line: 1, Col: 1}, true)
	if res := a.Do("find_in_selection", nil); res.Err != nil {
		t.Fatalf("find_in_selection: %v", res.Err)
	}
	q := search.Query{Text: "a", CaseSensitive: true}

	rng, limited := a.SearchRange()
	if !limited {
		t.Fatal("SearchRange reports no range after find_in_selection")
	}
	all, err := a.FindMatches(q)
	if err != nil {
		t.Fatalf("FindMatches: %v", err)
	}
	from, to := rng.Ordered()
	count := 0
	for _, m := range all {
		if !m.Start.Less(from) && !to.Less(m.End) {
			count++
		}
	}

	n, err := a.ReplaceMatches(q, "b")
	if err != nil {
		t.Fatalf("ReplaceMatches: %v", err)
	}
	if n != count || n != 1 {
		t.Fatalf("replaced = %d, counted = %d, want both 1", n, count)
	}
	if got := ed.Active().Buffer.Text(); got != "a\nb\na\n" {
		t.Fatalf("text = %q, want the match outside the selection kept", got)
	}
}
