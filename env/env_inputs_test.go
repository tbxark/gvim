package env

import (
	"strings"
	"testing"
)

func TestEnvInputRootNames(t *testing.T) {
	files := EnvInputFiles()
	if len(files) == 0 || files[0] != ".envrc" {
		t.Fatalf("EnvInputFiles = %v", files)
	}
	files[0] = "mutated"
	if EnvInputFiles()[0] != ".envrc" {
		t.Fatal("EnvInputFiles aliases the internal list")
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
