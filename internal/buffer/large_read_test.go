package buffer_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/tbxark/gvim/internal/buffer"
	"github.com/tbxark/gvim/internal/buffer/buffertest"
)

// Tiny geometry: chunk edges fall inside lines, CRLFs and runes.
const (
	tChunk   = 16
	tMaxLine = 64
	tInitial = 32
)

func tinyGeom(skip int64) buffer.LargeOptions {
	return buffer.LargeOptions{
		Chunk: tChunk, CachePages: 2, MaxLine: tMaxLine, InitialBytes: tInitial, Skip: skip,
		Budget: buffer.NewPageBudget(1 << 20),
	}
}

func openTiny(t testing.TB, data []byte, skip int64) (*buffer.Buffer, *buffertest.MemSource) {
	t.Helper()
	src := buffertest.NewMemSource(data)
	b, err := buffer.OpenLarge(src, tinyGeom(skip))
	if err != nil {
		t.Fatalf("OpenLarge: %v", err)
	}
	t.Cleanup(func() { _ = b.Close() })
	return b, src
}

// indexAll applies index batches until the index is complete.
func indexAll(t testing.TB, b *buffer.Buffer) {
	t.Helper()
	for !b.Index(0).Complete {
		sc := b.BeginIndexScan()
		if sc == nil {
			t.Fatalf("BeginIndexScan returned nil with an incomplete index (err %v)", b.SourceErr())
		}
		batch, err := sc.Next(context.Background(), 1<<20)
		sc.Close()
		if err != nil {
			t.Fatalf("Next: %v", err)
		}
		b.ApplyIndex(batch)
	}
}

// wantLines is what a large buffer must show for the bytes after skip: the
// source lines, a CR dropped from every terminated line, bad UTF-8 shown as
// U+FFFD, and lines over maxLine bytes cut at a rune start.
func wantLines(data []byte, skip, maxLine int) []string {
	parts := bytes.Split(data[skip:], []byte("\n"))
	out := make([]string, len(parts))
	for i, p := range parts {
		if i < len(parts)-1 {
			p = bytes.TrimSuffix(p, []byte("\r"))
		}
		if len(p) > maxLine {
			cut := maxLine
			for k := 0; k < 3 && cut > 0 && !utf8.RuneStart(p[cut]); k++ {
				cut--
			}
			p = p[:cut]
		}
		out[i] = string([]rune(string(p)))
	}
	return out
}

type corpus struct {
	name       string
	data       []byte
	skip       int64
	exactBytes bool // LineBytes agrees with a memory buffer
}

func lines(format string, n int) string {
	var sb strings.Builder
	for i := range n {
		fmt.Fprintf(&sb, format, i)
	}
	return sb.String()
}

func corpora() []corpus {
	return []corpus{
		{name: "ascii", data: []byte(lines("alpha line %02d\n", 24)), exactBytes: true},
		{name: "multibyte", data: []byte(lines("héllo wörld %d 日本語 😀\n", 12)), exactBytes: true},
		{name: "crlf", data: []byte(lines("crlf line %02d\r\n", 12)), exactBytes: true},
		{name: "mixed", data: []byte("a\r\nb\nc\r\n\nx\ry\nz\r\r\n\r\nend\r"), exactBytes: true},
		{name: "no trailing newline", data: []byte("one\ntwo\nthree"), exactBytes: true},
		{name: "trailing newline", data: []byte("one\ntwo\nthree\n"), exactBytes: true},
		{name: "empty", data: nil, exactBytes: true},
		{name: "only newlines", data: bytes.Repeat([]byte("\n"), 40), exactBytes: true},
		{name: "one line over many chunks", data: []byte(strings.Repeat("0123456789", 6) + "\nshort\n"), exactBytes: true},
		{name: "line over MaxLine", data: []byte(strings.Repeat("a", 63) + "é" + strings.Repeat("b", 100) + "\nnext\n")},
		{name: "line over MaxLine with CRLF", data: []byte(strings.Repeat("c", 90) + "\r\nnext\r\n")},
		{name: "line exactly MaxLine", data: []byte(strings.Repeat("d", 64) + "\nnext"), exactBytes: true},
		{name: "bom", data: []byte("\xef\xbb\xbfline one\nline two\r\nline three"), skip: 3, exactBytes: true},
		{name: "invalid utf8", data: []byte("ok\n\xff\xfebad\nend\xc3\n\xe2\x82\n\xc3\x28 x\n")},
		{name: "newline at chunk edge", data: []byte(strings.Repeat(strings.Repeat("a", 15)+"\n", 5)), exactBytes: true},
		{name: "CRLF split by chunk edge", data: []byte(strings.Repeat("a", 15) + "\r\nb\r\n" + strings.Repeat("c", 14) + "\r\nd"), exactBytes: true},
		{name: "rune split by chunk edge", data: []byte(strings.Repeat("a", 15) + "é\n" + strings.Repeat("b", 14) + "日\nend"), exactBytes: true},
	}
}

func TestLargeReadMatchesMemoryBuffer(t *testing.T) {
	for _, c := range corpora() {
		t.Run(c.name, func(t *testing.T) {
			want := wantLines(c.data, int(c.skip), tMaxLine)
			b, _ := openTiny(t, c.data, c.skip)
			if !b.Large() || !b.Stats().Large {
				t.Fatal("buffer is not large")
			}

			// Apply the index one chunk at a time. Lines() never shrinks and
			// every visible line already has its final text.
			prev := 0
			for step := 0; ; step++ {
				info := b.Index(0)
				n := b.Lines()
				if n < prev {
					t.Fatalf("step %d: Lines() shrank from %d to %d", step, prev, n)
				}
				prev = n
				wantN := len(want)
				if !info.Complete {
					wantN = max(1, bytes.Count(c.data[c.skip:c.skip+info.Indexed], []byte("\n")))
				}
				if n != wantN || info.Lines != n {
					t.Fatalf("step %d: Lines() = %d, Index.Lines = %d, want %d (indexed %d of %d)",
						step, n, info.Lines, wantN, info.Indexed, info.Size)
				}
				for i := range n {
					if got := b.Line(i); got != want[i] {
						t.Fatalf("step %d: Line(%d) = %q, want %q", step, i, got, want[i])
					}
				}
				if info.Complete {
					break
				}
				sc := b.BeginIndexScan()
				batch, err := sc.Next(context.Background(), 1)
				sc.Close()
				if err != nil || len(batch.Counts) != 1 {
					t.Fatalf("step %d: Next = %d chunks, %v; want exactly one chunk", step, len(batch.Counts), err)
				}
				b.ApplyIndex(batch)
			}
			compareWithMemory(t, b, want, c.exactBytes)
		})
	}
}

// compareWithMemory checks every read method against a memory buffer that holds
// the expected lines.
func compareWithMemory(t *testing.T, b *buffer.Buffer, want []string, exactBytes bool) {
	t.Helper()
	m := buffer.New(strings.Join(want, "\n"))
	if b.Lines() != m.Lines() {
		t.Fatalf("Lines = %d, want %d", b.Lines(), m.Lines())
	}
	if got, w := b.Text(), m.Text(); got != w {
		t.Fatalf("Text = %q, want %q", got, w)
	}
	if got, err := b.TextChecked(); err != nil || got != m.Text() {
		t.Fatalf("TextChecked = %q, %v", got, err)
	}
	if b.Empty() != m.Empty() {
		t.Errorf("Empty = %v, want %v", b.Empty(), m.Empty())
	}
	if b.End() != m.End() {
		t.Errorf("End = %v, want %v", b.End(), m.End())
	}
	for _, n := range []int{0, 1, 5, 17, 1000} {
		if got, w := b.TextPrefix(n), m.TextPrefix(n); got != w {
			t.Errorf("TextPrefix(%d) = %q, want %q", n, got, w)
		}
	}
	for _, p := range []buffer.Pos{{Line: -3, Col: -3}, {Line: 1 << 30, Col: 1 << 30}, {Line: 0, Col: 1000}} {
		if got, w := b.Clamp(p), m.Clamp(p); got != w {
			t.Errorf("Clamp(%v) = %v, want %v", p, got, w)
		}
	}
	for i := -1; i <= m.Lines(); i++ {
		if got, w := b.Line(i), m.Line(i); got != w {
			t.Errorf("Line(%d) = %q, want %q", i, got, w)
		}
		if got, w := b.LineLen(i), m.LineLen(i); got != w {
			t.Errorf("LineLen(%d) = %d, want %d", i, got, w)
		}
		if exactBytes {
			if got, w := b.LineBytes(i), m.LineBytes(i); got != w {
				t.Errorf("LineBytes(%d) = %d, want %d", i, got, w)
			}
		}
		for _, s := range [][2]int{{0, 3}, {1, 3}, {2, 100}, {50, 3}, {-2, 2}} {
			if got, w := b.LineSlice(i, s[0], s[1]), m.LineSlice(i, s[0], s[1]); got != w {
				t.Errorf("LineSlice(%d, %d, %d) = %q, want %q", i, s[0], s[1], got, w)
			}
		}
	}
	for i := range m.Lines() {
		for col := -1; col <= m.LineLen(i)+1; col++ {
			p := buffer.Pos{Line: i, Col: col}
			gr, gok := b.RuneAt(p)
			wr, wok := m.RuneAt(p)
			if gr != wr || gok != wok {
				t.Errorf("RuneAt(%v) = %q, %v; want %q, %v", p, gr, gok, wr, wok)
			}
		}
		for j := i; j < min(i+4, m.Lines()); j++ {
			for _, cols := range [][2]int{{0, 0}, {1, 2}, {0, 1 << 20}, {3, 0}} {
				from, to := buffer.Pos{Line: i, Col: cols[0]}, buffer.Pos{Line: j, Col: cols[1]}
				if got, w := b.Range(from, to), m.Range(from, to); got != w {
					t.Errorf("Range(%v, %v) = %q, want %q", from, to, got, w)
				}
				if got, w := b.Range(to, from), m.Range(to, from); got != w {
					t.Errorf("Range(%v, %v) = %q, want %q", to, from, got, w)
				}
			}
		}
	}
	if s := b.Stats(); s.OverCap != 0 {
		t.Errorf("OverCap = %d, want 0", s.OverCap)
	}
	if b.Version() != 0 || b.Modified() {
		t.Errorf("reading changed the version (%d) or Modified (%v)", b.Version(), b.Modified())
	}
}

func TestLargeLongLineCutAtRuneStart(t *testing.T) {
	tests := []struct {
		name string
		line string
		want string
	}{
		{"cut falls inside a rune", strings.Repeat("a", 63) + "é" + "bbb", strings.Repeat("a", 63)},
		{"cut falls on a rune start", strings.Repeat("a", 62) + "é" + "bbb", strings.Repeat("a", 62) + "é"},
		{"line is exactly MaxLine", strings.Repeat("a", 64), strings.Repeat("a", 64)},
		{"line is one byte over", strings.Repeat("a", 65), strings.Repeat("a", 64)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b, _ := openTiny(t, []byte(tt.line+"\ntail\n"), 0)
			indexAll(t, b)
			if got := b.Line(0); got != tt.want {
				t.Errorf("Line(0) = %q, want %q", got, tt.want)
			}
			if got := b.Line(1); got != "tail" {
				t.Errorf("Line(1) = %q, want tail", got)
			}
		})
	}
}

func TestLargeProvisionalFirstLine(t *testing.T) {
	data := []byte(strings.Repeat("x", 200) + "\nrest\n")
	b, _ := openTiny(t, data, 0)

	if got := b.Stats().ReadBytes; got > tInitial+tMaxLine {
		t.Errorf("ReadBytes after open = %d, want <= %d", got, tInitial+tMaxLine)
	}
	info := b.Index(0)
	if info.Complete || !info.HugeLine {
		t.Fatalf("Index = %+v, want incomplete with HugeLine", info)
	}
	if b.Lines() != 1 {
		t.Fatalf("Lines = %d, want the one provisional line", b.Lines())
	}
	want := strings.Repeat("x", tMaxLine)
	if got := b.Line(0); got != want {
		t.Fatalf("provisional Line(0) = %q, want %d x", got, tMaxLine)
	}
	if got := b.LineLen(0); got != tMaxLine {
		t.Errorf("LineLen(0) = %d, want %d", got, tMaxLine)
	}

	indexAll(t, b)
	if b.Lines() != 3 || b.Line(0) != want || b.Line(1) != "rest" || b.Line(2) != "" {
		t.Errorf("after index: %d lines, %q, %q, %q", b.Lines(), b.Line(0), b.Line(1), b.Line(2))
	}
	if !b.Index(0).HugeLine {
		t.Error("HugeLine cleared after the scan")
	}
}

func TestLargeIndexInfo(t *testing.T) {
	data := []byte(lines("line %05d\n", 3000)) // 10 bytes each
	b, _ := openTiny(t, data, 0)
	info := b.Index(10)
	if info.Complete || info.Size != int64(len(data)) {
		t.Fatalf("Index = %+v", info)
	}
	known := info.Lines
	// Fewer than 1000 lines known: the estimate uses estLineLen.
	if known >= 1000 {
		t.Fatalf("test needs fewer than 1000 known lines, got %d", known)
	}
	if want := known + int(info.Size-info.Indexed)/10; info.ApproxLines != want {
		t.Errorf("ApproxLines = %d, want %d", info.ApproxLines, want)
	}
	if got := b.Index(0).ApproxLines; got < known {
		t.Errorf("ApproxLines with default estimate = %d, below Lines %d", got, known)
	}
	indexAll(t, b)
	info = b.Index(10)
	if !info.Complete || info.ApproxLines != 3001 || info.Lines != 3001 || info.Indexed != info.Size {
		t.Errorf("complete Index = %+v, want 3001 exact lines", info)
	}
	if info.HugeLine {
		t.Error("HugeLine set for short lines")
	}

	// With 1000 or more lines known the average of the indexed part is used.
	big := bytes.Repeat([]byte("0123456789\n"), 5000)
	b2, err := buffer.OpenLarge(buffertest.NewMemSource(big), buffer.LargeOptions{Chunk: 256, InitialBytes: 256 * 100, Budget: buffer.NewPageBudget(1 << 20)})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = b2.Close() }()
	in := b2.Index(1)
	if in.Complete || in.Lines < 1000 {
		t.Fatalf("Index = %+v, want incomplete with 1000+ lines", in)
	}
	if want := in.Lines + int(in.Size-in.Indexed)/11; in.ApproxLines != want {
		t.Errorf("ApproxLines = %d, want %d", in.ApproxLines, want)
	}
}

func TestLargeBoundedWork(t *testing.T) {
	data := []byte(lines("line %05d\n", 6000)) // 66 KB, 11 bytes per line
	b, _ := openTiny(t, data, 0)
	if got := b.Stats().ReadBytes; got > tInitial+tMaxLine {
		t.Errorf("ReadBytes after open = %d, want <= %d", got, tInitial+tMaxLine)
	}
	indexAll(t, b)

	for k := 2000; k < 2050; k++ {
		before := b.Stats().ReadBytes
		if got, want := b.Line(k), fmt.Sprintf("line %05d", k); got != want {
			t.Fatalf("Line(%d) = %q, want %q", k, got, want)
		}
		if b.LineLen(k) != 10 {
			t.Fatalf("LineLen(%d) = %d", k, b.LineLen(k))
		}
		if d := b.Stats().ReadBytes - before; d > 2*tChunk+11 {
			t.Fatalf("Line(%d) read %d bytes, want <= %d", k, d, 2*tChunk+11)
		}
	}
	s := b.Stats()
	if s.MaterializedBytes != 0 || s.OverCap != 0 {
		t.Errorf("scrolling materialized %d bytes, OverCap %d", s.MaterializedBytes, s.OverCap)
	}
	if s.LineCalls == 0 || s.DecodedRunes == 0 {
		t.Errorf("LineCalls %d and DecodedRunes %d should have counted the scroll", s.LineCalls, s.DecodedRunes)
	}
}

func TestLargePageCacheEvicts(t *testing.T) {
	data := []byte(lines("line %05d\n", 2000))
	b, _ := openTiny(t, data, 0)
	indexAll(t, b)
	for _, k := range []int{5, 1500, 700, 1999, 0, 1200} {
		if got, want := b.Line(k), fmt.Sprintf("line %05d", k); got != want {
			t.Fatalf("Line(%d) = %q, want %q", k, got, want)
		}
		if n := b.PageCount(); n > 2 {
			t.Fatalf("after Line(%d): %d pages cached, want <= 2", k, n)
		}
		if got := b.Stats().CacheBytes; got > 2*tChunk {
			t.Fatalf("after Line(%d): CacheBytes %d, want <= %d", k, got, 2*tChunk)
		}
	}
}

func TestLargeDecodedCacheByteBudgetEvicts(t *testing.T) {
	data := []byte(lines("line %05d\n", 100))
	o := tinyGeom(0)
	o.DecodedBudget = 3 * (4*10 + 64) // three 10-rune lines
	b, err := buffer.OpenLarge(buffertest.NewMemSource(data), o)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = b.Close() }()
	indexAll(t, b)

	for i := range 50 {
		_ = b.LineLen(i)
		if n := b.DecodedCount(); n > 3 {
			t.Fatalf("after LineLen(%d): %d decoded lines, want <= 3", i, n)
		}
	}
	// A cached line is not decoded again.
	_ = b.LineLen(49)
	before := b.Stats().DecodedRunes
	for range 10 {
		_ = b.LineLen(49)
		_, _ = b.RuneAt(buffer.Pos{Line: 49, Col: 2})
	}
	if got := b.Stats().DecodedRunes; got != before {
		t.Errorf("DecodedRunes grew from %d to %d on cached reads", before, got)
	}
}

func TestLargeRuneAtOnLongLineDecodesOnce(t *testing.T) {
	const n = 500_000
	data := append(bytes.Repeat([]byte("a"), n), "\nnext\n"...)
	b, err := buffer.OpenLarge(buffertest.NewMemSource(data), buffer.LargeOptions{Budget: buffer.NewPageBudget(64 << 20)})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = b.Close() }()
	for c := range n {
		if r, ok := b.RuneAt(buffer.Pos{Col: c}); !ok || r != 'a' {
			t.Fatalf("RuneAt(0, %d) = %q, %v", c, r, ok)
		}
	}
	if got := b.Stats().DecodedRunes; got > 2*n {
		t.Errorf("DecodedRunes = %d, want <= %d", got, 2*n)
	}
	// Word bounds read the line through the same cache.
	if s, e := buffer.WordBounds(b, buffer.Pos{Col: n / 2}); s.Col != 0 || e.Col != n {
		t.Errorf("WordBounds = %v, %v", s, e)
	}
}

func TestLargeLineSliceDecodesOnlyThePrefix(t *testing.T) {
	data := append(bytes.Repeat([]byte("é"), 100_000), "\n"...)
	b, err := buffer.OpenLarge(buffertest.NewMemSource(data), buffer.LargeOptions{Budget: buffer.NewPageBudget(64 << 20)})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = b.Close() }()
	if got := b.LineSlice(0, 100, 5); got != "ééééé" {
		t.Fatalf("LineSlice = %q", got)
	}
	if got := b.Stats().DecodedRunes; got != 105 {
		t.Errorf("DecodedRunes = %d, want 105", got)
	}
	if b.DecodedCount() != 0 {
		t.Error("LineSlice cached the line")
	}
	// A cached line is sliced without decoding.
	_ = b.LineLen(0)
	before := b.Stats().DecodedRunes
	if got := b.LineSlice(0, 99_998, 10); got != "éé" {
		t.Errorf("cached LineSlice = %q", got)
	}
	if got := b.Stats().DecodedRunes; got != before {
		t.Errorf("DecodedRunes grew on a cached slice")
	}
}

func TestLargeEmptySource(t *testing.T) {
	b, _ := openTiny(t, nil, 0)
	if b.Lines() != 1 || b.Line(0) != "" || b.Text() != "" || !b.Empty() || !b.Index(0).Complete {
		t.Fatalf("empty source: %d lines, text %q, empty %v", b.Lines(), b.Text(), b.Empty())
	}
	if _, ok := b.Undo(); ok {
		t.Error("Undo succeeded with no edits")
	}
}

func TestLargeOverCapRefuses(t *testing.T) {
	data := []byte(strings.Repeat("0123456789ABCDEFGH\n", 10)) // 19 bytes per line
	b, _ := openTiny(t, data, 0)
	indexAll(t, b)
	text := string(data) // ten newlines, then an empty last line
	if got := b.Text(); got != text {
		t.Fatalf("Text = %q", got)
	}

	buffer.SetMaterializeCap(t, int64(len(text)))
	if got := b.Text(); got != text {
		t.Fatalf("Text at exactly the cap = %q", got)
	}
	if err := b.Refusal(); err != nil {
		t.Fatalf("Refusal after a read within the cap = %v", err)
	}

	buffer.SetMaterializeCap(t, int64(len(text))-1)
	before := b.Stats().MaterializedBytes
	if got := b.Text(); got != "" {
		t.Errorf("Text over the cap = %q, want empty", got)
	}
	if err := b.Refusal(); !errors.Is(err, buffer.ErrOverCap) {
		t.Errorf("Refusal = %v, want ErrOverCap", err)
	}
	if err := b.Refusal(); err != nil {
		t.Errorf("second Refusal = %v, want nil (cleared)", err)
	}
	if s := b.Stats(); s.OverCap != 1 || s.MaterializedBytes != before {
		t.Errorf("OverCap %d, Materialized %d -> %d", s.OverCap, before, s.MaterializedBytes)
	}

	// The checked variants return the error and leave no refusal behind.
	if got, err := b.TextChecked(); got != "" || !errors.Is(err, buffer.ErrOverCap) {
		t.Errorf("TextChecked = %q, %v", got, err)
	}
	if got, err := b.RangeChecked(buffer.Pos{}, b.End()); got != "" || !errors.Is(err, buffer.ErrOverCap) {
		t.Errorf("RangeChecked = %q, %v", got, err)
	}
	if err := b.Refusal(); err != nil {
		t.Errorf("Refusal after checked reads = %v", err)
	}
	if got := b.Stats().OverCap; got != 3 {
		t.Errorf("OverCap = %d, want 3", got)
	}

	// A small range still works, and Range refuses like Text.
	if got := b.Range(buffer.Pos{Line: 1, Col: 2}, buffer.Pos{Line: 2, Col: 3}); got != "23456789ABCDEFGH\n012" {
		t.Errorf("small Range = %q", got)
	}
	if got := b.Range(buffer.Pos{}, b.End()); got != "" {
		t.Errorf("Range over the cap = %q", got)
	}
	_ = b.Refusal()

	// Inside Group a refused read marks the group.
	b.Group(func() {
		if b.TxnRefused() {
			t.Error("group starts refused")
		}
		_ = b.Text()
		if !b.TxnRefused() {
			t.Error("over-cap Text in a Group did not mark it refused")
		}
	})
}

func TestMemoryTextCheckedNeverErrors(t *testing.T) {
	m := buffer.New("a\nb")
	buffer.SetMaterializeCap(t, 1)
	if got, err := m.TextChecked(); got != "a\nb" || err != nil {
		t.Errorf("TextChecked = %q, %v", got, err)
	}
	if got, err := m.RangeChecked(buffer.Pos{}, m.End()); got != "a\nb" || err != nil {
		t.Errorf("RangeChecked = %q, %v", got, err)
	}
	if m.Refusal() != nil || m.Large() || m.Stats().Large {
		t.Error("memory buffer reports large state")
	}
}

func TestOpenLargeRefuses32Bit(t *testing.T) {
	buffer.SetIntSize(t, 32)
	src := buffertest.NewMemSource([]byte("x"))
	if _, err := buffer.OpenLarge(src, tinyGeom(0)); !errors.Is(err, buffer.ErrUnsupportedPlatform) {
		t.Fatalf("OpenLarge err = %v, want ErrUnsupportedPlatform", err)
	}
}

func TestOpenLargeErrorsLeaveSourceOpen(t *testing.T) {
	boom := errors.New("boom")
	budget := buffer.NewPageBudget(1 << 20)
	o := tinyGeom(0)
	o.Budget = budget

	src := buffertest.NewMemSource([]byte(lines("line %02d\n", 20)))
	src.FailNext(1, boom)
	if _, err := buffer.OpenLarge(src, o); !errors.Is(err, boom) {
		t.Fatalf("OpenLarge err = %v, want boom", err)
	}
	if src.Closed() || budget.Stores() != 0 {
		t.Errorf("after a failed open: closed %v, stores %d", src.Closed(), budget.Stores())
	}

	o.Skip = 1000
	if _, err := buffer.OpenLarge(src, o); err == nil {
		t.Error("OpenLarge accepted a skip beyond the source")
	}
	if budget.Stores() != 0 {
		t.Errorf("stores = %d after a rejected open", budget.Stores())
	}
}

func TestLargeBeginIndexScanResumesAfterReadError(t *testing.T) {
	boom := errors.New("boom")
	data := []byte(lines("line %03d\n", 100))
	b, src := openTiny(t, data, 0)

	sc := b.BeginIndexScan()
	src.FailNext(1, boom)
	batch, err := sc.Next(context.Background(), 1<<20)
	sc.Close()
	if !errors.Is(err, boom) || len(batch.Counts) != 0 {
		t.Fatalf("Next = %d chunks, %v; want the read error and nothing counted", len(batch.Counts), err)
	}
	b.ApplyIndex(batch)
	scannedBefore := b.Index(0).Indexed

	// The scan resumes where the last applied batch ended.
	indexAll(t, b)
	want := wantLines(data, 0, tMaxLine)
	if b.Lines() != len(want) || b.Text() != strings.Join(want, "\n") {
		t.Fatalf("after retry: %d lines, want %d", b.Lines(), len(want))
	}
	if scannedBefore >= b.Index(0).Indexed {
		t.Error("retry did not advance the index")
	}
}

func TestLargePageReadErrorIsStickyUntilScanRetry(t *testing.T) {
	boom := errors.New("boom")
	data := []byte(lines("line %03d\n", 100))
	b, src := openTiny(t, data, 0)
	indexAll(t, b)

	src.FailNext(1, boom)
	if got := b.Line(60); got != "" {
		t.Fatalf("Line(60) with a failing read = %q, want empty", got)
	}
	if err := b.SourceErr(); !errors.Is(err, boom) {
		t.Fatalf("SourceErr = %v, want boom", err)
	}
	// Frozen: an uncached page is not read again, even though the source works.
	if got := b.Line(70); got != "" {
		t.Errorf("Line(70) while frozen = %q, want empty", got)
	}
	// A complete index needs no scan, but asking for one thaws a read error.
	if sc := b.BeginIndexScan(); sc != nil {
		t.Fatal("BeginIndexScan returned a scan for a complete index")
	}
	if b.SourceErr() != nil {
		t.Fatalf("SourceErr after BeginIndexScan = %v, want cleared", b.SourceErr())
	}
	if got := b.Line(70); got != "line 070" {
		t.Errorf("Line(70) after thaw = %q", got)
	}
}

func TestLargeApplyIndexIgnoresForeignAndStaleBatches(t *testing.T) {
	data := []byte(lines("line %03d\n", 100))
	a, _ := openTiny(t, data, 0)
	b, _ := openTiny(t, data, 0)

	sc := a.BeginIndexScan()
	defer sc.Close()
	batch, err := sc.Next(context.Background(), 64)
	if err != nil || len(batch.Counts) == 0 {
		t.Fatalf("Next = %d chunks, %v", len(batch.Counts), err)
	}
	before := b.Index(0)
	b.ApplyIndex(batch) // another buffer's batch
	if b.Index(0) != before {
		t.Error("ApplyIndex took a batch from another buffer")
	}
	a.ApplyIndex(batch)
	applied := a.Index(0)
	a.ApplyIndex(batch) // the same batch again: First no longer matches
	if a.Index(0) != applied {
		t.Error("ApplyIndex applied a batch twice")
	}
	a.ApplyIndex(buffer.IndexBatch{}) // zero value
	if a.Index(0) != applied {
		t.Error("ApplyIndex changed state for a zero batch")
	}
}

func TestLargeScanReadsOnlyTheOriginalSize(t *testing.T) {
	data := []byte(lines("line %03d\n", 100))
	b, src := openTiny(t, data, 0)
	src.Grow([]byte(lines("appended %03d\n", 50)))

	if err := b.CheckSource(); !errors.Is(err, buffer.ErrSourceGrew) {
		t.Fatalf("CheckSource = %v, want ErrSourceGrew", err)
	}
	if !b.SourceGrew() || b.SourceErr() != nil {
		t.Errorf("SourceGrew %v, SourceErr %v; growth must not freeze the buffer", b.SourceGrew(), b.SourceErr())
	}
	indexAll(t, b)
	want := wantLines(data, 0, tMaxLine)
	if b.Lines() != len(want) || b.Text() != strings.Join(want, "\n") {
		t.Errorf("grown source changed the document: %d lines, want %d", b.Lines(), len(want))
	}
}

func TestLargeSourceChangedFreezesButKeepsCachedLines(t *testing.T) {
	data := []byte(lines("line %03d\n", 100))
	b, src := openTiny(t, data, 0)
	indexAll(t, b)
	want0 := b.Line(0)
	if want0 != "line 000" {
		t.Fatalf("Line(0) = %q", want0)
	}

	src.Truncate(40)
	if err := b.CheckSource(); !errors.Is(err, buffer.ErrSourceChanged) {
		t.Fatalf("CheckSource = %v, want ErrSourceChanged", err)
	}
	if !errors.Is(b.SourceErr(), buffer.ErrSourceChanged) {
		t.Fatalf("SourceErr = %v", b.SourceErr())
	}
	if got := b.Line(0); got != want0 {
		t.Errorf("cached Line(0) after change = %q, want %q", got, want0)
	}
	if got := b.Line(80); got != "" {
		t.Errorf("uncached Line(80) after change = %q, want empty", got)
	}
	if sc := b.BeginIndexScan(); sc != nil {
		t.Error("BeginIndexScan on a changed source returned a scan")
	}
}

func TestLargeShortReadNeverZeroFills(t *testing.T) {
	data := []byte(lines("line %03d\n", 100))
	b, src := openTiny(t, data, 0)
	indexAll(t, b)
	src.Truncate(500) // no Check: the buffer finds out when it reads

	for _, k := range []int{90, 99, 55} {
		got := b.Line(k)
		if got != "" && got != fmt.Sprintf("line %03d", k) {
			t.Errorf("Line(%d) = %q after truncation", k, got)
		}
		if strings.ContainsRune(got, 0) {
			t.Errorf("Line(%d) holds zero bytes: %q", k, got)
		}
	}
	if got := b.Line(99); got != "" {
		t.Errorf("Line(99) = %q, want empty past the truncation", got)
	}
	if !errors.Is(b.SourceErr(), buffer.ErrSourceChanged) {
		t.Errorf("SourceErr = %v, want ErrSourceChanged", b.SourceErr())
	}
}

func TestLargeLineStartMismatchSetsChanged(t *testing.T) {
	data := []byte(lines("line %03d\n", 100))
	b, src := openTiny(t, data, 0)
	indexAll(t, b)

	// Rewrite a newline in place: same size, same mtime. The page no longer
	// agrees with the index.
	at := bytes.IndexByte(data[400:], '\n') + 400
	src.Mutate(int64(at), []byte("X"))
	for i := range b.Lines() {
		_ = b.Line(i) // must not panic
		_ = b.LineLen(i)
	}
	if !errors.Is(b.SourceErr(), buffer.ErrSourceChanged) {
		t.Fatalf("SourceErr = %v, want ErrSourceChanged", b.SourceErr())
	}
}

func TestLargeCloseReleasesSourceWhenLastScanCloses(t *testing.T) {
	data := []byte(lines("line %03d\n", 100))
	b, src := openTiny(t, data, 0)
	sc := b.BeginIndexScan()
	if err := b.Close(); err != nil || b.Close() != nil {
		t.Fatalf("Close = %v (twice must be fine)", err)
	}
	if src.Closed() {
		t.Fatal("source closed while an index scan still holds it")
	}
	batch, err := sc.Next(context.Background(), 1<<20)
	if err != nil || len(batch.Counts) == 0 {
		t.Fatalf("scan after Close: %d chunks, %v", len(batch.Counts), err)
	}
	b.ApplyIndex(batch) // ignored: the buffer is closed
	if b.BeginIndexScan() != nil {
		t.Error("BeginIndexScan on a closed buffer returned a scan")
	}
	sc.Close()
	sc.Close()
	if !src.Closed() {
		t.Error("source still open after the last reference was released")
	}
}

func TestLargeBudgetDividesAcrossStores(t *testing.T) {
	budget := buffer.NewPageBudget(6 * tChunk)
	open := func() *buffer.Buffer {
		o := tinyGeom(0)
		o.CachePages = 4
		o.Budget = budget
		b, err := buffer.OpenLarge(buffertest.NewMemSource([]byte(lines("line %03d\n", 100))), o)
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	var bufs []*buffer.Buffer
	for i, want := range []int{4, 3, 2, 2} {
		bufs = append(bufs, open())
		if got := bufs[i].PageCap(); got != want {
			t.Errorf("with %d stores: PageCap = %d, want %d", i+1, got, want)
		}
	}
	if budget.Stores() != 4 {
		t.Fatalf("Stores = %d", budget.Stores())
	}
	// Every buffer stays within its share while reading.
	for _, b := range bufs {
		indexAll(t, b)
		for k := 0; k < 100; k += 7 {
			_ = b.Line(k)
		}
		if n := b.PageCount(); n > b.PageCap() {
			t.Errorf("PageCount %d over cap %d", n, b.PageCap())
		}
	}
	for _, b := range bufs[1:] {
		_ = b.Close()
	}
	if got := bufs[0].PageCap(); got != 4 || budget.Stores() != 1 {
		t.Errorf("after closing three: PageCap %d, Stores %d", got, budget.Stores())
	}
	_ = bufs[0].Close()
}

func TestLargeOpenFileSource(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f.txt")
	data := []byte(lines("file line %03d\n", 50))
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	src, err := buffer.OpenFileSource(path)
	if err != nil {
		t.Fatal(err)
	}
	if src.Size() != int64(len(data)) || src.Check() != nil {
		t.Fatalf("Size %d, Check %v", src.Size(), src.Check())
	}
	b, err := buffer.OpenLarge(src, tinyGeom(0))
	if err != nil {
		t.Fatal(err)
	}
	indexAll(t, b)
	want := wantLines(data, 0, tMaxLine)
	compareWithMemory(t, b, want, true)

	if err := b.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := src.ReadAt(make([]byte, 1), 0); err == nil {
		t.Error("file source still readable after the buffer closed it")
	}

	if _, err := buffer.OpenFileSource(dir); err == nil {
		t.Error("OpenFileSource accepted a directory")
	}
	if _, err := buffer.OpenFileSource(filepath.Join(dir, "missing")); err == nil {
		t.Error("OpenFileSource accepted a missing file")
	}
}

func TestLargeFileSourceCheck(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(t *testing.T, path string)
		want   error
	}{
		{"unchanged", func(*testing.T, string) {}, nil},
		{"append", func(t *testing.T, path string) {
			f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = f.Close() }()
			if _, err := f.WriteString("more\n"); err != nil {
				t.Fatal(err)
			}
		}, buffer.ErrSourceGrew},
		{"truncate in place", func(t *testing.T, path string) {
			if err := os.Truncate(path, 20); err != nil {
				t.Fatal(err)
			}
		}, buffer.ErrSourceChanged},
		{"same size, new mtime", func(t *testing.T, path string) {
			future := time.Now().Add(time.Hour)
			if err := os.Chtimes(path, future, future); err != nil {
				t.Fatal(err)
			}
		}, buffer.ErrSourceChanged},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "f.txt")
			if err := os.WriteFile(path, []byte(lines("file line %03d\n", 20)), 0o644); err != nil {
				t.Fatal(err)
			}
			b, err := openLargeFile(t, path)
			if err != nil {
				t.Fatal(err)
			}
			tt.mutate(t, path)
			if got := b.CheckSource(); !errors.Is(got, tt.want) {
				t.Errorf("CheckSource = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestLargeAtomicReplaceKeepsDisplayingOldFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "f.txt")
	if err := os.WriteFile(path, []byte(lines("old line %03d\n", 20)), 0o644); err != nil {
		t.Fatal(err)
	}
	b, err := openLargeFile(t, path)
	if err != nil {
		t.Fatal(err)
	}
	indexAll(t, b)

	// Another program saves by rename: the open descriptor still sees the old
	// inode, so nothing changes for the buffer.
	repl := path + ".new"
	if err := os.WriteFile(repl, []byte("brand new\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(repl, path); err != nil {
		t.Fatal(err)
	}
	if err := b.CheckSource(); err != nil {
		t.Errorf("CheckSource after atomic replace = %v, want nil", err)
	}
	if got := b.Line(15); got != "old line 015" {
		t.Errorf("Line(15) = %q", got)
	}
}

func openLargeFile(t *testing.T, path string) (*buffer.Buffer, error) {
	t.Helper()
	src, err := buffer.OpenFileSource(path)
	if err != nil {
		return nil, err
	}
	b, err := buffer.OpenLarge(src, tinyGeom(0))
	if err != nil {
		_ = src.Close()
		return nil, err
	}
	t.Cleanup(func() { _ = b.Close() })
	return b, nil
}

func TestVirtualSourceOffsetsAbove4GiB(t *testing.T) {
	if testing.Short() {
		t.Skip("scale test")
	}
	const (
		width    = 64
		chunk    = 1 << 20
		perChunk = chunk / width
		size     = 8 << 30
		jumped   = 6000 // chunks, 6 GiB
	)
	src := buffertest.NewVirtual(size, width)
	b, err := buffer.OpenLarge(src, buffer.LargeOptions{Chunk: chunk, Budget: buffer.NewPageBudget(64 << 20)})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = b.Close() }()
	if got := b.Stats().ReadBytes; got > 5<<20 {
		t.Fatalf("ReadBytes after open = %d, want only the initial 4 MiB", got)
	}

	b.JumpScanned(jumped, perChunk)
	lastKnown := int64(jumped)*perChunk - 1
	for _, k := range []int64{lastKnown, lastKnown - 1, int64(5<<30) / width, 12345} {
		before := b.Stats().ReadBytes
		if got, want := b.Line(int(k)), buffertest.VirtualLine(k, width); got != want {
			t.Fatalf("Line(%d) = %q, want %q", k, got, want)
		}
		if d := b.Stats().ReadBytes - before; d > 3*chunk+width {
			t.Errorf("Line(%d) read %d bytes", k, d)
		}
	}
	if int64(b.Lines()) != int64(jumped)*perChunk {
		t.Errorf("Lines = %d, want %d", b.Lines(), int64(jumped)*perChunk)
	}
	info := b.Index(0)
	if info.Indexed <= 1<<32 || info.Size != size {
		t.Errorf("Index = %+v", info)
	}
	if want := int(size / width); info.ApproxLines != want {
		t.Errorf("ApproxLines = %d, want %d", info.ApproxLines, want)
	}
}

// A CRLF file's text joins lines with a single newline, so a span whose real
// text fits the cap must not be refused because its terminators were counted
// as two bytes each.
func TestTextCheckedCRLFAtCap(t *testing.T) {
	data := strings.Repeat("abcdefghi\r\n", 20)
	text := strings.ReplaceAll(data, "\r\n", "\n")
	for _, tc := range []struct {
		name    string
		cap     int64
		wantErr bool
	}{
		{"at cap", int64(len(text)), false},
		{"one under", int64(len(text)) - 1, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b, _ := openTiny(t, []byte(data), 0)
			indexAll(t, b)
			buffer.SetMaterializeCap(t, tc.cap)
			got, err := b.TextChecked()
			if tc.wantErr {
				if !errors.Is(err, buffer.ErrOverCap) || got != "" {
					t.Fatalf("TextChecked() = %q, %v, want ErrOverCap", got, err)
				}
				return
			}
			if err != nil || got != text {
				t.Fatalf("TextChecked() = %q, %v, want %q", got, err, text)
			}
		})
	}
}

// TestLargeLineSliceReadsOnlyThePrefix is the regression test for a frame that read
// every visible long line whole to show a window of columns: a slice of an uncached
// base line reads at most 4 bytes per rune up to the end of the slice, whether or
// not the line crosses a chunk, and it does not fill the page cache.
func TestLargeLineSliceReadsOnlyThePrefix(t *testing.T) {
	const lines, width = 40, 1000
	var sb strings.Builder
	for i := range lines {
		fmt.Fprintf(&sb, "%04d%s\n", i, strings.Repeat("é", width))
	}
	b, err := buffer.OpenLarge(buffertest.NewMemSource([]byte(sb.String())), buffer.LargeOptions{
		Chunk: 4096, CachePages: 2, Budget: buffer.NewPageBudget(1 << 20), InitialBytes: 1 << 30,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = b.Close() }()
	indexAll(t, b)
	const col, n = 10, 20
	// The first pass places the lines, which reads the pages that hold their ends.
	// A pass after it, as an idle frame is, reads the windows and nothing else.
	for pass := range 3 {
		before := b.Stats()
		for i := range lines {
			want := strings.Repeat("é", n)
			if got := b.LineSlice(i, col, n); got != want {
				t.Fatalf("pass %d: LineSlice(%d) = %q, want %q", pass, i, got, want)
			}
		}
		read := b.Stats().ReadBytes - before.ReadBytes
		if limit := int64(lines * 4 * (col + n)); pass > 0 && read > limit {
			t.Errorf("pass %d: LineSlice read %d bytes for %d lines, want at most %d", pass, read, lines, limit)
		}
	}
	if got := b.DecodedCount(); got != 0 {
		t.Errorf("LineSlice cached %d decoded lines", got)
	}
}
