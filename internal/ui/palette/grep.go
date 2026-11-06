package palette

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
)

// Live Grep is the palette's text search over the workspace files. Its query is
// the whole query line, with no mode prefix, and its rows are the matching lines.
// Searching reads the files, so it runs in a tea.Cmd and its result comes back as
// a GrepResultMsg. The app adapter implements Grepper over internal/search.

// Grepper searches the text of the files, which are relative to root, for query.
// It returns at most limit hits, in file order, one per matching line. Only files
// that are text are searched. Search is called from a tea.Cmd, never from Update
// or Draw.
type Grepper interface {
	Search(root string, files []string, query string, limit int) ([]GrepHit, error)
}

// GrepHit is one matching line. Rel is the file relative to the root, Line and Col
// are the 1-based position of the match, and Text is the line.
type GrepHit struct {
	Rel       string
	Line, Col int
	Text      string
}

// GrepResultMsg carries the hits of one live grep query. The palette applies it
// only when Seq is the query it sent last. Text is the query that was searched.
type GrepResultMsg struct {
	Seq  int
	Text string
	Hits []GrepHit
	Err  error
}

// OpenLineMsg asks the root to open Path at Line and Col, both 1-based. It is
// sent when a live grep row is chosen, after the palette has closed.
type OpenLineMsg struct {
	Path      string
	Line, Col int
}

// grepLimit is how many hits a live grep query asks for: the palette's row cap,
// the same as the file mode's.
const grepLimit = fileLimit

// liveQuery is requery in Live Grep mode. An empty query has no rows and searches
// nothing. Any other query searches, and its rows arrive as a GrepResultMsg, so the
// rows on screen stay until then.
func (p *Palette) liveQuery(seq int, text string) tea.Cmd {
	if text == "" {
		p.rows = nil
		p.syncSize()
		p.nav.Select(0)
		return nil
	}
	return p.grepCmd(seq, text)
}

// grepCmd returns the command that searches the files for text. The file list is
// read and searched in the command, never in Update or Draw. It returns nil when no
// Grepper is wired.
func (p *Palette) grepCmd(seq int, text string) tea.Cmd {
	if p.opts.Grep == nil || text == "" {
		return nil
	}
	g, files, root := p.opts.Grep, p.opts.Files, p.opts.Root
	return func() tea.Msg {
		var list []string
		if files != nil {
			list, _ = files.Snapshot()
		}
		hits, err := g.Search(root, list, text, grepLimit)
		return GrepResultMsg{Seq: seq, Text: text, Hits: hits, Err: err}
	}
}

// applyGrep shows a live grep result when it answers the current query. The
// highlight goes to the top when the text changed.
func (p *Palette) applyGrep(msg GrepResultMsg) tea.Cmd {
	if msg.Seq != p.seq || !p.live {
		return nil
	}
	p.rows = grepRows(msg)
	p.syncSize()
	if msg.Text != p.shown {
		p.nav.Select(0)
	}
	p.shown = msg.Text
	return nil
}

// grepRows turns a result into rows: one row per hit, or one row with the error.
// Tabs in a hit's line become spaces, so the row keeps its columns.
func grepRows(msg GrepResultMsg) []row {
	if msg.Err != nil {
		return []row{{kind: rowMessage, name: "Live Grep: " + msg.Err.Error()}}
	}
	rows := make([]row, 0, len(msg.Hits))
	for _, h := range msg.Hits {
		text := strings.TrimSpace(strings.ReplaceAll(h.Text, "\t", " "))
		rows = append(rows, row{
			kind: rowGrep,
			name: fmt.Sprintf("%s:%d: %s", h.Rel, h.Line, text),
			rel:  h.Rel,
			line: h.Line,
			col:  h.Col,
		})
	}
	return rows
}
