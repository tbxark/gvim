package bundled_test

import (
	"io/fs"
	"slices"
	"strings"
	"testing"

	"github.com/tbxark/gvim/plugin/bundled"
	"github.com/tbxark/gvim/plugin/plugintest"
	"github.com/tbxark/gvim/pluginapi"
)

// compareFilesFiles reads the plugin's files out of the embedded bundled tree,
// including the i18n table the shared rig does not load.
func compareFilesFiles(t *testing.T) plugintest.Files {
	t.Helper()
	files := plugintest.Files{}
	for _, name := range []string{"init.lua", "compare_files.i18n.json"} {
		data, err := fs.ReadFile(bundled.FS, "plugins/compare_files/"+name)
		if err != nil {
			t.Fatalf("read the bundled plugin file %s: %v", name, err)
		}
		files["compare_files/"+name] = string(data)
	}
	return files
}

// compareRig is one comparison of two fake buffers: a.txt with the bytes
// "a\nb\nc" and b.txt with "a\nX\nc".
type compareRig struct {
	t     *testing.T
	h     *plugintest.Harness
	host  *plugintest.FakeHost
	left  pluginapi.BufferID
	right pluginapi.BufferID
}

const (
	compareLeftPath  = "/w/a.txt"
	compareRightPath = "/w/b.txt"
)

func startCompare(t *testing.T) *compareRig {
	t.Helper()
	host := plugintest.NewFakeHost()
	left := host.AddBuffer(compareLeftPath, "a\nb\nc")
	right := host.AddBuffer(compareRightPath, "a\nX\nc")
	os := plugintest.NewFakeOS()
	os.SetFile(compareLeftPath, "a\nb\nc")
	os.SetFile(compareRightPath, "a\nX\nc")
	h := plugintest.New(t, compareFilesFiles(t), plugintest.Options{
		Host: host, OS: os, Services: &plugintest.FakeServices{}, Timeout: 2 * 1e9,
	})
	h.Start()
	return &compareRig{t: t, h: h, host: host, left: left, right: right}
}

// prompt answers the open gvim.ui.input with value.
func (r *compareRig) prompt(value string) {
	r.t.Helper()
	var overlay *plugintest.FakeOverlay
	for _, o := range r.host.UI().Opened() {
		if o.Kind == "input" {
			overlay = o
		}
	}
	if overlay == nil {
		r.t.Fatalf("no input prompt is open (log %v)", r.h.LogText("compare_files"))
	}
	r.h.Resolve(pluginapi.Completion{Token: overlay.Token, Values: []any{value}})
	r.h.Settle()
}

// open runs Compare Files... and answers the two prompts.
func (r *compareRig) open() {
	r.t.Helper()
	if err := r.h.Run("compare_files.compare_files", nil); err != nil {
		r.t.Fatalf("Compare Files: %v", err)
	}
	r.prompt(compareLeftPath)
	r.prompt(compareRightPath)
}

// group returns the one open comparison group.
func (r *compareRig) group() *plugintest.FakeGroup {
	r.t.Helper()
	g := r.host.Groups()
	if len(g.ByID) != 1 {
		r.t.Fatalf("groups = %d, want the one comparison", len(g.ByID))
	}
	for _, fg := range g.ByID {
		return fg
	}
	return nil
}

// bufferText returns a fake buffer's lines.
func (r *compareRig) bufferText(id pluginapi.BufferID) []string {
	r.t.Helper()
	for _, b := range r.host.Buffers {
		if b.Info.ID == id {
			return b.Lines
		}
	}
	r.t.Fatalf("no fake buffer %d", id)
	return nil
}

// TestCompareFilesOpensPaintsAndNavigates checks the port's main flow: the two
// files are shown as a group of two panels, the title segments name them, scroll
// sync is created with one anchor per change, and the change is decorated with a
// line background, a span, a gutter glyph and a scrollbar mark.
func TestCompareFilesOpensPaintsAndNavigates(t *testing.T) {
	r := startCompare(t)
	r.open()

	g := r.group()
	if g.Closed {
		t.Fatal("the comparison group is closed")
	}
	leftPanel, rightPanel := g.Panels["left"], g.Panels["right"]
	if leftPanel.Buffer != r.left || rightPanel.Buffer != r.right {
		t.Fatalf("panels = %+v / %+v, want the two file buffers", leftPanel, rightPanel)
	}
	if len(g.Title) == 0 || !strings.Contains(titleText(g.Title), "a.txt") || !strings.Contains(titleText(g.Title), "b.txt") {
		t.Fatalf("title = %+v, want both file names", g.Title)
	}
	if g.Focused != "left" {
		t.Fatalf("focused panel = %q, want left", g.Focused)
	}
	scroll := r.host.Groups().Scroll
	if len(scroll) != 1 {
		t.Fatalf("scroll sync groups = %d, want 1", len(scroll))
	}
	for _, sg := range scroll {
		if sg.Left != leftPanel.Window || sg.Right != rightPanel.Window {
			t.Fatalf("scroll sync windows = %d/%d, want the panel windows %d/%d",
				sg.Left, sg.Right, leftPanel.Window, rightPanel.Window)
		}
		if len(sg.Anchors) != 1 || sg.Anchors[0].Left != 2 || sg.Anchors[0].Right != 2 {
			t.Fatalf("anchors = %+v, want the change's end 2/2", sg.Anchors)
		}
	}
	// The changed line is painted: a background line, its differing span, a
	// gutter glyph and a scrollbar range on each side.
	check := func(id pluginapi.BufferID, glyph string) {
		if got := r.host.Decor().Items("compare_files", "compare_files_lines", id); len(got) != 1 ||
			got[0].Line != 1 || got[0].Style.BG.Key != "editor.diff_modify_bg" {
			t.Fatalf("line decorations = %+v, want one modified background on line 1", got)
		}
		if got := r.host.Decor().Items("compare_files", "compare_files_spans", id); len(got) != 1 ||
			got[0].Range.From.Line != 1 || got[0].Range.To.Col != 1 {
			t.Fatalf("span decorations = %+v, want the changed byte on line 1", got)
		}
		if got := r.host.Decor().Items("compare_files", "compare_files_gutter", id); len(got) != 1 ||
			got[0].Text != glyph {
			t.Fatalf("gutter decorations = %+v, want the %q glyph", got, glyph)
		}
		if got := r.host.Decor().Items("compare_files", "compare_files_scroll", id); len(got) != 1 {
			t.Fatalf("scroll decorations = %+v, want one mark", got)
		}
	}
	check(r.left, "~")
	check(r.right, "~")

	// The cursors sit at the change after the opening jump.
	for _, id := range []pluginapi.BufferID{r.left, r.right} {
		carets, _ := r.host.Cursors(id)
		if len(carets) == 0 || carets[0].Pos.Line != 1 {
			t.Fatalf("cursor on buffer %d = %+v, want line 1", id, carets)
		}
	}
	// Next Difference from the change wraps and says so.
	if err := r.h.Run("compare_files.next_difference", nil); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(r.h.NoticeTexts(), " | "); !strings.Contains(got, "Difference 1/1 [wrapped]") {
		t.Fatalf("notices = %q, want the wrapped difference status", got)
	}
}

// TestCompareFilesCopiesAndTakesAChange checks the two merge commands: Copy
// Change to Other Side makes the other side's hunk bytes equal, Take Change from
// Other Side does the reverse, each through replace_range one edit at a time.
func TestCompareFilesCopiesAndTakesAChange(t *testing.T) {
	t.Run("copy", func(t *testing.T) {
		r := startCompare(t)
		r.open()
		if err := r.h.Run("compare_files.copy_change", nil); err != nil {
			t.Fatal(err)
		}
		if got := r.bufferText(r.right); !slices.Equal(got, []string{"a", "b", "c"}) {
			t.Fatalf("right lines = %q, want the copied change", got)
		}
		if got := strings.Join(r.h.NoticeTexts(), " | "); !strings.Contains(got, "the files are identical") {
			t.Fatalf("notices = %q, want the identical summary after the copy", got)
		}
	})
	t.Run("take", func(t *testing.T) {
		r := startCompare(t)
		r.open()
		if err := r.h.Run("compare_files.take_change", nil); err != nil {
			t.Fatal(err)
		}
		if got := r.bufferText(r.left); !slices.Equal(got, []string{"a", "X", "c"}) {
			t.Fatalf("left lines = %q, want the taken change", got)
		}
	})
}

// TestCompareFilesClosesAndRefuses checks the close command and the refusal of a
// file that cannot be compared: the group is closed and nothing is decorated.
func TestCompareFilesClosesAndRefuses(t *testing.T) {
	t.Run("close", func(t *testing.T) {
		r := startCompare(t)
		r.open()
		if err := r.h.Run("compare_files.close", nil); err != nil {
			t.Fatal(err)
		}
		if !r.group().Closed {
			t.Fatal("the group is still open after Compare: Close")
		}
		for _, sg := range r.host.Groups().Scroll {
			if !sg.Removed {
				t.Fatal("the scroll-sync group is still mounted after Compare: Close")
			}
		}
	})
	t.Run("uncomparable", func(t *testing.T) {
		r := startCompare(t)
		for _, b := range r.host.Buffers {
			if b.Info.ID == r.right {
				b.Info.Large = true
			}
		}
		r.open()
		for _, g := range r.host.Groups().ByID {
			if !g.Closed {
				t.Fatal("an open group was left behind for a large-file refusal")
			}
		}
		if got := strings.Join(r.h.NoticeTexts(), " | "); !strings.Contains(got, "large-file mode") {
			t.Fatalf("notices = %q, want the refusal", got)
		}
	})
}

// titleText joins the group title segments.
func titleText(segments []pluginapi.BufferGroupTitleSegment) string {
	var b strings.Builder
	for _, s := range segments {
		b.WriteString(s.Text)
	}
	return b.String()
}
