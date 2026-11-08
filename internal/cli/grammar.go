package cli

import (
	"slices"

	"github.com/spf13/cobra"

	"github.com/tbxark/gvim/syntax"
)

// grammarDeps are the functions the grammar commands call. The syntax package
// supplies the real one, and tests pass a fake list.
type grammarDeps struct {
	names func() []string
}

// defaultGrammarDeps returns the dependencies that read the chroma registry.
func defaultGrammarDeps() grammarDeps {
	return grammarDeps{names: syntax.GrammarNames}
}

// newGrammarCommand returns the grammar command group, wired to the real
// dependencies.
func newGrammarCommand() *cobra.Command {
	return newGrammarCommandWith(defaultGrammarDeps())
}

// newGrammarCommandWith returns the grammar command group that uses d.
func newGrammarCommandWith(d grammarDeps) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "grammar",
		Short: "Inspect the syntax grammars",
	}
	cmd.AddCommand(newGrammarListCommand(d))
	return cmd
}

// newGrammarListCommand prints the grammar names, one per line, sorted and
// without repeats.
func newGrammarListCommand(d grammarDeps) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List the available grammars",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			names := slices.Clone(d.names())
			slices.Sort(names)
			names = slices.Compact(names)
			w := cmd.OutOrStdout()
			for _, name := range names {
				if err := writeLine(w, name); err != nil {
					return err
				}
			}
			return nil
		},
	}
}
