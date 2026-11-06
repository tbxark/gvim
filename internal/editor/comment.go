package editor

import (
	"fmt"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/tbxark/gvim/internal/buffer"
)

// commentTokens maps a lowercased file extension, with its dot, to its line
// comment token. An extension with no entry has no comment token, and
// toggle_comment says so rather than guessing.
var commentTokens = map[string]string{
	".go":   "//",
	".rs":   "//",
	".c":    "//",
	".h":    "//",
	".cc":   "//",
	".cpp":  "//",
	".cxx":  "//",
	".hpp":  "//",
	".js":   "//",
	".jsx":  "//",
	".mjs":  "//",
	".ts":   "//",
	".tsx":  "//",
	".java": "//",
	".py":   "#",
	".sh":   "#",
	".bash": "#",
	".zsh":  "#",
	".rb":   "#",
	".yaml": "#",
	".yml":  "#",
	".toml": "#",
}

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

// toggleComment comments the selected lines, or the caret line, unless every
// one of them is already commented. Then it removes the token from each. A
// comment is the token and one space, placed at column 0. An uncomment removes
// one space after the token when there is one. The caret and selection move
// with the text. The token comes from the file extension. It fails when the
// buffer is untitled or its extension has no comment token.
func (d *Document) toggleComment() error {
	ext := strings.ToLower(filepath.Ext(d.Path))
	token, ok := commentTokens[ext]
	if !ok {
		what := ext
		switch {
		case d.Path == "":
			what = "untitled"
		case ext == "":
			what = filepath.Base(d.Path)
		}
		return fmt.Errorf("toggle_comment: no comment token for %s", what)
	}
	first, last := d.selectedLines()
	allCommented := true
	for line := first; line <= last; line++ {
		if !strings.HasPrefix(strings.TrimLeftFunc(d.Buffer.Line(line), unicode.IsSpace), token) {
			allCommented = false
			break
		}
	}
	var edits []lineEdit
	d.Buffer.Group(func() {
		for line := first; line <= last; line++ {
			if !allCommented {
				d.Buffer.Insert(buffer.Pos{Line: line}, token+" ")
				edits = append(edits, lineEdit{line: line, inserted: len(token) + 1})
				continue
			}
			runes := []rune(d.Buffer.Line(line))
			indent := 0
			for indent < len(runes) && unicode.IsSpace(runes[indent]) {
				indent++
			}
			n := len(token)
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
	d.Cursor = adjust(d.Cursor, d.Sel != nil)
	if d.Sel != nil {
		d.Sel.Anchor = adjust(d.Sel.Anchor, true)
		d.Sel.Head = d.Cursor
	}
	d.want = -1
	return nil
}
