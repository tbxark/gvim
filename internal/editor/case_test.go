package editor

import (
	"testing"

	"github.com/tbxark/gvim/internal/buffer"
)

// The case actions use full Unicode case mapping, as Rust's to_uppercase and
// to_lowercase do, and a converted selection stays selected.
func TestCaseTransformUsesUnicodeSpecialCasing(t *testing.T) {
	t.Parallel()
	all := func(text string) placed {
		return placed{sel: &buffer.Selection{Anchor: buffer.Pos{}, Head: buffer.Pos{Col: len([]rune(text))}}}
	}
	tests := []struct {
		name    string
		action  Action
		text    string
		want    string
		wantSel buffer.Selection
	}{
		{"sharp s becomes SS", "to_upper_case", "straße", "STRASSE",
			buffer.Selection{Head: buffer.Pos{Col: 7}}},
		{"ligature fi becomes FI", "to_upper_case", "ﬁx", "FIX",
			buffer.Selection{Head: buffer.Pos{Col: 3}}},
		{"final sigma at the end of a word", "to_lower_case", "ΑΣ", "ας",
			buffer.Selection{Head: buffer.Pos{Col: 2}}},
		{"sigma inside a word stays medial", "to_lower_case", "ΣΊΣΥΦΟΣ", "σίσυφος",
			buffer.Selection{Head: buffer.Pos{Col: 7}}},
		{"dotted capital I lowers to i and a combining dot", "to_lower_case", "İ", "i̇",
			buffer.Selection{Head: buffer.Pos{Col: 2}}},
		{"plain ASCII", "to_upper_case", "abc", "ABC",
			buffer.Selection{Head: buffer.Pos{Col: 3}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			e := docWith(Options{}, tt.text)
			all(tt.text).apply(e)
			run(t, e, tt.action)
			d := e.Active()
			if got := text(e); got != tt.want {
				t.Fatalf("text = %q, want %q", got, tt.want)
			}
			if d.Sel == nil || *d.Sel != tt.wantSel {
				t.Fatalf("selection = %+v, want %+v", d.Sel, tt.wantSel)
			}
		})
	}
}

// A selection made backwards keeps its direction through a conversion, so the next
// conversion of the same text works without selecting again.
func TestCaseTransformKeepsSelectionDirection(t *testing.T) {
	t.Parallel()
	e := docWith(Options{}, "a straße b")
	e.SelectRange(buffer.Pos{Col: 8}, buffer.Pos{Col: 2})
	run(t, e, "to_upper_case")
	if got, want := text(e), "a STRASSE b"; got != want {
		t.Fatalf("text = %q, want %q", got, want)
	}
	d := e.Active()
	want := buffer.Selection{Anchor: buffer.Pos{Col: 9}, Head: buffer.Pos{Col: 2}}
	if d.Sel == nil || *d.Sel != want {
		t.Fatalf("selection = %+v, want %+v", d.Sel, want)
	}
	run(t, e, "to_lower_case")
	if got, want := text(e), "a strasse b"; got != want {
		t.Fatalf("second conversion: text = %q, want %q", got, want)
	}
}

// A conversion over several lines keeps the selection over the converted text.
func TestCaseTransformKeepsAMultiLineSelection(t *testing.T) {
	t.Parallel()
	e := docWith(Options{}, "ß\nab")
	e.SelectRange(buffer.Pos{}, buffer.Pos{Line: 1, Col: 2})
	run(t, e, "to_upper_case")
	if got, want := text(e), "SS\nAB"; got != want {
		t.Fatalf("text = %q, want %q", got, want)
	}
	want := buffer.Selection{Head: buffer.Pos{Line: 1, Col: 2}}
	if d := e.Active(); d.Sel == nil || *d.Sel != want {
		t.Fatalf("selection = %+v, want %+v", d.Sel, want)
	}
}
