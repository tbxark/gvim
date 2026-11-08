package app

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/tbxark/gvim/internal/tui"
	"github.com/tbxark/gvim/syntax"
)

// MaxStdinBytes bounds what "gvim -" (or --stdin) reads before the TUI starts. Fresh
// streams piped input into a spool and pages it lazily; gvim holds the buffer in memory,
// so the read is bounded and an oversized pipe is refused instead of half-opened.
const MaxStdinBytes = 32 << 20

// readStdin reads at most limit bytes from r. Reading limit+1 lets it tell an
// oversized input from one exactly at the cap without buffering more.
func readStdin(r io.Reader, limit int64) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err != nil {
		return nil, fmt.Errorf("stdin: %w", err)
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("stdin: input exceeds %d MiB", limit>>20)
	}
	return data, nil
}

// stdinHasNUL reports whether the piped text looks binary. The buffer keeps the bytes
// (a Go string can hold a NUL), so nothing is dropped; the caller only warns.
func stdinHasNUL(data []byte) bool {
	return bytes.IndexByte(data, 0) >= 0
}

// stdinLanguage names the lexer for the piped content: --stdin-name is treated as the
// file name (so its extension decides), else the content is sniffed the way a new file's
// path-less content is (shebang, then chroma's analysis).
func stdinLanguage(opts Options) string {
	if opts.StdinName != "" {
		return syntax.FileLanguage(opts.StdinName)
	}
	if len(opts.Stdin) == 0 {
		return ""
	}
	name := strings.ToLower(syntax.Detect("", opts.Stdin).Config().Name)
	if name == "" || syntax.IsPlain(name) {
		return ""
	}
	return name
}

// stdinInputTTY opens the terminal for the TUI keyboard after the pipe has been consumed:
// standard input is at EOF, so the program reads keys from the controlling terminal
// (/dev/tty on Unix, CONIN$ on Windows, through tui.OpenInputTTY).
func stdinInputTTY() (*os.File, error) {
	in, err := tui.OpenInputTTY()
	if err != nil {
		return nil, fmt.Errorf("stdin: opening the terminal for input: %w", err)
	}
	return in, nil
}
