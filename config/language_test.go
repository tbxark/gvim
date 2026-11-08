package config

import (
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
)

func TestLanguageID(t *testing.T) {
	tests := []struct{ name, want string }{
		{"Go", "go"},
		{"Markdown", "markdown"},
		{"C++", "cpp"},
		{"C#", "csharp"},
		{"Docker", "dockerfile"},
		{"Go Mod", "gomod"},
		{"plaintext", "text"},
		{"fallback", "text"},
		{"  Makefile ", "makefile"},
		{"", ""},
	}
	for _, tt := range tests {
		if got := LanguageID(tt.name); got != tt.want {
			t.Errorf("LanguageID(%q) = %q, want %q", tt.name, got, tt.want)
		}
	}
}

func TestEditorForBuiltinLanguages(t *testing.T) {
	tests := []struct {
		name     string
		language string
		path     string
		check    func(EditorSettings) string // returns a problem, or ""
	}{
		{"go uses tabs of 8 and hides tab marks", "Go", "", func(s EditorSettings) string {
			if !s.UseTabs || s.TabSize != 8 || s.Whitespace.TabsLeading || s.Whitespace.TabsInner || s.Whitespace.TabsTrailing {
				return "go: want tabs, size 8, no tab marks"
			}
			return ""
		}},
		{"go found by path alone", "", "/src/main.go", func(s EditorSettings) string {
			if s.Language != "go" || !s.UseTabs {
				return "path main.go should resolve to go"
			}
			return ""
		}},
		{"makefile by file name", "", "/p/Makefile", func(s EditorSettings) string {
			if s.Language != "makefile" || !s.UseTabs || s.TabSize != 8 || s.AutoIndent {
				return "Makefile: want tabs, size 8, no auto indent"
			}
			return ""
		}},
		{"markdown suppresses pairs", "Markdown", "", func(s EditorSettings) string {
			if s.AutoClose || s.AutoSurround || s.AutoIndent {
				return "markdown: want auto_close, auto_surround and auto_indent off"
			}
			return ""
		}},
		{"markdown by extension, case-insensitive", "", "notes.MD", func(s EditorSettings) string {
			if s.Language != "markdown" || s.AutoClose {
				return "notes.MD should resolve to markdown"
			}
			return ""
		}},
		{"go.mod file name beats the mod extension", "", "go.mod", func(s EditorSettings) string {
			if s.Language != "gomod" || !s.UseTabs || s.TabSize != 4 {
				return "go.mod: want gomod with tabs and the global size"
			}
			return ""
		}},
		{"suffix file name pattern", "", "foo.pc.in", func(s EditorSettings) string {
			if s.Language != "pkg-config" {
				return "foo.pc.in should match *.pc.in"
			}
			return ""
		}},
		{"unknown language gets the globals", "Rust", "main.rs", func(s EditorSettings) string {
			if s.Language != "rust" || s.UseTabs || s.TabSize != 4 || !s.AutoClose || !s.AutoIndent {
				return "rust: want the global defaults"
			}
			return ""
		}},
		{"plain text has no indentation guides", "Plaintext", "", func(s EditorSettings) string {
			if s.Language != "text" || s.IndentationGuide {
				return "text: want indentation guides suppressed"
			}
			return ""
		}},
		{"code has indentation guides allowed", "Python", "", func(s EditorSettings) string {
			if !s.IndentationGuide || s.IndentationGuideMode != IndentationGuideNone {
				return "python: want guides allowed, mode none"
			}
			return ""
		}},
	}
	cfg := Defaults()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if msg := tt.check(cfg.EditorFor(tt.language, tt.path)); msg != "" {
				t.Error(msg)
			}
		})
	}
}

func TestEditorMatchesGlobalsWithoutLanguage(t *testing.T) {
	cfg := Defaults()
	cfg.TabSize = 2
	cfg.InsertSpaces = false
	cfg.WrapColumn = 90
	got := cfg.Editor("")
	want := EditorSettings{
		TabSize: 2, UseTabs: true, AutoIndent: true, AutoClose: true, AutoSurround: true,
		VirtualSpace: VirtualSpaceOff, LineWrap: true, WrapColumn: 90, PageWidth: 80,
		Whitespace: WhitespaceSettings{
			TabsLeading: true, TabsInner: true, TabsTrailing: true, InSelection: true,
		},
		IndentationGuide: true, IndentationGuideMode: IndentationGuideNone,
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Editor(\"\") = %+v\nwant %+v", got, want)
	}
}

func TestLanguagesSectionParsing(t *testing.T) {
	body := `{
  "languages": {
    "Rust": {
      "tab_size": 2, "use_tabs": false, "line_wrap": false, "wrap_column": 100,
      "page_view": true, "page_width": 72, "auto_indent": false, "auto_close": false,
      "auto_surround": false, "comment_prefix": "//", "word_characters": "-",
      "indentation_guide": true, "show_whitespace_tabs": false,
      "format_on_save": true,
      "formatter": {"command": "rustfmt", "args": ["--edition", "2021"]},
      "on_save": [{"command": "cargo", "args": ["check"]}],
      "indent": {"increase_indent_pattern": "\\{$", "self_close_pattern": null},
      "extensions": ["rs"], "filenames": ["Cargo.lock"], "grammar": "rust",
      "textmate_grammar": null, "highlighter": "auto"
    }
  }
}`
	cfg, diags := loadBody(t, body)
	if len(diags) != 0 {
		t.Fatalf("diagnostics: %v", diags)
	}
	s := cfg.Editor("Rust")
	if s.Language != "rust" || s.TabSize != 2 || s.UseTabs || s.LineWrap || s.WrapColumn != 100 ||
		!s.PageView || s.PageWidth != 72 || s.AutoIndent || s.AutoClose || s.AutoSurround ||
		s.CommentPrefix != "//" || s.WordCharacters != "-" || !s.IndentationGuide ||
		s.Whitespace.TabsInner || !s.FormatOnSave {
		t.Errorf("rust settings = %+v", s)
	}
	if s.Formatter == nil || s.Formatter.Command != "rustfmt" || !s.Formatter.Stdin || s.Formatter.TimeoutMS != DefaultFormatterTimeoutMS ||
		!slices.Equal(s.Formatter.Args, []string{"--edition", "2021"}) {
		t.Errorf("formatter = %+v", s.Formatter)
	}
	if len(s.OnSave) != 1 || s.OnSave[0].Command != "cargo" || !s.OnSave[0].Enabled || s.OnSave[0].TimeoutMS != DefaultOnSaveTimeoutMS {
		t.Errorf("on_save = %+v", s.OnSave)
	}
	if s.Indent == nil || s.Indent.IncreaseIndentPattern != `\{$` || s.Indent.SelfClosePattern != "" {
		t.Errorf("indent = %+v", s.Indent)
	}
	// The user entry names its own file, so a path finds it with no language name.
	if got := cfg.EditorFor("", "x/Cargo.lock").Language; got != "rust" {
		t.Errorf("Cargo.lock resolved to %q, want rust", got)
	}
	if got := cfg.EditorFor("", "x/lib.rs").Language; got != "rust" {
		t.Errorf("lib.rs resolved to %q, want rust", got)
	}
}

func TestLanguageOverrideResolution(t *testing.T) {
	tests := []struct {
		name  string
		body  string
		lang  string
		check func(EditorSettings) bool
	}{
		{"language beats global", `{"editor": {"tab_size": 2}, "languages": {"python": {"tab_size": 4}}}`, "Python",
			func(s EditorSettings) bool { return s.TabSize == 4 }},
		{"global when the language sets none", `{"editor": {"tab_size": 2}, "languages": {"python": {"line_wrap": false}}}`, "Python",
			func(s EditorSettings) bool { return s.TabSize == 2 && !s.LineWrap }},
		{"null inherits", `{"editor": {"tab_size": 2}, "languages": {"python": {"tab_size": null}}}`, "Python",
			func(s EditorSettings) bool { return s.TabSize == 2 }},
		{"zero tab_size inherits", `{"editor": {"tab_size": 2}, "languages": {"python": {"tab_size": 0}}}`, "Python",
			func(s EditorSettings) bool { return s.TabSize == 2 }},
		{"zero wrap_column inherits", `{"editor": {"wrap_column": 90}, "languages": {"python": {"wrap_column": 0}}}`, "Python",
			func(s EditorSettings) bool { return s.WrapColumn == 90 }},
		{"user overrides builtin go", `{"languages": {"go": {"use_tabs": false, "tab_size": 4}}}`, "Go",
			func(s EditorSettings) bool { return !s.UseTabs && s.TabSize == 4 && !s.Whitespace.TabsInner }},
		{"user turns markdown auto_close back on", `{"languages": {"markdown": {"auto_close": true}}}`, "Markdown",
			func(s EditorSettings) bool { return s.AutoClose && !s.AutoSurround }},
		{"language cannot enable auto_close when globally off", `{"editor": {"auto_close": false}, "languages": {"python": {"auto_close": true}}}`, "Python",
			func(s EditorSettings) bool { return !s.AutoClose }},
		{"language can disable auto_surround", `{"languages": {"python": {"auto_surround": false}}}`, "Python",
			func(s EditorSettings) bool { return !s.AutoSurround && s.AutoClose }},
		{"auto_indent follows the global when the entry is silent", `{"editor": {"auto_indent": false}, "languages": {"python": {"tab_size": 2}}}`, "Python",
			func(s EditorSettings) bool { return !s.AutoIndent }},
		{"entry of another language is not applied", `{"languages": {"python": {"tab_size": 2}}}`, "Go",
			func(s EditorSettings) bool { return s.TabSize == 8 }},
		{"default_language applies to plain text", `{"default_language": "bash", "languages": {"bash": {"tab_size": 3}}}`, "Plaintext",
			func(s EditorSettings) bool { return s.TabSize == 3 && s.Language == "text" }},
		{"default_language applies to no name", `{"default_language": "bash", "languages": {"bash": {"tab_size": 3}}}`, "",
			func(s EditorSettings) bool { return s.TabSize == 3 }},
		{"default_language does not apply to a known language", `{"default_language": "bash", "languages": {"bash": {"tab_size": 3}}}`, "Python",
			func(s EditorSettings) bool { return s.TabSize == 4 }},
		{"indentation_guide override for text", `{"languages": {"text": {"indentation_guide": true}}}`, "Plaintext",
			func(s EditorSettings) bool { return s.IndentationGuide }},
		{"whitespace tabs stay off when the global is off", `{"editor": {"whitespace_tabs_inner": false}}`, "Python",
			func(s EditorSettings) bool { return !s.Whitespace.TabsInner && s.Whitespace.TabsLeading }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, diags := loadBody(t, tt.body)
			if len(diags) != 0 {
				t.Fatalf("diagnostics: %v", diags)
			}
			if s := cfg.Editor(tt.lang); !tt.check(s) {
				t.Errorf("Editor(%q) = %+v", tt.lang, s)
			}
		})
	}
}

func TestLanguagesBadValues(t *testing.T) {
	tests := []struct {
		name    string
		body    string
		key     string
		wantMsg string
	}{
		{"not an object", `{"languages": []}`, "languages", "must be a JSON object"},
		{"entry not an object", `{"languages": {"go": 1}}`, "languages.go", "must be a JSON object"},
		{"unknown field", `{"languages": {"go": {"tabsize": 2}}}`, "languages.go.tabsize", "unknown setting"},
		{"tab_size too large", `{"languages": {"go": {"tab_size": 99}}}`, "languages.go.tab_size", "between 1 and 16"},
		{"use_tabs string", `{"languages": {"go": {"use_tabs": "yes"}}}`, "languages.go.use_tabs", "must be true or false"},
		{"wrap_column negative", `{"languages": {"go": {"wrap_column": -1}}}`, "languages.go.wrap_column", "at least 0"},
		{"extensions not a list", `{"languages": {"go": {"extensions": "go"}}}`, "languages.go.extensions", "list of strings"},
		{"extensions null", `{"languages": {"go": {"extensions": null}}}`, "languages.go.extensions", "null"},
		{"formatter without command", `{"languages": {"go": {"formatter": {"args": []}}}}`, "languages.go.formatter", "command must not be empty"},
		{"formatter unknown field", `{"languages": {"go": {"formatter": {"command": "x", "cmd": 1}}}}`, "languages.go.formatter", "unknown field"},
		{"on_save without command", `{"languages": {"go": {"on_save": [{"args": []}]}}}`, "languages.go.on_save", "command must not be empty"},
		{"indent unknown field", `{"languages": {"go": {"indent": {"x": "y"}}}}`, "languages.go.indent", "unknown field"},
		{"empty language name", `{"languages": {" ": {}}}`, "languages. ", "must not be empty"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, diags := loadBody(t, tt.body)
			if len(diags) != 1 || diags[0].Key != tt.key || !strings.Contains(diags[0].Message, tt.wantMsg) {
				t.Fatalf("diags = %+v, want one for key %q containing %q", diags, tt.key, tt.wantMsg)
			}
			if g := cfg.Editor("go"); g.TabSize != 8 || !g.UseTabs {
				t.Errorf("a rejected key changed go: %+v", g)
			}
		})
	}
}

func TestLanguageFieldFailureKeepsTheOthers(t *testing.T) {
	cfg, diags := loadBody(t, `{"languages": {"python": {"tab_size": 99, "line_wrap": false}}}`)
	if len(diags) != 1 || diags[0].Key != "languages.python.tab_size" {
		t.Fatalf("diags = %+v", diags)
	}
	if s := cfg.Editor("Python"); s.TabSize != 4 || s.LineWrap {
		t.Errorf("python = %+v, want tab_size 4 and line_wrap off", s)
	}
}

func TestLanguagesMergeAcrossLayers(t *testing.T) {
	isolateHome(t)
	root := t.TempDir()
	user := filepath.Join(t.TempDir(), "config.json")
	writeFile(t, user, `{"languages": {"python": {"tab_size": 2, "use_tabs": true}}}`)
	writeFile(t, filepath.Join(root, ".gvim", "config.json"), `{"languages": {"Python": {"tab_size": 8, "line_wrap": false}}}`)
	cfg, diags, err := Load(Options{Root: root, UserFile: user})
	if err != nil || len(diags) != 0 {
		t.Fatalf("Load: %v %v", err, diags)
	}
	s := cfg.Editor("Python")
	if s.TabSize != 8 || !s.UseTabs || s.LineWrap {
		t.Errorf("python = %+v, want tab_size 8 (project), use_tabs (user), line_wrap off (project)", s)
	}
}

func TestEditorSettingsAreIndependentOfConfig(t *testing.T) {
	cfg, _ := loadBody(t, `{"languages": {"go": {"on_save": [{"command": "x"}]}}}`)
	s := cfg.Editor("go")
	s.OnSave[0].Command = "changed"
	if got := cfg.Editor("go").OnSave[0].Command; got != "x" {
		t.Errorf("changing a result changed the config: %q", got)
	}
}

func TestEditorForResolvesFreshIDs(t *testing.T) {
	tests := []struct {
		name     string
		body     string
		language string
		path     string
		wantLang string
		wantTab  int
	}{
		{"sql by lexer name", `{"languages": {"sql": {"tab_size": 2}}}`, "MySQL", "/p/a.sql", "sql", 2},
		{"protobuf by lexer name", `{"languages": {"protobuf": {"tab_size": 2}}}`, "Protocol Buffer", "/p/a.proto", "protobuf", 2},
		{"alias without a path", `{"languages": {"protobuf": {"tab_size": 2}}}`, "Protocol Buffer", "", "protobuf", 2},
		{"latex by extension", `{"languages": {"latex": {"tab_size": 2}}}`, "TeX", "/p/a.tex", "latex", 2},
		{"tex by extension", `{"languages": {"tex": {"tab_size": 3}}}`, "TeX", "/p/a.sty", "tex", 3},
		{"jsonc by extension", `{"languages": {"jsonc": {"tab_size": 2}}}`, "JSON", "/p/a.jsonc", "jsonc", 2},
		{"jsonc by file name", `{"languages": {"jsonc": {"tab_size": 2}}}`, "JSON", "/p/tsconfig.app.json", "jsonc", 2},
		{"plain json is not jsonc", `{"languages": {"jsonc": {"tab_size": 2}}}`, "JSON", "/p/a.json", "json", 4},
		{"latex with fallback lexer", `{"languages": {"latex": {"tab_size": 2}}}`, "fallback", "/p/a.latex", "latex", 2},
		{"justfile beats makefile lexer", `{"languages": {"justfile": {"tab_size": 3, "auto_indent": true}}}`, "Makefile", "/p/justfile", "justfile", 3},
		{"makefile keeps its entry", `{}`, "Makefile", "/p/Makefile", "makefile", 8},
		{"explicit language kept over extension", `{"languages": {"go": {"tab_size": 3}}}`, "Go", "/p/a.txt", "go", 3},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, diags := loadBody(t, tt.body)
			if len(diags) != 0 {
				t.Fatalf("diagnostics: %v", diags)
			}
			s := cfg.EditorFor(tt.language, tt.path)
			if s.Language != tt.wantLang || s.TabSize != tt.wantTab {
				t.Errorf("EditorFor(%q, %q) = {Language %q, TabSize %d}, want {%q, %d}",
					tt.language, tt.path, s.Language, s.TabSize, tt.wantLang, tt.wantTab)
			}
		})
	}
}

func TestJustfileOverrideAutoIndent(t *testing.T) {
	cfg, _ := loadBody(t, `{"languages": {"justfile": {"auto_indent": true}}}`)
	if s := cfg.EditorFor("Makefile", "/p/justfile"); !s.AutoIndent || !s.UseTabs {
		t.Errorf("justfile settings = %+v, want auto_indent and tabs", s)
	}
}

func TestLanguageSourceUsesLowercasedID(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "user.json")
	writeFile(t, file, `{"languages": {"Go": {"tab_size": 2, "bogus": 1}}}`)
	cfg, diags, err := Load(Options{UserFile: file})
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.Source("languages.go.tab_size"); got != file {
		t.Errorf("Source(languages.go.tab_size) = %q, want %q", got, file)
	}
	if len(diags) != 1 || diags[0].Key != "languages.go.bogus" {
		t.Errorf("diags = %+v, want one for languages.go.bogus", diags)
	}
}

func TestMatchesName(t *testing.T) {
	tests := []struct {
		names []string
		base  string
		want  bool
	}{
		{[]string{"justfile"}, "justfile", true},
		{[]string{"*.pc.in"}, "a.pc.in", true},
		{[]string{"tsconfig.*.json"}, "tsconfig.app.json", true},
		{[]string{"tsconfig.*.json"}, "tsconfig.json", false},
		{[]string{"Makefile"}, "makefile", false},
	}
	for _, tt := range tests {
		if got := matchesName(tt.names, tt.base); got != tt.want {
			t.Errorf("matchesName(%v, %q) = %v, want %v", tt.names, tt.base, got, tt.want)
		}
	}
}

// TestLanguageInheritedValue checks the inherited display the Settings page shows
// for a per-language scalar: the built-in language entry applied over the global
// option (Fresh's built-in defaults), and the fields with no single display form.
func TestLanguageInheritedValue(t *testing.T) {
	t.Parallel()
	cfg := Defaults()
	tests := []struct {
		language, field, want string
		ok                    bool
	}{
		{"go", "tab_size", "8", true},            // built-in languages.go
		{"go", "use_tabs", "on", true},           // built-in use_tabs
		{"go", "auto_indent", "on", true},        // global default
		{"markdown", "auto_indent", "off", true}, // built-in markdown
		{"markdown", "auto_close", "off", true},  // built-in markdown
		{"text", "tab_size", "4", true},          // global default
		{"custom", "line_wrap", "on", true},      // no entry: global
		{"go", "wrap_column", "0", true},         // 0 reads as the viewport
		{"go", "indentation_guide", "on", true},  // guides are on by default
		{"plaintext-ish", "grammar", "", false},  // no single display form
	}
	for _, tt := range tests {
		got, ok := cfg.LanguageInheritedValue(tt.language, tt.field)
		if ok != tt.ok || got != tt.want {
			t.Errorf("LanguageInheritedValue(%q, %q) = %q, %v; want %q, %v", tt.language, tt.field, got, ok, tt.want, tt.ok)
		}
	}
}

// TestRemovablePathLanguageStructured checks the reset paths of the new
// per-language structured values: the page may remove a whole list/object or one
// of its fields.
func TestRemovablePathLanguageStructured(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		path []string
		want bool
	}{
		{[]string{"languages", "go", "extensions"}, true},
		{[]string{"languages", "go", "filenames"}, true},
		{[]string{"languages", "go", "formatter"}, true},
		{[]string{"languages", "go", "formatter", "args"}, true},
		{[]string{"languages", "go", "on_save"}, true},
		{[]string{"languages", "go", "indent"}, true},
		{[]string{"languages", "go", "indent", "increase_indent_pattern"}, true},
		{[]string{"languages", "go", "nonsense"}, false},
		{[]string{"languages", "go", "nonsense", "deeper"}, false},
	} {
		if got := RemovablePath(tt.path); got != tt.want {
			t.Errorf("RemovablePath(%v) = %v, want %v", tt.path, got, tt.want)
		}
	}
}
