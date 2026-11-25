package env

import (
	"strings"
	"testing"
)

func TestEnvInputRootNames(t *testing.T) {
	if len(envInputFiles) == 0 || envInputFiles[0] != ".envrc" {
		t.Fatalf("envInputFiles = %v", envInputFiles)
	}
	got := strings.Join(EnvInputRootNames(), ",")
	want := ".envrc,mise.toml,.mise.toml,.tool-versions,pyvenv.cfg,.venv"
	if got != want {
		t.Fatalf("EnvInputRootNames = %q, want %q", got, want)
	}
	if nested := strings.Join(EnvInputNestedDirs(), ","); nested != ".venv" {
		t.Fatalf("EnvInputNestedDirs = %q, want .venv", nested)
	}
}
