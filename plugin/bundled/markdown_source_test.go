package bundled_test

import (
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/tbxark/gvim/plugin/plugintest"
	"github.com/tbxark/gvim/pluginapi"
)

// markdownRig loads markdown_source with one buffer at /work/a.md.
func markdownRig(t *testing.T, text string) *rig {
	t.Helper()
	return startRig(t, rigOpts{
		plugins: []string{"markdown_source"},
		buffers: []rigBuffer{{"/work/a.md", text}},
		script:  func(o *plugintest.FakeOS) { o.SetDir("/work") },
	})
}

func markdownCursor(t *testing.T, r *rig) pluginapi.Pos {
	t.Helper()
	carets, err := r.host.Cursors(r.ids["/work/a.md"])
	if err != nil || len(carets) == 0 {
		t.Fatalf("cursors = %+v, %v", carets, err)
	}
	return carets[0].Pos
}

func TestMarkdownSourceManifestAndBindings(t *testing.T) {
	t.Parallel()
	r := markdownRig(t, "- foo")
	if info, ok := r.h.Plugin("markdown_source"); !ok || info.Status != "loaded" {
		t.Fatalf("plugin = %+v (log %v)", info, r.h.LogText(""))
	}
	for _, c := range r.host.Commands["markdown_source"] {
		if !c.Hidden {
			t.Errorf("command %s is visible in the palette, want hidden", c.ID)
		}
	}
	wantBindings := map[string]string{
		"enter":     "markdown_source.enter",
		"tab":       "markdown_source.tab",
		"shift+tab": "markdown_source.shift_tab",
	}
	gotBindings := map[string]string{}
	for _, b := range r.host.Bindings["markdown_source"] {
		gotBindings[b.Chord] = b.Action
	}
	if !maps.Equal(gotBindings, wantBindings) {
		t.Fatalf("bindings = %v, want %v", gotBindings, wantBindings)
	}
}

func markdownEnterRows() []struct {
	name       string
	text       string
	line, col  int
	want       string
	wantCursor pluginapi.Pos
} {
	return []struct {
		name       string
		text       string
		line, col  int
		want       string
		wantCursor pluginapi.Pos
	}{
		{"dash bullet", "- foo", 0, 5, "- foo\n- ", pluginapi.Pos{Line: 1, Col: 2}},
		{"star bullet", "* foo", 0, 5, "* foo\n* ", pluginapi.Pos{Line: 1, Col: 2}},
		{"ordered list increments", "9. foo", 0, 6, "9. foo\n10. ", pluginapi.Pos{Line: 1, Col: 4}},
		{"checkbox continues unchecked", "- [x] done", 0, 10, "- [x] done\n- [ ] ", pluginapi.Pos{Line: 1, Col: 6}},
		{"empty dash item removes the marker", "- ", 0, 2, "", pluginapi.Pos{}},
		{"empty ordered item removes the marker", "2. ", 0, 3, "", pluginapi.Pos{}},
		{"empty checkbox removes the marker", "- [ ] ", 0, 6, "", pluginapi.Pos{}},
		{"empty indented item removes the marker", "  - ", 0, 4, "", pluginapi.Pos{}},
		{"no marker copies leading blanks", "  text", 0, 6, "  text\n  ", pluginapi.Pos{Line: 1, Col: 2}},
		{"plain line gets a new line", "text", 0, 4, "text\n", pluginapi.Pos{Line: 1, Col: 0}},
	}
}

func TestMarkdownSourceEnter(t *testing.T) {
	t.Parallel()
	for _, tt := range markdownEnterRows() {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			r := markdownRig(t, tt.text)
			id := r.ids["/work/a.md"]
			if err := r.host.SetCursor(id, pluginapi.Pos{Line: tt.line, Col: tt.col}, false, true, false); err != nil {
				t.Fatal(err)
			}
			if err := r.h.Run("markdown_source.enter", nil); err != nil {
				t.Fatal(err)
			}
			if got := fullText(t, r.h, id); got != tt.want {
				t.Fatalf("text = %q, want %q (log %v)", got, tt.want, r.h.LogText(""))
			}
			if got := markdownCursor(t, r); got != tt.wantCursor {
				t.Errorf("cursor = %+v, want %+v", got, tt.wantCursor)
			}
		})
	}
}

func TestMarkdownSourceTab(t *testing.T) {
	t.Parallel()
	rows := []struct {
		name       string
		text       string
		col        int
		want       string
		wantCursor pluginapi.Pos
	}{
		{"dash cycles to plus", "- ", 2, "    + ", pluginapi.Pos{Col: 6}},
		{"star cycles to dash", "* ", 2, "    - ", pluginapi.Pos{Col: 6}},
		{"plus cycles to star", "+ ", 2, "    * ", pluginapi.Pos{Col: 6}},
		{"the indent is kept and widened", "  - ", 4, "      + ", pluginapi.Pos{Col: 8}},
		{"checkbox keeps unchecked", "- [ ] ", 6, "    + [ ] ", pluginapi.Pos{Col: 10}},
		{"checkbox keeps checked", "- [x] ", 6, "    + [x] ", pluginapi.Pos{Col: 10}},
		{"an ordered item takes four spaces", "1. ", 3, "1.     ", pluginapi.Pos{Col: 7}},
		{"a marked item with content takes four spaces", "- foo", 5, "- foo    ", pluginapi.Pos{Col: 9}},
	}
	for _, tt := range rows {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			r := markdownRig(t, tt.text)
			id := r.ids["/work/a.md"]
			if err := r.host.SetCursor(id, pluginapi.Pos{Line: 0, Col: tt.col}, false, true, false); err != nil {
				t.Fatal(err)
			}
			if err := r.h.Run("markdown_source.tab", nil); err != nil {
				t.Fatal(err)
			}
			if got := fullText(t, r.h, id); got != tt.want {
				t.Fatalf("text = %q, want %q (log %v)", got, tt.want, r.h.LogText(""))
			}
			if got := markdownCursor(t, r); got != tt.wantCursor {
				t.Errorf("cursor = %+v, want %+v", got, tt.wantCursor)
			}
		})
	}
}

func TestMarkdownSourceShiftTab(t *testing.T) {
	t.Parallel()
	rows := []struct {
		name       string
		text       string
		col        int
		want       string
		wantCursor pluginapi.Pos
	}{
		{"dash de-indents and cycles back to star", "    - ", 6, "* ", pluginapi.Pos{Col: 2}},
		{"checkbox keeps its state", "    - [x] ", 10, "* [x] ", pluginapi.Pos{Col: 6}},
		{"less than four blanks falls through", "  - ", 4, "  - ", pluginapi.Pos{Col: 4}},
	}
	for _, tt := range rows {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			r := markdownRig(t, tt.text)
			id := r.ids["/work/a.md"]
			if err := r.host.SetCursor(id, pluginapi.Pos{Line: 0, Col: tt.col}, false, true, false); err != nil {
				t.Fatal(err)
			}
			if err := r.h.Run("markdown_source.shift_tab", nil); err != nil {
				t.Fatal(err)
			}
			if got := fullText(t, r.h, id); got != tt.want {
				t.Fatalf("text = %q, want %q (log %v)", got, tt.want, r.h.LogText(""))
			}
			if got := markdownCursor(t, r); got != tt.wantCursor {
				// The fallback dispatch is checked below; the fake action does not move.
				if tt.wantCursor.Line != 0 || tt.wantCursor.Col != tt.col {
					t.Errorf("cursor = %+v, want %+v", got, tt.wantCursor)
				}
			}
			if strings.HasPrefix(tt.name, "less than four") {
				if got := r.host.Cmd().Ran(); !slices.Contains(got, "dedent_selection") {
					t.Errorf("actions = %v, want the dedent_selection fallback", got)
				}
			}
		})
	}
}

func TestMarkdownSourceFallsBackOutsideMarkdown(t *testing.T) {
	t.Parallel()
	r := startRig(t, rigOpts{
		plugins: []string{"markdown_source"},
		buffers: []rigBuffer{{"/work/a.txt", "- foo"}},
		script:  func(o *plugintest.FakeOS) { o.SetDir("/work") },
	})
	id := r.ids["/work/a.txt"]
	if err := r.host.SetCursor(id, pluginapi.Pos{Line: 0, Col: 5}, false, true, false); err != nil {
		t.Fatal(err)
	}
	if err := r.h.Run("markdown_source.enter", nil); err != nil {
		t.Fatal(err)
	}
	if got := fullText(t, r.h, id); got != "- foo" {
		t.Fatalf("a non-Markdown buffer was edited: %q", got)
	}
	if got := r.host.Cmd().Ran(); !slices.Contains(got, "insert_newline") {
		t.Fatalf("actions = %v, want insert_newline", got)
	}
}

func TestMarkdownSourceFallsBackForSelection(t *testing.T) {
	t.Parallel()
	r := markdownRig(t, "- foo")
	id := r.ids["/work/a.md"]
	// Extend from the start of the line to the caret: a selection must not be rewritten
	// by the list rules (the editor handles it).
	if err := r.host.SetCursor(id, pluginapi.Pos{Line: 0, Col: 5}, true, true, false); err != nil {
		t.Fatal(err)
	}
	if err := r.h.Run("markdown_source.enter", nil); err != nil {
		t.Fatal(err)
	}
	if got := fullText(t, r.h, id); got != "- foo" {
		t.Fatalf("a selection was edited by the plugin: %q", got)
	}
	if got := r.host.Cmd().Ran(); !slices.Contains(got, "insert_newline") {
		t.Fatalf("actions = %v, want insert_newline", got)
	}
}

func TestMarkdownSourceReadOnlyReportsTheEdit(t *testing.T) {
	t.Parallel()
	r := markdownRig(t, "- foo")
	r.host.Buffers[0].Info.ReadOnly = true
	id := r.ids["/work/a.md"]
	if err := r.host.SetCursor(id, pluginapi.Pos{Line: 0, Col: 5}, false, true, false); err != nil {
		t.Fatal(err)
	}
	if err := r.h.Run("markdown_source.enter", nil); err != nil {
		t.Fatal(err)
	}
	if got := lastNoticeText(r.h); !strings.Contains(got, "Could not edit the Markdown list") {
		t.Fatalf("status = %q", got)
	}
}
