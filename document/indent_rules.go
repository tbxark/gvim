package document

import (
	"github.com/tbxark/gvim/buffer"
	"regexp"
	"strings"
	"sync"
)

// NonCodeContext reports, for each rune column of a line of d, whether the character
// is inside a comment or a string literal, as the syntax highlighter classifies it. A
// nil result means every character is code. The editor does not import the
// highlighter, so the app supplies this. Without it every character is code.
type NonCodeContext func(d *Document, line int) []bool

// indentDef is the source of an indentRules. An empty pattern never matches.
type indentDef struct {
	Increase, Decrease, IndentNext, DedentNext, SelfClose string
	// significant marks a language where indentation is the block structure: Enter on a
	// blank line keeps the column, so a manual dedent sticks.
	Significant bool
}

const smaliBlocks = `(?:method|annotation|subannotation|packed-switch|sparse-switch|array-data|param|parameter)`

// indentFamilies are Fresh's built-in rule sets, by family.
var indentFamilies = map[string]indentDef{
	"curly": {
		Increase:   `[\{\[\(]\s*$`,
		Decrease:   `^\s*[\}\]\)]`,
		IndentNext: `^\s*((if|for|while)\b.*\)|else)\s*$`,
	},
	"python": {
		Increase:    `:\s*$`,
		Decrease:    `^\s*(elif\s.*|else\s*|except.*|finally\s*|case\s.*):\s*`,
		DedentNext:  `^\s*(return|pass|raise|break|continue)\b`,
		Significant: true,
	},
	"ruby": {
		Increase:  `(^\s*(if|unless|while|until|for|begin|def|class|module|case|else|elsif|when|in|rescue|ensure)\b)|(\bdo(\s*\|[^|]*\|)?\s*$)`,
		Decrease:  `^\s*(end|else|elsif|when|in|rescue|ensure)\b`,
		SelfClose: `\bend\b`,
	},
	"lua": {
		Increase:  `(^\s*((local\s+)?function|if|elseif|else|for|while|repeat)\b)|(\b(do|then)\s*$)`,
		Decrease:  `^\s*(end|else|elseif|until)\b`,
		SelfClose: `\bend\b`,
	},
	"bash": {
		// A trailing "(" is a subshell, not a block.
		Increase: `(\b(then|do)\s*$)|(^\s*case\b.*\bin\s*$)|(\{\s*$)`,
		Decrease: `^\s*(fi|done|esac|else|elif|\})`,
	},
	"fish": {
		Increase:  `^\s*(if|else|for|while|begin|function|switch|case)\b`,
		Decrease:  `^\s*(end|else|case)\b`,
		SelfClose: `\bend\b`,
	},
	"smali": {
		Increase:  `(^\s*\.` + smaliBlocks + `\b)|([\{\[\(]\s*$)`,
		Decrease:  `^\s*(?:\.end\s+` + smaliBlocks + `\b|[\}\]\)])`,
		SelfClose: `^\s*\.` + smaliBlocks + `\b.*\s\.end\s+` + smaliBlocks + `\b`,
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
	Increase, Decrease, IndentNext, DedentNext, SelfClose *regexp.Regexp
	Significant                                           bool
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
		Increase: c(def.Increase), Decrease: c(def.Decrease), IndentNext: c(def.IndentNext),
		DedentNext: c(def.DedentNext), SelfClose: c(def.SelfClose), Significant: def.Significant,
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
		set(&def.Increase, u.IncreaseIndentPattern)
		set(&def.Decrease, u.DecreaseIndentPattern)
		set(&def.IndentNext, u.IndentNextLinePattern)
		set(&def.DedentNext, u.DedentNextLinePattern)
		set(&def.SelfClose, u.SelfClosePattern)
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
	Doc  *Document
	Tab  int
	Mask NonCodeContext
}

func (v indentView) runes(line int) []rune { return []rune(v.Doc.Buffer.Line(line)) }

// code returns runes[from:to] of line with the comment and string characters turned
// into spaces and the carriage returns dropped.
func (v indentView) Code(line int, runes []rune, from, to int) string {
	from, to = min(max(from, 0), len(runes)), min(max(to, 0), len(runes))
	var masked []bool
	if v.Mask != nil {
		masked = v.Mask(v.Doc, line)
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
	return matchRe(r.Increase, code) && !matchRe(r.SelfClose, code)
}

// newLineIndent returns the indent width for a line that Enter starts at p
// (calculate_indent).
func (r *indentRules) NewLineIndent(v indentView, p buffer.Pos) int {
	line := v.runes(p.Line)
	col := min(p.Col, len(line))
	curHasContent := hasText(line, 0, col)
	if r.Significant && !curHasContent && !hasText(line, col, len(line)) {
		return leadColumns(line, col, v.Tab)
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
	indent := leadColumns(refRunes, refEnd, v.Tab)
	ref := v.Code(refLine, refRunes, 0, refEnd)
	opened := r.increases(ref) || matchRe(r.IndentNext, ref)
	switch {
	case opened:
		indent += v.Tab
	case matchRe(r.DedentNext, ref):
		indent = max(indent-v.Tab, 0)
	}
	// A closer at the start of the text that moves down dedents the new line, unless
	// the opener that was just counted is on this same line: Enter between "{" and
	// "}" puts the closer on its own line, and must not cancel the indent.
	tail := v.Code(p.Line, line, col, len(line))
	if matchRe(r.Decrease, tail) && (!opened || !curHasContent) {
		indent = max(indent-v.Tab, 0)
	}
	return indent
}

// dedentForDelimiter returns the indent of a line whose first typed character is the
// closing delimiter ch, or false when the language has no decrease rule for it
// (calculate_dedent_for_delimiter).
func (r *indentRules) DedentForDelimiter(v indentView, p buffer.Pos, ch rune) (int, bool) {
	if !matchRe(r.Decrease, string(ch)) {
		return 0, false
	}
	refLine := v.prevTextLine(p.Line)
	if refLine < 0 {
		return 0, false
	}
	runes := v.runes(refLine)
	indent := leadColumns(runes, len(runes), v.Tab)
	if r.increases(v.Code(refLine, runes, 0, len(runes))) {
		indent += v.Tab
	}
	return max(indent-v.Tab, 0), true
}

// consumesLine reports whether line is exactly a decrease trigger: the pattern
// matches, and the leftmost match is the whole text (decrease_consumes_line).
func (r *indentRules) ConsumesLine(line string) bool {
	if r.Decrease == nil {
		return false
	}
	m := r.Decrease.FindStringIndex(line)
	return m != nil && m[0] == 0 && m[1] == len(line)
}

// onTypeDedentTarget returns the indent for a line that a keystroke just made a
// decrease trigger: one level shallower than the previous line with text, or false when
// that line opens a block, where the trigger is the block's first body line
// (on_type_dedent_target).
func (r *indentRules) OnTypeDedentTarget(v indentView, line int) (int, bool) {
	refLine := v.prevTextLine(line)
	if refLine < 0 {
		return 0, false
	}
	runes := v.runes(refLine)
	ref := v.Code(refLine, runes, 0, len(runes))
	if r.increases(ref) || matchRe(r.IndentNext, ref) {
		return 0, false
	}
	return max(leadColumns(runes, len(runes), v.Tab)-v.Tab, 0), true
}

// keywordDedentReach is how much of a line before the caret the electric keyword
// dedent examines, so that a long line costs nothing per keystroke.
const keywordDedentReach = 128
