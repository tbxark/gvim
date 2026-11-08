package daemon

import (
	"bytes"
	"net"
	"sync"
	"testing"
	"time"
)

// shortWriter writes at most 3 bytes per call and reports the short count with a nil
// error, like a connection that fills its buffer: FrameWriter must complete the rest.
type shortWriter struct{ buf bytes.Buffer }

func (w *shortWriter) Write(p []byte) (int, error) {
	if len(p) > 3 {
		p = p[:3]
	}
	return w.buf.Write(p)
}

// TestFrameWriterCompletesShortWrites checks a short write never truncates a frame.
func TestFrameWriterCompletesShortWrites(t *testing.T) {
	var sw shortWriter
	fw := NewFrameWriter(&sw)
	payload := bytes.Repeat([]byte("abcdefgh"), 64)
	if err := fw.WriteFrame(TypeOutput, payload); err != nil {
		t.Fatalf("WriteFrame: %v", err)
	}
	f, err := ReadFrame(bytes.NewReader(sw.buf.Bytes()))
	if err != nil {
		t.Fatalf("the written stream has no valid frame: %v", err)
	}
	if f.Type != TypeOutput || !bytes.Equal(f.Payload, payload) {
		t.Fatalf("frame = type %d, %d payload bytes; want output, %d", f.Type, len(f.Payload), len(payload))
	}
}

// TestFrameWriterRejectsOversize checks the size guard runs before any write.
func TestFrameWriterRejectsOversize(t *testing.T) {
	var buf bytes.Buffer
	fw := NewFrameWriter(&buf)
	if err := fw.WriteFrame(TypeOutput, make([]byte, MaxPayload+1)); err == nil {
		t.Fatal("an oversize frame was accepted")
	}
	if buf.Len() != 0 {
		t.Fatalf("an oversize frame wrote %d bytes", buf.Len())
	}
}

// TestFrameWriterSerializesConcurrentFrames hammers one connection from many goroutines
// and checks every frame arrives whole. Without the writer's lock the 8 KiB payloads
// interleave and the reader sees a corrupt stream (type bytes and payloads mismatch).
func TestFrameWriterSerializesConcurrentFrames(t *testing.T) {
	const n = 24
	server, client := net.Pipe()
	defer func() { _ = server.Close() }()
	defer func() { _ = client.Close() }()
	fw := NewFrameWriter(server)
	want := make([][]byte, n)
	for i := range want {
		want[i] = bytes.Repeat([]byte{byte('A' + i)}, 8<<10)
	}
	// The reader consumes frames until all arrived.
	readDone := make(chan [][]byte, 1)
	go func() {
		var got [][]byte
		for len(got) < n {
			f, err := ReadFrame(client)
			if err != nil {
				readDone <- got
				return
			}
			if f.Type != TypeOutput {
				readDone <- got
				return
			}
			got = append(got, f.Payload)
		}
		readDone <- got
	}()
	var wg sync.WaitGroup
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := fw.WriteFrame(TypeOutput, want[i]); err != nil {
				t.Errorf("WriteFrame(%d): %v", i, err)
			}
		}()
	}
	wg.Wait()
	var got [][]byte
	select {
	case got = <-readDone:
	case <-time.After(5 * time.Second):
		t.Fatal("the reader did not finish")
	}
	if len(got) != n {
		t.Fatalf("read %d frames, want %d", len(got), n)
	}
	seen := make(map[string]bool, n)
	for _, p := range got {
		seen[string(p)] = true
	}
	for i, p := range want {
		if !seen[string(p)] {
			t.Fatalf("frame %d never arrived whole", i)
		}
	}
}
