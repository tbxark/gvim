package app

import (
	"os/exec"
	"slices"
	"strings"

	"github.com/tbxark/gvim/config"
)

// defaultFormatter is one entry of Fresh's built-in formatter table
// (crates/fresh-editor-core/src/config.rs, the languages map): the program, the command
// that runs it on the text in standard input, and the keys the editor finds it by, which
// are the language names gvim's detection gives and the file extensions. $FILE in the
// command is the path of the buffer.
type defaultFormatter struct {
	program string
	command string
	keys    []string
}

// defaultFormatters is the table. Fresh gives these languages a formatter and no others.
// Its `format_on_save` is false for all of them, so Format Buffer is the only way they run.
var defaultFormatters = []defaultFormatter{
	{"rustfmt", "rustfmt --edition 2021", []string{"rust", ".rs"}},
	{"gofmt", "gofmt", []string{"go", ".go"}},
	{"ruff", "ruff format --stdin-filename $FILE", []string{"python", ".py"}},
	{"clang-format", "clang-format", []string{"c", ".c", ".h"}},
	{"clang-format", "clang-format", []string{"c++", "cpp", ".cc", ".cpp", ".cxx", ".hpp", ".hh", ".hxx"}},
	{"prettier", "prettier --stdin-filepath $FILE", []string{"javascript", ".js", ".mjs", ".cjs", ".jsx"}},
	{"prettier", "prettier --stdin-filepath $FILE", []string{"typescript", "tsx", ".ts", ".tsx", ".mts", ".cts"}},
	{"prettier", "prettier --stdin-filepath $FILE", []string{"json", ".json"}},
	{"prettier", "prettier --stdin-filepath $FILE", []string{"jsonc", ".jsonc"}},
	{"prettier", "prettier --stdin-filepath $FILE", []string{"yaml", ".yaml", ".yml"}},
}

// formattersFor returns the formatters the editor runs for Format Buffer: the formatters key of
// the config over Fresh's table. A table entry is left out when its program is not on PATH, so
// that Format Buffer falls back to the language server instead of running a program that is not
// there, and when the config sets a formatter for any key of its language (the name or an
// extension), so that no default key shadows the user's. lookPath is exec.LookPath in the
// program, and it runs once at startup, never in the update loop.
func formattersFor(cfg config.Config, lookPath func(string) (string, error)) map[string]string {
	out := make(map[string]string, len(cfg.Formatters)+len(defaultFormatters)*4)
	user := make(map[string]bool, len(cfg.Formatters))
	for k, v := range cfg.Formatters {
		out[k] = v
		user[strings.ToLower(k)] = true
	}
	for _, f := range defaultFormatters {
		if slices.ContainsFunc(f.keys, func(k string) bool { return user[k] }) {
			continue
		}
		if _, err := lookPath(f.program); err != nil {
			continue
		}
		for _, k := range f.keys {
			out[k] = f.command
		}
	}
	return out
}

// lookPath is how the program finds formatters. A variable so that a test may replace it.
var lookPath = exec.LookPath
