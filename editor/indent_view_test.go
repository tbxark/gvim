package editor

import "testing"

// Tab and dedent follow the document's own indentation settings, when it has them, and
// the editor's options otherwise.
func TestTabFollowsTheDocumentIndentation(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		opts    Options
		actions []Action
		args    []map[string]any // one per action; nil when the action takes none
		want    string
	}{
		{
			name: "editor options insert spaces",
			opts: Options{UseTabs: false, TabSize: 4},
			want: "    ",
		},
		{
			name:    "document tab size wins over the editor's",
			opts:    Options{UseTabs: false, TabSize: 4},
			actions: []Action{"set_tab_size"},
			args:    []map[string]any{{"size": 2}},
			want:    "  ",
		},
		{
			name:    "document indentation wins over the editor's tabs",
			opts:    Options{UseTabs: true, TabSize: 4},
			actions: []Action{"toggle_indentation_style"},
			args:    []map[string]any{{"value": false}},
			want:    "    ",
		},
		{
			name:    "document keeps tabs when the editor uses spaces",
			opts:    Options{UseTabs: false, TabSize: 4},
			actions: []Action{"toggle_indentation_style"},
			args:    []map[string]any{{"value": true}},
			want:    "\t",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			e := docWith(tt.opts, "")
			for i, a := range tt.actions {
				if res := e.Do(a, tt.args[i]); res.Err != nil {
					t.Fatalf("%s: %v", a, res.Err)
				}
			}
			if res := e.Do("insert_tab", nil); res.Err != nil {
				t.Fatalf("insert_tab: %v", res.Err)
			}
			if got := e.Active().Buffer.Text(); got != tt.want {
				t.Errorf("text after Tab = %q, want %q", got, tt.want)
			}
		})
	}
}
