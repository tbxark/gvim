package editor

import "testing"

func TestGotoMatchingBracket(t *testing.T) {
	t.Parallel()
	tests := []struct {
		Name     string
		Text     string
		Line     int // caret line before the jump
		Col      int // caret column before the jump
		wantLine int
		wantCol  int
		wantErr  bool
	}{
		{Name: "open paren jumps to its close", Text: "f(a[1])", Col: 1, wantCol: 6},
		{Name: "close paren jumps to its open", Text: "f(a[1])", Col: 6, wantCol: 1},
		{Name: "square brackets", Text: "f(a[1])", Col: 3, wantCol: 5},
		{Name: "braces", Text: "{[x]}", Col: 0, wantCol: 4},
		{Name: "caret just after a close bracket", Text: "f(a)", Col: 4, wantCol: 1},
		{Name: "nested pairs of one kind", Text: "((a)(b))", Col: 0, wantCol: 7},
		{Name: "match on a later line", Text: "if (\n  x\n)", Line: 0, Col: 3, wantLine: 2, wantCol: 0},
		{Name: "no bracket at or before the caret", Text: "abc", Col: 1, wantErr: true},
		{Name: "unmatched open bracket", Text: "(abc", Col: 0, wantErr: true},
		{Name: "a bracket of another kind does not match", Text: "(]", Col: 0, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			t.Parallel()
			e := withDoc(tt.Text)
			marksCaret(e, tt.Line, tt.Col)
			r := e.Do("goto_matching_bracket", nil)
			if (r.Err != nil) != tt.wantErr {
				t.Fatalf("goto_matching_bracket err = %v, want error %v", r.Err, tt.wantErr)
			}
			if tt.wantErr {
				if got := e.Active().Cursor; got.Line != tt.Line || got.Col != tt.Col {
					t.Fatalf("caret moved to %+v on failure, want line %d col %d", got, tt.Line, tt.Col)
				}
				return
			}
			c := e.Active().Cursor
			if c.Line != tt.wantLine || c.Col != tt.wantCol {
				t.Fatalf("caret = line %d col %d, want line %d col %d", c.Line, c.Col, tt.wantLine, tt.wantCol)
			}
		})
	}
}
