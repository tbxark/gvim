package editor

import "testing"

func TestDabbrevExpandCyclesCandidates(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		text    string
		line    int
		col     int
		presses int
		want    []string // text of the caret line after each press
		wantErr bool     // the first press fails and changes nothing
	}{
		{
			name:    "nearest earlier word first, then the next, then the prefix",
			text:    "foobar fox fo",
			col:     13,
			presses: 4,
			want:    []string{"foobar fox fox", "foobar fox foobar", "foobar fox fo", "foobar fox fox"},
		},
		{
			name:    "an uppercase prefix matches case-sensitively",
			text:    "Foobar foo Fo",
			col:     13,
			presses: 3,
			want:    []string{"Foobar foo Foobar", "Foobar foo Fo", "Foobar foo Foobar"},
		},
		{
			name:    "a lowercase prefix matches any case",
			text:    "FOOBAR foo fo",
			col:     13,
			presses: 3,
			want:    []string{"FOOBAR foo foo", "FOOBAR foo FOOBAR", "FOOBAR foo fo"},
		},
		{
			name:    "a word that is not longer than the prefix is not a candidate",
			text:    "fo fo",
			col:     5,
			wantErr: true,
		},
		{
			name:    "no earlier word matches",
			text:    "xyz fo",
			col:     6,
			wantErr: true,
		},
		{
			name:    "words after the caret are not candidates",
			text:    "fo\nfoxy",
			line:    0,
			col:     2,
			wantErr: true,
		},
		{
			name:    "no prefix before the caret",
			text:    "foo ",
			col:     4,
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			e := withDoc(tt.text)
			marksCaret(e, tt.line, tt.col)
			if tt.wantErr {
				if r := e.Do("dabbrev_expand", nil); r.Err == nil {
					t.Fatal("dabbrev_expand returned nil, want an error")
				}
				if got := e.Active().Buffer.Text(); got != tt.text {
					t.Fatalf("text changed to %q on failure", got)
				}
				return
			}
			for i := range tt.presses {
				if r := e.Do("dabbrev_expand", nil); r.Err != nil {
					t.Fatalf("press %d: %v", i+1, r.Err)
				}
				if got := e.Active().Buffer.Line(tt.line); got != tt.want[i] {
					t.Fatalf("after press %d line = %q, want %q", i+1, got, tt.want[i])
				}
			}
		})
	}
}
