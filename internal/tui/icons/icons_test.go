package icons

import (
	"os"
	"testing"
	"unicode/utf8"

	"github.com/charmbracelet/x/ansi"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/font/sfnt"
)

// plainEmpty are the uses that plain mode does not draw.
var plainEmpty = map[Icon]bool{Folder: true, FolderOpen: true, File: true}

// allNerd lists every Nerd Font glyph of the package, with where it is used.
func allNerd() map[string]string {
	out := map[string]string{}
	for _, i := range All() {
		out[Nerd.Glyph(i)] = GlyphName(i)
	}
	for k, f := range fileNames {
		out[f.glyph] = "file name " + k
	}
	for k, f := range fileExts {
		out[f.glyph] = "extension " + k
	}
	for k, f := range gitMarks {
		out[f.glyph] = "git mark " + string(k)
	}
	return out
}

func TestEveryUseHasAGlyphInBothModes(t *testing.T) {
	t.Parallel()
	if got := len(All()); got != int(count) {
		t.Fatalf("All lists %d uses, the table has %d", got, count)
	}
	for _, i := range All() {
		for _, m := range []Mode{Plain, Nerd} {
			g := m.Glyph(i)
			if g == "" && m == Plain && plainEmpty[i] {
				continue
			}
			if g == "" {
				t.Errorf("%v icon %d has no glyph", m, i)
			}
			if w := ansi.StringWidth(g); w != 1 {
				t.Errorf("%v icon %d is %d cells wide, want 1", m, i, w)
			}
		}
		if GlyphName(i) == "" {
			t.Errorf("icon %d has no Nerd Fonts name", i)
		}
	}
}

func TestNerdGlyphsAreOneCellInTheBasicPrivateUseArea(t *testing.T) {
	t.Parallel()
	for g, use := range allNerd() {
		r, n := utf8.DecodeRuneInString(g)
		if n != len(g) || r < 0xE000 || r > 0xF8FF {
			t.Errorf("%s = %q, want one rune in U+E000..U+F8FF", use, g)
		}
		if w := ansi.StringWidth(g); w != 1 {
			t.Errorf("%s = %q is %d cells wide, want 1", use, g, w)
		}
	}
}

// TestNerdGlyphsAreInTheFont checks every code point against a Nerd Font file. It
// runs only when GVIM_NERD_FONT names a Nerd Font Mono file, for example
// ~/Library/Fonts/JetBrainsMonoNerdFontMono-Regular.ttf.
func TestNerdGlyphsAreInTheFont(t *testing.T) {
	path := os.Getenv("GVIM_NERD_FONT")
	if path == "" {
		t.Skip("GVIM_NERD_FONT is not set")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	f, err := opentype.Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	var buf sfnt.Buffer
	for g, use := range allNerd() {
		r, _ := utf8.DecodeRuneInString(g)
		if idx, err := f.GlyphIndex(&buf, r); err != nil || idx == 0 {
			t.Errorf("%s: U+%04X is not in %s", use, r, path)
		}
	}
}

func TestPlainKeepsTheGlyphsGvimAlwaysDrew(t *testing.T) {
	t.Parallel()
	want := map[Icon]string{Close: "×", Refresh: "⟳", New: "+", DirExpanded: "▼", DirCollapsed: "▶"}
	for i, g := range want {
		if got := Plain.Glyph(i); got != g {
			t.Errorf("Plain icon %d = %q, want %q", i, got, g)
		}
	}
}

func TestParse(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		want Mode
		ok   bool
	}{
		{"plain", Plain, true},
		{"nerd", Nerd, true},
		{"", Plain, false},
		{"Nerd", Plain, false},
		{"emoji", Plain, false},
	}
	for _, tt := range tests {
		got, ok := Parse(tt.name)
		if got != tt.want || ok != tt.ok {
			t.Errorf("Parse(%q) = %v, %v, want %v, %v", tt.name, got, ok, tt.want, tt.ok)
		}
	}
}

func TestZeroModeIsPlain(t *testing.T) {
	t.Parallel()
	var m Mode
	if m != Plain || m.String() != PlainName {
		t.Fatalf("zero Mode = %v, want plain", m)
	}
}

func TestFileIcon(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name          string
		dir, expanded bool
		want          string
	}{
		{"main.go", false, false, "\ue627"},
		{"MAIN.GO", false, false, "\ue627"},
		{"/src/app/main.go", false, false, "\ue627"},
		{`C:\src\main.go`, false, false, "\ue627"},
		{"Makefile", false, false, "\ue779"},
		{"go.mod", false, false, "\ue627"},
		{"notes.unknownext", false, false, Nerd.Glyph(File)},
		{"README", false, false, Nerd.Glyph(File)},
		{".gitignore", false, false, "\ue702"},
		{"src", true, false, Nerd.Glyph(Folder)},
		{"src", true, true, Nerd.Glyph(FolderOpen)},
		{"dir.go", true, false, Nerd.Glyph(Folder)}, // a directory is never typed by its name
	}
	for _, tt := range tests {
		if got := Nerd.FileIcon(tt.name, tt.dir, tt.expanded); got != tt.want {
			t.Errorf("Nerd.FileIcon(%q, %v, %v) = %q, want %q", tt.name, tt.dir, tt.expanded, got, tt.want)
		}
		if got := Plain.FileIcon(tt.name, tt.dir, tt.expanded); got != "" {
			t.Errorf("Plain.FileIcon(%q) = %q, want none", tt.name, got)
		}
	}
}

func TestGitMark(t *testing.T) {
	t.Parallel()
	for _, r := range "AMDRUC" {
		if got := Plain.GitMark(r); got != string(r) {
			t.Errorf("Plain.GitMark(%q) = %q", r, got)
		}
		if got := Nerd.GitMark(r); got == string(r) || ansi.StringWidth(got) != 1 {
			t.Errorf("Nerd.GitMark(%q) = %q, want a one-cell glyph", r, got)
		}
	}
	if got := Nerd.GitMark('?'); got != "?" {
		t.Errorf("Nerd.GitMark('?') = %q, want the letter", got)
	}
}

func TestIsNerdFontName(t *testing.T) {
	t.Parallel()
	tests := map[string]bool{
		"JetBrainsMono Nerd Font Mono": true,
		"JetBrainsMono Nerd Font":      true,
		"FiraCode NFM":                 true,
		"Hack NFP":                     true,
		"Meslo LG S NF":                true,
		"MesloLGS-NerdFont-Regular":    true,
		"  jetbrainsmono nerd font  ":  true,
		"Symbols Nerd Font Mono":       true,
		"JetBrains Mono":               false,
		"Menlo":                        false,
		"NF":                           false,
		"":                             false,
		"Inconsolata Nfinity":          false,
	}
	for name, want := range tests {
		if got := IsNerdFontName(name); got != want {
			t.Errorf("IsNerdFontName(%q) = %v, want %v", name, got, want)
		}
	}
}

func TestResolve(t *testing.T) {
	t.Parallel()
	env := func(kv ...string) func(string) string {
		m := map[string]string{}
		for i := 0; i < len(kv); i += 2 {
			m[kv[i]] = kv[i+1]
		}
		return func(k string) string { return m[k] }
	}
	tests := []struct {
		name          string
		setting, font string
		env           func(string) string
		want          Mode
	}{
		{"plain wins over everything", "plain", "Hack Nerd Font", env("TERM", "xterm-kitty"), Plain},
		{"nerd wins over everything", "nerd", "Menlo", env(), Nerd},
		{"auto, nothing known", "auto", "", env("TERM", "xterm-256color"), Plain},
		{"empty setting is auto", "", "", env("KITTY_WINDOW_ID", "3"), Nerd},
		{"unknown setting is auto", "bogus", "", env("TERM", "xterm-256color"), Plain},
		{"kitty by TERM", "auto", "", env("TERM", "xterm-kitty"), Nerd},
		{"kitty by window id", "auto", "", env("KITTY_WINDOW_ID", "1"), Nerd},
		{"WezTerm", "auto", "", env("TERM_PROGRAM", "WezTerm"), Nerd},
		{"Ghostty", "auto", "", env("TERM_PROGRAM", "ghostty"), Nerd},
		{"Apple Terminal", "auto", "", env("TERM_PROGRAM", "Apple_Terminal"), Plain},
		{"Nerd Font declared", "auto", "JetBrainsMono Nerd Font Mono", env(), Nerd},
		{"plain font declared beats the terminal", "auto", "Menlo", env("TERM", "xterm-kitty"), Plain},
		{"Nerd Font declared in any terminal", "auto", "Hack NFM", env("TERM_PROGRAM", "Apple_Terminal"), Nerd},
	}
	for _, tt := range tests {
		got := Resolve(tt.setting, tt.font, tt.env)
		if got.Mode != tt.want {
			t.Errorf("%s: mode %v, want %v (%s)", tt.name, got.Mode, tt.want, got.Why)
		}
		if got.Why == "" {
			t.Errorf("%s: no reason", tt.name)
		}
	}
}
