package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// TestFontServesTheFileGiven checks that /font serves the -font file, and that without one
// there is no font to serve.
func TestFontServesTheFileGiven(t *testing.T) {
	path := filepath.Join(t.TempDir(), "screen.ttf")
	const data = "font bytes"
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name string
		font string
		code int
		body string
	}{
		{"with a font", path, http.StatusOK, data},
		{"without a font", "", http.StatusNotFound, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := &debugger{font: tt.font, state: "stopped"}
			rec := httptest.NewRecorder()
			d.routes().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "http://127.0.0.1/font", nil))
			if rec.Code != tt.code {
				t.Fatalf("status = %d, want %d", rec.Code, tt.code)
			}
			if tt.code == http.StatusOK && rec.Body.String() != tt.body {
				t.Errorf("body = %q, want %q", rec.Body.String(), tt.body)
			}
		})
	}
}
