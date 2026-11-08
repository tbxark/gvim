package editor

import "testing"

// TestEncodingLabelFollowsTheTextBeforeSave types non-ASCII text into an ASCII file
// and deletes it again. The label is what the status row shows, so it must follow the
// text at once, not only at the next save.
func TestEncodingLabelFollowsTheTextBeforeSave(t *testing.T) {
	t.Parallel()
	e, path := openFixture(t, Options{}, "hello\n")
	d := e.Active()
	if got := d.Encoding(); got != "ASCII" {
		t.Fatalf("after open Encoding() = %q, want ASCII", got)
	}
	if r := e.Do("insert_char", map[string]any{"text": "中文é"}); r.Err != nil {
		t.Fatalf("insert_char: %v", r.Err)
	}
	if got := d.Encoding(); got != "UTF-8" {
		t.Fatalf("after typing CJK, before save, Encoding() = %q, want UTF-8", got)
	}
	for range 3 {
		if r := e.Do("delete_backward", nil); r.Err != nil {
			t.Fatalf("delete_backward: %v", r.Err)
		}
	}
	if got := d.Buffer.Text(); got != "hello\n" {
		t.Fatalf("text = %q after deleting the typed text", got)
	}
	if got := d.Encoding(); got != "ASCII" {
		t.Fatalf("after deleting the typed text, Encoding() = %q, want ASCII", got)
	}
	if r := e.Do("insert_char", map[string]any{"text": "中"}); r.Err != nil {
		t.Fatalf("insert_char: %v", r.Err)
	}
	run(t, e, "save")
	if got := d.Encoding(); got != "UTF-8" {
		t.Fatalf("after save, Encoding() = %q, want UTF-8", got)
	}
	if got := readFile(t, path); got != "中hello\n" {
		t.Fatalf("saved %q", got)
	}
}

// TestUntitledBufferKeepsItsUTF8Label checks that a buffer with no path keeps the label
// it has always had, whatever its text.
func TestUntitledBufferKeepsItsUTF8Label(t *testing.T) {
	t.Parallel()
	e := New(Options{})
	if got := e.Active().Encoding(); got != "UTF-8" {
		t.Fatalf("untitled Encoding() = %q, want UTF-8", got)
	}
	if r := e.Do("insert_char", map[string]any{"text": "abc"}); r.Err != nil {
		t.Fatalf("insert_char: %v", r.Err)
	}
	if got := e.Active().Encoding(); got != "UTF-8" {
		t.Fatalf("untitled Encoding() after ASCII text = %q, want UTF-8", got)
	}
}
