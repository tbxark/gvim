package trust

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// mkFile writes a regular file at dir/name, creating the parent directory.
func mkFile(t *testing.T, dir, name string) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestExecutableContentMarkers(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		setup func(t *testing.T, dir string)
		want  []string
	}{
		{
			name:  "a plain folder has nothing to gate",
			setup: func(t *testing.T, dir string) { mkFile(t, dir, "notes.txt") },
		},
		{
			name:  "an env file",
			setup: func(t *testing.T, dir string) { mkFile(t, dir, ".envrc") },
			want:  []string{".envrc"},
		},
		{
			name: "a virtualenv directory",
			setup: func(t *testing.T, dir string) {
				if err := os.Mkdir(filepath.Join(dir, ".venv"), 0o755); err != nil {
					t.Fatal(err)
				}
			},
			want: []string{".venv"},
		},
		{
			name:  "a project manifest",
			setup: func(t *testing.T, dir string) { mkFile(t, dir, "go.mod") },
			want:  []string{"go.mod"},
		},
		{
			name: "a dev container config",
			setup: func(t *testing.T, dir string) {
				mkFile(t, dir, filepath.Join(".devcontainer", "devcontainer.json"))
			},
			want: []string{"devcontainer.json"},
		},
		{
			name:  "a .NET project file",
			setup: func(t *testing.T, dir string) { mkFile(t, dir, "App.csproj") },
			want:  []string{"App.csproj"},
		},
		{
			name: "the order is env, manifests, dev container, .NET files by name",
			setup: func(t *testing.T, dir string) {
				mkFile(t, dir, "App.csproj")
				mkFile(t, dir, "Zed.sln")
				mkFile(t, dir, "mise.toml")
				mkFile(t, dir, ".envrc")
				mkFile(t, dir, ".devcontainer.json")
				mkFile(t, dir, "package.json")
			},
			want: []string{".envrc", "mise.toml", "package.json", "devcontainer.json", "App.csproj", "Zed.sln"},
		},
		{
			name: "a project manifest must be a file, a directory does not count",
			setup: func(t *testing.T, dir string) {
				if err := os.Mkdir(filepath.Join(dir, "go.mod"), 0o755); err != nil {
					t.Fatal(err)
				}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			tt.setup(t, dir)
			got := ExecutableContentMarkers(dir)
			if !slices.Equal(got, tt.want) {
				t.Fatalf("ExecutableContentMarkers = %v, want %v", got, tt.want)
			}
			if has := HasExecutableContent(dir); has != (len(tt.want) > 0) {
				t.Fatalf("HasExecutableContent = %v, want %v", has, len(tt.want) > 0)
			}
		})
	}
}

func TestExecutableContentMarkersOfMissingFolder(t *testing.T) {
	t.Parallel()
	if got := ExecutableContentMarkers(filepath.Join(t.TempDir(), "missing")); len(got) != 0 {
		t.Fatalf("ExecutableContentMarkers(missing) = %v, want none", got)
	}
}
