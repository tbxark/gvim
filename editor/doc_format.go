package editor

import (
	"errors"
	"fmt"
	"maps"
	"path/filepath"
	"slices"
	"strings"

	"github.com/tbxark/gvim/buffer"
	"github.com/tbxark/gvim/diff"
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

// SetLanguageNamer sets the function that names the language of a file from its path, as the
// highlighter does ("Go" for x.go, "" for plain text). A document with no language set with
// SetLanguage is named by it, so a formatters key given as a language name ("go") matches a
// file opened from disk. The function runs on the caller's goroutine, on every menu check, so
// it should cache. Nil means that only the language set and the extension are used.
func (e *Editor) SetLanguageNamer(namer func(path string) string) { e.languageNamer = namer }

// formatterOf returns the formatter command of d, or "" when it has none. The language
// name is tried first (the one set with SetLanguage, else the one the namer gives the file),
// then the file extension with its dot, so a file with no language still finds the formatter
// of its extension.
func (e *Editor) formatterOf(d *Document) string {
	lang := d.language
	if lang == "" && e.languageNamer != nil && d.Path != "" {
		lang = e.languageNamer(d.Path)
	}
	if lang = strings.ToLower(lang); lang != "" {
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
		Command:  expandFile(command, d),
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

// formatLine returns the line of the formatted text that line of the text before it goes
// to. hunks are the line hunks between the two texts (diff.Lines). A line the formatter kept
// moves with the lines above it. A line in a run it changed goes to the line of the new run at
// the same offset, or to the run's last line when the new run is shorter, and a line in a run it
// removed goes to the line that follows the run.
func formatLine(hunks []diff.Hunk, line int) int {
	shift := 0
	for _, h := range hunks {
		if line < h.OldStart {
			break
		}
		if h.OldLines > 0 && line < h.OldStart+h.OldLines {
			if h.NewLines == 0 {
				return h.NewStart
			}
			return h.NewStart + min(line-h.OldStart, h.NewLines-1)
		}
		shift += h.NewLines - h.OldLines
	}
	return line + shift
}

// expandFile replaces $FILE in a formatter command with the path of d, quoted for the shell:
// prettier and ruff read the text on standard input and use the name to pick their parser. A
// document with no path gives its name.
func expandFile(command string, d *Document) string {
	if !strings.Contains(command, "$FILE") {
		return command
	}
	name := d.Path
	if name == "" {
		name = d.Name()
	}
	return strings.ReplaceAll(command, "$FILE", "'"+strings.ReplaceAll(name, "'", `'\''`)+"'")
}
