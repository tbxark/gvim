package app

import (
	"testing"
	"time"

	"github.com/tbxark/gvim/buffer"
	"github.com/tbxark/gvim/search"
)

// A match past the first MiB of a huge line of a large file is found by the find bar's scan
// and by F3, at the column the line really has it. A large buffer used to read a line only as
// far as its 1 MiB cap, so the match was reported as absent.
func TestSearchReachesAMatchPastTheLineCapOfALargeFile(t *testing.T) {
	isolateConfig(t)
	const at = 2 << 20
	const needle = "NEEDLE-FAR"
	content := longText(11<<20, "")
	content = content[:at] + needle + content[at+len(needle):]
	rt := openLong(t, "far.json", content)
	if d := rt.ed.Active(); !d.Large() {
		t.Fatalf("%d bytes did not open in large file mode", len(content))
	}
	a := editorAdapter{ed: rt.ed}
	q := search.Query{Text: needle}
	want := search.Match{Start: buffer.Pos{Line: 0, Col: at}, End: buffer.Pos{Line: 0, Col: at + len(needle)}}

	sc, err := a.StartScan(q, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer sc.Close()
	for done := false; !done; {
		if done, err = sc.Step(time.Minute); err != nil {
			t.Fatal(err)
		}
	}
	if got := sc.Matches(); len(got) != 1 || got[0] != want {
		t.Fatalf("scan matches = %v, want [%v]", got, want)
	}

	var cursor search.Cursor
	for range 1000 {
		m, found, scanned, next, err := a.FindNext(q, buffer.Pos{}, false, cursor)
		if err != nil {
			t.Fatal(err)
		}
		if found {
			if m != want {
				t.Fatalf("F3 went to %v, want %v", m, want)
			}
			return
		}
		if scanned {
			t.Fatal("F3 found no match, although the find bar counts one")
		}
		cursor = next
	}
	t.Fatal("F3 did not reach the match")
}
