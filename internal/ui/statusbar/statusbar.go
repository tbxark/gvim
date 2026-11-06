// Package statusbar is the status row of gvim, the last row of the frame (spec
// section 3). On the left it shows the status message of the active tab, with
// the spinner while files load. In the centre it shows the path of the active
// document. On the right it shows the cursor position, the encoding, the line
// ending and the chroma language name.
//
// Runs is a pure function of the width and the content, and Paint and Hit both
// read it, so a label and its click target cannot drift apart. The right group
// is flush with the last column, and it drops items from its end when the row is
// narrow. The message keeps min(floor(2w/5), 40) cells before the right group
// is cut.
package statusbar

import (
	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/tbxark/gvim/internal/ui/grid"
	"github.com/tbxark/gvim/internal/ui/style"
)

// Config holds the labels that come from configuration rather than from the
// document. An empty field takes the default.
type Config struct {
	Loading string // message shown while files load and no message is set
}

// DefaultConfig is the configuration the status row uses by default.
func DefaultConfig() Config {
	return Config{Loading: "Loading files…"}
}

// Content is what the status row shows for the active document and editor state.
type Content struct {
	Message  string // the status message of the active tab, as the editor reports it
	Path     string // the document path for the centre group; empty when unnamed
	Position string // for example "Ln 1, Col 1"
	Encoding string // "ASCII" or "UTF-8"
	EOL      string // "LF", "CRLF"
	Language string // the chroma lexer name, for example "Go" or "Text"
}

// Kind names what a click on a run reaches. Each segment of the right-hand group
// has its own kind, so a click can open that segment's picker.
type Kind int

const (
	KindNone       Kind = iota // text only, or the gap
	KindPosition               // "Ln 1, Col 1": no picker
	KindEncoding               // "UTF-8" or "ASCII": opens the encoding picker
	KindLineEnding             // "LF": opens the line-ending picker
	KindLanguage               // the chroma lexer name: opens the language picker
)

// Run is a piece of status text at a fixed column, with what a click on it reaches.
type Run struct {
	X, W int // first column and width in cells
	Text string
	Kind Kind
}

// covers reports whether column x falls inside the run.
func (r Run) covers(x int) bool { return x >= r.X && x < r.X+r.W }

// Model is the status bar. It is a value type. Update returns the next state.
type Model struct {
	cfg     Config
	bar     lipgloss.Style
	spinner spinner.Model
	loading bool
}

// New returns a status bar that takes its labels from cfg and its colors from st.
// The spinner is plain text, so its frame can be placed in a cell run.
func New(cfg Config, st style.Styles) Model {
	cfg.Loading = orDefault(cfg.Loading, DefaultConfig().Loading)
	return Model{
		cfg:     cfg,
		bar:     st.Status,
		spinner: spinner.New(spinner.WithSpinner(spinner.Line), spinner.WithStyle(lipgloss.NewStyle())),
	}
}

// orDefault returns s, or def when s is empty.
func orDefault(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

// Loading reports whether the spinner is running.
func (m Model) Loading() bool { return m.loading }

// SetLoading starts or stops the loading state. Starting it returns the command
// that drives the spinner; stopping it returns nil, and the pending tick is
// dropped when it arrives.
func (m Model) SetLoading(on bool) (Model, tea.Cmd) {
	if on == m.loading {
		return m, nil
	}
	m.loading = on
	if on {
		return m, m.spinner.Tick
	}
	return m, nil
}

// Update advances the spinner on its tick messages. A tick after the loading
// state has ended is dropped, so the spinner stops.
func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	if _, ok := msg.(spinner.TickMsg); !ok {
		return m, nil
	}
	if !m.loading {
		return m, nil
	}
	var cmd tea.Cmd
	m.spinner, cmd = m.spinner.Update(msg)
	return m, cmd
}

// Runs lays out the status row for a w-cell row. The right group is flush with
// column w and drops items from its end until the message keeps at least
// min(floor(2w/5), 40) cells. The message is cut first to the room the right
// group leaves, and the path is centred in what is left between them.
func (m Model) Runs(w int, c Content) []Run {
	if w <= 0 {
		return nil
	}
	right := m.rightSegments(c)

	minLeft := min(2*w/5, 40)
	budget := w - (minLeft + 1)
	keep := 0
	if w >= 15 {
		keep = 1 // the position always stays while the row is readable
	}
	for rightWidth(right) > budget && len(right) > keep {
		right = right[:len(right)-1]
	}
	rightW := rightWidth(right)
	room := w - rightW

	var runs []Run
	message := m.messageText(c)
	msgW := 0
	if message != "" && room > 0 {
		pathW := ansi.StringWidth(pathText(c.Path))
		msgMax := room
		if pathW > 0 {
			// The path keeps the room it needs, and the message keeps at least half.
			msgMax = max(room-pathW, room/2)
		}
		msg := truncateCells(message, msgMax)
		msgW = ansi.StringWidth(msg)
		if msgW > 0 {
			runs = append(runs, Run{X: 0, W: msgW, Text: msg})
		}
	}

	if path := pathText(c.Path); path != "" {
		gapStart, gapEnd := msgW, w-rightW
		if gap := gapEnd - gapStart; gap > 0 {
			text := truncateLeftCells(path, gap)
			pw := ansi.StringWidth(text)
			x := min(max((w-pw)/2, gapStart), gapEnd-pw)
			runs = append(runs, Run{X: x, W: pw, Text: text})
		}
	}

	x := w - rightW
	for _, seg := range right {
		tw := ansi.StringWidth(seg.text)
		runs = append(runs, Run{X: x, W: tw, Text: seg.text, Kind: seg.kind})
		x += tw
	}
	return runs
}

// segment is one right-hand item and the kind a click on it reaches.
type segment struct {
	text string
	kind Kind
}

// messageText is the left group: the message, with the spinner frame in front of
// it while the loading state is on. It is empty when there is nothing to show.
func (m Model) messageText(c Content) string {
	msg := c.Message
	if m.loading {
		msg = orDefault(msg, m.cfg.Loading)
		msg = m.spinner.View() + " " + msg
	}
	if msg == "" {
		return ""
	}
	return " " + msg + " "
}

// pathText is the centre group for a path, or "" when the document is unnamed.
func pathText(path string) string {
	if path == "" {
		return ""
	}
	return " " + path + " "
}

// rightSegments returns the right-hand items in display order.
func (m Model) rightSegments(c Content) []segment {
	return []segment{
		{text: " " + c.Position + " ", kind: KindPosition},
		{text: " " + c.Encoding + " ", kind: KindEncoding},
		{text: " " + c.EOL + " ", kind: KindLineEnding},
		{text: " " + c.Language + " ", kind: KindLanguage},
	}
}

// rightWidth is the total width of the right-hand segments.
func rightWidth(right []segment) int {
	total := 0
	for _, seg := range right {
		total += ansi.StringWidth(seg.text)
	}
	return total
}

// truncateCells shortens s to at most w cells, ending with "…" when it is cut.
func truncateCells(s string, w int) string {
	if w <= 0 {
		return ""
	}
	if ansi.StringWidth(s) <= w {
		return s
	}
	return ansi.Truncate(s, max(w-1, 0), "") + "…"
}

// truncateLeftCells shortens s to at most w cells by dropping its start. The
// cut keeps the end of the text, which is the file name of a path, and starts
// with "…" when it is cut.
func truncateLeftCells(s string, w int) string {
	if w <= 0 {
		return ""
	}
	n := ansi.StringWidth(s)
	if n <= w {
		return s
	}
	return "…" + ansi.TruncateLeft(s, n-(w-1), "")
}

// Hit returns the run under column x of a w-cell row, or a KindNone run when no
// run covers the column.
func (m Model) Hit(w int, c Content, x int) Run {
	for _, r := range m.Runs(w, c) {
		if r.covers(x) {
			return r
		}
	}
	return Run{Kind: KindNone}
}

// Paint draws the status row on row y of g. Every run takes the bar style.
func (m Model) Paint(g *grid.Grid, y int, c Content) {
	bar := style.Cell(m.bar)
	g.Fill(0, g.Width(), y, bar)
	for _, r := range m.Runs(g.Width(), c) {
		g.Put(r.X, y, g.Width(), r.Text, bar)
	}
}
