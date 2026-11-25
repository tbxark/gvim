package terminal

import (
	"bytes"
	"encoding/base64"
	"sync"
)

// OSC 52 is the clipboard escape: ESC ] 52 ; selection ; base64 [BEL | ESC \].
// The emulator has no clipboard callback, so the session registers an OSC
// handler for command 52 (see newSession) and hands the decoded text to the
// dock's clipboard hook. The emulator's own parser buffers a sequence split
// over reads and ends it at BEL or ST.
//
// The handler stays bounded: a sequence whose raw payload is longer than
// maxOSC52Sequence is dropped, and the decoded text is capped by
// maxOSC52Decoded. The emulator itself buffers up to 4 MB per sequence.
const (
	maxOSC52Sequence = 128 << 10 // bytes of one OSC 52 payload we are willing to parse
	maxOSC52Decoded  = 96 << 10  // decoded text we hand to the clipboard hook
	// maxOSC52ResponseText bounds the clipboard text sent back to a child that
	// asked for a paste. The base64 response stays under maxOSC52Sequence so a
	// nested gvim can parse it.
	maxOSC52ResponseText = 64 << 10
)

// osc52Event is one decoded OSC 52 sequence waiting for feed to dispatch it: a
// clipboard write (selection and text) or a read request (query).
type osc52Event struct {
	selection string
	text      string
	query     bool
}

// maxOSC52Events bounds the clipboard events one feed queues while it holds
// s.mu; a child printing sequences in a loop cannot grow memory past it.
const maxOSC52Events = 64

// handleOSC52 is the emulator's OSC 52 handler. It decodes the payload and
// queues an event for feed to dispatch after s.mu is released: the clipboard
// hook may touch the system clipboard (IO) and must never run under the session
// lock. data is the whole OSC payload, "52;selection;base64".
func (s *Session) handleOSC52(data []byte) bool {
	selection, text, query, ok := parseOSC52(data)
	if !ok || len(s.osc52Events) >= maxOSC52Events {
		return true
	}
	s.osc52Events = append(s.osc52Events, osc52Event{selection: selection, text: text, query: query})
	return true
}

// parseOSC52 parses the OSC 52 payload the emulator hands the handler. It
// reports the selection and either the decoded clipboard text or a read
// request ("?"), enforcing the sequence and decoded-size bounds.
func parseOSC52(data []byte) (selection, text string, query, ok bool) {
	if len(data) > maxOSC52Sequence {
		return "", "", false, false
	}
	rest, isOSC52 := bytes.CutPrefix(data, []byte("52;"))
	if !isOSC52 {
		return "", "", false, false
	}
	sel, payload, hasData := bytes.Cut(rest, []byte(";"))
	if !hasData {
		return "", "", false, false
	}
	selection = string(sel)
	// "c" (alone or combined, e.g. "pc"/"cp") and the empty selection name the
	// clipboard; p/s alone and numbered cut buffers are not handled.
	if len(sel) > 0 && !bytes.ContainsRune(sel, 'c') {
		return "", "", false, false
	}
	// A "?" payload is a read request: the terminal is asked to put clipboard
	// contents into the child's input. It is a query, never clipboard text.
	if string(bytes.TrimSpace(payload)) == "?" {
		return selection, "", true, true
	}
	text, err := decodeOSC52(payload)
	if err != nil || len(text) > maxOSC52Decoded {
		return "", "", false, false
	}
	return selection, text, false, true
}

// decodeOSC52 decodes the base64 payload of an OSC 52 sequence.
func decodeOSC52(data []byte) (string, error) {
	data = bytes.TrimSpace(data)
	if len(data) == 0 {
		return "", nil
	}
	out := make([]byte, base64.StdEncoding.DecodedLen(len(data)))
	n, err := base64.StdEncoding.Decode(out, data)
	if err != nil {
		return "", err
	}
	return string(out[:n]), nil
}

// osc52Response builds the terminal's answer to a read request: the OSC 52
// clipboard sequence carrying text. selection is "c" or "" (the clipboard).
func osc52Response(selection, text string) []byte {
	resp := make([]byte, 0, 8+len(selection)+base64.StdEncoding.EncodedLen(len(text))+1)
	resp = append(resp, 0x1b, ']', '5', '2', ';')
	resp = append(resp, selection...)
	resp = append(resp, ';')
	resp = base64.StdEncoding.AppendEncode(resp, []byte(text))
	resp = append(resp, 0x07)
	return resp
}

// osc52Handler is the session's clipboard hook; nil means the feature is inert
// (the default: the dock installs it only with clipboard.use_osc52 on and a
// trusted workspace).
type osc52Handler struct {
	mu      sync.RWMutex
	fn      func(selection, text string) bool
	queryFn func(selection string)
}

// set installs the hook, or clears it with nil.
func (h *osc52Handler) set(fn func(selection, text string) bool) {
	h.mu.Lock()
	h.fn = fn
	h.mu.Unlock()
}

// emit hands one decoded sequence to the hook, if any.
func (h *osc52Handler) emit(selection, text string) {
	h.mu.RLock()
	fn := h.fn
	h.mu.RUnlock()
	if fn != nil {
		fn(selection, text)
	}
}

// setQuery installs the read-request hook, or clears it with nil.
func (h *osc52Handler) setQuery(fn func(selection string)) {
	h.mu.Lock()
	h.queryFn = fn
	h.mu.Unlock()
}

// query reports one clipboard read request, if a hook is installed.
func (h *osc52Handler) query(selection string) {
	h.mu.RLock()
	fn := h.queryFn
	h.mu.RUnlock()
	if fn != nil {
		fn(selection)
	}
}

// SetOSC52Handler installs or clears the session's OSC 52 clipboard hook. The
// dock installs one only when clipboard.use_osc52 is on and the workspace is
// trusted; restored/read-only transcripts have no live session and so no hook.
func (s *Session) SetOSC52Handler(fn func(selection, text string) bool) {
	s.osc52.set(fn)
}

// SetOSC52QueryHandler installs or clears the session's OSC 52 read-request
// handler. When a child prints ESC ] 52 ; c ; ? BEL (an OSC 52 read), fn gets
// the selection ("c" or "") and returns the clipboard text to hand to the child.
// The response is queued as terminal input (the way a terminal answers a query),
// so the child reads it from its stdin. A nil handler, ok=false, or text over
// maxOSC52ResponseText sends nothing. The dock installs this only when
// clipboard.osc52_paste is on and the workspace is trusted.
func (s *Session) SetOSC52QueryHandler(fn func(selection string) (string, bool)) {
	if fn == nil {
		s.osc52.setQuery(nil)
		return
	}
	s.osc52.setQuery(func(selection string) {
		text, ok := fn(selection)
		if !ok || len(text) > maxOSC52ResponseText {
			return
		}
		_, _ = s.Write(osc52Response(selection, text))
	})
}
