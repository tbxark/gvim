package buffer_test

import (
	"bytes"
	"errors"
	"fmt"
	"math/rand/v2"
	"strings"
	"testing"
	"time"

	"github.com/tbxark/gvim/internal/buffer"
	"github.com/tbxark/gvim/internal/buffer/buffertest"
)

func fixedClock() time.Time { return time.Unix(1_700_000_000, 0) }

// editCorpora are the corpora whose lines can all be edited: none has a line
// that is cut at MaxLine or holds invalid UTF-8.
func editCorpora() []corpus {
	var out []corpus
	for _, c := range corpora() {
		switch c.name {
		case "line over MaxLine", "line over MaxLine with CRLF", "invalid utf8":
		default:
			out = append(out, c)
		}
	}
	return out
}

// diffEdit is one edit applied to both buffers of a differential run.
type diffEdit struct {
	insert   bool
	from, to buffer.Pos
	text     string
}

var diffTexts = []string{
	"a", "é", "\n", "xy\nz", "日本", "\r", "\n\n", " ", "word ", "tail\nof\nmany\nlines\n",
	strings.Repeat("0123456789", 12),
}

// diffRun drives the same random edits, groups, breaks, undos and redos on a
// memory buffer and on a large buffer over the same text, and compares them
// after every step. With midScan the index is built one chunk at a time between
// steps, so the large buffer sees only a prefix of the memory buffer's lines.
func diffRun(t *testing.T, c corpus, midScan bool, rng *rand.Rand) {
	t.Helper()
	b, _ := openTiny(t, c.data, c.skip)
	m := buffer.New(strings.Join(wantLines(c.data, int(c.skip), tMaxLine), "\n"))
	b.SetClock(fixedClock)
	m.SetClock(fixedClock)
	if !midScan {
		indexAll(t, b)
	}
	// A first line that is not terminated yet is provisional and not
	// editable; start once it is known.
	firstNL := bytes.IndexByte(c.data[c.skip:], '\n')
	for midScan && firstNL >= 0 && b.Index(0).Indexed <= int64(firstNL) {
		sc := b.BeginIndexScan()
		batch, err := sc.Next(t.Context(), 1)
		sc.Close()
		if err != nil {
			t.Fatal(err)
		}
		b.ApplyIndex(batch)
	}

	pos := func() buffer.Pos {
		line := rng.IntN(b.Lines())
		return buffer.Pos{Line: line, Col: rng.IntN(b.LineLen(line) + 2)}
	}
	apply := func(buf *buffer.Buffer, e diffEdit) (buffer.Pos, string) {
		if e.insert {
			return buf.Insert(e.from, e.text), ""
		}
		return buffer.Pos{}, buf.Delete(e.from, e.to)
	}
	randomEdit := func() diffEdit {
		if rng.IntN(100) < 55 {
			return diffEdit{insert: true, from: pos(), text: diffTexts[rng.IntN(len(diffTexts))]}
		}
		return diffEdit{from: pos(), to: pos()}
	}
	check := func(step int, what string) {
		t.Helper()
		n := b.Lines()
		complete := b.Index(0).Complete
		if complete && n != m.Lines() || n > m.Lines() {
			t.Fatalf("step %d (%s): Lines = %d, memory %d (complete %v)", step, what, n, m.Lines(), complete)
		}
		for i := range n {
			if got, want := b.Line(i), m.Line(i); got != want {
				t.Fatalf("step %d (%s): Line(%d) = %q, want %q", step, what, i, got, want)
			}
			if got, want := b.LineBytes(i), m.LineBytes(i); got != want {
				t.Fatalf("step %d (%s): LineBytes(%d) = %d, want %d", step, what, i, got, want)
			}
		}
		if b.Modified() != m.Modified() {
			t.Fatalf("step %d (%s): Modified = %v, want %v", step, what, b.Modified(), m.Modified())
		}
		if complete && step%10 == 0 && b.Text() != m.Text() {
			t.Fatalf("step %d (%s): Text differs", step, what)
		}
		if s := b.Stats(); s.OverCap != 0 {
			t.Fatalf("step %d (%s): OverCap = %d", step, what, s.OverCap)
		}
		if err := b.Refusal(); err != nil {
			t.Fatalf("step %d (%s): unexpected refusal %v", step, what, err)
		}
	}

	check(-1, "open")
	for step := range 200 {
		var what string
		switch r := rng.IntN(100); {
		case r < 45:
			e := randomEdit()
			what = fmt.Sprintf("%+v", e)
			gp, gt := apply(b, e)
			wp, wt := apply(m, e)
			if gp != wp || gt != wt {
				t.Fatalf("step %d (%s): returned %v %q, memory %v %q", step, what, gp, gt, wp, wt)
			}
		case r < 55:
			what = "group"
			var edits []diffEdit
			b.Group(func() {
				for range 1 + rng.IntN(3) {
					e := randomEdit()
					edits = append(edits, e)
					apply(b, e)
				}
			})
			m.Group(func() {
				for _, e := range edits {
					apply(m, e)
				}
			})
		case r < 63:
			what = "break"
			b.Break()
			m.Break()
		case r < 77:
			what = "undo"
			gp, gok := b.Undo()
			wp, wok := m.Undo()
			if gp != wp || gok != wok {
				t.Fatalf("step %d: Undo = %v %v, memory %v %v", step, gp, gok, wp, wok)
			}
		case r < 87:
			what = "redo"
			gp, gok := b.Redo()
			wp, wok := m.Redo()
			if gp != wp || gok != wok {
				t.Fatalf("step %d: Redo = %v %v, memory %v %v", step, gp, gok, wp, wok)
			}
		case r < 90:
			what = "save"
			b.MarkSaved()
			m.MarkSaved()
		default:
			what = "index"
			if sc := b.BeginIndexScan(); sc != nil && midScan {
				batch, err := sc.Next(t.Context(), 1)
				sc.Close()
				if err != nil {
					t.Fatal(err)
				}
				b.ApplyIndex(batch)
			}
		}
		check(step, what)
	}
	// Whatever the index state was, finishing it must give the memory text.
	indexAll(t, b)
	check(1000, "final")
	if b.Text() != m.Text() {
		t.Fatalf("final Text differs:\n%q\n%q", b.Text(), m.Text())
	}
	for {
		gp, gok := b.Undo()
		wp, wok := m.Undo()
		if gp != wp || gok != wok {
			t.Fatalf("undo-all: %v %v, memory %v %v", gp, gok, wp, wok)
		}
		if !gok {
			break
		}
	}
	if b.Text() != m.Text() || b.Modified() != m.Modified() {
		t.Fatalf("after undo-all: %q modified %v, memory %q modified %v", b.Text(), b.Modified(), m.Text(), m.Modified())
	}
}

func TestLargeEditsMatchMemoryBuffer(t *testing.T) {
	cs := editCorpora()
	for seq := range 200 {
		c := cs[seq%len(cs)]
		mid := seq/len(cs)%2 == 1
		t.Run(fmt.Sprintf("%03d %s mid=%v", seq, c.name, mid), func(t *testing.T) {
			diffRun(t, c, mid, rand.New(rand.NewPCG(uint64(seq), 99)))
		})
	}
}

// bigEditOpen opens data with the tiny geometry plus option overrides.
func bigEditOpen(t *testing.T, data string, mod func(*buffer.LargeOptions)) *buffer.Buffer {
	t.Helper()
	o := tinyGeom(0)
	if mod != nil {
		mod(&o)
	}
	b, err := buffer.OpenLarge(buffertest.NewMemSource([]byte(data)), o)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = b.Close() })
	b.SetClock(fixedClock)
	indexAll(t, b)
	return b
}

func TestLargeEditInTailRegionCoversBaseLines(t *testing.T) {
	b := bigEditOpen(t, lines("line %02d\n", 24), nil)
	if got := b.Stats().Pieces; got != 0 {
		t.Fatalf("Pieces before any edit = %d", got)
	}
	b.Insert(buffer.Pos{Line: 10, Col: 2}, "X")
	// A run of the lines before the edit, the edited literal line, and the tail.
	if got := b.Stats().Pieces; got != 2 {
		t.Errorf("Pieces after one edit = %d, want 2", got)
	}
	if got := b.Line(10); got != "liXne 10" {
		t.Errorf("Line(10) = %q", got)
	}
	b.Break()
	b.Insert(buffer.Pos{Line: 20, Col: 0}, "Y")
	if got := b.Stats().Pieces; got < 4 {
		t.Errorf("Pieces after an edit further down = %d, want at least 4", got)
	}
	if b.Line(9) != "line 09" || b.Line(11) != "line 11" || b.Line(20) != "Yline 20" || b.Line(24) != "" {
		t.Errorf("neighbouring lines changed: %q %q %q %q", b.Line(9), b.Line(11), b.Line(20), b.Line(24))
	}
}

func TestLargeDeleteMostOfFileUndoReadsNothing(t *testing.T) {
	const nLines = 4000
	data := lines("%063d\n", nLines) // 64 bytes a line, 250 KiB
	big := func(o *buffer.LargeOptions) { o.Chunk, o.MaxLine = 4096, 1<<16 }

	t.Run("under InlineOpMax", func(t *testing.T) {
		b := bigEditOpen(t, data, big)
		want := b.Text()
		got := b.Delete(buffer.Pos{Line: 100}, buffer.Pos{Line: 3900})
		if len(got) != 3800*64 {
			t.Fatalf("Delete returned %d bytes, want %d", len(got), 3800*64)
		}
		if s := b.Stats(); s.OverlayBytes != 0 {
			t.Errorf("OverlayBytes after deleting whole lines = %d, want 0", s.OverlayBytes)
		}
		readAfterDelete := b.Stats().ReadBytes
		if _, ok := b.Undo(); !ok {
			t.Fatal("Undo failed")
		}
		if b.Stats().ReadBytes != readAfterDelete {
			t.Errorf("Undo read %d bytes", b.Stats().ReadBytes-readAfterDelete)
		}
		if b.Modified() || b.Text() != want {
			t.Error("Undo did not restore the text")
		}
	})

	t.Run("over InlineOpMax", func(t *testing.T) {
		b := bigEditOpen(t, lines("%063d\n", 16000), big) // 1 MB
		before := b.Stats()
		if got := b.Delete(buffer.Pos{Line: 100}, buffer.Pos{Line: 15000}); got != "" {
			t.Fatalf("Delete returned %d bytes, want none over InlineOpMax", len(got))
		}
		after := b.Stats()
		// Only the index is consulted to see that the span is too big to return.
		if read := after.ReadBytes - before.ReadBytes; read > 4*4096 || after.MaterializedBytes != before.MaterializedBytes {
			t.Errorf("Delete read %d bytes, materialized %d", read, after.MaterializedBytes-before.MaterializedBytes)
		}
		if after.OverlayBytes != 0 || b.Lines() != 1101 {
			t.Errorf("OverlayBytes %d, Lines %d", after.OverlayBytes, b.Lines())
		}
		if b.Line(100) != fmt.Sprintf("%063d", 15000) {
			t.Errorf("Line(100) = %q", b.Line(100))
		}
		if _, ok := b.Undo(); !ok || b.Lines() != 16001 || b.Modified() {
			t.Errorf("Undo: lines %d modified %v", b.Lines(), b.Modified())
		}
		if got := b.Stats().ReadBytes; got != after.ReadBytes {
			t.Errorf("Undo read %d bytes", got-after.ReadBytes)
		}
	})

	t.Run("mid line to mid line", func(t *testing.T) {
		b := bigEditOpen(t, data, big)
		b.Delete(buffer.Pos{Line: 100, Col: 5}, buffer.Pos{Line: 3900, Col: 7})
		if s := b.Stats(); s.OverlayBytes > 1024 {
			t.Errorf("OverlayBytes = %d, want a single joined line", s.OverlayBytes)
		}
		if got := b.Line(100); got != fmt.Sprintf("%05d", 0)+fmt.Sprintf("%063d", 3900)[7:] {
			t.Errorf("joined line = %q", got)
		}
	})
}

func TestLargeEditRefusals(t *testing.T) {
	long := strings.Repeat("a", 100)
	tests := []struct {
		name string
		data string
		mod  func(*buffer.LargeOptions)
		do   func(b *buffer.Buffer, src *buffertest.MemSource)
		want error
	}{
		{
			name: "line too long",
			data: long + "\nshort\n",
			do:   func(b *buffer.Buffer, _ *buffertest.MemSource) { b.Insert(buffer.Pos{Line: 0, Col: 3}, "X") },
			want: buffer.ErrLineTooLong,
		},
		{
			name: "delete inside a long line",
			data: "short\n" + long + "\n",
			do: func(b *buffer.Buffer, _ *buffertest.MemSource) {
				b.Delete(buffer.Pos{Line: 1, Col: 2}, buffer.Pos{Line: 1, Col: 4})
			},
			want: buffer.ErrLineTooLong,
		},
		{
			name: "invalid utf8",
			data: "ok\n\xff\xfebad\nend\n",
			do:   func(b *buffer.Buffer, _ *buffertest.MemSource) { b.Insert(buffer.Pos{Line: 1, Col: 1}, "X") },
			want: buffer.ErrInvalidUTF8,
		},
		{
			name: "overlay full",
			data: lines("line %02d\n", 24),
			mod:  func(o *buffer.LargeOptions) { o.MaxOverlayBytes = 100 },
			do: func(b *buffer.Buffer, _ *buffertest.MemSource) {
				b.Insert(buffer.Pos{Line: 1, Col: 0}, "fits")
				b.Break()
				b.Insert(buffer.Pos{Line: 2, Col: 0}, strings.Repeat("x", 200))
			},
			want: buffer.ErrOverlayFull,
		},
		{
			name: "source changed",
			data: lines("line %02d\n", 24),
			do: func(b *buffer.Buffer, src *buffertest.MemSource) {
				src.Truncate(10)
				if err := b.CheckSource(); !errors.Is(err, buffer.ErrSourceChanged) {
					panic(fmt.Sprintf("CheckSource = %v", err))
				}
				b.Insert(buffer.Pos{Line: 1, Col: 0}, "X")
			},
			want: buffer.ErrSourceChanged,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			o := tinyGeom(0)
			if tt.mod != nil {
				tt.mod(&o)
			}
			src := buffertest.NewMemSource([]byte(tt.data))
			b, err := buffer.OpenLarge(src, o)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = b.Close() }()
			b.SetClock(fixedClock)
			indexAll(t, b)
			if tt.name == "overlay full" {
				// Edits before the cap is hit are fine.
				b.Insert(buffer.Pos{Line: 1, Col: 0}, "fits")
				if err := b.Refusal(); err != nil {
					t.Fatalf("first edit refused: %v", err)
				}
				b.Undo()
			}
			beforeText, beforeVer := textOf(b), b.Version()
			tt.do(b, src)
			if err := b.Refusal(); !errors.Is(err, tt.want) {
				t.Fatalf("Refusal = %v, want %v", err, tt.want)
			}
			// The refused edit is the last one tried; nothing of it shows.
			switch tt.name {
			case "overlay full":
				if b.Line(1) != "fits"+"line 01" || b.Line(2) != "line 02" {
					t.Errorf("lines after the refused edit: %q %q", b.Line(1), b.Line(2))
				}
			case "source changed":
				if b.Version() != beforeVer || b.Modified() {
					t.Errorf("version %d -> %d, modified %v", beforeVer, b.Version(), b.Modified())
				}
			default:
				if textOf(b) != beforeText || b.Version() != beforeVer {
					t.Errorf("refused edit changed the buffer: version %d -> %d", beforeVer, b.Version())
				}
			}
		})
	}
}

// textOf returns the visible text line by line, which stays available when
// the source has failed.
func textOf(b *buffer.Buffer) string {
	var sb strings.Builder
	for i := range b.Lines() {
		sb.WriteString(b.Line(i))
		sb.WriteByte('\n')
	}
	return sb.String()
}

func TestRefusedEditDoesNotTouchRedoOrSeq(t *testing.T) {
	long := strings.Repeat("a", 100)
	b := bigEditOpen(t, "one\ntwo\n"+long+"\n", nil)
	b.Insert(buffer.Pos{Line: 0, Col: 3}, "!")
	b.Break()
	if _, ok := b.Undo(); !ok {
		t.Fatal("Undo failed")
	}
	ver := b.Version()

	if got := b.Insert(buffer.Pos{Line: 2, Col: 4}, "X"); got != (buffer.Pos{Line: 2, Col: 4}) {
		t.Errorf("refused Insert returned %v, want the clamped position", got)
	}
	if got := b.Delete(buffer.Pos{Line: 2, Col: 1}, buffer.Pos{Line: 2, Col: 2}); got != "" {
		t.Errorf("refused Delete returned %q", got)
	}
	if !errors.Is(b.Refusal(), buffer.ErrLineTooLong) {
		t.Error("no refusal recorded")
	}
	if b.Version() != ver {
		t.Errorf("version %d -> %d", ver, b.Version())
	}
	// The redo step survived the refused edits.
	if _, ok := b.Redo(); !ok || b.Line(0) != "one!" {
		t.Errorf("Redo after refused edits: line %q", b.Line(0))
	}
}

func TestGroupRefusalRolledBack(t *testing.T) {
	long := strings.Repeat("a", 100)
	newBuf := func(t *testing.T) *buffer.Buffer {
		b := bigEditOpen(t, "one\ntwo\n"+long+"\nfour\n", nil)
		b.Insert(buffer.Pos{Line: 0, Col: 3}, "!")
		b.Break()
		b.Insert(buffer.Pos{Line: 1, Col: 0}, "#")
		b.Break()
		if _, ok := b.Undo(); !ok { // leave one redo step
			t.Fatal("Undo failed")
		}
		return b
	}

	t.Run("restores redo, version and state", func(t *testing.T) {
		b := newBuf(t)
		text, ver, stats := textOf(b), b.Version(), b.Stats()
		b.Group(func() {
			b.Insert(buffer.Pos{Line: 0, Col: 0}, "A")
			b.Delete(buffer.Pos{Line: 1, Col: 0}, buffer.Pos{Line: 1, Col: 2})
			b.Insert(buffer.Pos{Line: 2, Col: 1}, "refused")
			b.Insert(buffer.Pos{Line: 3, Col: 0}, "B")
		})
		if !errors.Is(b.Refusal(), buffer.ErrLineTooLong) {
			t.Error("Refusal does not report the refused edit")
		}
		if textOf(b) != text || b.Version() != ver {
			t.Errorf("group was not rolled back: version %d -> %d\n%s", ver, b.Version(), textOf(b))
		}
		if after := b.Stats(); after.HistoryGroups != stats.HistoryGroups || after.OverlayBytes != stats.OverlayBytes {
			t.Errorf("history/overlay changed: %+v -> %+v", stats, after)
		}
		if b.TxnRefused() {
			t.Error("refusal mark outlived the group")
		}
		if _, ok := b.Redo(); !ok || b.Line(1) != "#two" {
			t.Errorf("redo step lost: line 1 = %q", b.Line(1))
		}
	})

	t.Run("nested group rolls back the outer one", func(t *testing.T) {
		b := newBuf(t)
		text := textOf(b)
		b.Group(func() {
			b.Insert(buffer.Pos{Line: 0, Col: 0}, "A")
			b.Group(func() { b.Insert(buffer.Pos{Line: 2, Col: 1}, "refused") })
			b.Insert(buffer.Pos{Line: 3, Col: 0}, "B")
		})
		if textOf(b) != text {
			t.Errorf("nested refusal left edits behind:\n%s", textOf(b))
		}
	})

	t.Run("a group without refusals stays one step", func(t *testing.T) {
		b := newBuf(t)
		b.Group(func() {
			b.Insert(buffer.Pos{Line: 0, Col: 0}, "A")
			b.Insert(buffer.Pos{Line: 3, Col: 0}, "B")
		})
		if err := b.Refusal(); err != nil {
			t.Fatal(err)
		}
		if _, ok := b.Undo(); !ok || b.Line(0) != "one!" || b.Line(3) != "four" {
			t.Errorf("Undo did not revert both edits: %q %q", b.Line(0), b.Line(3))
		}
		if _, ok := b.Redo(); !ok || b.Line(0) != "Aone!" || b.Line(3) != "Bfour" {
			t.Errorf("Redo did not reapply both edits: %q %q", b.Line(0), b.Line(3))
		}
	})

	t.Run("over-cap read marks the group", func(t *testing.T) {
		b := bigEditOpen(t, lines("line %02d\n", 24), nil)
		before := textOf(b)
		ver := b.Version()
		buffer.SetMaterializeCap(t, 8)
		b.Group(func() {
			b.Insert(buffer.Pos{Line: 0, Col: 0}, "X")
			_ = b.Text()
		})
		if textOf(b) != before || b.Version() != ver || b.Modified() {
			t.Errorf("over-cap read left the group applied: version %d -> %d", ver, b.Version())
		}
		if _, ok := b.Undo(); ok {
			t.Error("rolled back group is on the undo stack")
		}
	})
}

func TestGroupPanicKeepsEditsLargeAndMemory(t *testing.T) {
	long := strings.Repeat("a", 100)
	large := bigEditOpen(t, "one\ntwo\n"+long+"\n", nil)
	for name, b := range map[string]*buffer.Buffer{"large": large, "memory": buffer.New("one\ntwo\n" + long + "\n")} {
		t.Run(name, func(t *testing.T) {
			func() {
				defer func() {
					if recover() == nil {
						t.Error("panic was swallowed")
					}
				}()
				b.Group(func() {
					b.Insert(buffer.Pos{Line: 0, Col: 0}, "A")
					b.Insert(buffer.Pos{Line: 1, Col: 0}, "B")
					if name == "large" {
						b.Insert(buffer.Pos{Line: 2, Col: 3}, "refused")
					}
					panic("boom")
				})
			}()
			if b.Line(0) != "Aone" || b.Line(1) != "Btwo" {
				t.Errorf("edits before the panic were dropped: %q %q", b.Line(0), b.Line(1))
			}
			if _, ok := b.Undo(); !ok || b.Line(0) != "one" || b.Line(1) != "two" {
				t.Errorf("the edits are not one undo step: %q %q", b.Line(0), b.Line(1))
			}
		})
	}
}

func TestLargeHistoryCaps(t *testing.T) {
	step := func(b *buffer.Buffer, line int) {
		b.Break()
		b.Insert(buffer.Pos{Line: line, Col: 0}, "x")
	}

	t.Run("group count", func(t *testing.T) {
		b := bigEditOpen(t, lines("line %02d\n", 24), func(o *buffer.LargeOptions) { o.MaxHistoryGroups = 3 })
		b.MarkSaved()
		for i := range 5 {
			step(b, i)
		}
		if got := b.Stats().HistoryGroups; got != 3 {
			t.Fatalf("HistoryGroups = %d, want 3", got)
		}
		undone := 0
		for {
			if _, ok := b.Undo(); !ok {
				break
			}
			undone++
		}
		if undone != 3 {
			t.Errorf("undid %d steps, want 3", undone)
		}
		// The saved text is out of reach, so the buffer stays modified.
		if !b.Modified() || b.Line(0) != "xline 00" || b.Line(1) != "xline 01" || b.Line(2) != "line 02" {
			t.Errorf("after undoing everything: modified %v, lines %q %q %q", b.Modified(), b.Line(0), b.Line(1), b.Line(2))
		}
	})

	t.Run("bytes", func(t *testing.T) {
		b := bigEditOpen(t, lines("line %02d\n", 24), func(o *buffer.LargeOptions) { o.MaxHistoryBytes = 200 })
		for i := range 10 {
			step(b, i)
		}
		s := b.Stats()
		if s.HistoryBytes > 200+200 || s.HistoryGroups < 1 || s.HistoryGroups >= 10 {
			t.Errorf("HistoryBytes %d, HistoryGroups %d: oldest groups were not dropped", s.HistoryBytes, s.HistoryGroups)
		}
		if _, ok := b.Undo(); !ok {
			t.Error("the newest edit cannot be undone")
		}
	})

	t.Run("modified stays right", func(t *testing.T) {
		b := bigEditOpen(t, lines("line %02d\n", 24), func(o *buffer.LargeOptions) { o.MaxHistoryGroups = 2 })
		step(b, 0)
		step(b, 1)
		b.MarkSaved()
		if b.Modified() {
			t.Fatal("Modified after save")
		}
		step(b, 2)
		step(b, 3)
		b.Undo()
		b.Undo()
		if b.Modified() {
			t.Error("Modified after undoing back to the saved state, which history still holds")
		}
		b.Redo()
		b.Redo()
		step(b, 4) // clears nothing: the redo steps were taken
		if !b.Modified() {
			t.Fatal("not Modified after edits")
		}
		b.Undo()
		b.Undo()
		b.Undo()
		if !b.Modified() {
			t.Error("Modified reports saved when the saved state was dropped from history")
		}
	})
}

func TestLargeDeleteWholeDocumentWithUnreadableLastLine(t *testing.T) {
	long := strings.Repeat("z", 100)
	tests := []struct{ name, data string }{
		{"huge last line", "one\ntwo\n" + long},
		{"invalid utf8 last line", "one\ntwo\n\xff\xfe"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := bigEditOpen(t, tt.data, nil)
			b.Delete(buffer.Pos{}, b.End())
			if err := b.Refusal(); err != nil {
				t.Fatalf("Delete of everything refused: %v", err)
			}
			if b.Lines() != 1 || b.Line(0) != "" || !b.Empty() {
				t.Errorf("document after deleting everything: %d lines, %q", b.Lines(), b.Line(0))
			}
			if _, ok := b.Undo(); !ok || b.Lines() != 3 || b.Modified() {
				t.Errorf("Undo: %d lines modified %v", b.Lines(), b.Modified())
			}
		})
	}
}

func TestLargeDeleteFromColZeroToLineStartReadsNothing(t *testing.T) {
	long := strings.Repeat("y", 100)
	// The lines being dropped are one that is too long and one that is not
	// valid UTF-8; neither is read.
	b := bigEditOpen(t, "keep\n"+long+"\n\xff\xfe\nrest\n", nil)
	before := b.Stats()
	got := b.Delete(buffer.Pos{Line: 1}, buffer.Pos{Line: 3})
	if err := b.Refusal(); err != nil {
		t.Fatalf("refused: %v", err)
	}
	// The dropped lines are neither decoded nor checked; the only reads are
	// the bytes of the text Delete returns.
	if after := b.Stats(); after.DecodedRunes != before.DecodedRunes {
		t.Errorf("Delete decoded %d runes", after.DecodedRunes-before.DecodedRunes)
	}
	if want := long[:64] + "\n\ufffd\ufffd\n"; got != want {
		t.Errorf("Delete returned %q, want %q", got, want)
	}
	if b.Line(0) != "keep" || b.Line(1) != "rest" || b.Lines() != 3 {
		t.Errorf("lines: %q %q (%d)", b.Line(0), b.Line(1), b.Lines())
	}
}

func TestLargeDeleteMidHugeTailRefused(t *testing.T) {
	long := strings.Repeat("y", 100)
	b := bigEditOpen(t, "head\n"+long+"\nlast\n", nil)
	// The cut line is not the last one, so a delete ending inside it is refused.
	b.Delete(buffer.Pos{Line: 0, Col: 1}, buffer.Pos{Line: 1, Col: 10})
	if err := b.Refusal(); !errors.Is(err, buffer.ErrLineTooLong) {
		t.Errorf("Refusal = %v, want ErrLineTooLong", err)
	}
	// Ending at the end of its cut text is not enough either.
	b.Delete(buffer.Pos{Line: 0, Col: 1}, buffer.Pos{Line: 1, Col: b.LineLen(1)})
	if err := b.Refusal(); !errors.Is(err, buffer.ErrLineTooLong) {
		t.Errorf("Refusal = %v, want ErrLineTooLong", err)
	}
	if b.Modified() || b.Line(0) != "head" {
		t.Error("refused deletes changed the buffer")
	}
}

func TestLargeLineDirtyFollowsEdits(t *testing.T) {
	b := bigEditOpen(t, lines("line %02d\n", 8), nil)
	for i := -1; i <= b.Lines(); i++ {
		if b.LineDirty(i) {
			t.Fatalf("LineDirty(%d) on an unedited buffer", i)
		}
	}
	b.Insert(buffer.Pos{Line: 3, Col: 0}, "new\nlines\n")
	got := ""
	for i := range b.Lines() {
		if b.LineDirty(i) {
			got += fmt.Sprint(i)
		}
	}
	if got != "345" {
		t.Errorf("dirty lines %q, want 345", got)
	}
	b.MarkSaved()
	if b.LineDirty(3) {
		t.Error("LineDirty after MarkSaved")
	}
	b.Insert(buffer.Pos{Line: 0, Col: 0}, "x")
	if !b.LineDirty(0) {
		t.Error("not dirty after editing line 0")
	}
	b.Undo()
	if b.LineDirty(0) {
		t.Error("dirty after undoing back to the saved text")
	}
}

func TestLargeStatsTrackOverlayAndHistory(t *testing.T) {
	b := bigEditOpen(t, lines("line %02d\n", 24), nil)
	s := b.Stats()
	if s.Pieces != 0 || s.OverlayBytes != 0 || s.HistoryBytes != 0 || s.HistoryGroups != 0 {
		t.Fatalf("fresh stats: %+v", s)
	}
	b.Insert(buffer.Pos{Line: 5, Col: 0}, "abcdef")
	s = b.Stats()
	if s.Pieces != 2 || s.OverlayBytes <= 0 || s.HistoryBytes <= 0 || s.HistoryGroups != 1 {
		t.Errorf("after an edit: %+v", s)
	}
	b.Undo()
	s = b.Stats()
	if s.Pieces != 0 || s.OverlayBytes != 0 || s.HistoryGroups != 1 {
		t.Errorf("after undo: %+v (the undone group stays redoable)", s)
	}
}

func TestLargeInsertJoinAndEnterOnCRLFDoc(t *testing.T) {
	b := bigEditOpen(t, "a\r\nb\r\nc\r\n", nil)
	b.Insert(buffer.Pos{Line: 0, Col: 1}, "\n")
	if b.Lines() != 5 || b.Line(0) != "a" || b.Line(1) != "" || b.Line(2) != "b" {
		t.Errorf("lines after Enter: %q %q %q (%d)", b.Line(0), b.Line(1), b.Line(2), b.Lines())
	}
	for i := range b.Lines() {
		if strings.Contains(b.Line(i), "\r") {
			t.Errorf("Line(%d) = %q shows a carriage return", i, b.Line(i))
		}
	}
	// Join the two new lines back.
	b.Delete(buffer.Pos{Line: 0, Col: 1}, buffer.Pos{Line: 1, Col: 0})
	if b.Text() != "a\nb\nc\n" {
		t.Errorf("Text after join = %q", b.Text())
	}
}

func TestLineCutReportsOnlyCutLines(t *testing.T) {
	long := strings.Repeat("y", 100)
	b := bigEditOpen(t, "head\n"+long+"\nlast\n", nil)
	for line, want := range []bool{false, true, false, false} {
		if got := b.LineCut(line); got != want {
			t.Errorf("LineCut(%d) = %v, want %v", line, got, want)
		}
	}
	// An edit elsewhere leaves the cut line cut.
	b.Insert(buffer.Pos{Line: 0, Col: 0}, "x")
	if b.LineCut(0) || !b.LineCut(1) {
		t.Errorf("after an edit: LineCut(0) = %v, LineCut(1) = %v", b.LineCut(0), b.LineCut(1))
	}
	if buffer.New(long).LineCut(0) {
		t.Error("a memory buffer cuts no line")
	}
}
