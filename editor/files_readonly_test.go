package editor

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode"
)

func TestBinaryFileOpensReadOnlyAndNeverAsGarbage(t *testing.T) {
	t.Parallel()
	// A PNG header: NUL and control bytes, and bytes that are not valid UTF-8.
	content := "\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR\x1b[31m\x9b\xff"
	e, path := openFixture(t, Options{}, content)
	d := e.Active()
	if !d.Binary() || !d.ReadOnly() {
		t.Fatalf("Binary() = %v, ReadOnly() = %v, want both true", d.Binary(), d.ReadOnly())
	}
	for _, r := range d.Buffer.Text() {
		if r != '\n' && r != '\t' && r != '\r' && unicode.IsControl(r) {
			t.Fatalf("the buffer holds the control character %U, which would reach the terminal", r)
		}
	}
	if st := e.Status(); !strings.Contains(st, "binary") {
		t.Errorf("Status() = %q, want a binary notice", st)
	}
	if r := e.Do("insert_char", map[string]any{"text": "x"}); !errors.Is(r.Err, ErrReadOnly) {
		t.Errorf("insert_char err = %v, want ErrReadOnly", r.Err)
	}
	if r := e.Do("toggle_read_only", nil); r.Err == nil || !d.ReadOnly() {
		t.Errorf("toggle_read_only err = %v, ReadOnly() = %v, want a refusal that keeps the file read-only", r.Err, d.ReadOnly())
	}
	other := filepath.Join(filepath.Dir(path), "copy.bin")
	if err := e.SaveAs(other); err == nil {
		t.Error("SaveAs of a binary document succeeded, want a refusal")
	}
	if _, err := os.Stat(other); err == nil {
		t.Error("SaveAs wrote a lossy copy of the binary file")
	}
	if got := readFile(t, path); got != content {
		t.Errorf("the binary file on disk changed to %q", got)
	}
}

func TestBinaryStaysReadOnlyAfterReloadWithEncoding(t *testing.T) {
	t.Parallel()
	e, _ := openFixture(t, Options{}, "ab\x00cd\n")
	e.Do("reload_with_encoding", map[string]any{"name": "utf-8", "force": true})
	if !e.Active().ReadOnly() {
		t.Fatal("ReadOnly() = false after reload_with_encoding, want true")
	}
}

func TestUTF16FileIsNotBinary(t *testing.T) {
	t.Parallel()
	e, _ := openFixture(t, Options{}, "\xff\xfeh\x00i\x00")
	if d := e.Active(); d.Binary() || d.ReadOnly() {
		t.Fatalf("Binary() = %v, ReadOnly() = %v, want both false", d.Binary(), d.ReadOnly())
	}
}

func TestUnwritableFileOpensReadOnly(t *testing.T) {
	t.Parallel()
	if os.Geteuid() == 0 {
		t.Skip("root can write any file")
	}
	tests := []struct {
		Name string
		opts Options
		Mode os.FileMode
		Want bool
	}{
		{"auto read-only on, 0444", Options{AutoReadOnly: true}, 0o444, true},
		{"auto read-only on, 0644", Options{AutoReadOnly: true}, 0o644, false},
		{"auto read-only off, 0444", Options{}, 0o444, false},
	}
	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			t.Parallel()
			path := filepath.Join(t.TempDir(), "f.txt")
			if err := os.WriteFile(path, []byte("abc\n"), tt.Mode); err != nil {
				t.Fatal(err)
			}
			e := New(tt.opts)
			if err := e.OpenFile(path); err != nil {
				t.Fatal(err)
			}
			if got := e.Active().ReadOnly(); got != tt.Want {
				t.Fatalf("ReadOnly() = %v, want %v", got, tt.Want)
			}
			r := e.Do("insert_char", map[string]any{"text": "x"})
			if tt.Want != errors.Is(r.Err, ErrReadOnly) {
				t.Fatalf("insert_char err = %v, want ErrReadOnly = %v", r.Err, tt.Want)
			}
			if tt.Want && !strings.Contains(e.Status(), "read-only") {
				t.Errorf("Status() = %q, want a read-only notice", e.Status())
			}
		})
	}
}

func TestLoadFuncAppliesAutoReadOnly(t *testing.T) {
	t.Parallel()
	if os.Geteuid() == 0 {
		t.Skip("root can write any file")
	}
	path := filepath.Join(t.TempDir(), "f.txt")
	if err := os.WriteFile(path, []byte("abc\n"), 0o444); err != nil {
		t.Fatal(err)
	}
	d, err := New(Options{AutoReadOnly: true}).LoadFunc(path)()
	if err != nil {
		t.Fatal(err)
	}
	if !d.ReadOnly() {
		t.Fatal("ReadOnly() = false for a document loaded by LoadFunc, want true")
	}
}

func TestLibraryPath(t *testing.T) {
	t.Parallel()
	tests := []struct {
		Path string
		Want bool
	}{
		{"/usr/include/stdio.h", true},
		{"/usr/local/include/x.h", true},
		{"/nix/store/abc-foo/bin/foo", true},
		{"/opt/homebrew/Cellar/go/1.22/src/fmt/print.go", true},
		{"/usr/local/Cellar/go/1.22/README", true},
		{"/home/u/.rustup/toolchains/x/lib/a.rs", true},
		{"/home/u/rustup/toolchains/x/lib/a.rs", true},
		{"/home/u/.cargo/registry/src/x/lib.rs", true},
		{"/home/u/.nuget/packages/x/y.cs", true},
		{"/Applications/Xcode.app/Contents/Developer/Platforms/a.h", true},
		{"/Library/Developer/CommandLineTools/SDKs/MacOSX.sdk/a.h", true},
		{"/home/u/proj/node_modules/x/index.js", true},
		{"/home/u/proj/main.go", false},
		{"/home/u/usr/include/a.h", false},
	}
	for _, tt := range tests {
		if got := libraryPath(tt.Path); got != tt.Want {
			t.Errorf("libraryPath(%q) = %v, want %v", tt.Path, got, tt.Want)
		}
	}
}
