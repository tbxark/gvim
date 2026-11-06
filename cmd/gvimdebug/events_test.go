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
