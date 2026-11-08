package uiadapter

import (
	"os"
	"path/filepath"
	goruntime "runtime"
	"slices"
	"strings"
	"testing"
)

func TestClipboardWriteCommands(t *testing.T) {
	tests := []struct {
		goos string
		want [][]string
	}{
		{goos: "darwin", want: [][]string{{"pbcopy"}}},
		{goos: "linux", want: [][]string{
			{"wl-copy"},
			{"xclip", "-selection", "clipboard", "-in"},
			{"xsel", "--clipboard", "--input"},
			{"clip.exe"},
		}},
		{goos: "windows", want: [][]string{{"clip.exe"}}},
		{goos: "plan9", want: nil},
	}
	for _, tt := range tests {
		t.Run(tt.goos, func(t *testing.T) {
			if got := clipboardWriteCommands(tt.goos); !slices.EqualFunc(got, tt.want, slices.Equal[[]string]) {
				t.Fatalf("clipboardWriteCommands(%q) = %q, want %q", tt.goos, got, tt.want)
			}
		})
	}
}

// TestSystemClipboardWritePipesTheTextToTheTool puts a stand-in tool first on PATH and
// checks that Write hands it the text on stdin.
func TestSystemClipboardWritePipesTheTextToTheTool(t *testing.T) {
	cmds := clipboardWriteCommands(goruntime.GOOS)
	if goruntime.GOOS == "windows" || len(cmds) == 0 {
		t.Skip("the stand-in tool is a shell script")
	}
	dir, out := t.TempDir(), filepath.Join(t.TempDir(), "clip.txt")
	script := "#!/bin/sh\n/bin/cat > '" + out + "'\n"
	if err := os.WriteFile(filepath.Join(dir, cmds[0][0]), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	if err := (SystemClipboard{}).Write("a\nb é"); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "a\nb é" {
		t.Fatalf("the tool got %q", got)
	}
}

func TestSystemClipboardWriteFailsWithoutATool(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	if err := (SystemClipboard{}).Write("x"); err == nil {
		t.Fatal("Write with no tool on PATH returned nil")
	}
}

// TestSystemClipboardWriteTriesTheNextToolAfterAFailure puts a failing wl-copy and a working
// xclip on PATH and checks that Write falls through to xclip.
func TestSystemClipboardWriteTriesTheNextToolAfterAFailure(t *testing.T) {
	if goruntime.GOOS == "windows" {
		t.Skip("the stand-in tools are shell scripts")
	}
	dir, out := t.TempDir(), filepath.Join(t.TempDir(), "clip.txt")
	tools := map[string]string{
		"wl-copy": "#!/bin/sh\nexit 1\n",
		"xclip":   "#!/bin/sh\n/bin/cat > '" + out + "'\n",
	}
	for name, script := range tools {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(script), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", dir)
	if err := writeClipboard([][]string{{"wl-copy"}, {"xclip"}}, "x"); err != nil {
		t.Fatalf("Write = %v, want xclip to take over", err)
	}
	if got, err := os.ReadFile(out); err != nil || string(got) != "x" {
		t.Fatalf("xclip got %q, %v", got, err)
	}
}

func TestSystemClipboardWriteReturnsTheLastErrorWhenEveryToolFails(t *testing.T) {
	if goruntime.GOOS == "windows" {
		t.Skip("the stand-in tools are shell scripts")
	}
	dir := t.TempDir()
	for _, name := range []string{"wl-copy", "xclip"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/sh\nexit 1\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", dir)
	err := writeClipboard([][]string{{"wl-copy"}, {"xclip"}}, "x")
	if err == nil || !strings.HasPrefix(err.Error(), "xclip:") {
		t.Fatalf("Write = %v, want the error of the last tool tried", err)
	}
}

func TestClipboardInput(t *testing.T) {
	tests := []struct {
		name, tool, text string
		want             []byte
	}{
		{name: "utf-8 for pbcopy", tool: "pbcopy", text: "aé", want: []byte("aé")},
		{name: "utf-16le with a BOM for clip.exe", tool: "clip.exe", text: "aé", want: []byte{0xFF, 0xFE, 'a', 0, 0xE9, 0}},
		{name: "clip.exe surrogate pair", tool: "clip.exe", text: "😀", want: []byte{0xFF, 0xFE, 0x3D, 0xD8, 0x00, 0xDE}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := clipboardInput(tt.tool, tt.text); !slices.Equal(got, tt.want) {
				t.Fatalf("clipboardInput(%q, %q) = %x, want %x", tt.tool, tt.text, got, tt.want)
			}
		})
	}
}

func TestClipboardHTMLTools(t *testing.T) {
	tests := []struct {
		goos  string
		argv0 []string
		input string
	}{
		{"darwin", []string{"osascript", "-"}, "set the clipboard to {«class utf8»:«data utf8" + "6126" + "», «class HTML»:«data HTML" + "3c623e" + "»}\n"},
		{"linux", []string{"wl-copy", "--type", "text/html"}, "<b>"},
		{"windows", nil, ""},
	}
	for _, tt := range tests {
		t.Run(tt.goos, func(t *testing.T) {
			tools := clipboardHTMLTools(tt.goos, "<b>", "a&")
			if tt.argv0 == nil {
				if len(tools) != 0 {
					t.Fatalf("tools = %v, want none", tools)
				}
				return
			}
			if len(tools) == 0 || !slices.Equal(tools[0].argv, tt.argv0) || string(tools[0].input) != tt.input {
				t.Fatalf("first tool = %+v, want %v with input %q", tools, tt.argv0, tt.input)
			}
		})
	}
}

func TestSystemClipboardWriteHTMLFailsWithoutATool(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	if err := (SystemClipboard{}).WriteHTML("<b>", "b"); err == nil {
		t.Fatal("WriteHTML with no tool on PATH returned nil")
	}
}
