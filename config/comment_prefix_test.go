package config

import "testing"

func TestDefaultCommentPrefixes(t *testing.T) {
	tests := []struct {
		name     string
		body     string
		language string
		path     string
		want     string
	}{
		{"go by name", `{}`, "Go", "", "//"},
		{"rust by path", `{}`, "", "/p/main.rs", "//"},
		{"python by path", `{}`, "", "/p/a.py", "#"},
		{"makefile by file name", `{}`, "", "/p/Makefile", "#"},
		{"dockerfile by file name", `{}`, "", "/p/Dockerfile", "#"},
		{"zsh by extension", `{}`, "", "/p/a.zsh", "#"},
		{"lua", `{}`, "", "/p/a.lua", "--"},
		{"sql by lexer name", `{}`, "MySQL", "", "--"},
		{"latex", `{}`, "", "/p/a.tex", "%"},
		{"ini", `{}`, "", "/p/a.ini", ";"},
		{"fortran", `{}`, "", "/p/a.f90", "!"},
		{"json has none", `{}`, "", "/p/a.json", ""},
		{"markdown has none", `{}`, "Markdown", "", ""},
		{"plain text has none", `{}`, "", "", ""},
		{"user prefix wins", `{"languages": {"go": {"comment_prefix": "#"}}}`, "Go", "", "#"},
		{"user prefix for a language without one", `{"languages": {"json": {"comment_prefix": "//"}}}`, "JSON", "", "//"},
		{"empty user prefix switches it off", `{"languages": {"go": {"comment_prefix": ""}}}`, "Go", "", ""},
		{"null user prefix keeps the default", `{"languages": {"go": {"comment_prefix": null}}}`, "Go", "", "//"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, diags := loadBody(t, tt.body)
			if len(diags) != 0 {
				t.Fatalf("diagnostics: %v", diags)
			}
			if got := cfg.EditorFor(tt.language, tt.path).CommentPrefix; got != tt.want {
				t.Errorf("EditorFor(%q, %q).CommentPrefix = %q, want %q", tt.language, tt.path, got, tt.want)
			}
		})
	}
}
