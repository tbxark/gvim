package trust

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The spawn gate matrix: Fresh's documented rules (Trusted allows, Blocked denies
// every spawn, Restricted denies an executable inside the workspace) plus the
// PATH resolution and symlink checks the gvim host needs.

type gateFixture struct {
	root, outside string
	rootBin       string
	outsideBin    string
	pathSep       string
}

func newGateFixture(t *testing.T) *gateFixture {
	t.Helper()
	root := t.TempDir()
	outside := t.TempDir()
	f := &gateFixture{root: root, outside: outside, pathSep: string(filepath.ListSeparator)}
	f.rootBin = filepath.Join(root, "bin")
	f.outsideBin = filepath.Join(outside, "bin")
	mustMkdir(t, filepath.Join(root, "sub"))
	mustMkdir(t, filepath.Join(root, ".venv", "bin"))
	mustMkdir(t, f.rootBin)
	mustMkdir(t, f.outsideBin)
	return f
}

func mustMkdir(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
}

func mustExec(t *testing.T, path string) {
	t.Helper()
	if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o755); err != nil {
		t.Fatal(err)
	}
}

func (f *gateFixture) path(dirs ...string) string {
	return strings.Join(dirs, f.pathSep)
}

func TestDecideSpawnLevels(t *testing.T) {
	t.Parallel()
	f := newGateFixture(t)
	mustExec(t, filepath.Join(f.root, "gradlew"))
	project := []string{"./gradlew"}
	bare := []string{"git"}
	if err := DecideSpawn(Trusted, f.root, f.root, project, f.path(f.outsideBin)); err != nil {
		t.Errorf("Trusted denied a project executable: %v", err)
	}
	if err := DecideSpawn(Blocked, f.root, f.root, bare, f.path(f.outsideBin)); err == nil {
		t.Error("Blocked allowed a bare command")
	}
	if err := DecideSpawn(Blocked, f.root, f.root, nil, ""); err == nil {
		t.Error("Blocked allowed the terminal (empty argv)")
	}
	if err := DecideSpawn(Restricted, f.root, f.root, nil, ""); err != nil {
		t.Errorf("Restricted denied the terminal's shell probe: %v", err)
	}
	if err := DecideSpawn(Undecided, f.root, f.root, project, f.path(f.outsideBin)); err == nil {
		t.Error("Undecided (effective Restricted) allowed a project executable")
	}
	if err := DecideSpawn(Restricted, "", f.root, project, ""); err != nil {
		t.Errorf("no workspace root denied: %v", err)
	}
}

func TestDecideSpawnExplicitPaths(t *testing.T) {
	t.Parallel()
	f := newGateFixture(t)
	mustExec(t, filepath.Join(f.root, "gradlew"))
	mustExec(t, filepath.Join(f.root, "sub", "build.sh"))
	mustExec(t, filepath.Join(f.root, ".venv", "bin", "python"))
	mustExec(t, filepath.Join(f.outside, "tool"))

	tests := []struct {
		name string
		cwd  string
		argv []string
		deny bool
	}{
		{"relative project", f.root, []string{"./gradlew"}, true},
		{"relative dot dot", f.root, []string{"../outside/tool"}, false},
		{"nested relative project", f.root, []string{".venv/bin/python"}, true},
		{"absolute project", f.root, []string{filepath.Join(f.root, "gradlew")}, true},
		{"absolute outside", f.root, []string{filepath.Join(f.outside, "tool")}, false},
		{"relative from sub", filepath.Join(f.root, "sub"), []string{"./build.sh"}, true},
		{"relative escape from sub", filepath.Join(f.root, "sub"), []string{"../../outside/tool"}, false},
		{"empty cwd resolves against root", "", []string{"./gradlew"}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := DecideSpawn(Restricted, f.root, tt.cwd, tt.argv, "")
			if tt.deny && err == nil {
				t.Fatalf("allowed %v", tt.argv)
			}
			if !tt.deny && err != nil {
				t.Fatalf("denied %v: %v", tt.argv, err)
			}
		})
	}
}

func TestDecideSpawnSymlinks(t *testing.T) {
	t.Parallel()
	f := newGateFixture(t)
	mustExec(t, filepath.Join(f.root, "gradlew"))
	mustExec(t, filepath.Join(f.outside, "tool"))
	// A link inside the project pointing outside, and one outside pointing in.
	if err := os.Symlink(filepath.Join(f.outside, "tool"), filepath.Join(f.root, "link-out")); err != nil {
		t.Skipf("no symlinks: %v", err)
	}
	if err := os.Symlink(filepath.Join(f.root, "gradlew"), filepath.Join(f.outside, "link-in")); err != nil {
		t.Skipf("no symlinks: %v", err)
	}
	relOutside, err := filepath.Rel(f.root, filepath.Join(f.outside, "link-in"))
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name string
		argv []string
		deny bool
	}{
		{"project link to outside is still project code", []string{filepath.Join(f.root, "link-out")}, true},
		{"outside link to project is denied by its target", []string{filepath.Join(f.outside, "link-in")}, true},
		{"relative outside link", []string{relOutside}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := DecideSpawn(Restricted, f.root, f.root, tt.argv, "")
			if tt.deny && err == nil {
				t.Fatalf("allowed %v", tt.argv)
			}
			if !tt.deny && err != nil {
				t.Fatalf("denied %v: %v", tt.argv, err)
			}
		})
	}
}

func TestDecideSpawnPathResolution(t *testing.T) {
	t.Parallel()
	f := newGateFixture(t)
	mustExec(t, filepath.Join(f.root, "tool"))
	mustExec(t, filepath.Join(f.root, "gradlew"))
	mustExec(t, filepath.Join(f.rootBin, "tool"))
	mustExec(t, filepath.Join(f.outsideBin, "git"))
	mustExec(t, filepath.Join(f.outsideBin, "tool"))

	tests := []struct {
		name string
		cwd  string
		argv []string
		path string
		deny bool
	}{
		{"bare system tool", f.root, []string{"git"}, f.path(f.outsideBin), false},
		{"bare project tool via PATH", f.root, []string{"tool"}, f.path(f.rootBin), true},
		{"PATH order: project first", f.root, []string{"tool"}, f.path(f.rootBin, f.outsideBin), true},
		{"PATH order: outside first", f.root, []string{"tool"}, f.path(f.outsideBin, f.rootBin), false},
		{"empty PATH entry is cwd", f.root, []string{"tool"}, f.path("", f.outsideBin), true},
		{"empty PATH entry outside cwd", f.outside, []string{"tool"}, f.path(f.outsideBin, ""), false},
		{"relative PATH entry", f.root, []string{"tool"}, "bin", true},
		{"no entry resolvable is allowed", f.root, []string{"definitely-not-here"}, f.path(f.outsideBin), false},
		{"path value empty allows (nothing resolves)", f.root, []string{"tool"}, "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := DecideSpawn(Restricted, f.root, tt.cwd, tt.argv, tt.path)
			if tt.deny && err == nil {
				t.Fatalf("allowed %v with PATH %q", tt.argv, tt.path)
			}
			if !tt.deny && err != nil {
				t.Fatalf("denied %v with PATH %q: %v", tt.argv, tt.path, err)
			}
		})
	}
}

func TestDecideSpawnShellWrappedCommands(t *testing.T) {
	t.Parallel()
	f := newGateFixture(t)
	mustExec(t, filepath.Join(f.root, "gradlew"))
	mustExec(t, filepath.Join(f.rootBin, "gradlew"))
	mustExec(t, filepath.Join(f.outsideBin, "gradlew"))
	mustExec(t, filepath.Join(f.outsideBin, "sh"))
	mustExec(t, filepath.Join(f.rootBin, "sh"))

	tests := []struct {
		name string
		argv []string
		path string
		deny bool
	}{
		{"first token relative", []string{"sh", "-c", "./gradlew --help"}, f.path(f.outsideBin), true},
		{"first token PATH project", []string{"sh", "-c", "gradlew --help"}, f.path(f.rootBin), true},
		{"first token PATH outside", []string{"sh", "-c", "gradlew --help"}, f.path(f.outsideBin), false},
		{"plain command", []string{"sh", "-c", "echo hello"}, f.path(f.outsideBin), false},
		{"compound command after a safe first token is the documented residual", []string{"sh", "-c", "echo hi && ./gradlew"}, f.path(f.outsideBin), false},
		{"shell found in the project", []string{"sh", "-c", "echo hi"}, f.path(f.rootBin), true},
		{"env assignment prefix is not a path", []string{"sh", "-c", "PATH=bin tool"}, f.path(f.outsideBin), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := DecideSpawn(Restricted, f.root, f.root, tt.argv, tt.path)
			if tt.deny && err == nil {
				t.Fatalf("allowed %v with PATH %q", tt.argv, tt.path)
			}
			if !tt.deny && err != nil {
				t.Fatalf("denied %v with PATH %q: %v", tt.argv, tt.path, err)
			}
		})
	}
}

func TestSpawnRefusalCarriesTheReason(t *testing.T) {
	t.Parallel()
	f := newGateFixture(t)
	mustExec(t, filepath.Join(f.root, "gradlew"))
	err := DecideSpawn(Restricted, f.root, f.root, []string{"./gradlew"}, "")
	var refusal *SpawnRefusal
	if !errors.As(err, &refusal) {
		t.Fatalf("error = %v, want a *SpawnRefusal", err)
	}
	if refusal.Level != Restricted || refusal.Root != f.root || refusal.Path != "./gradlew" {
		t.Errorf("refusal = %+v", refusal)
	}
	if !errors.Is(err, ErrSpawnDenied) {
		t.Error("refusal does not unwrap to ErrSpawnDenied")
	}
	if got := err.Error(); !strings.Contains(got, "Restricted") || !strings.Contains(got, "trust the folder") {
		t.Errorf("message = %q", got)
	}
	blocked := DecideSpawn(Blocked, f.root, f.root, nil, "")
	if got := blocked.Error(); !strings.Contains(got, "Blocked") {
		t.Errorf("blocked message = %q", got)
	}
}

func TestResolveSpawnPath(t *testing.T) {
	t.Parallel()
	f := newGateFixture(t)
	mustExec(t, filepath.Join(f.rootBin, "tool"))
	mustExec(t, filepath.Join(f.root, "local"))
	if got, ok := ResolveSpawnPath(f.root, "tool", f.path(f.rootBin)); !ok || got != filepath.Join(f.rootBin, "tool") {
		t.Errorf("PATH entry = %q, %v", got, ok)
	}
	if got, ok := ResolveSpawnPath(f.root, "local", string(filepath.ListSeparator)); !ok || got != filepath.Join(f.root, "local") {
		t.Errorf("empty entry = %q, %v", got, ok)
	}
	if _, ok := ResolveSpawnPath(f.root, "tool", ""); ok {
		t.Error("an empty PATH resolved a name")
	}
	if _, ok := ResolveSpawnPath(f.root, "sub/tool", f.path(f.rootBin)); ok {
		t.Error("a path was resolved as a bare name")
	}
}
