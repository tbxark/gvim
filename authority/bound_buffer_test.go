package authority

import (
	"strings"
	"testing"
)

// TestBoundBufferCapsAndFlagsOverflow checks the transport buffer the SSH session pumps
// write into: it keeps at most limit bytes, keeps draining (every write reports its full
// length so the channel never blocks), and records the overflow so callers reject an
// over-cap answer instead of using a silently truncated one.
func TestBoundBufferCapsAndFlagsOverflow(t *testing.T) {
	b := newBoundBuffer(4)
	n, err := b.Write([]byte("abcdef"))
	if err != nil || n != 6 {
		t.Fatalf("Write = %d, %v; want the full length consumed", n, err)
	}
	if got := string(b.Bytes()); got != "abcd" {
		t.Fatalf("kept = %q, want the 4-byte cap", got)
	}
	if !b.Overflow() {
		t.Fatal("overflow was not recorded")
	}
	if _, _ = b.Write([]byte("more")); string(b.Bytes()) != "abcd" {
		t.Fatalf("a write after the cap changed the bytes: %q", b.Bytes())
	}

	unbounded := newBoundBuffer(0)
	if _, _ = unbounded.Write([]byte("hello")); string(unbounded.Bytes()) != "hello" || unbounded.Overflow() {
		t.Fatalf("unbounded buffer = %q overflow %v", unbounded.Bytes(), unbounded.Overflow())
	}
}

// TestBoundBufferKeepsThePrefixOnManyWrites checks the kept bytes are the first bytes in
// order across several writes that cross the cap.
func TestBoundBufferKeepsThePrefixOnManyWrites(t *testing.T) {
	b := newBoundBuffer(5)
	var out strings.Builder
	for _, s := range []string{"ab", "cde", "fg"} {
		_, _ = b.Write([]byte(s))
		out.WriteString(s)
	}
	if got, want := string(b.Bytes()), out.String()[:5]; got != want {
		t.Fatalf("kept = %q, want %q", got, want)
	}
	if !b.Overflow() {
		t.Fatal("overflow was not recorded across writes")
	}
}
