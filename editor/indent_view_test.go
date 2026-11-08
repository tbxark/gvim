package editor

import "testing"

// Tab and dedent follow the document's own indentation settings, when it has them, and
// the editor's options otherwise.
func TestTabFollowsTheDocumentIndentation(t *testing.T) {
	t.Parallel()
	tests := []struct {
		Name    string
		opts    Options
		actions []Action
		Args    []map[string]any // one per action; nil when the action takes none
		Want    string
	}{
		{
			Name: "editor options insert spaces",
			opts: Options{UseTabs: false, TabSize: 4},
			Want: "    ",
		},
		{
			Name:    "document tab size wins over the editor's",
			opts:    Options{UseTabs: false, TabSize: 4},
			actions: []Action{"set_tab_size"},
			Args:    []map[string]any{{"size": 2}},
			Want:    "  ",
		},
		{
			Name:    "document indentation wins over the editor's tabs",
			opts:    Options{UseTabs: true, TabSize: 4},
			actions: []Action{"toggle_indentation_style"},
			Args:    []map[string]any{{"value": false}},
			Want:    "    ",
		},
		{
			Name:    "document keeps tabs when the editor uses spaces",
			opts:    Options{UseTabs: false, TabSize: 4},
			actions: []Action{"toggle_indentation_style"},
			Args:    []map[string]any{{"value": true}},
			Want:    "\t",
		},
	}
	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			t.Parallel()
			e := docWith(tt.opts, "")
			for i, a := range tt.actions {
				if res := e.Do(a, tt.Args[i]); res.Err != nil {
					t.Fatalf("%s: %v", a, res.Err)
				}
			}
			if res := e.Do("insert_tab", nil); res.Err != nil {
				t.Fatalf("insert_tab: %v", res.Err)
			}
			if got := e.Active().Buffer.Text(); got != tt.Want {
				t.Errorf("text after Tab = %q, want %q", got, tt.Want)
			}
		})
	}
}
