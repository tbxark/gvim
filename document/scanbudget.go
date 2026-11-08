package document

import (
	"errors"
	"github.com/tbxark/gvim/buffer"
)

// A scan that walks the lines of a document from the caret, to find a bracket, a blank
// line, a word or an indent, reads as far as the answer is. In a large document that
// can be the whole file, so the scan has a budget: a count of lines and a count of
// bytes. When the budget is gone the scan stops, and the action says so. A document in
// memory has no budget.
const (
	// ScanLinesCap is the most lines one scan reads in a large document.
	ScanLinesCap = 10_000
	// ScanBytesBudget is the most bytes of line text one scan reads in a large document.
	ScanBytesBudget = 4 << 20
)

// ErrScanLimit is returned by an action whose scan stopped at the budget of a large
// document before it found its answer.
var ErrScanLimit = errors.New("no match within scan limit")

// scanBudget is what is left of the budget of one scan. A nil budget is unlimited.
type scanBudget struct {
	Lines, Bytes int
	// spent is set when take refused a line.
	Spent bool
}

// newScanBudget returns the budget of a scan of b: ScanLinesCap lines and
// ScanBytesBudget bytes for a large buffer, and nil, no limit, for a memory buffer.
func newScanBudget(b *buffer.Buffer) *scanBudget {
	if !b.Large() {
		return nil
	}
	return &scanBudget{Lines: ScanLinesCap, Bytes: ScanBytesBudget}
}

// take charges line of b to the budget before the line is read, and reports whether
// the scan may read it. The charge is the stored length of the line, which the buffer
// knows without reading it. A refused scan stays refused.
func (s *scanBudget) Take(b *buffer.Buffer, line int) bool {
	if s == nil {
		return true
	}
	if s.Spent {
		return false
	}
	n := b.LineBytes(line)
	if s.Lines <= 0 || s.Bytes < n {
		s.Spent = true
		return false
	}
	s.Lines--
	s.Bytes -= n
	return true
}

// exhausted reports whether the budget refused a line.
func (s *scanBudget) Exhausted() bool { return s != nil && s.Spent }

// indexUnfinished reports whether b is a large buffer whose line index is incomplete,
// so that a scan that ran to its last line has not seen the end of the document.
func indexUnfinished(b *buffer.Buffer) bool {
	return b.Large() && !b.Index(0).Complete
}
