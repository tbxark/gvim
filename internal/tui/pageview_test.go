package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/buffer"
	"github.com/tbxark/gvim/editor"
	"github.com/tbxark/gvim/internal/tui/editorpane"
)

// pageDoc is a document with the optional page-view view state and a path.
type pageDoc struct {
	*fakeDoc
	path string
	view editor.ViewSettings
}

func (d pageDoc) ViewSettings() editor.ViewSettings { return d.view }
func (d pageDoc) FilePath() string                  { return d.path }

// largePageDoc is a large-file document.
type largePageDoc struct{ pageDoc }

func (largePageDoc) Large() bool             { return true }
func (largePageDoc) Index() buffer.IndexInfo { return buffer.IndexInfo{} }

func boolPtrPage(v bool) *bool { return &v }

func TestMarkdownDocDetection(t *testing.T) {
	t.Parallel()
	m, _ := viewModel(t, DefaultViewOptions(), "abc\n", 80, 8)
	tests := []struct {
		name string
		doc  Document
		want bool
	}{
		{"markdown suffix", pageDoc{fakeDoc: &fakeDoc{lines: []string{"x"}}, path: "/w/a.md"}, true},
		{"markdown long suffix", pageDoc{fakeDoc: &fakeDoc{lines: []string{"x"}}, path: "/w/a.markdown"}, true},
		{"mdx", pageDoc{fakeDoc: &fakeDoc{lines: []string{"x"}}, path: "/w/a.mdx"}, true},
		{"text file", pageDoc{fakeDoc: &fakeDoc{lines: []string{"x"}}, path: "/w/a.txt"}, false},
	}
	for _, tt := range tests {
		if got := m.markdownDoc(tt.doc); got != tt.want {
			t.Fatalf("%s: markdownDoc = %v, want %v", tt.name, got, tt.want)
		}
	}
}

func TestPageViewForResolvesTheBufferOverride(t *testing.T) {
	t.Parallel()
	m, _ := viewModel(t, DefaultViewOptions(), "abc\n", 80, 8)
	doc := pageDoc{fakeDoc: &fakeDoc{lines: []string{"x"}}, path: "/w/a.md",
		view: editor.ViewSettings{PageView: boolPtrPage(true), PageWidth: 42}}
	pv := m.pageViewFor(doc)
	if !pv.On || pv.Measure != 42 || !pv.Markdown {
		t.Fatalf("pageViewFor = %+v, want on at 42 with Markdown", pv)
	}
	off := pageDoc{fakeDoc: &fakeDoc{lines: []string{"x"}}, path: "/w/a.md",
		view: editor.ViewSettings{PageView: boolPtrPage(false)}}
	if pv := m.pageViewFor(off); pv.On {
		t.Fatalf("pageViewFor on an off override = %+v", pv)
	}
	// A large file falls back to the normal pane.
	large := largePageDoc{pageDoc{fakeDoc: &fakeDoc{lines: []string{"x"}}, path: "/w/a.md",
		view: editor.ViewSettings{PageView: boolPtrPage(true)}}}
	if pv := m.pageViewFor(large); pv.On {
		t.Fatalf("large-file page view = %+v, want the fallback", pv)
	}
}

// checkPageViewPane checks that the page view reaches the pane built for a doc.
func TestPageViewReachesThePaneThroughTheView(t *testing.T) {
	t.Parallel()
	m, _ := viewModel(t, DefaultViewOptions(), "abc\n", 80, 8)
	doc := pageDoc{fakeDoc: &fakeDoc{lines: []string{"Some **bold** text"}}, path: "/w/a.md",
		view: editor.ViewSettings{PageView: boolPtrPage(true), PageWidth: 30}}
	p := m.viewPane(editorpane.Pane{}, doc)
	if p.PageView != (editorpane.PageView{On: true, Measure: 30, Markdown: true}) {
		t.Fatalf("pane page view = %+v", p.PageView)
	}
}

func TestPageViewComposesInTheView(t *testing.T) {
	t.Parallel()
	ed := editor.New(editor.Options{})
	m, ed := viewModelWith(t, ed, DefaultViewOptions(), "Some **bold** text\nplain\ntail\n", 60, 8)
	ed.Active().SetLanguage("markdown")
	// Keep the caret off the line under test so its markup stays concealed.
	ed.SetCursor(buffer.Pos{Line: 2, Col: 0}, false)
	runAction(t, m, "toggle_page_view")
	runAction(t, m, "set_page_width")
	press(m, typed("20"))
	settle(t, m, press(m, special(tea.KeyEnter)))
	rows := frameText(m)
	found := false
	for _, row := range rows {
		if !strings.Contains(row, "bold") {
			continue
		}
		found = true
		if strings.Contains(row, "**") {
			t.Fatalf("row = %q: the markup is not concealed", row)
		}
		if !strings.HasPrefix(row, " ") {
			t.Fatalf("row = %q: the page has no left margin", row)
		}
	}
	if !found {
		t.Fatalf("the composed text is not in the frame: %q", rows)
	}
}
