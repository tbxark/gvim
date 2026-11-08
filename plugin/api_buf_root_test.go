package plugin_test

import (
	"testing"

	"github.com/tbxark/gvim/plugin/plugintest"
	"github.com/tbxark/gvim/pluginapi"
)

// gvim.buf.open resolves a relative path against the workspace root, the base of the paths that
// gvim.workspace.files and grep return: those paths can be passed to open as they are.
func TestBufOpenResolvesRelativePathsAgainstTheRoot(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, path, want string
	}{
		{"relative", "src/main.go", "/work/src/main.go"},
		{"dot relative", "./src/main.go", "/work/src/main.go"},
		{"parent", "../other/x.go", "/other/x.go"},
		{"absolute is left alone", "/w/x.go", "/w/x.go"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			host := plugintest.NewFakeHost()
			h := cmd(t, "", `gvim.buf.open("`+tt.path+`")`, plugintest.Options{Host: host})
			settleBuffers(h, host)
			for _, c := range host.Calls() {
				if c.Method == "Open" {
					if got := c.Args[1].(pluginapi.OpenRequest).Path; got != tt.want {
						t.Fatalf("Host.Open path = %q, want %q", got, tt.want)
					}
					return
				}
			}
			t.Fatal("Host.Open was not called")
		})
	}
}
