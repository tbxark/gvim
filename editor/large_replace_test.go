package editor

import (
	"errors"
	"strings"
	"testing"

	"github.com/tbxark/gvim/buffer"
	"github.com/tbxark/gvim/search"
)

func TestLargeReplaceMatchReplacesOneMatch(t *testing.T) {
	t.Parallel()
	e, d := openIndexed(t, largeOptions(), lineText(300))
	before := d.Buffer.Stats()
	q := search.Query{Text: `line (\d+)`, CaseSensitive: true, Regex: true}
	m := search.Match{Start: buffer.Pos{Line: 250}, End: buffer.Pos{Line: 250, Col: 9}}

	got, err := e.ReplaceMatch(q, "row $1", m)
	if err != nil || got != "row 0250" {
		t.Fatalf("ReplaceMatch = %q, %v; want row 0250", got, err)
	}
	if line := d.Buffer.Line(250); line != "row 0250" || d.Buffer.Line(249) != "line 0249" || d.Buffer.Line(251) != "line 0251" {
		t.Errorf("lines 249-251 = %q, %q, %q", d.Buffer.Line(249), d.Buffer.Line(250), d.Buffer.Line(251))
	}
	if !d.Modified() {
		t.Error("the document is not modified")
	}
	st := d.Buffer.Stats()
	if st.OverCap != 0 || st.MaterializedBytes-before.MaterializedBytes > 64 {
		t.Errorf("OverCap %d, %d bytes materialized: the whole text was read", st.OverCap, st.MaterializedBytes-before.MaterializedBytes)
	}

	// One undo step reverts it.
	e.Do("undo", nil)
	if d.Buffer.Line(250) != "line 0250" {
		t.Errorf("line 250 after undo = %q", d.Buffer.Line(250))
	}
}

func TestLargeReplaceMatchNoMatchAndReadOnly(t *testing.T) {
	t.Parallel()
	e, d := openIndexed(t, largeOptions(), lineText(100))
	version := d.Buffer.Version()
	stale := search.Match{Start: buffer.Pos{Line: 5}, End: buffer.Pos{Line: 5, Col: 4}}
	if _, err := e.ReplaceMatch(search.Query{Text: "nope"}, "x", stale); !errors.Is(err, search.ErrNoMatch) {
		t.Fatalf("ReplaceMatch of a stale match = %v, want ErrNoMatch", err)
	}
	assertUntouched(t, d, version)

	d.readOnly = true
	if _, err := e.ReplaceMatch(search.Query{Text: "line"}, "x", stale); !errors.Is(err, ErrReadOnly) {
		t.Fatalf("ReplaceMatch in a read-only document = %v, want ErrReadOnly", err)
	}
	assertUntouched(t, d, version)
}

func TestLargeReplaceMatchRefusals(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		content string
		line    int
		wantErr error
		want    string // part of the status
	}{
		{"line not valid UTF-8", withHead("ok\nfoo \xff bar\nlast\n"), headLines + 1, buffer.ErrInvalidUTF8, "not valid UTF-8"},
		{"line too long", withHead("ok\n" + "foo " + strings.Repeat("y", 200) + "\nlast\n"), headLines + 1, buffer.ErrLineTooLong, "not editable"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			e, d := openIndexed(t, largeOptions(), tt.content)
			version := d.Buffer.Version()
			m := search.Match{Start: buffer.Pos{Line: tt.line}, End: buffer.Pos{Line: tt.line, Col: 3}}
			d.Cursor = m.Start
			_, err := e.ReplaceMatch(search.Query{Text: "foo"}, "x", m)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("ReplaceMatch = %v, want %v", err, tt.wantErr)
			}
			if !strings.Contains(e.Status(), tt.want) {
				t.Errorf("status = %q, want it to contain %q", e.Status(), tt.want)
			}
			assertUntouched(t, d, version)
		})
	}
}

// Replace All and a find-selection search still list every match, which a large
// document does not allow.
func TestLargeSearchActionsStayRefused(t *testing.T) {
	t.Parallel()
	e, d := openIndexed(t, largeOptions(), lineText(200))
	version := d.Buffer.Version()
	if _, err := e.ReplaceMatches(search.Query{Text: "line"}, "x"); !errors.Is(err, ErrLargeFile) {
		t.Errorf("ReplaceMatches = %v, want ErrLargeFile", err)
	}
	if r := e.Do("find_selection_next", nil); !errors.Is(r.Err, ErrLargeFile) {
		t.Errorf("find_selection_next = %v, want ErrLargeFile", r.Err)
	}
	assertUntouched(t, d, version)
}
