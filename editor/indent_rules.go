package editor

import (
	"github.com/tbxark/gvim/buffer"
	"strings"
)

// SetNonCodeContext sets the query that masks comments and strings when the indent
// rules look at a line.
func (e *Editor) SetNonCodeContext(f NonCodeContext) { e.nonCode = f }

func (e *Editor) indentViewOf(d *Document, tab int) indentView {
	return indentView{Doc: d, Tab: max(tab, 1), Mask: e.nonCode}
}

// keywordDedent is Fresh's electric keyword dedent (handle_keyword_dedent). Typing ch
// at the end of a line, when it makes the line exactly leading blanks plus a decrease
// trigger (Python "else:", Ruby "end"), re-indents the line one level shallower. It
// fires only at the keystroke that completes the trigger, only dedents, and not under
// a line that opens a block. It returns the edit that replaces the text before the
// caret.
func (e *Editor) keywordDedent(d *Document, rules *indentRules, s LanguageSettings, tab int, p buffer.Pos, ch rune) (edit, bool) {
	line := []rune(d.Buffer.Line(p.Line))
	if p.Col > keywordDedentReach || p.Col > len(line) || hasText(line, p.Col, len(line)) {
		return edit{}, false
	}
	prefix := line[:p.Col]
	// The raw text has to be a trigger first, which spares the comment and string
	// lookup on nearly every keystroke.
	rawBefore := strings.ReplaceAll(string(prefix), "\r", "")
	if !rules.ConsumesLine(rawBefore+string(ch)) || rules.ConsumesLine(rawBefore) {
		return edit{}, false
	}
	v := e.indentViewOf(d, tab)
	before := v.Code(p.Line, line, 0, p.Col)
	if !rules.ConsumesLine(before+string(ch)) || rules.ConsumesLine(before) {
		return edit{}, false
	}
	target, ok := rules.OnTypeDedentTarget(v, p.Line)
	if !ok || target >= leadColumns(line, p.Col, v.Tab) {
		return edit{}, false
	}
	body := strings.TrimLeft(string(prefix), " \t")
	return edit{
		From: buffer.Pos{Line: p.Line},
		To:   p,
		Text: indentString(target, s.UseTabs, tab) + body + string(ch),
	}, true
}

// closerIndentFor returns the indent for a closing bracket ch typed at p: the rules
// of the language when they have a decrease rule for it, else the opener scan.
func (e *Editor) closerIndentFor(d *Document, rules *indentRules, p buffer.Pos, ch rune, tab int) int {
	if rules != nil {
		if w, ok := rules.DedentForDelimiter(e.indentViewOf(d, tab), p, ch); ok {
			return w
		}
	}
	return closerIndent(d.Buffer, p, tab)
}
