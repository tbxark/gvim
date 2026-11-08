// Package authority is the editable workspace's backend slot: the one place that answers
// "where does this primitive run?" for file access, one-shot processes, long-running stdio
// children and the integrated terminal. Consumers ask the slot; they never branch on the
// backend kind (local today, SSH, containers later).
//
// The four routing surfaces are Host's contract. A slot also carries a trust gate and an
// environment provider, applied at the choke point before every launch, and connection
// state (label, connected, reconnect) for the status row. The local implementation wraps
// the existing plugin OS port, so switching the slot to Local changes no behaviour.
package authority

import (
	"context"
	"fmt"

	"github.com/tbxark/gvim/plugin/hostos"
)

// FileSystem is the file part of the slot. The value types are the ones the plugin OS
// port already uses, so the local adapter is a forwarding layer.
type FileSystem interface {
	ReadFile(ctx context.Context, path string, max int64) ([]byte, error)
	WriteFile(ctx context.Context, path string, data []byte, o hostos.WriteOptions) error
	Stat(ctx context.Context, path string) (hostos.FileStat, error)
	ReadDir(ctx context.Context, path string, hidden bool) ([]hostos.DirEntry, error)
	Watch(ctx context.Context, path string, o hostos.WatchOptions) (hostos.Watch, error)
}

// OneShot runs an argv to completion and collects its result.
type OneShot interface {
	Run(ctx context.Context, argv []string, o hostos.ProcOptions) (hostos.ProcResult, error)
}

// LongRunning starts a stdio child: the shape language servers and tool agents need.
type LongRunning interface {
	Start(ctx context.Context, argv []string, o hostos.ProcOptions) (hostos.Proc, error)
}

// Gate is the trust check run before every launch on every backend. A nil gate allows.
type Gate func(ctx context.Context, argv []string, cwd string) error

// GateEnv is Gate with the launch's effective environment, so the gate can resolve a
// bare command name through the PATH the child will actually inherit (the parent
// environment plus the project overlay plus the launch's own entries). The local
// backend uses it when set; the plain Gate remains for the backends that only know the
// argv and cwd (and for callers that do not need PATH resolution).
type GateEnv func(ctx context.Context, argv []string, cwd string, env map[string]string) error

// EnvProvider supplies environment entries ("K=V") for remote launches; nil gives none.
type EnvProvider func() []string

// TerminalOptions describes one integrated terminal.
type TerminalOptions struct {
	Dir        string   // starting directory; the wrapper may pin it
	Shell      string   // local shell override; ignored by a re-parenting wrapper
	Args       []string // shell arguments
	Env        []string // extra environment entries
	Cols, Rows int
}

// Backend is a terminal transport: a local PTY or a remote exec channel that requested a
// PTY. internal/terminal wraps one with StartWithBackend; nil means the caller starts a
// local PTY as usual.
type Backend interface {
	Read([]byte) (int, error)
	Write([]byte) (int, error)
	Resize(cols, rows int) error
	Kill() error
	Wait() error
	Close() error
}

// TerminalLaunch says how to start the integrated terminal through the slot.
type TerminalLaunch struct {
	Shell      string
	Args       []string
	Dir        string
	Env        []string
	ManagesCwd bool   // the wrapper pins Dir; the caller must not override it
	Label      string // human label of the backend ("SSH:user@host"), "" for local
	Backend    Backend
}

// Terminal is the terminal wrapper of the slot.
type Terminal interface {
	Launch(ctx context.Context, o TerminalOptions) (TerminalLaunch, error)
}

// RemoteState is the connection state of a non-local backend.
type RemoteState interface {
	Label() string
	Connected() bool
	Reconnect(ctx context.Context) error
	Close() error
}

// State is the snapshot the status row and reconnect-on-activate read.
type State struct {
	Kind      string // "local" or the backend's kind
	Label     string // "" for local
	Remote    *RemoteSpec
	Connected bool
	Reason    string // why it is disconnected, "" when connected
	// Connecting says a reconnect is dialing right now; the status row shows it.
	Connecting bool
}

// Parts are the four surfaces and the state of one mounted slot.
type Parts struct {
	Files       FileSystem
	OneShot     OneShot
	LongRunning LongRunning
	Terminal    Terminal
	Label       string
	Remote      RemoteState
	// RemoteSpec is the parsed target of a remote backend (nil for local).
	RemoteSpec *RemoteSpec
}

// Slot is what the app stores and the TUI reads: the four surfaces plus state.
type Slot interface {
	Files() FileSystem
	OneShot() OneShot
	LongRunning() LongRunning
	Terminal() Terminal
	Label() string
	State() State
	Connected() bool
	Reconnect(ctx context.Context) error
	Close() error
}

// Authority is one mounted backend slot.
type Authority struct {
	files  FileSystem
	one    OneShot
	long   LongRunning
	term   Terminal
	label  string
	remote RemoteState
	spec   *RemoteSpec
}

// New mounts a slot from parts. Nil parts refuse their operations with a clear error.
func New(p Parts) *Authority {
	a := &Authority{files: p.Files, one: p.OneShot, long: p.LongRunning, term: p.Terminal, label: p.Label, remote: p.Remote, spec: p.RemoteSpec}
	if a.files == nil {
		a.files = UnsupportedFiles{}
	}
	if a.one == nil {
		a.one = UnsupportedProcs{}
	}
	if a.long == nil {
		a.long = UnsupportedProcs{}
	}
	if a.term == nil {
		a.term = UnsupportedTerminal{}
	}
	return a
}

func (a *Authority) Files() FileSystem        { return a.files }
func (a *Authority) OneShot() OneShot         { return a.one }
func (a *Authority) LongRunning() LongRunning { return a.long }
func (a *Authority) Terminal() Terminal       { return a.term }
func (a *Authority) Label() string {
	if a.remote != nil && a.remote.Label() != "" {
		return a.remote.Label()
	}
	return a.label
}

// State is the slot's snapshot.
func (a *Authority) State() State {
	st := State{Kind: "local", Label: a.label, Remote: a.spec, Connected: true}
	if a.remote != nil {
		st.Kind, st.Connected = "remote", a.remote.Connected()
		st.Label = a.remote.Label()
		if r, ok := a.remote.(interface{ Reason() string }); ok {
			st.Reason = r.Reason()
		}
		if c, ok := a.remote.(interface{ Connecting() bool }); ok {
			st.Connecting = c.Connecting()
		}
	}
	return st
}

// Connected reports whether the backend is usable.
func (a *Authority) Connected() bool { return a.remote == nil || a.remote.Connected() }

// Reconnect re-establishes a dropped remote backend; a local slot does nothing.
func (a *Authority) Reconnect(ctx context.Context) error {
	if a.remote == nil {
		return nil
	}
	return a.remote.Reconnect(ctx)
}

// NeedsPassword reports whether a reconnect needs a password from the UI (the SSH
// backend after an auth failure). A local or healthy slot says false.
func (a *Authority) NeedsPassword() bool {
	if r, ok := a.remote.(interface{ NeedsPassword() bool }); ok {
		return r.NeedsPassword()
	}
	return false
}

// ReconnectWithPassword reconnects a remote slot with a UI-supplied password.
func (a *Authority) ReconnectWithPassword(ctx context.Context, password string) error {
	if r, ok := a.remote.(interface {
		ReconnectWithPassword(context.Context, string) error
	}); ok {
		return r.ReconnectWithPassword(ctx, password)
	}
	return a.Reconnect(ctx)
}

// Close releases the backend.
// Connecting reports whether a reconnect is dialing right now (false for local/healthy).
func (a *Authority) Connecting() bool {
	if r, ok := a.remote.(interface{ Connecting() bool }); ok {
		return r.Connecting()
	}
	return false
}

// NeedsPassphrase reports whether the last dial needs an encrypted identity's passphrase.
func (a *Authority) NeedsPassphrase() bool {
	if r, ok := a.remote.(interface{ NeedsPassphrase() bool }); ok {
		return r.NeedsPassphrase()
	}
	return false
}

// ReconnectWithPassphrase reconnects a remote slot with a UI-supplied key passphrase.
func (a *Authority) ReconnectWithPassphrase(ctx context.Context, passphrase string) error {
	if r, ok := a.remote.(interface {
		ReconnectWithPassphrase(context.Context, string) error
	}); ok {
		return r.ReconnectWithPassphrase(ctx, passphrase)
	}
	return a.Reconnect(ctx)
}

// NeedsKeyboardInteractive reports whether the last dial needs a 2FA challenge answered.
func (a *Authority) NeedsKeyboardInteractive() bool {
	if r, ok := a.remote.(interface{ NeedsKeyboardInteractive() bool }); ok {
		return r.NeedsKeyboardInteractive()
	}
	return false
}

// KeyboardInteractiveChallenge returns the pending 2FA challenge of a remote slot.
func (a *Authority) KeyboardInteractiveChallenge() (KeyboardPrompt, bool) {
	if r, ok := a.remote.(interface {
		KeyboardInteractiveChallenge() (KeyboardPrompt, bool)
	}); ok {
		return r.KeyboardInteractiveChallenge()
	}
	return KeyboardPrompt{}, false
}

// ReconnectWithAnswers reconnects a remote slot with the UI's 2FA answers.
func (a *Authority) ReconnectWithAnswers(ctx context.Context, answers []string) error {
	if r, ok := a.remote.(interface {
		ReconnectWithAnswers(context.Context, []string) error
	}); ok {
		return r.ReconnectWithAnswers(ctx, answers)
	}
	return a.Reconnect(ctx)
}

func (a *Authority) Close() error {
	if a.remote == nil {
		return nil
	}
	return a.remote.Close()
}

// ErrUnsupportedFileIO is the error a slot whose backend mounts no file surface returns:
// a slot always answers the FileSystem contract, and the SSH backend mounts the helper
// backend of ssh_files.go instead of refusing.
var ErrUnsupportedFileIO = fmt.Errorf("authority: this authority mounts no file surface")

// UnsupportedFiles refuses every file operation with ErrUnsupportedFileIO.
type UnsupportedFiles struct{}

func (UnsupportedFiles) ReadFile(context.Context, string, int64) ([]byte, error) {
	return nil, ErrUnsupportedFileIO
}
func (UnsupportedFiles) WriteFile(context.Context, string, []byte, hostos.WriteOptions) error {
	return ErrUnsupportedFileIO
}
func (UnsupportedFiles) Stat(context.Context, string) (hostos.FileStat, error) {
	return hostos.FileStat{}, ErrUnsupportedFileIO
}
func (UnsupportedFiles) ReadDir(context.Context, string, bool) ([]hostos.DirEntry, error) {
	return nil, ErrUnsupportedFileIO
}
func (UnsupportedFiles) Watch(context.Context, string, hostos.WatchOptions) (hostos.Watch, error) {
	return nil, ErrUnsupportedFileIO
}

// UnsupportedProcs refuses process operations (a slot without a process backend).
type UnsupportedProcs struct{}

func (UnsupportedProcs) Run(context.Context, []string, hostos.ProcOptions) (hostos.ProcResult, error) {
	return hostos.ProcResult{}, ErrUnsupportedFileIO
}
func (UnsupportedProcs) Start(context.Context, []string, hostos.ProcOptions) (hostos.Proc, error) {
	return nil, ErrUnsupportedFileIO
}

// UnsupportedTerminal refuses terminal launches.
type UnsupportedTerminal struct{}

func (UnsupportedTerminal) Launch(context.Context, TerminalOptions) (TerminalLaunch, error) {
	return TerminalLaunch{}, ErrUnsupportedFileIO
}
