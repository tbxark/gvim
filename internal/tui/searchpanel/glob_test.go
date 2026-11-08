package searchpanel

import "testing"

func TestGlobMatching(t *testing.T) {
	t.Parallel()
	tests := []struct {
		glob, rel string
		want      bool
	}{
		{"", "a.go", true},
		{"", "dir/a.txt", true},
		{"*.go", "a.go", true},
		{"*.go", "dir/b.go", true},
		{"*.go", "dir/a.txt", false},
		{"*.go, *.md", "readme.md", true},
		{"*.go *.md", "readme.md", true},
		{"  *.go ,  *.md  ", "a.go", true},
		{"src/**", "src/a.go", true},
		{"src/**", "src/x/y.go", true},
		{"src/**", "lib/a.go", false},
		{"src/*.go", "src/a.go", true},
		{"src/*.go", "src/x/b.go", false},
		{"**/test_*.go", "test_a.go", true},
		{"**/test_*.go", "pkg/test_a.go", true},
		{"**/test_*.go", "pkg/a_test.go", false},
		{"?oo.go", "foo.go", true},
		{"?oo.go", "fooo.go", false},
		{"*.go", ".go", true},
		{"[ab].go", "a.go", true},
		{"[ab].go", "c.go", false},
		{"[a-c].go", "b.go", true},
	}
	for _, tt := range tests {
		m, err := parseGlob(tt.glob)
		if err != nil {
			t.Fatalf("parseGlob(%q): %v", tt.glob, err)
		}
		if got := m.match(tt.rel); got != tt.want {
			t.Errorf("glob %q on %q = %v, want %v", tt.glob, tt.rel, got, tt.want)
		}
	}
}

func TestGlobInvalidPattern(t *testing.T) {
	t.Parallel()
	if _, err := parseGlob("src/["); err == nil {
		t.Fatal("an invalid glob pattern gave no error")
	}
	if m, err := parseGlob(""); err != nil || m != nil {
		t.Fatalf("an empty glob = %v, %v; want the nil matcher", m, err)
	}
}

func TestGlobFilter(t *testing.T) {
	t.Parallel()
	m, err := parseGlob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	got := m.filter([]string{"a.go", "b.txt", "sub/c.go"})
	if len(got) != 2 || got[0] != "a.go" || got[1] != "sub/c.go" {
		t.Fatalf("filter = %v", got)
	}
	if files := (*globMatcher)(nil).filter([]string{"a.txt"}); len(files) != 1 {
		t.Fatalf("the nil matcher filtered: %v", files)
	}
}
