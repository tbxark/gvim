package bundled_test

import (
	"slices"
	"sort"
	"strings"
	"testing"
)

// The vi_mode README tells users which ctrl keys vi owns. Every other ctrl key, in insert
// mode and in normal and visual mode, is not bound by the plugin, so the editor's own
// binding of it runs (ctrl+w selects a word or closes the tab, ctrl+o opens a file, ...).
func TestViOwnsOnlyDocumentedCtrlKeys(t *testing.T) {
	tests := []struct {
		name string
		keys string // typed to reach the mode
		want []string
	}{
		{"normal", "", []string{"ctrl+[", "ctrl+a", "ctrl+b", "ctrl+c", "ctrl+d", "ctrl+e", "ctrl+f", "ctrl+g", "ctrl+r", "ctrl+u", "ctrl+v", "ctrl+x", "ctrl+y"}},
		{"visual", "v", []string{"ctrl+[", "ctrl+a", "ctrl+b", "ctrl+c", "ctrl+d", "ctrl+e", "ctrl+f", "ctrl+g", "ctrl+r", "ctrl+u", "ctrl+v", "ctrl+x", "ctrl+y"}},
		{"insert", "i", []string{"ctrl+[", "ctrl+c"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := newVi(t, "hello world\n").keys(tt.keys)
			info, ok := v.modeInfo()
			if !ok {
				t.Fatalf("no input mode; log %v", v.h.LogText(viPlugin))
			}
			if !info.Fallthrough {
				t.Fatal("unbound keys must fall through to the editor")
			}
			var got []string
			for k := range info.Bindings {
				if strings.HasPrefix(k, "ctrl+") {
					got = append(got, k)
				}
			}
			sort.Strings(got)
			if !slices.Equal(got, tt.want) {
				t.Fatalf("ctrl bindings = %v, want %v", got, tt.want)
			}
		})
	}
}
