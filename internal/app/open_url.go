package app

// URL opening for plugins (gvim.url.open): argv-only process start, no shell. The URL is
// validated again here as defence in depth; the TUI host is the primary policy gate and
// asks the user to confirm before calling this.

import (
	"fmt"
	"net/url"
	"os/exec"
	goruntime "runtime"
	"strings"
)

// urlProcessStart is the actual process start; a test replaces it to record argv.
var urlProcessStart = func(name string, arg ...string) error {
	cmd := exec.Command(name, arg...)
	if err := cmd.Start(); err != nil {
		return err
	}
	// Reap the opener without blocking the update loop (this runs in a tea.Cmd goroutine).
	go func() { _ = cmd.Wait() }()
	return nil
}

// openURL starts the platform browser opener for a validated http(s) URL: 'open' on
// macOS and 'xdg-open' on Linux, with the URL as a single argv element. Unsupported
// platforms and any non-http(s) URL return an error; no shell is involved.
func openURL(raw string) error {
	return openURLOn(goruntime.GOOS, raw, urlProcessStart)
}

// openURLOn is openURL with an explicit platform and process starter (tests).
func openURLOn(goos, raw string, start func(name string, arg ...string) error) error {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" || u.Host == "" || u.User != nil || u.Opaque != "" {
		return fmt.Errorf("open url: refusing malformed URL")
	}
	switch strings.ToLower(u.Scheme) {
	case "http", "https":
	default:
		return fmt.Errorf("open url: refusing scheme %q", u.Scheme)
	}
	var name string
	switch goos {
	case "darwin":
		name = "open"
	case "linux":
		name = "xdg-open"
	default:
		return fmt.Errorf("open url: not supported on %s", goos)
	}
	if err := start(name, raw); err != nil {
		return fmt.Errorf("open url: %s: %w", name, err)
	}
	return nil
}
