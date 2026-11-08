package editor

import (
	"errors"
	"fmt"
	"math/rand/v2"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/tbxark/gvim/internal/buffer"
)

func pos(l, c int) buffer.Pos { return buffer.Pos{Line: l, Col: c} }

func lspEdit(fl, fc, tl, tc int, text string) LSPEdit {
	return LSPEdit{From: pos(fl, fc), To: pos(tl, tc), Text: text}
}

// tabsWith returns an editor with one untitled tab per text, the first active.
func tabsWith(texts ...string) (*Editor, []DocID) {
	e := New(Options{})
	e.docs = nil
	for _, s := range texts {
		e.addDoc(newDocument("", s))
	}
	e.switchTo(0)
	return e, e.DocIDs()
}

func TestDocIDsAreStableAndMonotonic(t *testing.T) {
	t.Parallel()
	e, ids := tabsWith("a", "b", "c")
	if !slices.IsSorted(ids) || ids[0] == 0 || ids[0] == ids[1] || ids[1] == ids[2] {
		t.Fatalf("ids = %v, want distinct and increasing", ids)
	}
	if got := e.ActiveID(); got != ids[0] {
		t.Fatalf("ActiveID = %d, want %d", got, ids[0])
	}
	// A moved tab, a closed neighbour and an activation change no id.
	if err := e.MoveTab(ids[0], 2); err != nil {
		t.Fatal(err)
	}
	if err := e.CloseID(ids[1], false); err != nil {
		t.Fatal(err)
	}
	if err := e.ActivateID(ids[2]); err != nil {
		t.Fatal(err)
	}
	want := []DocID{ids[2], ids[0]}
	if got := e.DocIDs(); !slices.Equal(got, want) {
		t.Fatalf("DocIDs = %v, want %v", got, want)
	}
	d, i, ok := e.DocByID(ids[0])
	if !ok || i != 1 || d.ID() != ids[0] || d.Buffer.Text() != "a" {
		t.Fatalf("DocByID(%d) = %v, %d, %v", ids[0], d, i, ok)
	}
	// A new tab gets an id above every earlier one, also above a closed tab's.
	e.addDoc(newDocument("", "n"))
	if got := e.DocIDs(); got[2] <= ids[2] {
		t.Fatalf("new id %d is not above %d", got[2], ids[2])
	}
	if _, _, ok := e.DocByID(ids[1]); ok {
		t.Error("the id of a closed document still resolves")
	}
	if _, _, ok := e.DocByID(0); ok {
		t.Error("id 0 resolves")
	}
}

func TestDocIDsThroughTheOpeningPaths(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	write := func(name string) string {
		p := dir + "/" + name
		if err := os.WriteFile(p, []byte(name), 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	e := New(Options{})
	// The placeholder is replaced by the first file; its id is not reused.
	first := e.ActiveID()
	if first == 0 {
		t.Fatal("the placeholder has no id")
	}
	if err := e.OpenFile(write("a.txt")); err != nil {
		t.Fatal(err)
	}
	a := e.ActiveID()
	if a == 0 || a == first {
		t.Fatalf("id after replacing the placeholder = %d (placeholder %d)", a, first)
	}
	if err := e.OpenFile(write("b.txt")); err != nil {
		t.Fatal(err)
	}
	if res := e.Do("new", nil); res.Err != nil {
		t.Fatal(res.Err)
	}
	ids := e.DocIDs()
	if len(ids) != 3 || ids[0] != a || !slices.IsSorted(ids) {
		t.Fatalf("ids = %v, want 3 increasing starting at %d", ids, a)
	}
	// A preview replaces the previous preview: a new document, a new id.
	if err := e.OpenPreview(write("c.txt")); err != nil {
		t.Fatal(err)
	}
	p1 := e.ActiveID()
	if err := e.OpenPreview(write("d.txt")); err != nil {
		t.Fatal(err)
	}
	if p2 := e.ActiveID(); p2 == p1 || p2 == 0 {
		t.Fatalf("preview ids %d then %d", p1, p2)
	}
	if len(e.DocIDs()) != 4 {
		t.Fatalf("tabs = %d, want 4", len(e.DocIDs()))
	}
}

func TestMoveTab(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		move   int // index of the tab to move
		delta  int
		active int // index of the active tab before
		want   string
		wantAt int // index of the active tab after
	}{
		{"right by one", 0, 1, 0, "bacd", 1},
		{"left by one", 2, -1, 0, "acbd", 0},
		{"to the end", 0, 9, 0, "bcda", 3},
		{"to the start", 3, -9, 3, "dabc", 0},
		{"past the start stops there", 0, -1, 0, "abcd", 0},
		{"zero", 2, 0, 2, "abcd", 2},
		{"moves the tab before the active one over it", 3, -3, 1, "dabc", 2},
		{"moves a tab from before the active one to after it", 0, 2, 1, "bcad", 0},
		{"the active tab moves", 1, 2, 1, "acdb", 3},
	}
	for _, tt := range tests {
		e, ids := tabsWith("a", "b", "c", "d")
		e.switchTo(tt.active)
		activeDoc := e.Active()
		if err := e.MoveTab(ids[tt.move], tt.delta); err != nil {
			t.Fatalf("%s: %v", tt.name, err)
		}
		var got strings.Builder
		for _, d := range e.docs {
			got.WriteString(d.Buffer.Text())
		}
		if got.String() != tt.want || e.active != tt.wantAt || e.Active() != activeDoc {
			t.Errorf("%s: order %q active %d (doc kept: %v), want %q active %d", tt.name, got.String(), e.active, e.Active() == activeDoc, tt.want, tt.wantAt)
		}
		if !slices.Equal(slices.Sorted(slices.Values(e.DocIDs())), ids) {
			t.Errorf("%s: the set of ids changed", tt.name)
		}
	}
	e, _ := tabsWith("a")
	if err := e.MoveTab(999, 1); !errors.Is(err, ErrNotOpen) {
		t.Errorf("MoveTab of an unknown id: %v, want ErrNotOpen", err)
	}
}

func TestDocumentGenBumpedBySetBuffer(t *testing.T) {
	t.Parallel()
	d := newDocument("", "abc")
	g0 := d.Gen()
	if g0 == 0 || d.Gen() != g0 {
		t.Fatalf("Gen = %d then %d, want a stable non-zero number", g0, d.Gen())
	}
	v0 := d.Version()
	if v0.IsZero() || v0.Gen != g0 || v0.Ver != d.Buffer.Version() {
		t.Fatalf("Version = %+v", v0)
	}
	d.setBuffer(buffer.New("abc"))
	if d.Gen() <= g0 {
		t.Errorf("Gen after setBuffer = %d, want above %d", d.Gen(), g0)
	}
	// The same text in a new buffer gives a token that never equals the old one.
	if d.Version() == v0 {
		t.Error("a replaced buffer gave the same Version")
	}
	if other := newDocument("", ""); other.Gen() == d.Gen() {
		t.Error("two documents share a Gen")
	}
}

func TestRevertChangesGen(t *testing.T) {
	t.Parallel()
	t.Run("small file edits in place", func(t *testing.T) {
		e, path := openFixture(t, Options{}, "one\ntwo\n")
		d := e.Active()
		token := d.Version()
		d.Buffer.Insert(pos(0, 0), "x")
		if err := os.WriteFile(path, []byte("changed\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := e.Revert(true); err != nil {
			t.Fatal(err)
		}
		if d.Buffer.Text() != "changed\n" || d.Version() == token {
			t.Errorf("text %q, token %+v after %+v", d.Buffer.Text(), d.Version(), token)
		}
	})
	t.Run("large file swaps the buffer", func(t *testing.T) {
		e, d := openLarge(t, largeOptions(), lineText(200))
		token := d.Version()
		replaceFile(t, d.Path, "new first line\n"+lineText(250))
		if err := e.Revert(false); err != nil {
			t.Fatal(err)
		}
		if d.Buffer.Line(0) != "new first line" {
			t.Fatalf("line 0 = %q", d.Buffer.Line(0))
		}
		if d.Version() == token || d.Gen() == token.Gen {
			t.Errorf("revert kept the token %+v (now %+v)", token, d.Version())
		}
	})
	t.Run("reload with an encoding", func(t *testing.T) {
		e, _ := openFixture(t, Options{}, "caf\xe9\n")
		d := e.Active()
		token := d.Version()
		if res := e.Do("reload_with_encoding", map[string]any{"name": "windows-1252"}); res.Err != nil {
			t.Fatal(res.Err)
		}
		if d.Gen() == token.Gen || d.Version() == token {
			t.Errorf("reload kept the token %+v (now %+v)", token, d.Version())
		}
	})
}

func TestSetBufferEmitsCoarseAndReinstallsObserver(t *testing.T) {
	t.Parallel()
	e, ids := tabsWith("abc")
	var got []buffer.Change
	e.AddChangeListener("t", func(id DocID, c buffer.Change) {
		if id != ids[0] {
			t.Errorf("listener got id %d, want %d", id, ids[0])
		}
		got = append(got, c)
	})
	e.TrackChanges("events", true)
	d := e.Active()
	old := d.Buffer
	nb := buffer.New("fresh")
	d.setBuffer(nb)
	if len(got) != 1 || !got[0].Coarse || got[0].Version != nb.Version() {
		t.Fatalf("changes after setBuffer = %+v, want one coarse change", got)
	}
	got = nil
	nb.Insert(pos(0, 0), "X")
	if len(got) != 1 || got[0].Text != "X" {
		t.Fatalf("an edit of the new buffer reported %+v", got)
	}
	got = nil
	old.Insert(pos(0, 0), "Y")
	if len(got) != 0 {
		t.Errorf("the old buffer still reports: %+v", got)
	}
	if changes, overflow := e.TakeChanges(ids[0], "events"); !overflow || changes != nil {
		t.Errorf("TakeChanges after a coarse change = %v, %v, want overflow", changes, overflow)
	}
	// Without tracking, a replacement reports nothing.
	e.TrackChanges("events", false)
	got = nil
	d.setBuffer(buffer.New("again"))
	if len(got) != 0 {
		t.Errorf("untracked setBuffer reported %+v", got)
	}
}

func TestTrackChangesRefCounted(t *testing.T) {
	t.Parallel()
	e, ids := tabsWith("a", "b")
	var n int
	e.AddChangeListener("count", func(DocID, buffer.Change) { n++ })
	insert := func() { e.Active().Buffer.Insert(pos(0, 0), "x") }

	insert()
	if n != 0 {
		t.Fatal("changes were reported before any tracking")
	}
	e.TrackChanges("events", true)
	e.TrackChanges("events", true)
	e.TrackChanges("decor", true)
	insert()
	if n != 1 {
		t.Fatalf("n = %d after tracking, want 1", n)
	}
	// A document added while tracking is observed at once.
	e.addDoc(newDocument("", "late"))
	e.docs[2].Buffer.Insert(pos(0, 0), "y")
	if n != 2 {
		t.Fatalf("n = %d after an edit of a new document, want 2", n)
	}
	e.TrackChanges("events", false)
	insert()
	if n != 3 {
		t.Fatalf("n = %d with events still on once and decor on, want 3", n)
	}
	e.TrackChanges("events", false)
	e.TrackChanges("decor", false)
	insert()
	e.docs[2].Buffer.Insert(pos(0, 0), "z")
	if n != 3 {
		t.Fatalf("n = %d after everything went off, want 3", n)
	}
	// Extra off calls change nothing, and tracking can start again.
	e.TrackChanges("events", false)
	e.TrackChanges("decor", true)
	insert()
	if n != 4 {
		t.Fatalf("n = %d after tracking again, want 4", n)
	}
	// Only while tracked does a ring exist.
	e.TrackChanges("decor", false)
	if changes, over := e.TakeChanges(ids[0], "decor"); changes != nil || over {
		t.Errorf("TakeChanges after tracking ended = %v, %v", changes, over)
	}
}

func TestListenersRunSynchronouslyInOrder(t *testing.T) {
	t.Parallel()
	e, ids := tabsWith("abc")
	e.TrackChanges("events", true)
	var log []string
	e.AddChangeListener("decor", func(id DocID, c buffer.Change) { log = append(log, "decor:"+c.Text) })
	e.AddChangeListener("events", func(id DocID, c buffer.Change) { log = append(log, "events:"+c.Text) })
	rm := e.AddChangeListener("third", func(id DocID, c buffer.Change) { log = append(log, "third:"+c.Text) })

	e.Active().Buffer.Insert(pos(0, 1), "X")
	log = append(log, "returned")
	want := []string{"decor:X", "events:X", "third:X", "returned"}
	if !slices.Equal(log, want) {
		t.Fatalf("log = %v, want %v", log, want)
	}

	// A key registered again replaces its function and keeps its place.
	log = nil
	e.AddChangeListener("decor", func(id DocID, c buffer.Change) { log = append(log, "decor2:"+c.Text) })
	rm()
	rm() // removing twice is harmless
	e.Active().Buffer.Insert(pos(0, 0), "Y")
	if want := []string{"decor2:Y", "events:Y"}; !slices.Equal(log, want) {
		t.Fatalf("log = %v, want %v", log, want)
	}

	// A listener sees the change before the edit call returns, with the buffer
	// already changed, and may remove itself while it runs.
	var self func()
	var seen string
	self = e.AddChangeListener("once", func(id DocID, c buffer.Change) {
		seen = e.Active().Buffer.Text()
		self()
	})
	e.Active().Buffer.Insert(pos(0, 0), "Z")
	if seen != "ZYaXbc" {
		t.Fatalf("listener saw %q, want the edited text", seen)
	}
	log = nil
	e.Active().Buffer.Insert(pos(0, 0), "W")
	if len(log) != 2 {
		t.Fatalf("log = %v, want two listeners left", log)
	}
	_ = ids
}

func TestRemoveOfAReplacedListenerKeepsTheNewOne(t *testing.T) {
	t.Parallel()
	e, _ := tabsWith("abc")
	e.TrackChanges("events", true)
	var log []string
	rm1 := e.AddChangeListener("k", func(DocID, buffer.Change) { log = append(log, "old") })
	rm2 := e.AddChangeListener("k", func(DocID, buffer.Change) { log = append(log, "new") })
	rm1() // the teardown of the plugin instance that was replaced
	e.Active().Buffer.Insert(pos(0, 0), "X")
	if want := []string{"new"}; !slices.Equal(log, want) {
		t.Fatalf("after the old remove: log = %v, want %v", log, want)
	}
	rm2()
	log = nil
	e.Active().Buffer.Insert(pos(0, 0), "Y")
	if len(log) != 0 {
		t.Errorf("after its own remove the listener still ran: %v", log)
	}
	// A key removed and added again is a new registration too.
	rm3 := e.AddChangeListener("k", func(DocID, buffer.Change) { log = append(log, "third") })
	rm2()
	e.Active().Buffer.Insert(pos(0, 0), "Z")
	if want := []string{"third"}; !slices.Equal(log, want) {
		t.Fatalf("a stale remove deleted a later registration: log = %v", log)
	}
	rm3()
}

func TestApplyEditsReportsInvalidUTF8AsStored(t *testing.T) {
	t.Parallel()
	e, ids := tabsWith("ab\n")
	e.TrackChanges("events", true)
	if err := e.ApplyEdits(ids[0], []LSPEdit{lspEdit(0, 1, 0, 1, "\xff")}, EditOptions{}); err != nil {
		t.Fatal(err)
	}
	changes, overflow := e.TakeChanges(ids[0], "events")
	if overflow || len(changes) != 1 {
		t.Fatalf("changes = %+v overflow %v, want one", changes, overflow)
	}
	if got, want := changes[0].Text, "\uFFFD"; got != want {
		t.Errorf("Text = %q, want %q", got, want)
	}
	b := e.Active().Buffer
	if got := b.ByteCol(pos(0, 3)); got != 5 {
		t.Errorf("ByteCol after the edit = %d, want 5 (line %q)", got, b.Line(0))
	}
}

func TestTakeChangesPerConsumerCursor(t *testing.T) {
	t.Parallel()
	e, ids := tabsWith("abc")
	e.TrackChanges("events", true)
	e.TrackChanges("decor", true)
	b := e.Active().Buffer
	texts := func(cs []buffer.Change) string {
		var sb strings.Builder
		for _, c := range cs {
			sb.WriteString(c.Text)
			sb.WriteString("/")
		}
		return sb.String()
	}
	b.Insert(pos(0, 0), "1")
	b.Insert(pos(0, 0), "2")
	got, over := e.TakeChanges(ids[0], "events")
	if over || texts(got) != "1/2/" {
		t.Fatalf("events first take = %q overflow %v", texts(got), over)
	}
	b.Insert(pos(0, 0), "3")
	if got, _ = e.TakeChanges(ids[0], "events"); texts(got) != "3/" {
		t.Fatalf("events second take = %q", texts(got))
	}
	// decor has read nothing: it gets everything, and events is not affected.
	if got, over = e.TakeChanges(ids[0], "decor"); over || texts(got) != "1/2/3/" {
		t.Fatalf("decor take = %q overflow %v", texts(got), over)
	}
	if got, over = e.TakeChanges(ids[0], "decor"); len(got) != 0 || over {
		t.Fatalf("decor second take = %v, %v, want nothing", got, over)
	}
	b.Insert(pos(0, 0), "4")
	if got, _ = e.TakeChanges(ids[0], "events"); texts(got) != "4/" {
		t.Fatalf("events third take = %q", texts(got))
	}
	if got, _ = e.TakeChanges(ids[0], "decor"); texts(got) != "4/" {
		t.Fatalf("decor third take = %q", texts(got))
	}
	// The result is a copy.
	b.Insert(pos(0, 0), "5")
	got, _ = e.TakeChanges(ids[0], "events")
	got[0].Text = "mutated"
	if again, _ := e.TakeChanges(ids[0], "decor"); texts(again) != "5/" {
		t.Fatalf("a consumer's slice aliases the ring: %q", texts(again))
	}
	if got, over = e.TakeChanges(12345, "events"); got != nil || over {
		t.Errorf("unknown id: %v, %v", got, over)
	}
	// A consumer that starts later starts at the present.
	e.TrackChanges("late", true)
	if got, over = e.TakeChanges(ids[0], "late"); len(got) != 0 || over {
		t.Errorf("a consumer that was switched on late got %v, %v", got, over)
	}
}

func TestTakeChangesOverflow(t *testing.T) {
	t.Parallel()
	t.Run("ring wraps", func(t *testing.T) {
		e, ids := tabsWith("")
		e.TrackChanges("events", true)
		b := e.Active().Buffer
		for range changeRingRecords {
			b.Insert(pos(0, 0), "x")
		}
		if got, over := e.TakeChanges(ids[0], "events"); over || len(got) != changeRingRecords {
			t.Fatalf("a full ring: %d changes overflow %v", len(got), over)
		}
		for range changeRingRecords + 1 {
			b.Insert(pos(0, 0), "x")
		}
		if got, over := e.TakeChanges(ids[0], "events"); !over || got != nil {
			t.Fatalf("a wrapped ring: %d changes overflow %v", len(got), over)
		}
		b.Insert(pos(0, 0), "y")
		if got, over := e.TakeChanges(ids[0], "events"); over || len(got) != 1 {
			t.Fatalf("after the overflow was reported: %d changes overflow %v", len(got), over)
		}
	})
	t.Run("coarse change", func(t *testing.T) {
		e, ids := tabsWith("a")
		e.TrackChanges("events", true)
		d := e.Active()
		d.Buffer.Insert(pos(0, 0), "x")
		d.setBuffer(buffer.New("new"))
		d.Buffer.Insert(pos(0, 0), "y")
		if got, over := e.TakeChanges(ids[0], "events"); !over || got != nil {
			t.Fatalf("coarse: %v overflow %v", got, over)
		}
		d.Buffer.Insert(pos(0, 0), "z")
		if got, over := e.TakeChanges(ids[0], "events"); over || len(got) != 1 {
			t.Fatalf("after: %v overflow %v", got, over)
		}
	})
	t.Run("large buffer edits are coarse", func(t *testing.T) {
		e, d := openLarge(t, largeOptions(), lineText(200))
		indexAll(t, e)
		e.TrackChanges("events", true)
		d.Buffer.Insert(pos(3, 0), "x")
		if got, over := e.TakeChanges(d.ID(), "events"); !over || got != nil {
			t.Fatalf("large edit: %v overflow %v", got, over)
		}
	})
}

func TestChangesPayloadCap(t *testing.T) {
	t.Parallel()
	piece := strings.Repeat("p", 100<<10)
	run := func(n int) (int, bool) {
		e, ids := tabsWith("")
		e.TrackChanges("events", true)
		for range n {
			e.Active().Buffer.Insert(pos(0, 0), piece)
		}
		got, over := e.TakeChanges(ids[0], "events")
		return len(got), over
	}
	if n, over := run(2); n != 2 || over {
		t.Errorf("200 KiB of changes: %d records overflow %v, want both", n, over)
	}
	if n, over := run(3); n != 0 || !over {
		t.Errorf("300 KiB of changes: %d records overflow %v, want overflow", n, over)
	}
	// One record over the cap is kept without its text, as a coarse change.
	e, ids := tabsWith("")
	e.TrackChanges("events", true)
	e.Active().Buffer.Insert(pos(0, 0), strings.Repeat("q", changePayloadMax+1))
	if got, over := e.TakeChanges(ids[0], "events"); !over || got != nil {
		t.Errorf("one huge change: %d records overflow %v", len(got), over)
	}
	// The ring also bounds the bytes it keeps: old records go first.
	e, ids = tabsWith("")
	e.TrackChanges("events", true)
	for range 20 {
		e.Active().Buffer.Insert(pos(0, 0), strings.Repeat("r", 200<<10))
	}
	ring := e.Active().seam.ring
	if ring.bytes > changeRingBytes || len(ring.recs)-ring.start < 1 {
		t.Errorf("ring keeps %d bytes in %d records", ring.bytes, len(ring.recs)-ring.start)
	}
	if got, over := e.TakeChanges(ids[0], "events"); !over || got != nil {
		t.Errorf("a consumer that fell behind got %d records, overflow %v", len(got), over)
	}
}

func TestApplyEditsTable(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		text  string
		edits []LSPEdit
		want  string
	}{
		{"replace", "hello world", []LSPEdit{lspEdit(0, 6, 0, 11, "gvim")}, "hello gvim"},
		{"unsorted edits", "a b c", []LSPEdit{lspEdit(0, 4, 0, 5, "z"), lspEdit(0, 0, 0, 1, "x")}, "x b z"},
		{"inserts at one point keep list order", "ab", []LSPEdit{lspEdit(0, 1, 0, 1, "1"), lspEdit(0, 1, 0, 1, "2"), lspEdit(0, 1, 0, 1, "3")}, "a123b"},
		{"insert before a replace at the same point", "abc", []LSPEdit{lspEdit(0, 1, 0, 2, "X"), lspEdit(0, 1, 0, 1, "I")}, "aIXc"},
		{"touching edits are not overlapping", "abcd", []LSPEdit{lspEdit(0, 0, 0, 2, "X"), lspEdit(0, 2, 0, 4, "Y")}, "XY"},
		{"across lines", "one\ntwo\nthree", []LSPEdit{lspEdit(0, 1, 2, 2, "-")}, "o-ree"},
		{"multibyte columns are runes", "é日x", []LSPEdit{lspEdit(0, 1, 0, 2, "o")}, "éox"},
		{"clamped", "abc\n", []LSPEdit{lspEdit(0, 1, 9, 0, "!")}, "a!"},
		{"reversed range", "abcd", []LSPEdit{lspEdit(0, 3, 0, 1, "X")}, "aXd"},
		{"newline inserted", "ab", []LSPEdit{lspEdit(0, 1, 0, 1, "\n")}, "a\nb"},
		{"delete", "abc", []LSPEdit{lspEdit(0, 1, 0, 2, "")}, "ac"},
		{"nothing", "abc", nil, "abc"},
	}
	for _, tt := range tests {
		e, ids := tabsWith("zzz", tt.text)
		if err := e.ApplyEdits(ids[1], tt.edits, EditOptions{}); err != nil {
			t.Errorf("%s: %v", tt.name, err)
			continue
		}
		// The second tab is not active, and the active one is untouched.
		if got := e.docs[1].Buffer.Text(); got != tt.want {
			t.Errorf("%s: text = %q, want %q", tt.name, got, tt.want)
		}
		if e.docs[0].Buffer.Text() != "zzz" || e.ActiveID() != ids[0] {
			t.Errorf("%s: the active tab changed", tt.name)
		}
	}
}

func TestApplyEditsErrors(t *testing.T) {
	t.Parallel()
	e, ids := tabsWith("hello world")
	d := e.Active()
	good := []LSPEdit{lspEdit(0, 0, 0, 1, "H")}
	v := d.Version()
	stale := &v
	d.Buffer.Insert(pos(0, 0), "x")
	d.Buffer.Delete(pos(0, 0), pos(0, 1))
	// The text is the same again, but the token is not equal to the old one:
	// the buffer version moved on.
	if *stale == d.Version() {
		t.Fatal("test setup: the version did not change")
	}
	cases := []struct {
		name  string
		id    DocID
		edits []LSPEdit
		opts  EditOptions
		want  error
	}{
		{"unknown id", 999, good, EditOptions{}, ErrNotOpen},
		{"overlap", ids[0], []LSPEdit{lspEdit(0, 0, 0, 5, "a"), lspEdit(0, 3, 0, 7, "b")}, EditOptions{}, ErrOverlappingEdits},
		{"insert inside a replace", ids[0], []LSPEdit{lspEdit(0, 0, 0, 5, "a"), lspEdit(0, 3, 0, 3, "b")}, EditOptions{}, ErrOverlappingEdits},
		{"stale", ids[0], good, EditOptions{Expect: stale}, ErrStale},
	}
	for _, c := range cases {
		before := d.Buffer.Text()
		if err := e.ApplyEdits(c.id, c.edits, c.opts); !errors.Is(err, c.want) {
			t.Errorf("%s: err = %v, want %v", c.name, err, c.want)
		}
		if d.Buffer.Text() != before {
			t.Errorf("%s: the text changed", c.name)
		}
	}
	// A current token passes, also after Undo returns an earlier state.
	cur := d.Version()
	if err := e.ApplyEdits(ids[0], good, EditOptions{Expect: &cur}); err != nil {
		t.Fatalf("current token: %v", err)
	}
	if d.Buffer.Text() != "Hello world" {
		t.Fatalf("text = %q", d.Buffer.Text())
	}
	d.Buffer.Undo()
	if d.Version() != cur {
		t.Fatalf("undo gave %+v, want the equal token %+v", d.Version(), cur)
	}
	if err := e.ApplyEdits(ids[0], good, EditOptions{Expect: &cur}); err != nil {
		t.Fatalf("token after undo: %v", err)
	}
	// Read-only.
	d.readOnly = true
	if err := e.ApplyEdits(ids[0], good, EditOptions{}); !errors.Is(err, ErrReadOnly) {
		t.Errorf("read-only: %v", err)
	}
}

func TestUndoRestoresEqualToken(t *testing.T) {
	t.Parallel()
	e, ids := tabsWith("abc")
	d := e.Active()
	orig := d.Version()
	d.Buffer.Insert(pos(0, 0), "x")
	if d.Version() == orig {
		t.Fatal("an edit kept the token")
	}
	if _, ok := d.Buffer.Undo(); !ok || d.Version() != orig {
		t.Fatalf("token after undo = %+v, want %+v", d.Version(), orig)
	}
	if err := e.ApplyEdits(ids[0], []LSPEdit{lspEdit(0, 0, 0, 1, "A")}, EditOptions{Expect: &orig}); err != nil {
		t.Fatalf("the original token was refused after undo: %v", err)
	}
}

func TestStaleOnUneditedBuffer(t *testing.T) {
	t.Parallel()
	// A fresh buffer has Version() 0; the token must still detect an edit.
	e, ids := tabsWith("abc")
	d := e.Active()
	tok := d.Version()
	if tok.Ver != 0 || tok.IsZero() {
		t.Fatalf("token of a fresh buffer = %+v, want Ver 0 and a Gen", tok)
	}
	d.Buffer.Insert(pos(0, 0), "x")
	if err := e.ApplyEdits(ids[0], []LSPEdit{lspEdit(0, 0, 0, 1, "")}, EditOptions{Expect: &tok}); !errors.Is(err, ErrStale) {
		t.Fatalf("err = %v, want ErrStale", err)
	}
}

func TestApplyEditsIsOneUndoStep(t *testing.T) {
	t.Parallel()
	for _, n := range []int{3, bulkEditMin + 20} {
		var sb strings.Builder
		var edits []LSPEdit
		for i := range n {
			fmt.Fprintf(&sb, "line %d\n", i)
			edits = append(edits, lspEdit(i, 0, i, 4, "LINE"))
		}
		text := sb.String()
		e, ids := tabsWith(text)
		if err := e.ApplyEdits(ids[0], edits, EditOptions{}); err != nil {
			t.Fatal(err)
		}
		b := e.Active().Buffer
		if !strings.HasPrefix(b.Text(), "LINE 0\nLINE 1\n") {
			t.Fatalf("n=%d: text = %q", n, b.Text()[:20])
		}
		if _, ok := b.Undo(); !ok || b.Text() != text {
			t.Fatalf("n=%d: one Undo did not restore the text", n)
		}
		if _, ok := b.Undo(); ok {
			t.Fatalf("n=%d: a second undo step exists", n)
		}
		b.Redo()
		if !strings.HasPrefix(b.Text(), "LINE 0\n") {
			t.Fatalf("n=%d: redo did not reapply", n)
		}
	}
}

func TestApplyEditsBulkSingleUndoStep(t *testing.T) {
	t.Parallel()
	e, ids := tabsWith(strings.Repeat("abc def\n", 300))
	var edits []LSPEdit
	for i := range 300 {
		edits = append(edits, lspEdit(i, 4, i, 7, "XYZ"))
	}
	b := e.Active().Buffer
	before := b.Text()
	if err := e.ApplyEdits(ids[0], edits, EditOptions{}); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(b.Text(), "abc XYZ\nabc XYZ\n") || b.Lines() != 301 {
		t.Fatalf("text = %q... lines %d", b.Text()[:20], b.Lines())
	}
	if _, ok := b.Undo(); !ok || b.Text() != before {
		t.Fatal("Undo did not restore the text")
	}
	if _, ok := b.Undo(); ok {
		t.Fatal("more than one undo step")
	}
}

// randomEdits returns n non-overlapping edits over text, in random order, with
// multi-byte and multi-line replacements.
func randomEdits(rng *rand.Rand, b *buffer.Buffer, n int) []LSPEdit {
	var edits []LSPEdit
	cur := pos(0, 0)
	repl := []string{"", "x", "é", "日本", "a\nb", "\n", "long replacement text", "😀\n😀"}
	for range n {
		// Advance a little, then take a short or empty range.
		from := advanceBy(b, cur, rng.IntN(7))
		to := advanceBy(b, from, rng.IntN(5))
		if b.End().Less(to) {
			break
		}
		edits = append(edits, LSPEdit{From: from, To: to, Text: repl[rng.IntN(len(repl))]})
		cur = to
		if rng.IntN(4) == 0 && to == from {
			cur = advanceBy(b, to, 1)
		}
	}
	rng.Shuffle(len(edits), func(i, j int) { edits[i], edits[j] = edits[j], edits[i] })
	return edits
}

// advanceBy moves p forward by n runes, crossing line ends as one step each.
func advanceBy(b *buffer.Buffer, p buffer.Pos, n int) buffer.Pos {
	for range n {
		switch {
		case p.Col < b.LineLen(p.Line):
			p.Col++
		case p.Line < b.Lines()-1:
			p = pos(p.Line+1, 0)
		}
	}
	return p
}

// applyOneByOne is the reference: the edits made through the buffer, last first.
func applyOneByOne(text string, edits []LSPEdit) string {
	b := buffer.New(text)
	sorted := slices.Clone(edits)
	slices.SortStableFunc(sorted, func(x, y LSPEdit) int {
		if c := comparePos(x.From, y.From); c != 0 {
			return c
		}
		return comparePos(x.To, y.To)
	})
	for _, ed := range slices.Backward(sorted) {
		b.Delete(ed.From, ed.To)
		b.Insert(ed.From, ed.Text)
	}
	return b.Text()
}

func TestApplyEditsBulkMatchesIncremental(t *testing.T) {
	t.Parallel()
	var base strings.Builder
	for i := range 400 {
		fmt.Fprintf(&base, "line %d héllo 日本語 😀 tail\n", i)
		if i%7 == 0 {
			base.WriteString("\n")
		}
	}
	text := base.String()
	for seed := uint64(1); seed <= 25; seed++ {
		rng := rand.New(rand.NewPCG(seed, 3))
		n := 5 + rng.IntN(300)
		e, ids := tabsWith(text)
		edits := randomEdits(rng, e.Active().Buffer, n)
		want := applyOneByOne(text, edits)
		if err := e.ApplyEdits(ids[0], edits, EditOptions{}); err != nil {
			t.Fatalf("seed %d (%d edits): %v", seed, len(edits), err)
		}
		if got := e.Active().Buffer.Text(); got != want {
			t.Fatalf("seed %d (%d edits): text differs from the one-by-one result", seed, len(edits))
		}
	}
}

func TestApplyEditsChangeRecordsReplayToTheSameText(t *testing.T) {
	t.Parallel()
	text := strings.Repeat("héllo\nwörld\n", 100)
	for _, n := range []int{10, 150} {
		rng := rand.New(rand.NewPCG(uint64(n), 8))
		e, ids := tabsWith(text)
		e.TrackChanges("events", true)
		edits := randomEdits(rng, e.Active().Buffer, n)
		if err := e.ApplyEdits(ids[0], edits, EditOptions{}); err != nil {
			t.Fatal(err)
		}
		changes, over := e.TakeChanges(ids[0], "events")
		if over {
			// A bulk replacement of a big span is over the payload cap only if the
			// span is; this one is small.
			t.Fatalf("n=%d: overflow", n)
		}
		m := strings.Split(text, "\n")
		lines := make([][]rune, len(m))
		for i, s := range m {
			lines[i] = []rune(s)
		}
		for _, c := range changes {
			head := slices.Clone(lines[c.From.Line][:c.From.Col])
			tail := slices.Clone(lines[c.To.Line][c.To.Col:])
			parts := strings.Split(c.Text, "\n")
			ins := make([][]rune, len(parts))
			for i, p := range parts {
				ins[i] = []rune(p)
			}
			ins[0] = append(head, ins[0]...)
			ins[len(ins)-1] = append(ins[len(ins)-1], tail...)
			lines = slices.Concat(lines[:c.From.Line], ins, lines[c.To.Line+1:])
		}
		var out []string
		for _, l := range lines {
			out = append(out, string(l))
		}
		if got := strings.Join(out, "\n"); got != e.Active().Buffer.Text() {
			t.Fatalf("n=%d: replaying %d change records gives a different text", n, len(changes))
		}
	}
}

func TestApplyEditsBulkPerf(t *testing.T) {
	if testing.Short() {
		t.Skip("performance check")
	}
	var sb strings.Builder
	for i := range 100_000 {
		fmt.Fprintf(&sb, "line %d some text\n", i)
	}
	e, ids := tabsWith(sb.String())
	edits := make([]LSPEdit, 0, 50_000)
	for i := range 50_000 {
		edits = append(edits, lspEdit(i*2, 0, i*2, 4, "LINE"))
	}
	start := time.Now()
	if err := e.ApplyEdits(ids[0], edits, EditOptions{}); err != nil {
		t.Fatal(err)
	}
	if took := time.Since(start); took > 300*time.Millisecond {
		t.Errorf("50000 edits on 100000 lines took %v, want under 300ms", took)
	}
	b := e.Active().Buffer
	if b.Line(0) != "LINE 0 some text" || b.Line(1) != "line 1 some text" || b.Line(99_998) != "LINE 99998 some text" {
		t.Errorf("unexpected lines %q %q %q", b.Line(0), b.Line(1), b.Line(99_998))
	}
}

func TestApplyEditsCursorModes(t *testing.T) {
	t.Parallel()
	// "hello world\nsecond line": edit "world" -> "gvim!" and insert "> " at (1,0).
	edits := []LSPEdit{lspEdit(0, 6, 0, 11, "gvim!"), lspEdit(1, 0, 1, 0, "> ")}
	tests := []struct {
		name    string
		mode    CursorMode
		cursor  buffer.Pos
		want    buffer.Pos
		sel     *buffer.Selection
		wantSel *buffer.Selection
	}{
		{"map: before the edit", CursorMap, pos(0, 2), pos(0, 2), nil, nil},
		{"map: at the start of a replaced range", CursorMap, pos(0, 6), pos(0, 6), nil, nil},
		{"map: inside a replaced range goes after the text", CursorMap, pos(0, 8), pos(0, 11), nil, nil},
		{"map: at the end of a replaced range", CursorMap, pos(0, 11), pos(0, 11), nil, nil},
		{"map: at an insert point moves with it", CursorMap, pos(1, 0), pos(1, 2), nil, nil},
		{"map: after an insert on its line", CursorMap, pos(1, 4), pos(1, 6), nil, nil},
		{"map: selection", CursorMap, pos(1, 3), pos(1, 5),
			&buffer.Selection{Anchor: pos(0, 0), Head: pos(1, 3)}, &buffer.Selection{Anchor: pos(0, 0), Head: pos(1, 5)}},
		{"keep", CursorKeep, pos(1, 4), pos(1, 4),
			&buffer.Selection{Anchor: pos(0, 1), Head: pos(1, 4)}, &buffer.Selection{Anchor: pos(0, 1), Head: pos(1, 4)}},
		{"end: the end of the last edit", CursorEnd, pos(0, 0), pos(1, 2),
			&buffer.Selection{Anchor: pos(0, 1), Head: pos(0, 0)}, nil},
	}
	for _, tt := range tests {
		e, ids := tabsWith("hello world\nsecond line")
		d := e.Active()
		d.Cursor, d.Sel = tt.cursor, tt.sel
		if err := e.ApplyEdits(ids[0], edits, EditOptions{Cursor: tt.mode}); err != nil {
			t.Fatalf("%s: %v", tt.name, err)
		}
		if d.Cursor != tt.want || !sameSel(d.Sel, tt.wantSel) {
			t.Errorf("%s: cursor %v sel %v, want %v sel %v", tt.name, d.Cursor, d.Sel, tt.want, tt.wantSel)
		}
	}
}

func sameSel(a, b *buffer.Selection) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

func TestApplyEditsMapsExtraCaretsAndMergesThem(t *testing.T) {
	t.Parallel()
	e, ids := tabsWith("aaa bbb ccc")
	d := e.Active()
	d.Cursor = pos(0, 0)
	d.extra = []Caret{{Cursor: pos(0, 4)}, {Cursor: pos(0, 8)}}
	if err := e.ApplyEdits(ids[0], []LSPEdit{lspEdit(0, 0, 0, 0, ">>"), lspEdit(0, 4, 0, 7, "B")}, EditOptions{}); err != nil {
		t.Fatal(err)
	}
	if d.Buffer.Text() != ">>aaa B ccc" {
		t.Fatalf("text = %q", d.Buffer.Text())
	}
	got := []buffer.Pos{d.Cursor}
	for _, c := range d.Cursors() {
		got = append(got, c.Cursor)
	}
	// The first caret sat at an insert point and moved after it; the second sat
	// at the start of the replaced range; the third was after it.
	want := []buffer.Pos{pos(0, 2), pos(0, 6), pos(0, 8)}
	if !slices.Equal(got, want) {
		t.Fatalf("carets = %v, want %v", got, want)
	}
	// Two carets inside one replaced range end up at one point and merge.
	e, ids = tabsWith("abcdef")
	d = e.Active()
	d.Cursor = pos(0, 2)
	d.extra = []Caret{{Cursor: pos(0, 4)}}
	if err := e.ApplyEdits(ids[0], []LSPEdit{lspEdit(0, 1, 0, 5, "X")}, EditOptions{}); err != nil {
		t.Fatal(err)
	}
	if len(d.Cursors()) != 0 || d.Cursor != pos(0, 2) {
		t.Fatalf("carets = %v + %v, want one at 0,2", d.Cursor, d.Cursors())
	}
}

func TestApplyEditsOnLargeDocument(t *testing.T) {
	t.Parallel()
	e, d := openLarge(t, largeOptions(), lineText(300))
	indexAll(t, e)
	id := d.ID()
	if err := e.ApplyEdits(id, []LSPEdit{lspEdit(5, 0, 5, 4, "LINE"), lspEdit(7, 9, 7, 9, "!")}, EditOptions{}); err != nil {
		t.Fatalf("ApplyEdits: %v", err)
	}
	if d.Buffer.Line(5) != "LINE 0005" || d.Buffer.Line(7) != "line 0007!" {
		t.Fatalf("lines = %q %q", d.Buffer.Line(5), d.Buffer.Line(7))
	}
	if _, ok := d.Buffer.Undo(); !ok || d.Buffer.Line(5) != "line 0005" {
		t.Fatal("the edits are not one undo step")
	}
	// More than the bulk threshold still goes edit by edit, never materialising.
	var many []LSPEdit
	for i := range bulkEditMin + 20 {
		many = append(many, lspEdit(i, 0, i, 4, "LINE"))
	}
	reads := d.Buffer.Stats().MaterializedBytes
	if err := e.ApplyEdits(id, many, EditOptions{}); err != nil {
		t.Fatalf("many edits: %v", err)
	}
	if d.Buffer.Line(80) != "LINE 0080" || d.Buffer.Stats().MaterializedBytes != reads {
		t.Errorf("line 80 = %q, materialized %d -> %d", d.Buffer.Line(80), reads, d.Buffer.Stats().MaterializedBytes)
	}
	if err := e.ApplyEdits(id, []LSPEdit{lspEdit(1, 0, 1, 1, "x"), lspEdit(1, 0, 1, 2, "y")}, EditOptions{}); !errors.Is(err, ErrOverlappingEdits) {
		t.Errorf("overlap: %v", err)
	}
}

func TestApplyEditsRefusedInLargeDocumentChangesNothing(t *testing.T) {
	t.Parallel()
	// Line 100 is longer than the line cap, so it is read whole for an edit, and its runes do not
	// fit the overlay budget: the edit is refused, and the edit before it in the group is undone.
	content := lineText(100) + strings.Repeat("a", 200) + "\n" + lineText(100)
	opts := largeOptions()
	opts.largeGeom.MaxOverlayBytes = 400
	e, d := openLarge(t, opts, content)
	indexAll(t, e)
	before := d.Version()
	err := e.ApplyEdits(d.ID(), []LSPEdit{lspEdit(2, 0, 2, 4, "LINE"), lspEdit(100, 0, 100, 1, "x")}, EditOptions{})
	if err == nil {
		t.Fatal("an edit of an invalid line was accepted")
	}
	if d.Buffer.Line(2) != "line 0002" || d.Version() != before {
		t.Errorf("the refused group left changes: %q, %+v -> %+v", d.Buffer.Line(2), before, d.Version())
	}
}

func TestIDMethods(t *testing.T) {
	t.Parallel()
	e, ids := tabsWith("one\ntwo", "three")
	// SetCursorID and SelectID act on a document that is not active.
	if err := e.SetCursorID(ids[1], pos(0, 2), false); err != nil {
		t.Fatal(err)
	}
	if err := e.SelectID(ids[1], pos(0, 1), pos(0, 4)); err != nil {
		t.Fatal(err)
	}
	other := e.docs[1]
	if other.Cursor != pos(0, 4) || other.Sel == nil || other.Sel.Anchor != pos(0, 1) {
		t.Errorf("cursor %v sel %v", other.Cursor, other.Sel)
	}
	if e.Active().Cursor != pos(0, 0) {
		t.Errorf("the active tab's cursor moved to %v", e.Active().Cursor)
	}
	if err := e.SetCursorID(ids[1], pos(9, 9), true); err != nil || other.Cursor != pos(0, 5) {
		t.Errorf("extending past the end: %v, cursor %v", err, other.Cursor)
	}
	if err := e.SetLanguageID(ids[1], "go"); err != nil || other.language != "go" {
		t.Errorf("SetLanguageID: %v, %q", err, other.language)
	}
	// Group makes one undo step of edits through the document.
	err := e.Group(ids[1], func() error {
		other.Buffer.Insert(pos(0, 0), "a")
		other.Buffer.Insert(pos(0, 3), "b")
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := other.Buffer.Undo(); !ok || other.Buffer.Text() != "three" {
		t.Errorf("group is not one undo step: %q", other.Buffer.Text())
	}
	boom := errors.New("boom")
	if err := e.Group(ids[1], func() error { return boom }); !errors.Is(err, boom) {
		t.Errorf("Group error = %v", err)
	}
	// CloseID refuses unsaved text unless forced.
	other.Buffer.Insert(pos(0, 0), "x")
	if err := e.CloseID(ids[1], false); !errors.Is(err, ErrUnsaved) {
		t.Errorf("CloseID of a modified document: %v", err)
	}
	if err := e.CloseID(ids[1], true); err != nil || len(e.docs) != 1 {
		t.Errorf("forced CloseID: %v, %d tabs", err, len(e.docs))
	}
	for name, err := range map[string]error{
		"SetCursorID": e.SetCursorID(ids[1], pos(0, 0), false),
		"SelectID":    e.SelectID(ids[1], pos(0, 0), pos(0, 1)),
		"Language":    e.SetLanguageID(ids[1], "go"),
		"Group":       e.Group(ids[1], func() error { return nil }),
		"CloseID":     e.CloseID(ids[1], true),
		"ActivateID":  e.ActivateID(ids[1]),
	} {
		if !errors.Is(err, ErrNotOpen) {
			t.Errorf("%s of a closed document: %v, want ErrNotOpen", name, err)
		}
	}
}
