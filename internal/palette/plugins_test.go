package palette

import (
	"reflect"
	"testing"
)

func TestPluginEntry(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name                               string
		action, title, description, plugin string
		when                               []string
		wantDesc                           string
		wantWhen                           []string
	}{
		{name: "full", action: "greet.hello", title: "Say Hello", description: "Greets", plugin: "greet", wantDesc: "Greets"},
		{name: "empty description names the plugin", action: "greet.hello", title: "Say Hello", plugin: "greet", wantDesc: "Command from plugin greet"},
		{name: "no plugin name", action: "x", title: "X", wantDesc: "Command from a plugin"},
		{name: "when is kept as predicates", action: "a.b", title: "B", plugin: "a", when: []string{"has_buffer", "!read_only"}, wantDesc: "Command from plugin a", wantWhen: []string{"has_buffer", "!read_only"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := PluginEntry(tt.action, tt.title, tt.description, tt.plugin, tt.when)
			want := Entry{
				Name:        tt.title,
				Description: tt.wantDesc,
				Action:      tt.action,
				Source:      SourcePlugin,
				When:        tt.wantWhen,
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("PluginEntry = %+v, want %+v", got, want)
			}
		})
	}
}

func TestPluginEntryCopiesWhen(t *testing.T) {
	t.Parallel()
	when := []string{"has_buffer"}
	e := PluginEntry("a.b", "B", "", "a", when)
	when[0] = "read_only"
	if e.When[0] != "has_buffer" {
		t.Fatalf("When = %v, aliases the caller's slice", e.When)
	}
}

// A plugin entry goes through the same ranking and availability rules as a built-in
// one: it is listed by name and its action is the plugin command id. Its when
// predicates are not key contexts: the ui evaluates them, so they never disable the
// row here.
func TestPluginEntrySearch(t *testing.T) {
	t.Parallel()
	entries := []Entry{
		PluginEntry("greet.hello", "Say Hello", "", "greet", nil),
		PluginEntry("greet.panel", "Panel Only", "", "greet", []string{"has_buffer", "!read_only"}),
	}
	got := Search(entries, ">hello")
	if len(got) != 1 || got[0].Action != "greet.hello" || got[0].Disabled {
		t.Fatalf("Search(>hello) = %+v, want the enabled greet.hello row", got)
	}
	got = Search(entries, ">panel")
	if len(got) != 1 || got[0].Action != "greet.panel" || got[0].Disabled {
		t.Fatalf("Search(>panel) = %+v, want greet.panel enabled", got)
	}
	if want := []string{"has_buffer", "!read_only"}; !reflect.DeepEqual(got[0].When, want) {
		t.Fatalf("Search(>panel) When = %v, want %v", got[0].When, want)
	}
}

// A when predicate is not a key context: with a buffer open, SearchIn must list the
// row enabled whatever the predicates are.
func TestPluginEntryWhenDoesNotDisable(t *testing.T) {
	t.Parallel()
	e := PluginEntry("a.b", "B", "", "a", []string{"has_buffer"})
	got := SearchIn([]Entry{e}, ">b", DefaultState())
	if len(got) != 1 || got[0].Disabled {
		t.Fatalf("SearchIn = %+v, want one enabled row", got)
	}
}
