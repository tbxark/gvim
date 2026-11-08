package hostos

import (
	"context"
	"os"
	"time"

	"github.com/tbxark/gvim/pluginapi"
)

// NopOS is an OS that refuses every file and process operation with CodeUnsupported. It
// is the default of plugin.Options.OS, so a runtime without an OS still starts.
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
