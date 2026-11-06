package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestResizeAcceptsOnlySizesInRange checks that a valid size is kept, and that an invalid
// one is refused without changing the size the page shows.
func TestResizeAcceptsOnlySizesInRange(t *testing.T) {
	d := &debugger{cols: 140, rows: 40, state: "stopped"}
	srv := httptest.NewServer(d.routes())
	defer srv.Close()

	tests := []struct {
		name string
		body string
		code int
	}{
		{"valid size", `{"cols":120,"rows":30}`, http.StatusNoContent},
		{"too narrow", `{"cols":5,"rows":30}`, http.StatusBadRequest},
		{"too tall", `{"cols":120,"rows":1000}`, http.StatusBadRequest},
		{"not JSON", `cols=120`, http.StatusBadRequest},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res, err := srv.Client().Post(srv.URL+"/resize", "application/json", strings.NewReader(tt.body))
			if err != nil {
				t.Fatal(err)
			}
			_ = res.Body.Close()
			if res.StatusCode != tt.code {
				t.Fatalf("status = %d, want %d", res.StatusCode, tt.code)
			}
		})
	}

	// Only the valid size changed the state. The refused sizes left it alone.
	if st := d.snapshot(); st.Cols != 120 || st.Rows != 30 {
		t.Fatalf("size after the requests = %dx%d, want 120x30", st.Cols, st.Rows)
	}
}
