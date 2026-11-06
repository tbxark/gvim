package app

import (
	"path/filepath"
	"testing"
)

// TestBuildModelPassesTheConfigTabSizeToTheUI checks that the UI gets the tab size of
// the loaded config, so the pane draws tabs at that width. Without a tab_size key the
// config default is used.
func TestBuildModelPassesTheConfigTabSizeToTheUI(t *testing.T) {
	tests := []struct {
		name string
		body string
		want int
	}{
		{name: "tab_size from the config", body: `{"tab_size": 2}`, want: 2},
		{name: "default when the config omits it", body: `{}`, want: 4},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := projectRoot(t, "")
			alt := filepath.Join(t.TempDir(), "alt.json")
			writeTestFile(t, alt, tt.body)
			opts.Config = alt
			got, err := buildCaptured(t, opts)
			if err != nil {
				t.Fatalf("buildModel() error = %v", err)
			}
			if got.deps.TabSize != tt.want {
				t.Fatalf("Deps.TabSize = %d, want %d", got.deps.TabSize, tt.want)
			}
		})
	}
}
