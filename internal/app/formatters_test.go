package app

import (
	"errors"
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
			lang:       "go",
			wantText:   "hello\n",
			wantErr:    editor.ErrNoFormatter,
			wantStatus: "format_buffer: no formatter configured for go",
		},
		{
			name:       "a formatter of another language is not used",
			body:       `{"formatters": {"rust": "tr a-z A-Z"}}`,
			lang:       "go",
			wantText:   "hello\n",
			wantErr:    editor.ErrNoFormatter,
			wantStatus: "format_buffer: no formatter configured for go",
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
