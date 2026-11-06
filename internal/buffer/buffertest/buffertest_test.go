package buffertest

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/tbxark/gvim/internal/buffer"
)

func TestMemSourceReadAndCount(t *testing.T) {
	m := NewMemSource([]byte("hello world"))
	p := make([]byte, 5)
	if n, err := m.ReadAt(p, 6); n != 5 || err != nil || string(p) != "world" {
		t.Fatalf("ReadAt = %d, %v, %q", n, err, p)
	}
	if got := m.BytesRead(); got != 5 {
		t.Fatalf("BytesRead = %d, want 5", got)
	}
	// A short read reports EOF and leaves the tail of p alone.
	p = bytes.Repeat([]byte{'#'}, 5)
	n, err := m.ReadAt(p, 9)
	if n != 2 || !errors.Is(err, io.EOF) || string(p) != "ld###" {
		t.Fatalf("short ReadAt = %d, %v, %q", n, err, p)
	}
}

func TestMemSourceCheck(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(m *MemSource)
		want   error
	}{
		{"untouched", func(*MemSource) {}, nil},
		{"mutate same size", func(m *MemSource) { m.Mutate(0, []byte("X")) }, nil},
		{"grow", func(m *MemSource) { m.Grow([]byte("more")) }, buffer.ErrSourceGrew},
		{"truncate", func(m *MemSource) { m.Truncate(3) }, buffer.ErrSourceChanged},
		{"touch", func(m *MemSource) { m.Touch() }, buffer.ErrSourceChanged},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := NewMemSource([]byte("hello world"))
			tt.mutate(m)
			if got := m.Check(); !errors.Is(got, tt.want) {
				t.Fatalf("Check = %v, want %v", got, tt.want)
			}
			if m.Size() != 11 {
				t.Fatalf("Size = %d, want the size at creation", m.Size())
			}
		})
	}
}

func TestMemSourceFailNextAndClose(t *testing.T) {
	boom := errors.New("boom")
	m := NewMemSource([]byte("abc"))
	m.FailNext(1, boom)
	p := make([]byte, 3)
	if _, err := m.ReadAt(p, 0); !errors.Is(err, boom) {
		t.Fatalf("first ReadAt err = %v, want boom", err)
	}
	if n, err := m.ReadAt(p, 0); n != 3 || err != nil {
		t.Fatalf("second ReadAt = %d, %v", n, err)
	}
	_ = m.Close()
	if !m.Closed() {
		t.Fatal("Closed = false after Close")
	}
	if _, err := m.ReadAt(p, 0); err == nil {
		t.Fatal("ReadAt after Close succeeded")
	}
}

func TestVirtualMatchesVirtualLine(t *testing.T) {
	const width = 24
	v := NewVirtual(5*width+7, width) // last line cut after 7 bytes
	all := make([]byte, v.Size())
	if n, err := v.ReadAt(all, 0); n != len(all) || err != nil {
		t.Fatalf("ReadAt = %d, %v", n, err)
	}
	lines := strings.Split(string(all), "\n")
	if len(lines) != 6 {
		t.Fatalf("got %d lines, want 6", len(lines))
	}
	for n := range 5 {
		if want := VirtualLine(int64(n), width); lines[n] != want {
			t.Errorf("line %d = %q, want %q", n, lines[n], want)
		}
	}
	if want := VirtualLine(5, width)[:7]; lines[5] != want {
		t.Errorf("cut last line = %q, want %q", lines[5], want)
	}
	// Reading from the middle gives the same bytes as reading from zero.
	mid := make([]byte, 40)
	if _, err := v.ReadAt(mid, 37); err != nil || !bytes.Equal(mid, all[37:77]) {
		t.Errorf("ReadAt(37) = %q, %v", mid, err)
	}
}

func TestVirtualOffsetsAbove4GiB(t *testing.T) {
	const width = 64
	v := NewVirtual(8<<30, width)
	line := int64(5<<30) / width
	p := make([]byte, width)
	if n, err := v.ReadAt(p, line*width); n != width || err != nil {
		t.Fatalf("ReadAt = %d, %v", n, err)
	}
	if want := VirtualLine(line, width) + "\n"; string(p) != want {
		t.Fatalf("line %d = %q, want %q", line, p, want)
	}
}
