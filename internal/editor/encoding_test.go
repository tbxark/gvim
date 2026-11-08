package editor

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestNonUTF8FilesOpenAndSaveByteForByte(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		file  string // the bytes on disk
		text  string // the buffer text after open
		label string // Encoding() after open
	}{
		{"windows-1252 with CRLF", "caf\xe9 \x80\r\nline\r\n", "café €\nline\n", "Windows-1252"},
		{"windows-1252 without a break", "na\xefve", "naïve", "Windows-1252"},
		{"UTF-16LE with BOM", "\xff\xfeh\x00i\x00\r\x00\n\x00", "hi\n", "UTF-16 LE"},
		{"UTF-16BE with BOM", "\xfe\xff\x00h\x00i", "hi", "UTF-16 BE"},
		{"UTF-16LE without BOM", "c\x00a\x00f\x00\xe9\x00\n\x00", "caf\u00e9\n", "UTF-16 LE"},
		{"UTF-16BE without BOM", "\x00c\x00a\x00f\x00\xe9\x00\n", "caf\u00e9\n", "UTF-16 BE"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			e, path := openFixture(t, Options{}, tt.file)
			d := e.Active()
			if got := d.Buffer.Text(); got != tt.text {
				t.Fatalf("text = %q, want %q", got, tt.text)
			}
			if got := d.Encoding(); got != tt.label {
				t.Fatalf("Encoding() = %q, want %q", got, tt.label)
			}
			run(t, e, "save")
			if got := readFile(t, path); got != tt.file {
				t.Fatalf("saved %q, want the original bytes %q", got, tt.file)
			}
		})
	}
}

func TestUndecodableFileIsRefused(t *testing.T) {
	t.Parallel()
	// A UTF-16LE byte-order mark, then a lone high surrogate: no text decodes.
	path := filepath.Join(t.TempDir(), "bad.txt")
	if err := os.WriteFile(path, []byte{0xff, 0xfe, 0x00, 0xd8}, 0o600); err != nil {
		t.Fatal(err)
	}
	e := New(Options{})
	if err := e.OpenFile(path); !errors.Is(err, ErrNotDecodable) {
		t.Fatalf("OpenFile err = %v, want ErrNotDecodable", err)
	}
	if tabs := e.Tabs(); len(tabs) != 1 || tabs[0].Path != "" {
		t.Fatalf("failed open changed tabs: %+v", tabs)
	}
}

func TestEditedNonUTF8FileSavesInItsEncoding(t *testing.T) {
	t.Parallel()
	e, path := openFixture(t, Options{}, "caf\xe9")
	run(t, e, "move_document_end")
	typeText(t, e, "ü")
	run(t, e, "save")
	if got := readFile(t, path); got != "caf\xe9\xfc" {
		t.Fatalf("saved %q, want windows-1252 bytes %q", got, "caf\xe9\xfc")
	}
}

func TestSaveRefusesTextTheEncodingCannotHold(t *testing.T) {
	t.Parallel()
	e, path := openFixture(t, Options{}, "caf\xe9")
	run(t, e, "move_document_end")
	typeText(t, e, "中")
	if err := e.Save(); err == nil {
		t.Fatal("Save of a CJK rune in windows-1252 succeeded")
	}
	if got := readFile(t, path); got != "caf\xe9" {
		t.Fatalf("failed save changed the file to %q", got)
	}
	if d := e.Active(); !d.Modified() || d.Buffer.Text() != "café中" {
		t.Fatalf("failed save lost the buffer: modified %v, text %q", d.Modified(), d.Buffer.Text())
	}
}

func TestSetEncodingChangesTheSaveEncoding(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		file  string // the bytes on disk
		text  string // the buffer text after open, which is the decoded file
		codec string
		want  string // the bytes on disk after save
		label string // Encoding() after set_encoding
	}{
		{"windows-1252 to UTF-8", "caf\xe9", "café", "utf-8", "caf\xc3\xa9", "UTF-8"},
		{"ASCII to UTF-8 with BOM", "abc", "abc", "utf-8-bom", "\xef\xbb\xbfabc", "UTF-8 BOM"},
		{"ASCII to UTF-16LE", "abc", "abc", "utf-16le", "\xff\xfea\x00b\x00c\x00", "UTF-16 LE"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			e, path := openFixture(t, Options{}, tt.file)
			if r := e.Do("set_encoding", map[string]any{"name": tt.codec}); r.Err != nil {
				t.Fatalf("set_encoding: %v", r.Err)
			}
			if got := e.Active().Encoding(); got != tt.label {
				t.Fatalf("Encoding() = %q, want %q", got, tt.label)
			}
			if got := e.Active().Buffer.Text(); got != tt.text {
				t.Fatalf("set_encoding changed the text to %q, want %q", got, tt.text)
			}
			run(t, e, "save")
			if got := readFile(t, path); got != tt.want {
				t.Fatalf("saved %q, want %q", got, tt.want)
			}
		})
	}
}

func TestEncodingActionsRefuseBadArguments(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		action  Action
		args    map[string]any
		wantErr error // nil means any error will do
	}{
		{"set_encoding unknown name", "set_encoding", map[string]any{"name": "klingon"}, ErrUnknownEncoding},
		{"set_encoding missing name", "set_encoding", nil, nil},
		{"set_encoding name not a string", "set_encoding", map[string]any{"name": 1}, nil},
		{"reload_with_encoding unknown name", "reload_with_encoding", map[string]any{"name": "klingon"}, ErrUnknownEncoding},
		{"reload_with_encoding missing name", "reload_with_encoding", nil, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			e, _ := openFixture(t, Options{}, "abc")
			r := e.Do(tt.action, tt.args)
			if r.Err == nil {
				t.Fatal("Do succeeded, want an error")
			}
			if tt.wantErr != nil && !errors.Is(r.Err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", r.Err, tt.wantErr)
			}
			if got := e.Active().Encoding(); got != "ASCII" {
				t.Fatalf("a refused action changed the encoding to %q", got)
			}
		})
	}
}

func TestReloadWithEncodingDecodesTheFileAgain(t *testing.T) {
	t.Parallel()
	// The file is valid UTF-8 "café", so it opens as UTF-8. Read as
	// windows-1252 the same bytes are "cafÃ©", and saving writes them back.
	e, path := openFixture(t, Options{}, "caf\xc3\xa9")
	if got := e.Active().Buffer.Text(); got != "café" {
		t.Fatalf("text = %q, want %q", got, "café")
	}
	if r := e.Do("reload_with_encoding", map[string]any{"name": "windows-1252"}); r.Err != nil {
		t.Fatalf("reload_with_encoding: %v", r.Err)
	}
	d := e.Active()
	if got := d.Buffer.Text(); got != "cafÃ©" {
		t.Fatalf("reloaded text = %q, want %q", got, "cafÃ©")
	}
	if got := d.Encoding(); got != "Windows-1252" {
		t.Fatalf("Encoding() = %q, want Windows-1252", got)
	}
	if d.Modified() {
		t.Fatal("reload should leave the buffer unmodified")
	}
	run(t, e, "save")
	if got := readFile(t, path); got != "caf\xc3\xa9" {
		t.Fatalf("saved %q, want the original bytes", got)
	}
}

func TestReloadWithEncodingGuardsEditsAndPaths(t *testing.T) {
	t.Parallel()
	t.Run("edited buffer is refused unless forced", func(t *testing.T) {
		t.Parallel()
		e, _ := openFixture(t, Options{}, "abc")
		typeText(t, e, "x")
		r := e.Do("reload_with_encoding", map[string]any{"name": "windows-1252"})
		if !errors.Is(r.Err, ErrUnsaved) {
			t.Fatalf("err = %v, want ErrUnsaved", r.Err)
		}
		if got := e.Active().Buffer.Text(); got != "xabc" {
			t.Fatalf("refused reload changed the text to %q", got)
		}
		if r := e.Do("reload_with_encoding", map[string]any{"name": "windows-1252", "force": true}); r.Err != nil {
			t.Fatalf("forced reload: %v", r.Err)
		}
		if got := e.Active().Buffer.Text(); got != "abc" {
			t.Fatalf("forced reload text = %q, want the disk text", got)
		}
	})
	t.Run("untitled buffer has no file to reload", func(t *testing.T) {
		t.Parallel()
		e := New(Options{})
		if r := e.Do("reload_with_encoding", map[string]any{"name": "utf-8"}); !errors.Is(r.Err, ErrNoPath) {
			t.Fatalf("err = %v, want ErrNoPath", r.Err)
		}
	})
	t.Run("undecodable file keeps the buffer", func(t *testing.T) {
		t.Parallel()
		e, _ := openFixture(t, Options{}, "abc")
		// Three bytes are an odd length for UTF-16, so utf-16le cannot decode them.
		r := e.Do("reload_with_encoding", map[string]any{"name": "utf-16le"})
		if !errors.Is(r.Err, ErrNotDecodable) {
			t.Fatalf("err = %v, want ErrNotDecodable", r.Err)
		}
		if got := e.Active().Buffer.Text(); got != "abc" {
			t.Fatalf("failed reload changed the text to %q", got)
		}
	})
}

func TestSaveWithUTF8BOMKeepsTheBOM(t *testing.T) {
	t.Parallel()
	// A UTF-8 file with a BOM opens as valid UTF-8. The BOM is removed from the
	// text, and the codec keeps it, so the save writes it back unchanged.
	e, path := openFixture(t, Options{}, "\xef\xbb\xbfabc")
	run(t, e, "save")
	if got := readFile(t, path); got != "\xef\xbb\xbfabc" {
		t.Fatalf("saved %q, want the BOM kept", got)
	}
}

func TestShiftJISFileWithWindows1252UndefinedBytesOpens(t *testing.T) {
	t.Parallel()
	// Each file is Shift_JIS text with one character whose lead byte is undefined
	// in windows-1252. The detected encoding, windows-1252, cannot decode it.
	tests := []struct {
		name string
		file string // the bytes on disk
		text string // the buffer text after open
	}{
		{"lead byte 0x81", "\x93\xfa\x96\x7b\x81\x40", "日本　"},
		{"lead byte 0x8d", "\x93\xfa\x96\x7b\x8d\x40", "日本后"},
		{"lead byte 0x8f", "\x93\xfa\x96\x7b\x8f\x40", "日本宗"},
		{"lead byte 0x90", "\x93\xfa\x96\x7b\x90\x40", "日本拭"},
		{"lead byte 0x9d", "\x93\xfa\x96\x7b\x9d\x40", "日本戞"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			e, path := openFixture(t, Options{}, tt.file)
			d := e.Active()
			if got := d.Buffer.Text(); got != tt.text {
				t.Fatalf("text = %q, want %q", got, tt.text)
			}
			if got := d.Encoding(); got != "Shift-JIS" {
				t.Fatalf("Encoding() = %q, want Shift-JIS", got)
			}
			run(t, e, "save")
			if got := readFile(t, path); got != tt.file {
				t.Fatalf("saved %q, want the original bytes %q", got, tt.file)
			}
		})
	}
}

func TestUndecodableWindows1252FileStillRefused(t *testing.T) {
	t.Parallel()
	// 0x81 is undefined in windows-1252, and 0x81 0x20 is not a Shift_JIS character
	// either, so neither encoding decodes the file.
	path := filepath.Join(t.TempDir(), "bad.txt")
	if err := os.WriteFile(path, []byte("abc\x81\x20"), 0o600); err != nil {
		t.Fatal(err)
	}
	e := New(Options{})
	if err := e.OpenFile(path); !errors.Is(err, ErrNotDecodable) {
		t.Fatalf("OpenFile err = %v, want ErrNotDecodable", err)
	}
}

func TestUTF8BOMIsWrittenOnceWhenTheEncodingIsSet(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		file  string // the bytes on disk
		setup func(t *testing.T, e *Editor)
		text  string // the buffer text before set_encoding
	}{
		{
			name: "a UTF-8 file that starts with a BOM",
			file: "\xef\xbb\xbfabc\n",
			text: "abc\n",
		},
		{
			name: "a U+FEFF typed at the start of a plain UTF-8 file",
			file: "abc\n",
			setup: func(t *testing.T, e *Editor) {
				e.SetCursor(caretPos(0, 0), false)
				typeKey(t, e, "\ufeff")
			},
			text: "\ufeffabc\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			e, path := openFixture(t, Options{}, tt.file)
			setCaretsAfter(t, e, tt.setup)
			if got := e.Active().Buffer.Text(); got != tt.text {
				t.Fatalf("text before set_encoding = %q, want %q", got, tt.text)
			}
			if r := e.Do("set_encoding", map[string]any{"name": "utf-8-bom"}); r.Err != nil {
				t.Fatalf("set_encoding: %v", r.Err)
			}
			run(t, e, "save")
			if got := readFile(t, path); got != "\xef\xbb\xbfabc\n" {
				t.Fatalf("saved %q, want exactly one BOM before abc", got)
			}
		})
	}
}

func TestOpenedBOMFileKeepsItsBOMOutOfTheText(t *testing.T) {
	t.Parallel()
	e, _ := openFixture(t, Options{}, "\xef\xbb\xbfabc\n")
	d := e.Active()
	if got := d.Buffer.Text(); got != "abc\n" {
		t.Fatalf("text = %q, want the BOM removed on decode", got)
	}
	if got := d.Encoding(); got != "UTF-8 BOM" {
		t.Fatalf("Encoding() = %q, want UTF-8 BOM", got)
	}
}
