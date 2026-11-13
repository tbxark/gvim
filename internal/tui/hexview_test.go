package tui

import (
	"context"
	"errors"
	"io"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/internal/tui/frame"
	"github.com/tbxark/gvim/internal/tui/grid"
	"github.com/tbxark/gvim/internal/tui/split"
)

// hexFakeDoc is a fakeDoc that also carries the binary hex surface. It does not
// touch an editor or a real file.
type hexFakeDoc struct {
	*fakeDoc
	binary  bool
	image   bool
	path    string
	size    int64
	data    []byte
	reads   []int64
	readErr error
}

func (d *hexFakeDoc) Binary() bool                    { return d.binary }
func (d *hexFakeDoc) IsImage() bool                   { return d.image }
func (d *hexFakeDoc) BinaryStat() (size, mtime int64) { return d.size, 0 }
func (d *hexFakeDoc) ReadBinaryRange(_ context.Context, off, n int64) ([]byte, error) {
	d.reads = append(d.reads, off)
	if d.readErr != nil {
		return nil, d.readErr
	}
	if off >= int64(len(d.data)) {
		return nil, io.EOF
	}
	end := min(off+n, int64(len(d.data)))
	out := append([]byte(nil), d.data[off:end]...)
	if end < off+n {
		return out, io.EOF
	}
	return out, nil
}

func (d *hexFakeDoc) FilePath() string { return d.path }

// newHexModel returns a model whose active document is a binary file. The fake
// editor keeps the document and tab shape the other TUI tests use.
func newHexModel(t *testing.T, data []byte) (*Model, *fakeEditor, *hexFakeDoc) {
	t.Helper()
	ed := newFakeEditor("summary")
	path := filepath.Join(t.TempDir(), "blob.bin")
	doc := &hexFakeDoc{
		fakeDoc: ed.doc,
		binary:  true,
		path:    path,
		size:    int64(len(data)),
		data:    data,
	}
	ed.docAlt = doc
	m := newTestModel(t, ed, nil, 80, 12)
	return m, ed, doc
}

// TestHexLineLayout checks the documented 16-byte layout: offset, hex grouped
// after eight bytes, then the printable ASCII field.
func TestHexLineLayout(t *testing.T) {
	t.Parallel()
	data := []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a, 0, 0, 0, 0x0d, 'I', 'H', 'D', 'R'}
	got := hexLine(0, data)
	want := "00000000  89 50 4E 47 0D 0A 1A 0A  00 00 00 0D 49 48 44 52  |.PNG........IHDR|"
	if got != want {
		t.Fatalf("hexLine = %q\nwant      %q", got, want)
	}
	short := hexLine(16, []byte("abc"))
	if !strings.HasPrefix(short, "00000010  61 62 63") || !strings.HasSuffix(short, "|abc             |") {
		t.Fatalf("short row = %q", short)
	}
}

// TestHexToggleRejectsTextAndImages checks that only a non-image binary may
// enter the dump. The image check comes first because images report Binary too.
func TestHexToggleRejectsTextAndImages(t *testing.T) {
	t.Parallel()
	m := newTestModel(t, newFakeEditor("hello"), nil, 80, 24)
	if cmd := m.toggleHexView(); cmd != nil {
		t.Fatal("toggle text returned a command")
	}
	if !strings.Contains(m.notice, "binary files only") {
		t.Errorf("text notice = %q", m.notice)
	}

	m, _, doc := newHexModel(t, []byte{0})
	doc.image = true
	if cmd := m.toggleHexView(); cmd != nil {
		t.Fatal("toggle image returned a command")
	}
	if !strings.Contains(m.notice, "Image files") {
		t.Errorf("image notice = %q", m.notice)
	}
}

// TestHexToggleLoadsAWindowAndDrawsTheDump checks the state transition and the
// first paint. The dump's first row carries the offset, bytes and ASCII field.
func TestHexToggleLoadsAWindowAndDrawsTheDump(t *testing.T) {
	t.Parallel()
	m, _, doc := newHexModel(t, []byte("ABC"))
	cmd := m.toggleHexView()
	if cmd == nil {
		t.Fatal("toggle did not queue a range read")
	}
	if h := m.hexStateFor(doc.path); h == nil || !h.on {
		t.Fatal("the hex view is not on")
	}
	msg := cmd()
	if _, ok := msg.(hexWindowMsg); !ok {
		t.Fatalf("range command returned %T", msg)
	}
	m.Update(msg)
	g := grid.New(70, 6)
	p := split.Pane{Focused: true, X: 0, Y: 0, W: 70, H: 6}
	if !m.hexPane(g, p, doc) {
		t.Fatal("hexPane = false")
	}
	var line strings.Builder
	for x := 0; x < 70; x++ {
		line.WriteString(g.At(x, 0).Text)
	}
	if !strings.HasPrefix(strings.TrimSpace(line.String()), "00000000  41 42 43") {
		t.Fatalf("hex row = %q", line.String())
	}
}

// TestHexSmallFileIsReadOnce checks that a file shorter than the read window is
// not requested again after its first window was loaded. The whole file is in
// cache, so a repeated queue would waste the reader and flash the loading row.
func TestHexSmallFileIsReadOnce(t *testing.T) {
	t.Parallel()
	data := []byte("ABC")
	m, _, doc := newHexModel(t, data)
	cmd := m.toggleHexView()
	if cmd == nil {
		t.Fatal("toggle did not queue the first read")
	}
	m.Update(cmd())
	if got := len(doc.reads); got != 1 {
		t.Fatalf("reads after the first window = %d, want 1", got)
	}
	if cmd := m.hexQueue(); cmd != nil {
		t.Fatal("hexQueue queued a second read for a fully cached small file")
	}
	if got := len(doc.reads); got != 1 {
		t.Fatalf("reads after the second queue = %d, want 1", got)
	}
}

// TestHexCacheIsReplacedWhenTheReadWindowMoves checks the cache rule after a
// scroll: moving the top row off the cached window asks for the new rows once,
// and a result that still covers the visible rows does not ask again.
func TestHexCacheIsReplacedWhenTheReadWindowMoves(t *testing.T) {
	t.Parallel()
	rows := hexReadWindow/hexBytesPerRow + 50
	m, _, doc := newHexModel(t, make([]byte, hexBytesPerRow*rows))
	cmd := m.toggleHexView()
	if cmd == nil {
		t.Fatal("toggle did not queue the first read")
	}
	m.Update(cmd())
	doc.reads = nil
	m.hexMove(hexReadWindow / hexBytesPerRow)
	cmd = m.hexQueue()
	if cmd == nil {
		t.Fatal("moving past the cached window queued no read")
	}
	m.Update(cmd())
	if got := len(doc.reads); got != 1 {
		t.Fatalf("reads after moving once = %d, want 1", got)
	}
	if cmd := m.hexQueue(); cmd != nil {
		t.Fatal("hexQueue asked again although the new window covers the visible rows")
	}
}

// TestHexReadDeterminesEOFForSmallFiles checks the document reader itself: a
// short read that reaches the file end is io.EOF, and the UI turns that into the
// atEOF flag instead of asking for another window forever.
func TestHexReadDeterminesEOFForSmallFiles(t *testing.T) {
	t.Parallel()
	doc := &hexFakeDoc{
		fakeDoc: &fakeDoc{},
		binary:  true,
		path:    filepath.Join(t.TempDir(), "tiny.bin"),
		size:    3,
		data:    []byte("abc"),
	}
	msg, ok := hexReadWindowForTest(doc, doc.path, 0, hexReadWindow)
	if !ok {
		t.Fatal("hexReadWindowForTest returned no message")
	}
	if !msg.atEOF {
		t.Fatalf("atEOF = false for a %d-byte file, want true", doc.size)
	}
}

// hexReadWindowForTest runs the read command without a Model, so the EOF rule
// of hexReadCmd is checked at its own boundary.
func hexReadWindowForTest(doc Document, path string, off, n int64) (hexWindowMsg, bool) {
	m := &Model{}
	cmd := m.hexReadCmd(doc, path, off, n, 1)
	if cmd == nil {
		return hexWindowMsg{}, false
	}
	msg, ok := cmd().(hexWindowMsg)
	return msg, ok
}

// TestHexMoveClampsToTheFile checks that navigation cannot scroll past the
// last row or above the first.
func TestHexMoveClampsToTheFile(t *testing.T) {
	t.Parallel()
	m, _, doc := newHexModel(t, make([]byte, hexBytesPerRow*4))
	m.toggleHexView()
	h := m.hexStateFor(doc.path)
	h.top = 99
	m.hexMove(0)
	if h.top != 0 {
		t.Fatalf("top = %d, want 0 for a four-row file in a nine-row pane", h.top)
	}
	h.top = 1
	m.hexMove(-5)
	if h.top != 0 {
		t.Fatalf("top after moving above = %d, want 0", h.top)
	}
}

// TestHexReadResultGenerationDropsStaleWindows checks that a result from an
// older scroll position cannot replace the current window.
func TestHexReadResultGenerationDropsStaleWindows(t *testing.T) {
	t.Parallel()
	m, _, doc := newHexModel(t, make([]byte, hexBytesPerRow*100))
	m.toggleHexView()
	h := m.hexStateFor(doc.path)
	stale := hexWindowMsg{path: doc.path, off: 0, gen: h.gen - 1, data: []byte("stale")}
	if cmd, taken := m.hexMessage(stale); !taken || cmd != nil {
		t.Fatalf("stale result was not dropped: cmd=%v taken=%v", cmd, taken)
	}
	if len(h.data) != 0 {
		t.Fatalf("stale result stored data %q", h.data)
	}
}

// TestHexReadErrorsAreVisible checks that a range failure is kept as an error
// instead of showing zeroes as if they were bytes.
func TestHexReadErrorsAreVisible(t *testing.T) {
	t.Parallel()
	m, _, doc := newHexModel(t, []byte{1})
	doc.readErr = errors.New("authority disconnected")
	cmd := m.toggleHexView()
	if cmd == nil {
		t.Fatal("no read command was queued")
	}
	m.Update(cmd())
	h := m.hexStateFor(doc.path)
	if h.err == nil || !strings.Contains(h.err.Error(), "authority disconnected") {
		t.Fatalf("hex error = %v", h.err)
	}
	if got := m.hexNotice(); !strings.Contains(got, "authority disconnected") {
		t.Fatalf("hex notice = %q", got)
	}
}

// TestHexHiddenCaret checks that the terminal cursor is hidden in the dump.
func TestHexHiddenCaret(t *testing.T) {
	t.Parallel()
	m, _, doc := newHexModel(t, []byte{1})
	m.toggleHexView()
	if _, _, ok := m.caret(); ok {
		t.Fatalf("caret is visible in hex mode for %s", doc.path)
	}
}

// TestHexNavigationActions checks that the pager actions move the hex window
// and never reach the editor.
func TestHexNavigationActions(t *testing.T) {
	t.Parallel()
	m, ed, _ := newHexModel(t, make([]byte, hexBytesPerRow*200))
	m.toggleHexView()
	ed.actions = nil
	for _, action := range []string{"move_down", "move_down", "move_page_down", "scroll_down", "move_document_end", "move_up"} {
		if cmd, taken := m.hexAction(action); !taken || cmd != nil {
			t.Errorf("hexAction(%q) = (%v, %v), want handled without a command", action, cmd, taken)
		}
	}
	if len(ed.actions) != 0 {
		t.Fatalf("hex navigation reached the editor: %v", ed.actions)
	}
}

// TestHexQueueReadsEnoughForTheTallestPane checks a split that shows one binary
// tab in two panes of different heights. The read window must cover the taller
// pane, including when it is not the focused one.
func TestHexQueueReadsEnoughForTheTallestPane(t *testing.T) {
	t.Parallel()
	m, ed, doc := newHexModel(t, make([]byte, hexBytesPerRow*200))
	cmd := m.toggleHexView()
	if cmd == nil {
		t.Fatal("toggle did not queue a read")
	}
	m.Update(cmd())

	// Both panes show tab 0. The unfocused pane is taller than the focused one.
	root := &split.Node{
		Dir: split.Stacked,
		A:   &split.Node{Tab: 0},
		B:   &split.Node{Tab: 0},
	}
	m.splits.Root = root
	m.splits.Focus = root.B
	ed.docAlt = doc
	l := m.layout()
	if len(l.Panes) != 2 {
		t.Fatalf("panes = %d, want 2", len(l.Panes))
	}
	tallest := max(hexPaneVisibleRows(l.Panes[0]), hexPaneVisibleRows(l.Panes[1]))
	h := m.hexStateFor(doc.path)
	h.data = nil
	h.dataOff = 0
	h.atEOF = false
	cmd = m.hexQueue()
	if cmd == nil {
		t.Fatal("the split queued no read")
	}
	msg := cmd()
	w, ok := msg.(hexWindowMsg)
	if !ok {
		t.Fatalf("read returned %T, want hexWindowMsg", msg)
	}
	if want := int64(tallest * hexBytesPerRow); int64(len(w.data)) < want {
		t.Fatalf("window = %d bytes, want at least %d for the tallest pane", len(w.data), want)
	}
}

// TestHexMessageQueuesForTheTallestPaneOfABackgroundDocument checks the fallback
// after a read result: when the result belongs to a document the editor did not
// activate, the window is still sized for the tallest pane that shows its path.
func TestHexMessageQueuesForTheTallestPaneOfABackgroundDocument(t *testing.T) {
	t.Parallel()
	data := make([]byte, hexBytesPerRow*400)
	m, ed, doc := newHexModel(t, data)
	cmd := m.toggleHexView()
	if cmd == nil {
		t.Fatal("toggle did not queue a read")
	}
	m.Update(cmd())

	// A split shows the binary tab in the unfocused, taller pane while the
	// focused pane and the editor's active document show plain text.
	text := &fakeDoc{lines: []string{"notes"}}
	ed.docAlt = text
	ed.tabs = []TabInfo{
		{Name: doc.path, Path: doc.path},
		{Name: "notes.txt", Path: "notes.txt", Active: true},
	}
	ed.tabDocs = []Document{doc, text}
	root := &split.Node{
		Dir:   split.Stacked,
		Ratio: 85,
		A:     &split.Node{Tab: 0},
		B:     &split.Node{Tab: 1},
	}
	m.splits.Root = root
	m.splits.Focus = root.B
	if got := m.paneDoc(m.layout().Panes[0]); got != doc {
		t.Fatalf("the tall pane shows %T, want the binary document", got)
	}
	tallest := hexPaneVisibleRows(m.layout().Panes[0])
	if tallest <= hexPaneVisibleRows(m.layout().Panes[1]) {
		t.Fatalf("tallest pane has %d rows, want more than the focused pane", tallest)
	}

	h := m.hexStateFor(doc.path)
	h.data = data[:hexBytesPerRow]
	h.dataOff = 0
	h.atEOF = false
	h.top = 0
	msg := hexWindowMsg{
		path: doc.path,
		off:  0,
		gen:  h.gen,
		data: data[:hexBytesPerRow],
		size: int64(len(data)),
	}
	next, taken := m.hexMessage(msg)
	if !taken || next == nil {
		t.Fatalf("hexMessage = (%v, %v), want another read for the taller pane", next, taken)
	}
	if !h.loading {
		t.Fatal("the follow-up read was not queued")
	}
}

// TestHexClickFocusesThePressedPane checks that the pager follows the pane a
// press lands in, not the tab the editor happened to activate last. A split can
// show one document in both panes; clicking the other pane must focus it so the
// wheel and the pager keys act there.
func TestHexClickFocusesThePressedPane(t *testing.T) {
	t.Parallel()
	m, ed, doc := newHexModel(t, make([]byte, hexBytesPerRow*200))
	cmd := m.toggleHexView()
	if cmd == nil {
		t.Fatal("toggle did not queue a read")
	}
	m.Update(cmd())

	root := &split.Node{
		Dir: split.SideBySide,
		A:   &split.Node{Tab: 0},
		B:   &split.Node{Tab: 0},
	}
	m.splits.Root = root
	m.splits.Focus = root.B
	ed.docAlt = doc
	ps := m.layout().Panes
	if len(ps) != 2 {
		t.Fatalf("panes = %d, want 2", len(ps))
	}

	m.Update(tea.MouseClickMsg(tea.Mouse{X: ps[0].TextX + 1, Y: ps[0].Y + 1, Button: tea.MouseLeft}))
	if got := m.layout().Focus; got != 0 {
		t.Fatalf("focus = %d after a press in pane 0, want 0", got)
	}
	if got := m.splits.Focus; got != root.A {
		t.Fatalf("focused leaf = %p, want the left pane %p", got, root.A)
	}
}

// TestHexNoticeReachesTheStatusRow checks the failure of a range read is shown
// on the status row, not only kept in the internal state.
func TestHexNoticeReachesTheStatusRow(t *testing.T) {
	t.Parallel()
	m, _, doc := newHexModel(t, []byte{1})
	doc.readErr = errors.New("authority disconnected")
	if cmd := m.toggleHexView(); cmd == nil {
		t.Fatal("no read command was queued")
	}
	// The tool command sets its own "Hex view: read-only" notice after the
	// command is dispatched; the error must win once the read lands.
	m.notice = ""
	h := m.hexStateFor(doc.path)
	h.loading = false
	cmd := m.hexQueue()
	if cmd == nil {
		t.Fatal("the failing read was not queued")
	}
	m.Update(cmd())
	if got := m.statusView().Message; !strings.Contains(got, "authority disconnected") {
		t.Fatalf("status message = %q, want the hex error", got)
	}
}

// TestHexStatusButtonSwitchesBothWays checks the status-row shortcut: a binary tab
// shows [Hex] above the summary, a click opens the dump and turns the chip into
// [Summary], and clicking that returns to the summary. Text and image tabs have no
// binary switch.
func TestHexStatusButtonSwitchesBothWays(t *testing.T) {
	t.Parallel()
	m, ed, doc := newHexModel(t, []byte("ABC"))
	if got := m.statusView().HexView; got != "Hex" {
		t.Fatalf("summary chip = %q, want Hex", got)
	}
	l := m.layout()
	r := statusRun(t, m, frame.StatusSegmentHexView)
	if r.Text != " [Hex] " {
		t.Fatalf("summary status run = %+v, want the [Hex] button", r)
	}
	if got := l.Hit(r.X+1, l.StatusY); got.Kind != frame.HitStatusSegment || got.Index != frame.StatusSegmentHexView {
		t.Fatalf("summary button hit = %+v, want the hex switch", got)
	}
	_, cmd := m.Update(tea.MouseClickMsg(tea.Mouse{X: r.X + 1, Y: l.StatusY, Button: tea.MouseLeft}))
	if cmd == nil {
		t.Fatal("the summary button queued no hex read")
	}
	if got := m.statusView().HexView; got != "Summary" {
		t.Fatalf("hex chip = %q, want Summary", got)
	}
	r = statusRun(t, m, frame.StatusSegmentHexView)
	if r.Text != " [Summary] " {
		t.Fatalf("hex status run = %+v, want the [Summary] button", r)
	}
	clickAt(m, r.X+1, m.layout().StatusY)
	if h := m.hexStateFor(doc.path); h == nil || h.on {
		t.Fatalf("hex state after the second click = %+v, want the summary", h)
	}
	if got := m.statusView().HexView; got != "Hex" {
		t.Fatalf("summary chip after the second click = %q, want Hex", got)
	}

	ed.docAlt = newFakeEditor("text").doc
	if got := m.statusView().HexView; got != "" {
		t.Errorf("text document chip = %q, want none", got)
	}
}

// TestHexPaneReservesTheHelpRowForShortPanes checks the paint and the read
// agree on the row count of a one- or two-row pane.
func TestHexPaneReservesTheHelpRowForShortPanes(t *testing.T) {
	t.Parallel()
	if got := hexPaneVisibleRows(split.Pane{H: 1}); got != 1 {
		t.Errorf("one-row pane = %d, want 1", got)
	}
	if got := hexPaneVisibleRows(split.Pane{H: 2}); got != 2 {
		t.Errorf("two-row pane = %d, want 2 (no help row)", got)
	}
	if got := hexPaneVisibleRows(split.Pane{H: 5}); got != 4 {
		t.Errorf("five-row pane = %d, want 4 (one help row)", got)
	}
}
