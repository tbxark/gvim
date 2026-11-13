package tui

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/internal/tui/grid"
	"github.com/tbxark/gvim/internal/tui/split"
	"github.com/tbxark/gvim/internal/tui/style"
)

// hexBytesPerRow is the number of bytes one hex row shows. The layout is the
// familiar offset | hex | ASCII form:
//
//	00000000  89 50 4E 47 0D 0A 1A 0A  00 00 00 0D 49 48 44 52  |.PNG........IHDR|
//
// Sixteen bytes keep the ASCII field readable in a normal 80-column pane.
const hexBytesPerRow = 16

// hexReadWindow is how much one range read fetches. It is aligned to the row
// size and bounded below the authority's 1 MiB answer cap, so scrolling over
// a huge binary loads a small window and never the whole file.
const hexReadWindow = 64 << 10

// hexPageRows is the default page size used when a movement action asks for a
// page but the pane is not laid out yet.
const hexPageRows = 10

// hexView is the per-document state of the read-only hex viewer. On is false
// while the document shows its summary; Top is the first visible byte row.
type hexView struct {
	on  bool
	top int

	data    []byte
	dataOff int64
	loading bool
	atEOF   bool
	err     error

	// gen invalidates reads that were in flight when the window moved or the
	// view closed. statSize/statMtime identify the file version the cache is for.
	gen       uint64
	statSize  int64
	statMtime int64
}

// hexWindowMsg is the bounded bytes a hex read returned. off is the window's
// start and gen the view generation that asked for it.
type hexWindowMsg struct {
	path  string
	off   int64
	gen   uint64
	data  []byte
	err   error
	atEOF bool
	size  int64
	mtime int64
}

// isBinaryDoc reports whether doc is a non-image binary file. The adapter has
// Binary on every document, so the method returns false for text.
func isBinaryDoc(doc Document) bool {
	if doc == nil {
		return false
	}
	b, ok := doc.(interface{ Binary() bool })
	if !ok || !b.Binary() {
		return false
	}
	return !isImageDoc(doc)
}

// hexForKey returns the per-path state, creating it on first use.
func (m *Model) hexForKey(path string) *hexView {
	if path == "" {
		return nil
	}
	if m.hexViews == nil {
		m.hexViews = make(map[string]*hexView)
	}
	h := m.hexViews[path]
	if h == nil {
		h = &hexView{}
		m.hexViews[path] = h
	}
	return h
}

// hexStateFor returns the hex state of path if it exists, without creating it.
func (m *Model) hexStateFor(path string) *hexView {
	if path == "" || m.hexViews == nil {
		return nil
	}
	return m.hexViews[path]
}

// activeHex returns the hex state of the active binary tab when the hex view
// is on, and reports whether it is.
func (m *Model) activeHex() (*hexView, bool) {
	doc := m.ed.Active()
	if !isBinaryDoc(doc) {
		return nil, false
	}
	path := docPath(doc)
	h := m.hexStateFor(path)
	if h == nil || !h.on {
		return nil, false
	}
	return h, true
}

// hexAtPane returns the hex state of the document shown in pane i when the
// pane is in hex mode. A split can show a text document beside a binary one,
// and only the binary pane handles a press as the pager rather than as text.
func (m *Model) hexAtPane(i int) (*hexView, bool) {
	l := m.layout()
	if i < 0 || i >= len(l.Panes) {
		return nil, false
	}
	doc := m.paneDoc(l.Panes[i])
	if !isBinaryDoc(doc) {
		return nil, false
	}
	h := m.hexStateFor(docPath(doc))
	if h == nil || !h.on {
		return nil, false
	}
	return h, true
}

// toggleHexView switches the active binary file between its summary and the hex
// dump. Text and images are left alone with a notice.
func (m *Model) toggleHexView() tea.Cmd {
	doc := m.ed.Active()
	if doc == nil {
		m.notice = noDocumentNotice
		return nil
	}
	if isImageDoc(doc) {
		m.notice = "Image files have no hex view: close the tab to reload the picture"
		return nil
	}
	if !isBinaryDoc(doc) {
		m.notice = "Hex view is available for binary files only"
		return nil
	}
	path := docPath(doc)
	if path == "" {
		m.notice = "Hex view needs a file on disk"
		return nil
	}
	h := m.hexForKey(path)
	if h.on {
		h.on = false
		h.gen++
		h.data = nil
		h.loading = false
		m.notice = "Binary summary"
		return nil
	}
	h.on = true
	h.loading = false
	h.atEOF = false
	h.err = nil
	h.data = nil
	h.dataOff = 0
	if h.top < 0 {
		h.top = 0
	}
	size, mtime := binaryStatOf(doc)
	h.statSize, h.statMtime = size, mtime
	h.gen++
	m.notice = ""
	return m.hexQueue()
}

// binaryStatOf returns the binary file's size and modification time, or zeros.
func binaryStatOf(doc Document) (size, mtime int64) {
	if b, ok := doc.(interface{ BinaryStat() (int64, int64) }); ok {
		return b.BinaryStat()
	}
	return 0, 0
}

// hexPaneVisibleRows returns the number of hex rows pane p has. The view uses
// the whole pane, less one help line at the bottom when there is room.
func hexPaneVisibleRows(p split.Pane) int {
	if p.H <= 0 {
		return hexPageRows
	}
	if p.H > 2 {
		return max(p.H-1, 1)
	}
	return max(p.H, 1)
}

// hexVisibleRows returns the number of rows the focused pane has for hex lines.
// Movement commands act on the focused pane.
func (m *Model) hexVisibleRows() int {
	return hexPaneVisibleRows(m.layout().FocusedPane())
}

// hexRowsForDoc returns the hex rows that must be available for doc across all
// panes: a split may show the same binary in panes of different heights, and
// the tallest one decides the read window.
func (m *Model) hexRowsForDoc(doc Document) int {
	if doc == nil {
		return m.hexVisibleRows()
	}
	return m.hexRowsForPath(docPath(doc))
}

// hexRowsForPath returns the hex rows a document must have available across all
// panes that show its path. A read result is keyed by path, not by document
// identity, so the fallback after a result finds every pane the same way.
func (m *Model) hexRowsForPath(path string) int {
	visible := 0
	if path != "" {
		for _, pane := range m.layout().Panes {
			doc := m.paneDoc(pane)
			if doc != nil && docPath(doc) == path {
				visible = max(visible, hexPaneVisibleRows(pane))
			}
		}
	}
	if visible == 0 {
		return m.hexVisibleRows()
	}
	return visible
}

// hexRows returns the number of complete hex rows in a file of size bytes.
func hexRows(size int64) int {
	if size <= 0 {
		return 0
	}
	return int((size + hexBytesPerRow - 1) / hexBytesPerRow)
}

// hexClampTop keeps the first visible row inside the file and leaves at least a
// full page reachable at the end. size is the file size from the open stat.
func (m *Model) hexClampTop(h *hexView, size int64, visible int) {
	rows := hexRows(size)
	maxTop := max(rows-visible, 0)
	h.top = min(max(h.top, 0), maxTop)
}

// hexMove scrolls the active hex view by delta rows. It preserves the row in
// view and queues a new bounded read only when the visible window changes.
func (m *Model) hexMove(delta int) bool {
	h, ok := m.activeHex()
	if !ok {
		return false
	}
	doc := m.ed.Active()
	size, _ := binaryStatOf(doc)
	h.top += delta
	m.hexClampTop(h, size, m.hexVisibleRows())
	return true
}

// hexAction handles the navigation actions while the active tab is in hex mode.
// It consumes movement and refuses actions that would edit or search the summary
// text behind the dump. ok is false for an action the normal editor should run.
func (m *Model) hexAction(action string) (tea.Cmd, bool) {
	if _, on := m.activeHex(); !on {
		return nil, false
	}
	visible := m.hexVisibleRows()
	switch action {
	case "move_up":
		m.hexMove(-1)
		return nil, true
	case "move_down":
		m.hexMove(1)
		return nil, true
	case "move_page_up":
		m.hexMove(-visible)
		return nil, true
	case "move_page_down":
		m.hexMove(visible)
		return nil, true
	case "scroll_up":
		m.hexMove(-wheelRows)
		return nil, true
	case "scroll_down":
		m.hexMove(wheelRows)
		return nil, true
	case "move_document_start":
		if h, ok := m.activeHex(); ok {
			h.top = 0
		}
		return nil, true
	case "move_document_end":
		if h, ok := m.activeHex(); ok {
			size, _ := binaryStatOf(m.ed.Active())
			m.hexClampTop(h, size, visible)
			h.top = max(hexRows(size)-visible, 0)
		}
		return nil, true
	case "toggle_hex_view":
		return m.toggleHexView(), true
	case "insert_char", "insert_newline", "insert_tab", "paste", "cut", "delete_backward",
		"delete_forward", "delete_word_backward", "delete_word_forward", "undo", "redo",
		"search", "find", "replace", "find_next", "find_previous", "goto_line",
		"select_all", "copy":
		m.notice = "Hex view is read-only: Toggle Hex View returns to the summary"
		return nil, true
	}
	return nil, false
}

// hexPaneWheel scrolls the hex view of pane i by delta rows. It reports whether
// that pane is showing a hex dump. Only the focused pane is scrolled; the other
// panes keep their own dump position.
func (m *Model) hexPaneWheel(i, delta int) bool {
	l := m.layout()
	if i < 0 || i >= len(l.Panes) {
		return false
	}
	p := l.Panes[i]
	if !p.Focused {
		return false
	}
	return m.hexMove(delta)
}

// hexFallbackKey maps an unbound key in hex mode to its pager action. It is a fallback
// for a keymap that does not bind a key; a bound key still goes through the
// keymap first.
func (m *Model) hexFallbackKey(kp KeyPress, text string) (tea.Cmd, bool) {
	if len(kp.Mods) != 0 {
		return nil, false
	}
	switch kp.Name {
	case "Up", "k":
		return m.hexAction("move_up")
	case "Down", "j":
		return m.hexAction("move_down")
	case "PageUp", "b":
		return m.hexAction("move_page_up")
	case "PageDown", " ":
		return m.hexAction("move_page_down")
	case "Home", "g":
		return m.hexAction("move_document_start")
	case "End", "G":
		return m.hexAction("move_document_end")
	}
	if text != "" {
		m.notice = "Hex view is read-only: Toggle Hex View returns to the summary"
		return nil, true
	}
	return nil, false
}

// hexScrollTo moves the hex view to the row clicked on the scrollbar track.
func (m *Model) hexScrollTo(i, y int) {
	h, ok := m.activeHex()
	if !ok {
		return
	}
	l := m.layout()
	if i < 0 || i >= len(l.Panes) {
		return
	}
	p := l.Panes[i]
	rows := m.hexVisibleRows()
	if p.H <= 0 || rows <= 1 {
		return
	}
	size, _ := binaryStatOf(m.ed.Active())
	total := hexRows(size)
	maxTop := max(total-rows, 0)
	if maxTop == 0 {
		return
	}
	local := y - p.Y
	h.top = min(max(local*maxTop/max(p.H-1, 1), 0), maxTop)
}

// hexWindowStart returns the aligned byte offset of a window that fully covers
// the visible rows.
func (m *Model) hexWindowStart(h *hexView) int64 {
	return int64(h.top) * hexBytesPerRow
}

// hexQueue starts the next bounded read for every visible hex pane. Reads are
// single-flight per document: a view that is loading waits for its result.
func (m *Model) hexQueue() tea.Cmd {
	if m.ed == nil || len(m.hexViews) == 0 {
		return nil
	}
	var cmds []tea.Cmd
	seen := make(map[string]bool)
	for _, pane := range m.layout().Panes {
		doc := m.paneDoc(pane)
		if doc == nil {
			continue
		}
		path := docPath(doc)
		if path == "" || seen[path] {
			continue
		}
		seen[path] = true
		h := m.hexStateFor(path)
		if h == nil || !h.on || h.loading || h.err != nil || !isBinaryDoc(doc) {
			continue
		}
		size, mtime := binaryStatOf(doc)
		if h.statSize != size || h.statMtime != mtime {
			// A reopened or replaced file invalidates the cached bytes, but the
			// view stays on and re-reads the first visible window.
			h.statSize, h.statMtime = size, mtime
			h.data = nil
			h.atEOF = false
			h.err = nil
		}
		if size <= 0 || h.atEOF && int64(h.top)*hexBytesPerRow >= size {
			continue
		}
		if _, ok := doc.(interface {
			ReadBinaryRange(context.Context, int64, int64) ([]byte, error)
		}); !ok {
			h.err = errors.New("hex view is not supported by this document")
			continue
		}
		start := m.hexWindowStart(h)
		visibleEnd := int64(h.top+m.hexRowsForDoc(doc)) * hexBytesPerRow
		needFrom, needTo := start, min(start+hexReadWindow, size)
		needTo = min(max(needTo, visibleEnd), size)
		if needFrom < h.dataOff || needTo > h.dataOff+int64(len(h.data)) ||
			(h.dataOff == 0 && len(h.data) == 0 && !h.atEOF) {
			h.loading = true
			h.gen++
			n := needTo - start
			cmds = append(cmds, m.hexReadCmd(doc, path, start, n, h.gen))
		}
	}
	return tea.Batch(cmds...)
}

// hexReadCmd reads a bounded window in a command, so the update loop never
// waits on a disk or an authority.
func (m *Model) hexReadCmd(doc Document, path string, off, n int64, gen uint64) tea.Cmd {
	reader, ok := doc.(interface {
		ReadBinaryRange(context.Context, int64, int64) ([]byte, error)
	})
	if !ok {
		return nil
	}
	return func() tea.Msg {
		data, err := reader.ReadBinaryRange(context.Background(), off, n)
		atEOF := errors.Is(err, io.EOF)
		if atEOF {
			err = nil
		}
		size, mtime := binaryStatOf(doc)
		if size > 0 && off+int64(len(data)) >= size {
			atEOF = true
		}
		return hexWindowMsg{path: path, off: off, gen: gen, data: data, err: err, atEOF: atEOF, size: size, mtime: mtime}
	}
}

// hexMessage applies a bounded hex read. A result from an older generation is
// dropped, so scrolling quickly never paints a stale window.
func (m *Model) hexMessage(msg tea.Msg) (tea.Cmd, bool) {
	w, ok := msg.(hexWindowMsg)
	if !ok {
		return nil, false
	}
	h := m.hexStateFor(w.path)
	if h == nil || !h.on || h.gen != w.gen {
		return nil, true
	}
	h.loading = false
	if w.err != nil {
		h.err = w.err
		return nil, true
	}
	h.err = nil
	h.data = w.data
	h.dataOff = w.off
	h.atEOF = w.atEOF
	h.statSize, h.statMtime = w.size, w.mtime
	// Queue another window when the visible rows extend past this one.
	visible := m.hexRowsForPath(w.path)
	visibleEnd := int64(h.top+visible) * hexBytesPerRow
	if !h.atEOF && visibleEnd > h.dataOff+int64(len(h.data)) {
		return m.hexQueue(), true
	}
	return nil, true
}

// hexReleaseClosed drops the state of hex views whose files are no longer open.
func (m *Model) hexReleaseClosed() {
	if len(m.hexViews) == 0 {
		return
	}
	open := make(map[string]bool)
	for _, f := range m.openTabFiles() {
		open[f.path] = true
	}
	for path := range m.hexViews {
		if !open[path] {
			delete(m.hexViews, path)
		}
	}
}

// hexLine formats one row of the dump. offset is the byte offset of the row and
// data its bytes (at most hexBytesPerRow).
func hexLine(offset int64, data []byte) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%08X  ", offset)
	for i := 0; i < hexBytesPerRow; i++ {
		if i == hexBytesPerRow/2 {
			b.WriteByte(' ')
		}
		if i < len(data) {
			fmt.Fprintf(&b, "%02X ", data[i])
		} else {
			b.WriteString("   ")
		}
	}
	b.WriteString(" |")
	for _, c := range data {
		if c >= 0x20 && c <= 0x7e {
			b.WriteByte(c)
		} else {
			b.WriteByte('.')
		}
	}
	for i := len(data); i < hexBytesPerRow; i++ {
		b.WriteByte(' ')
	}
	b.WriteByte('|')
	return b.String()
}

// hexPane paints the hex dump for doc in pane p. It returns false when the pane
// is not in hex mode or its document is not binary.
func (m *Model) hexPane(g *grid.Grid, p split.Pane, doc Document) bool {
	path := docPath(doc)
	if path == "" || !isBinaryDoc(doc) {
		return false
	}
	h := m.hexStateFor(path)
	if h == nil || !h.on {
		return false
	}
	cell := style.Cell(m.styles.Editor)
	dim := cell
	dim.Faint = true
	for y := 0; y < p.H; y++ {
		g.Fill(p.X, p.X+p.W, p.Y+y, cell)
	}
	visible := hexPaneVisibleRows(p)
	helpRow := -1
	if p.H > 2 {
		helpRow = p.Y + visible
	}
	start := int64(h.top) * hexBytesPerRow
	for row := 0; row < visible; row++ {
		off := start + int64(row)*hexBytesPerRow
		var bytes []byte
		if off >= h.dataOff && off < h.dataOff+int64(len(h.data)) {
			from := off - h.dataOff
			to := min(from+hexBytesPerRow, int64(len(h.data)))
			bytes = h.data[from:to]
		}
		text := hexLine(off, bytes)
		g.Put(p.X, p.Y+row, p.X+p.W, text, cell)
	}
	if helpRow >= 0 {
		help := "Toggle Hex View: summary"
		if h.loading {
			help = hexNoticeLoading
		} else if h.err != nil {
			help = hexNoticeError + h.err.Error()
		}
		g.Put(p.X, helpRow, p.X+p.W, help, dim)
	}
	return true
}

// hexNotice is the status-row hint while the active tab is in hex mode.
func (m *Model) hexNotice() string {
	h, ok := m.activeHex()
	if !ok {
		return ""
	}
	if h.err != nil {
		return hexNoticeError + h.err.Error()
	}
	if h.loading {
		return hexNoticeLoading
	}
	return hexNoticeReadOnly
}

const (
	hexNoticeLoading  = "Loading bytes..."
	hexNoticeReadOnly = "Hex view: read-only"
	hexNoticeError    = "Hex view error: "
)
