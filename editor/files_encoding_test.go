package editor

import (
	"testing"
	"time"
)

func TestLegacyEncodingsOpenWithTheirLabel(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		file  string
		text  string
		label string
	}{
		{"windows-1250", "m\x9dsto\n", "mťsto\n", "Windows-1250"},
		{"windows-1251", "\xcf\xf0\xe8\xe2\xe5\xf2, \xcc\xe8\xf0\n", "Привет, Мир\n", "Windows-1251"},
		{"gb18030", "\xc4\xe3\xba\xc3\xa3\xac\xca\xc0\xbd\xe7\xa1\xa3\xd5\xe2\xca\xc7\xd2\xbb\xb8\xf6\xb2\xe2\xca\xd4\xa1\xa3\n", "你好，世界。这是一个测试。\n", "GB18030"},
		{"euc-kr", "\xbe\xc8\xb3\xe7\xc7\xcf\xbc\xbc\xbf\xe4 \xbc\xbc\xb0\xe8\n", "안녕하세요 세계\n", "EUC-KR"},
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

func TestSetEncodingNamesAreAllAccepted(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"windows-1250", "windows-1251", "gbk", "gb18030", "euc-kr"} {
		e, _ := openFixture(t, Options{}, "abc")
		if r := e.Do("set_encoding", map[string]any{"name": name}); r.Err != nil {
			t.Errorf("set_encoding %s: %v", name, r.Err)
		}
		if got := e.Active().Encoding(); got == "" || got == "ASCII" {
			t.Errorf("set_encoding %s: Encoding() = %q, want a label", name, got)
		}
	}
}

func TestEncodingAndLineEndingChangesMarkTheDocumentModified(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		file   string
		action Action
		args   map[string]any
		want   bool // modified after the action
	}{
		{"new encoding", "abc\n", "set_encoding", map[string]any{"name": "utf-16le"}, true},
		{"same encoding", "abc\n", "set_encoding", map[string]any{"name": "utf-8"}, false},
		{"new line ending", "abc\n", "set_line_ending", map[string]any{"value": "crlf"}, true},
		{"same line ending", "abc\n", "set_line_ending", map[string]any{"value": "lf"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			e, path := openFixture(t, Options{AutoSaveInterval: time.Second}, tt.file)
			if r := e.Do(tt.action, tt.args); r.Err != nil {
				t.Fatalf("%s: %v", tt.action, r.Err)
			}
			d := e.Active()
			if d.Modified() != tt.want || e.Tabs()[0].Modified != tt.want {
				t.Fatalf("Modified() = %v, Tabs()[0].Modified = %v, want %v", d.Modified(), e.Tabs()[0].Modified, tt.want)
			}
			if got := len(e.PlanAutoSave(time.Now())) == 1; got != tt.want {
				t.Fatalf("auto-save plans the document = %v, want %v", got, tt.want)
			}
			if got := len(e.Quit(false)) == 1; got != tt.want {
				t.Fatalf("quit lists the document as unsaved = %v, want %v", got, tt.want)
			}
			if !tt.want {
				return
			}
			run(t, e, "save")
			if d.Modified() {
				t.Fatal("Modified() after save, want false")
			}
			if got := readFile(t, path); got == tt.file {
				t.Fatalf("saved file is unchanged: %q", got)
			}
		})
	}
}

func TestEncodingChangeSurvivesUndoToTheSavedText(t *testing.T) {
	t.Parallel()
	e, _ := openFixture(t, Options{}, "abc\n")
	e.Do("insert_char", map[string]any{"text": "x"})
	e.Do("set_line_ending", map[string]any{"value": "crlf"})
	run(t, e, "undo")
	if !e.Active().Modified() {
		t.Fatal("undo to the saved text cleared the line ending change")
	}
}

func TestReloadWithEncodingClearsTheEncodingChange(t *testing.T) {
	t.Parallel()
	e, _ := openFixture(t, Options{}, "caf\xe9\n")
	e.Do("set_encoding", map[string]any{"name": "utf-8"})
	if r := e.Do("reload_with_encoding", map[string]any{"name": "iso-8859-1", "force": true}); r.Err != nil {
		t.Fatalf("reload_with_encoding: %v", r.Err)
	}
	if e.Active().Modified() {
		t.Fatal("Modified() after a reload, want false")
	}
}
