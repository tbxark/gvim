package uiadapter

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tbxark/gvim/buffer"
	"github.com/tbxark/gvim/editor"
	"github.com/tbxark/gvim/internal/apptest"
	"github.com/tbxark/gvim/search"
)

// longText is text of n runes of words, ending in the marker.
func longText(n int, marker string) string {
	var b strings.Builder
	for k := 0; b.Len() < n; k++ {
		b.WriteString(`{"k":"v`)
		b.WriteString(strings.Repeat("a", k%7))
		b.WriteString(`"}, `)
	}
	return b.String()[:n] + marker
}

// A match past the first MiB of a huge line of a large file is found by the find bar's scan
// and by F3, at the column the line really has it. A large buffer used to read a line only as
// far as its 1 MiB cap, so the match was reported as absent.
func TestSearchReachesAMatchPastTheLineCapOfALargeFile(t *testing.T) {
	apptest.IsolateConfig(t)
	const at = 2 << 20
	const needle = "NEEDLE-FAR"
	content := longText(11<<20, "")
	content = content[:at] + needle + content[at+len(needle):]
	path := filepath.Join(t.TempDir(), "far.json")
	apptest.Must(t, os.WriteFile(path, []byte(content), 0o644))
	ed := editor.New(editor.Options{Empty: true, LargeFileMode: true, LargeFileThresholdBytes: 1024})
	apptest.Must(t, ed.OpenFile(path))
	t.Cleanup(ed.Close)
	if d := ed.Active(); !d.Large() {
		t.Fatalf("%d bytes did not open in large file mode", len(content))
	}
	for !ed.Active().Index().Complete {
		job, ok := ed.PlanIndex()
		if !ok {
			t.Fatal("no index job for an incomplete index")
		}
		ed.ApplyIndex(job.Run(t.Context()))
	}
	a := &Editor{ed: ed}
	q := search.Query{Text: needle}
	want := search.Match{Start: buffer.Pos{Line: 0, Col: at}, End: buffer.Pos{Line: 0, Col: at + len(needle)}}

	sc, err := a.StartScan(q, 0)
	apptest.Must(t, err)
	defer sc.Close()
	for done := false; !done; {
		if done, err = sc.Step(time.Minute); err != nil {
			t.Fatal(err)
		}
	}
	if got := sc.Matches(); len(got) != 1 || got[0] != want {
		t.Fatalf("scan matches = %v, want [%v]", got, want)
	}
}
