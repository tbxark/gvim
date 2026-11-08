package daemon

import (
	"encoding/binary"
	"encoding/json"
	"io"
	"sync"
)

// FrameWriter serializes frames on one connection. A frame's header and payload are
// written under one mutex, so producers on different goroutines (program output, pong,
// detach/exit, client input and resize) never interleave; a reader always sees whole
// frames. Writes complete short writes in a loop. One FrameWriter belongs to one
// connection, so the lock never spans connections.
//
// FrameWriter does no IO beyond the writes themselves: callers that need a bound (a
// slow client must not wedge the program) wrap a net.Conn in a deadline writer before
// handing it here.
type FrameWriter struct {
	mu sync.Mutex
	w  io.Writer
}

// NewFrameWriter wraps w.
func NewFrameWriter(w io.Writer) *FrameWriter { return &FrameWriter{w: w} }

// WriteFrame writes one frame: the 4-byte big-endian length, the type byte and the
// payload, all under the writer's lock. A payload over MaxPayload is refused before
// anything is written.
func (fw *FrameWriter) WriteFrame(typ byte, payload []byte) error {
	if len(payload) > MaxPayload {
		return ErrFrameTooLarge
	}
	header := make([]byte, 5)
	binary.BigEndian.PutUint32(header[:4], uint32(len(payload)))
	header[4] = typ
	fw.mu.Lock()
	defer fw.mu.Unlock()
	if err := writeAll(fw.w, header); err != nil {
		return err
	}
	if len(payload) == 0 {
		return nil
	}
	return writeAll(fw.w, payload)
}

// WriteJSON writes a frame whose payload is the JSON of v.
func (fw *FrameWriter) WriteJSON(typ byte, v any) error {
	payload, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return fw.WriteFrame(typ, payload)
}

// WriteNotice writes a notice frame.
func (fw *FrameWriter) WriteNotice(text string) error {
	return fw.WriteFrame(TypeNotice, []byte(text))
}

// writeAll writes b completely: a short write (n < len(b), nil) continues, and a zero
// write is io.ErrShortWrite so a broken writer cannot loop forever.
func writeAll(w io.Writer, b []byte) error {
	for len(b) > 0 {
		n, err := w.Write(b)
		if n > 0 {
			b = b[n:]
		}
		if err != nil {
			return err
		}
		if n == 0 {
			return io.ErrShortWrite
		}
	}
	return nil
}
