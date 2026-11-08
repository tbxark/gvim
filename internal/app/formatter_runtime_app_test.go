package app

import (
	"path/filepath"
	"testing"
)

func TestConfiguredExtensionFormatterRunsOnAnOpenedFile(t *testing.T) {
	isolateConfig(t)
	root := t.TempDir()
	file := filepath.Join(root, "main.go")
	writeTestFile(t, file, "hello\n")
	writeTestFile(t, filepath.Join(root, ".gvim", "config.json"), `{"formatters": {".go": "tr a-z A-Z"}}`)
	rt, err := buildRuntime(t.Context(), Options{Root: root, Files: []FileTarget{{Path: file}}}, false)
	must(t, err)
	t.Cleanup(rt.close)
	if r := rt.ed.Do("format_buffer", nil); r.Err != nil {
		t.Fatalf("format_buffer: %v", r.Err)
	}
	if got := rt.ed.Active().Buffer.Text(); got != "HELLO\n" {
		t.Fatalf("text = %q, want %q", got, "HELLO\n")
	}
}
