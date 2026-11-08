package theme

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// urlThemeBody is a minimal valid URL theme: a partial theme extending the
// bundled dark one.
const urlThemeBody = `{"extends":"builtin://dark","editor":{"cursor":[1,2,3]}}`

func TestValidateFormURLThemes(t *testing.T) {
	t.Parallel()
	tests := []struct {
		ref     string
		wantErr string
	}{
		{ref: "https://example.com/themes/cool.json"},
		{ref: "http://127.0.0.1:8080/cool.json"},
		{ref: "http://localhost:8080/cool.json"},
		{ref: "dark"},
		{ref: "file:///tmp/cool.json"},
		{ref: "subdir/cool.json"},
		{ref: "https://example.com/themes#dark", wantErr: "packaged repository themes"},
		{ref: "ftp://example.com/cool.json", wantErr: "unsupported theme URL scheme"},
	}
	for _, tt := range tests {
		err := ValidateForm(tt.ref)
		if tt.wantErr == "" {
			if err != nil {
				t.Errorf("ValidateForm(%q) = %v, want nil", tt.ref, err)
			}
			continue
		}
		if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
			t.Errorf("ValidateForm(%q) = %v, want %q", tt.ref, err, tt.wantErr)
		}
	}
	if !IsURLForm("https://example.com/t.json") || !IsURLForm("http://127.0.0.1/t.json") {
		t.Fatal("IsURLForm did not recognize an http(s) URL")
	}
	if IsURLForm("dark") || IsURLForm("file:///tmp/x.json") {
		t.Fatal("IsURLForm recognized a non-URL theme value")
	}
}

func TestRegistryURLCache(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	cacheDir := URLCacheDir(dir)
	ref := "https://example.com/themes/cool.json"
	reg := NewRegistry("")
	// Without a cache dir a URL theme is a clear error, never a network wait.
	if _, err := reg.Load(ref); err == nil || !strings.Contains(err.Error(), "no cache directory") {
		t.Fatalf("Load without a cache dir = %v", err)
	}
	reg.SetURLCacheDir(cacheDir)
	// Without a cached body the load says it was not fetched (startup fetches).
	path := URLCachePath(cacheDir, ref)
	if len(filepath.Base(path)) != 69 || !strings.HasSuffix(path, ".json") {
		t.Fatalf("cache path = %q, want a sha256 .json name", path)
	}
	if _, err := reg.Load(ref); err == nil || !strings.Contains(err.Error(), "not cached yet") {
		t.Fatalf("Load before the fetch = %v", err)
	}
	// A fetched body validates through the same parser/resolver user themes use.
	th, err := reg.LoadURLBody(ref, []byte(urlThemeBody))
	if err != nil {
		t.Fatal(err)
	}
	if th.Name != "cool" {
		t.Fatalf("URL theme default name = %q, want the file name from the URL", th.Name)
	}
	// The runtime path reads the cached bytes only.
	if err := os.MkdirAll(cacheDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(urlThemeBody), 0o600); err != nil {
		t.Fatal(err)
	}
	th, err = reg.Load(ref)
	if err != nil || th.Name != "cool" {
		t.Fatalf("cached Load = %+v, %v", th, err)
	}
	if canonical, err := reg.Canonical(ref); err != nil || canonical != ref {
		t.Fatalf("Canonical(URL) = %q, %v", canonical, err)
	}
	// A broken body and a broken cache are errors with the URL named.
	if _, err := reg.LoadURLBody(ref, []byte("{not json")); err == nil {
		t.Fatal("a broken URL theme body was accepted")
	}
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := reg.Load(ref); err == nil {
		t.Fatal("a broken cached URL theme was accepted")
	}
	// A URL theme is not a user theme: it cannot be deleted or listed as a file.
	if err := reg.DeleteTheme(ref); err == nil {
		t.Fatal("DeleteTheme accepted a URL theme")
	}
	if _, err := reg.ThemePath(ref); err == nil {
		t.Fatal("ThemePath accepted a URL theme")
	}
}

func TestURLCachePathIsStableAndKeyedByURL(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	a := URLCachePath(dir, "https://example.com/a.json")
	b := URLCachePath(dir, " https://example.com/a.json ")
	c := URLCachePath(dir, "https://example.com/b.json")
	if a != b {
		t.Fatalf("the same URL (trimmed) mapped to two cache files: %q / %q", a, b)
	}
	if a == c {
		t.Fatalf("two URLs mapped to the same cache file: %q", a)
	}
}

func TestURLThemeName(t *testing.T) {
	t.Parallel()
	tests := map[string]string{
		"https://example.com/themes/nord.json": "nord",
		"https://example.com/themes/":          "example.com",
		"https://example.com":                  "example.com",
	}
	for ref, want := range tests {
		if got := URLThemeName(ref); got != want {
			t.Errorf("URLThemeName(%q) = %q, want %q", ref, got, want)
		}
	}
}
