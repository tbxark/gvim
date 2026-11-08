package app

import (
	"os/exec"
	"strings"
	"testing"

	"github.com/tbxark/gvim/config"
)

// found is a lookPath that finds every program in have.
func found(have ...string) func(string) (string, error) {
	return func(name string) (string, error) {
		for _, h := range have {
			if h == name {
				return "/bin/" + name, nil
			}
		}
		return "", exec.ErrNotFound
	}
}

func TestDefaultFormattersFollowFreshsTable(t *testing.T) {
	all := found("gofmt", "rustfmt", "prettier", "ruff", "clang-format")
	got := formattersFor(config.Defaults(), all)
	tests := []struct{ key, want string }{
		{"go", "gofmt"},
		{".go", "gofmt"},
		{"rust", "rustfmt --edition 2021"},
		{"python", "ruff format --stdin-filename $FILE"},
		{".py", "ruff format --stdin-filename $FILE"},
		{"javascript", "prettier --stdin-filepath $FILE"},
		{".ts", "prettier --stdin-filepath $FILE"},
		{"typescript", "prettier --stdin-filepath $FILE"},
		{"json", "prettier --stdin-filepath $FILE"},
		{".jsonc", "prettier --stdin-filepath $FILE"},
		{"yaml", "prettier --stdin-filepath $FILE"},
		{".yml", "prettier --stdin-filepath $FILE"},
		{"c", "clang-format"},
		{"c++", "clang-format"},
		{".cpp", "clang-format"},
	}
	for _, tt := range tests {
		if got[tt.key] != tt.want {
			t.Errorf("formatter of %q = %q, want %q", tt.key, got[tt.key], tt.want)
		}
	}
	for _, none := range []string{"zig", "markdown", ".md", "toml", "html"} {
		if c, ok := got[none]; ok {
			t.Errorf("%q has the formatter %q, but Fresh's table has none", none, c)
		}
	}
}

// TestDefaultFormattersNeedTheirProgram checks that a formatter whose program is not on PATH is not
// registered, so that Format Buffer falls back to the language server instead of failing.
func TestDefaultFormattersNeedTheirProgram(t *testing.T) {
	got := formattersFor(config.Defaults(), found("gofmt"))
	if got["go"] != "gofmt" {
		t.Errorf("go = %q, want gofmt", got["go"])
	}
	for _, key := range []string{"rust", "python", ".ts", "c"} {
		if c, ok := got[key]; ok {
			t.Errorf("%q has the formatter %q although its program is missing", key, c)
		}
	}
}

func TestUserFormattersWinOverTheDefaults(t *testing.T) {
	all := found("gofmt", "prettier")
	for _, key := range []string{"go", "Go", ".go"} {
		cfg := config.Defaults()
		cfg.Formatters = map[string]string{key: "my-fmt"}
		got := formattersFor(cfg, all)
		if got[key] != "my-fmt" {
			t.Errorf("user key %q lost: %q", key, got[key])
		}
		// Every default key of the language is gone, so that none shadows the user's.
		for _, other := range []string{"go", ".go"} {
			if _, own := cfg.Formatters[other]; !own && got[other] != "" {
				t.Errorf("default %q = %q is kept next to the user's %q", other, got[other], key)
			}
		}
		if got["typescript"] != "prettier --stdin-filepath $FILE" {
			t.Errorf("the user's go formatter removed typescript's default")
		}
	}
}

// TestGofmtIsTheDefaultFormatBufferOfGo runs the path of the shipped binary: with gofmt on PATH,
// Format Buffer on a Go buffer formats it and ends the text with a newline.
func TestGofmtIsTheDefaultFormatBufferOfGo(t *testing.T) {
	if _, err := exec.LookPath("gofmt"); err != nil {
		t.Skip("gofmt is not on PATH")
	}
	isolateConfig(t)
	rt, err := buildRuntime(t.Context(), projectRoot(t, ``), false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(rt.close)
	rt.ed.Do("select_all", nil)
	if r := rt.ed.Do("insert_char", map[string]any{"text": "package   a"}); r.Err != nil {
		t.Fatal(r.Err)
	}
	if r := rt.ed.Do("set_language", map[string]any{"name": "go"}); r.Err != nil {
		t.Fatal(r.Err)
	}
	if r := rt.ed.Do("format_buffer", nil); r.Err != nil {
		t.Fatalf("format_buffer: %v", r.Err)
	}
	if got := rt.ed.Active().Buffer.Text(); got != "package a\n" {
		t.Fatalf("text = %q, want gofmt's output with its final newline", got)
	}
	if strings.Contains(rt.ed.Status(), "no formatter") {
		t.Fatalf("status = %q", rt.ed.Status())
	}
}
