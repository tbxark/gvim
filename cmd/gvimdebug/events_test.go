package main

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// TestEventsPushStateChanges checks that an open event stream gets each change as soon
// as it happens, without polling.
func TestEventsPushStateChanges(t *testing.T) {
	d := &debugger{state: "stopped"}
	srv := httptest.NewServer(d.routes())
	defer srv.Close()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/events", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	events := readEvents(resp.Body)

	next := func() pageState {
		t.Helper()
		select {
		case msg, ok := <-events:
			if !ok {
				t.Fatal("event stream ended")
			}
			var st pageState
			if err := json.Unmarshal([]byte(msg), &st); err != nil {
				t.Fatalf("event %q is not a page state: %v", msg, err)
			}
			return st
		case <-time.After(5 * time.Second):
			t.Fatal("no event within 5s")
		}
		return pageState{}
	}

	if got := next(); got.Status != "stopped" {
		t.Fatalf("first event status = %q, want %q", got.Status, "stopped")
	}
	d.setState("building")
	if got := next(); got.Status != "building" {
		t.Fatalf("status after setState = %q, want %q", got.Status, "building")
	}
}

// readEvents sends the data of each server-sent event on the returned channel. The
// channel closes when the body ends.
func readEvents(body io.Reader) <-chan string {
	ch := make(chan string)
	go func() {
		defer close(ch)
		sc := bufio.NewScanner(body)
		for sc.Scan() {
			if data, ok := strings.CutPrefix(sc.Text(), "data: "); ok {
				ch <- data
			}
		}
	}()
	return ch
}

func TestRoutesRefuseCrossSiteAndRebindingRequests(t *testing.T) {
	h := (&debugger{}).routes()
	for _, tc := range []struct {
		name   string
		host   string
		header map[string]string
		want   int
	}{
		{"a rebound domain", "evil.example:8765", nil, http.StatusForbidden},
		{"another site's page", "127.0.0.1:8765", map[string]string{"Sec-Fetch-Site": "cross-site"}, http.StatusForbidden},
	} {
		req := httptest.NewRequest(http.MethodPost, "http://"+tc.host+"/status", nil)
		req.Host = tc.host
		for k, v := range tc.header {
			req.Header.Set(k, v)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != tc.want {
			t.Errorf("%s: status %d, want %d", tc.name, rec.Code, tc.want)
		}
	}
	// The page itself, at the loopback address, is served.
	req := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8765/status", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("loopback GET /status = %d, want 200", rec.Code)
	}
}
