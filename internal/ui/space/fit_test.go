package space

import (
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestFit(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		fn   func(string, int) string
		in   string
		w    int
		want string
	}{
		{"end fits", End, "abc", 3, "abc"},
		{"end cut", End, "abcdef", 4, "abc…"},
		{"end zero", End, "abc", 0, ""},
		{"middle fits", Middle, "abcdef", 6, "abcdef"},
		{"middle cut keeps both ends", Middle, "abcdefghij", 7, "abc…hij"},
		{"middle even room favours the head", Middle, "abcdefghij", 6, "abc…ij"},
		{"middle one cell", Middle, "abcdef", 1, "…"},
		{"path fits", Path, "a/b.go", 10, "a/b.go"},
		{"path keeps base and front", Path, "internal/search/deep/search.go", 22, "internal/…/search.go"},
		{"path keeps base when the front does not fit", Path, "internal/search/search.go", 12, "…/search.go"},
		{"path cuts a base that does not fit", Path, "dir/averyveryverylongbasename.go", 8, "averyve…"},
		{"path without slash", Path, "averylongname.go", 8, "averylo…"},
		{"name keeps extension", Name, "file_open_orchestrator.rs", 18, "file_open_orch….rs"},
		{"name fits", Name, "main.go", 7, "main.go"},
		{"name without extension", Name, "Makefile_long_name", 8, "Makefil…"},
		{"name with a long extension is cut at the end", Name, "a.verylongextension", 8, "a.veryl…"},
	}
	for _, tt := range tests {
		got := tt.fn(tt.in, tt.w)
		if got != tt.want {
			t.Errorf("%s: got %q, want %q", tt.name, got, tt.want)
		}
		if ansi.StringWidth(got) > max(tt.w, 0) {
			t.Errorf("%s: %q is %d cells, over %d", tt.name, got, ansi.StringWidth(got), tt.w)
		}
	}
}

func TestWrap(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		in   string
		w    int
		rows int
		want []string
	}{
		{"fits", "hello world", 20, 3, []string{"hello world"}},
		{"wraps at words", "hello world again", 11, 3, []string{"hello world", "again"}},
		{"last row ends with an ellipsis", "aaaa bbbb cccc dddd", 4, 2, []string{"aaaa", "bbb…"}},
		{"no room", "x", 0, 3, nil},
	}
	for _, tt := range tests {
		got := Wrap(tt.in, tt.w, tt.rows)
		if !slices.Equal(got, tt.want) {
			t.Errorf("%s: got %q, want %q", tt.name, got, tt.want)
		}
	}
}

func TestMiddleAndTailNeverOverflowOnWideRunes(t *testing.T) {
	t.Parallel()
	msg := "save /home/用户/文档/我的-项目/报告-最终版.txt: open /home/用户/文档/我的-项目/报告-最终版.txt: permission denied"
	for w := 1; w <= 110; w++ {
		if got := ansi.StringWidth(Middle(msg, w)); got > w {
			t.Errorf("Middle(msg, %d) is %d cells wide", w, got)
		}
		if got := ansi.StringWidth(Tail(msg, w)); got > w {
			t.Errorf("Tail(msg, %d) is %d cells wide", w, got)
		}
	}
	// The end of the message, the reason, survives the cut.
	if got := Middle(msg, 50); !strings.HasSuffix(got, "permission denied") {
		t.Errorf("Middle(msg, 50) = %q, want the end of the message", got)
	}
	if got := Tail("文档文档", 5); got != "文档" {
		t.Errorf("Tail cuts a wide rune: %q, want 文档", got)
	}
	// 8 cells of wide runes: head, "…" and tail fill 7 at best, never 9.
	if got := ansi.StringWidth(Middle("文档文档文档文档", 8)); got < 7 || got > 8 {
		t.Errorf("Middle of wide runes in 8 cells is %d cells wide, want 7 or 8", got)
	}
}
