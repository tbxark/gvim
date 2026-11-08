package editor

import (
	"github.com/tbxark/gvim/buffer"
	"maps"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"
)

// commentPrefix returns the line comment token of d, without surrounding blanks, or
// "" when its language has none. A language chosen with Set Language decides when it
// has a prefix. Otherwise the effective settings of the file decide, which hold the
// languages.<id>.comment_prefix of the config and Fresh's built-in prefixes.
func (e *Editor) commentPrefix(d *Document) string {
	if d.Language() != "" && e.languages != nil {
		if p := strings.TrimSpace(e.languages(d.Language(), "").CommentPrefix); p != "" {
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
	cs := d.AllCarets()
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
				edits = append(edits, lineEdit{Line: line, Inserted: width + 1})
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
			edits = append(edits, lineEdit{Line: line, Col: indent, Removed: n})
		}
	})
	adjust := func(p buffer.Pos, strict bool) buffer.Pos {
		for _, e := range edits {
			p = e.Move(p, strict)
		}
		return p
	}
	for i := range cs {
		if cs[i].Anchor == cs[i].Head {
			// An empty caret moves as one point, so it never grows a selection.
			cs[i].Head = adjust(cs[i].Head, false)
			cs[i].Anchor = cs[i].Head
			continue
		}
		cs[i].Head = adjust(cs[i].Head, true)
		cs[i].Anchor = adjust(cs[i].Anchor, true)
	}
	d.SetCarets(cs)
	return nil
}
