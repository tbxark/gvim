package plugin

import (
	"reflect"
	"strings"
	"testing"
)

func TestParseManifest(t *testing.T) {
	yes, no := true, false
	tests := []struct {
		name    string
		in      string
		want    manifest
		wantErr string
	}{
		{name: "empty object", in: `{}`, want: manifest{}},
		{
			name: "everything, with comments and trailing commas",
			in: `{
  // JSONC
  "name": "git_gutter", "version": "1.0.0", "description": "d",
  "api": 1, "main": "src/../main.lua", "default_enabled": false,
  "requires": ["lib_x", "lib_x", "lib_y",],
  "after": ["flash", "Bad Name"], /* block */
  "future": 1,
}`,
			want: manifest{Name: "git_gutter", Version: "1.0.0", Description: "d", API: 1, Main: "main.lua",
				DefaultEnabled: &no, Requires: []string{"lib_x", "lib_y"}, After: []string{"flash"}, Unknown: []string{"future"}},
		},
		{name: "default_enabled true", in: `{"default_enabled": true}`, want: manifest{DefaultEnabled: &yes}},
		{name: "not an object", in: `[1]`, wantErr: "want a JSON object"},
		{name: "not json", in: `{name: x}`, wantErr: "plugin.json:"},
		{name: "wrong type", in: `{"version": 3}`, wantErr: `"version" has the wrong type`},
		{name: "requires not a list", in: `{"requires": "x"}`, wantErr: `"requires" has the wrong type`},
		{name: "bad name", in: `{"name": "Git-Gutter"}`, wantErr: "invalid name"},
		{name: "bad requires entry", in: `{"requires": ["ok", "No Good"]}`, wantErr: `lists the invalid plugin name "No Good"`},
		{name: "api zero", in: `{"api": 0}`, wantErr: `"api" must be a positive integer`},
		{name: "api fraction", in: `{"api": 1.5}`, wantErr: `"api" must be a positive integer`},
		{name: "api too new", in: `{"api": 2}`, wantErr: "needs plugin API 2, this gvim provides 1"},
		{name: "main escapes", in: `{"main": "../x.lua"}`, wantErr: `"main" must be a .lua file inside the plugin directory`},
		{name: "main absolute", in: `{"main": "/etc/x.lua"}`, wantErr: `"main" must be a .lua file inside`},
		{name: "main not lua", in: `{"main": "init.txt"}`, wantErr: `"main" must be a .lua file inside`},
		{name: "main with backslash escape", in: `{"main": "..\\x.lua"}`, wantErr: `"main" must be a .lua file inside`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseManifest([]byte(tt.in))
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("err = %v, want it to contain %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("manifest = %+v\nwant       %+v", got, tt.want)
			}
		})
	}
}

func TestPlanOrder(t *testing.T) {
	mk := func(name string, requires, after []string) *candidate {
		return &candidate{name: name, man: manifest{Requires: requires, After: after}}
	}
	ext := func(name string) (bool, string) {
		if name == "running" {
			return true, ""
		}
		return false, "not installed"
	}
	tests := []struct {
		name        string
		set         []*candidate
		wantOrder   []string
		wantBlocked map[string]string // name -> substring
	}{
		{"keeps source order without edges", []*candidate{mk("b", nil, nil), mk("a", nil, nil)}, []string{"b", "a"}, nil},
		{"dependency first", []*candidate{mk("a", []string{"b"}, nil), mk("b", nil, nil)}, []string{"b", "a"}, nil},
		{"chain", []*candidate{mk("a", []string{"b"}, nil), mk("b", []string{"c"}, nil), mk("c", nil, nil)}, []string{"c", "b", "a"}, nil},
		{"after only orders", []*candidate{mk("a", nil, []string{"b"}), mk("b", nil, nil)}, []string{"b", "a"}, nil},
		{"after cycle is still a cycle", []*candidate{mk("a", nil, []string{"b"}), mk("b", nil, []string{"a"}), mk("c", nil, nil)}, []string{"c"}, map[string]string{"a": "cycle", "b": "cycle"}},
		{"external running dependency", []*candidate{mk("a", []string{"running"}, nil)}, []string{"a"}, nil},
		{"external missing dependency", []*candidate{mk("a", []string{"gone"}, nil), mk("b", nil, nil)}, []string{"b"}, map[string]string{"a": `requires "gone" (not installed)`}},
		{"blocked propagates", []*candidate{mk("a", []string{"b"}, nil), mk("b", []string{"gone"}, nil)}, nil, map[string]string{"a": "cannot be loaded", "b": "not installed"}},
		{"behind a cycle", []*candidate{mk("a", []string{"b"}, nil), mk("b", []string{"a"}, nil), mk("z", []string{"a"}, nil)}, nil, map[string]string{"a": "cycle", "b": "cycle", "z": "cycle"}},
		{"self after is ignored", []*candidate{mk("a", nil, []string{"a"})}, []string{"a"}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			order, blocked := planOrder(tt.set, ext)
			var got []string
			for _, c := range order {
				got = append(got, c.name)
			}
			if !reflect.DeepEqual(got, tt.wantOrder) {
				t.Fatalf("order = %v, want %v", got, tt.wantOrder)
			}
			if len(blocked) != len(tt.wantBlocked) {
				t.Fatalf("blocked = %v, want %v", blocked, tt.wantBlocked)
			}
			for c, why := range blocked {
				if want, ok := tt.wantBlocked[c.name]; !ok || !strings.Contains(why, want) {
					t.Errorf("%s blocked by %q, want %q", c.name, why, want)
				}
			}
		})
	}
}

func TestSanitizeName(t *testing.T) {
	for in, want := range map[string]string{
		"hello": "hello", "Hello World": "hello_world", "my-plugin.v2": "my_plugin_v2", "_x": "x",
		"007": "buffer", "": "buffer", "日本語": "buffer", strings.Repeat("z", 50): strings.Repeat("z", 40),
	} {
		got := sanitizeName(in)
		if got != want || !nameRE.MatchString(got) {
			t.Errorf("sanitizeName(%q) = %q, want %q", in, got, want)
		}
	}
}
