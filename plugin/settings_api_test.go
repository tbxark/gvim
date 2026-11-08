package plugin_test

import (
	"testing"

	"github.com/tbxark/gvim/plugin/plugintest"
)

// settingsPlugin declares a boolean, an enum, a bounded integer and a project setting.
const settingsPlugin = `
local gvim = require 'gvim'
gvim.settings.define{
  enabled = { type = "boolean", default = true, description = "turn it on" },
  mode = { type = "enum", values = { "fast", "slow" }, default = "fast" },
  count = { type = "integer", minimum = 1, maximum = 10, default = 3, project = true },
}
gvim.command.register{ name = "hi", title = "Hi", run = function() end }
`

// TestSettingsSchemaAndValuesAPI checks the read-only runtime view of a plugin's declared
// settings: types, defaults, choices, bounds, project marker and effective values with the
// layer that supplied them.
func TestSettingsSchemaAndValuesAPI(t *testing.T) {
	h := plugintest.New(t, plugintest.Files{"cfg.lua": settingsPlugin}, plugintest.Options{})
	h.Start()
	schema, ok := h.RT.SettingsSchema("cfg")
	if !ok || len(schema) != 3 {
		t.Fatalf("schema = %+v, %v", schema, ok)
	}
	byKey := map[string]int{}
	for i, s := range schema {
		byKey[s.Key] = i
	}
	enabled := schema[byKey["enabled"]]
	if enabled.Type != "boolean" || enabled.Default != false && enabled.Default != true || enabled.Description != "turn it on" {
		t.Fatalf("enabled spec = %+v", enabled)
	}
	mode := schema[byKey["mode"]]
	if mode.Type != "enum" || len(mode.Choices) != 2 || mode.Default != "fast" || mode.Project {
		t.Fatalf("mode spec = %+v", mode)
	}
	count := schema[byKey["count"]]
	if count.Type != "integer" || count.Min == nil || *count.Min != 1 || count.Max == nil || *count.Max != 10 || !count.Project {
		t.Fatalf("count spec = %+v", count)
	}

	values, ok := h.RT.SettingsValues("cfg")
	if !ok {
		t.Fatal("no values")
	}
	got := map[string]any{}
	for _, v := range values {
		got[v.Key] = v.Value
	}
	if got["enabled"] != true || got["mode"] != "fast" || got["count"] != float64(3) {
		t.Fatalf("default values = %+v", got)
	}
	// A user layer and a project layer override the effective values.
	h.RT.UpdateSettings("cfg", map[string]any{"mode": "slow", "count": float64(7)}, map[string]any{"count": float64(9)})
	values, _ = h.RT.SettingsValues("cfg")
	for _, v := range values {
		switch v.Key {
		case "mode":
			if v.Value != "slow" {
				t.Fatalf("mode value = %v", v.Value)
			}
		case "count":
			if v.Value != float64(9) || !v.Project {
				t.Fatalf("count value = %+v, want the project layer", v)
			}
		}
	}
	if _, ok := h.RT.SettingsSchema("nope"); ok {
		t.Fatal("unknown plugin has a schema")
	}
}
