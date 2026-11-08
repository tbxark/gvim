package lsphost

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/tbxark/gvim/buffer"
	"github.com/tbxark/gvim/config"
	"github.com/tbxark/gvim/lsp"
)

// TestLSPPortSignatureHelpForUnopenedDocumentIsNotOpen checks that the port offers
// signature help, and that a request for a document the server does not hold is refused
// as the other requests are.
func TestLSPPortSignatureHelpForUnopenedDocumentIsNotOpen(t *testing.T) {
	t.Parallel()
	port := New(map[string]config.LSPServer{"go": {Command: "gopls"}})
	sig, ok := port.(interface {
		SignatureHelp(path string, at buffer.Pos) (string, error)
	})
	if !ok {
		t.Fatalf("the LSP port %T has no SignatureHelp", port)
	}
	_, err := sig.SignatureHelp(filepath.Join(t.TempDir(), "a.go"), buffer.Pos{})
	if !errors.Is(err, lsp.ErrNotOpen) {
		t.Fatalf("SignatureHelp: err = %v, want ErrNotOpen", err)
	}
}
