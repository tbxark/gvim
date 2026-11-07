package bundled

import (
	"io/fs"
	"testing"
	"testing/fstest"
)

func TestEmbedHoldsPlaceholders(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"plugins/README.md", "lib/README.md"} {
		data, err := fs.ReadFile(FS, name)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		if len(data) == 0 {
			t.Errorf("%s is empty", name)
		}
	}
}

// Discovery only enters directories, so a stray file at the top of plugins/ or lib/
// must not look like a plugin.
func TestEmbedPluginsHoldNoPluginDirectoryYet(t *testing.T) {
	t.Parallel()
	entries, err := fs.ReadDir(FS, "plugins")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.IsDir() {
			if _, err := fs.Stat(FS, "plugins/"+e.Name()+"/plugin.json"); err != nil {
				t.Errorf("directory plugins/%s has no plugin.json: %v", e.Name(), err)
			}
		}
	}
}

func TestLib(t *testing.T) {
	t.Parallel()
	src, ok := Lib("README")
	if ok || src != "" {
		t.Fatalf("Lib(README) = %q, %v; only lib/<name>.lua files are modules", src, ok)
	}
	for _, name := range []string{"", "git/../x", "../plugins/README", "a/b", `a\b`, ".", "..", "x.lua"} {
		if src, ok := Lib(name); ok || src != "" {
			t.Errorf("Lib(%q) = %q, %v, want not found", name, src, ok)
		}
	}
}

func TestLibReadsLuaModules(t *testing.T) {
	t.Parallel()
	fsys := fstest.MapFS{
		"lib/git.lua":            {Data: []byte("return {}")},
		"lib/README.md":          {Data: []byte("x")},
		"plugins/other/init.lua": {Data: []byte("y")},
	}
	tests := []struct {
		name, want string
		ok         bool
	}{
		{"git", "return {}", true},
		{"README", "", false},
		{"missing", "", false},
		{"other/init", "", false},
	}
	for _, tt := range tests {
		got, ok := lib(fsys, tt.name)
		if got != tt.want || ok != tt.ok {
			t.Errorf("lib(%q) = %q, %v, want %q, %v", tt.name, got, ok, tt.want, tt.ok)
		}
	}
}
