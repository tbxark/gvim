package tui

import (
	"image/color"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/decorations"
	"github.com/tbxark/gvim/internal/tui/editorpane"
	"github.com/tbxark/gvim/pluginapi"
	"github.com/tbxark/gvim/theme"
)

// decorDoc is a fake document that is a plugin buffer.
type decorDoc struct {
	*fakeDoc
	id pluginapi.BufferID
}

func (d decorDoc) BufferID() pluginapi.BufferID { return d.id }

// decorFake is a multiFake whose documents have buffer ids: tab i is buffer i+1.
type decorFake struct{ *multiFake }

func (f decorFake) Active() Document {
	return decorDoc{f.docs[f.active], pluginapi.BufferID(f.active + 1)}
}

func (f decorFake) DocAt(i int) Document {
	if i < 0 || i >= len(f.docs) {
		return nil
	}
	return decorDoc{f.docs[i], pluginapi.BufferID(i + 1)}
}

// decorPort is a plugin editor port that can also read lines, as the host does to find
// the end of a line.
type decorPort struct {
	*fakePluginEditor
	lines map[pluginapi.BufferID][]string
}

func (p decorPort) Lines(id pluginapi.BufferID, first, count int) ([]string, error) {
	ls := p.lines[id]
	if first >= len(ls) {
		return nil, nil
	}
	return ls[first:min(first+count, len(ls))], nil
}

// decorModel returns a model over documents with the given lines, with the plugin host
// wired to a plugin editor port that knows the same buffers.
func decorModel(t *testing.T, docs ...[]string) (*Model, decorFake, decorPort) {
	t.Helper()
	f := decorFake{newDocsFake(docs...)}
	pe := decorPort{&fakePluginEditor{active: 1}, map[pluginapi.BufferID][]string{}}
	for i, d := range docs {
		id := pluginapi.BufferID(i + 1)
		pe.bufs = append(pe.bufs, pluginapi.BufferInfo{ID: id, Name: "b", LineCount: len(d), Version: pluginapi.Version{Gen: 1}})
		pe.lines[id] = d
	}
	rt := newFakeRuntime(t)
	m := New(Deps{Editor: f, Keys: testKeys(), Theme: theme.Theme{}, Clock: testClock, Root: RootPorts{Plugins: rt, PluginEditor: pe}})
	m.Update(tea.WindowSizeMsg{Width: 60, Height: 14})
	useHost(rt, m)
	return m, f, pe
}

var (
	dRed   = color.RGBA{R: 255, A: 255}
	dGreen = color.RGBA{G: 255, A: 255}
	dBlue  = color.RGBA{B: 255, A: 255}
)

func lit(c color.RGBA) pluginapi.Color { return pluginapi.Color{R: c.R, G: c.G, B: c.B, Set: true} }

func bgStyle(c color.RGBA) pluginapi.Style { return pluginapi.Style{BG: lit(c)} }

func TestDecorPaintsTheDocumentsPane(t *testing.T) {
	m, _, _ := decorModel(t, []string{"hello world", "second line", "third"})
	err := m.plug.host.DecorSet("p", "a", 1, []pluginapi.Decoration{
		{Kind: pluginapi.DecorHighlight, Range: pluginapi.Range{From: pluginapi.Pos{Col: 6}, To: pluginapi.Pos{Col: 9}}, Style: bgStyle(dBlue)},
		{Kind: pluginapi.DecorEOL, Line: 1, Text: " <- eol", Style: pluginapi.Style{FG: lit(dGreen)}},
		{Kind: pluginapi.DecorGutter, Line: 2, Text: "+", Style: pluginapi.Style{FG: lit(dGreen)}},
		{Kind: pluginapi.DecorScroll, Line: 1, Color: lit(dRed)},
		{Kind: pluginapi.DecorReplace, Pos: pluginapi.Pos{Line: 2, Col: 1}, Text: "X", Style: bgStyle(dRed)},
		{Kind: pluginapi.DecorLine, Line: 2, Style: bgStyle(dBlue)},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	p := panesOf(t, m, 1)[0]
	g := m.paint()
	at := func(col, line int) (string, color.Color, color.Color) {
		c := g.At(p.TextX+col, p.Y+line)
		return c.Text, c.Style.FG, c.Style.BG
	}
	// The highlight covers "wor" and nothing else.
	for col, want := range map[int]color.Color{5: nil, 6: dBlue, 8: dBlue, 9: nil} {
		if _, _, got := at(col, 0); got != want {
			t.Errorf("highlight: cell %d bg = %v, want %v", col, got, want)
		}
	}
	// Virtual text follows the line.
	if txt, fg, _ := at(len("second line")+1, 1); txt != "<" || fg != dGreen {
		t.Errorf("virtual text cell = %q fg %v, want a green <", txt, fg)
	}
	// The gutter mark replaces the first gutter column of line 2.
	if c := g.At(p.X, p.Y+2); c.Text != "+" || c.Style.FG != dGreen {
		t.Errorf("gutter cell = %q fg %v, want a green +", c.Text, c.Style.FG)
	}
	// The scroll mark is in the last column of the pane.
	if c := g.At(p.X+p.W-1, p.Y+1); c.Style.FG != dRed {
		t.Errorf("scroll mark fg = %v, want red", c.Style.FG)
	}
	// The replacement shows over the "h" of "third", on a line that has a row style.
	if txt, _, b := at(1, 2); txt != "X" || b != dRed {
		t.Errorf("replace cell = %q bg %v, want a red X", txt, b)
	}
	if _, _, b := at(0, 2); b != dBlue {
		t.Errorf("row style: first cell bg = %v, want blue", b)
	}
	if _, _, b := at(p.TextW-3, 2); b != dBlue {
		t.Errorf("row style: blank cell near the edge bg = %v, want blue", b)
	}
	// An undecorated cell carries none of it.
	if _, _, b := at(0, 0); b != nil {
		t.Errorf("an undecorated cell has bg %v", b)
	}
}

func TestDecorFollowsTheTextWhenAnchorsMove(t *testing.T) {
	m, _, _ := decorModel(t, []string{"hello world", "second line here"})
	if err := m.plug.host.DecorSet("p", "a", 1, []pluginapi.Decoration{
		{Kind: pluginapi.DecorHighlight, Range: pluginapi.Range{From: pluginapi.Pos{Col: 6}, To: pluginapi.Pos{Col: 11}}, Style: bgStyle(dBlue)},
	}, nil); err != nil {
		t.Fatal(err)
	}
	p := panesOf(t, m, 1)[0]
	// The editor's change listener does this when a line is inserted above. The fake
	// document does not change, so only the row of the highlight is checked: it moved down
	// one line, and the first line is clear.
	m.plug.decor.Apply(1, []decorations.Edit{{Text: "new\n"}}, false)
	g := m.paint()
	if got := g.At(p.TextX+6, p.Y).Style.BG; got != nil {
		t.Errorf("line 0 is still highlighted: bg %v", got)
	}
	if got := g.At(p.TextX+6, p.Y+1).Style.BG; got != dBlue {
		t.Errorf("line 1 bg = %v, want the highlight that moved down", got)
	}
}

func TestDecorByteColumnsBecomeRuneColumns(t *testing.T) {
	m, _, _ := decorModel(t, []string{"é世x end"})
	// é is 2 bytes, 世 is 3: bytes 2 to 5 are the 世.
	if err := m.plug.host.DecorSet("p", "a", 1, []pluginapi.Decoration{
		{Kind: pluginapi.DecorHighlight, Range: pluginapi.Range{From: pluginapi.Pos{Col: 2}, To: pluginapi.Pos{Col: 5}}, Style: bgStyle(dBlue)},
	}, nil); err != nil {
		t.Fatal(err)
	}
	p := panesOf(t, m, 1)[0]
	g := m.paint()
	// Cells: é at 0, 世 at 1 and 2 (wide), x at 3.
	for col, want := range map[int]color.Color{0: nil, 1: dBlue, 3: nil} {
		if got := g.At(p.TextX+col, p.Y).Style.BG; got != want {
			t.Errorf("cell %d bg = %v, want %v", col, got, want)
		}
	}
	if got := g.At(p.TextX+1, p.Y).Text; got != "世" {
		t.Errorf("cell 1 = %q, want 世", got)
	}
}

func TestDecorOnlyOnItsOwnDocumentAcrossSplits(t *testing.T) {
	m, _, _ := decorModel(t, []string{"alpha"}, []string{"beta"})
	leavesOver(m, 0, 1)
	if err := m.plug.host.DecorSet("p", "a", 2, []pluginapi.Decoration{
		{Kind: pluginapi.DecorLine, Line: 0, Style: bgStyle(dBlue)},
	}, nil); err != nil {
		t.Fatal(err)
	}
	ps := panesOf(t, m, 2)
	g := m.paint()
	if got := g.At(ps[0].TextX, ps[0].Y).Style.BG; got != nil {
		t.Errorf("the pane of buffer 1 shows buffer 2's decoration: bg %v", got)
	}
	if got := g.At(ps[1].TextX, ps[1].Y).Style.BG; got != dBlue {
		t.Errorf("the pane of buffer 2 bg = %v, want blue", got)
	}
}

func TestDecorHooksAreOnlyOnPanesWithDecorations(t *testing.T) {
	m, f, _ := decorModel(t, []string{"a"}, []string{"b"})
	pane := func() editorpane.Pane { return m.paneGeometry(panesOf(t, m, 1)[0], f.Active()) }
	if p := pane(); p.Overlays != nil || p.EOL != nil || p.Replaces != nil || p.Gutter != nil || p.ScrollMarks != nil || p.RowStyle != nil {
		t.Fatal("a pane with no decorations has hooks")
	}
	_ = m.plug.host.DecorSet("p", "a", 2, []pluginapi.Decoration{{Kind: pluginapi.DecorLine, Line: 0}}, nil)
	if p := pane(); p.RowStyle != nil {
		t.Fatal("the active buffer got the hooks of another buffer")
	}
	_ = m.plug.host.DecorSet("p", "a", 1, []pluginapi.Decoration{{Kind: pluginapi.DecorLine, Line: 0}}, nil)
	if p := pane(); p.RowStyle == nil || p.Overlays == nil {
		t.Fatal("a pane with decorations has no hooks")
	}
	// A root with no runtime has no store, and its panes are untouched.
	plain := newTestModel(t, newFakeEditor("x"), nil, 40, 10)
	if q := plain.paneGeometry(panesOf(t, plain, 1)[0], plain.ed.Active()); q.RowStyle != nil || plain.plug.decor != nil {
		t.Fatal("a model with no plugins has decoration state")
	}
}

func TestDecorHostRefusals(t *testing.T) {
	m, _, pe := decorModel(t, []string{"a", "b", "c"})
	h := m.plug.host
	one := []pluginapi.Decoration{{Kind: pluginapi.DecorLine, Line: 0}}
	stale := pluginapi.Version{Gen: 1, Ver: 9}
	same := pe.bufs[0].Version
	tests := []struct {
		name string
		call func() error
		want pluginapi.Code
	}{
		{"set on a closed buffer", func() error { return h.DecorSet("p", "a", 9, one, nil) }, pluginapi.CodeClosed},
		{"add on a closed buffer", func() error { _, err := h.DecorAdd("p", "a", 9, one[0], nil); return err }, pluginapi.CodeClosed},
		{"set with a stale version", func() error { return h.DecorSet("p", "a", 1, one, &stale) }, pluginapi.CodeStale},
		{"add with a stale version", func() error { _, err := h.DecorAdd("p", "a", 1, one[0], &stale); return err }, pluginapi.CodeStale},
		{"set with the current version", func() error { return h.DecorSet("p", "a", 1, one, &same) }, ""},
		{"set with no version check", func() error { return h.DecorSet("p", "a", 1, one, nil) }, ""},
		{"an unknown kind", func() error {
			return h.DecorSet("p", "a", 1, []pluginapi.Decoration{{Kind: 99}}, nil)
		}, pluginapi.CodeInvalid},
		{"text that is too long", func() error {
			return h.DecorSet("p", "a", 1, []pluginapi.Decoration{{Kind: pluginapi.DecorEOL, Text: strings.Repeat("x", decorMaxText+1)}}, nil)
		}, pluginapi.CodeInvalid},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.call()
			if tc.want == "" {
				if err != nil {
					t.Fatalf("err = %v", err)
				}
				return
			}
			if got := pluginapi.CodeOf(err); err == nil || got != tc.want {
				t.Fatalf("err = %v (%s), want code %s", err, got, tc.want)
			}
		})
	}
	// A refused set leaves the namespace as it was.
	_ = h.DecorSet("p", "keep", 1, one, nil)
	_ = h.DecorSet("p", "keep", 1, []pluginapi.Decoration{{Kind: 99}}, nil)
	if n := len(m.plug.decor.Items(1, "p", "keep")); n != 1 {
		t.Fatalf("a refused set changed the namespace: %d items, want 1", n)
	}
}

func TestDecorStaleAfterEditChangesVersion(t *testing.T) {
	m, _, pe := decorModel(t, []string{"a"})
	tok := pe.bufs[0].Version
	pe.bufs[0].Version.Ver++ // an edit
	err := m.plug.host.DecorSet("p", "a", 1, []pluginapi.Decoration{{Kind: pluginapi.DecorLine}}, &tok)
	if pluginapi.CodeOf(err) != pluginapi.CodeStale {
		t.Fatalf("err = %v, want stale", err)
	}
	if m.plug.decor.Count(1) != 0 {
		t.Fatal("a stale set changed the store")
	}
}

func TestDecorLimitsMapToLimitErrors(t *testing.T) {
	m, _, _ := decorModel(t, []string{"a"})
	h := m.plug.host
	items := make([]pluginapi.Decoration, decorations.MaxPerNamespace+1)
	for i := range items {
		items[i] = pluginapi.Decoration{Kind: pluginapi.DecorGutter, Text: "+"}
	}
	if err := h.DecorSet("p", "a", 1, items, nil); pluginapi.CodeOf(err) != pluginapi.CodeLimit {
		t.Fatalf("err = %v, want limit", err)
	}
	if err := h.DecorSet("p", "a", 1, items[:decorations.MaxPerNamespace], nil); err != nil {
		t.Fatal(err)
	}
	if _, err := h.DecorAdd("p", "a", 1, items[0], nil); pluginapi.CodeOf(err) != pluginapi.CodeLimit {
		t.Fatalf("Add at the cap: err = %v, want limit", err)
	}
}

func TestDecorItemConversion(t *testing.T) {
	m, _, _ := decorModel(t, []string{"abc", "de", "fgh"})
	h := m.plug.host
	tests := []struct {
		name string
		in   pluginapi.Decoration
		want decorations.Item
	}{
		{"line past the end is the last line", pluginapi.Decoration{Kind: pluginapi.DecorGutter, Line: 99, Text: "+"},
			decorations.Item{Kind: decorations.Gutter, FromLine: 2, ToLine: 2, Text: "+"}},
		{"negative line is the first", pluginapi.Decoration{Kind: pluginapi.DecorLine, Line: -4},
			decorations.Item{Kind: decorations.Line}},
		{"a column past anything is the end of the line", pluginapi.Decoration{Kind: pluginapi.DecorHighlight,
			Range: pluginapi.Range{From: pluginapi.Pos{Line: 1, Col: 1}, To: pluginapi.Pos{Line: 1, Col: 1 << 31}}},
			decorations.Item{Kind: decorations.Highlight, FromLine: 1, FromCol: 1, ToLine: 1, ToCol: 2}},
		{"a range past the end of the document ends at the end of it", pluginapi.Decoration{Kind: pluginapi.DecorHighlight,
			Range: pluginapi.Range{From: pluginapi.Pos{Line: 2, Col: 1}, To: pluginapi.Pos{Line: 50}}},
			decorations.Item{Kind: decorations.Highlight, FromLine: 2, FromCol: 1, ToLine: 2, ToCol: 3}},
		{"a scroll range keeps its lines", pluginapi.Decoration{Kind: pluginapi.DecorScroll,
			Range: pluginapi.Range{From: pluginapi.Pos{Line: 0, Col: 2}, To: pluginapi.Pos{Line: 2}}, Color: pluginapi.Color{Key: "add"}},
			decorations.Item{Kind: decorations.Scroll, ToLine: 2, Color: decorations.Color{Key: "add"}}},
		{"a scroll with an empty range is its line", pluginapi.Decoration{Kind: pluginapi.DecorScroll, Line: 1},
			decorations.Item{Kind: decorations.Scroll, FromLine: 1, ToLine: 1}},
		{"replace keeps its column", pluginapi.Decoration{Kind: pluginapi.DecorReplace, Pos: pluginapi.Pos{Line: 1, Col: 1}, Text: "x", Priority: 4},
			decorations.Item{Kind: decorations.Replace, FromLine: 1, FromCol: 1, ToLine: 1, ToCol: 1, Text: "x", Priority: 4}},
		{"style and color are carried over", pluginapi.Decoration{Kind: pluginapi.DecorEOL, Text: "t",
			Style: pluginapi.Style{FG: pluginapi.Color{Key: "error"}, BG: lit(dRed), Bold: true, Strike: true, Extend: true}},
			decorations.Item{Kind: decorations.EOL, Text: "t", Style: decorations.Style{
				FG: decorations.Color{Key: "error"}, BG: decorations.Color{R: 255, Set: true}, Bold: true, Strike: true, Extend: true}}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if err := h.DecorSet("p", "n", 1, []pluginapi.Decoration{tc.in}, nil); err != nil {
				t.Fatal(err)
			}
			got := m.plug.decor.Items(1, "p", "n")
			if len(got) != 1 || got[0] != tc.want {
				t.Fatalf("store holds %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestDecorOwnerGoneAndRemoveAndClear(t *testing.T) {
	m, _, _ := decorModel(t, []string{"a", "b"}, []string{"c"})
	h := m.plug.host
	set := func(plugin, ns string, buf pluginapi.BufferID) {
		t.Helper()
		if err := h.DecorSet(plugin, ns, buf, []pluginapi.Decoration{{Kind: pluginapi.DecorLine, Line: 0}}, nil); err != nil {
			t.Fatal(err)
		}
	}
	set("p", "a", 1)
	set("p", "b", 2)
	set("q", "a", 1)
	id, err := h.DecorAdd("q", "a", 1, pluginapi.Decoration{Kind: pluginapi.DecorLine, Line: 1}, nil)
	if err != nil || id == 0 {
		t.Fatalf("DecorAdd = %d, %v", id, err)
	}
	h.DecorRemove("q", "a", 1, id)
	if got := m.plug.decor.OwnerCount("q"); got != 1 {
		t.Fatalf("q holds %d after removing one, want 1", got)
	}
	h.DecorClear("p", "b", 0) // every buffer
	if m.plug.decor.OwnerCount("p") != 1 {
		t.Fatalf("p holds %d after clearing b, want 1", m.plug.decor.OwnerCount("p"))
	}
	h.OwnerGone("p", 1)
	if m.plug.decor.OwnerCount("p") != 0 || m.plug.decor.OwnerCount("q") != 1 {
		t.Fatalf("OwnerGone: p %d q %d, want 0 and 1", m.plug.decor.OwnerCount("p"), m.plug.decor.OwnerCount("q"))
	}
}

func TestDecorForgetsClosedBuffers(t *testing.T) {
	m, _, pe := decorModel(t, []string{"a"}, []string{"b"})
	h := m.plug.host
	for _, id := range []pluginapi.BufferID{1, 2} {
		if err := h.DecorSet("p", "a", id, []pluginapi.Decoration{{Kind: pluginapi.DecorLine}}, nil); err != nil {
			t.Fatal(err)
		}
	}
	pe.bufs = pe.bufs[:1] // buffer 2 is closed
	// The next call that touches the store frees what the closed buffer held.
	h.DecorClear("p", "nothing", 1)
	if got := m.plug.decor.Docs(); !slices.Equal(got, []uint64{1}) {
		t.Fatalf("documents with decorations = %v, want [1]", got)
	}
}

// storePort is a plugin editor port that names the store its editor's listener feeds.
type storePort struct {
	*fakePluginEditor
	st *decorations.Store
}

func (p storePort) DecorStore() *decorations.Store { return p.st }

func TestDecorStoreIsTheOneThePortNames(t *testing.T) {
	st := decorations.New()
	f := decorFake{newDocsFake([]string{"x"})}
	pe := storePort{&fakePluginEditor{bufs: []pluginapi.BufferInfo{{ID: 1, LineCount: 1}}}, st}
	m := New(Deps{Editor: f, Keys: testKeys(), Theme: theme.Theme{}, Clock: testClock, Root: RootPorts{Plugins: newFakeRuntime(t), PluginEditor: pe}})
	if m.plug.decor != st {
		t.Fatal("the model made its own store")
	}
}

// longLineDoc reports line lengths through the LineWindower view and counts the reads.
type longLineDoc struct {
	*fakeDoc
	bytes []int
	read  map[int]int
}

func (d *longLineDoc) LineBytes(i int) int          { return d.bytes[i] }
func (d *longLineDoc) LineSlice(i, _, _ int) string { return d.lines[i] }
func (d *longLineDoc) Line(i int) string {
	if d.read == nil {
		d.read = map[int]int{}
	}
	d.read[i]++
	return d.fakeDoc.Line(i)
}

func TestDecorViewConvertsColumnsWithoutReadingLongLines(t *testing.T) {
	long := &longLineDoc{fakeDoc: &fakeDoc{lines: []string{"é世x", "short"}}, bytes: []int{editorpane.LongLineRunes + 1, 5}}
	st := decorations.New()
	_ = st.Set(1, "p", "a", []decorations.Item{
		{Kind: decorations.Highlight, FromLine: 0, FromCol: 2, ToLine: 0, ToCol: 5},
		{Kind: decorations.Highlight, FromLine: 1, FromCol: 1, ToLine: 1, ToCol: 3},
	})
	v := &decorView{st: st, id: 1, doc: long}
	// A line of a large document that is too long to read keeps its columns: they are the
	// runes' for ASCII, and the line is not decoded.
	if got := v.spans(0); len(got) != 1 || got[0].Start != 2 || got[0].End != 5 {
		t.Fatalf("long line spans = %+v, want the byte columns as they are", got)
	}
	if long.read[0] != 0 {
		t.Fatalf("the long line was read %d times", long.read[0])
	}
	if got := v.spans(1); len(got) != 1 || got[0].Start != 1 || got[0].End != 3 {
		t.Fatalf("short line spans = %+v", got)
	}
}

func TestRuneCol(t *testing.T) {
	tests := []struct {
		text string
		b    int
		want int
	}{
		{"abc", 0, 0}, {"abc", 2, 2}, {"abc", 3, 3}, {"abc", 99, 3},
		{"é世x", 2, 1}, {"é世x", 5, 2}, {"é世x", 3, 1}, // in the middle of 世: its start
		{"é世x", 100, 3}, {"", 0, 0}, {"", 5, 0},
	}
	for _, tc := range tests {
		if got := runeCol(tc.text, tc.b); got != tc.want {
			t.Errorf("runeCol(%q, %d) = %d, want %d", tc.text, tc.b, got, tc.want)
		}
	}
}

func TestDecorViewColMatchesRuneCol(t *testing.T) {
	doc := &fakeDoc{lines: []string{"hello", "é世x", ""}}
	v := &decorView{st: decorations.New(), id: 1, doc: doc}
	for line, text := range doc.lines {
		for b := -2; b <= len(text)+2; b++ {
			if got, want := v.col(line, b), runeCol(text, b); got != want {
				t.Errorf("col(%d, %d) over %q = %d, runeCol gives %d", line, b, text, got, want)
			}
		}
	}
}
