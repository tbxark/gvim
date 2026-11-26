package config

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestLoadFormatters(t *testing.T) {
	tests := []struct {
		name     string
		body     string
		want     map[string]string
		wantDiag string // part of the diagnostic message for key formatters; empty when none
	}{
		{
			name: "language to command",
			body: `{"formatters": {"go": "gofmt", "Python": "black -q -"}}`,
			want: map[string]string{"go": "gofmt", "Python": "black -q -"},
		},
		{
			name: "empty map has no formatter",
			body: `{"formatters": {}}`,
			want: map[string]string{},
		},
		{
			name:     "empty command",
			body:     `{"formatters": {"go": ""}}`,
			wantDiag: "command must not be empty",
		},
		{
			name:     "blank command",
			body:     `{"formatters": {"go": "   "}}`,
			wantDiag: "command must not be empty",
		},
		{
			name:     "null command",
			body:     `{"formatters": {"go": null}}`,
			wantDiag: "command must not be empty",
		},
		{
			name:     "command not a string",
			body:     `{"formatters": {"go": ["gofmt"]}}`,
			wantDiag: "must be a JSON object of strings",
		},
		{
			name:     "empty language name",
			body:     `{"formatters": {"": "gofmt"}}`,
			wantDiag: "language name must not be empty",
		},
		{
			name:     "not an object",
			body:     `{"formatters": "gofmt"}`,
			wantDiag: "must be a JSON object of strings",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.json")
			writeFile(t, path, tt.body)
			cfg, diags, err := Load(Options{UserFile: path})
			if err != nil {
				t.Fatal(err)
			}
			if tt.wantDiag == "" {
				if len(diags) != 0 {
					t.Fatalf("diags = %v, want none", diags)
				}
				if !reflect.DeepEqual(cfg.Formatters, tt.want) {
					t.Fatalf("Formatters = %+v, want %+v", cfg.Formatters, tt.want)
				}
				return
			}
			if len(diags) != 1 || diags[0].Key != "formatters" || !strings.Contains(diags[0].Message, tt.wantDiag) {
				t.Fatalf("diags = %+v, want one for key formatters containing %q", diags, tt.wantDiag)
			}
			if cfg.Formatters != nil {
				t.Fatalf("Formatters = %+v after a bad formatters key, want none", cfg.Formatters)
			}
		})
	}
}

func TestLoadFormattersAreEmptyByDefault(t *testing.T) {
	cfg, diags, err := Load(Options{UserFile: filepath.Join(t.TempDir(), "missing.json")})
	if err != nil {
		t.Fatal(err)
	}
	if len(diags) != 0 {
		t.Fatalf("diags = %v, want none", diags)
	}
	if cfg.Formatters != nil {
		t.Fatalf("Formatters = %+v with no file, want none", cfg.Formatters)
	}
}

func TestProjectLayerCannotChooseCommands(t *testing.T) {
	tests := []struct {
		name    string
		project string
		wantKey string
	}{
		{"formatters", `{"formatters": {"python": "black -q -"}}`, "formatters"},
		{"language formatter", `{"languages": {"go": {"formatter": {"command": "evil"}, "format_on_save": true}}}`, "languages.go.formatter"},
		{"language on_save", `{"languages": {"go": {"on_save": [{"command": "evil"}]}}}`, "languages.go.on_save"},
	}
	const user = `{"formatters": {"go": "gofmt"}, "languages": {"go": {"on_save": [{"command": "good"}]}}}`
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, diags := loadLayers(t, user, tt.project)
			if len(diags) != 1 || diags[0].Key != tt.wantKey || !strings.Contains(diags[0].Message, "project config cannot set") {
				t.Fatalf("diags = %+v, want one refusal for %s", diags, tt.wantKey)
			}
			if want := map[string]string{"go": "gofmt"}; !reflect.DeepEqual(cfg.Formatters, want) {
				t.Errorf("Formatters = %+v, want the user map %+v", cfg.Formatters, want)
			}
			s := cfg.Editor("Go")
			if s.Formatter != nil || len(s.OnSave) != 1 || s.OnSave[0].Command != "good" {
				t.Errorf("Go settings = %+v, want the user on_save only", s)
			}
		})
	}
}

func TestProjectLayerKeepsFormatOnSaveFlag(t *testing.T) {
	cfg, diags := loadLayers(t, `{}`, `{"languages": {"go": {"format_on_save": true, "formatter": {"command": "evil"}}}}`)
	if len(diags) != 1 || diags[0].Key != "languages.go.formatter" {
		t.Fatalf("diags = %+v", diags)
	}
	if s := cfg.Editor("Go"); !s.FormatOnSave || s.Formatter != nil {
		t.Errorf("Go settings = %+v, want format_on_save kept and the formatter dropped", s)
	}
}
