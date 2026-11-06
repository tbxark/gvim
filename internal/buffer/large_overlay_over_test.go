package buffer_test

import (
	"bytes"
	"testing"

	"github.com/tbxark/gvim/internal/buffer"
)

func TestOverlayOver(t *testing.T) {
	del := func(b *buffer.Buffer, from, to buffer.Pos) { b.Delete(from, to) }
	tests := []struct {
		name string
		data []byte
		eol  string
		// saved edits the buffer before the text state is taken, later after.
		saved, later func(b *buffer.Buffer)
		wantEmpty    bool
	}{
		{name: "no edits at all", data: overlayData("\n"), eol: "\n", saved: func(*buffer.Buffer) {}, later: func(*buffer.Buffer) {}, wantEmpty: true},
		{
			name: "edit after the saved one",
			data: overlayData("\n"), eol: "\n",
			saved: func(b *buffer.Buffer) { ins(b, 3, 0, "A") },
			later: func(b *buffer.Buffer) { ins(b, 40, 2, "B") },
		},
		{
			name: "edit before the saved one",
			data: overlayData("\n"), eol: "\n",
			saved: func(b *buffer.Buffer) { ins(b, 40, 0, "A") },
			later: func(b *buffer.Buffer) { ins(b, 2, 2, "B") },
		},
		{
			name: "same line",
			data: overlayData("\n"), eol: "\n",
			saved: func(b *buffer.Buffer) { ins(b, 7, 0, "A") },
			later: func(b *buffer.Buffer) { ins(b, 7, 0, "B") },
		},
		{
			name: "lines removed and added",
			data: overlayData("\n"), eol: "\n",
			saved: func(b *buffer.Buffer) {
				ins(b, 10, 0, "x\ny\nz")
				del(b, buffer.Pos{Line: 20}, buffer.Pos{Line: 25})
			},
			later: func(b *buffer.Buffer) {
				del(b, buffer.Pos{Line: 30}, buffer.Pos{Line: 33})
				ins(b, 59, 4, "\nnew end")
			},
		},
		{
			name: "the edit of the saved text is undone",
			data: overlayData("\n"), eol: "\n",
			saved: func(b *buffer.Buffer) { ins(b, 7, 0, "A") },
			later: func(b *buffer.Buffer) { b.Undo(); ins(b, 9, 0, "B") },
		},
		{
			name: "delete the saved end",
			data: overlayData("\n"), eol: "\n",
			saved: func(b *buffer.Buffer) { ins(b, 7, 0, "A") },
			later: func(b *buffer.Buffer) { del(b, buffer.Pos{Line: 50}, b.End()) },
		},
		{
			name: "crlf file",
			data: overlayData("\r\n"), eol: "\r\n",
			saved: func(b *buffer.Buffer) { ins(b, 7, 0, "A"); ins(b, 8, 0, "\n") },
			later: func(b *buffer.Buffer) { ins(b, 9, 0, "B"); ins(b, 20, 1, "\n") },
		},
		{
			name: "mixed line endings",
			data: []byte("a\nb\r\nc\nd\r\ne\nf"), eol: "\n",
			saved: func(b *buffer.Buffer) { ins(b, 1, 0, "A") },
			later: func(b *buffer.Buffer) { ins(b, 3, 0, "B"); ins(b, 1, 0, "C") },
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b, _ := openTiny(t, tt.data, 0)
			b.SetClock(fixedClock)
			indexAll(t, b)
			tt.saved(b)
			base, ok := b.TextState()
			if !ok {
				t.Fatal("TextState() = false for a large buffer")
			}
			var out bytes.Buffer
			snap := b.Snapshot()
			_, err := snap.WriteTo(t.Context(), &out, buffer.WriteOptions{EOL: tt.eol})
			snap.Release()
			if err != nil {
				t.Fatalf("WriteTo: %v", err)
			}
			tt.later(b)
			o, err := b.OverlayOver(base)
			if err != nil {
				t.Fatalf("OverlayOver: %v", err)
			}
			if o.Empty() != tt.wantEmpty {
				t.Errorf("Empty() = %v, want %v: %+v", o.Empty(), tt.wantEmpty, o)
			}
			fresh, _ := openTiny(t, out.Bytes(), 0)
			indexAll(t, fresh)
			if err := fresh.RestoreOverlay(o); err != nil {
				t.Fatalf("RestoreOverlay: %v", err)
			}
			if got, want := fresh.Text(), b.Text(); got != want {
				t.Errorf("restored text differs:\n got %q\nwant %q", got, want)
			}
		})
	}
}

func TestOverlayOverNeedsTheIndex(t *testing.T) {
	b, _ := openTiny(t, overlayData("\n"), 0)
	base, _ := b.TextState()
	if _, err := b.OverlayOver(base); err == nil {
		t.Error("OverlayOver with an incomplete index succeeded")
	}
	if _, err := b.OverlayOver(nil); err == nil {
		t.Error("OverlayOver(nil) succeeded")
	}
	mem := buffer.New("a")
	if base, ok := mem.TextState(); ok || base != nil {
		t.Error("a memory buffer has a text state")
	}
	if _, err := mem.OverlayOver(&buffer.TextState{}); err == nil {
		t.Error("OverlayOver on a memory buffer succeeded")
	}
}
