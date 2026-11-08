package plugin_test

import (
	"testing"

	"github.com/tbxark/gvim/internal/pluginapi"
)

// gvim.buf.open takes a relative path from the workspace root, the way workspace.grep and
// workspace.files report their paths; an absolute path stays as it is.
func TestBufOpenResolvesRelativePathsAgainstTheWorkspaceRoot(t *testing.T) {
	tests := []struct{ name, path, want string }{
		{"relative", "src/main.go", "/work/src/main.go"},
		{"dot slash", "./a.txt", "/work/a.txt"},
		{"parent", "../other/b.txt", "/other/b.txt"},
		{"absolute", "/elsewhere/c.txt", "/elsewhere/c.txt"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := cmd(t, "", `gvim.buf.open("`+tt.path+`")`)
			var got string
			for _, c := range h.Host.Calls() {
				if c.Method == "Open" {
					got = c.Args[1].(pluginapi.OpenRequest).Path
				}
			}
			if got != tt.want {
				t.Errorf("the host was asked to open %q, want %q", got, tt.want)
			}
		})
	}
}
