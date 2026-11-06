package editor

import (
	"regexp"
	"strings"
	"sync"

	"github.com/tbxark/gvim/internal/buffer"
)

// Per-language, regex-based auto-indentation, a port of Fresh's
// primitives/indent_rules.rs. A language is described by five anchored regular
// expressions that are matched against the code view of a line, which is the line
// with its comment and string characters blanked to spaces (see NonCodeContext):
//
//   - increase: the line before the new one matches, so the new line is one level deeper
//   - decrease: the new line's own text matches, so it is one level shallower
//   - indent_next_line: a one-shot increase, for a braceless "if (x)"
//   - dedent_next_line: a one-shot decrease, for a Python "return"
//   - self_close: cancels increase when the same line also closes its block
//
// Not ported: Fresh's tree-sitter tier (Go, JSON, JavaScript, TypeScript and Templ keep
// the language-agnostic heuristic of smart_indent.go) and Pascal's construct-stack
// indenter (Pascal keeps the heuristic).

// NonCodeContext reports, for each rune column of a line of d, whether the character
// is inside a comment or a string literal, as the syntax highlighter classifies it. A
// nil result means every character is code. The editor does not import the
// highlighter, so the app supplies this. Without it every character is code.
type NonCodeContext func(d *Document, line int) []bool

// SetNonCodeContext sets the query that masks comments and strings when the indent
// rules look at a line.
func (e *Editor) SetNonCodeContext(f NonCodeContext) { e.nonCode = f }

// indentDef is the source of an indentRules. An empty pattern never matches.
type indentDef struct {
	increase, decrease, indentNext, dedentNext, selfClose string
	// significant marks a language where indentation is the block structure: Enter on a
	// blank line keeps the column, so a manual dedent sticks.
	significant bool
}

const smaliBlocks = `(?:method|annotation|subannotation|packed-switch|sparse-switch|array-data|param|parameter)`

// indentFamilies are Fresh's built-in rule sets, by family.
var indentFamilies = map[string]indentDef{
	"curly": {
		increase:   `[\{\[\(]\s*$`,
		decrease:   `^\s*[\}\]\)]`,
		indentNext: `^\s*((if|for|while)\b.*\)|else)\s*$`,
	},
	"python": {
		increase:    `:\s*$`,
		decrease:    `^\s*(elif\s.*|else\s*|except.*|finally\s*|case\s.*):\s*`,
		dedentNext:  `^\s*(return|pass|raise|break|continue)\b`,
		significant: true,
	},
	"ruby": {
		increase:  `(^\s*(if|unless|while|until|for|begin|def|class|module|case|else|elsif|when|in|rescue|ensure)\b)|(\bdo(\s*\|[^|]*\|)?\s*$)`,
		decrease:  `^\s*(end|else|elsif|when|in|rescue|ensure)\b`,
		selfClose: `\bend\b`,
	},
	"lua": {
		increase:  `(^\s*((local\s+)?function|if|elseif|else|for|while|repeat)\b)|(\b(do|then)\s*$)`,
		decrease:  `^\s*(end|else|elseif|until)\b`,
		selfClose: `\bend\b`,
	},
	"bash": {
		// A trailing "(" is a subshell, not a block.
		increase: `(\b(then|do)\s*$)|(^\s*case\b.*\bin\s*$)|(\{\s*$)`,
		decrease: `^\s*(fi|done|esac|else|elif|\})`,
	},
	"fish": {
		increase:  `^\s*(if|else|for|while|begin|function|switch|case)\b`,
		decrease:  `^\s*(end|else|case)\b`,
		selfClose: `\bend\b`,
	},
	"smali": {
		increase:  `(^\s*\.` + smaliBlocks + `\b)|([\{\[\(]\s*$)`,
		decrease:  `^\s*(?:\.end\s+` + smaliBlocks + `\b|[\}\]\)])`,
		selfClose: `^\s*\.` + smaliBlocks + `\b.*\s\.end\s+` + smaliBlocks + `\b`,
	},
}

// indentFamilyOf maps a language id to its family (family_for_id).
var indentFamilyOf = map[string]string{
	"rust": "curly", "c": "curly", "cpp": "curly", "c++": "curly", "csharp": "curly",
	"c_sharp": "curly", "java": "curly", "go": "curly", "javascript": "curly",
	"typescript": "curly", "typescriptreact": "curly", "javascriptreact": "curly",
	"php": "curly", "swift": "curly", "kotlin": "curly", "dart": "curly", "scala": "curly",
	"json": "curly", "jsonc": "curly", "css": "curly", "scss": "curly", "less": "curly",
	"odin":   "curly",
	"python": "python", "ruby": "ruby", "lua": "lua",
	"bash": "bash", "sh": "bash", "shell": "bash", "shellscript": "bash",
	"fish": "fish", "smali": "smali",
}

// treeSitterLanguages are the languages for which Fresh indents from a bundled
// tree-sitter grammar rather than from the rules. gvim has no grammar, so these keep the
// heuristic of smart_indent.go unless the user sets languages.<id>.indent.
var treeSitterLanguages = map[string]bool{
	"go": true, "json": true, "jsonc": true, "javascript": true, "typescript": true, "templ": true,
}

// indentRules is a compiled indentDef.
type indentRules struct {
	increase, decrease, indentNext, dedentNext, selfClose *regexp.Regexp
	significant                                           bool
}

var compiledRules sync.Map // indentDef -> *indentRules

// compileRules compiles def once. A pattern that does not compile is dropped, so one
// typo in a config does not take the editor down.
func compileRules(def indentDef) *indentRules {
	if r, ok := compiledRules.Load(def); ok {
		return r.(*indentRules)
	}
	c := func(p string) *regexp.Regexp {
		if p == "" {
			return nil
		}
		re, err := regexp.Compile(p)
		if err != nil {
			return nil
		}
		return re
	}
	r := &indentRules{
		increase: c(def.increase), decrease: c(def.decrease), indentNext: c(def.indentNext),
		dedentNext: c(def.dedentNext), selfClose: c(def.selfClose), significant: def.significant,
	}
	compiledRules.Store(def, r)
	return r
}

// indentRulesOf returns the rules that indent a document with settings s, or nil to
// use the heuristic of smart_indent.go. A languages.<id>.indent block wins, each
// pattern it sets replacing the family's and the others kept (set_user_rule).
// Otherwise the family of the language decides, except where Fresh uses a grammar.
func indentRulesOf(s LanguageSettings) *indentRules {
	def, hasFamily := indentFamilies[indentFamilyOf[s.Language]]
	if u := s.Indent; u != nil && *u != (IndentRules{}) {
		set := func(dst *string, v string) {
			if v != "" {
				*dst = v
			}
		}
		set(&def.increase, u.IncreaseIndentPattern)
		set(&def.decrease, u.DecreaseIndentPattern)
		set(&def.indentNext, u.IndentNextLinePattern)
		set(&def.dedentNext, u.DedentNextLinePattern)
		set(&def.selfClose, u.SelfClosePattern)
		return compileRules(def)
	}
	if !hasFamily || treeSitterLanguages[s.Language] {
		return nil
	}
	return compileRules(def)
}

// indentView gives the rules access to the lines of one document, with the display
// width of a tab and the mask of comments and strings.
type indentView struct {
	d    *Document
	tab  int
	mask NonCodeContext
}

func (e *Editor) indentViewOf(d *Document, tab int) indentView {
	return indentView{d: d, tab: max(tab, 1), mask: e.nonCode}
}

func (v indentView) runes(line int) []rune { return []rune(v.d.Buffer.Line(line)) }

// code returns runes[from:to] of line with the comment and string characters turned
// into spaces and the carriage returns dropped.
func (v indentView) code(line int, runes []rune, from, to int) string {
	from, to = min(max(from, 0), len(runes)), min(max(to, 0), len(runes))
	var masked []bool
	if v.mask != nil {
		masked = v.mask(v.d, line)
	}
	var sb strings.Builder
	for i := from; i < to; i++ {
		switch {
		case runes[i] == '\r':
		case i < len(masked) && masked[i]:
			sb.WriteByte(' ')
		default:
			sb.WriteRune(runes[i])
		}
	}
	return sb.String()
}

// hasText reports whether runes[from:to] holds a character that is not a blank.
func hasText(runes []rune, from, to int) bool {
	from, to = min(max(from, 0), len(runes)), min(max(to, 0), len(runes))
	for _, r := range runes[from:to] {
		if !isBlank(r) {
			return true
		}
	}
	return false
}

// prevTextLine returns the nearest line above line that has text, or -1.
func (v indentView) prevTextLine(line int) int {
	for q := line - 1; q >= 0; q-- {
		if r := v.runes(q); hasText(r, 0, len(r)) {
			return q
		}
	}
	return -1
}

func matchRe(re *regexp.Regexp, s string) bool { return re != nil && re.MatchString(s) }

// increases reports whether code opens a block: increase matches and self_close does not.
func (r *indentRules) increases(code string) bool {
	return matchRe(r.increase, code) && !matchRe(r.selfClose, code)
}

// newLineIndent returns the indent width for a line that Enter starts at p
// (calculate_indent).
func (r *indentRules) newLineIndent(v indentView, p buffer.Pos) int {
	line := v.runes(p.Line)
	col := min(p.Col, len(line))
	curHasContent := hasText(line, 0, col)
	if r.significant && !curHasContent && !hasText(line, col, len(line)) {
		return leadColumns(line, col, v.tab)
	}
	refLine, refRunes, refEnd := p.Line, line, col
	if !curHasContent {
		refLine = v.prevTextLine(p.Line)
		if refLine < 0 {
			return 0
		}
		refRunes = v.runes(refLine)
		refEnd = len(refRunes)
	}
	indent := leadColumns(refRunes, refEnd, v.tab)
	ref := v.code(refLine, refRunes, 0, refEnd)
	opened := r.increases(ref) || matchRe(r.indentNext, ref)
	switch {
	case opened:
		indent += v.tab
	case matchRe(r.dedentNext, ref):
		indent = max(indent-v.tab, 0)
	}
	// A closer at the start of the text that moves down dedents the new line, unless
	// the opener that was just counted is on this same line: Enter between "{" and
	// "}" puts the closer on its own line, and must not cancel the indent.
	tail := v.code(p.Line, line, col, len(line))
	if matchRe(r.decrease, tail) && (!opened || !curHasContent) {
		indent = max(indent-v.tab, 0)
	}
	return indent
}

// dedentForDelimiter returns the indent of a line whose first typed character is the
// closing delimiter ch, or false when the language has no decrease rule for it
// (calculate_dedent_for_delimiter).
func (r *indentRules) dedentForDelimiter(v indentView, p buffer.Pos, ch rune) (int, bool) {
	if !matchRe(r.decrease, string(ch)) {
		return 0, false
	}
	refLine := v.prevTextLine(p.Line)
	if refLine < 0 {
		return 0, false
	}
	runes := v.runes(refLine)
	indent := leadColumns(runes, len(runes), v.tab)
	if r.increases(v.code(refLine, runes, 0, len(runes))) {
		indent += v.tab
	}
	return max(indent-v.tab, 0), true
}

// consumesLine reports whether line is exactly a decrease trigger: the pattern
// matches, and the leftmost match is the whole text (decrease_consumes_line).
func (r *indentRules) consumesLine(line string) bool {
	if r.decrease == nil {
		return false
	}
	m := r.decrease.FindStringIndex(line)
	return m != nil && m[0] == 0 && m[1] == len(line)
}

// onTypeDedentTarget returns the indent for a line that a keystroke just made a
// decrease trigger: one level shallower than the previous line with text, or false when
// that line opens a block, where the trigger is the block's first body line
// (on_type_dedent_target).
func (r *indentRules) onTypeDedentTarget(v indentView, line int) (int, bool) {
	refLine := v.prevTextLine(line)
	if refLine < 0 {
		return 0, false
	}
	runes := v.runes(refLine)
	ref := v.code(refLine, runes, 0, len(runes))
	if r.increases(ref) || matchRe(r.indentNext, ref) {
		return 0, false
	}
	return max(leadColumns(runes, len(runes), v.tab)-v.tab, 0), true
}

// keywordDedentReach is how much of a line before the caret the electric keyword
// dedent examines, so that a long line costs nothing per keystroke.
const keywordDedentReach = 128

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
	if !rules.consumesLine(rawBefore+string(ch)) || rules.consumesLine(rawBefore) {
		return edit{}, false
	}
	v := e.indentViewOf(d, tab)
	before := v.code(p.Line, line, 0, p.Col)
	if !rules.consumesLine(before+string(ch)) || rules.consumesLine(before) {
		return edit{}, false
	}
	target, ok := rules.onTypeDedentTarget(v, p.Line)
	if !ok || target >= leadColumns(line, p.Col, v.tab) {
		return edit{}, false
	}
	body := strings.TrimLeft(string(prefix), " \t")
	return edit{
		from: buffer.Pos{Line: p.Line},
		to:   p,
		text: indentString(target, s.UseTabs, tab) + body + string(ch),
	}, true
}

// closerIndentFor returns the indent for a closing bracket ch typed at p: the rules
// of the language when they have a decrease rule for it, else the opener scan.
func (e *Editor) closerIndentFor(d *Document, rules *indentRules, p buffer.Pos, ch rune, tab int) int {
	if rules != nil {
		if w, ok := rules.dedentForDelimiter(e.indentViewOf(d, tab), p, ch); ok {
			return w
		}
	}
	return closerIndent(d.Buffer, p, tab)
}
