package plugin

import (
	"context"

	"github.com/tbxark/gvim/pluginapi"
)

// noServices is the Services of a runtime without one: every method that can fail
// reports CodeUnsupported, Width falls back to the rune count.
type noServices struct{}

var _ pluginapi.Services = noServices{}

func (noServices) Width(s string) int { return len([]rune(s)) }
func (noServices) Truncate(s string, w int, ellipsis string) string {
	rs := []rune(s)
	if len(rs) <= w {
		return s
	}
	e := []rune(ellipsis)
	if w <= len(e) {
		return string(e[:max(w, 0)])
	}
	return string(rs[:w-len(e)]) + ellipsis
}
func (noServices) Diff(a, b []string) []pluginapi.Hunk                                 { return nil }
func (noServices) DiffText(a, b string, _ pluginapi.DiffOptions) []pluginapi.Hunk      { return nil }
func (noServices) Fuzzy(context.Context, string, []string, int) []pluginapi.FuzzyMatch { return nil }
func (noServices) Grep(context.Context, pluginapi.GrepRequest) ([]pluginapi.GrepHit, error) {
	return nil, pluginapi.Errf(pluginapi.CodeUnsupported, "workspace.grep: no search service")
}
func (noServices) Decode(string, []byte) (string, error) {
	return "", pluginapi.Errf(pluginapi.CodeUnsupported, "text.decode: no encoding service")
}
func (noServices) GitRoot(string) (string, bool) { return "", false }

// ValidateChord accepts every chord: without a keymap there is nothing to check against.
func (noServices) ValidateChord(string) error { return nil }
func (noServices) GitBranch(string) (string, error) {
	return "", pluginapi.Errf(pluginapi.CodeUnsupported, "git.branch: no git service")
}
