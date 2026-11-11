package outline

import (
	"strings"
	"testing"
)

// linesOf splits a document written in a test on newlines, the way a buffer
// holds it: no trailing empty line unless the text ends with a newline.
func linesOf(text string) []string {
	return strings.Split(strings.TrimSuffix(text, "\n"), "\n")
}

// headingAt is the byte offset of line i in lines, the values Scan reports.
func headingAt(lines []string, i int) int {
	off := 0
	for j := 0; j < i; j++ {
		off += len(lines[j]) + 1
	}
	return off
}

func TestScanHeadings(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		text string
		want []Heading
	}{
		{
			name: "atx levels and titles",
			text: "# One\n## Two\n### Three\n#### Four\n##### Five\n###### Six\n",
			want: []Heading{
				{Line: 0, Level: 1, Title: "One", HasChildren: true, EndLine: 5},
				{Line: 1, Level: 2, Title: "Two", HasChildren: true, EndLine: 5},
				{Line: 2, Level: 3, Title: "Three", HasChildren: true, EndLine: 5},
				{Line: 3, Level: 4, Title: "Four", HasChildren: true, EndLine: 5},
				{Line: 4, Level: 5, Title: "Five", HasChildren: true, EndLine: 5},
				{Line: 5, Level: 6, Title: "Six", EndLine: 5},
			},
		},
		{
			name: "closing hashes stripped only after a space",
			text: "## Title ##\n## C#\n## ###\n",
			want: []Heading{
				{Line: 0, Level: 2, Title: "Title", EndLine: 0},
				{Line: 1, Level: 2, Title: "C#", EndLine: 1},
				// All hashes is content, as Fresh and the Lua plugin read it:
				// the markers are only stripped when the run is a closing one.
				{Line: 2, Level: 2, Title: "###", EndLine: 2},
			},
		},
		{
			name: "not headings",
			text: "####### seven\n#no space\n  # two spaces is a heading\n    ## four spaces is not\ntext # tag\n",
			want: []Heading{
				{Line: 2, Level: 1, Title: "two spaces is a heading", EndLine: 4},
			},
		},
		{
			name: "fenced code is skipped",
			text: "# Real\n\n\u0060\u0060\u0060sh\n# not a heading\n## neither\n\u0060\u0060\u0060\n## After\n",
			want: []Heading{
				{Line: 0, Level: 1, Title: "Real", HasChildren: true, EndLine: 6},
				{Line: 6, Level: 2, Title: "After", EndLine: 6},
			},
		},
		{
			name: "a closer matches its fence character and length",
			text: "~~~\n# code\n\u0060\u0060\u0060\n# still code\n~~~\n# Heading\n",
			want: []Heading{
				{Line: 5, Level: 1, Title: "Heading", EndLine: 5},
			},
		},
		{
			name: "setext headings",
			text: "Title\n=====\n\nSection\n-------\n\ntext\n",
			want: []Heading{
				{Line: 0, Level: 1, Title: "Title", HasChildren: true, EndLine: 6},
				{Line: 3, Level: 2, Title: "Section", EndLine: 6},
			},
		},
		{
			name: "a rule after a blank line or a list item is not a setext heading",
			text: "text\n\n---\n\n- item\n---\n",
			want: nil,
		},
		{
			name: "a setext underline under an ATX heading is not a heading",
			text: "# Title\n---\n",
			want: []Heading{
				{Line: 0, Level: 1, Title: "Title", EndLine: 1},
			},
		},
		{
			name: "four spaces of indent is a code block, not a heading",
			text: "    # code block style\n",
			want: nil,
		},
		{
			name: "a tilde inside a backtick fence does not close it",
			text: "\u0060\u0060\u0060\n~~~\n# code\n\u0060\u0060\u0060\n",
			want: nil,
		},
		{
			name: "no headings",
			text: "plain text\nmore text\n",
			want: nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			lines := linesOf(tt.text)
			got := Scan(lines)
			if len(got) != len(tt.want) {
				t.Fatalf("headings = %+v, want %+v", got, tt.want)
			}
			for i := range got {
				g, w := got[i], tt.want[i]
				if w.Byte == 0 {
					w.Byte = headingAt(lines, w.Line)
				}
				if g.Byte != w.Byte || g.Line != w.Line || g.Level != w.Level || g.Title != w.Title || g.HasChildren != w.HasChildren || g.EndLine != w.EndLine {
					t.Errorf("heading %d = %+v, want %+v", i, g, w)
				}
			}
		})
	}
}

// TestScanSectionExtents checks the byte extents a fold over a section uses.
func TestScanSectionExtents(t *testing.T) {
	t.Parallel()
	text := "# A\nbody\n## B\nmore\n# C\nlast"
	got := Scan(linesOf(text))
	if len(got) != 3 {
		t.Fatalf("headings = %+v, want 3", got)
	}
	// "# A" opens the first section, which runs to the line before "# C":
	// "body" starts at byte 4 and "more" ends at byte 18.
	if got[0].BodyStart != 4 {
		t.Errorf("A body starts at %d, want 4", got[0].BodyStart)
	}
	if got[0].EndByte != 18 {
		t.Errorf("A section ends at %d, want 18", got[0].EndByte)
	}
	// B runs to the line before C, and C runs to the end of the document.
	if got[1].EndLine != 3 || got[2].EndLine != 5 {
		t.Errorf("sections end at %d and %d, want 3 and 5", got[1].EndLine, got[2].EndLine)
	}
	if got[2].EndByte != len(text) {
		t.Errorf("C ends at %d, want %d", got[2].EndByte, len(text))
	}
}

// fakeLines is a document for ScanDoc: its lines and no IO.
type fakeLines []string

func (f fakeLines) LineCount() int    { return len(f) }
func (f fakeLines) Line(i int) string { return f[i] }

func TestScanDocBounded(t *testing.T) {
	t.Parallel()
	doc := fakeLines{"# One", "# Two", "# Three"}
	// Sixteen bytes of lines, plus one newline each: twenty bytes in all.
	got, complete := ScanDoc(doc, 0)
	if !complete || len(got) != 3 {
		t.Fatalf("ScanDoc = %d headings, complete %v, want 3 and true", len(got), complete)
	}
	got, complete = ScanDoc(doc, 19)
	if complete || got != nil {
		t.Fatalf("over the cap: %d headings, complete %v, want nil and false", len(got), complete)
	}
	got, complete = ScanDoc(doc, 20)
	if !complete || len(got) != 3 {
		t.Fatalf("at the cap: %d headings, complete %v, want 3 and true", len(got), complete)
	}
}

func TestScanDoesNotKeepAWholeLongTitle(t *testing.T) {
	t.Parallel()
	long := "# " + strings.Repeat("x", MaxTitleBytes*2)
	got := Scan([]string{long})
	if len(got) != 1 {
		t.Fatalf("headings = %d, want 1", len(got))
	}
	if len(got[0].Title) != MaxTitleBytes {
		t.Fatalf("title is %d bytes, want %d", len(got[0].Title), MaxTitleBytes)
	}
}

func TestScanSkipsYAMLFrontMatter(t *testing.T) {
	lines := []string{"---", "title: Foo", "# not a heading", "---", "", "# Real", "text"}
	heads := Scan(lines)
	if len(heads) != 1 || heads[0].Title != "Real" || heads[0].Line != 5 {
		t.Fatalf("headings = %+v, want only Real on line 5", heads)
	}
	// A "---" that never closes is not front matter.
	open := Scan([]string{"---", "# Title"})
	if len(open) != 1 || open[0].Title != "Title" {
		t.Fatalf("unclosed fence headings = %+v, want Title", open)
	}
}
