package daemon

import (
	"bytes"
	"errors"
	"io"
	"testing"
)

// TestFrameTypesAreDistinct checks no two frame types share a byte: a client must never
// read one reply as another.
func TestFrameTypesAreDistinct(t *testing.T) {
	seen := map[byte]string{}
	for name, b := range map[string]byte{
		"Hello": TypeHello, "Input": TypeInput, "Resize": TypeResize, "Detach": TypeDetach,
		"OpenFile": TypeOpenFile, "Ping": TypePing, "VersionRejected": TypeVersionRejected,
		"Output": TypeOutput, "Notice": TypeNotice, "Detached": TypeDetached, "Pong": TypePong,
		"Exit": TypeExit, "WorkspaceList": TypeWorkspaceList, "WorkspaceSelect": TypeWorkspaceSelect,
		"WorkspaceNew": TypeWorkspaceNew, "WorkspaceClose": TypeWorkspaceClose,
		"WorkspaceRename": TypeWorkspaceRename, "Workspaces": TypeWorkspaces,
	} {
		if other, ok := seen[b]; ok {
			t.Errorf("%s and %s share frame type %d", name, other, b)
		}
		seen[b] = name
	}
}

type writeCountingBuffer struct {
	buf    bytes.Buffer
	writes int
}

func (w *writeCountingBuffer) Write(p []byte) (int, error) {
	w.writes++
	return w.buf.Write(p)
}

func TestWriteReadFrameSizesAndSingleWrite(t *testing.T) {
	cases := []struct {
		name       string
		size       int
		wantWrites int
	}{
		{"empty", 0, 1},
		{"small", 1, 1},
		{"boundary 4096", maxSmallFramePayload, 1},
		{"over stack buffer 4097", maxSmallFramePayload + 1, 2},
		{"64KiB", 64 << 10, 2},
		{"MaxPayload", MaxPayload, 2},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var dst writeCountingBuffer
			payload := bytes.Repeat([]byte{'k'}, tc.size)
			if err := WriteFrame(&dst, TypeOutput, payload); err != nil {
				t.Fatalf("WriteFrame(%d): %v", tc.size, err)
			}
			if dst.writes != tc.wantWrites {
				t.Fatalf("WriteFrame(%d) issued %d Write calls, want %d", tc.size, dst.writes, tc.wantWrites)
			}
			got, err := ReadFrame(&dst.buf)
			if err != nil {
				t.Fatalf("ReadFrame(%d): %v", tc.size, err)
			}
			if got.Type != TypeOutput || !bytes.Equal(got.Payload, payload) {
				t.Fatalf("round-trip mismatch: type=%d len=%d", got.Type, len(got.Payload))
			}
			if _, err := ReadFrame(&dst.buf); !errors.Is(err, io.EOF) {
				t.Fatalf("trailing ReadFrame = %v, want io.EOF", err)
			}
		})
	}
}

func BenchmarkWriteFrameSmall(b *testing.B) {
	payload := bytes.Repeat([]byte("a"), 256)
	b.ReportAllocs()
	for b.Loop() {
		if err := WriteFrame(io.Discard, TypeOutput, payload); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkWriteFrameLarge(b *testing.B) {
	payload := bytes.Repeat([]byte("a"), 32<<10)
	b.ReportAllocs()
	for b.Loop() {
		if err := WriteFrame(io.Discard, TypeOutput, payload); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkReadFrameSmall(b *testing.B) {
	var raw bytes.Buffer
	_ = WriteFrame(&raw, TypeOutput, bytes.Repeat([]byte("a"), 256))
	frameBytes := raw.Bytes()
	var r bytes.Reader
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		r.Reset(frameBytes)
		if _, err := ReadFrame(&r); err != nil {
			b.Fatal(err)
		}
	}
}
