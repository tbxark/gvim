package app

import (
	"github.com/tbxark/gvim/config"
	"github.com/tbxark/gvim/internal/uiadapter"
)

// systemClipboardRead is the app's system-clipboard reader. It is a variable so
// the OSC 52 read/paste behavior tests can script the clipboard without touching
// the host; production uses the timeout-bounded platform adapter.
var systemClipboardRead = func() (string, error) {
	return uiadapter.SystemClipboard{}.Read()
}

// clipboardOSC52PasteMax bounds the text handed to a child that asked for the
// clipboard with OSC 52. The terminal also bounds the encoded response.
const clipboardOSC52PasteMax = 64 << 10

// dockOSC52Query builds the dock's OSC 52 read-request handler. It answers only
// when clipboard.osc52_paste is on and the live trust closure says Trusted, and
// only with a non-empty, bounded clipboard. The handler runs on the terminal
// session's read goroutine (never the UI goroutine), and a failed/empty/oversize
// read answers nothing, so a child's paste times out instead of stalling the
// editor.
func dockOSC52Query(cfg config.Config, trusted func() bool, read func() (string, error)) func(selection string) (string, bool) {
	return func(selection string) (string, bool) {
		if !cfg.Clipboard.OSC52Paste || trusted == nil || !trusted() {
			return "", false
		}
		if read == nil {
			return "", false
		}
		text, err := read()
		if err != nil || text == "" || len(text) > clipboardOSC52PasteMax {
			return "", false
		}
		return text, true
	}
}
