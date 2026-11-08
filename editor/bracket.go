package editor

import (
	"errors"
	"strings"
)

// registerBracketHandlers registers goto_matching_bracket.
func registerBracketHandlers(h map[Action]handler) {
	h["goto_matching_bracket"] = gotoMatchingBracket
}

// gotoMatchingBracket moves the caret to the bracket that matches the bracket
// at the caret. Anywhere else it goes to the closing bracket of the pair that
// encloses the caret. When neither exists, it uses the bracket just before the
// caret. With a mark active, the selection grows to the match.
func gotoMatchingBracket(e *Editor, _ map[string]any) error {
	d := e.Active()
	pairs := e.bracketPairsOf(d)
	at, ok, limited := bracketTarget(d, pairs)
	if limited {
		return ErrScanLimit
	}
	if !ok {
		return errors.New("no bracket at cursor")
	}
	to, ok, limited := matchingBracket(d.Buffer, at, pairs)
	if limited {
		return ErrScanLimit
	}
	if !ok {
		return errors.New("no matching bracket found")
	}
	d.Want = -1
	d.Move(to, e.marking(d))
	return nil
}

// bracketPairsOf returns the bracket pairs of d's language.
func (e *Editor) bracketPairsOf(d *Document) [][2]rune {
	lang := e.SettingsFor(d).Language
	if lang == "" {
		lang = strings.ToLower(d.Language())
	}
	if lang == "html" || lang == "templ" {
		return append(bracketPairs[:len(bracketPairs):len(bracketPairs)], angleBrackets)
	}
	return bracketPairs
}
