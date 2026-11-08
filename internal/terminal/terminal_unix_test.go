//go:build unix

package terminal

import (
	"context"
	"testing"
	"time"
)

// TestUpdatesSignalsScreenChange checks that the output notification fires as the
// child prints, and that a receiver that reads the screen after a signal sees the
// output the signal announces.
func TestUpdatesSignalsScreenChange(t *testing.T) {
	s := startSh(t, "read line; printf 'got:%s\\n' \"$line\"; sleep 30")
	if _, err := s.Write([]byte("abc\n")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	deadline := time.After(pollTimeout)
	for !hasText("got:abc")(s.Screen()) {
		select {
		case _, ok := <-s.Updates():
			if !ok {
				t.Fatal("Updates closed before the child printed its line")
			}
		case <-deadline:
			t.Fatalf("no update announced got:abc; last screen:\n%s", screenText(s.Screen()))
		}
	}
}

// TestUpdatesClosesWhenOutputEnds checks that the notification channel closes once
// the child's output is over, so a receiver can stop waiting.
func TestUpdatesClosesWhenOutputEnds(t *testing.T) {
	s := startSh(t, "printf 'done\\n'")
	deadline := time.After(pollTimeout)
	for {
		select {
		case _, ok := <-s.Updates():
			if !ok {
				return
			}
		case <-deadline:
			t.Fatal("Updates not closed within the timeout after the child exited")
		}
	}
}

// TestScreenCursor checks that a snapshot carries the cursor position and whether
// the child hid the cursor.
func TestScreenCursor(t *testing.T) {
	tests := []struct {
		name   string
		script string
		text   string // the screen text to wait for before the cursor is checked
		want   Cursor
	}{
		{
			name:   "after printed text",
			script: "printf 'ab'; sleep 30",
			text:   "ab",
			want:   Cursor{Row: 0, Col: 2, Visible: true, Shape: "block"},
		},
		{
			name:   "on the next row after a newline",
			script: "printf 'ab\\r\\n'; sleep 30",
			text:   "ab",
			want:   Cursor{Row: 1, Col: 0, Visible: true, Shape: "block"},
		},
		{
			name:   "hidden by DECTCEM",
			script: "printf 'x\\033[?25l'; sleep 30",
			text:   "x",
			want:   Cursor{Row: 0, Col: 1, Visible: false, Shape: "block"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := startSh(t, tt.script)
			waitFor(t, s, "cursor "+tt.text, func(sc Screen) bool {
				return hasText(tt.text)(sc) && sc.Cursor == tt.want
			})
		})
	}
}

// TestPasteFollowsBracketedPasteMode checks that Paste wraps the text in the
// bracketed-paste markers only when the child has turned that mode on. The child
// runs cat -v, so the markers show on the screen as ^[ and [200~.
func TestPasteFollowsBracketedPasteMode(t *testing.T) {
	tests := []struct {
		name    string
		enable  string // escape sequence the child sends before it reports ready
		wantHas string
		wantNot string
	}{
		{
			name:    "bracketed when the child enables the mode",
			enable:  "printf '\\033[?2004h'; ",
			wantHas: "^[[200~hello",
		},
		{
			name:    "raw when the child does not enable the mode",
			enable:  "",
			wantHas: "hello",
			wantNot: "^[[200~",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := startSh(t, "stty -echo; "+tt.enable+"printf 'ready\\n'; cat -v")
			waitFor(t, s, "ready", hasText("ready"))
			if err := s.Paste("hello\n"); err != nil {
				t.Fatalf("Paste: %v", err)
			}
			waitFor(t, s, tt.wantHas, hasText(tt.wantHas))
			if tt.wantNot != "" && hasText(tt.wantNot)(s.Screen()) {
				t.Fatalf("screen shows %q, want raw text only; screen:\n%s", tt.wantNot, screenText(s.Screen()))
			}
		})
	}
}

func TestPasteAfterCloseFails(t *testing.T) {
	s, err := Start(context.Background(), Options{Shell: "/bin/sh", Args: []string{"-c", "exec cat"}})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := s.Paste("x"); err == nil {
		t.Fatal("Paste after Close error = nil, want an error")
	}
}
