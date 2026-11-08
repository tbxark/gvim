package main

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/tbxark/gvim/internal/app"
	"github.com/tbxark/gvim/internal/cli"
)

// TestHelpListsCommandTree checks the wiring from main to the command tree
// with the production runner. --help exits before any program starts.
func TestHelpListsCommandTree(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := cli.Execute(context.Background(), []string{"--help"}, &stdout, &stderr, app.Run)
	if code != 0 {
		t.Fatalf("exit = %d, stderr = %q", code, stderr.String())
	}
	for _, want := range []string{"version", "config", "grammar", "completion", "--keymap", "--log-file"} {
		if !strings.Contains(stdout.String(), want) {
			t.Errorf("help output misses %q:\n%s", want, stdout.String())
		}
	}
}
