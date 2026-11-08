package editor

import "testing"

func TestScanLineIndexReportsTheLineCount(t *testing.T) {
	t.Parallel()
	tests := []struct {
		Name       string
		Text       string
		wantStatus string
	}{
		{Name: "three lines", Text: "a\nb\nc", wantStatus: "Line index built: 3 lines"},
		{Name: "a single line", Text: "a", wantStatus: "Line index built: 1 line"},
		{Name: "a text ending in a break counts the empty last line", Text: "a\n", wantStatus: "Line index built: 2 lines"},
	}
	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			t.Parallel()
			e := docWith(Options{}, tt.Text)
			run(t, e, "scan_line_index")
			if e.Status() != tt.wantStatus {
				t.Fatalf("status = %q, want %q", e.Status(), tt.wantStatus)
			}
			if got := e.Active().Buffer.Text(); got != tt.Text {
				t.Fatalf("scan changed the text to %q", got)
			}
		})
	}
}
