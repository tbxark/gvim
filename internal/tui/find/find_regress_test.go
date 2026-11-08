package find

import (
	"reflect"
	"testing"

	"github.com/tbxark/gvim/buffer"
)

func TestSetSearchKeepsLineBreaks(t *testing.T) {
	t.Parallel()
	f := newFake("ab\ncd x ab\ncd")
	b := Open(f, testStyles(), false)
	b.SetSearch("ab\ncd")
	if got := b.Query().Text; got != "ab\ncd" {
		t.Fatalf("query text = %q, want the text with its line break", got)
	}
	ms, err := f.Find(b.Query())
	if err != nil || len(ms) != 2 {
		t.Fatalf("matches = %v, err %v, want two", ms, err)
	}
	// Typing changes the field, so the held text no longer applies.
	press(b, "x")
	if got := b.Query().Text; got == "ab\ncd" || got == "" {
		t.Fatalf("query text after typing = %q, want the field text", got)
	}
	// A later bar that inherits the search sees the same text.
	b.Clear()
	if got := b.Query().Text; got != "" {
		t.Fatalf("query text after Clear = %q", got)
	}
}

func TestEscRestoresAfterF3ThenEdit(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		act  func(b *Bar)
	}{
		{"typing", func(b *Bar) { typeText(b, "x") }},
		{"option toggle", func(b *Bar) { press(b, "alt+c") }},
		{"history walk", func(b *Bar) {
			b.searchHist.push("bar")
			press(b, "up")
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			f := newFake("foo bar foo")
			f.cursor = buffer.Pos{Col: 4}
			b := Open(f, testStyles(), false)
			typeText(b, "foo")
			press(b, "f3")
			if f.sel == nil {
				t.Fatal("F3 selected nothing")
			}
			tt.act(b)
			press(b, "esc")
			if f.sel != nil || f.cursor != (buffer.Pos{Col: 4}) {
				t.Fatalf("caret %v selection %v after Esc, want 0:4 and none", f.cursor, f.sel)
			}
		})
	}
}

func TestStateCarriesHistoryAndToggles(t *testing.T) {
	t.Parallel()
	f := newFake("foo bar")
	b := Open(f, testStyles(), true)
	press(b, "alt+c")
	press(b, "alt+r")
	b.searchHist.push("one")
	b.searchHist.push("two")
	b.replHist.push("x")
	st := b.State()
	want := State{Find: []string{"one", "two"}, Replace: []string{"x"}, Case: true, Regex: true}
	if !reflect.DeepEqual(st, want) {
		t.Fatalf("State = %+v, want %+v", st, want)
	}

	nb := Open(newFake("foo bar"), testStyles(), false)
	nb.Load(st)
	if got := nb.State(); !reflect.DeepEqual(got, want) {
		t.Fatalf("State after Load = %+v, want %+v", got, want)
	}
	nb.Prefill()
	if got := nb.Query(); got.Text != "two" || !got.CaseSensitive || !got.Regex {
		t.Fatalf("query after Prefill = %+v, want two with the toggles", got)
	}
	press(nb, "up")
	if got := nb.Query().Text; got != "two" {
		t.Fatalf("Up gave %q, want the newest entry", got)
	}
}
