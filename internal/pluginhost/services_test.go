package pluginhost

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/tbxark/gvim/internal/apptest"
	"github.com/tbxark/gvim/pluginapi"
)

// TestPluginServicesGrepConvertsColumns checks the conversion of AMENDMENTS C12/C24: a hit
// has a 0-based line and a 0-based BYTE column into its text, where live grep reports a
// 1-based line and rune column.
func TestPluginServicesGrepConvertsColumns(t *testing.T) {
	root := t.TempDir()
	write := func(rel, text string) {
		t.Helper()
		path := filepath.Join(root, filepath.FromSlash(rel))
		apptest.Must(t, os.MkdirAll(filepath.Dir(path), 0o755))
		apptest.Must(t, os.WriteFile(path, []byte(text), 0o644))
	}
	write("a.txt", "first line\nhéllo wörld needle here\nlast needle needle\n")
	write("sub/b.txt", "no match\nNEEDLE upper\n")
	write("bin.dat", "needle\x00binary\n")
	svc := pluginServices{}

	tests := []struct {
		name string
		req  pluginapi.GrepRequest
		want []pluginapi.GrepHit
	}{
		{
			name: "byte columns after multibyte characters, one hit per line",
			req:  pluginapi.GrepRequest{Root: root, Query: "needle", CaseSensitive: true},
			want: []pluginapi.GrepHit{
				{Path: "a.txt", Line: 1, Col: strings.Index("héllo wörld needle here", "needle"), Text: "héllo wörld needle here"},
				{Path: "a.txt", Line: 2, Col: 5, Text: "last needle needle"},
			},
		},
		{
			name: "case insensitive by default",
			req:  pluginapi.GrepRequest{Root: root, Query: "needle"},
			want: []pluginapi.GrepHit{
				{Path: "a.txt", Line: 1, Col: strings.Index("héllo wörld needle here", "needle"), Text: "héllo wörld needle here"},
				{Path: "a.txt", Line: 2, Col: 5, Text: "last needle needle"},
				{Path: "sub/b.txt", Line: 1, Col: 0, Text: "NEEDLE upper"},
			},
		},
		{
			name: "regex",
			req:  pluginapi.GrepRequest{Root: root, Query: `w.rld`, Regex: true},
			want: []pluginapi.GrepHit{{Path: "a.txt", Line: 1, Col: strings.Index("héllo wörld needle here", "wörld"), Text: "héllo wörld needle here"}},
		},
		{
			name: "max",
			req:  pluginapi.GrepRequest{Root: root, Query: "needle", Max: 1},
			want: []pluginapi.GrepHit{{Path: "a.txt", Line: 1, Col: strings.Index("héllo wörld needle here", "needle"), Text: "héllo wörld needle here"}},
		},
		{name: "no match", req: pluginapi.GrepRequest{Root: root, Query: "absent"}},
		{name: "empty query", req: pluginapi.GrepRequest{Root: root}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := svc.Grep(t.Context(), tt.req)
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(got, tt.want) {
				t.Fatalf("hits = %+v\nwant   %+v", got, tt.want)
			}
		})
	}
	// The column is a byte offset into Text.
	hits, _ := svc.Grep(t.Context(), pluginapi.GrepRequest{Root: root, Query: "needle", CaseSensitive: true})
	for _, h := range hits {
		if !strings.HasPrefix(h.Text[h.Col:], "needle") {
			t.Errorf("Text[%d:] = %q, want it to start with the match", h.Col, h.Text[h.Col:])
		}
	}

	t.Run("a bad regex is invalid", func(t *testing.T) {
		_, err := svc.Grep(t.Context(), pluginapi.GrepRequest{Root: root, Query: "(", Regex: true})
		if pluginapi.CodeOf(err) != pluginapi.CodeInvalid {
			t.Fatalf("error = %v, want invalid", err)
		}
	})
	t.Run("cancelled", func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		_, err := svc.Grep(ctx, pluginapi.GrepRequest{Root: root, Query: "needle"})
		if pluginapi.CodeOf(err) != pluginapi.CodeCancelled {
			t.Fatalf("error = %v, want cancelled", err)
		}
	})
}

// TestGrepConvertsColumns compares the service with live grep on the same file: live grep
// reports a 1-based line and a 1-based RUNE column, the service a 0-based line and a
// 0-based BYTE column.

func TestPluginServicesText(t *testing.T) {
	svc := pluginServices{}
	if got := svc.Width("abc"); got != 3 {
		t.Errorf("Width(abc) = %d", got)
	}
	if got := svc.Width("日本"); got != 4 {
		t.Errorf("Width of two wide characters = %d, want 4", got)
	}
	if got := svc.Width("\x1b[31mred\x1b[0m"); got != 3 {
		t.Errorf("Width ignores escape sequences, got %d", got)
	}
	tests := []struct {
		s        string
		w        int
		ellipsis string
		want     string
	}{
		{"hello world", 20, "…", "hello world"},
		{"hello world", 8, "…", "hello w…"},
		{"hello world", 5, "", "hello"},
		{"日本語です", 5, "", "日本"},
	}
	for _, tt := range tests {
		if got := svc.Truncate(tt.s, tt.w, tt.ellipsis); got != tt.want {
			t.Errorf("Truncate(%q, %d, %q) = %q, want %q", tt.s, tt.w, tt.ellipsis, got, tt.want)
		}
	}
}

func TestPluginServicesDiff(t *testing.T) {
	svc := pluginServices{}
	a := []string{"a", "b", "c", "d"}
	b := []string{"a", "B", "c", "d", "e"}
	want := []pluginapi.Hunk{
		{Kind: "change", OldStart: 1, OldCount: 1, NewStart: 1, NewCount: 1},
		{Kind: "insert", OldStart: 4, OldCount: 0, NewStart: 4, NewCount: 1},
	}
	if got := svc.Diff(a, b); !slices.Equal(got, want) {
		t.Errorf("Diff = %+v, want %+v", got, want)
	}
	if got := svc.Diff([]string{"x", "y"}, []string{"y"}); len(got) != 1 || got[0].Kind != "delete" || got[0].OldCount != 1 {
		t.Errorf("Diff of a deletion = %+v", got)
	}
	// DiffText splits on lines, and IgnoreEOL treats CRLF and CR as LF.
	if got := svc.DiffText("a\nb\n", "a\nb\n", pluginapi.DiffOptions{}); len(got) != 0 {
		t.Errorf("DiffText of equal texts = %+v", got)
	}
	if got := svc.DiffText("a\r\nb\r\n", "a\nb\n", pluginapi.DiffOptions{}); len(got) == 0 {
		t.Error("DiffText without IgnoreEOL ignored the line ending")
	}
	if got := svc.DiffText("a\r\nb\rc\n", "a\nb\nc\n", pluginapi.DiffOptions{IgnoreEOL: true}); len(got) != 0 {
		t.Errorf("DiffText with IgnoreEOL = %+v, want no hunks", got)
	}
}

func TestPluginServicesFuzzy(t *testing.T) {
	svc := pluginServices{}
	ctx := context.Background()
	items := []string{"internal/ui/model.go", "README.md", "main.go", "internal/model/main.go"}
	got := svc.Fuzzy(ctx, "main", items, 0)
	if len(got) < 2 || got[0].Index != 2 {
		t.Fatalf("Fuzzy(main) = %+v, want main.go first", got)
	}
	for _, m := range got {
		if len(m.Indexes) != 4 {
			t.Errorf("match %+v has %d indexes, want 4", m, len(m.Indexes))
		}
	}
	if got := svc.Fuzzy(ctx, "main", items, 1); len(got) != 1 {
		t.Errorf("limit 1 gave %d matches", len(got))
	}
	if got := svc.Fuzzy(ctx, "zzz", items, 0); len(got) != 0 {
		t.Errorf("Fuzzy(zzz) = %+v", got)
	}
	// Equal scores keep the order of the items.
	same := svc.Fuzzy(ctx, "a", []string{"a", "a", "a"}, 0)
	if len(same) != 3 || same[0].Index != 0 || same[1].Index != 1 || same[2].Index != 2 {
		t.Errorf("Fuzzy with equal scores = %+v, want item order", same)
	}
}

func TestPluginServicesFuzzyStopsWhenCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	items := make([]string, 1000)
	for i := range items {
		items[i] = "main.go"
	}
	if got := (pluginServices{}).Fuzzy(ctx, "main", items, 0); len(got) != 0 {
		t.Fatalf("a cancelled Fuzzy ranked %d items", len(got))
	}
}

func TestPluginServicesDecode(t *testing.T) {
	svc := pluginServices{}
	tests := []struct {
		enc  string
		data []byte
		want string
		code pluginapi.Code
	}{
		{"", []byte("héllo"), "héllo", ""},
		{"utf8", []byte("héllo"), "héllo", ""},
		{"UTF-8", []byte("héllo"), "héllo", ""},
		{"windows-1252", []byte{'h', 0xe9, 'l'}, "hél", ""},
		// The labels of Document.Encoding, which is what b:info().encoding holds.
		{"ASCII", []byte("plain"), "plain", ""},
		{"Windows-1252", []byte{'h', 0xe9, 'l'}, "hél", ""},
		{"Latin-1", []byte{'h', 0xe9, 'l'}, "hél", ""},
		{"UTF-16 LE", []byte{'h', 0, 'i', 0}, "hi", ""},
		{"UTF-8 BOM", []byte{0xef, 0xbb, 0xbf, 'h', 'i'}, "hi", ""},
		{"Shift-JIS", []byte{0x82, 0xa0}, "あ", ""},
		{"utf-16le", []byte{'h', 0, 'i', 0}, "hi", ""},
		{"no-such-encoding", []byte("x"), "", pluginapi.CodeInvalid},
		{"utf-8", []byte{0xff, 0xfe, 'x'}, "", pluginapi.CodeInvalid},
	}
	for _, tt := range tests {
		got, err := svc.Decode(tt.enc, tt.data)
		if codeOf(err) != tt.code || got != tt.want {
			t.Errorf("Decode(%q, %v) = %q, %v, want %q, code %q", tt.enc, tt.data, got, err, tt.want, tt.code)
		}
	}
}

func TestPluginServicesGit(t *testing.T) {
	svc := pluginServices{}
	repo := t.TempDir()
	apptest.Must(t, os.MkdirAll(filepath.Join(repo, ".git"), 0o755))
	apptest.Must(t, os.WriteFile(filepath.Join(repo, ".git", "HEAD"), []byte("ref: refs/heads/trunk\n"), 0o644))
	apptest.Must(t, os.MkdirAll(filepath.Join(repo, "sub", "deep"), 0o755))
	// FindRoot resolves symlinks of the temp dir the same way on both calls.
	if root, ok := svc.GitRoot(filepath.Join(repo, "sub", "deep")); !ok || root != repo {
		t.Errorf("GitRoot = %q, %v, want %q", root, ok, repo)
	}
	if root, ok := svc.GitRoot(t.TempDir()); ok {
		t.Errorf("GitRoot outside a repository = %q, true", root)
	}
	if _, err := svc.GitBranch(t.TempDir()); codeOf(err) != pluginapi.CodeNotFound {
		t.Errorf("GitBranch outside a repository = %v, want not_found", err)
	}
}

// A hit carries a window of its line, not the line: a minified file of 1 MiB with one
// line must not put 1 MiB into every hit.
func TestGrepHitTextIsAWindowAroundTheMatch(t *testing.T) {
	root := t.TempDir()
	long := strings.Repeat("é", 100_000) + "needle" + strings.Repeat("x", 100_000)
	apptest.Must(t, os.WriteFile(filepath.Join(root, "min.js"), []byte(long+"\nshort needle\n"), 0o644))
	got, err := pluginServices{}.Grep(t.Context(), pluginapi.GrepRequest{Root: root, Query: "needle"})
	apptest.Must(t, err)
	if len(got) != 2 {
		t.Fatalf("hits = %d, want 2", len(got))
	}
	for _, h := range got {
		if len(h.Text) > grepHitTextMax+utf8.UTFMax*2 {
			t.Errorf("hit %d:%d text is %d bytes, want at most about %d", h.Line, h.Col, len(h.Text), grepHitTextMax)
		}
		if !utf8.ValidString(h.Text) {
			t.Errorf("hit %d: the window cuts a character: %q", h.Line, h.Text)
		}
		if h.Col < 0 || !strings.HasPrefix(h.Text[h.Col:], "needle") {
			t.Errorf("hit %d: Col %d does not point at the match in %q", h.Line, h.Col, h.Text)
		}
	}
	if got[1].Text != "short needle" || got[1].Col != len("short ") {
		t.Errorf("a short line is kept whole: %+v", got[1])
	}
}

// A file larger than the 1 MiB that live grep reads is skipped, not searched in part: a
// miss must not read as "the plugin searched the whole file".
func TestGrepSkipsFilesLargerThanItReads(t *testing.T) {
	root := t.TempDir()
	big := "needle at the start\n" + strings.Repeat("filler line\n", 200_000) + "needle at the end\n"
	apptest.Must(t, os.WriteFile(filepath.Join(root, "big.txt"), []byte(big), 0o644))
	apptest.Must(t, os.WriteFile(filepath.Join(root, "ok.txt"), []byte("needle\n"), 0o644))
	got, err := pluginServices{}.Grep(t.Context(), pluginapi.GrepRequest{Root: root, Query: "needle"})
	apptest.Must(t, err)
	if len(got) != 1 || got[0].Path != "ok.txt" {
		t.Fatalf("hits = %+v, want only ok.txt", got)
	}
	// A file of exactly the cap is read whole.
	edge := strings.Repeat("a", GrepMaxFileBytes-len("\nneedle")) + "\nneedle"
	apptest.Must(t, os.WriteFile(filepath.Join(root, "edge.txt"), []byte(edge), 0o644))
	got, err = pluginServices{}.Grep(t.Context(), pluginapi.GrepRequest{Root: root, Query: "needle"})
	apptest.Must(t, err)
	if len(got) != 2 {
		t.Fatalf("hits = %+v, want edge.txt and ok.txt", got)
	}
}

// TestGrepScopeIsTheFileIndex backs the docs of workspace.grep: it searches what the file
// index lists. Inside a git work tree that is git's list (tracked and untracked files that
// are not ignored, hidden directories and node_modules included); elsewhere it is a walk
// that honours .gitignore and skips hidden entries and node_modules.
func TestGrepScopeIsTheFileIndex(t *testing.T) {
	fill := func(t *testing.T, root string) {
		t.Helper()
		for rel, text := range map[string]string{
			".gitignore":           "build/\n",
			"src/a.txt":            "needle\n",
			"build/out.txt":        "needle\n",
			".hidden/h.txt":        "needle\n",
			"node_modules/m/m.txt": "needle\n",
		} {
			path := filepath.Join(root, filepath.FromSlash(rel))
			apptest.Must(t, os.MkdirAll(filepath.Dir(path), 0o755))
			apptest.Must(t, os.WriteFile(path, []byte(text), 0o644))
		}
	}
	paths := func(t *testing.T, root string) []string {
		t.Helper()
		hits, err := pluginServices{}.Grep(t.Context(), pluginapi.GrepRequest{Root: root, Query: "needle"})
		apptest.Must(t, err)
		var out []string
		for _, h := range hits {
			out = append(out, h.Path)
		}
		slices.Sort(out)
		return out
	}
	t.Run("walk", func(t *testing.T) {
		t.Setenv("PATH", "") // no git binary: the finder walks the directory
		root := t.TempDir()
		fill(t, root)
		if got, want := paths(t, root), []string{"src/a.txt"}; !slices.Equal(got, want) {
			t.Fatalf("paths = %v, want %v", got, want)
		}
	})
	t.Run("git work tree", func(t *testing.T) {
		if _, err := exec.LookPath("git"); err != nil {
			t.Skip("no git binary")
		}
		root := t.TempDir()
		fill(t, root)
		gitIn(t, root, "init")
		if got, want := paths(t, root), []string{".hidden/h.txt", "node_modules/m/m.txt", "src/a.txt"}; !slices.Equal(got, want) {
			t.Fatalf("paths = %v, want %v", got, want)
		}
	})
}

// gitIn runs git in dir with the arguments, with the environment of a test repository.
func gitIn(t *testing.T, dir string, args ...string) {
	t.Helper()
	full := append([]string{"-c", "user.name=t", "-c", "user.email=t@example.com", "-c", "commit.gpgsign=false", "-c", "init.defaultBranch=main"}, args...)
	cmd := exec.Command("git", full...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL="+os.DevNull)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}
