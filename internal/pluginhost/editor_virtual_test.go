package pluginhost

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/tbxark/gvim/buffer"
	"github.com/tbxark/gvim/decorations"
	"github.com/tbxark/gvim/editor"
	"github.com/tbxark/gvim/internal/apptest"
	"github.com/tbxark/gvim/internal/restore"
	"github.com/tbxark/gvim/internal/tui"
	"github.com/tbxark/gvim/pluginapi"
	"github.com/tbxark/gvim/session"
)

// virtualOver returns a plugin port over an editor with one regular tab holding "regular"
// and a virtual buffer with the options, and the id of the virtual buffer.
func virtualOver(t *testing.T, o pluginapi.CreateBuffer) (*pluginEditor, *editor.Editor, pluginapi.BufferID, pluginapi.BufferID) {
	t.Helper()
	pe, ed, ids := pluginEditorOver(t, "regular")
	if o.Name == "" {
		o.Name = "v"
	}
	id, err := pe.CreateBuffer(o)
	apptest.Must(t, err)
	return pe, ed, ids[0], id
}

func TestPluginCreateBuffer(t *testing.T) {
	tests := []struct {
		name string
		in   pluginapi.CreateBuffer
		want pluginapi.BufferInfo
	}{
		{"defaults", pluginapi.CreateBuffer{Name: "outline"},
			pluginapi.BufferInfo{Name: "*outline*", Virtual: true, Encoding: "utf-8", LineCount: 1}},
		{"everything", pluginapi.CreateBuffer{Name: "log", Text: "a\r\nb\rc", Language: "go", Mode: "log-mode", ReadOnly: true},
			pluginapi.BufferInfo{Name: "*log*", Virtual: true, ReadOnly: true, Language: "go", Mode: "log-mode", LineCount: 3}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pe, ed, regular, _ := virtualOver(t, pluginapi.CreateBuffer{Name: "other"})
			id, err := pe.CreateBuffer(tt.in)
			apptest.Must(t, err)
			got, ok := pe.Buffer(id)
			if !ok {
				t.Fatal("the buffer is not open")
			}
			if got.Path != "" || got.Modified || got.Active || !got.Virtual {
				t.Errorf("info = %+v", got)
			}
			if got.Name != tt.want.Name || got.ReadOnly != tt.want.ReadOnly || got.Language != tt.want.Language ||
				got.Mode != tt.want.Mode || got.LineCount != tt.want.LineCount {
				t.Errorf("info = %+v, want %+v", got, tt.want)
			}
			if pe.ActiveBuffer() != regular {
				t.Error("CreateBuffer changed the active tab without Focus")
			}
			if text := textOf(pe, id); strings.Contains(text, "\r") {
				t.Errorf("text %q keeps a carriage return", text)
			}
			if infos := pe.ListBuffers(); infos[len(infos)-1].ID != id {
				t.Error("the new buffer is not the last tab")
			}
			_ = ed
		})
	}
}

func TestPluginCreateBufferFocus(t *testing.T) {
	pe, _, _, _ := virtualOver(t, pluginapi.CreateBuffer{})
	id, err := pe.CreateBuffer(pluginapi.CreateBuffer{Name: "f", Focus: true})
	apptest.Must(t, err)
	if pe.ActiveBuffer() != id {
		t.Fatalf("active = %d, want the new buffer %d", pe.ActiveBuffer(), id)
	}
}

func TestPluginCreateBufferRefusals(t *testing.T) {
	tests := []struct {
		name string
		in   pluginapi.CreateBuffer
		want pluginapi.Code
	}{
		{"no name", pluginapi.CreateBuffer{}, pluginapi.CodeInvalid},
		{"blank name", pluginapi.CreateBuffer{Name: " \t"}, pluginapi.CodeInvalid},
		{"text over the cap", pluginapi.CreateBuffer{Name: "n", Text: strings.Repeat("x", editor.MaxVirtualText+1)}, pluginapi.CodeTooLarge},
		{"unknown language", pluginapi.CreateBuffer{Name: "n", Language: "no-such-language"}, pluginapi.CodeInvalid},
		{"language name longer than any lexer", pluginapi.CreateBuffer{Name: "n", Language: strings.Repeat("x", pluginMaxLanguageName+1)}, pluginapi.CodeInvalid},
		{"mode name too long", pluginapi.CreateBuffer{Name: "n", Mode: strings.Repeat("m", pluginMaxModeName+1)}, pluginapi.CodeInvalid},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pe, ed, _ := pluginEditorOver(t, "x")
			before := len(ed.Tabs())
			if _, err := pe.CreateBuffer(tt.in); codeOf(err) != tt.want {
				t.Fatalf("CreateBuffer = %v, want code %q", err, tt.want)
			}
			if len(ed.Tabs()) != before {
				t.Fatal("a refused call opened a tab")
			}
		})
	}
}

// quotas sets small virtual-buffer quotas for one test.
func quotas(t *testing.T, perPlugin, all, bytesPerPlugin, bytesAll int) {
	t.Helper()
	p, a, bp, ba := pluginMaxVirtual, pluginMaxVirtualAll, pluginMaxVirtualBytes, pluginMaxVirtualBytesAll
	t.Cleanup(func() {
		pluginMaxVirtual, pluginMaxVirtualAll, pluginMaxVirtualBytes, pluginMaxVirtualBytesAll = p, a, bp, ba
	})
	pluginMaxVirtual, pluginMaxVirtualAll, pluginMaxVirtualBytes, pluginMaxVirtualBytesAll = perPlugin, all, bytesPerPlugin, bytesAll
}

// The buffer count is a quota of each plugin: one plugin that fills its own cannot take the
// places of another, and the editor-wide ceiling stops all of them together.
func TestPluginCreateBufferQuota(t *testing.T) {
	quotas(t, 3, 5, 1<<20, 1<<20)
	pe, ed, _ := pluginEditorOver(t, "x")
	make1 := func(plugin, name string) error {
		_, err := pe.CreateBuffer(pluginapi.CreateBuffer{Plugin: plugin, Name: name})
		return err
	}
	for i := range 3 {
		apptest.Must(t, make1("greedy", fmt.Sprint("b", i)))
	}
	if err := make1("greedy", "one more"); codeOf(err) != pluginapi.CodeLimit {
		t.Fatalf("CreateBuffer past the plugin's quota = %v, want limit", err)
	}
	apptest.Must(t, make1("modest", "mine"))
	apptest.Must(t, make1("modest", "mine too"))
	if err := make1("third", "no room"); codeOf(err) != pluginapi.CodeLimit {
		t.Fatalf("CreateBuffer past the editor-wide ceiling = %v, want limit", err)
	}
	// closing one of greedy's frees a place for greedy
	apptest.Must(t, pe.Close(pluginapi.BufferID(ed.DocAt(1).ID()), false))
	apptest.Must(t, make1("greedy", "again"))
}

// The text of virtual buffers lives in host memory, outside the Lua heap guard, so a plugin
// has a byte budget of its own, and the editor one for all. A replaced content frees
// the bytes it held; a closed buffer frees its text.
func TestPluginVirtualTextBudget(t *testing.T) {
	quotas(t, 16, 16, 100, 150)
	pe, _, _ := pluginEditorOver(t, "x")
	text := func(n int) string { return strings.Repeat("a", n) }
	create := func(plugin string, n int) (pluginapi.BufferID, error) {
		return pe.CreateBuffer(pluginapi.CreateBuffer{Plugin: plugin, Name: "v", Text: text(n)})
	}
	a1, err := create("a", 60)
	apptest.Must(t, err)
	if _, err := create("a", 41); codeOf(err) != pluginapi.CodeLimit {
		t.Fatalf("CreateBuffer over the plugin's budget = %v, want limit", err)
	}
	a2, err := create("a", 40)
	apptest.Must(t, err)
	if err := pe.SetContent(a2, []pluginapi.ContentEntry{{Text: text(41)}}); codeOf(err) != pluginapi.CodeLimit {
		t.Fatalf("SetContent over the plugin's budget = %v, want limit", err)
	}
	if got, _, _ := pe.FullText(a2); got != text(40) {
		t.Fatalf("a refused SetContent changed the text to %d bytes", len(got))
	}
	// replacing the content counts the new size, not the sum
	apptest.Must(t, pe.SetContent(a1, []pluginapi.ContentEntry{{Text: text(10)}}))
	apptest.Must(t, pe.SetContent(a2, []pluginapi.ContentEntry{{Text: text(90)}}))
	// another plugin has its own budget, up to the editor-wide one (100 + 50 of 150)
	if _, err := create("b", 51); codeOf(err) != pluginapi.CodeLimit {
		t.Fatalf("CreateBuffer over the editor-wide budget = %v, want limit", err)
	}
	b1, err := create("b", 50)
	apptest.Must(t, err)
	// closing frees the text
	apptest.Must(t, pe.Close(b1, false))
	if _, err := create("b", 50); err != nil {
		t.Fatalf("CreateBuffer after a close: %v", err)
	}
}

func TestPluginSetContent(t *testing.T) {
	red := pluginapi.Style{FG: pluginapi.Color{Key: "error"}}
	bold := pluginapi.Style{Bold: true}
	// span is what the painter gets for one line: byte columns, End -1 when the run goes on
	// past the line break.
	type span struct {
		line, start, end int
		bold, red        bool
		priority         int
	}
	tests := []struct {
		name    string
		entries []pluginapi.ContentEntry
		text    string
		spans   []span
	}{
		{"nothing", nil, "", nil},
		{"verbatim concatenation, no styles", []pluginapi.ContentEntry{{Text: "ab"}, {Text: "c\nd\n"}, {Text: "e"}}, "abc\nd\ne", nil},
		{"entry style stops before the line break", []pluginapi.ContentEntry{{Text: "head\n", Style: bold}, {Text: "body\n"}},
			"head\nbody\n", []span{{0, 0, 4, true, false, 0}}},
		{"a style on a line break alone is nothing", []pluginapi.ContentEntry{{Text: "\n", Style: bold}}, "\n", nil},
		{"multi-line entry", []pluginapi.ContentEntry{{Text: "a\nbb\n", Style: red}}, "a\nbb\n",
			[]span{{0, 0, -1, false, true, 0}, {1, 0, 2, false, true, 0}}},
		{"segments are the text and sit over the entry style",
			[]pluginapi.ContentEntry{{Text: "ignored", Style: bold, Segments: []pluginapi.Segment{{Text: "ab", Style: red}, {Text: "cd"}, {Text: "é\n"}}}},
			"abcdé\n", []span{{0, 0, 6, true, false, 0}, {0, 0, 2, false, true, 1}}},
		{"byte columns after multi-byte text", []pluginapi.ContentEntry{{Text: "é"}, {Text: "x", Style: bold}}, "éx", []span{{0, 2, 3, true, false, 0}}},
		{"invalid UTF-8 becomes U+FFFD and CRLF becomes LF", []pluginapi.ContentEntry{{Text: "a\xffb\r\n"}, {Text: "c", Style: bold}},
			"a\uFFFDb\nc", []span{{1, 0, 1, true, false, 0}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pe, _, regular, id := virtualOver(t, pluginapi.CreateBuffer{})
			apptest.Must(t, pe.SetContent(id, tt.entries))
			if got := textOf(pe, id); got != tt.text {
				t.Fatalf("text = %q, want %q", got, tt.text)
			}
			if text := textOf(pe, regular); text != "regular" {
				t.Fatalf("the regular buffer changed: %q", text)
			}
			var got []span
			for line := 0; line <= strings.Count(tt.text, "\n"); line++ {
				for _, sp := range pe.DecorStore().Line(uint64(id), line).Spans {
					got = append(got, span{line, sp.Start, sp.End, sp.Style.Bold, sp.Style.FG.Key == "error", sp.Priority})
				}
			}
			if !slices.Equal(got, tt.spans) {
				t.Fatalf("spans = %+v, want %+v", got, tt.spans)
			}
		})
	}
}

func TestPluginSetContentIsAtomicAndReplacesWhole(t *testing.T) {
	pe, _, _, id := virtualOver(t, pluginapi.CreateBuffer{Text: "old text"})
	bold := pluginapi.Style{Bold: true}
	apptest.Must(t, pe.SetContent(id, []pluginapi.ContentEntry{{Text: "first\n", Style: bold, Props: map[string]any{"k": "v"}}, {Text: "second"}}))
	info, _ := pe.Buffer(id)
	if info.Modified {
		t.Fatal("SetContent marked the buffer modified")
	}
	// A content that does not fit changes nothing.
	tooMany := make([]pluginapi.ContentEntry, decorations.MaxPerNamespace+1)
	for i := range tooMany {
		tooMany[i] = pluginapi.ContentEntry{Text: "x\n", Style: bold}
	}
	before := textOf(pe, id)
	beforeVer, _ := pe.Buffer(id)
	if err := pe.SetContent(id, tooMany); codeOf(err) != pluginapi.CodeLimit {
		t.Fatalf("SetContent with %d styled runs = %v, want limit", len(tooMany), err)
	}
	if after := textOf(pe, id); after != before {
		t.Fatalf("a refused SetContent changed the text: %q", after)
	}
	if now, _ := pe.Buffer(id); now.Version != beforeVer.Version {
		t.Fatal("a refused SetContent changed the version")
	}
	if props, _ := pe.PropsAt(id, apos(0, 0)); props["k"] != "v" {
		t.Fatalf("a refused SetContent dropped the props: %v", props)
	}
	if n := pe.DecorStore().Count(uint64(id)); n != 1 {
		t.Fatalf("styles = %d after a refused SetContent, want the one from before", n)
	}
	// A second SetContent replaces the styles and the props.
	apptest.Must(t, pe.SetContent(id, []pluginapi.ContentEntry{{Text: "plain"}}))
	if n := pe.DecorStore().Count(uint64(id)); n != 0 {
		t.Fatalf("styles = %d after replacing the content, want 0", n)
	}
	if props, _ := pe.PropsAt(id, apos(0, 0)); props != nil {
		t.Fatalf("props = %v after replacing the content, want none", props)
	}
}

func TestPluginSetContentRefusals(t *testing.T) {
	tests := []struct {
		name    string
		target  func(regular, virtual pluginapi.BufferID) pluginapi.BufferID
		entries []pluginapi.ContentEntry
		want    pluginapi.Code
	}{
		{"a regular buffer", func(r, _ pluginapi.BufferID) pluginapi.BufferID { return r }, nil, pluginapi.CodeInvalid},
		{"a closed buffer", func(_, _ pluginapi.BufferID) pluginapi.BufferID { return 999 }, nil, pluginapi.CodeClosed},
		{"over the byte cap", func(_, v pluginapi.BufferID) pluginapi.BufferID { return v },
			[]pluginapi.ContentEntry{{Text: strings.Repeat("x", editor.MaxVirtualText/2+1)}, {Text: strings.Repeat("x", editor.MaxVirtualText/2+1)}}, pluginapi.CodeTooLarge},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pe, _, regular, id := virtualOver(t, pluginapi.CreateBuffer{Text: "keep"})
			if err := pe.SetContent(tt.target(regular, id), tt.entries); codeOf(err) != tt.want {
				t.Fatalf("SetContent = %v, want code %q", err, tt.want)
			}
			if got := textOf(pe, id); got != "keep" {
				t.Fatalf("virtual text = %q, want it unchanged", got)
			}
			if got := textOf(pe, regular); got != "regular" {
				t.Fatalf("regular text = %q, want it unchanged", got)
			}
		})
	}
}

func TestPluginSetContentKeepsCursorLineAndTakesNewGeneration(t *testing.T) {
	pe, _, _, id := virtualOver(t, pluginapi.CreateBuffer{Text: "a\nb\nc"})
	apptest.Must(t, pe.SetCursor(id, apos(2, 0), false, false, false))
	before, _ := pe.Buffer(id)
	apptest.Must(t, pe.SetContent(id, []pluginapi.ContentEntry{{Text: "1\n2\n3\n4\n"}}))
	after, _ := pe.Buffer(id)
	if after.Version == before.Version || after.Version.Gen == before.Version.Gen {
		t.Fatalf("version %v after %v, want a new generation", after.Version, before.Version)
	}
	cs, _ := pe.Cursors(id)
	if cs[0].Pos.Line != 2 {
		t.Fatalf("cursor = %v, want line 2", cs[0].Pos)
	}
}

func TestPluginPropsAt(t *testing.T) {
	pe, _, regular, id := virtualOver(t, pluginapi.CreateBuffer{})
	apptest.Must(t, pe.SetContent(id, []pluginapi.ContentEntry{
		{Text: "title\n"},
		{Text: "one\n", Props: map[string]any{"line": int64(10), "kind": "h1"}},
		{Segments: []pluginapi.Segment{{Text: "tw"}, {Text: "o\n"}}, Props: map[string]any{"line": int64(20), "nested": map[string]any{"a": []any{int64(1)}}}},
		{Text: "last", Props: map[string]any{"line": int64(30)}},
	}))
	tests := []struct {
		name string
		pos  pluginapi.Pos
		want any // the "line" prop, or nil
	}{
		{"title has none", apos(0, 2), nil},
		{"first entry start", apos(1, 0), int64(10)},
		{"first entry end of line", apos(1, 3), int64(10)},
		{"segments entry", apos(2, 1), int64(20)},
		{"last entry", apos(3, 2), int64(30)},
		{"end of the document", apos(3, 4), int64(30)},
		{"negative position clamps to the start", apos(-4, -4), nil},
		{"past the end", apos(50, 50), int64(30)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := pe.PropsAt(id, tt.pos)
			apptest.Must(t, err)
			if tt.want == nil {
				if got != nil {
					t.Fatalf("props = %v, want none", got)
				}
				return
			}
			if got["line"] != tt.want {
				t.Fatalf("props = %v, want line %v", got, tt.want)
			}
		})
	}
	if got, _ := pe.PropsAt(id, apos(2, 0)); fmt.Sprint(got["nested"]) != "map[a:[1]]" {
		t.Errorf("nested props = %v", got["nested"])
	}
	if props, err := pe.PropsAt(regular, apos(0, 0)); props != nil || err != nil {
		t.Errorf("PropsAt on a regular buffer = %v, %v, want nil, nil", props, err)
	}
	if _, err := pe.PropsAt(999, apos(0, 0)); codeOf(err) != pluginapi.CodeClosed {
		t.Errorf("PropsAt on a closed buffer = %v", err)
	}
	// an edit leaves props with nothing to describe
	apptest.Must(t, pe.SetReadOnly(id, false))
	apptest.Must(t, pe.Edit(id, []pluginapi.Edit{{Range: arange(0, 0, 0, 0), Text: "X"}}, pluginapi.EditOptions{}))
	if props, _ := pe.PropsAt(id, apos(1, 0)); props != nil {
		t.Errorf("props = %v after an edit, want none", props)
	}
}

func TestPluginSetReadOnly(t *testing.T) {
	pe, _, regular, id := virtualOver(t, pluginapi.CreateBuffer{ReadOnly: true})
	edit := []pluginapi.Edit{{Range: arange(0, 0, 0, 0), Text: "x"}}
	if err := pe.Edit(id, edit, pluginapi.EditOptions{}); codeOf(err) != pluginapi.CodeReadOnly {
		t.Fatalf("Edit on a read-only virtual buffer = %v", err)
	}
	// the plugin's own content write goes through the read-only flag
	apptest.Must(t, pe.SetContent(id, []pluginapi.ContentEntry{{Text: "from the plugin"}}))
	apptest.Must(t, pe.SetReadOnly(id, false))
	apptest.Must(t, pe.Edit(id, edit, pluginapi.EditOptions{}))
	if info, _ := pe.Buffer(id); info.ReadOnly || info.Modified {
		t.Fatalf("info = %+v: want writable and never modified", info)
	}
	apptest.Must(t, pe.SetReadOnly(id, true))
	if err := pe.Edit(id, edit, pluginapi.EditOptions{}); codeOf(err) != pluginapi.CodeReadOnly {
		t.Fatalf("Edit after SetReadOnly(true) = %v", err)
	}
	if err := pe.SetReadOnly(regular, true); codeOf(err) != pluginapi.CodeInvalid {
		t.Fatalf("SetReadOnly on a regular buffer = %v, want invalid", err)
	}
	if err := pe.SetReadOnly(999, true); codeOf(err) != pluginapi.CodeClosed {
		t.Fatalf("SetReadOnly on a closed buffer = %v, want closed", err)
	}
}

func TestPluginSetBufferMode(t *testing.T) {
	pe, _, regular, id := virtualOver(t, pluginapi.CreateBuffer{})
	for _, target := range []pluginapi.BufferID{id, regular} {
		apptest.Must(t, pe.SetBufferMode(target, "outline"))
		if info, _ := pe.Buffer(target); info.Mode != "outline" {
			t.Fatalf("mode = %q", info.Mode)
		}
		apptest.Must(t, pe.SetBufferMode(target, ""))
		if info, _ := pe.Buffer(target); info.Mode != "" {
			t.Fatalf("mode = %q after clearing", info.Mode)
		}
	}
	if err := pe.SetBufferMode(id, strings.Repeat("m", pluginMaxModeName+1)); codeOf(err) != pluginapi.CodeInvalid {
		t.Fatalf("a long mode name = %v", err)
	}
	if err := pe.SetBufferMode(999, "x"); codeOf(err) != pluginapi.CodeClosed {
		t.Fatalf("mode of a closed buffer = %v", err)
	}
}

func TestPluginOptions(t *testing.T) {
	type step struct {
		name string
		v    any
		code pluginapi.Code // "" = ok
		want any            // GetOption afterwards
	}
	tests := []struct {
		option string
		steps  []step
	}{
		{"wrap", []step{
			{"off", false, "", false}, {"on", true, "", true}, {"cleared", nil, "", nil},
			{"not a bool", "yes", pluginapi.CodeInvalid, nil},
		}},
		{"line_numbers", []step{
			{"off", false, "", false}, {"cleared", nil, "", nil}, {"number", int64(1), pluginapi.CodeInvalid, nil},
		}},
		{"tab_size", []step{
			{"8", int64(8), "", 8}, {"float", float64(2), "", 2}, {"cleared", nil, "", nil},
			{"zero", int64(0), pluginapi.CodeInvalid, nil}, {"too big", int64(editor.MaxViewTabSize + 1), pluginapi.CodeInvalid, nil},
			{"fraction", float64(2.5), pluginapi.CodeInvalid, nil}, {"string", "4", pluginapi.CodeInvalid, nil},
		}},
	}
	for _, tt := range tests {
		t.Run(tt.option, func(t *testing.T) {
			for _, targetKind := range []string{"virtual", "regular"} {
				pe, _, regular, id := virtualOver(t, pluginapi.CreateBuffer{})
				target := id
				if targetKind == "regular" {
					target = regular
				}
				for _, s := range tt.steps {
					err := pe.SetOption(target, tt.option, s.v)
					if codeOf(err) != s.code {
						t.Fatalf("%s %s: SetOption = %v, want code %q", targetKind, s.name, err, s.code)
					}
					got, gerr := pe.GetOption(target, tt.option)
					apptest.Must(t, gerr)
					if s.code == "" && got != s.want {
						t.Fatalf("%s %s: GetOption = %v, want %v", targetKind, s.name, got, s.want)
					}
				}
			}
		})
	}
	pe, _, _, id := virtualOver(t, pluginapi.CreateBuffer{})
	if err := pe.SetOption(id, "bogus", true); codeOf(err) != pluginapi.CodeInvalid {
		t.Errorf("SetOption(bogus) = %v", err)
	}
	if _, err := pe.GetOption(id, "bogus"); codeOf(err) != pluginapi.CodeInvalid {
		t.Errorf("GetOption(bogus) = %v", err)
	}
	if err := pe.SetOption(999, "wrap", true); codeOf(err) != pluginapi.CodeClosed {
		t.Errorf("SetOption(closed) = %v", err)
	}
	if _, err := pe.GetOption(999, "wrap"); codeOf(err) != pluginapi.CodeClosed {
		t.Errorf("GetOption(closed) = %v", err)
	}
}

// The option is the buffer's own override and the editor's display settings read it.
func TestPluginOptionReachesTheEditorSettings(t *testing.T) {
	pe, ed, _, id := virtualOver(t, pluginapi.CreateBuffer{})
	apptest.Must(t, pe.SetOption(id, "wrap", false))
	apptest.Must(t, pe.SetOption(id, "tab_size", int64(3)))
	d, _, _ := ed.DocByID(editor.DocID(id))
	if s := ed.SettingsFor(d); s.LineWrap || s.TabSize != 3 {
		t.Fatalf("settings = wrap %v tab %d, want false and 3", s.LineWrap, s.TabSize)
	}
}

// The styles of a closed virtual buffer do not stay in the store.
func TestPluginClosedVirtualBufferDropsItsStyles(t *testing.T) {
	pe, _, _, id := virtualOver(t, pluginapi.CreateBuffer{})
	apptest.Must(t, pe.SetContent(id, []pluginapi.ContentEntry{{Text: "x", Style: pluginapi.Style{Bold: true}}}))
	if pe.DecorStore().Count(uint64(id)) != 1 {
		t.Fatal("the style is missing")
	}
	apptest.Must(t, pe.Close(id, false))
	if _, err := pe.CreateBuffer(pluginapi.CreateBuffer{Name: "next"}); err != nil {
		t.Fatal(err)
	}
	if n := pe.DecorStore().Count(uint64(id)); n != 0 {
		t.Fatalf("%d styles of a closed buffer are left", n)
	}
}

// The user can close a virtual buffer without being asked, whatever was done to it.
func TestPluginCloseVirtualNeverAsks(t *testing.T) {
	pe, ed, _, id := virtualOver(t, pluginapi.CreateBuffer{Text: "a"})
	apptest.Must(t, pe.Edit(id, []pluginapi.Edit{{Range: arange(0, 1, 0, 1), Text: "b"}}, pluginapi.EditOptions{}))
	if err := pe.Close(id, false); err != nil {
		t.Fatalf("Close = %v", err)
	}
	if _, _, ok := ed.DocByID(editor.DocID(id)); ok {
		t.Fatal("the buffer is still open")
	}
}

// ---------- exclusion sites ----------

func TestSessionLeavesOutVirtualBuffers(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "a.txt")
	apptest.Must(t, os.WriteFile(file, []byte("one\n"), 0o644))
	ed := apptest.NewSessionEditor()
	apptest.Must(t, ed.OpenFile(file))
	id := ed.NewVirtual(editor.VirtualOptions{Name: "outline", Text: "heading\nbody"})
	// edited, active: still not part of the session
	apptest.Must(t, ed.ActivateID(id))
	apptest.Must(t, ed.ApplyEdits(id, []editor.LSPEdit{{From: buffer.Pos{}, To: buffer.Pos{}, Text: "X"}}, editor.EditOptions{}))
	sess := filepath.Join(dir, "session")
	apptest.Must(t, restore.SaveSession(ed, sess, tui.ExplorerState{}))
	s, ok, err := session.Load(sess)
	apptest.Must(t, err)
	if !ok || len(s.Files) != 1 || s.Files[0].Path != file {
		t.Fatalf("session files = %+v, want only %s", s.Files, file)
	}
	if len(apptest.StashFiles(t, sess)) != 0 {
		t.Fatalf("stash files %v written for a virtual buffer", apptest.StashFiles(t, sess))
	}
	if ed.ActiveID() != id {
		t.Fatal("saving the session changed the active tab")
	}
}

func TestRecoveryWriterLeavesOutVirtualBuffers(t *testing.T) {
	dir := t.TempDir()
	rec := filepath.Join(dir, "recovery")
	ed := apptest.NewSessionEditor()
	id := ed.NewVirtual(editor.VirtualOptions{Name: "v", Text: "text"})
	apptest.Must(t, ed.ApplyEdits(id, []editor.LSPEdit{{From: buffer.Pos{}, To: buffer.Pos{}, Text: "X"}}, editor.EditOptions{}))
	w := restore.NewRecoveryWriter(ed, rec)
	apptest.Must(t, w.Plan()())
	if _, err := os.Stat(rec); err == nil {
		t.Fatal("a recovery copy was made for a virtual buffer alone")
	}
	// next to a real unsaved document, only that one is kept
	apptest.Must(t, ed.Do("new", nil).Err)
	ed.Do("insert_char", map[string]any{"text": "real"})
	apptest.Must(t, w.Plan()())
	s, _, err := session.Load(rec)
	apptest.Must(t, err)
	if len(s.Files) != 1 {
		t.Fatalf("recovery files = %+v, want the one regular document", s.Files)
	}
	text, err := session.ReadStash(rec, s.Files[0].StashFile)
	apptest.Must(t, err)
	if text != "r" && text != "real" {
		t.Fatalf("stash = %q, want the regular document's text", text)
	}
}

// textOf returns the text of a buffer through the plugin port.
func textOf(pe *pluginEditor, id pluginapi.BufferID) string {
	text, _, _ := pe.FullText(id)
	return text
}
