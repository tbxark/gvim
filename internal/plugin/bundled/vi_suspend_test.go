package bundled_test

import (
	"strings"
	"testing"
	"time"
)

// When vi is suspended after three strikes, its status item and its input mode go: a stale
// "-- NORMAL --" would say that a mode is on that no longer takes keys.
func TestViSuspendedDropsItsStatusAndMode(t *testing.T) {
	t.Parallel()
	v := newViTimeout(t, strings.Repeat("alpha beta gamma delta\n", 120000), 15*time.Millisecond)
	v.lenient = true // the command that runs out of time is an error of the plugin
	if v.label() != "-- NORMAL --" {
		t.Fatalf("label = %q before", v.label())
	}
	for i := 0; i < 6; i++ {
		p, _ := v.h.Plugin(viPlugin)
		if p.Status == "suspended" {
			break
		}
		v.keys(":«%s/a/b/g»")
	}
	if p, _ := v.h.Plugin(viPlugin); p.Status != "suspended" {
		t.Fatalf("status = %q, want suspended; log %v", p.Status, v.h.LogText(viPlugin))
	}
	if got := v.label(); got != "" {
		t.Errorf("label = %q after the suspension, want none", got)
	}
	if got := v.mode(); got != "" {
		t.Errorf("input mode = %q after the suspension, want none", got)
	}
}
