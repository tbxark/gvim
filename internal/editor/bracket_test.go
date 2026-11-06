package editor

import "testing"

func TestGotoMatchingBracket(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		text     string
		line     int // caret line before the jump
		col      int // caret column before the jump
		wantLine int
		wantCol  int
		wantErr  bool
	}{
		{name: "open paren jumps to its close", text: "f(a[1])", col: 1, wantCol: 6},
		{name: "close paren jumps to its open", text: "f(a[1])", col: 6, wantCol: 1},
		{name: "square brackets", text: "f(a[1])", col: 3, wantCol: 5},
		{name: "braces", text: "{[x]}", col: 0, wantCol: 4},
		{name: "caret just after a close bracket", text: "f(a)", col: 4, wantCol: 1},
		{name: "nested pairs of one kind", text: "((a)(b))", col: 0, wantCol: 7},
		{name: "match on a later line", text: "if (\n  x\n)", line: 0, col: 3, wantLine: 2, wantCol: 0},
		{name: "no bracket at or before the caret", text: "abc", col: 1, wantErr: true},
		{name: "unmatched open bracket", text: "(abc", col: 0, wantErr: true},
		{name: "a bracket of another kind does not match", text: "(]", col: 0, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			e := withDoc(tt.text)
			marksCaret(e, tt.line, tt.col)
			r := e.Do("goto_matching_bracket", nil)
			if (r.Err != nil) != tt.wantErr {
				t.Fatalf("goto_matching_bracket err = %v, want error %v", r.Err, tt.wantErr)
			}
			if tt.wantErr {
				if got := e.Active().Cursor; got.Line != tt.line || got.Col != tt.col {
					t.Fatalf("caret moved to %+v on failure, want line %d col %d", got, tt.line, tt.col)
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
