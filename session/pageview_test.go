package session

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFileStatePageViewMigrates(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	// A session written before the page-view fields existed, with a field a newer
	// writer might add: it must load with the defaults and ignore the unknown key.
	old := `{"version":1,"root":"/w","active":0,"files":[
		{"path":"/w/a.md","line":2,"col":3,"top":1,"futureView":"x"}
	]}`
	if err := os.WriteFile(filepath.Join(dir, "session.json"), []byte(old), 0o600); err != nil {
		t.Fatal(err)
	}
	s, ok, err := Load(dir)
	if err != nil || !ok {
		t.Fatalf("Load = %v, %v", ok, err)
	}
	if len(s.Files) != 1 {
		t.Fatalf("files = %+v", s.Files)
	}
	if s.Files[0].PageView != nil || s.Files[0].PageWidth != 0 {
		t.Fatalf("page fields = %v/%d, want the defaults", s.Files[0].PageView, s.Files[0].PageWidth)
	}
	// A save of a file without the page view omits both fields.
	if err := Save(dir, s); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "session.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"pageView", "pageWidth"} {
		if bytesContains(data, field) {
			t.Fatalf("saved session has %q: %s", field, data)
		}
	}
}

// bytesContains reports whether b holds the ASCII s.
func bytesContains(b []byte, s string) bool {
	for i := 0; i+len(s) <= len(b); i++ {
		if string(b[i:i+len(s)]) == s {
			return true
		}
	}
	return false
}
