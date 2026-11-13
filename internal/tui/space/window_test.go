package space

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestWindow(t *testing.T) {
	t.Parallel()
	long := strings.Repeat("abcdefghij", 12)
	wide := strings.Repeat("中文字符", 20)
	tests := []struct {
		name  string
		value string
		pos   int
		room  int
	}{
		{"fits", "abc", 3, 10},
		{"caret at the end", long, len([]rune(long)), 30},
		{"caret in the middle", long, 60, 30},
		{"caret at the start", long, 0, 30},
		{"wide runes at the end", wide, len([]rune(wide)), 31},
		{"wide runes in the middle", wide, 33, 20},
		{"tiny room", long, len([]rune(long)), 2},
	}
	for _, tt := range tests {
		for room := tt.room; room >= max(tt.room-3, 1); room-- {
			shown, caret := Window(tt.value, tt.pos, room)
			if got := ansi.StringWidth(shown); got > room {
				t.Errorf("%s room %d: %q is %d cells wide", tt.name, room, shown, got)
			}
			if caret < 0 || caret >= room {
				t.Errorf("%s room %d: caret column %d is outside the room", tt.name, room, caret)
			}
			// The cells left of the caret are the runes just before it.
			before := string([]rune(tt.value)[:tt.pos])
			head := ansi.Truncate(shown, caret, "")
			if ansi.StringWidth(head) != caret || !strings.HasSuffix(before, head) {
				t.Errorf("%s room %d: shown %q with caret %d does not end at the caret in %q", tt.name, room, shown, caret, before)
			}
		}
	}
}

func TestWindowShowsTheEndOfTheTypedText(t *testing.T) {
	t.Parallel()
	value := "this is a very long query with 中文字符 that overflows the find bar"
	shown, caret := Window(value, len([]rune(value)), 20)
	if !strings.HasSuffix(value, shown) || caret != ansi.StringWidth(shown) {
		t.Errorf("Window = (%q, %d), want the end of the value with the caret after it", shown, caret)
	}
	if shown, caret := Window("abc", 1, 10); shown != "abc" || caret != 1 {
		t.Errorf("a value that fits: Window = (%q, %d), want (abc, 1)", shown, caret)
	}
	if shown, caret := Window("abc", 1, 0); shown != "" || caret != 0 {
		t.Errorf("no room: Window = (%q, %d)", shown, caret)
	}
}

// TestWindowRangeMatchesWindow checks that the rune range is the part Window shows.
func TestWindowRangeMatchesWindow(t *testing.T) {
	t.Parallel()
	long := strings.Repeat("abcdefghij", 12)
	wide := strings.Repeat("中文字符", 20)
	for _, value := range []string{"", "abc", long, wide} {
		runes := []rune(value)
		for _, pos := range []int{0, 1, len(runes) / 2, len(runes)} {
			for _, room := range []int{0, 1, 2, 9, 30} {
				start, end := WindowRange(value, pos, room)
				shown, _ := Window(value, pos, room)
				if start < 0 || end < start || end > len(runes) || string(runes[start:end]) != shown {
					t.Errorf("WindowRange(%q, %d, %d) = [%d, %d), Window shows %q", value, pos, room, start, end, shown)
				}
			}
		}
	}
}
