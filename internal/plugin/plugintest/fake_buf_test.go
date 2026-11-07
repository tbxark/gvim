package plugintest_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/tbxark/gvim/internal/plugin/plugintest"
	"github.com/tbxark/gvim/internal/pluginapi"
)

func rg(l1, c1, l2, c2 int) pluginapi.Range {
	return pluginapi.Range{From: pluginapi.Pos{Line: l1, Col: c1}, To: pluginapi.Pos{Line: l2, Col: c2}}
}

func edit(r pluginapi.Range, text string) pluginapi.Edit { return pluginapi.Edit{Range: r, Text: text} }

func text(f *plugintest.FakeHost, id pluginapi.BufferID) string {
	s, _, err := f.FullText(id)
	if err != nil {
		panic(err)
	}
	return s
}

func TestFakeSliceRules(t *testing.T) {
	f := plugintest.NewFakeHost()
	id := f.AddBuffer("a.txt", "one\ntwo\nthree")
	rows := []struct {
		name string
		r    pluginapi.Range
		want string
	}{
		{"mid line", rg(0, 1, 0, 3), "ne"},
		{"column past the end clamps", rg(0, 0, 0, 99), "one"},
		{"next line start includes the newline", rg(0, 0, 1, 0), "one\n"},
		{"line count clamps to the end", rg(1, 0, 3, 0), "two\nthree"},
		{"far past the end", rg(2, 2, 100, 100), "ree"},
		{"from past the end", rg(5, 0, 6, 0), ""},
		{"reversed range is taken in order", rg(1, 0, 0, 0), "one\n"},
		{"negative positions clamp to the start", rg(-3, -3, 0, 2), "on"},
	}
	for _, r := range rows {
		t.Run(r.name, func(t *testing.T) {
			got, err := f.Slice(id, r.r)
			if err != nil || got != r.want {
				t.Fatalf("Slice = %q, %v, want %q", got, err, r.want)
			}
		})
	}
	if _, err := f.Slice(99, rg(0, 0, 0, 1)); pluginapi.CodeOf(err) != pluginapi.CodeClosed {
		t.Fatalf("closed buffer: %v", err)
	}
}

func TestFakeMultibyteColumns(t *testing.T) {
	f := plugintest.NewFakeHost()
	id := f.AddBuffer("a.txt", "héllo")
	off, _ := f.OffsetOf(id, pluginapi.Pos{Col: 2}) // inside "é": rounds down to its start
	if off != 1 {
		t.Fatalf("offset of an inner byte = %d, want 1", off)
	}
	if p, _ := f.PosOf(id, 3); p != (pluginapi.Pos{Col: 3}) {
		t.Fatalf("PosOf(3) = %+v", p)
	}
	if n, _ := f.ByteLength(id); n != 6 {
		t.Fatalf("ByteLength = %d", n)
	}
}

func TestFakeEditAndUndo(t *testing.T) {
	f := plugintest.NewFakeHost()
	id := f.AddBuffer("a.txt", "abc\ndef")
	v0 := f.Buffers[0].Info.Version

	// unsorted, pre-edit coordinates
	err := f.Edit(id, []pluginapi.Edit{edit(rg(1, 0, 1, 1), "D"), edit(rg(0, 2, 0, 3), "C"), edit(rg(0, 0, 0, 0), ">")}, pluginapi.EditOptions{})
	if err != nil || text(f, id) != ">abC\nDef" {
		t.Fatalf("Edit: %v, text %q", err, text(f, id))
	}
	if v1 := f.Buffers[0].Info.Version; v1 == v0 || v1.Gen != v0.Gen {
		t.Fatalf("version after edit = %+v, before %+v", v1, v0)
	}
	if !f.Buffers[0].Info.Modified || f.Buffers[0].Info.LineCount != 2 {
		t.Fatalf("info after edit = %+v", f.Buffers[0].Info)
	}

	// overlap changes nothing
	err = f.Edit(id, []pluginapi.Edit{edit(rg(0, 0, 0, 3), "x"), edit(rg(0, 2, 0, 4), "y")}, pluginapi.EditOptions{})
	if pluginapi.CodeOf(err) != pluginapi.CodeInvalid || text(f, id) != ">abC\nDef" {
		t.Fatalf("overlap: %v, text %q", err, text(f, id))
	}

	// stale
	stale := v0
	err = f.Edit(id, []pluginapi.Edit{edit(rg(0, 0, 0, 0), "z")}, pluginapi.EditOptions{Expect: &stale})
	if pluginapi.CodeOf(err) != pluginapi.CodeStale || text(f, id) != ">abC\nDef" {
		t.Fatalf("stale: %v, text %q", err, text(f, id))
	}

	// one undo step, equal token
	if err := f.Undo(id); err != nil {
		t.Fatal(err)
	}
	if text(f, id) != "abc\ndef" || f.Buffers[0].Info.Version != v0 || f.Buffers[0].Info.Modified {
		t.Fatalf("after undo: %q %+v", text(f, id), f.Buffers[0].Info)
	}
	if err := f.Redo(id); err != nil || text(f, id) != ">abC\nDef" {
		t.Fatalf("redo: %v %q", err, text(f, id))
	}
	if err := f.Undo(id); err != nil {
		t.Fatal(err)
	}
	if pluginapi.CodeOf(f.Undo(id)) != pluginapi.CodeNotFound {
		t.Fatal("undo past the start must fail")
	}
}

func TestFakeUndoGroup(t *testing.T) {
	f := plugintest.NewFakeHost()
	id := f.AddBuffer("a.txt", "x")
	ins := func(s string) { _ = f.Edit(id, []pluginapi.Edit{edit(rg(0, 0, 0, 0), s)}, pluginapi.EditOptions{}) }
	_ = f.UndoGroup(id, func() error {
		ins("1")
		_ = f.UndoGroup(id, func() error { ins("2"); return nil })
		ins("3")
		return nil
	})
	ins("4")
	if text(f, id) != "4321x" {
		t.Fatalf("text = %q", text(f, id))
	}
	_ = f.Undo(id)
	if text(f, id) != "321x" {
		t.Fatalf("after one undo: %q", text(f, id))
	}
	_ = f.Undo(id)
	if text(f, id) != "x" {
		t.Fatalf("after the group undo: %q", text(f, id))
	}
}

func TestFakeCursorMapping(t *testing.T) {
	rows := []struct {
		name string
		mode pluginapi.CursorMode
		want pluginapi.Pos
	}{
		{"map", pluginapi.CursorMap, pluginapi.Pos{Col: 5}},
		{"keep", pluginapi.CursorKeep, pluginapi.Pos{Col: 3}},
		{"end", pluginapi.CursorEnd, pluginapi.Pos{Col: 8}},
	}
	for _, r := range rows {
		t.Run(r.name, func(t *testing.T) {
			f := plugintest.NewFakeHost()
			id := f.AddBuffer("a.txt", "abcdef")
			_ = f.SetCursor(id, pluginapi.Pos{Col: 3}, false, true, false)
			err := f.Edit(id, []pluginapi.Edit{edit(rg(0, 0, 0, 0), "12"), edit(rg(0, 5, 0, 5), "Z")}, pluginapi.EditOptions{Cursor: r.mode})
			cs, _ := f.Cursors(id)
			if err != nil || len(cs) != 1 || cs[0].Pos != r.want {
				t.Fatalf("cursors = %+v, %v, want %+v", cs, err, r.want)
			}
		})
	}
}

func TestFakeSearch(t *testing.T) {
	f := plugintest.NewFakeHost()
	id := f.AddBuffer("a.txt", "foo Foo\nbar foo\nfoobar")
	rows := []struct {
		name      string
		q         pluginapi.SearchQuery
		n         int
		truncated bool
	}{
		{"case insensitive literal", pluginapi.SearchQuery{Text: "foo"}, 4, false},
		{"case sensitive", pluginapi.SearchQuery{Text: "foo", CaseSensitive: true}, 3, false},
		{"whole word", pluginapi.SearchQuery{Text: "foo", WholeWord: true}, 3, false},
		{"max reached with more left", pluginapi.SearchQuery{Text: "foo", Max: 2}, 2, true},
		{"max equal to the count", pluginapi.SearchQuery{Text: "foo", Max: 4}, 4, false},
		{"within", pluginapi.SearchQuery{Text: "foo", Within: &pluginapi.Range{From: pluginapi.Pos{Line: 1}, To: pluginapi.Pos{Line: 2}}}, 1, false},
		{"regex", pluginapi.SearchQuery{Text: "o+", Regex: true}, 4, false},
		{"literal metacharacters", pluginapi.SearchQuery{Text: "o+"}, 0, false},
	}
	for _, r := range rows {
		t.Run(r.name, func(t *testing.T) {
			res, err := f.Search(id, r.q)
			if err != nil || len(res.Ranges) != r.n || res.Truncated != r.truncated {
				t.Fatalf("Search = %d ranges, truncated %v, %v; want %d, %v", len(res.Ranges), res.Truncated, err, r.n, r.truncated)
			}
		})
	}
	if _, err := f.Search(id, pluginapi.SearchQuery{Text: "(", Regex: true}); pluginapi.CodeOf(err) != pluginapi.CodeInvalid {
		t.Fatalf("bad regex: %v", err)
	}
}

func TestFakeFindLinesChunks(t *testing.T) {
	f := plugintest.NewFakeHost()
	id := f.AddBuffer("a.txt", strings.Repeat("a\nb\n", 5)+"a")
	f.SetFindLinesChunk(4)
	var all []int
	from, calls := 0, 0
	for from >= 0 {
		res, err := f.FindLines(id, pluginapi.FindLinesQuery{Pattern: "a", From: from})
		if err != nil {
			t.Fatal(err)
		}
		all = append(all, res.Lines...)
		from = res.Next
		calls++
	}
	if !slices.Equal(all, []int{0, 2, 4, 6, 8, 10}) || calls != 3 {
		t.Fatalf("lines %v in %d calls", all, calls)
	}
}

func TestFakeAsyncBufferCalls(t *testing.T) {
	f := plugintest.NewFakeHost()
	id := f.AddBuffer("/w/a.go", "old")
	f.SetFile("/w/b.go", "b text")
	_ = f.Open(1, pluginapi.OpenRequest{Path: "/w/b.go", Focus: true})
	_ = f.Open(2, pluginapi.OpenRequest{Path: "/w/zzz.go"})
	_ = f.Edit(id, []pluginapi.Edit{edit(rg(0, 0, 0, 0), "new ")}, pluginapi.EditOptions{})
	_ = f.Save(3, id, "")
	_ = f.Revert(4, id)
	if f.PendingBufferCalls() != 4 {
		t.Fatalf("pending = %d", f.PendingBufferCalls())
	}
	h := plugintest.New(t, nil, plugintest.Options{Host: f})
	h.Start()
	h.CompleteBuffers() // no plugin waits on these tokens: the runtime ignores the completions
	if f.PendingBufferCalls() != 0 || len(f.Buffers) != 2 || f.ActiveBuffer() != f.Buffers[1].Info.ID {
		t.Fatalf("after completing: pending %d, buffers %d, active %d", f.PendingBufferCalls(), len(f.Buffers), f.ActiveBuffer())
	}
	if f.Buffers[0].Info.Version.Gen != 2 || strings.Join(f.Buffers[0].Lines, "\n") != "new old" {
		t.Fatalf("revert after save must restore the saved text and bump the generation: %+v %q", f.Buffers[0].Info.Version, f.Buffers[0].Lines)
	}
}
