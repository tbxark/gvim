package search

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/tbxark/gvim/buffer"
)

// editsOf plans the replacement and returns the edits as text/positions for a compact
// comparison.
func editsOf(t *testing.T, text string, q Query, repl string) []string {
	t.Helper()
	edits, err := PlanReplace(buffer.New(text), q, repl)
	if err != nil {
		t.Fatalf("PlanReplace(%q, %+v, %q): %v", text, q, repl, err)
	}
	out := make([]string, len(edits))
	for i, e := range edits {
		out[i] = fmt.Sprintf("%d:%d->%d:%d=%s", e.From.Line, e.From.Col, e.To.Line, e.To.Col, e.Text)
	}
	return out
}

func TestPlanReplaceLiteral(t *testing.T) {
	got := editsOf(t, "foo bar foo", Query{Text: "foo"}, "x")
	want := []string{"0:0->0:3=x", "0:8->0:11=x"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("edits = %q, want %q", got, want)
	}
}

func TestPlanReplaceRegexExpansion(t *testing.T) {
	got := editsOf(t, "a1 b2", Query{Text: "([a-z])([0-9])", Regex: true}, "$2$1")
	want := []string{"0:0->0:2=1a", "0:3->0:5=2b"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("edits = %q, want %q", got, want)
	}
	// A named group expands too.
	got = editsOf(t, "one two", Query{Text: "(?P<w>[a-z]+)", Regex: true}, "${w}!")
	want = []string{"0:0->0:3=one!", "0:4->0:7=two!"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("named group edits = %q, want %q", got, want)
	}
}

func TestPlanReplaceEscapes(t *testing.T) {
	// A regex replacement reads the C-style escapes, as the in-buffer replace does.
	got := editsOf(t, "a", Query{Text: "a", Regex: true}, "\\n\\t\\r\\\\")
	want := []string{"0:0->0:1=\n\t\r\\"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("escaped edits = %q, want %q", got, want)
	}
	// A literal replacement is inserted unchanged, escapes included.
	got = editsOf(t, "a", Query{Text: "a"}, "\\n")
	want = []string{`0:0->0:1=\n`}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("literal escaped edits = %q, want %q", got, want)
	}
}

func TestPlanReplaceNoMatchAndBadRegex(t *testing.T) {
	edits, err := PlanReplace(buffer.New("abc"), Query{Text: "zzz"}, "x")
	if err != nil || edits != nil {
		t.Fatalf("no match = %v, %v; want nil, nil", edits, err)
	}
	if _, err := PlanReplace(buffer.New("abc"), Query{Text: "(", Regex: true}, "x"); err == nil {
		t.Fatal("an invalid regex gave no error")
	}
}
