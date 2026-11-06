package main

import (
	"strings"
	"testing"

	"github.com/tbxark/gvim/internal/term"
)

func TestEncodeKey(t *testing.T) {
	tests := []struct {
		name      string
		spec      string
		appCursor bool
		want      string
	}{
		{"character", "a", false, "a"},
		{"plus is a key", "+", false, "+"},
		{"unicode character", "é", false, "é"},
		{"space name", "space", false, " "},
		{"enter", "enter", false, "\r"},
		{"escape", "esc", false, "\x1b"},
		{"backspace", "backspace", false, "\x7f"},
		{"ctrl letter", "ctrl+e", false, "\x05"},
		{"ctrl upper letter", "ctrl+E", false, "\x05"},
		{"ctrl space", "ctrl+space", false, "\x00"},
		{"alt letter", "alt+x", false, "\x1bx"},
		{"alt enter", "alt+enter", false, "\x1b\r"},
		{"shift letter", "shift+a", false, "A"},
		{"shift tab", "shift+tab", false, "\x1b[Z"},
		{"arrow", "up", false, "\x1b[A"},
		{"arrow in application cursor mode", "up", true, "\x1bOA"},
		{"shift arrow", "shift+down", false, "\x1b[1;2B"},
		{"ctrl arrow in application cursor mode", "ctrl+right", true, "\x1b[1;5C"},
		{"home", "home", false, "\x1b[H"},
		{"page down", "pgdown", false, "\x1b[6~"},
		{"alt delete", "alt+delete", false, "\x1b[3;3~"},
		{"f1", "f1", false, "\x1bOP"},
		{"shift f1", "shift+f1", false, "\x1b[1;2P"},
		{"f5", "f5", false, "\x1b[15~"},
		{"f10", "f10", false, "\x1b[21~"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := encodeKey(tt.spec, tt.appCursor)
			if err != nil {
				t.Fatalf("encodeKey(%q): %v", tt.spec, err)
			}
			if string(got) != tt.want {
				t.Errorf("encodeKey(%q) = %q, want %q", tt.spec, got, tt.want)
			}
		})
	}
}

func TestEncodeKeyRejects(t *testing.T) {
	// ctrl++ is the plus key with ctrl, which has no control code.
	for _, spec := range []string{"", "ctrl+", "ctrl+nosuch", "hyper+a", "ctrl+1", "ctrl++", "ctrl++x"} {
		if _, err := encodeKey(spec, false); err == nil {
			t.Errorf("encodeKey(%q) succeeded, want an error", spec)
		}
	}
}

func TestMouseBytes(t *testing.T) {
	tests := []struct {
		name string
		ev   mouseEvent
		want string
	}{
		{"left click", mouseEvent{Action: "click", Button: "left"}, "\x1b[<0;1;1M\x1b[<0;1;1m"},
		{"right press", mouseEvent{Action: "press", Button: "right", X: 9, Y: 4}, "\x1b[<2;10;5M"},
		{"left release", mouseEvent{Action: "release", Button: "left", X: 1, Y: 1}, "\x1b[<0;2;2m"},
		{"wheel down", mouseEvent{Action: "wheel-down", X: 3, Y: 3}, "\x1b[<65;4;4M"},
		{"wheel up", mouseEvent{Action: "wheel-up", X: 0, Y: 0}, "\x1b[<64;1;1M"},
		{"move with no button", mouseEvent{Action: "move"}, "\x1b[<35;1;1M"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := mouseBytes(tt.ev)
			if err != nil {
				t.Fatalf("mouseBytes(%+v): %v", tt.ev, err)
			}
			if string(got) != tt.want {
				t.Errorf("mouseBytes(%+v) = %q, want %q", tt.ev, got, tt.want)
			}
		})
	}
}

func TestMouseBytesRejects(t *testing.T) {
	for _, ev := range []mouseEvent{
		{Action: "click", Button: "left", X: -1},
		{Action: "click", Button: "nosuch"},
		{Action: "drag", Button: "left"},
	} {
		if _, err := mouseBytes(ev); err == nil {
			t.Errorf("mouseBytes(%+v) succeeded, want an error", ev)
		}
	}
}

func TestFrameHTMLEscapesAndStyles(t *testing.T) {
	sc := term.NewScreen(1, 3, []term.Cell{
		{Rune: '<'},
		{Rune: 'b', Style: term.Style{Bold: true}},
		{Rune: '&', Style: term.Style{Bold: true}},
	})
	got := frameHTML(sc)
	for _, want := range []string{"&lt;", "&amp;", "font-weight:bold;"} {
		if !strings.Contains(got, want) {
			t.Errorf("frameHTML does not contain %q:\n%s", want, got)
		}
	}
}
