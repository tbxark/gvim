package plugin

import (
	"context"
	"testing"
	"testing/fstest"

	"github.com/tbxark/gvim/plugin/hostos"
)

// A plugin that is not enabled keeps neither its sources nor a compiled copy in the
// candidate: enabling reads it again, so holding them only costs heap.
func TestGatherDropsSourcesOfUnenabledPlugins(t *testing.T) {
	bundled := fstest.MapFS{
		"plugins/on/init.lua":          {Data: []byte("x = 1\n")},
		"plugins/off/init.lua":         {Data: []byte("x = 2\n")},
		"plugins/off/plugin.json":      {Data: []byte(`{"default_enabled": false}`)},
		"plugins/broken/init.lua":      {Data: []byte("x = 3\n")},
		"plugins/broken/plugin.json":   {Data: []byte(`{`)},
		"plugins/off/extra/helper.lua": {Data: []byte("return 1\n")},
	}
	opts := &LoaderOptions{Bundled: bundled}
	d := opts.gather(context.Background(), hostos.NopOS{}, "", nil)
	byName := map[string]*candidate{}
	for _, c := range d.cands {
		byName[c.name] = c
	}
	if c := byName["on"]; c == nil || !c.enabled || c.compiled == nil || c.raw != nil {
		t.Fatalf("enabled plugin: %+v", c)
	}
	for _, name := range []string{"off", "broken"} {
		c := byName[name]
		if c == nil {
			t.Fatalf("%s is not a candidate", name)
		}
		if c.raw != nil || c.compiled != nil {
			t.Errorf("%s keeps its sources: raw=%d compiled=%d", name, len(c.raw), len(c.compiled))
		}
	}
	if byName["off"].enabled {
		t.Error("off should be disabled by default_enabled=false")
	}
	if byName["broken"].err == "" {
		t.Error("broken should carry the manifest error")
	}
}
