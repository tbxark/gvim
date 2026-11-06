package editor

import (
	"errors"
	"fmt"
	"maps"
	"path/filepath"
	"slices"
	"strings"

	"github.com/tbxark/gvim/internal/buffer"
)

// ErrNoFormatter is returned by format_buffer when no formatter is set for the
// language of the document.
var ErrNoFormatter = errors.New("no formatter configured")

// SetFormatters sets the formatter command of each language. The keys are language
// names, as SetLanguage records them, or file extensions with their dot, such as ".go".
// Keys are matched without regard to case, so they are stored lowercased. Each command
// reads the whole text on its standard input and writes the formatted text to its
// standard output. A key with no entry has no formatter. The map is copied, so the
// caller may change it afterwards. Nil removes every formatter.
func (e *Editor) SetFormatters(formatters map[string]string) {
	e.formatters = make(map[string]string, len(formatters))
	// Sorted, so that two keys that differ only in case always give the same winner.
	for _, k := range slices.Sorted(maps.Keys(formatters)) {
		e.formatters[strings.ToLower(k)] = formatters[k]
	}
}

// formatterOf returns the formatter command of d, or "" when it has none. The language
// name is tried first, then the file extension with its dot, so a file opened from disk
// with no language set still finds the formatter of its extension.
func (e *Editor) formatterOf(d *Document) string {
	if lang := strings.ToLower(d.language); lang != "" {
		if command := e.formatters[lang]; command != "" {
			return command
		}
	}
	if ext := strings.ToLower(filepath.Ext(d.Path)); ext != "" {
		return e.formatters[ext]
	}
	return ""
}

// HasFormatter reports whether the active document has a formatter, which is what the
// menu's Format Buffer row needs. It changes no state.
func (e *Editor) HasFormatter() bool {
	d := e.Active()
	return d != nil && e.formatterOf(d) != ""
}

// formatBuffer runs format_buffer through the shell path, in the three steps of the
// split (see BeginShell), so that the UI can run the formatter off its update loop.
// Here it runs in Do, which blocks until the formatter ends.
func formatBuffer(e *Editor, _ map[string]any) error {
	job, err := e.beginFormat()
	if err != nil {
		return err
	}
	out, runErr := RunShellJob(job)
	return e.EndShell(job, out, runErr).Err
}

// beginFormat prepares the job of format_buffer. The whole text of the active document
// goes to the formatter of its language on standard input, and the output replaces
// the text, whatever the selection. A read-only document is refused with ErrReadOnly,
// and a document whose language has no formatter with ErrNoFormatter. The job changes
// no text.
func (e *Editor) beginFormat() (ShellJob, error) {
	d := e.Active()
	if d == nil {
		return ShellJob{}, ErrNoDocument
	}
	if d.readOnly {
		return ShellJob{}, ErrReadOnly
	}
	command := e.formatterOf(d)
	if command == "" {
		if d.language == "" {
			return ShellJob{}, fmt.Errorf("format_buffer: %w for this buffer", ErrNoFormatter)
		}
		return ShellJob{}, fmt.Errorf("format_buffer: %w for %s", ErrNoFormatter, d.language)
	}
	if d.large != nil {
		// The formatter takes the whole text of the file.
		return ShellJob{}, errLargeAction("format_buffer")
	}
	text := d.Buffer.Text()
	return ShellJob{
		Command:  command,
		Input:    text,
		Dir:      shellDir(d),
		Replace:  true,
		Timeout:  shellTimeout,
		format:   true,
		doc:      d,
		from:     buffer.Pos{},
		to:       d.Buffer.End(),
		snapshot: contentMark{text: text},
	}, nil
}
