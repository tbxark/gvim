package tui

import (
	"errors"
	"strings"

	"github.com/tbxark/gvim/buffer"
	"github.com/tbxark/gvim/internal/tui/popup"
)

// lspAskSignature is the request for signature help. It follows the asks of lsp.go,
// and lspAsked, lspRequest and lspReply route it like the others.
const lspAskSignature = lspAskResolveCodeAction + 1

// signatureHelper is the optional part of the language-server port that answers
// signature help. The app's port has it. A port without it reports that the feature
// is not available, so the port interface does not have to grow for it.
type signatureHelper interface {
	SignatureHelp(path string, at buffer.Pos) (string, error)
}

// signatureOf asks port for the signature help at at, and returns the text to show.
func signatureOf(port LSPSession, path string, at buffer.Pos) (string, error) {
	h, ok := port.(signatureHelper)
	if !ok {
		return "", errors.New("signature help is not available in this port")
	}
	return h.SignatureHelp(path, at)
}

// lspSignature shows the signature help text in a popup, one row per line. An empty
// reply says so on the status row.
func (m *Model) lspSignature(text string) {
	if strings.TrimSpace(text) == "" {
		m.notice = "No signature help"
		return
	}
	var items []popup.Item
	for line := range strings.SplitSeq(text, "\n") {
		items = append(items, popup.Item{Label: line})
	}
	m.lspPopup("Signature", items)
}
