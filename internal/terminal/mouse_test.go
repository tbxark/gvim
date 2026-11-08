//go:build unix

package terminal

import (
	"regexp"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestOSC7Path(t *testing.T) {
	t.Parallel()
	tests := []struct{ name, raw, want string }{
		{"a local file URL", "file:///tmp/x", "/tmp/x"},
		{"a file URL with a host", "file://host/tmp/x", "/tmp/x"},
		{"a file URL with an escaped space", "file:///tmp/a%20b", "/tmp/a b"},
		{"a plain path", "/tmp/x", "/tmp/x"},
		{"a remote URL without a path", "file://host", ""},
		{"a relative path", "tmp/x", ""},
		{"empty", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := osc7Path(tt.raw); got != tt.want {
				t.Fatalf("osc7Path(%q) = %q, want %q", tt.raw, got, tt.want)
			}
		})
	}
}

// TestMouseReportingTracksTheModes checks the flag logic without a PTY: the
// emulator's callbacks call noteMode, and the dock reads the result.
func TestMouseReportingTracksTheModes(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name                 string
		on, off              []ansi.Mode
		reporting, drag, all bool
	}{
		{name: "no mode", reporting: false},
		{name: "press reporting", on: []ansi.Mode{ansi.ModeMouseNormal}, reporting: true},
		{name: "drag reporting", on: []ansi.Mode{ansi.ModeMouseButtonEvent}, reporting: true, drag: true},
		{name: "all motion", on: []ansi.Mode{ansi.ModeMouseAnyEvent}, reporting: true, drag: true, all: true},
		{name: "all motion then a lower mode on", on: []ansi.Mode{ansi.ModeMouseAnyEvent, ansi.ModeMouseNormal}, reporting: true, drag: true, all: true},
		{name: "all motion off keeps the lower mode", on: []ansi.Mode{ansi.ModeMouseAnyEvent, ansi.ModeMouseNormal}, off: []ansi.Mode{ansi.ModeMouseAnyEvent}, reporting: true},
		{name: "all off", on: []ansi.Mode{ansi.ModeMouseAnyEvent}, off: []ansi.Mode{ansi.ModeMouseAnyEvent}, reporting: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			s := &Session{}
			for _, m := range tt.on {
				s.noteMode(m, true)
			}
			for _, m := range tt.off {
				s.noteMode(m, false)
			}
			reporting, drag, all := s.MouseReporting()
			if reporting != tt.reporting || drag != tt.drag || all != tt.all {
				t.Fatalf("MouseReporting = %v, %v, %v; want %v, %v, %v", reporting, drag, all, tt.reporting, tt.drag, tt.all)
			}
		})
	}
}

// TestSendMouseSendsNothingWithoutReporting checks that a mouse event for a child
// that did not ask for the mouse does not panic and writes nothing.
func TestSendMouseSendsNothingWithoutReporting(t *testing.T) {
	t.Parallel()
	s := &Session{}
	s.SendMouse(MouseEvent{Kind: MousePress, X: 1, Y: 2, Button: MouseLeft})
}

// TestSentMouseEncoding is the real-PTY check of the forwarded encoding: a child
// that turns on SGR mouse reporting and runs cat -v prints the sequence it read,
// so the screen shows the encoded event.
func TestSentMouseEncoding(t *testing.T) {
	s := startSh(t, "printf '\\033[?1000h\\033[?1006h'; exec cat -v")
	waitFor(t, s, "mouse reporting", func(Screen) bool {
		reporting, _, _ := s.MouseReporting()
		return reporting
	})
	s.SendMouse(MouseEvent{Kind: MousePress, X: 5, Y: 3, Button: MouseLeft})
	waitFor(t, s, "the encoded press", func(sc Screen) bool {
		return regexp.MustCompile("<0;[0-9]+;[0-9]+M").MatchString(strings.Join([]string{screenText(sc)}, ""))
	})
}
