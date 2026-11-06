package ui

import (
	"testing"

	"github.com/tbxark/gvim/internal/buffer"
)

// TestSignatureHelpShowsTheSignatureInAPopup checks that Show Signature Help asks the
// language server at the caret and shows the reply in a popup.
func TestSignatureHelpShowsTheSignatureInAPopup(t *testing.T) {
	port := newFakeLSP("go")
	port.sig = "Printf(format string, a ...any)\nParameter: format string"
	lt := newLSPTest(t, port, &lspTab{path: "/p/a.go", lines: []string{"package a"}})
	lt.open()

	for _, msg := range lt.act("lsp_signature_help") {
		lt.feed(msg)
	}
	lt.popup()
	if !frameHas(lt.m, "Printf(format string, a ...any)") || !frameHas(lt.m, "Parameter: format string") {
		t.Fatal("the signature is not on the frame")
	}
}

// TestSignatureHelpWithoutASignatureSaysSo checks that a reply with no signature opens
// no popup and says so on the status row.
func TestSignatureHelpWithoutASignatureSaysSo(t *testing.T) {
	port := newFakeLSP("go")
	lt := newLSPTest(t, port, &lspTab{path: "/p/a.go", lines: []string{"package a"}})
	lt.open()

	for _, msg := range lt.act("lsp_signature_help") {
		lt.feed(msg)
	}
	for _, o := range lt.m.overlays {
		if _, ok := o.(popupOverlay); ok {
			t.Fatal("a popup opened for an empty signature help")
		}
	}
	if lt.m.notice != "No signature help" {
		t.Fatalf("notice = %q, want the no-signature notice", lt.m.notice)
	}
}

// TestSignatureHelpNeedsAPortThatAnswersIt checks that a language-server port without
// the signature method is reported, not called.
func TestSignatureHelpNeedsAPortThatAnswersIt(t *testing.T) {
	t.Parallel()
	_, err := signatureOf(struct{ LSPSession }{}, "/p/a.go", buffer.Pos{})
	if err == nil || err.Error() != "signature help is not available in this port" {
		t.Fatalf("signatureOf = %v, want the not-available error", err)
	}
}
