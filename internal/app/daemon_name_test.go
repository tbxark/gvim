package app

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/tbxark/gvim/internal/daemon"
)

func TestAttachDaemonNameResolvesWorkspaceRoot(t *testing.T) {
	isolateConfig(t)
	root := t.TempDir()
	link := filepath.Join(t.TempDir(), "workspace-link")
	if err := os.Symlink(root, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	resolved, err := resolveRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	got, err := attachDaemonName(Options{Root: link})
	if err != nil {
		t.Fatal(err)
	}
	if want := daemon.DefaultName(resolved); got != want {
		t.Fatalf("attach name = %q; daemon name = %q", got, want)
	}
}

func TestAttachDaemonNameExplicitDoesNotRequireRoot(t *testing.T) {
	got, err := attachDaemonName(Options{Root: filepath.Join(t.TempDir(), "missing"), Daemon: &DaemonOptions{Name: "named"}})
	if err != nil || got != "named" {
		t.Fatalf("attachDaemonName = %q, %v; want named, nil", got, err)
	}
}
