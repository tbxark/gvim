package editor

import (
	"errors"
	"testing"

	"github.com/tbxark/gvim/internal/buffer"
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
		name     string
		text     string
		lang     string
		sel      *buffer.Selection
		wantText string
		wantErr  error
	}{
		{
			name:     "the whole text goes through the formatter of its language",
			text:     "b\na",
			lang:     "go",
			wantText: "B\nA",
		},
		{
			name:     "a selection does not limit the formatter",
			text:     "b\na",
			lang:     "go",
			sel:      &buffer.Selection{Anchor: buffer.Pos{}, Head: buffer.Pos{Col: 1}},
			wantText: "B\nA",
		},
		{
			name:     "a language with no formatter is refused",
			text:     "b\na",
			lang:     "rust",
			wantText: "b\na",
			wantErr:  ErrNoFormatter,
		},
		{
			name:     "a buffer with no language is refused",
			text:     "b\na",
			lang:     "",
			wantText: "b\na",
			wantErr:  ErrNoFormatter,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			e := formatDoc(tt.text, tt.lang, formatters)
			if tt.sel != nil {
				e.SelectRange(tt.sel.Anchor, tt.sel.Head)
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
		name       string
		file       string // the file the buffer is opened from, or "" for an untitled buffer
		lang       string // the language set with SetLanguage, or "" for none
		formatters map[string]string
		wantText   string
		wantErr    error
	}{
		{
			name:       "an opened file gets the formatter of its extension",
			file:       "a.go",
			formatters: map[string]string{".go": upper},
			wantText:   "HELLO\n",
		},
		{
			name:       "the extension is matched without case",
			file:       "A.GO",
			formatters: map[string]string{".go": upper},
			wantText:   "HELLO\n",
		},
		{
			name:       "a formatter key with capitals is matched without case",
			file:       "a.go",
			formatters: map[string]string{".Go": upper},
			wantText:   "HELLO\n",
		},
		{
			name:       "the language name is matched without case",
			lang:       "Go",
			formatters: map[string]string{"go": upper},
			wantText:   "HELLO\n",
		},
		{
			name:       "a language key with capitals is matched without case",
			lang:       "go",
			formatters: map[string]string{"Go": upper},
			wantText:   "HELLO\n",
		},
		{
			name:       "a language with no formatter falls back to the extension",
			file:       "a.go",
			lang:       "rust",
			formatters: map[string]string{".go": upper},
			wantText:   "HELLO\n",
		},
		{
			name:       "the language formatter wins over the extension",
			file:       "a.go",
			lang:       "go",
			formatters: map[string]string{"go": upper, ".go": "cat"},
			wantText:   "HELLO\n",
		},
		{
			name:       "a language key does not match the extension",
			file:       "a.go",
			formatters: map[string]string{"go": upper},
			wantText:   "hello\n",
			wantErr:    ErrNoFormatter,
		},
		{
			name:       "a file with no extension has no formatter",
			file:       "Makefile",
			formatters: map[string]string{".go": upper},
			wantText:   "hello\n",
			wantErr:    ErrNoFormatter,
		},
		{
			name:       "an untitled buffer with no language has no formatter",
			formatters: map[string]string{".go": upper},
			wantText:   "hello\n",
			wantErr:    ErrNoFormatter,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
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
