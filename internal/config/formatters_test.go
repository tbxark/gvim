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

func TestLoadFormattersProjectLayerReplacesUser(t *testing.T) {
	dir := t.TempDir()
	user := filepath.Join(dir, "user.json")
	writeFile(t, user, `{"formatters": {"go": "gofmt", "rust": "rustfmt"}}`)
	root := filepath.Join(dir, "proj")
	writeFile(t, filepath.Join(root, ".gvim", "config.json"), `{"formatters": {"python": "black -q -"}}`)

	cfg, diags, err := Load(Options{Root: root, UserFile: user})
	if err != nil {
		t.Fatal(err)
	}
	if len(diags) != 0 {
		t.Fatalf("diags = %v, want none", diags)
	}
	want := map[string]string{"python": "black -q -"}
	if !reflect.DeepEqual(cfg.Formatters, want) {
		t.Fatalf("Formatters = %+v, want %+v (the project map replaces the user map)", cfg.Formatters, want)
	}
}
