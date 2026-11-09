// Package hostos is the operating-system port of the plugin runtime: the OS
// interface that gvim.fs and gvim.proc call, its value types, and NopOS. The
// editor's implementation is internal/pluginhost; plugin/plugintest has scripted
// fakes.
package hostos

import (
	"context"
	"io/fs"
	"time"
)

// OS is everything a plugin can do to the machine outside the editor: files, processes,
// watchers, environment. internal/pluginhost provides the real implementation (process groups,
// fsnotify, os.OpenRoot); plugin/plugintest provides scripted fakes.
//
// Contract: every method is safe for concurrent use. The IO methods run on Work
// goroutines, never on the UI goroutine; Getenv, LookPath, Dirs and Now are cheap and may
// be called from either. The plugin package never holds a lock of the implementation and
// never touches Lua values from an OS call. Errors that a plugin may see should be
// *pluginapi.Error values (io, not_found, exec, limit, too_large ...); any other error is
// reported with code io.
type OS interface {
	// Files and directories.
	ReadFile(ctx context.Context, path string, max int64) ([]byte, error) // larger: too_large
	WriteFile(ctx context.Context, path string, data []byte, o WriteOptions) error
	ReadDir(ctx context.Context, path string, hidden bool) ([]DirEntry, error)
	Stat(ctx context.Context, path string) (FileStat, error)
	Mkdir(ctx context.Context, path string, parents bool) error
	Remove(ctx context.Context, path string, recursive bool) error
	Rename(ctx context.Context, from, to string) error
	Copy(ctx context.Context, from, to string) error
	RealPath(ctx context.Context, path string) (string, error)
	Walk(ctx context.Context, root string, o WalkOptions) (WalkResult, error)

	// Watch starts watching path. The returned Watch delivers coalesced events until
	// Stop or ctx is cancelled. A failure to add the watch is returned here.
	Watch(ctx context.Context, path string, o WatchOptions) (Watch, error)

	// Run starts argv (no shell), waits for it and returns the result. A process that
	// ran and exited non-zero is a result, not an error; an error means it could not start.
	Run(ctx context.Context, argv []string, o ProcOptions) (ProcResult, error)
	// Start starts argv and returns at once; the Proc streams output and exit.
	Start(ctx context.Context, argv []string, o ProcOptions) (Proc, error)
	LookPath(name string) (string, bool)

	Getenv(name string) (string, bool)
	Dirs() Dirs
	Now() time.Time

	// Shutdown ends everything the implementation still runs: it sends SIGKILL to the
	// process group of every live process and stops every watcher. It does not wait
	// for the processes. Idempotent; Runtime.Stop calls it (AMENDMENTS C16).
	Shutdown()
}

// Dirs are the well-known directories handed to gvim.dirs.
type Dirs struct {
	Config, Home, Temp, Cwd, Root string
}

// WriteOptions tune OS.WriteFile.
type WriteOptions struct {
	Atomic  bool // temp file in the same directory, then rename
	Append  bool
	Mode    fs.FileMode // 0: the mode of the file being replaced, else 0644
	Parents bool        // create missing parent directories
	// Exclusive creates the file only when the path does not exist as a file,
	// directory or symlink; an existing entry makes the write fail (fs.ErrExist) and
	// is left untouched. It is a create-new, not a replace: Atomic is ignored, Append
	// and Parents are refused by the backends that cannot honour them atomically.
	Exclusive bool
}

// DirEntry is one entry of OS.ReadDir.
type DirEntry struct {
	Name       string
	Kind       string // "file", "dir", "symlink"
	Size       int64
	MTime      time.Time
	TargetKind string // for a symlink: the kind it points to, "" when broken
}

// FileStat is the result of OS.Stat.
type FileStat struct {
	Kind  string // "file", "dir", "symlink"
	Size  int64
	MTime time.Time
	Mode  fs.FileMode
}

// WalkOptions tune OS.Walk.
type WalkOptions struct {
	SkipDirs   []string
	Hidden     bool
	MaxDepth   int // <= 0: unlimited
	MaxEntries int // <= 0: 50000
}

// WalkEntry is one entry found by OS.Walk.
type WalkEntry struct {
	Path, Rel string
	Kind      string
	Size      int64
	MTime     time.Time
}

// WalkResult is the result of OS.Walk.
type WalkResult struct {
	Entries   []WalkEntry
	Truncated bool
}

// WatchOptions tune OS.Watch.
type WatchOptions struct {
	Debounce time.Duration // per-path coalescing window; 0: 50 ms
}

// WatchEvent is one coalesced change.
type WatchEvent struct {
	Path string
	Kind string // "create", "write", "remove", "rename", "error"
	Err  string // set for Kind "error"
}

// Watch is a running watcher.
type Watch interface {
	// Next blocks for the next event; false after Stop or when ctx ended.
	Next(ctx context.Context) (WatchEvent, bool)
	Stop()
}

// ProcOptions tune OS.Run and OS.Start.
type ProcOptions struct {
	Cwd         string
	Env         map[string]string // added to the environment
	Stdin       []byte
	OpenStdin   bool // Start only: keep a stdin pipe for Proc.Write
	Timeout     time.Duration
	MaxOutput   int // bytes kept per stream; <= 0: 8 MiB
	MergeStderr bool
}

// ProcResult is the outcome of a process that ran.
type ProcResult struct {
	Code             int
	Signal           string
	Stdout, Stderr   []byte
	TimedOut         bool
	Truncated        bool
	StartErr, Cancel string // optional diagnostics, "" when none
}

// ProcChunk is a batch of output of a running process.
type ProcChunk struct {
	Stderr bool
	Data   []byte
}

// Proc is a running process.
type Proc interface {
	PID() int
	// Write queues data for stdin (bounded buffer); it never blocks. A full buffer is
	// a CodeBusy error, a closed stdin or exited process a CodeClosed error.
	Write(data []byte) error
	CloseStdin()
	// Kill sends the signal ("" or "TERM": terminate, "KILL": kill, "INT", "HUP") to the
	// process group.
	Kill(signal string) error
	// Terminate is the unload policy: SIGTERM now and SIGKILL after grace, from a
	// goroutine of the implementation (nothing waits). The process's output is
	// discarded from then on. Idempotent.
	Terminate(grace time.Duration)
	// Output blocks for the next batch of output; false after the process ended and
	// all output was delivered, or when ctx ended.
	Output(ctx context.Context) (ProcChunk, bool)
	// Wait blocks until the process ended.
	Wait(ctx context.Context) (ProcResult, error)
}
