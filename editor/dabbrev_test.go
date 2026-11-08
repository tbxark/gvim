package editor

import "testing"

func TestDabbrevExpandCyclesCandidates(t *testing.T) {
	t.Parallel()
	tests := []struct {
		Name    string
		Text    string
		Line    int
		Col     int
		presses int
		Want    []string // text of the caret line after each press
		wantErr bool     // the first press fails and changes nothing
	}{
		{
			Name:    "nearest earlier word first, then the next, then the prefix",
			Text:    "foobar fox fo",
			Col:     13,
			presses: 4,
			Want:    []string{"foobar fox fox", "foobar fox foobar", "foobar fox fo", "foobar fox fox"},
		},
		{
			Name:    "an uppercase prefix matches case-sensitively",
			Text:    "Foobar foo Fo",
			Col:     13,
			presses: 3,
			Want:    []string{"Foobar foo Foobar", "Foobar foo Fo", "Foobar foo Foobar"},
		},
		{
			Name:    "a lowercase prefix matches any case",
			Text:    "FOOBAR foo fo",
			Col:     13,
			presses: 3,
			Want:    []string{"FOOBAR foo foo", "FOOBAR foo FOOBAR", "FOOBAR foo fo"},
		},
		{
			Name:    "a word that is not longer than the prefix is not a candidate",
			Text:    "fo fo",
			Col:     5,
			wantErr: true,
		},
		{
			Name:    "no earlier word matches",
			Text:    "xyz fo",
			Col:     6,
			wantErr: true,
		},
		{
			Name:    "words after the caret are not candidates",
			Text:    "fo\nfoxy",
			Line:    0,
			Col:     2,
			wantErr: true,
		},
		{
			Name:    "no prefix before the caret",
			Text:    "foo ",
			Col:     4,
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			t.Parallel()
			e := withDoc(tt.Text)
			marksCaret(e, tt.Line, tt.Col)
			if tt.wantErr {
				if r := e.Do("dabbrev_expand", nil); r.Err == nil {
					t.Fatal("dabbrev_expand returned nil, want an error")
				}
				if got := e.Active().Buffer.Text(); got != tt.Text {
					t.Fatalf("text changed to %q on failure", got)
				}
				return
			}
			for i := range tt.presses {
				if r := e.Do("dabbrev_expand", nil); r.Err != nil {
					t.Fatalf("press %d: %v", i+1, r.Err)
				}
				if got := e.Active().Buffer.Line(tt.Line); got != tt.Want[i] {
					t.Fatalf("after press %d line = %q, want %q", i+1, got, tt.Want[i])
				}
			}
		})
	}
}
