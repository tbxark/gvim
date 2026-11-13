package terminal

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// Clean cuts at a rune boundary, so a clipped value is still valid UTF-8.
func TestLaunchCleanDoesNotSplitARune(t *testing.T) {
	// "é" is two bytes, so a cut at an odd byte count lands inside a rune.
	long := strings.Repeat("é", LaunchTextMax)
	l := Launch{
		Argv:       []string{long},
		ResumeArgv: []string{long},
		Agent:      strings.Repeat("é", LaunchAgentMax),
		Source:     "x" + long,
	}.Clean()
	for name, got := range map[string]string{
		"argv": l.Argv[0], "resume argv": l.ResumeArgv[0], "agent": l.Agent, "source": l.Source,
	} {
		if !utf8.ValidString(got) {
			t.Errorf("%s was cut inside a rune: %q", name, got[len(got)-3:])
		}
	}
	if len(l.Argv[0]) > LaunchTextMax || len(l.Agent) > LaunchAgentMax || len(l.Source) > LaunchTextMax {
		t.Errorf("Clean left a value over its bound: %d %d %d", len(l.Argv[0]), len(l.Agent), len(l.Source))
	}
}
