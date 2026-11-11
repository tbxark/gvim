package editor

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/tbxark/gvim/buffer"
)

// formatDoc returns an editor with one document holding text, in language lang,
// and the formatters given.
func formatDoc(text, lang string, formatters map[string]string) *Editor {
	e := docWith(Options{}, text)
	e.SetFormatters(formatters)
	e.SetLanguage(lang)
	return e
}

func TestFormatBufferRunsTheLanguageFormatter(t *testing.T) {
	t.Parallel()
	formatters := map[string]string{"go": "tr a-z A-Z"}
	tests := []struct {
		Name     string
		Text     string
		lang     string
		Sel      *buffer.Selection
		wantText string
		wantErr  error
	}{
		{
			Name:     "the whole text goes through the formatter of its language",
			Text:     "b\na",
			lang:     "go",
			wantText: "B\nA",
		},
		{
			Name:     "a selection does not limit the formatter",
			Text:     "b\na",
			lang:     "go",
			Sel:      &buffer.Selection{Anchor: buffer.Pos{}, Head: buffer.Pos{Col: 1}},
			wantText: "B\nA",
		},
		{
			Name:     "a language with no formatter is refused",
			Text:     "b\na",
			lang:     "rust",
			wantText: "b\na",
			wantErr:  ErrNoFormatter,
		},
		{
			Name:     "a buffer with no language is refused",
			Text:     "b\na",
			lang:     "",
			wantText: "b\na",
			wantErr:  ErrNoFormatter,
		},
	}
	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			t.Parallel()
			e := formatDoc(tt.Text, tt.lang, formatters)
			if tt.Sel != nil {
				e.SelectRange(tt.Sel.Anchor, tt.Sel.Head)
			}
			r := e.Do("format_buffer", nil)
			if !errors.Is(r.Err, tt.wantErr) {
				t.Fatalf("format_buffer err = %v, want %v", r.Err, tt.wantErr)
			}
			if got := e.Active().Buffer.Text(); got != tt.wantText {
				t.Fatalf("text = %q, want %q", got, tt.wantText)
			}
		})
	}
}

func TestFormatBufferReportsStatusAndFailure(t *testing.T) {
	t.Parallel()
	t.Run("success", func(t *testing.T) {
		t.Parallel()
		e := formatDoc("b", "go", map[string]string{"go": "tr a-z A-Z"})
		run(t, e, "format_buffer")
		if want := "Formatted buffer"; e.Status() != want {
			t.Fatalf("status = %q, want %q", e.Status(), want)
		}
	})
	t.Run("a failing formatter leaves the text alone", func(t *testing.T) {
		t.Parallel()
		e := formatDoc("b", "go", map[string]string{"go": "exit 3"})
		if r := e.Do("format_buffer", nil); r.Err == nil {
			t.Fatal("format_buffer with a failing formatter returned no error")
		}
		if got := e.Active().Buffer.Text(); got != "b" {
			t.Fatalf("text = %q, want it unchanged", got)
		}
	})
}

func TestFormatBufferIsOneUndoStep(t *testing.T) {
	t.Parallel()
	e := formatDoc("b\na", "go", map[string]string{"go": "tr a-z A-Z"})
	run(t, e, "format_buffer", "undo")
	if got := e.Active().Buffer.Text(); got != "b\na" {
		t.Fatalf("after undo text = %q, want the original", got)
	}
}

func TestFormatBufferRefusedOnReadOnlyDocument(t *testing.T) {
	t.Parallel()
	e := formatDoc("b", "go", map[string]string{"go": "tr a-z A-Z"})
	run(t, e, "toggle_read_only")
	if r := e.Do("format_buffer", nil); !errors.Is(r.Err, ErrReadOnly) {
		t.Fatalf("err = %v, want ErrReadOnly", r.Err)
	}
	if _, err := e.BeginShell("format_buffer", nil); !errors.Is(err, ErrReadOnly) {
		t.Fatalf("BeginShell err = %v, want ErrReadOnly", err)
	}
	if got := e.Active().Buffer.Text(); got != "b" {
		t.Fatalf("text = %q, want it unchanged", got)
	}
}

// TestBeginShellFormatsTheWholeText checks the split the UI uses: BeginShell
// prepares the job, RunShellJob runs it, and EndShell applies the output.
func TestBeginShellFormatsTheWholeText(t *testing.T) {
	t.Parallel()
	e := formatDoc("b\na", "go", map[string]string{"go": "tr a-z A-Z"})
	e.SelectRange(buffer.Pos{Line: 1}, buffer.Pos{Line: 1, Col: 1})
	job, err := e.BeginShell("format_buffer", nil)
	if err != nil {
		t.Fatalf("BeginShell: %v", err)
	}
	if job.Input != "b\na" || !job.Replace {
		t.Fatalf("job input = %q replace = %t, want the whole text and a replace", job.Input, job.Replace)
	}
	out, runErr := RunShellJob(job)
	if runErr != nil {
		t.Fatalf("RunShellJob: %v", runErr)
	}
	if r := e.EndShell(job, out, runErr); r.Err != nil {
		t.Fatalf("EndShell: %v", r.Err)
	}
	if got := e.Active().Buffer.Text(); got != "B\nA" {
		t.Fatalf("text = %q, want %q", got, "B\nA")
	}
	if want := "Formatted buffer"; e.Status() != want {
		t.Fatalf("status = %q, want %q", e.Status(), want)
	}
}

func TestSetFormattersCopiesTheMap(t *testing.T) {
	t.Parallel()
	m := map[string]string{"go": "tr a-z A-Z"}
	e := formatDoc("b", "go", m)
	m["go"] = "exit 9"
	delete(m, "go")
	run(t, e, "format_buffer")
	if got := e.Active().Buffer.Text(); got != "B" {
		t.Fatalf("text = %q, want the formatter given when it was set", got)
	}
}

// TestFormatBufferLooksUpTheFormatter checks which formatter a buffer gets. The language name
// is tried first, then the file extension with its dot, and both are matched without regard to
// case. A file opened from disk has no language, so its extension is what finds its formatter.
func TestFormatBufferLooksUpTheFormatter(t *testing.T) {
	t.Parallel()
	const upper = "tr a-z A-Z"
	tests := []struct {
		Name       string
		file       string // the file the buffer is opened from, or "" for an untitled buffer
		lang       string // the language set with SetLanguage, or "" for none
		formatters map[string]string
		wantText   string
		wantErr    error
	}{
		{
			Name:       "an opened file gets the formatter of its extension",
			file:       "a.go",
			formatters: map[string]string{".go": upper},
			wantText:   "HELLO\n",
		},
		{
			Name:       "the extension is matched without case",
			file:       "A.GO",
			formatters: map[string]string{".go": upper},
			wantText:   "HELLO\n",
		},
		{
			Name:       "a formatter key with capitals is matched without case",
			file:       "a.go",
			formatters: map[string]string{".Go": upper},
			wantText:   "HELLO\n",
		},
		{
			Name:       "the language name is matched without case",
			lang:       "Go",
			formatters: map[string]string{"go": upper},
			wantText:   "HELLO\n",
		},
		{
			Name:       "a language key with capitals is matched without case",
			lang:       "go",
			formatters: map[string]string{"Go": upper},
			wantText:   "HELLO\n",
		},
		{
			Name:       "a language with no formatter falls back to the extension",
			file:       "a.go",
			lang:       "rust",
			formatters: map[string]string{".go": upper},
			wantText:   "HELLO\n",
		},
		{
			Name:       "the language formatter wins over the extension",
			file:       "a.go",
			lang:       "go",
			formatters: map[string]string{"go": upper, ".go": "cat"},
			wantText:   "HELLO\n",
		},
		{
			Name:       "a language key does not match the extension",
			file:       "a.go",
			formatters: map[string]string{"go": upper},
			wantText:   "hello\n",
			wantErr:    ErrNoFormatter,
		},
		{
			Name:       "a file with no extension has no formatter",
			file:       "Makefile",
			formatters: map[string]string{".go": upper},
			wantText:   "hello\n",
			wantErr:    ErrNoFormatter,
		},
		{
			Name:       "an untitled buffer with no language has no formatter",
			formatters: map[string]string{".go": upper},
			wantText:   "hello\n",
			wantErr:    ErrNoFormatter,
		},
	}
	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			t.Parallel()
			e := docWith(Options{}, "hello\n")
			if tt.file != "" {
				e = openNamed(t, tt.file, "hello\n")
			}
			e.SetFormatters(tt.formatters)
			if tt.lang != "" {
				e.SetLanguage(tt.lang)
			}
			if got := e.HasFormatter(); got != (tt.wantErr == nil) {
				t.Fatalf("HasFormatter = %t, want %t", got, tt.wantErr == nil)
			}
			r := e.Do("format_buffer", nil)
			if !errors.Is(r.Err, tt.wantErr) {
				t.Fatalf("format_buffer err = %v, want %v", r.Err, tt.wantErr)
			}
			if got := text(e); got != tt.wantText {
				t.Fatalf("text = %q, want %q", got, tt.wantText)
			}
		})
	}
}

// TestFormatterCommandKnowsTheFileName checks that $FILE in a formatter command is the path of
// the document, quoted for the shell, which is how prettier and ruff pick their parser.
func TestFormatterCommandKnowsTheFileName(t *testing.T) {
	t.Parallel()
	dir := filepath.Join(t.TempDir(), "a b")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		Path, Want string
	}{
		{filepath.Join(dir, "it's.ts"), filepath.Join(dir, "it's.ts") + "\n"},
		{filepath.Join(dir, "plain.ts"), filepath.Join(dir, "plain.ts") + "\n"},
		{"", "[No Name]\n"},
	}
	for _, tt := range tests {
		e := docWith(Options{}, "x")
		e.Active().Path = tt.Path
		cmd := "cat >/dev/null; printf '%s\\n' $FILE"
		e.SetFormatters(map[string]string{"text": cmd})
		e.SetLanguage("text")
		if r := e.Do("format_buffer", nil); r.Err != nil {
			t.Fatalf("%q: format_buffer: %v", tt.Path, r.Err)
		}
		if got := e.Active().Buffer.Text(); got != tt.Want {
			t.Errorf("%q: $FILE gave %q, want %q", tt.Path, got, tt.Want)
		}
	}
}

// TestFormatterFindsTheLanguageOfAFileFromItsName checks that a formatters key given as a
// language name finds the formatter of a file opened with no language set: the editor asks
// the namer the app gives it for the name of the file's language ("Go" for x.go), as the
// highlighter names it. docs/lsp.md says the key can be a name, so "go" must match x.go.
func TestFormatterFindsTheLanguageOfAFileFromItsName(t *testing.T) {
	t.Parallel()
	const upper = "tr a-z A-Z"
	goNamer := func(path string) string {
		if filepath.Ext(path) == ".go" {
			return "Go"
		}
		return ""
	}
	tests := []struct {
		Name       string
		file       string
		namer      func(string) string
		formatters map[string]string
		Want       bool
	}{
		{
			Name:       "a language name key matches an opened file of that language",
			file:       "x.go",
			namer:      goNamer,
			formatters: map[string]string{"go": upper},
			Want:       true,
		},
		{
			Name:       "a language name key is matched without case",
			file:       "x.go",
			namer:      goNamer,
			formatters: map[string]string{"GO": upper},
			Want:       true,
		},
		{
			Name:       "a language name key of another language does not match",
			file:       "x.go",
			namer:      goNamer,
			formatters: map[string]string{"rust": upper},
			Want:       false,
		},
		{
			Name:       "a file the namer gives no language has no formatter by name",
			file:       "x.txt",
			namer:      goNamer,
			formatters: map[string]string{"go": upper},
			Want:       false,
		},
		{
			Name:       "without a namer the name key cannot match a file with no language",
			file:       "x.go",
			formatters: map[string]string{"go": upper},
			Want:       false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			t.Parallel()
			e := openNamed(t, tt.file, "hello\n")
			e.SetLanguageNamer(tt.namer)
			e.SetFormatters(tt.formatters)
			if got := e.HasFormatter(); got != tt.Want {
				t.Fatalf("HasFormatter = %t, want %t", got, tt.Want)
			}
			if !tt.Want {
				return
			}
			if r := e.Do("format_buffer", nil); r.Err != nil {
				t.Fatalf("format_buffer: %v", r.Err)
			}
			if got := text(e); got != "HELLO\n" {
				t.Fatalf("text = %q, want %q", got, "HELLO\n")
			}
		})
	}
}

// TestLanguageNamerIsNotCalledForALanguageSetWithSetLanguage checks that a language set with
// SetLanguage wins, so the namer is only a fallback for a document with no language.
func TestLanguageNamerIsNotCalledForALanguageSetWithSetLanguage(t *testing.T) {
	t.Parallel()
	e := openNamed(t, "x.go", "hello\n")
	e.SetLanguageNamer(func(string) string { return "Rust" })
	e.SetFormatters(map[string]string{"go": "tr a-z A-Z", "rust": "tr a-z A-Z"})
	e.SetLanguage("go")
	if !e.HasFormatter() {
		t.Fatal("HasFormatter = false, want the formatter of the language set with SetLanguage")
	}
}

// TestFormatBufferKeepsTheCaretOnItsText checks that an external formatter which adds or
// removes lines above the caret leaves the caret on its text, as docs/lsp.md says for Format
// Buffer. A caret on a line the formatter kept keeps its column.
func TestFormatBufferKeepsTheCaretOnItsText(t *testing.T) {
	t.Parallel()
	tests := []struct {
		Name      string
		Text      string
		caret     buffer.Pos
		formatter string
		wantText  string
		Want      buffer.Pos
	}{
		{
			Name:      "a blank line removed above the caret moves it up",
			Text:      "a\n\n\nb\n",
			caret:     buffer.Pos{Line: 3},
			formatter: `tr -s '\n'`,
			wantText:  "a\nb\n",
			Want:      buffer.Pos{Line: 1},
		},
		{
			Name:      "lines removed above the caret move it up and keep its column",
			Text:      "package main\n\n\n\nfunc main() {\n\tx := 1\n}\n",
			caret:     buffer.Pos{Line: 5, Col: 2},
			formatter: `sed '/^$/d'`,
			wantText:  "package main\nfunc main() {\n\tx := 1\n}\n",
			Want:      buffer.Pos{Line: 2, Col: 2},
		},
		{
			Name:      "a line added above the caret moves it down",
			Text:      "a\nb\n",
			caret:     buffer.Pos{Line: 1, Col: 1},
			formatter: `printf 'x\n'; cat`,
			wantText:  "x\na\nb\n",
			Want:      buffer.Pos{Line: 2, Col: 1},
		},
		{
			Name:      "a caret above the change stays where it is",
			Text:      "a\n\n\nb\n",
			caret:     buffer.Pos{Line: 0, Col: 1},
			formatter: `tr -s '\n'`,
			wantText:  "a\nb\n",
			Want:      buffer.Pos{Line: 0, Col: 1},
		},
		{
			Name:      "a caret on a line the formatter changes keeps its column",
			Text:      "hello\nworld\n",
			caret:     buffer.Pos{Line: 1, Col: 2},
			formatter: `tr a-z A-Z`,
			wantText:  "HELLO\nWORLD\n",
			Want:      buffer.Pos{Line: 1, Col: 2},
		},
		{
			Name:      "a caret on a removed line goes to the line that follows it",
			Text:      "a\n\nb\n",
			caret:     buffer.Pos{Line: 1},
			formatter: `sed '/^$/d'`,
			wantText:  "a\nb\n",
			Want:      buffer.Pos{Line: 1},
		},
	}
	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			t.Parallel()
			e := formatDoc(tt.Text, "go", map[string]string{"go": tt.formatter})
			e.Active().Cursor = tt.caret
			if r := e.Do("format_buffer", nil); r.Err != nil {
				t.Fatalf("format_buffer: %v", r.Err)
			}
			if got := text(e); got != tt.wantText {
				t.Fatalf("text = %q, want %q", got, tt.wantText)
			}
			if got := e.Active().Cursor; got != tt.Want {
				t.Fatalf("caret = %v, want %v", got, tt.Want)
			}
		})
	}
}

func TestFormatBufferWithNoChangeEditsNothing(t *testing.T) {
	t.Parallel()
	e := formatDoc("b\na", "go", map[string]string{"go": "cat"})
	before := e.Active().Buffer.Version()
	run(t, e, "format_buffer")
	if after := e.Active().Buffer.Version(); after != before {
		t.Fatalf("an unchanged format edited the buffer: version %v -> %v", before, after)
	}
	if want := "Formatted buffer"; e.Status() != want {
		t.Fatalf("status = %q, want %q", e.Status(), want)
	}
}
