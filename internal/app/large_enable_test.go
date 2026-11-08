package app

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/tbxark/gvim/editor"
)

// codeLines writes a file of at least size bytes that looks like indented code, with
// brackets, tabs and trailing blanks, in root. A function line
// with an unclosed brace opens every 50th line, so a bracket scan from it has to give up.
func codeLines(t *testing.T, root string, size int) (path string, lines int) {
	t.Helper()
	var sb strings.Builder
	for sb.Len() < size {
		if lines%50 == 0 {
			fmt.Fprintf(&sb, "func f%d(a, b int) {\n", lines)
		} else {
			fmt.Fprintf(&sb, "\t  x%d := (a + b[%d])  \n", lines, lines)
		}
		lines++
	}
	path = filepath.Join(root, "code.go.txt")
	must(t, os.WriteFile(path, []byte(sb.String()), 0o644))
	return path, lines
}

// largeRuntime builds the runtime that gvim starts with for the file at path, with the
// project config body, and returns it.
func largeRuntime(t *testing.T, root, body string, files ...string) *runtime {
	t.Helper()
	isolateConfig(t)
	must(t, os.MkdirAll(filepath.Join(root, ".gvim"), 0o755))
	must(t, os.WriteFile(filepath.Join(root, ".gvim", "config.json"), []byte(body), 0o644))
	opts := Options{Root: root}
	for _, f := range files {
		opts.Files = append(opts.Files, FileTarget{Path: f})
	}
	rt, err := buildRuntime(t.Context(), opts, false)
	if err != nil {
		t.Fatalf("buildRuntime: %v", err)
	}
	t.Cleanup(func() {
		rt.close()
		rt.ed.Close()
	})
	return rt
}

// TestAppOpensFilesFromTheThresholdInLargeMode checks that the composition root turns
// large file mode on, with the config threshold: at the threshold a file opens large,
// below it and with a threshold of zero it does not.
func TestAppOpensFilesFromTheThresholdInLargeMode(t *testing.T) {
	tests := []struct {
		name      string
		threshold string // the large_file_threshold_bytes line of the config, or ""
		size      int
		want      bool
	}{
		{"default threshold, small file", "", 200 << 10, false},
		{"config threshold, file at it", `"large_file_threshold_bytes": 4096`, 4096, true},
		{"config threshold, file below it", `"large_file_threshold_bytes": 4096`, 4095, false},
		{"zero turns it off", `"large_file_threshold_bytes": 0`, 1 << 20, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			path := filepath.Join(root, "f.txt")
			must(t, os.WriteFile(path, []byte(strings.Repeat("x", tt.size-1)+"\n"), 0o644))
			rt := largeRuntime(t, root, `{"editor": {`+tt.threshold+`}}`, path)
			if got := rt.ed.Active().Large(); got != tt.want {
				t.Fatalf("Large() = %v for %d bytes, want %v", got, tt.size, tt.want)
			}
		})
	}
}

// TestBigFileWithEveryViewFeatureStaysBounded runs a file of a few MiB through the real
// runtime with the config that turns on every feature that looks at lines around the
// caret (indentation guides, whitespace marks, occurrence and bracket highlight,
// rulers, relative numbers), and drives scrolling, multi-cursor, undo, find, save and
// reopen. Nothing may read the whole text, nothing may be refused for size, and the
// lines read from the file stay a small part of the file.
func TestBigFileWithEveryViewFeatureStaysBounded(t *testing.T) {
	root := t.TempDir()
	path, lines := codeLines(t, root, 6<<20)
	const body = `{"editor": {
		"large_file_threshold_bytes": 4096,
		"indentation_guide": "all", "whitespace_spaces_inner": true,
		"whitespace_spaces_leading": true, "whitespace_spaces_trailing": true,
		"whitespace_tabs_inner": true, "highlight_occurrences": true,
		"highlight_matching_brackets": true, "highlight_current_line": true,
		"relative_line_numbers": true, "rulers": [40, 80]
	}}`
	rt := largeRuntime(t, root, body, path)
	ed, m := rt.ed, rt.model
	d := ed.Active()
	if !d.Large() {
		t.Fatal("the document is not large")
	}
	pump := func(cmd tea.Cmd) {
		for range 400 {
			if cmd == nil {
				return
			}
			_, cmd = m.Update(cmd())
		}
	}
	_, cmd := m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	pump(cmd)
	if info := d.Index(); !info.Complete || info.Lines != lines+1 {
		t.Fatalf("index after the commands: %+v, want %d lines", info, lines+1)
	}
	frame := func() string { return ansi.Strip(m.View().Content) }
	if f := frame(); !strings.Contains(f, "func f0(a, b int) {") {
		t.Fatalf("the first screen lacks the first line:\n%s", f)
	}

	base := d.Buffer.Stats()
	do := func(action editor.Action, args map[string]any) {
		t.Helper()
		if r := ed.Do(action, args); r.Err != nil {
			t.Fatalf("Do(%s): %v", action, r.Err)
		}
	}
	for _, line := range []int{1, lines / 2, lines - 3, 51} {
		must(t, ed.GotoLine(line, 5))
		frame()
		for _, a := range []editor.Action{
			"goto_matching_bracket", "move_right", "move_word_right", "select_word",
			"add_cursor_below", "move_page_down", "move_page_up",
		} {
			ed.Do(a, nil) // a refusal is allowed; reading everything is not
			frame()
		}
		do("remove_secondary_cursors", nil)
		do("move_left", nil)
	}
	must(t, ed.GotoLine(lines/2+1, 1))
	do("insert_char", map[string]any{"text": "Q"})
	frame()
	do("undo", nil)
	frame()
	do("insert_char", map[string]any{"text": "Q"})
	frame()

	st := d.Buffer.Stats()
	if st.OverCap != 0 {
		t.Errorf("OverCap = %d: something asked for more text than the cap", st.OverCap)
	}
	if got := st.MaterializedBytes - base.MaterializedBytes; got > 1<<20 {
		t.Errorf("Text/Range returned %d bytes during the session, want under 1 MiB", got)
	}
	if got := st.LineCalls - base.LineCalls; got > int64(lines/4) {
		t.Errorf("%d lines read from the file of %d lines", got, lines)
	}

	// Save writes the edit and ends the undo history.
	must(t, ed.Save())
	if d.Modified() {
		t.Error("the document is modified after Save")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Count(string(data), "\n"); got != lines {
		t.Errorf("saved file has %d lines, want %d", got, lines)
	}
	if got := strings.Split(string(data), "\n")[lines/2]; !strings.HasPrefix(got, "Q") || strings.Count(string(data), "Q") != 1 {
		t.Errorf("saved line %d = %q, want the one edit at its start", lines/2, got)
	}
	if _, ok := d.Buffer.Undo(); ok {
		t.Error("Undo has history after a save of a large document")
	}
	if got := d.Buffer.Line(lines / 2); !strings.Contains(got, "Q") {
		t.Errorf("line %d after the save = %q, want the edit", lines/2, got)
	}
}
