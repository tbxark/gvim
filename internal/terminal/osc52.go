package terminal

import (
	"bytes"
	"encoding/base64"
	"sync"
)

// OSC 52 is the clipboard escape: ESC ] 52 ; selection ; base64 [BEL | ESC ].
// The terminal emulator ignores it (x/vt has no clipboard callback), so the
// session re-parses the child stream for it and hands the decoded text to the
// dock's clipboard hook. The bytes still go to the emulator unchanged.
//
// The parser is bounded: a pending OSC sequence longer than maxOSC52Sequence is
// dropped whole (including its terminator), and the decoded payload is capped by
// the encoded bound. It is chunk-safe: it runs on every read, so a sequence split
// over two reads is still found.
const (
	maxOSC52Sequence = 128 << 10 // bytes of one OSC sequence we are willing to buffer
	maxOSC52Decoded  = 96 << 10  // decoded text we hand to the clipboard hook
	// maxOSC52ResponseText bounds the clipboard text sent back to a child that
	// asked for a paste. The base64 response stays under maxOSC52Sequence so a
	// nested gvim can parse it.
	maxOSC52ResponseText = 64 << 10
)

// osc52Parser is a streaming parser for OSC 52 sequences in child output.
type osc52Parser struct {
	state int // oscGround, oscStart, oscBody, oscDiscard
	body  []byte
}

const (
	oscGround = iota
	oscStart
	oscBody
	oscDiscard
)

// scan consumes child output and calls emit for every complete, valid OSC 52
// clipboard write, and query for every clipboard read request ("?"). It never
// panics on arbitrary bytes and never keeps more than maxOSC52Sequence bytes.
func (p *osc52Parser) scan(data []byte, emit func(selection, text string), query func(selection string)) {
	for _, b := range data {
		switch p.state {
		case oscGround:
			if b == 0x1b {
				p.state = oscStart
			}
		case oscStart:
			if b == ']' {
				p.state = oscBody
				p.body = p.body[:0]
				continue
			}
			// Not an OSC: the ESC ends whatever it was; a new ESC may start one.
			p.state = oscGround
			if b == 0x1b {
				p.state = oscStart
			}
		case oscBody:
			switch b {
			case 0x07: // BEL terminates
				p.finish(emit, query)
			case 0x1b: // ESC \ (ST) terminates; the \ is the next byte
				p.finishAtST(emit, query)
			default:
				if len(p.body) >= maxOSC52Sequence {
					p.state = oscDiscard
					p.body = p.body[:0]
					continue
				}
				p.body = append(p.body, b)
			}
		case oscDiscard:
			// Drop until the sequence's terminator; a BEL or ESC + \\ ends it.
			switch b {
			case 0x07:
				p.state = oscGround
			case 0x1b:
				p.state = oscStart
			}
		}
	}
}

// finishAtST handles an ESC seen inside an OSC: it may be the ST (ESC \)
// terminator. The parser switches to oscStart so the following \ can complete
// it, but an ST inside an OSC must not restart collection as a new OSC, so the
// sequence is finished here and the next byte is handled as ground.
func (p *osc52Parser) finishAtST(emit func(selection, text string), query func(selection string)) {
	p.finish(emit, query)
	p.state = oscGround
}

// finish parses and emits the collected OSC body.
func (p *osc52Parser) finish(emit func(selection, text string), query func(selection string)) {
	body := p.body
	p.body = nil
	p.state = oscGround
	if emit == nil {
		return
	}
	rest, ok := bytes.CutPrefix(body, []byte("52;"))
	if !ok {
		return
	}
	selection, data, ok := bytes.Cut(rest, []byte(";"))
	if !ok {
		return
	}
	sel := string(selection)
	// "c" and the empty selection name the clipboard; p/s are the primary and
	// select selections, which gvim does not have, and numbered cut buffers are
	// not a terminal-wide surface. Decode only the clipboard ones.
	if sel != "" && sel != "c" {
		return
	}
	// A "?" payload is a read request: the terminal is asked to put clipboard
	// contents into the child's input. It is a query, never clipboard text.
	if string(bytes.TrimSpace(data)) == "?" {
		if query != nil {
			query(sel)
		}
		return
	}
	text, decoder := decodeOSC52(data)
	if decoder != nil || len(text) > maxOSC52Decoded {
		return
	}
	emit(sel, text)
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
