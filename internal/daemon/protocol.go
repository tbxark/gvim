// Package daemon is the gvim daemon MVP (#18a): a Unix-socket hosted editor and
// the small length-prefixed protocol a client uses to attach to it.
//
// The protocol is a stream of frames: a 4-byte big-endian payload length, one
// type byte, then the payload. Client frames are hello (JSON cols/rows/args),
// input (raw terminal bytes), resize (JSON cols/rows), detach, open-file
// (JSON paths/wait/cwd/split) and ping. Server frames are output (raw ANSI
// bytes), notice (text), detached, pong and exit. A frame payload is at most
// MaxPayload bytes; a reader that sees a larger length fails the connection.
package daemon

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

// Frame type bytes. Unknown types are a protocol error.
const (
	TypeHello    byte = 1
	TypeInput    byte = 2
	TypeResize   byte = 3
	TypeDetach   byte = 4
	TypeOpenFile byte = 5
	TypePing     byte = 6

	TypeOutput   byte = 101
	TypeNotice   byte = 102
	TypeDetached byte = 103
	TypePong     byte = 104
	TypeExit     byte = 105
)

// MaxPayload bounds one frame payload.
const MaxPayload = 1 << 20

// ErrFrameTooLarge is returned when a frame announces a payload over MaxPayload.
var ErrFrameTooLarge = errors.New("daemon: frame payload too large")

// Frame is one protocol frame.
type Frame struct {
	Type    byte
	Payload []byte
}

// Hello is the first client frame: the client's terminal size and the launch
// arguments the daemon should apply (open files and the like).
type Hello struct {
	Cols int      `json:"cols"`
	Rows int      `json:"rows"`
	Args []string `json:"args,omitempty"`
	// Workspace names the workspace to attach to (its stable id or label); empty
	// means the daemon's current foreground workspace.
	Workspace string `json:"workspace,omitempty"`
}

// Resize is a terminal size change.
type Resize struct {
	Cols int `json:"cols"`
	Rows int `json:"rows"`
}

// OpenFile asks the daemon to open paths. Wait makes the client wait for the
// daemon's answer (a notice/output) instead of returning at once; Split opens the
// paths in a horizontal split when the daemon supports it.
type OpenFile struct {
	Paths []string `json:"paths"`
	Wait  bool     `json:"wait,omitempty"`
	Cwd   string   `json:"cwd,omitempty"`
	Split bool     `json:"split,omitempty"`
}

// WriteFrame writes one frame to w.
func WriteFrame(w io.Writer, typ byte, payload []byte) error {
	if len(payload) > MaxPayload {
		return ErrFrameTooLarge
	}
	header := make([]byte, 5)
	binary.BigEndian.PutUint32(header[:4], uint32(len(payload)))
	header[4] = typ
	if err := writeAll(w, header); err != nil {
		return err
	}
	if len(payload) == 0 {
		return nil
	}
	return writeAll(w, payload)
}

// ReadFrame reads one frame from r. A clean EOF before any byte is io.EOF; a
// truncated frame is io.ErrUnexpectedEOF.
func ReadFrame(r io.Reader) (Frame, error) {
	header := make([]byte, 5)
	if _, err := io.ReadFull(r, header); err != nil {
		return Frame{}, err
	}
	n := binary.BigEndian.Uint32(header[:4])
	if n > MaxPayload {
		return Frame{}, fmt.Errorf("%w (%d bytes)", ErrFrameTooLarge, n)
	}
	f := Frame{Type: header[4]}
	if n > 0 {
		f.Payload = make([]byte, n)
		if _, err := io.ReadFull(r, f.Payload); err != nil {
			return Frame{}, io.ErrUnexpectedEOF
		}
	}
	return f, nil
}

// WriteJSON writes a frame whose payload is the JSON of v.
func WriteJSON(w io.Writer, typ byte, v any) error {
	payload, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return WriteFrame(w, typ, payload)
}

// DecodeJSON decodes a JSON payload into v.
func DecodeJSON(f Frame, v any) error {
	if len(f.Payload) == 0 {
		return errors.New("empty JSON frame")
	}
	return json.Unmarshal(f.Payload, v)
}

// WriteNotice sends a notice frame (a client-visible message).
func WriteNotice(w io.Writer, text string) error {
	return WriteFrame(w, TypeNotice, []byte(text))
}
