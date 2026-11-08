package pluginapi

import (
	"context"
	"time"
)

// ---------- async ----------

// Completion resumes a parked wait, or delivers one stream item. Values are plain Go values
// (nil, bool, int64, float64, string, []byte, []any, map[string]any, BufferID, WinID, Pos, Range,
// Key, Hunk) or, for Work the runtime created itself, opaque runtime-owned values.
type Completion struct {
	Token  Token
	Kind   string // "" for a final result; "query" and "move" for picker interaction streams
	Values []any
	Err    string // "<code>: <msg>" or ""
	More   bool   // true for stream items that are not the last
}

// Work is IO the UI layer runs in a tea.Cmd. Exactly one of Run, Stream, After is set.
// A Work goroutine never touches Lua values, the Host, the editor or the Model.
type Work struct {
	Token  Token
	Plugin string
	Gen    uint64
	Ctx    context.Context
	Run    func(ctx context.Context) Completion
	Stream func(ctx context.Context) (Completion, bool) // blocking next item; false after the last
	After  time.Duration                                // timer when Run and Stream are nil
}

// ---------- events ----------

// Event is something that happened in the editor, or a plugin's own emit.
// Target limits delivery to one plugin ("" = every subscriber); the runtime uses it
// for replay after a plugin loads.
type Event struct {
	Name   string
	Target string
	// Data holds plain values plus BufferID/WinID/Pos/Range, which the runtime turns into
	// handles/tables. buffer_closed is the exception: Data["id"] is the id of the buffer
	// that is gone, any integer type or a BufferID, and the handler receives it as a plain
	// number (the handle is dead and must not be re-created).
	Data map[string]any
}

// ---------- runtime side (ui -> plugin) ----------

// PluginInfo is one row of the plugin manager.
type PluginInfo struct {
	Name, Version, Source, Dir string // Source: bundled|user|cli|init|buffer
	// File is the absolute path of the file the plugin runs from: the entry of a directory
	// plugin (its plugin.json main, or init.lua) or the .lua file of a single-file plugin.
	// It is "" for a plugin of the binary or of a buffer.
	File                    string
	Status                  string // loaded|loading|disabled|failed|suspended|legacy
	Err                     string
	Locked                  string // non-empty: why the manager cannot toggle it ("set in config")
	Commands, Subscriptions int
	Enabled                 bool
}

// LogEntry is one line of the plugin log.
type LogEntry struct {
	Time          time.Time
	Plugin, Level string
	Msg           string
}

// Notice is a message for the status row.
type Notice struct {
	Text string
	Warn bool // also goes to the warnings log
}

// Control is a lifecycle request from the plugin manager or a command.
type Control struct {
	Op   string // "reload" | "reload_all" | "enable" | "disable" | "load_text" | "init_check" | "init_reload"
	Name string
	Text string // source for load_text
}

// Runtime is the Lua runtime as the ui sees it. It is single-goroutine: every
// method runs on the UI goroutine.
type Runtime interface {
	// Start attaches the host and returns the Work that discovers and reads plugin sources.
	// Call once from ui.Model.Init. Plugins load when that Work's Completion is Resolved.
	Start(h Host) []Work
	// Stop is synchronous and may do IO. It (1) runs `shutdown` handlers (total 200 ms for ALL
	// plugins, per-handler min(50 ms, remaining), no yield), (2) SIGKILLs live processes,
	// (3) flushes plugin state, (4) closes states. Idempotent.
	Stop()
	Wants(event string) bool
	Emit(e Event) // queue; never runs Lua
	// Veto runs before_file_save handlers. It must be called at InSlice()==false; when called
	// in a slice it returns (false, "") without running Lua and logs `veto skipped in slice`.
	Veto(e Event) (cancel bool, reason string)
	Owns(action string) bool
	// InSlice reports whether Lua of this runtime is on the Go stack right now.
	InSlice() bool
	// Run executes the first slice of a command task synchronously (macro replay keeps its
	// order) unless InSlice(); in a slice the request is queued FIFO and runs from Drain.
	Run(action string, args map[string]any, src CommandSource) error
	Resolve(c Completion) // in a slice: queued to the inbox, delivered by Drain
	// Drain runs queued tasks and events for at most budget; it returns new Work and whether
	// queued work remains (the caller then schedules another Drain with tea.Tick(0)).
	Drain(budget time.Duration) (work []Work, more bool)
	Plugins() []PluginInfo
	Log() []LogEntry
	// SettingsSchema returns the settings the plugin declared with gvim.settings.define,
	// sorted by key. ok is false when the plugin is not loaded or declared none.
	SettingsSchema(name string) ([]PluginSettingSpec, bool)
	// SettingsValues returns the effective value of each declared setting, in schema
	// order, with the config layer that supplied it.
	SettingsValues(name string) ([]PluginSettingValue, bool)
	// Control returns the Work it needs (re-read of sources, compile, state writes) and notices.
	// In a slice it only enqueues and returns (nil, nil).
	Control(c Control) ([]Work, []Notice)
}

// ---------- services implemented by internal/app, used by internal/plugin ----------

// DiffOptions tune Services.DiffText.
type DiffOptions struct{ IgnoreEOL bool } // normalise CRLF and lone CR to LF on both sides first

// FuzzyMatch is one fuzzy-ranked item.
type FuzzyMatch struct {
	Index   int // index into the items slice
	Score   int
	Indexes []int // matched rune indexes
}

// GrepRequest is a workspace text search.
type GrepRequest struct {
	Root, Query          string
	Regex, CaseSensitive bool
	Max                  int
}

// GrepHit is already converted by the implementation: Line 0-based, Col 0-based BYTE
// column into Text (live_grep.go yields 1-based line and rune column; the app converts).
type GrepHit struct {
	Path      string
	Line, Col int
	Text      string
}

// Services is passed in plugin.Options and implemented in internal/app. A nil
// Services makes every method CodeUnsupported (Width falls back to the rune count).
type Services interface {
	Width(s string) int                                                              // display cells; sync, O(len(s)), UI goroutine
	Truncate(s string, w int, ellipsis string) string                                // sync
	Diff(a, b []string) []Hunk                                                       // called inside a Work
	DiffText(a, b string, o DiffOptions) []Hunk                                      // called inside a Work
	Fuzzy(ctx context.Context, query string, items []string, limit int) []FuzzyMatch // inside a Work; stops early (nil) when ctx is done
	Grep(ctx context.Context, r GrepRequest) ([]GrepHit, error)                      // inside a Work
	Decode(encoding string, data []byte) (string, error)                             // sync, data <= 16 MiB (encoding)
	GitRoot(dir string) (string, bool)                                               // gitx.FindRoot; sync-cheap, called in a Work
	GitBranch(root string) (string, error)                                           // gitx.Branch;   called in a Work
	ValidateChord(chord string) error                                                // keymap.ValidateChord; sync, cheap; nil when chord can be bound
}
