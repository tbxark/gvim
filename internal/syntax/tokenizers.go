package syntax

import "github.com/alecthomas/chroma/v2"

// fixedTokens returns it with the fixups of lexer applied, or it itself for a lexer
// that has none, which keeps streaming.
func fixedTokens(lexer chroma.Lexer, it chroma.Iterator) chroma.Iterator {
	fs := fixups[lexer.Config().Name]
	if len(fs) == 0 {
		return it
	}
	toks := it.Tokens()
	for _, f := range fs {
		toks = f(toks)
	}
	return chroma.Literator(toks...)
}

// tokenizers maps a lexer name to a tokenizer that takes the place of chroma's lexer for
// that language. Markdown has one, because the chroma lexer reads a hard-wrapped
// paragraph character by character, loses the emphasis at a line start and makes a block
// quote italic, none of which a reader of markdown expects.
var tokenizers = map[string]func(text []string, inside bool) [][]Span{}

// The markdown tokenizer lexes fenced code through Tokenize, so it is registered here and
// not in the literal, which would make the initialization of the table a cycle.
func init() { tokenizers["markdown"] = tokenizeMarkdown }
