package editor

import "testing"

func TestScanLineIndexReportsTheLineCount(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		text       string
		wantStatus string
	}{
		{name: "three lines", text: "a\nb\nc", wantStatus: "Line index built: 3 lines"},
		{name: "a single line", text: "a", wantStatus: "Line index built: 1 line"},
		{name: "a text ending in a break counts the empty last line", text: "a\n", wantStatus: "Line index built: 2 lines"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			e := docWith(Options{}, tt.text)
			run(t, e, "scan_line_index")
			if e.Status() != tt.wantStatus {
				t.Fatalf("status = %q, want %q", e.Status(), tt.wantStatus)
			}
			if got := e.Active().Buffer.Text(); got != tt.text {
				t.Fatalf("scan changed the text to %q", got)
			}
		})
	}
}
