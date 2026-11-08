package lsp

import "testing"

func TestExpandSnippet(t *testing.T) {
	tests := []struct {
		name    string
		snippet string
		text    string
		cursor  int
	}{
		{"plain text", "hello world", "hello world", 11},
		{"final stop in call", "foo($0)", "foo()", 4},
		{"tabstop is empty", "foo($1)", "foo()", 5},
		{"numbered tabstops expand empty", "a$1b$2c", "abc", 3},
		{"placeholder default", "foo($" + "{1:arg})", "foo(arg)", 8},
		{"final placeholder before default", "foo($" + "{0:end})", "foo(end)", 4},
		{"choices use the first", "x$" + "{1|one,two,three|}", "xone", 4},
		{"escaped dollar", "cost \\$5", "cost $5", 7},
		{"escaped braces", "a\\}b", "a}b", 3},
		{"unterminated brace", "a$" + "{1:bc", "abc", 3},
		{"a lone dollar stays", "a$b", "a$b", 3},
		{"a trailing tabstop expands empty", "$5", "", 0},
		{"last final stop wins", "a$0b$0c", "abc", 2},
		{"nested default", "x$" + "{1:$" + "{2:y}}", "xy", 2},
		{"rune offsets for CJK", "x($" + "{1:参数}$0)", "x(参数)", 4},
		{"rune offsets for emoji", "e$0moji", "emoji", 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExpandSnippet(tt.snippet)
			if got.Text != tt.text || got.CursorOffset != tt.cursor {
				t.Fatalf("ExpandSnippet(%q) = %+v, want text %q cursor %d", tt.snippet, got, tt.text, tt.cursor)
			}
		})
	}
}

func TestIsSnippet(t *testing.T) {
	for _, tt := range []struct {
		text string
		want bool
	}{
		{"plain", false},
		{"$5", true},
		{"$" + "{1:x}", true},
		{"\\$5", false},
		{"cost \\$5 and $" + "{1:x}", true},
	} {
		if got := IsSnippet(tt.text); got != tt.want {
			t.Fatalf("IsSnippet(%q) = %v, want %v", tt.text, got, tt.want)
		}
	}
}
