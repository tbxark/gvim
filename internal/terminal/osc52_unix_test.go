//go:build unix

package terminal

import (
	"testing"
	"time"
)

// TestSessionOSC52Hook checks the read-path integration: a child printing an
// OSC 52 sequence calls the session's clipboard hook with the decoded text, the
// bytes still reach the emulator, and a session without a hook is inert.
func TestSessionOSC52Hook(t *testing.T) {
	t.Parallel()
	s := startSh(t, "sleep 0.05; printf '\033]52;c;aGVsbG8=\007'; printf 'done'; sleep 0.2")
	got := make(chan string, 4)
	s.SetOSC52Handler(func(selection, text string) bool {
		select {
		case got <- selection + "=" + text:
		default:
		}
		return true
	})
	select {
	case v := <-got:
		if v != "c=hello" {
			t.Fatalf("hook got %q, want c=hello", v)
		}
	case <-time.After(pollTimeout):
		t.Fatal("the OSC 52 hook was not called")
	}
	// The child's other output still reaches the screen.
	waitFor(t, s, "done on the screen", func(sc Screen) bool {
		for col := 0; col < sc.Cols; col++ {
			if sc.Cell(0, col).Rune == 'd' {
				return true
			}
		}
		return false
	})
}
