package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestPreviewLineWindow(t *testing.T) {
	path := previewFixture(t, "view.txt", "\ufefffirst\r\n\t界e\u0301xy\r\nlast")
	rec := &recorder{}
	code, stdout, stderr := execute(t, []string{
		"preview", "--skip", "1", "--lines", "2", "--width", "11",
		"--line-numbers", "--tab-size", "2", "--color", "never", "--", path,
	}, rec)
	if code != 0 || stderr != "" || rec.calls != 0 {
		t.Fatalf("exit = %d, stderr = %q, editor calls = %d", code, stderr, rec.calls)
	}
	if want := "  2    界e\u0301x\n  3  last\n"; stdout != want {
		t.Fatalf("stdout = %q, want %q", stdout, want)
	}
}

func TestPreviewOversizedLine(t *testing.T) {
	prefix := strings.Repeat("x", (32<<10)-1)
	path := previewFixture(t, "long.txt", prefix+"界"+strings.Repeat("x", 128<<10)+"\r\nafter\n")
	for _, tt := range []struct {
		name, skip, want string
	}{
		{"truncate before a partial rune", "0", prefix + "…\n"},
		{"drain before the next line", "1", "after\n"},
		{"range past EOF", "2", ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			code, stdout, stderr := execute(t, []string{
				"preview", "--color=never", "--skip=" + tt.skip, "--lines=1", path,
			}, &recorder{})
			if code != 0 || stderr != "" {
				t.Fatalf("exit = %d, stderr = %q", code, stderr)
			}
			if stdout != tt.want {
				t.Fatalf("stdout length/tail = %d/%q, want %d/%q", len(stdout), stdout[max(0, len(stdout)-32):], len(tt.want), tt.want[max(0, len(tt.want)-32):])
			}
		})
	}
}

func TestPreviewSanitizesFileText(t *testing.T) {
	path := previewFixture(t, "controls.txt", "\ufeffA\x1b[2J\a\x00B\u009b\xff\n")
	code, stdout, stderr := execute(t, []string{"preview", "--color=never", path}, &recorder{})
	if code != 0 || stderr != "" || stdout != "A[2JB\ufffd\n" {
		t.Fatalf("exit = %d, stdout = %q, stderr = %q", code, stdout, stderr)
	}
}

func TestPreviewHighlightsWithContextAndUserTheme(t *testing.T) {
	isolateHome(t)
	t.Setenv("NO_COLOR", "1")
	path := previewFixture(t, "context.go", "package main\n/* comment\nvar inside\n*/\nvar outside = 1\n")
	cfg := previewFixture(t, "config.json", `{"theme":"terminal"}`)
	code, stdout, stderr := execute(t, []string{
		"preview", "--config", cfg, "--skip=2", "--lines=3", "--color=always", path,
	}, &recorder{})
	if code != 0 || stderr != "" {
		t.Fatalf("exit = %d, stderr = %q", code, stderr)
	}
	if got, want := ansi.Strip(stdout), "var inside\n*/\nvar outside = 1\n"; got != want {
		t.Fatalf("visible stdout = %q, want %q", got, want)
	}
	if !strings.Contains(stdout, "\x1b[90mvar inside\x1b[") || !strings.Contains(stdout, "\x1b[31mvar\x1b[") {
		t.Fatalf("stdout does not color the skipped comment body and the Go keyword with the selected theme: %q", stdout)
	}
}

func TestPreviewAutoColorOnRedirectedOutput(t *testing.T) {
	t.Setenv("CLICOLOR_FORCE", "0")
	path := previewFixture(t, "main.go", "package main\n")
	code, stdout, stderr := execute(t, []string{"preview", path}, &recorder{})
	if code != 0 || stderr != "" || stdout != "package main\n" {
		t.Fatalf("exit = %d, stdout = %q, stderr = %q", code, stdout, stderr)
	}
}

func TestPreviewPreservesLinesAcrossHighlightWindows(t *testing.T) {
	isolateHome(t)
	source := "package main\n" + strings.Repeat("// leading line\n", 61) +
		"/*\ncomment before the boundary\ncomment after the boundary\n*/\nvar outside = 1\n"
	path := previewFixture(t, "windows.go", source)
	code, stdout, stderr := execute(t, []string{
		"preview", "--color=always", "--theme=terminal", path,
	}, &recorder{})
	if code != 0 || stderr != "" {
		t.Fatalf("exit = %d, stderr = %q", code, stderr)
	}
	if got := ansi.Strip(stdout); got != source {
		t.Fatalf("emitted source differs at the window boundary: got %d bytes, want %d", len(got), len(source))
	}
	if !strings.Contains(stdout, "\x1b[90mcomment after the boundary\x1b[") {
		t.Fatalf("comment color was lost at the window boundary: %q", stdout)
	}
}

func TestPreviewRejectsInvalidOptionsAndNonRegularFiles(t *testing.T) {
	dir := t.TempDir()
	for _, tt := range []struct {
		flag, message string
	}{
		{"--lines=-1", "must be non-negative"},
		{"--tab-size=0", "--tab-size must be between 1 and 32"},
		{"--color=on", "--color must be auto, always, or never"},
		{"--color=never", "not a regular file"},
	} {
		t.Run(tt.flag, func(t *testing.T) {
			rec := &recorder{}
			code, stdout, stderr := execute(t, []string{"preview", tt.flag, dir}, rec)
			if code != 1 || stdout != "" || !strings.Contains(stderr, tt.message) || rec.calls != 0 {
				t.Fatalf("exit = %d, stdout = %q, stderr = %q, editor calls = %d", code, stdout, stderr, rec.calls)
			}
		})
	}
}

func previewFixture(t *testing.T, name, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}
