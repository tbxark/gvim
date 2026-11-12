package editor

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestRevertOfTextTabIntoBinaryFileIsRefused checks that a text tab is never turned into a
// binary one: the file turned to binary data, the tab keeps its text and stays editable.
func TestRevertOfTextTabIntoBinaryFileIsRefused(t *testing.T) {
	t.Parallel()
	e, path := openFixture(t, Options{}, "hello\n")
	putFile(t, path, "\x00\x01\x02abc\x7f\n")
	if err := e.Revert(false); !errors.Is(err, ErrBinary) {
		t.Fatalf("Revert err = %v, want ErrBinary", err)
	}
	d := e.Active()
	if d.Binary() || d.ReadOnly() {
		t.Fatalf("Binary() = %v, ReadOnly() = %v, want both false", d.Binary(), d.ReadOnly())
	}
	if got := d.Buffer.Text(); got != "hello\n" {
		t.Fatalf("text = %q, want it unchanged", got)
	}
	if d.CodecName() != "utf-8" {
		t.Fatalf("codec = %q, want utf-8", d.CodecName())
	}
}

// TestRevertRefreshesAReadOnlyTab checks that a read-only tab, which the auto-reload
// reaches, takes the new text of its file and stays read-only. Binary tabs refresh too and
// stay binary.
func TestRevertRefreshesAReadOnlyTab(t *testing.T) {
	t.Parallel()
	tests := []struct {
		Name       string
		first      string
		second     string
		toggle     bool
		wantText   string
		wantBinary bool
	}{
		{"read-only text", "a\n", "b\n", true, "b\n", false},
		{"binary tab, binary file", "a\x00b\n", "c\x00d\n", false, binarySummaryText("doc.txt"), true},
		{"binary tab, file turns to text", "a\x00b\n", "plain\n", false, "plain\n", false},
	}
	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			t.Parallel()
			e, path := openFixture(t, Options{}, tt.first)
			if tt.toggle {
				run(t, e, "toggle_read_only")
			}
			putFile(t, path, tt.second)
			if err := e.Revert(false); err != nil {
				t.Fatalf("Revert: %v", err)
			}
			d := e.Active()
			got := d.Buffer.Text()
			want := tt.wantText
			if tt.wantBinary {
				if !strings.HasPrefix(got, want) || !strings.Contains(got, "Binary file: read-only.") {
					t.Errorf("text = %q, want the binary summary beginning %q", got, want)
				}
			} else if got != want {
				t.Errorf("text = %q, want %q", got, want)
			}
			if !d.ReadOnly() {
				t.Error("ReadOnly() = false, want true")
			}
			if d.Binary() != tt.wantBinary {
				t.Errorf("Binary() = %v, want %v", d.Binary(), tt.wantBinary)
			}
			if d.Modified() {
				t.Error("Modified() = true after revert, want false")
			}
			if !tt.wantBinary && d.Encoding() == "Binary" {
				t.Errorf("Encoding() = %q for text", d.Encoding())
			}
		})
	}
}

// binarySummaryText is the summary a binary document of path shows.
func binarySummaryText(path string) string {
	return filepath.Base(path) + "\n\nBinary file\n"
}

// TestRevertActionStillRefusesReadOnlyTab checks that the user-invoked revert action keeps
// refusing a read-only tab, as the other text-changing actions do.
func TestRevertActionStillRefusesReadOnlyTab(t *testing.T) {
	t.Parallel()
	e, path := openRevertable(t, "a\n")
	run(t, e, "toggle_read_only")
	putFile(t, path, "b\n")
	if r := e.Do("revert", nil); !errors.Is(r.Err, ErrReadOnly) {
		t.Fatalf("err = %v, want ErrReadOnly", r.Err)
	}
}

// TestEncodingAndLineEndingKeepReadOnlyTabClean checks that choosing an encoding or a line
// ending for a document that cannot be saved does not leave it modified.
func TestEncodingAndLineEndingKeepReadOnlyTabClean(t *testing.T) {
	t.Parallel()
	tests := []struct {
		Name   string
		binary bool
		action Action
		Args   map[string]any
	}{
		{"binary, line ending", true, "set_line_ending", map[string]any{"value": "crlf"}},
		{"binary, encoding", true, "set_encoding", map[string]any{"name": "windows-1252"}},
		{"read-only, line ending", false, "set_line_ending", map[string]any{"value": "crlf"}},
		{"read-only, encoding", false, "set_encoding", map[string]any{"name": "windows-1252"}},
	}
	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			t.Parallel()
			content := "a\n"
			if tt.binary {
				content = "a\x00b\n"
			}
			e, _ := openFixture(t, Options{}, content)
			if !tt.binary {
				run(t, e, "toggle_read_only")
			}
			if r := e.Do(tt.action, tt.Args); r.Err != nil {
				t.Fatalf("%s: %v", tt.action, r.Err)
			}
			if e.Active().Modified() {
				t.Error("Modified() = true, want false: the document cannot be saved")
			}
		})
	}
}

// TestEncodingChoiceStillMarksWritableTabModified is the other side of the test above: a
// writable document is modified by a new line ending or encoding.
func TestEncodingChoiceStillMarksWritableTabModified(t *testing.T) {
	t.Parallel()
	e, _ := openFixture(t, Options{}, "a\n")
	if r := e.Do("set_line_ending", map[string]any{"value": "crlf"}); r.Err != nil {
		t.Fatal(r.Err)
	}
	if !e.Active().Modified() {
		t.Error("Modified() = false, want true")
	}
}

// TestOpenFileFallsBackWhenLaterBytesBreakTheDetectedCodec checks that a legacy file whose
// first 8 KiB read as one code page and whose later bytes are undefined in it still opens.
func TestOpenFileFallsBackWhenLaterBytesBreakTheDetectedCodec(t *testing.T) {
	t.Parallel()
	base := strings.Repeat("\xcf\xf0\xe8\xe2\xe5\xf2, \xcc\xe8\xf0\n", 900)
	tests := []struct {
		Name string
		Data string
	}{
		{"undefined byte after the sample", base + "tail \x98\n"},
		{"undefined byte before the cyrillic text", "x \x98\n" + base},
	}
	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			t.Parallel()
			p := filepath.Join(t.TempDir(), "f.txt")
			if err := os.WriteFile(p, []byte(tt.Data), 0o600); err != nil {
				t.Fatal(err)
			}
			e := New(Options{})
			if err := e.OpenFile(p); err != nil {
				t.Fatalf("OpenFile: %v", err)
			}
			d := e.Active()
			if d.Encoding() == "" || d.Buffer.Text() == "" {
				t.Fatalf("Encoding() = %q, text empty = %v", d.Encoding(), d.Buffer.Text() == "")
			}
		})
	}
}
