package document

import (
	"github.com/tbxark/gvim/buffer"
)

// StringContext reports whether the character at p of d lies inside a string
// literal, as the syntax highlighter classifies it. The editor does not import the
// highlighter, so the app supplies this. Without it, quotes are never suppressed.
type StringContext func(d *Document, p buffer.Pos) bool

// pairFor returns the closer that typing ch inserts, for the languages Fresh
// pairs in (get_auto_close_char). Plain text pairs no quotes, and markdown no '.
func pairFor(ch rune, enabled bool, lang string) (rune, bool) {
	if !enabled {
		return 0, false
	}
	quote := ch == '"' || ch == '\'' || ch == '`'
	if lang == "text" && quote {
		return 0, false
	}
	if (lang == "markdown" || lang == "mdx") && ch == '\'' {
		return 0, false
	}
	switch ch {
	case '(':
		return ')', true
	case '[':
		return ']', true
	case '{':
		return '}', true
	case '"', '\'', '`':
		return ch, true
	}
	return 0, false
}

// isCloser reports whether ch is a closing bracket, the characters that re-indent a
// line as they are typed.
func isCloser(ch rune) bool { return ch == '}' || ch == ')' || ch == ']' }

// asciiWordRune is Fresh's test for "alphanumeric after the caret": auto-close does
// not fire in front of a word.
func asciiWordRune(r rune) bool {
	return r == '_' || (r >= '0' && r <= '9') || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')
}

// placeFn maps the end of an edit's inserted text, in the final document, to where
// the caret goes. Nil puts the caret at that end.
type placeFn func(end buffer.Pos) buffer.Pos

// EditPlaced makes one edit per caret and puts each caret where its place function
// says. With extra carets the edits are one undo step, as editAtCarets makes them.
// A single caret edits the buffer directly, so its typing still merges into the
// undo step of the typing around it. When the planned edits overlap, which two
// carets in one line's leading blanks can cause, plain gives the edits instead.
func (d *Document) EditPlaced(plan func(c caret) (edit, placeFn), plain func(c caret) edit) {
	cs := d.AllCarets()
	before := d.Cursor
	edits := make([]edit, len(cs))
	places := make([]placeFn, len(cs))
	for i, c := range cs {
		edits[i], places[i] = plan(c)
	}
	for i := 1; i < len(edits); i++ {
		if edits[i].From.Less(edits[i-1].To) {
			for j, c := range cs {
				edits[j], places[j] = plain(c), nil
			}
			break
		}
	}
	var ends []buffer.Pos
	if d.PlanErr != nil {
		// The planning failed (see failPlan): no edit is made.
		ends = make([]buffer.Pos, len(cs))
	} else if len(cs) == 1 {
		x := edits[0]
		if x.From != x.To {
			d.Buffer.Delete(x.From, x.To)
		}
		end := x.From
		if x.Text != "" && !d.editRefused() {
			// A refused delete leaves the text in place, so nothing is typed over it.
			end = d.Buffer.Insert(x.From, x.Text)
		}
		ends = []buffer.Pos{end}
	} else {
		ends = d.applyEdits(edits)
	}
	if d.editRefused() {
		return
	}
	out := make([]caret, len(cs))
	for i, c := range cs {
		p := ends[i]
		if places[i] != nil {
			p = places[i](p)
		}
		out[i] = caret{Anchor: p, Head: p, Primary: c.Primary}
	}
	d.SetCarets(out)
	if len(cs) > 1 {
		d.noteMultiEdit(before, edits)
	}
}

// maxIndentScan is how many runes of a line, from its start, count as its indent. A line
// with more blanks than this before the caret is not indented, as far as typing goes.
const maxIndentScan = 1024

// pairedBrackets reports whether open and closer are a matching ( ) [ ] or { }.
func pairedBrackets(open, closer rune) bool {
	return (open == '(' && closer == ')') || (open == '[' && closer == ']') || (open == '{' && closer == '}')
}

// pairedQuotes reports whether the two runes are a pair that auto-close inserts:
// brackets, or the same quote twice.
func pairedQuotes(open, closer rune) bool {
	return pairedBrackets(open, closer) ||
		((open == '"' || open == '\'' || open == '`') && open == closer)
}
