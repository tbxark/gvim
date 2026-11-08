package manager

import (
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/tbxark/gvim/lsp"
)

// TestHelperProcess is the "language server" that the start tests launch: the test binary
// itself, told what to do by an environment variable. It is not a test.
func TestHelperProcess(t *testing.T) {
	switch os.Getenv("GVIM_LSP_HELPER") {
	case "":
		return
	case "die-with-stderr":
		fmt.Fprint(os.Stderr, "bad flag: --serve\nusage: fake-server [options]\n")
		os.Exit(3)
	}
}

func TestDefaultsMirrorFreshsTable(t *testing.T) {
	// The rows of the table in docs/features/lsp.md of Fresh, with the program each runs.
	tests := []struct {
		language, command string
		args              []string
	}{
		{"rust", "rust-analyzer", nil},
		{"go", "gopls", nil},
		{"typescript", "typescript-language-server", []string{"--stdio"}},
		{"javascript", "typescript-language-server", []string{"--stdio"}},
		{"python", "pylsp", nil},
		{"java", "jdtls", nil},
		{"zig", "zls", nil},
		{"latex", "texlab", nil},
		{"markdown", "marksman", []string{"server"}},
		{"c", "clangd", nil},
		{"cpp", "clangd", nil},
		{"kotlin", "kotlin-lsp", []string{"--stdio"}},
		{"odin", "ols", nil},
		{"yaml", "yaml-language-server", []string{"--stdio"}},
		{"toml", "taplo", []string{"lsp", "stdio"}},
		{"lua", "lua-language-server", nil},
		// From populate_lsp_config of Fresh: svls and asm-lsp serve two languages each.
		{"verilog", "svls", nil},
		{"systemverilog", "svls", nil},
		{"asm", "asm-lsp", nil},
		{"gas", "asm-lsp", nil},
		{"nasm", "asm-lsp", nil}, // the name gvim's detection gives NASM
	}
	for _, tt := range tests {
		t.Run(tt.language, func(t *testing.T) {
			s, ok := DefaultFor(tt.language)
			if !ok {
				t.Fatalf("no default for %s", tt.language)
			}
			if s.Command != tt.command || !slices.Equal(s.Args, tt.args) {
				t.Errorf("default = %s %v, want %s %v", s.Command, s.Args, tt.command, tt.args)
			}
			if s.Install == "" {
				t.Errorf("no install hint for %s", tt.language)
			}
		})
	}
}

func TestDefaultsAreConsistent(t *testing.T) {
	seen := map[string]string{}
	for _, s := range Defaults() {
		if s.Language == "" || s.Command == "" || s.Install == "" {
			t.Errorf("incomplete entry %+v", s)
		}
		if s.Language != strings.ToLower(s.Language) {
			t.Errorf("%s: names are lower case", s.Language)
		}
		for _, name := range s.Names() {
			if name != strings.ToLower(name) {
				t.Errorf("%s: alias %q is not lower case", s.Language, name)
			}
			if other, dup := seen[name]; dup {
				t.Errorf("%q names both %s and %s", name, other, s.Language)
			}
			seen[name] = s.Language
		}
	}
	// Only the servers Fresh keeps off by default are manual.
	for _, s := range Defaults() {
		if want := s.Language == "odin" || s.Language == "gdscript"; s.Manual != want {
			t.Errorf("%s: Manual = %v, want %v", s.Language, s.Manual, want)
		}
	}
}

func TestLanguageKeyResolvesTheNamesOfGvimsDetection(t *testing.T) {
	tests := map[string]string{
		"Go": "go", "C++": "cpp", "c#": "csharp", "TeX": "latex", "Protocol Buffer": "protobuf",
		"F#": "fsharp", "Bash": "bash", "Shell": "bash", "react": "jsx", "TSX": "tsx", "Text": "text",
	}
	for in, want := range tests {
		if got := LanguageKey(in); got != want {
			t.Errorf("LanguageKey(%q) = %q, want %q", in, got, want)
		}
	}
	if s, _ := DefaultFor("tsx"); s.LanguageID != "typescriptreact" {
		t.Errorf("tsx languageId = %q, want typescriptreact", s.LanguageID)
	}
	if _, ok := DefaultFor("Text"); ok {
		t.Error("plain text has a default server")
	}
}

func TestManagerNamesTheServerThatIsNotInstalled(t *testing.T) {
	m := NewManager(mgrRoot, map[string][]ServerConfig{
		"go": {{Command: []string{"gvim-no-such-gopls"}, Install: "go install golang.org/x/tools/gopls@latest", AutoStart: true}},
	}, nil)
	err := m.Open(mgrAPath, "go", "package a")
	var ni *lsp.NotInstalledError
	if !errors.As(err, &ni) {
		t.Fatalf("Open error = %v, want a *lsp.NotInstalledError", err)
	}
	if ni.Language != "go" || ni.Install != "go install golang.org/x/tools/gopls@latest" {
		t.Errorf("error = %+v, want the language and the install hint", ni)
	}
	for _, want := range []string{"gvim-no-such-gopls is not installed", "language server for go", "go install golang.org/x/tools/gopls@latest"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("message %q lacks %q", err.Error(), want)
		}
	}
	if !lsp.IsNotInstalled(err) || lsp.IsNotInstalled(errors.New("other")) {
		t.Error("IsNotInstalled does not tell the two apart")
	}
}

func TestManagerExplainsAServerThatDiesDuringTheHandshake(t *testing.T) {
	t.Setenv("GVIM_LSP_HELPER", "die-with-stderr")
	m := NewManager(mgrRoot, map[string][]ServerConfig{
		"go": {{Command: []string{os.Args[0], "-test.run=^TestHelperProcess$"}, Name: "gopls", AutoStart: true}},
	}, nil)
	err := m.Open(mgrAPath, "go", "package a")
	var se *lsp.StartError
	if !errors.As(err, &se) {
		t.Fatalf("Open error = %v, want a *lsp.StartError", err)
	}
	for _, want := range []string{"gopls", "language server for go", "initialize", "-test.run", "bad flag: --serve"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("message %q lacks %q", err.Error(), want)
		}
	}
	if se.Stderr == "" || strings.Contains(se.Stderr, "\n") {
		t.Errorf("stderr tail = %q, want the lines of the output on one line", se.Stderr)
	}
}

func TestDefaultsCarryFreshsRootMarkers(t *testing.T) {
	tests := []struct {
		language string
		want     []string
	}{
		{"go", []string{"go.mod", "go.work", ".git"}},
		{"rust", []string{"Cargo.toml", "rust-project.json", ".git"}},
		{"typescript", []string{"tsconfig.json", "jsconfig.json", "package.json", ".git"}},
		{"verilog", []string{".svls.toml", ".git"}},
		{"asm", []string{".asm-lsp.toml", ".git"}},
		{"zig", nil}, // Fresh gives it no markers: it runs in the workspace root
	}
	for _, tt := range tests {
		s, ok := DefaultFor(tt.language)
		if !ok || !slices.Equal(s.RootMarkers, tt.want) {
			t.Errorf("%s: root markers = %v (found %v), want %v", tt.language, s.RootMarkers, ok, tt.want)
		}
	}
}
