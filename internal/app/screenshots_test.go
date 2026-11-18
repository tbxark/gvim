package app

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/tbxark/gvim/internal/uitest"
)

// TestWriteScreenshots writes PNG frames of the real UI in the common states,
// for a multimodal model to review. It runs only when GVIM_SHOTS names a
// directory, which is created if needed:
//
//	GVIM_SHOTS=/tmp/gvim-shots go test -run TestWriteScreenshots ./internal/app/
func TestWriteScreenshots(t *testing.T) {
	dir := os.Getenv("GVIM_SHOTS")
	if dir == "" {
		t.Skip("GVIM_SHOTS is not set; set it to a directory to write screenshots")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	root := shotProject(t)
	a := filepath.Join(root, "a.txt")
	shot := func(t *testing.T, s *uitest.Session, name string) {
		t.Helper()
		if err := s.Screenshot(filepath.Join(dir, name+".png")); err != nil {
			t.Fatalf("screenshot %s: %v", name, err)
		}
	}
	// after presses key and waits until the screen has changed from before.
	after := func(s *uitest.Session, key string) {
		t.Helper()
		before := s.Screen().String()
		s.Key(key)
		s.WaitFor(func(sc uitest.Screen) bool { return sc.String() != before })
	}

	t.Run("01-welcome", func(t *testing.T) {
		s := shotSession(t, Options{})
		shot(t, s, "01-welcome")
	})
	t.Run("02-one-file", func(t *testing.T) {
		s := shotSession(t, Options{Files: []FileTarget{{Path: a}}})
		shot(t, s, "02-one-file")
	})
	t.Run("03-explorer", func(t *testing.T) {
		s := shotSession(t, Options{Root: root, Files: []FileTarget{{Path: a}}})
		shot(t, s, "03-explorer")
	})
	t.Run("04-menu", func(t *testing.T) {
		s := shotSession(t, Options{Files: []FileTarget{{Path: a}}})
		s.Key("f10")
		s.WaitFor(func(sc uitest.Screen) bool { return sc.Contains("New File") })
		shot(t, s, "04-menu")
	})
	t.Run("05-palette", func(t *testing.T) {
		s := shotSession(t, Options{Root: root, Files: []FileTarget{{Path: a}}})
		after(s, "ctrl+p")
		shot(t, s, "05-palette")
	})
	t.Run("06-command-palette", func(t *testing.T) {
		s := shotSession(t, Options{Files: []FileTarget{{Path: a}}})
		after(s, "ctrl+t")
		shot(t, s, "06-command-palette")
	})
	t.Run("07-find", func(t *testing.T) {
		s := shotSession(t, Options{Files: []FileTarget{{Path: a}}})
		after(s, "ctrl+f")
		shot(t, s, "07-find")
	})
	t.Run("08-save-as", func(t *testing.T) {
		s := shotSession(t, Options{})
		after(s, "ctrl+n")
		after(s, "ctrl+s")
		s.WaitFor(func(sc uitest.Screen) bool { return sc.Contains("Save As") })
		shot(t, s, "08-save-as")
	})
	t.Run("09-unsaved", func(t *testing.T) {
		s := shotSession(t, Options{Files: []FileTarget{{Path: a}}})
		s.Type("X")
		s.Key("ctrl+w")
		s.WaitFor(func(sc uitest.Screen) bool { return sc.Contains("Unsaved Changes") })
		shot(t, s, "09-unsaved")
	})
	t.Run("10-twelve-tabs", func(t *testing.T) {
		var files []FileTarget
		for i := range 12 {
			p := filepath.Join(root, fmt.Sprintf("chapter-%02d-long-descriptive-name.txt", i+1))
			if err := os.WriteFile(p, []byte("text\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			files = append(files, FileTarget{Path: p})
		}
		s := shotSession(t, Options{Files: files})
		shot(t, s, "10-twelve-tabs")
	})

	goFile := filepath.Join(root, "src/handler.go")
	goSrc := `package main

import (
	"fmt"
	"strings"
)

// RenderSummary builds a formatted status line for the editor buffer.
func RenderSummary(name string, count int) string {
	if count <= 0 {
		return "empty"
	}
	var b strings.Builder
	for i := range count {
		fmt.Fprintf(&b, "%s:%d ", name, i+1)
	}
	return strings.TrimSpace(b.String())
}

func main() {
	fmt.Println(RenderSummary("gvim", 3))
}
`
	if err := os.WriteFile(goFile, []byte(goSrc), 0o600); err != nil {
		t.Fatal(err)
	}

	t.Run("11-welcome-gopher", func(t *testing.T) {
		s := shotSessionSize(t, Options{Theme: "gopher", Root: root}, 120, 34)
		s.WaitFor(func(sc uitest.Screen) bool { return sc.Contains("New File") })
		shot(t, s, "11-welcome-gopher")
	})
	t.Run("12-editor-split-gopher", func(t *testing.T) {
		s := shotSessionSize(t, Options{Theme: "gopher", Root: root, Files: []FileTarget{{Path: goFile}}}, 120, 34)
		s.WaitFor(func(sc uitest.Screen) bool { return sc.Contains("RenderSummary") })
		// Fold RenderSummary body or split vertically and run search to verify contrast and layout.
		after(s, "ctrl+\\")
		s.Key("ctrl+f")
		s.WaitFor(func(sc uitest.Screen) bool { return sc.Contains("Search:") })
		s.Type("count")
		s.WaitFor(func(sc uitest.Screen) bool { return sc.Contains("Match 1 of 3") })
		shot(t, s, "12-editor-split-gopher")
	})
	t.Run("13-quick-open-gopher", func(t *testing.T) {
		s := shotSessionSize(t, Options{Theme: "gopher", Root: root, Files: []FileTarget{{Path: goFile}}}, 120, 34)
		s.Key("ctrl+p")
		s.WaitFor(func(sc uitest.Screen) bool { return sc.Contains("file · >command") })
		s.Key("backspace")
		s.Type("handler.go:10")
		s.WaitFor(func(sc uitest.Screen) bool {
			return !sc.Contains("README.md") && sc.Contains(" 9 │ func RenderSummary") && sc.Contains("10 │")
		})
		shot(t, s, "13-quick-open-gopher")
	})
	t.Run("14-about-gopher", func(t *testing.T) {
		s := shotSessionSize(t, Options{Theme: "gopher", Files: []FileTarget{{Path: goFile}}}, 120, 34)
		s.Key("ctrl+shift+p")
		s.WaitFor(func(sc uitest.Screen) bool { return sc.Contains("Commands") })
		s.Type("About gvim")
		s.WaitFor(func(sc uitest.Screen) bool { return sc.Contains("Help: About gvim") })
		s.Key("enter")
		s.WaitFor(func(sc uitest.Screen) bool { return sc.Contains("Pure Go") })
		shot(t, s, "14-about-gopher")
	})
}

// shotSession runs the real UI, as buildModel builds it, at 100 by 30.
func shotSession(t *testing.T, opts Options) *uitest.Session {
	return shotSessionSize(t, opts, 100, 30)
}

func shotSessionSize(t *testing.T, opts Options, w, h int) *uitest.Session {
	t.Helper()
	isolateConfig(t)
	m, files, err := buildModel(t.Context(), opts)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { files.Close() })
	return uitest.Start(t, m, w, h)
}

// shotProject writes a small project tree for the explorer and returns its root.
func shotProject(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	files := map[string]string{
		"a.txt":            "hello from gvim\nsecond line of text\n",
		"README.md":        "# Project\n",
		"src/main.go":      "package main\n",
		"src/util/util.go": "package util\n",
		"docs/notes.md":    "notes\n",
	}
	for rel, text := range files {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return root
}
