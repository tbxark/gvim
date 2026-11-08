package tui

import (
	"strings"
	"testing"

	"github.com/tbxark/gvim/buffer"
)

// Tests of the guards of large file mode that the root applies: no language server, no
// compare view, and a bounded send of a selection to the shell.

func TestLSPRefusesALargeDocument(t *testing.T) {
	big := &lspTab{path: "/p/big.go", lines: []string{"package a"}, large: true, size: 123456}
	lt := newLSPTest(t, newFakeLSP("go"), big)
	lt.open()
	if len(lt.port.opens) != 0 || len(lt.m.lsp.tracks) != 0 {
		t.Fatalf("a large document was opened on the server: opens %v, tracks %d", lt.port.opens, len(lt.m.lsp.tracks))
	}
	const want = "LSP: File too large (123456 bytes)"
	for _, action := range []string{
		"lsp_hover", "lsp_goto_definition", "lsp_references", "lsp_completion",
		"lsp_rename", "lsp_code_actions", "show_lsp_status", "lsp_toggle_for_buffer",
	} {
		lt.m.notice = ""
		lt.act(action)
		if lt.m.notice != want {
			t.Errorf("%s: notice = %q, want %q", action, lt.m.notice, want)
		}
	}
	if len(lt.m.overlays) != 0 {
		t.Errorf("lsp_rename opened a prompt for a large document")
	}
	if len(lt.m.lsp.off) != 0 {
		t.Errorf("lsp_toggle_for_buffer switched a large document: %v", lt.m.lsp.off)
	}
	if got := lspDocText(lt.m.ed.Active()); got != "" {
		t.Errorf("lspDocText of a large document = %q, want none", got)
	}
}

func TestLSPStillServesASmallDocumentBesideALargeOne(t *testing.T) {
	small := &lspTab{path: "/p/a.go", lines: []string{"package a"}}
	big := &lspTab{path: "/p/big.go", lines: []string{"package big"}, large: true, size: 99}
	lt := newLSPTest(t, newFakeLSP("go"), small, big)
	lt.open()
	if got := lt.port.opens; len(got) != 1 || got[0] != "/p/a.go" {
		t.Fatalf("opens = %v, want only the small document", got)
	}
	lt.m.notice = ""
	lt.act("show_lsp_status")
	if strings.Contains(lt.m.notice, "too large") {
		t.Errorf("status of the small active document = %q", lt.m.notice)
	}
}

func TestCompareIsRefusedForALargeDocument(t *testing.T) {
	tests := []struct {
		name         string
		left, right  bool
		wantOpen     bool
		wantNameInfo string
	}{
		{"both small", false, false, true, ""},
		{"left large", true, false, false, "a.go"},
		{"right large", false, true, false, "b.go"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := &lspTab{path: "/p/a.go", lines: []string{"x"}, large: tt.left}
			b := &lspTab{path: "/p/b.go", lines: []string{"y"}, large: tt.right}
			lt := newLSPTest(t, nil, a, b)
			if got := lt.m.OpenCompare(0, 1); got != tt.wantOpen {
				t.Fatalf("OpenCompare = %v, want %v", got, tt.wantOpen)
			}
			if lt.m.Comparing() != tt.wantOpen {
				t.Errorf("Comparing = %v, want %v", lt.m.Comparing(), tt.wantOpen)
			}
			if !tt.wantOpen && lt.m.notice != tt.wantNameInfo+" is too large to compare" {
				t.Errorf("notice = %q", lt.m.notice)
			}
		})
	}
}

func TestCompareStepStopsWhenADocumentBecameLarge(t *testing.T) {
	a := &lspTab{path: "/p/a.go", lines: []string{"x"}}
	b := &lspTab{path: "/p/b.go", lines: []string{"y"}}
	lt := newLSPTest(t, nil, a, b)
	if !lt.m.OpenCompare(0, 1) {
		t.Fatal("the compare view did not open")
	}
	if _, ok := lt.m.compareHunks(); !ok {
		t.Fatal("the hunks of two small documents are not found")
	}
	b.large = true // a reload of its file turned it into a large document
	if hunks, ok := lt.m.compareHunks(); ok || hunks != nil {
		t.Errorf("compareHunks = %v, %v for a large document", hunks, ok)
	}
	lt.m.notice = ""
	lt.m.compareStep(true)
	if lt.m.notice != "b.go is too large to compare" {
		t.Errorf("notice = %q", lt.m.notice)
	}
}

// selectedDoc is a document with a selection.
type selectedDoc struct {
	*lspDoc
	from, to buffer.Pos
}

func (d selectedDoc) Selection() (anchor, head buffer.Pos, ok bool) { return d.from, d.to, true }

func TestSentTextOfALargeDocumentIsBounded(t *testing.T) {
	lines := make([]string, 300_000)
	for i := range lines {
		lines[i] = "x"
	}
	end := buffer.Pos{Line: len(lines) - 1, Col: 1}
	short := buffer.Pos{Line: 9, Col: 1}
	tests := []struct {
		name  string
		large bool
		to    buffer.Pos
		want  bool
		lines int
	}{
		{"large, short selection", true, short, true, 10},
		{"large, whole document", true, end, false, 0},
		{"small, whole document", false, end, true, len(lines)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc := selectedDoc{lspDoc: &lspDoc{t: &lspTab{path: "/p/f", lines: lines, large: tt.large}}, to: tt.to}
			text, ok := sentText(doc)
			if ok != tt.want {
				t.Fatalf("ok = %v, want %v", ok, tt.want)
			}
			if got := strings.Count(text, "\n") + 1; ok && got != tt.lines {
				t.Errorf("sent %d lines, want %d", got, tt.lines)
			}
			if !ok && text != "" {
				t.Errorf("a refused send returned %d bytes", len(text))
			}
		})
	}
}

func TestTextBetweenWithinLimits(t *testing.T) {
	doc := &lspDoc{t: &lspTab{path: "/p/f", lines: []string{"abcdef", "ghijkl", "mnopqr"}}}
	end := buffer.Pos{Line: 2, Col: 6}
	tests := []struct {
		name               string
		maxBytes, maxLines int
		want               string
		ok                 bool
	}{
		{"no limit", 0, 0, "abcdef\nghijkl\nmnopqr", true},
		{"bytes fit", 20, 0, "abcdef\nghijkl\nmnopqr", true},
		{"bytes exceeded", 10, 0, "", false},
		{"lines fit", 0, 3, "abcdef\nghijkl\nmnopqr", true},
		{"lines exceeded", 0, 2, "", false},
	}
	for _, tt := range tests {
		got, ok := textBetweenWithin(doc, buffer.Pos{}, end, tt.maxBytes, tt.maxLines)
		if got != tt.want || ok != tt.ok {
			t.Errorf("%s: textBetweenWithin = %q, %v, want %q, %v", tt.name, got, ok, tt.want, tt.ok)
		}
	}
}
