package lsphost

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/tbxark/gvim/config"
	"go.lsp.dev/uri"
)

// TestLocationFileAcceptsFileURIsOnly checks the scheme split: file URIs (and scheme-less
// paths) resolve to their path, while any other scheme is an explicit error rather than a
// guessed filesystem path.
func TestLocationFileAcceptsFileURIsOnly(t *testing.T) {
	port := NewFor(config.Defaults()).(*lspPort)
	want := filepath.Join(t.TempDir(), "core.slang")
	file, err := port.locationFile(uri.File(want).String())
	if err != nil || file != want {
		t.Fatalf("file URI = %q err=%v, want %q", file, err, want)
	}
	for _, raw := range []string{"slang-synth://core/core.builtin", "untitled:Untitled-1", "https://example.com/a.go"} {
		file, err := port.locationFile(raw)
		if err == nil || file != "" || !strings.Contains(err.Error(), "unsupported URI scheme") {
			t.Errorf("locationFile(%q) = %q, %v; want an unsupported-scheme error", raw, file, err)
		}
	}
}
