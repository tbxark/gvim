package shellcmd

import (
	"strings"
	"testing"
	"time"
)

// TestRunEnvAppliesTheSpawnOverlay: the active project environment's set-only
// overlay reaches the shell and its children.
func TestRunEnvAppliesTheSpawnOverlay(t *testing.T) {
	out, err := RunEnv(t.TempDir(), `printf %s "$GVIM_ENV_TEST"`, "", 5*time.Second, []string{"GVIM_ENV_TEST=activated"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(out) != "activated" {
		t.Fatalf("output = %q, want the overlay value", out)
	}
}

// TestRunArgsEnvAppliesTheSpawnOverlay: a no-shell spawn (git grep style) gets
// the overlay too.
func TestRunArgsEnvAppliesTheSpawnOverlay(t *testing.T) {
	out, code, err := RunArgsEnv(t.TempDir(), []string{"sh", "-c", `printf %s "$GVIM_ENV_TEST"`}, "", 5*time.Second, []string{"GVIM_ENV_TEST=activated"})
	if err != nil || code != 0 {
		t.Fatalf("run = %v, code %d", err, code)
	}
	if out != "activated" {
		t.Fatalf("output = %q", out)
	}
}
