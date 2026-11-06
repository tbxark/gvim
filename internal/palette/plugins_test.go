package palette

import (
	"reflect"
	"testing"
)

func TestPluginEntryRunsTheNamedCommand(t *testing.T) {
	t.Parallel()
	got := PluginEntry("greet", "Say Hello")
	want := Entry{
		Name:        "Say Hello",
		Description: "Command from a plugin",
		Action:      "plugin:greet",
		Source:      "plugin",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("PluginEntry = %+v, want %+v", got, want)
	}
}

func TestPluginNameParsesPluginActions(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		action string
		want   string
		ok     bool
	}{
		{name: "plugin action", action: "plugin:greet", want: "greet", ok: true},
		{name: "name with a colon", action: "plugin:a:b", want: "a:b", ok: true},
		{name: "builtin action", action: "save", want: "", ok: false},
		{name: "prefix must match exactly", action: "plugins:greet", want: "", ok: false},
		{name: "empty action", action: "", want: "", ok: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := PluginName(tt.action)
			if got != tt.want || ok != tt.ok {
				t.Fatalf("PluginName(%q) = %q, %v; want %q, %v", tt.action, got, ok, tt.want, tt.ok)
			}
		})
	}
}

func TestPluginEntryActionRoundTrips(t *testing.T) {
	t.Parallel()
	e := PluginEntry("shout", "Shout It")
	name, ok := PluginName(e.Action)
	if !ok || name != "shout" {
		t.Fatalf("PluginName(%q) = %q, %v; want shout, true", e.Action, name, ok)
	}
}
