package find

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/tbxark/gvim/search"
)

// The find bar of a document in large file mode. Such a document is searched in
// slices of time by a scan that the root runs, so the bar cannot list the matches
// when the search text changes. It shows the scan's progress and count, and it goes
// to the next or the previous match through Streamer.Next, which stops at the first
// hit and does not build a list. See Streamer.

// searchingText is the status of the bar between a change of the query and the first
// report of the scan.
const searchingText = "Searching..."

// StreamInfo is what a scan of a large document has found so far. The root passes it
// to SetStream each time the scan makes progress.
type StreamInfo struct {
	// Progress is the part of the document searched, from 0 to 1.
	Progress float64
	// Count is the number of matches found so far.
	Count int
	// Done is set when the scan has ended. Matches then lists the matches in document
	// order.
	Done    bool
	Matches []search.Match
	// Capped says that the scan stopped at its match limit, so there are more matches
	// than Count.
	Capped bool
	// Partial says that the line index of the document was not complete when the scan
	// began, so only the indexed part was searched.
	Partial bool
	// Regex says that the query is a regular expression.
	Regex bool
	// Elapsed is how long the scan has been running.
	Elapsed time.Duration
	// Err is the error that stopped the scan, if any.
	Err error
}

// streamer returns the bar's editor as a Streamer when its document is searched in a
// stream.
func (b *Bar) streamer() (Streamer, bool) {
	st, ok := b.ed.(Streamer)
	if !ok || !st.Streaming() {
		return nil, false
	}
	return st, true
}

// Replacing reports whether a query-replace is asking about the matches.
func (b *Bar) Replacing() bool { return b.qr != nil }

// SetStream records the progress of the scan of the bar's query and shows it in
// the status. It does nothing while a query-replace asks about the matches, or when
// the search text is empty.
func (b *Bar) SetStream(info StreamInfo) {
	b.stream = &info
	if b.qr == nil && b.search.Value() != "" {
		b.status = b.streamStatus()
	}
}

// streamStatus is the status text of the bar for a scan of a large document: the
// progress while it runs, and the count once it is done.
func (b *Bar) streamStatus() string {
	s := b.stream
	switch {
	case s == nil:
		return searchingText
	case s.Err != nil:
		return s.Err.Error()
	case !s.Done:
		text := fmt.Sprintf("%s %d%% (%s matches so far", searchingText, int(s.Progress*100), group(s.Count))
		if s.Elapsed >= time.Second {
			text += fmt.Sprintf(", %ds", int(s.Elapsed.Seconds()))
		}
		text += ")"
		if s.Regex && s.Elapsed >= 3*time.Second {
			text += " regex search is about 50-300 MB/s"
		}
		return text
	}
	var text string
	switch {
	case s.Count == 0:
		text = search.StatusText(0, 0)
	case s.Capped:
		text = group(s.Count) + "+ matches"
	default:
		text = search.StatusText(b.currentIndex(s.Matches), len(s.Matches))
	}
	if s.Partial {
		text += PartialIndexText
	}
	return text
}

// currentIndex is the index in matches, which are in document order, of the match
// the bar has selected, or else of the first match at or after the anchor.
func (b *Bar) currentIndex(matches []search.Match) int {
	if b.hasSel {
		for i, m := range matches {
			if m.Start == b.selStart && m.End == b.selEnd {
				return i
			}
		}
	}
	idx, _ := search.Next(matches, b.anchor, true)
	return idx
}

// group writes n with a comma between each group of three digits.
func group(n int) string {
	s := strconv.Itoa(n)
	var sb strings.Builder
	for i, c := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			sb.WriteByte(',')
		}
		sb.WriteRune(c)
	}
	return sb.String()
}

// ErrSearchLimit is the error of Streamer.Next when the search read as much as it
// may in one go before it knew whether there is a match.
var ErrSearchLimit = errors.New("search limit reached")

// SearchLimitText is the status for ErrSearchLimit.
const SearchLimitText = "Search limit reached, repeat to continue"

// PartialIndexText is what a status says when only the part of a large document that
// the line index has reached was searched.
const PartialIndexText = " (line index not complete: only the part indexed was searched)"

// PartialIndexer is the optional part of Editor that tells whether the search of a
// large document can only reach the part its line index has covered so far. The root
// implements it.
type PartialIndexer interface {
	IndexPartial() bool
}

// noMatchText is the status for a search that found no match, or that was cut short.
// partial says that the line index of the document is not complete, so the answer is
// only for the part of the document that is indexed.
func noMatchText(err error, partial bool) string {
	if errors.Is(err, ErrSearchLimit) {
		return SearchLimitText
	}
	if err != nil {
		return err.Error()
	}
	text := search.StatusText(0, 0)
	if partial {
		text += PartialIndexText
	}
	return text
}

// partial reports whether a search of the bar's document reaches only the part its
// line index has covered.
func (b *Bar) partial() bool {
	p, ok := b.ed.(PartialIndexer)
	return ok && p.IndexPartial()
}

// commitStream is commit for a large document: it selects the first match at or
// after the anchor, unless F3 already selected one.
func (b *Bar) commitStream(st Streamer) {
	if !b.matched {
		m, found, err := st.Next(b.query(), b.anchor, true)
		if err != nil || !found {
			b.status = noMatchText(err, b.partial())
			return
		}
		b.selectMatch(m)
	}
	b.status = b.streamStatus()
	b.committed = true
	b.closed = true
}

// stepStream is step for a large document: it selects the match after the
// selection end, or the one before its start.
func (b *Bar) stepStream(st Streamer, forward bool) {
	start, end := b.bounds()
	from := end
	if !forward {
		from = start
	} else if b.matched && start == end {
		// The selected match is empty, so the one that starts here is the current one.
		from.Col++
	}
	m, found, err := st.Next(b.query(), from, forward)
	if err != nil || !found {
		b.status = noMatchText(err, b.partial())
		return
	}
	b.selectMatch(m)
	b.status = b.streamStatus()
}

// queryCurrentStream is queryCurrent for a large document. Next wraps around the
// document by itself, so a match that starts before the position the run looks
// from means the run went past the end, and the run stops where it began.
func (b *Bar) queryCurrentStream(st Streamer) (search.Match, bool, error) {
	qr := b.qr
	m, found, err := st.Next(b.query(), qr.from, true)
	if err != nil || !found {
		return search.Match{}, false, err
	}
	if m.Start.Less(qr.from) {
		qr.wrapped = true
	}
	if qr.wrapped && !m.Start.Less(qr.start) {
		return search.Match{}, false, nil
	}
	return m, true, nil
}
