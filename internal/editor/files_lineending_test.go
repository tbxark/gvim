package editor

import "testing"

func TestMixedLineEndingsAreKeptOnSave(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		content string
		actions []Action // run from the start of the document
		typed   string   // typed after the actions
		want    string
	}{
		{"unedited", "a\r\nb\nc\r\n", nil, "", "a\r\nb\nc\r\n"},
		{"lf majority", "a\nb\r\nc\n", nil, "", "a\nb\r\nc\n"},
		{"no final break", "a\r\nb\nc", nil, "", "a\r\nb\nc"},
		{"edit a line", "a\r\nb\nc\r\n", []Action{"move_down", "move_line_end"}, "x", "a\r\nbx\nc\r\n"},
		{"edit the first line", "a\r\nb\nc\r\n", []Action{"move_line_end"}, "x", "ax\r\nb\nc\r\n"},
		{"delete a line", "a\r\nb\nc\r\n", []Action{"move_down", "delete_line"}, "", "a\r\nc\r\n"},
		{"delete the first line", "a\r\nb\nc\r\n", []Action{"delete_line"}, "", "b\nc\r\n"},
		{"new line takes the majority ending", "a\r\nb\nc\r\n", []Action{"move_down", "move_line_end", "insert_newline"}, "x", "a\r\nb\nx\r\nc\r\n"},
		{"new line at the end takes the majority ending", "a\r\nb\nc\r\n", []Action{"move_document_end"}, "x", "a\r\nb\nc\r\nx"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			e, path := openFixture(t, Options{}, tt.content)
			run(t, e, tt.actions...)
			typeText(t, e, tt.typed)
			run(t, e, "save")
			if got := readFile(t, path); got != tt.want {
				t.Fatalf("saved %q, want %q", got, tt.want)
			}
			// A second save writes the same bytes: the record follows the save.
			run(t, e, "save")
			if got := readFile(t, path); got != tt.want {
				t.Fatalf("second save wrote %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSetLineEndingNormalisesAMixedFile(t *testing.T) {
	t.Parallel()
	tests := []struct {
		value string
		want  string
	}{
		{"crlf", "a\r\nb\r\nc\r\n"},
		{"lf", "a\nb\nc\n"},
	}
	for _, tt := range tests {
		t.Run(tt.value, func(t *testing.T) {
			t.Parallel()
			e, path := openFixture(t, Options{}, "a\r\nb\nc\r\n")
			if r := e.Do("set_line_ending", map[string]any{"value": tt.value}); r.Err != nil {
				t.Fatal(r.Err)
			}
			if !e.Active().Modified() {
				t.Fatal("Modified() = false after normalising the line endings, want true")
			}
			run(t, e, "save")
			if got := readFile(t, path); got != tt.want {
				t.Fatalf("saved %q, want %q", got, tt.want)
			}
		})
	}
}

func TestMixedLineEndingsSurviveReloadAndRevert(t *testing.T) {
	t.Parallel()
	const content = "a\r\nb\nc\r\n"
	e, path := openFixture(t, Options{}, content)
	e.Do("reload_with_encoding", map[string]any{"name": "utf-8"})
	run(t, e, "save")
	if got := readFile(t, path); got != content {
		t.Fatalf("saved %q after a reload, want %q", got, content)
	}
}
