package browser

import (
	"slices"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestPasteExtendsTheFilter(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		typed     string
		paste     string
		wantInput string
		wantRows  []string
	}{
		{name: "into an empty filter", paste: "a.g", wantInput: "a.g", wantRows: []string{"a.go"}},
		{name: "after typed text", typed: "b", paste: ".t", wantInput: "b.t", wantRows: []string{"b.txt"}},
		{name: "line breaks and control characters are dropped", paste: "a.\r\ng\x00o", wantInput: "a. go", wantRows: nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dir, home := fixture(t)
			b := New(Options{Dir: dir, Home: home})
			if tt.typed != "" {
				send(t, b, typed(tt.typed))
			}
			if msg := send(t, b, tea.PasteMsg{Content: tt.paste}); msg != nil {
				t.Fatalf("a paste produced %v", msg)
			}
			if b.Input() != tt.wantInput {
				t.Errorf("input = %q, want %q", b.Input(), tt.wantInput)
			}
			if got := names(b); tt.wantRows != nil && !slices.Equal(got, tt.wantRows) {
				t.Errorf("rows = %v, want %v", got, tt.wantRows)
			}
		})
	}
}
