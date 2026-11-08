package lsp

import (
	"errors"
	"strings"
	"testing"
)

func TestStartReportsAServerThatIsNotInstalled(t *testing.T) {
	_, err := Start(t.Context(), Config{Command: []string{"gvim-no-such-language-server"}})
	var ni *NotInstalledError
	if !errors.As(err, &ni) || ni.Command != "gvim-no-such-language-server" {
		t.Fatalf("Start error = %v, want a *NotInstalledError for the command", err)
	}
}

func TestNotInstalledMessageWithoutAHintNamesTheConfigKey(t *testing.T) {
	err := &NotInstalledError{Language: "nim", Command: "nimlangserver"}
	if got := err.Error(); !strings.Contains(got, "lsp.nim.command") {
		t.Errorf("message %q does not name the config key", got)
	}
	if cmd, install := err.MissingServer(); cmd != "nimlangserver" || install != "" {
		t.Errorf("MissingServer = %q, %q", cmd, install)
	}
}

func TestTailBufferKeepsTheLastLines(t *testing.T) {
	b := &tailBuffer{max: 40}
	_, _ = b.Write([]byte("one\ntwo\n\nthree\nfour\nfive\n"))
	if got := b.tail(2, 100); got != "four | five" {
		t.Errorf("tail(2) = %q, want %q", got, "four | five")
	}
	if got := b.tail(10, 100); got != "one | two | three | four | five" {
		t.Errorf("tail(10) = %q", got)
	}
	if got := b.tail(10, 8); got != "...r | five" {
		t.Errorf("tail cut to 8 = %q", got)
	}
	// The buffer is bounded.
	_, _ = b.Write([]byte(strings.Repeat("x", 100) + "\nlast line\n"))
	if len(b.data) > 40 {
		t.Errorf("buffer holds %d bytes, want at most 40", len(b.data))
	}
	if got := b.tail(1, 100); got != "last line" {
		t.Errorf("tail(1) = %q, want %q", got, "last line")
	}
	var none *tailBuffer
	if none.tail(1, 10) != "" {
		t.Error("a nil buffer has a tail")
	}
}
