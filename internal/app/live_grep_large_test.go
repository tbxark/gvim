package app

import (
	"strings"
	"testing"

	"github.com/tbxark/gvim/editor"
	"github.com/tbxark/gvim/internal/pluginhost"
)

func TestLiveGrepSkipsTheFilesThatOpenInLargeFileMode(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	// Both files match, and the big one matches in its first MiB too, so only the
	// size can be the reason it is left out.
	writeTree(t, dir, map[string]string{
		"small.txt": "needle\n",
		"big.txt":   "needle\n" + strings.Repeat("x", 2000) + "\n",
		"edge.txt":  "needle" + strings.Repeat("y", 1000-len("needle")), // exactly the threshold
	})
	files := []string{"small.txt", "big.txt", "edge.txt"}
	tests := []struct {
		name string
		opts editor.Options
		want []string
	}{
		{"large mode on at 1000 bytes", editor.Options{LargeFileMode: true, LargeFileThresholdBytes: 1000}, []string{"small.txt"}},
		{"large mode off", editor.Options{LargeFileThresholdBytes: 1000}, []string{"small.txt", "big.txt", "edge.txt"}},
		{"threshold zero is off", editor.Options{LargeFileMode: true}, []string{"small.txt", "big.txt", "edge.txt"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hits, err := grepFor(tt.opts).Search(dir, files, "needle", 100)
			if err != nil {
				t.Fatal(err)
			}
			var got []string
			for _, h := range hits {
				got = append(got, h.Rel)
			}
			if strings.Join(got, ",") != strings.Join(tt.want, ",") {
				t.Errorf("files with hits = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestReadSearchableSkipFrom(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{"f.txt": strings.Repeat("a", 100)})
	path := dir + "/f.txt"
	tests := []struct {
		skipFrom int64
		want     bool
	}{
		{0, true},
		{101, true},
		{100, false},
		{1, false},
	}
	for _, tt := range tests {
		if _, ok := pluginhost.ReadSearchable(path, tt.skipFrom); ok != tt.want {
			t.Errorf("pluginhost.ReadSearchable(100-byte file, skipFrom %d) = %v, want %v", tt.skipFrom, ok, tt.want)
		}
	}
}
