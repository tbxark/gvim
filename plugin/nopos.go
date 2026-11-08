package plugin

import (
	"context"
	"os"
	"time"

	"github.com/tbxark/gvim/pluginapi"
)

// NopOS is an OS that refuses every file and process operation with CodeUnsupported. It
// is the default of Options.OS, so a runtime without an OS still starts.
type NopOS struct{}

var _ OS = NopOS{}

func errNoOS(what string) error {
	return pluginapi.Errf(pluginapi.CodeUnsupported, "%s: no operating system access is configured", what)
}

func (NopOS) ReadFile(context.Context, string, int64) ([]byte, error) { return nil, errNoOS("fs.read") }
func (NopOS) WriteFile(context.Context, string, []byte, WriteOptions) error {
	return errNoOS("fs.write")
}
func (NopOS) ReadDir(context.Context, string, bool) ([]DirEntry, error) {
	return nil, errNoOS("fs.list")
}
func (NopOS) Stat(context.Context, string) (FileStat, error)   { return FileStat{}, errNoOS("fs.stat") }
func (NopOS) Mkdir(context.Context, string, bool) error        { return errNoOS("fs.mkdir") }
func (NopOS) Remove(context.Context, string, bool) error       { return errNoOS("fs.remove") }
func (NopOS) Rename(context.Context, string, string) error     { return errNoOS("fs.rename") }
func (NopOS) Copy(context.Context, string, string) error       { return errNoOS("fs.copy") }
func (NopOS) RealPath(context.Context, string) (string, error) { return "", errNoOS("fs.realpath") }
func (NopOS) Walk(context.Context, string, WalkOptions) (WalkResult, error) {
	return WalkResult{}, errNoOS("fs.walk")
}
func (NopOS) Watch(context.Context, string, WatchOptions) (Watch, error) {
	return nil, errNoOS("fs.watch")
}
func (NopOS) Run(context.Context, []string, ProcOptions) (ProcResult, error) {
	return ProcResult{}, errNoOS("proc.run")
}
func (NopOS) Start(context.Context, []string, ProcOptions) (Proc, error) {
	return nil, errNoOS("proc.spawn")
}
func (NopOS) LookPath(string) (string, bool) { return "", false }

// Getenv reads the real environment: a read has no side effect.
func (NopOS) Getenv(name string) (string, bool) { return os.LookupEnv(name) }
func (NopOS) Dirs() Dirs                        { return Dirs{} }
func (NopOS) Now() time.Time                    { return time.Now() }
func (NopOS) Shutdown()                         {}

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
