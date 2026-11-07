package bundled_test

import (
	"runtime"
	"strings"
	"testing"
)

// A plugin that keys a Lua table by absolute line number makes gopher-lua pad the array part
// with nils up to the key (16 bytes a slot, plus the garbage of doubling). In a file with
// hundreds of thousands of lines and the caret far down, every key press paid for it. These
// tests put the caret near the end of a million-line buffer and bound what one step allocates.

const (
	bigLines     = 1_000_000
	bigAllocCap  = 16 << 20 // an offset keyed cache needs a few KiB; the padded one 80 MiB or more
	bigLineText  = "word\n"
	bigCaretLine = bigLines - 10
)

func millionLines() string {
	return strings.Repeat(bigLineText, bigLines-1) + "word"
}

// allocated runs f and returns the bytes the process allocated meanwhile. The tests that use
// it are not parallel, so nothing else allocates in the meantime but the runtime.
func allocated(f func()) uint64 {
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	f()
	runtime.ReadMemStats(&after)
	return after.TotalAlloc - before.TotalAlloc
}

func TestFlashDoesNotPadTablesByLineNumber(t *testing.T) {
	e := newFlash(t, millionLines(), nil)
	if err := e.h.Host.ScrollWindow(e.h.Host.CurrentWindow(), bigCaretLine, false); err != nil {
		t.Fatal(err)
	}
	n := allocated(func() {
		e.jump(t)
		e.press("w")
	})
	if n > bigAllocCap {
		t.Errorf("a flash session near line %d allocated %d MiB", bigCaretLine, n>>20)
	}
	if got := e.labels(e.buf); len(got) == 0 {
		t.Errorf("no labels near the end; log: %v", e.h.LogText("flash"))
	}
}

func TestViDoesNotPadTablesByLineNumber(t *testing.T) {
	v := newVi(t, millionLines())
	v.at(bigCaretLine, 0)
	n := allocated(func() {
		v.keys("lh")
	})
	if n > bigAllocCap {
		t.Errorf("two vi key presses near line %d allocated %d MiB", bigCaretLine, n>>20)
	}
}
