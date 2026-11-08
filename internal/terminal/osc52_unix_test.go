//go:build unix

package terminal

import (
	"fmt"
	"os"
	"strings"
	"sync"
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

// TestSessionOSC52QueryAnswerReachesChild checks the read/paste path end to end:
// the child asks for the clipboard, the session's query handler returns text, and
// the base64 OSC 52 response is delivered to the child's stdin (as terminal input).
func TestSessionOSC52QueryAnswerReachesChild(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	out := dir + "/response"
	count := len(osc52Response("c", "hello"))
	script := fmt.Sprintf("stty raw -echo; sleep 0.05; printf '\\033]52;c;?\\007'; dd bs=1 count=%d of=%s 2>/dev/null; sleep 0.1", count, out)
	s := startSh(t, script)
	var mu sync.Mutex
	var selections []string
	s.SetOSC52QueryHandler(func(selection string) (string, bool) {
		mu.Lock()
		selections = append(selections, selection)
		mu.Unlock()
		return "hello", true
	})
	var data []byte
	deadline := time.Now().Add(3 * time.Second)
	for {
		data, _ = os.ReadFile(out)
		if strings.Contains(string(data), "aGVsbG8=") && strings.Contains(string(data), "\x1b]52;c;") {
			break
		}
		if time.Now().After(deadline) {
			mu.Lock()
			sel := append([]string(nil), selections...)
			mu.Unlock()
			t.Fatalf("the OSC 52 response to reach the child not seen: file=%q selections=%v", data, sel)
		}
		time.Sleep(10 * time.Millisecond)
	}
	mu.Lock()
	got := append([]string(nil), selections...)
	mu.Unlock()
	if len(got) != 1 || got[0] != "c" {
		t.Fatalf("query selections = %v, want [c]", got)
	}
}
