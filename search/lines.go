package search

import (
	"regexp"
	"strings"
	"time"

	"github.com/tbxark/gvim/buffer"
)

// Limits of FindLines.
const (
	// DefaultLinesMax is the number of lines FindLines returns when Max is 0 or
	// less.
	DefaultLinesMax = 10_000
	// DefaultLinesBudget is the time FindLines may take when Budget is 0 or
	// less, and MaxLinesBudget the most it may ever take.
	DefaultLinesBudget = 20 * time.Millisecond
	MaxLinesBudget     = 100 * time.Millisecond
)

// clockWork is the estimated work, in units of MaxRegexWork (about 6 ns each for
// a regular expression), after which FindLines reads the clock.
const clockWork = 100_000

// linesClock is the clock FindLines measures its budget with. Tests replace it.
var linesClock = time.Now

// LinesQuery is the argument of FindLines.
type LinesQuery struct {
	// Query is what to find. A line matches when the query matches inside the
	// line: a pattern never matches across lines, and ^ and $ are the line's ends.
	Query Query
	// From and To bound the lines scanned, [From, To). To of 0 or less means the
	// end of the document.
	From, To int
	// Max is the most lines returned, 0 or less meaning DefaultLinesMax.
	Max int
	// Budget is the time the scan may take, 0 or less meaning DefaultLinesBudget
	// and anything above MaxLinesBudget meaning MaxLinesBudget. The scan always
	// examines at least one line, so a caller that continues from Next makes
	// progress however small the budget.
	Budget time.Duration
}

// FindLines returns the 0-based numbers of the lines in the range that match
// the query, in order. It reads one line at a time, never the whole text, so it
// serves a large buffer too, and it stops when Max lines are found or the budget
// is spent. next is the first line not examined, to continue from; it is -1
// when the range was examined to its end. The index of a large buffer may still
// be growing: the scan then ends at the lines known now, with next at that line
// and not -1, as the lines to come are not examined yet.
//
// A regular expression is held to MaxRegexWork for each line, except one that is
// a plain string; a line too long for it fails the whole call with ErrTooCostly.
// An empty query matches nothing and the range is reported as examined.
func FindLines(b *buffer.Buffer, q LinesQuery) (lines []int, next int, err error) {
	match, cost, err := lineMatcher(q.Query)
	if err != nil {
		return nil, -1, err
	}
	limit := q.Max
	if limit <= 0 {
		limit = DefaultLinesMax
	}
	budget := q.Budget
	switch {
	case budget <= 0:
		budget = DefaultLinesBudget
	case budget > MaxLinesBudget:
		budget = MaxLinesBudget
	}
	total := b.Lines()
	end := total
	if q.To > 0 {
		end = min(q.To, total)
	}
	first := max(q.From, 0)
	if q.Query.Text == "" || first >= end {
		return nil, finished(b, end, max(first, end)), nil
	}
	deadline := linesClock().Add(budget)
	i := first
	// The clock is read every few lines, and whenever the lines since the last
	// reading may have cost more than clockWork: a line cannot be interrupted,
	// so a run of costly lines would otherwise overshoot the budget by far.
	var work int64
	for ; i < end; i++ {
		text := b.Line(i)
		work += cost(text)
		if i > first && (i%32 == 0 || work >= clockWork) {
			if !linesClock().Before(deadline) {
				break
			}
			work = 0
		}
		ok, err := match(text)
		if err != nil {
			return nil, -1, err
		}
		if ok {
			lines = append(lines, i)
			if len(lines) == limit {
				i++
				break
			}
		}
	}
	return lines, finished(b, end, i), nil
}

// finished returns the next line to continue from after the lines below i were
// examined in a scan that was to end at end: -1 when the range is done.
func finished(b *buffer.Buffer, end, i int) int {
	if i < end {
		return i
	}
	if !b.Index(0).Complete && end >= b.Lines() {
		return end
	}
	return -1
}

// lineMatcher returns a function that tells whether a line matches q, and one
// that estimates the work of matching a line, in the units of MaxRegexWork: the
// length of the line, times the size of the program for a regular expression
// that is not a plain string.
func lineMatcher(q Query) (match func(line string) (bool, error), cost func(line string) int64, err error) {
	length := func(line string) int64 { return int64(len(line)) }
	if q.Text == "" {
		return func(string) (bool, error) { return false, nil }, length, nil
	}
	if !q.Regex {
		ls := newLiteralSearch(q)
		return func(line string) (bool, error) {
			switch {
			case q.WholeWord:
				return len(ls.offsets(line, 1)) > 0, nil
			case q.CaseSensitive:
				return strings.Contains(line, q.Text), nil
			}
			ls.sc.start(line, 0)
			s, _ := ls.sc.next()
			return s >= 0, nil
		}, length, nil
	}
	re, err := compile(q)
	if err != nil {
		return nil, nil, err
	}
	if _, complete := re.LiteralPrefix(); complete {
		return func(line string) (bool, error) { return matchLine(re, q, line), nil }, length, nil
	}
	insts := int64(programSize(re))
	cost = func(line string) int64 { return int64(len(line)) * insts }
	return func(line string) (bool, error) {
		if cost(line) > MaxRegexWork {
			return false, ErrTooCostly
		}
		return matchLine(re, q, line), nil
	}, cost, nil
}

// matchLine reports whether re matches line, with a word boundary on both sides
// of the match when q asks for whole words.
func matchLine(re *regexp.Regexp, q Query, line string) bool {
	if !q.WholeWord {
		return re.MatchString(line)
	}
	for _, loc := range re.FindAllStringIndex(line, -1) {
		if isWholeWord(line, loc[0], loc[1]) {
			return true
		}
	}
	return false
}
