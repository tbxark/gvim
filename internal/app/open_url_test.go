package app

import (
	"errors"
	"strings"
	"testing"
)

// TestOpenURLOnUsesArgvOnlyOpener checks the platform opener is started with the URL as
// ONE argv element (no shell), the platform mapping, unsupported platforms, and that a
// non-http(s) URL never reaches the starter.
func TestOpenURLOnUsesArgvOnlyOpener(t *testing.T) {
	var gotName string
	var gotArgs []string
	start := func(name string, args ...string) error {
		gotName, gotArgs = name, args
		return nil
	}
	if err := openURLOn("darwin", "https://example.com/a?b=1", start); err != nil {
		t.Fatal(err)
	}
	if gotName != "open" || len(gotArgs) != 1 || gotArgs[0] != "https://example.com/a?b=1" {
		t.Fatalf("darwin opener = %q %q, want open with one URL arg", gotName, gotArgs)
	}
	if err := openURLOn("linux", "http://127.0.0.1/x", start); err != nil {
		t.Fatal(err)
	}
	if gotName != "xdg-open" || len(gotArgs) != 1 || gotArgs[0] != "http://127.0.0.1/x" {
		t.Fatalf("linux opener = %q %q", gotName, gotArgs)
	}
	// Unsupported platform and unsafe URL: no process started.
	gotName, gotArgs = "", nil
	if err := openURLOn("windows", "https://example.com", start); err == nil || gotName != "" {
		t.Fatalf("windows: err=%v name=%q, want refusal without a start", err, gotName)
	}
	for _, raw := range []string{"javascript:alert(1)", "file:///etc/passwd", "https://u:p@example.com", "not a url"} {
		if err := openURLOn("darwin", raw, start); err == nil {
			t.Errorf("openURLOn(%q) succeeded, want refusal", raw)
		}
	}
	if gotName != "" || gotArgs != nil {
		t.Fatalf("unsafe URL started %q %q", gotName, gotArgs)
	}
}

// TestOpenURLOnReportsStartFailure checks a failed start is returned with context.
func TestOpenURLOnReportsStartFailure(t *testing.T) {
	boom := errors.New("no opener")
	err := openURLOn("darwin", "https://example.com", func(string, ...string) error { return boom })
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v, want the start error", err)
	}
	if !strings.Contains(err.Error(), "open") {
		t.Fatalf("err = %v, want the opener name", err)
	}
}
