package editor

import (
	"maps"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/tbxark/gvim/internal/buffer"
)

// lineEdit is one edit inside a line: the runes inserted at col, or the runes
// removed from col on.
type lineEdit struct {
	line, col         int
	inserted, removed int
}

// move returns where p ends up after the edit. A position on another line does
// not move. After a removal, a position past the removed run moves back by its
// length, and a position inside it moves to where the run began. After an
// insertion, a position past col moves forward by the inserted length. A
// position at col moves only when strict is false: a caret moves with the text
// it sits in front of, while a selection end stays where it was.
func (e lineEdit) move(p buffer.Pos, strict bool) buffer.Pos {
	if p.Line != e.line {
		return p
	}
	switch {
	case e.removed > 0:
		switch {
		case p.Col >= e.col+e.removed:
			p.Col -= e.removed
		case p.Col > e.col:
			p.Col = e.col
		}
	case e.inserted > 0:
		if p.Col > e.col || (!strict && p.Col == e.col) {
			p.Col += e.inserted
		}
	}
	return p
}

// commentPrefix returns the line comment token of d, without surrounding blanks, or
// "" when its language has none. A language chosen with Set Language decides when it
// has a prefix. Otherwise the effective settings of the file decide, which hold the
// languages.<id>.comment_prefix of the config and Fresh's built-in prefixes.
func (e *Editor) commentPrefix(d *Document) string {
	if d.language != "" && e.languages != nil {
		if p := strings.TrimSpace(e.languages(d.language, "").CommentPrefix); p != "" {
			return p
		}
	}
	return strings.TrimSpace(e.SettingsFor(d).CommentPrefix)
}

// toggleComment comments the selected lines, and the line of every caret without a
// selection, unless every one of them is already commented. Then it removes the
// token from each. A comment is the token and one space, placed at column 0. An
// uncomment removes one space after the token when there is one. Every caret and
// selection moves with the text. The token is the comment_prefix of the document's
// language (see commentPrefix). It does nothing, as Fresh does, when the language has none.
func (e *Editor) toggleComment(d *Document) error {
	token := e.commentPrefix(d)
	if token == "" {
		return nil
	}
	width := utf8.RuneCountInString(token)
	cs := d.allCarets()
	lineSet := make(map[int]bool)
	for _, c := range cs {
		first, last := caretLines(c)
		for line := first; line <= last; line++ {
			lineSet[line] = true
		}
	}
	lines := slices.Sorted(maps.Keys(lineSet))
	allCommented := true
	for _, line := range lines {
		if !strings.HasPrefix(strings.TrimLeftFunc(d.Buffer.Line(line), unicode.IsSpace), token) {
			allCommented = false
			break
		}
	}
	var edits []lineEdit
	d.Buffer.Group(func() {
		for _, line := range lines {
			if !allCommented {
				d.Buffer.Insert(buffer.Pos{Line: line}, token+" ")
				edits = append(edits, lineEdit{line: line, inserted: width + 1})
				continue
			}
			runes := []rune(d.Buffer.Line(line))
			indent := 0
			for indent < len(runes) && unicode.IsSpace(runes[indent]) {
				indent++
			}
			n := width
			if strings.HasPrefix(string(runes[indent:]), token+" ") {
				n++
			}
			d.Buffer.Delete(buffer.Pos{Line: line, Col: indent}, buffer.Pos{Line: line, Col: indent + n})
			edits = append(edits, lineEdit{line: line, col: indent, removed: n})
		}
	})
	adjust := func(p buffer.Pos, strict bool) buffer.Pos {
		for _, e := range edits {
			p = e.move(p, strict)
		}
		return p
	}
	for i := range cs {
		if cs[i].anchor == cs[i].head {
			// An empty caret moves as one point, so it never grows a selection.
			cs[i].head = adjust(cs[i].head, false)
			cs[i].anchor = cs[i].head
			continue
		}
		cs[i].head = adjust(cs[i].head, true)
		cs[i].anchor = adjust(cs[i].anchor, true)
	}
	d.setCarets(cs)
	return nil
}
