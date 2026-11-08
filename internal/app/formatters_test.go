package app

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/tbxark/gvim/internal/editor"
)

// TestConfiguredFormatterRunsFromFormatBuffer checks the shipped path end to end: the
// formatters key of the project config reaches the editor that buildRuntime builds, and
// Format Buffer runs the formatter of the buffer's language through the editor's Do.
func TestConfiguredFormatterRunsFromFormatBuffer(t *testing.T) {
	isolateConfig(t)
	tests := []struct {
		name       string
		body       string // the project config file, empty for none
		lang       string // the language set on the buffer, empty for none
		wantText   string
		wantErr    error
		wantStatus string
	}{
		{
			name:       "the formatter of the language changes the buffer",
			body:       `{"formatters": {"go": "tr a-z A-Z"}}`,
			lang:       "go",
			wantText:   "HELLO\n",
			wantStatus: "Formatted buffer",
		},
		{
			name:       "no formatter configured reports that none is configured",
			body:       ``,
			lang:       "zig", // Fresh's table has no formatter for it, unlike go
			wantText:   "hello\n",
			wantErr:    editor.ErrNoFormatter,
			wantStatus: "format_buffer: no formatter configured for zig",
		},
		{
			name:       "a formatter of another language is not used",
			body:       `{"formatters": {"rust": "tr a-z A-Z"}}`,
			lang:       "zig",
			wantText:   "hello\n",
			wantErr:    editor.ErrNoFormatter,
			wantStatus: "format_buffer: no formatter configured for zig",
		},
		{
			name:       "a buffer with no language has no formatter",
			body:       `{"formatters": {"go": "tr a-z A-Z"}}`,
			wantText:   "hello\n",
			wantErr:    editor.ErrNoFormatter,
			wantStatus: "format_buffer: no formatter configured for this buffer",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rt, err := buildRuntime(t.Context(), projectRoot(t, tt.body), false)
			if err != nil {
				t.Fatalf("buildRuntime() error = %v", err)
			}
			t.Cleanup(rt.close)
			if tt.lang != "" {
				if r := rt.ed.Do("set_language", map[string]any{"name": tt.lang}); r.Err != nil {
					t.Fatalf("set_language: %v", r.Err)
				}
			}
			r := rt.ed.Do("format_buffer", nil)
			if !errors.Is(r.Err, tt.wantErr) {
				t.Fatalf("format_buffer err = %v, want %v", r.Err, tt.wantErr)
			}
			if got := rt.ed.Active().Buffer.Text(); got != tt.wantText {
				t.Fatalf("text = %q, want %q", got, tt.wantText)
			}
			if got := rt.ed.Status(); got != tt.wantStatus {
				t.Fatalf("status = %q, want %q", got, tt.wantStatus)
			}
		})
	}
}

// TestConfiguredFormatterFindsTheLanguageOfAnOpenedFile checks a language-name key on a file
// opened from disk, with no language set on the buffer: "go" in formatters runs for x.go, as
// docs/lsp.md says the key can be a name.
func TestConfiguredFormatterFindsTheLanguageOfAnOpenedFile(t *testing.T) {
	isolateConfig(t)
	root := t.TempDir()
	file := filepath.Join(root, "x.go")
	writeTestFile(t, file, "hello\n")
	writeTestFile(t, filepath.Join(root, ".gvim", "config.json"), `{"formatters": {"go": "tr a-z A-Z"}}`)
	rt, err := buildRuntime(t.Context(), Options{Root: root, Files: []FileTarget{{Path: file}}}, false)
	if err != nil {
		t.Fatalf("buildRuntime() error = %v", err)
	}
	t.Cleanup(rt.close)
	if r := rt.ed.Do("format_buffer", nil); r.Err != nil {
		t.Fatalf("format_buffer err = %v", r.Err)
	}
	if got := rt.ed.Active().Buffer.Text(); got != "HELLO\n" {
		t.Fatalf("text = %q, want %q", got, "HELLO\n")
	}
}
