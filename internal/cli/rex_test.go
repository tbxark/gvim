package cli

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/tbxark/gvim/internal/rexplugin"
)

// okRex is a rex that accepts every command.
func okRex(args ...string) (string, error) { return "", nil }

// TestRexInstallCommand prints where it wrote and how to use the integration.
func TestRexInstallCommand(t *testing.T) {
	isolateHome(t)
	t.Setenv("REX_CONFIG", "")
	cmd := newRexCommandWith(rexplugin.Installer{Rex: okRex, Bin: "/bin/gvim"})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"install"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("rex install: %v", err)
	}
	for _, want := range []string{"Installed gvim's Rex integration.", "Rex plugin", "Rex config", "reloaded", "Gvim: Open File…"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output lacks %q:\n%s", want, out.String())
		}
	}
}

// TestRexUninstallCommand prints the config it cleaned.
func TestRexUninstallCommand(t *testing.T) {
	isolateHome(t)
	t.Setenv("REX_CONFIG", "")
	installer := rexplugin.Installer{Rex: okRex, Bin: "/bin/gvim"}
	if _, err := installer.Install(); err != nil {
		t.Fatalf("Install: %v", err)
	}
	cmd := newRexCommandWith(installer)
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"uninstall"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("rex uninstall: %v", err)
	}
	if !strings.Contains(out.String(), "Removed gvim's Rex integration.") {
		t.Errorf("output = %q", out.String())
	}
}

// TestRexInstallReportsAFailure keeps the usage text out of a failed install.
func TestRexInstallReportsAFailure(t *testing.T) {
	isolateHome(t)
	t.Setenv("REX_CONFIG", "")
	installer := rexplugin.Installer{
		Rex: func(args ...string) (string, error) { return "bad config", errors.New("bad config") },
		Bin: "/bin/gvim",
	}
	cmd := newRexCommandWith(installer)
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"install"})
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "bad config") {
		t.Fatalf("rex install error = %v", err)
	}
	if strings.Contains(out.String(), "Usage:") {
		t.Errorf("a failed install printed the usage text:\n%s", out.String())
	}
}
