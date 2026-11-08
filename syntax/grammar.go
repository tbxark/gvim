package syntax

import "github.com/alecthomas/chroma/v2/lexers"

// GrammarNames returns the names and aliases of the syntax grammars chroma
// knows. The order is not specified, and names may repeat.
func GrammarNames() []string {
	return lexers.Names(true)
}
