package editor

import (
	"github.com/tbxark/gvim/buffer"
	"unicode/utf8"
)

// SetStringContext sets the query that decides whether a typed quote is inside a
// string. Typing a quote there inserts only the quote, as Fresh does.
func (e *Editor) SetStringContext(f StringContext) { e.inString = f }

// langOf returns the language id that the quote and markdown exceptions test: the
// id of the settings, where an unknown language reads as plain text once a resolver
// is installed (Fresh names every buffer without a detected language "text").
func (e *Editor) langOf(s LanguageSettings) string {
	if s.Language == "" && e.languages != nil {
		return "text"
	}
	return s.Language
}

// typeText types text at every caret. A single rune goes through the smart rules
// of the document's language settings. Longer text, which a paste or a plugin
// sends, is inserted as it is.
func (e *Editor) typeText(d *Document, text string) {
	// Typing at a virtual caret first turns the gap into real spaces, in the same
	// undo step as the text (the buffer keeps one group until the next Break).
	d.MaterializeVirtual()
	plain := func(c caret) edit { return replaceEdit(c, text) }
	r, size := utf8.DecodeRuneInString(text)
	s := e.SettingsFor(d)
	rules := indentRulesOf(s)
	// A language with a decrease rule watches every typed character for the keyword
	// that completes a dedent trigger.
	electric := s.AutoIndent && rules != nil && rules.Decrease != nil
	smart := size == len(text) && (s.AutoClose || s.AutoSurround || (s.AutoIndent && isCloser(r)) || electric)
	if !smart {
		if len(d.Extra) == 0 {
			d.InsertText(text)
			return
		}
		d.EditAtCarets(plain)
		return
	}
	tab := e.tabSizeOf(d)
	d.EditPlaced(func(c caret) (edit, placeFn) { return e.planChar(d, s, rules, tab, c, r) }, plain)
}

// planChar plans typing ch at one caret, in Fresh's order: wrap a selection, step
// over a closer that is already there, fix the indent of a typed closer, dedent a
// line that the character makes a trigger keyword, close a typed opener, or type the
// character.
func (e *Editor) planChar(d *Document, s LanguageSettings, rules *indentRules, tab int, c caret, ch rune) (edit, placeFn) {
	lo, hi := c.Bounds()
	lang := e.langOf(s)
	text := string(ch)
	if lo != hi {
		if closer, ok := pairFor(ch, s.AutoSurround, lang); ok {
			return edit{From: lo, To: hi, Text: text + d.PlanRange(lo, hi) + string(closer)}, nil
		}
	}
	after, hasAfter := d.Buffer.RuneAt(hi)
	// Only the text before the caret matters here, and only while it can be indent, so a
	// very long line is not read whole for every character typed on it.
	line := []rune(d.Buffer.LineSlice(lo.Line, 0, min(lo.Col, maxIndentScan)))
	atIndent := lo.Col > 0 && lo.Col <= maxIndentScan && blankPrefix(line, lo.Col)
	step := func(end buffer.Pos) buffer.Pos { return buffer.Pos{Line: end.Line, Col: end.Col + 1} }

	if s.AutoClose && hasAfter && after == ch && (ch != '\'' || (lang != "markdown" && lang != "mdx")) {
		switch ch {
		case ')', ']', '}', '"', '\'', '`':
			if isCloser(ch) && atIndent {
				// Fix the indent of the line, then step over the closer.
				want := e.closerIndentFor(d, rules, lo, ch, tab)
				if leadColumns(line, lo.Col, tab) != want {
					return edit{From: buffer.Pos{Line: lo.Line}, To: hi, Text: indentString(want, s.UseTabs, tab)}, step
				}
			}
			return edit{From: lo, To: hi}, step
		}
	}
	if isCloser(ch) && s.AutoIndent && atIndent {
		want := e.closerIndentFor(d, rules, lo, ch, tab)
		return edit{From: buffer.Pos{Line: lo.Line}, To: hi, Text: indentString(want, s.UseTabs, tab) + text}, nil
	}
	if s.AutoIndent && rules != nil && lo == hi && !isCloser(ch) {
		if x, ok := e.keywordDedent(d, rules, s, tab, lo, ch); ok {
			return x, nil
		}
	}
	if closer, ok := pairFor(ch, s.AutoClose, lang); ok {
		quote := ch == '"' || ch == '\'' || ch == '`'
		inside := quote && e.inString != nil && e.inString(d, lo)
		if !inside && (!hasAfter || !asciiWordRune(after)) {
			back := func(end buffer.Pos) buffer.Pos { return buffer.Pos{Line: end.Line, Col: end.Col - 1} }
			return edit{From: lo, To: hi, Text: text + string(closer)}, back
		}
	}
	return edit{From: lo, To: hi, Text: text}, nil
}

// newline breaks the line at every caret, replacing each selection. With
// auto_indent the new line gets the indent Fresh's heuristic gives it, and with
// auto_close a caret between a bracket pair opens a three-line block with the
// closer on its own line.
func (e *Editor) newline(d *Document) {
	// Enter at a virtual caret is a line break at the real end of the line: the
	// gap is not typed out (Fresh does the same).
	d.ClearVirtual()
	s := e.SettingsFor(d)
	tab := e.tabSizeOf(d)
	plain := func(c caret) edit { return replaceEdit(c, "\n") }
	if !s.AutoIndent {
		if len(d.Extra) == 0 {
			d.DeleteSelection()
			d.InsertText("\n")
			return
		}
		d.EditAtCarets(plain)
		return
	}
	plainText := e.langOf(s) == "text"
	rules := indentRulesOf(s)
	d.EditPlaced(func(c caret) (edit, placeFn) {
		lo, hi := c.Bounds()
		width := 0
		if rules != nil {
			width = rules.NewLineIndent(e.indentViewOf(d, tab), lo)
		} else {
			width = newLineIndent(d.Buffer, lo, tab, plainText)
		}
		indent := indentString(width, s.UseTabs, tab)
		text := "\n" + indent
		before, hasBefore := d.Buffer.RuneAt(buffer.Pos{Line: lo.Line, Col: lo.Col - 1})
		after, hasAfter := d.Buffer.RuneAt(lo)
		if !s.AutoClose || !hasBefore || !hasAfter || !pairedBrackets(before, after) {
			return edit{From: lo, To: hi, Text: text}, nil
		}
		opener := []rune(d.Buffer.Line(lo.Line))
		text += "\n" + indentString(leadColumns(opener, len(opener), tab), s.UseTabs, tab)
		middle := func(end buffer.Pos) buffer.Pos {
			return buffer.Pos{Line: end.Line - 1, Col: utf8.RuneCountInString(indent)}
		}
		return edit{From: lo, To: hi, Text: text}, middle
	}, plain)
}

// backspace removes each selection. With no selection it removes, at each caret,
// one indent unit when only blanks precede the caret (a tab, or up to tab_size
// spaces), both characters of an empty pair that auto_close inserted, or else the
// character before the caret, which may be a line break.
func (e *Editor) backspace(d *Document) {
	// Backspace in virtual space walks the caret back with no buffer change
	// until it reaches the line's text.
	if n := d.VirtualColumns(); n > 0 {
		d.SetVirtualColumns(n - 1)
		d.Want = -1
		return
	}
	s := e.SettingsFor(d)
	tab := e.tabSizeOf(d)
	plan := func(c caret) edit {
		if lo, hi := c.Bounds(); lo != hi {
			return edit{From: lo, To: hi}
		}
		head := c.Head
		prev, ok := prevPos(d.Buffer, head)
		if !ok {
			return edit{From: head, To: head}
		}
		line := []rune(d.Buffer.Line(head.Line))
		if head.Col > 0 && blankPrefix(line, head.Col) {
			n := 1
			if line[head.Col-1] != '\t' {
				n = 0
				for i := head.Col - 1; i >= 0 && line[i] == ' ' && n < tab; i-- {
					n++
				}
			}
			return edit{From: buffer.Pos{Line: head.Line, Col: head.Col - n}, To: head}
		}
		if s.AutoClose && head.Col > 0 && head.Col < len(line) && pairedQuotes(line[head.Col-1], line[head.Col]) {
			return edit{From: prev, To: buffer.Pos{Line: head.Line, Col: head.Col + 1}}
		}
		return edit{From: prev, To: head}
	}
	// Carets closer together than an indent unit plan edits that overlap, so then
	// every caret removes one character, which never does.
	single := func(c caret) edit {
		if lo, hi := c.Bounds(); lo != hi {
			return edit{From: lo, To: hi}
		}
		prev, _ := prevPos(d.Buffer, c.Head)
		return edit{From: prev, To: c.Head}
	}
	d.EditPlaced(func(c caret) (edit, placeFn) { return plan(c), nil }, single)
}
