// Package plugintest drives the plugin runtime in tests: a fake pluginapi.Host, a harness
// that plays the role of the ui (Start, Run, Emit, Settle, Advance) and deterministic
// fakes of the services. It is an ordinary package so tests of other packages can use it.
package plugintest

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/tbxark/gvim/internal/pluginapi"
)

// Call is one recorded Host method call.
type Call struct {
	Method string
	Args   []any
}

func (c Call) String() string { return fmt.Sprintf("%s%v", c.Method, c.Args) }

// Notice is one Host.Notify call.
type Notice struct {
	Plugin string
	Level  pluginapi.Level
	Msg    string
}

// Gone is one Host.OwnerGone call.
type Gone struct {
	Plugin string
	Gen    uint64
}

// FakeBuffer is an in-memory buffer of the fake host. Lines have no line terminators.
type FakeBuffer struct {
	Info  pluginapi.BufferInfo
	Lines []string
}

// FakeHost is a pluginapi.Host for tests. Methods the fake does not implement are
// pluginapi.Unsupported's. Every area adds its fake in a fake_<area>.go file: it keeps its
// state in Ext (one entry per area), registers the resources it tracks with
// RegisterResourceKind from init, and adds the methods it implements.
//
// A FakeHost is used from one goroutine (the test's); the harness never lets a Work
// goroutine touch it.
type FakeHost struct {
	pluginapi.Unsupported

	// Buffers by id, in tab order.
	Buffers []*FakeBuffer
	active  pluginapi.BufferID
	nextBuf pluginapi.BufferID

	// LinesDelay makes Lines sleep, to model a slow editor (heavy host call).
	LinesDelay time.Duration

	// Notices are the Notify calls in order.
	Notices []Notice
	// Gone are the OwnerGone calls in order.
	Gone []Gone

	// Registrations by plugin, as the ui would hold them.
	Commands   map[string][]pluginapi.CommandInfo
	Bindings   map[string][]pluginapi.Binding
	Modes      map[string][]pluginapi.ModeInfo
	Contexts   map[string]map[string]bool
	Status     map[string][]pluginapi.StatusItem
	InputMode  map[string]string
	Clipboard  string
	Screen     [2]int
	ActionList []string
	HoldSet    map[string]bool

	// Ext is per-area state for the fake_<area>.go files.
	Ext map[string]any

	calls []Call
}

// NewFakeHost returns an empty host with an 80x24 screen.
func NewFakeHost() *FakeHost {
	return &FakeHost{
		Commands:  map[string][]pluginapi.CommandInfo{},
		Bindings:  map[string][]pluginapi.Binding{},
		Modes:     map[string][]pluginapi.ModeInfo{},
		Contexts:  map[string]map[string]bool{},
		Status:    map[string][]pluginapi.StatusItem{},
		InputMode: map[string]string{},
		HoldSet:   map[string]bool{},
		Ext:       map[string]any{},
		Screen:    [2]int{80, 24},
	}
}

var _ pluginapi.Host = (*FakeHost)(nil)

func (f *FakeHost) rec(method string, args ...any) { f.calls = append(f.calls, Call{method, args}) }

// Calls returns the recorded calls of the methods the fake implements.
func (f *FakeHost) Calls() []Call { return append([]Call(nil), f.calls...) }

// CallCount counts the recorded calls of method.
func (f *FakeHost) CallCount(method string) int {
	n := 0
	for _, c := range f.calls {
		if c.Method == method {
			n++
		}
	}
	return n
}

// ---------- resources ----------

var resourceKinds []func(*FakeHost) []string

// RegisterResourceKind adds a function that lists the resources an area of the fake
// holds, as strings like "command:hello/hello.show". Call it from init.
func RegisterResourceKind(f func(*FakeHost) []string) { resourceKinds = append(resourceKinds, f) }

// Resources lists everything plugins own in the fake host, sorted: the union of all
// registered kinds. Unload must leave it as it was before the load.
func (f *FakeHost) Resources() []string {
	var out []string
	for _, k := range resourceKinds {
		out = append(out, k(f)...)
	}
	sort.Strings(out)
	return out
}

func init() {
	RegisterResourceKind(func(f *FakeHost) []string {
		var out []string
		for p, l := range f.Commands {
			for _, c := range l {
				out = append(out, "command:"+p+"/"+c.ID)
			}
		}
		for p, l := range f.Bindings {
			for _, b := range l {
				out = append(out, "binding:"+p+"/"+b.Chord)
			}
		}
		for p, l := range f.Modes {
			for _, m := range l {
				out = append(out, "mode:"+p+"/"+m.Name)
			}
		}
		for p, m := range f.Contexts {
			for name, on := range m {
				if on {
					out = append(out, "context:"+p+"/"+name)
				}
			}
		}
		for p, l := range f.Status {
			for _, s := range l {
				out = append(out, "status:"+p+"/"+s.ID)
			}
		}
		for p, m := range f.InputMode {
			if m != "" {
				out = append(out, "input_mode:"+p+"/"+m)
			}
		}
		return out
	})
}

// ---------- UIHost ----------

// Notify records the notice.
func (f *FakeHost) Notify(plugin string, level pluginapi.Level, msg string) {
	f.rec("Notify", plugin, level, msg)
	f.Notices = append(f.Notices, Notice{plugin, level, msg})
}

// SetCommands replaces the plugin's commands.
func (f *FakeHost) SetCommands(plugin string, cmds []pluginapi.CommandInfo) {
	f.rec("SetCommands", plugin, len(cmds))
	f.Commands[plugin] = append([]pluginapi.CommandInfo(nil), cmds...)
}

// SetBindings replaces the plugin's bindings.
func (f *FakeHost) SetBindings(plugin string, b []pluginapi.Binding) {
	f.rec("SetBindings", plugin, len(b))
	f.Bindings[plugin] = append([]pluginapi.Binding(nil), b...)
}

// SetModes replaces the plugin's modes.
func (f *FakeHost) SetModes(plugin string, m []pluginapi.ModeInfo) {
	f.rec("SetModes", plugin, len(m))
	f.Modes[plugin] = append([]pluginapi.ModeInfo(nil), m...)
}

// SetContext sets one plugin context.
func (f *FakeHost) SetContext(plugin, name string, on bool) {
	f.rec("SetContext", plugin, name, on)
	if f.Contexts[plugin] == nil {
		f.Contexts[plugin] = map[string]bool{}
	}
	f.Contexts[plugin][name] = on
}

// SetStatusItems replaces the plugin's status items.
func (f *FakeHost) SetStatusItems(plugin string, items []pluginapi.StatusItem) {
	f.rec("SetStatusItems", plugin, len(items))
	f.Status[plugin] = append([]pluginapi.StatusItem(nil), items...)
}

// SetInputMode sets (or with "" clears) the plugin's input mode. The editor has one input
// mode: entering one ends the others. A mode the plugin has not defined is not_found.
func (f *FakeHost) SetInputMode(plugin, mode string) error {
	f.rec("SetInputMode", plugin, mode)
	if mode != "" {
		found := false
		for _, m := range f.Modes[plugin] {
			found = found || m.Name == mode
		}
		if !found {
			return pluginapi.Errf(pluginapi.CodeNotFound, "mode %q is not defined", mode)
		}
		for p := range f.InputMode {
			f.InputMode[p] = ""
		}
	}
	f.InputMode[plugin] = mode
	return nil
}

// SetClipboard records the clipboard text.
func (f *FakeHost) SetClipboard(text string) {
	f.rec("SetClipboard", text)
	f.Clipboard = text
}

// ScreenSize returns Screen.
func (f *FakeHost) ScreenSize() (w, h int) { return f.Screen[0], f.Screen[1] }

// OwnerGone drops everything the plugin registered and records the call.
func (f *FakeHost) OwnerGone(plugin string, gen uint64) {
	f.rec("OwnerGone", plugin, gen)
	f.Gone = append(f.Gone, Gone{plugin, gen})
	delete(f.Commands, plugin)
	delete(f.Bindings, plugin)
	delete(f.Modes, plugin)
	delete(f.Contexts, plugin)
	delete(f.Status, plugin)
	delete(f.InputMode, plugin)
	for _, g := range ownerGoneHooks {
		g(f, plugin, gen)
	}
}

var ownerGoneHooks []func(f *FakeHost, plugin string, gen uint64)

// OnOwnerGone registers a function OwnerGone calls, so an area's fake drops what it holds
// for the plugin. Call it from init.
func OnOwnerGone(g func(f *FakeHost, plugin string, gen uint64)) {
	ownerGoneHooks = append(ownerGoneHooks, g)
}

// ---------- CommandHost ----------

// Holds reports whether the built-in context name is set in HoldSet, or is the mode context
// of an input mode a plugin entered.
func (f *FakeHost) Holds(name string) bool { return f.HoldSet[name] || f.modeHolds(name) }

// Actions returns ActionList.
func (f *FakeHost) Actions() []string { return append([]string(nil), f.ActionList...) }

// ---------- buffers (read side; the edit side is stage S6's fake_buf.go) ----------

// AddBuffer appends a buffer with the text to the tabs and activates it when it is the
// first. The language is guessed from the extension. It returns the id.
func (f *FakeHost) AddBuffer(path, text string) pluginapi.BufferID {
	f.nextBuf++
	id := f.nextBuf
	lines := strings.Split(text, "\n")
	name := path
	if i := strings.LastIndexByte(path, '/'); i >= 0 {
		name = path[i+1:]
	}
	b := &FakeBuffer{Lines: lines, Info: pluginapi.BufferInfo{
		ID: id, Path: path, Name: name, Encoding: "utf-8", LineEnding: "lf",
		LineCount: len(lines), Version: pluginapi.Version{Gen: 1},
	}}
	f.Buffers = append(f.Buffers, b)
	if f.active == 0 {
		f.active = id
	}
	f.refreshActive()
	return id
}

// RemoveBuffer closes a buffer (without any check) and activates a neighbour.
func (f *FakeHost) RemoveBuffer(id pluginapi.BufferID) {
	for i, b := range f.Buffers {
		if b.Info.ID == id {
			f.Buffers = append(f.Buffers[:i:i], f.Buffers[i+1:]...)
			break
		}
	}
	if f.active == id {
		f.active = 0
		if len(f.Buffers) > 0 {
			f.active = f.Buffers[0].Info.ID
		}
	}
	f.refreshActive()
}

// SetActive activates a buffer.
func (f *FakeHost) SetActive(id pluginapi.BufferID) {
	f.active = id
	f.refreshActive()
}

func (f *FakeHost) refreshActive() {
	for _, b := range f.Buffers {
		b.Info.Active = b.Info.ID == f.active
	}
}

func (f *FakeHost) find(id pluginapi.BufferID) *FakeBuffer {
	for _, b := range f.Buffers {
		if b.Info.ID == id {
			return b
		}
	}
	return nil
}

// ListBuffers returns the buffers in tab order.
func (f *FakeHost) ListBuffers() []pluginapi.BufferInfo {
	out := make([]pluginapi.BufferInfo, 0, len(f.Buffers))
	for _, b := range f.Buffers {
		out = append(out, b.Info)
	}
	return out
}

// Buffer returns the info of one buffer.
func (f *FakeHost) Buffer(id pluginapi.BufferID) (pluginapi.BufferInfo, bool) {
	if b := f.find(id); b != nil {
		return b.Info, true
	}
	return pluginapi.BufferInfo{}, false
}

// ActiveBuffer returns the active buffer id.
func (f *FakeHost) ActiveBuffer() pluginapi.BufferID { return f.active }

// FindBuffer returns the id of the buffer with the path, or 0.
func (f *FakeHost) FindBuffer(path string) pluginapi.BufferID {
	for _, b := range f.Buffers {
		if b.Info.Path == path {
			return b.Info.ID
		}
	}
	return 0
}

// Lines returns count lines from first (count < 0: to the end), after LinesDelay.
func (f *FakeHost) Lines(id pluginapi.BufferID, first, count int) ([]string, error) {
	f.rec("Lines", id, first, count)
	if f.LinesDelay > 0 {
		time.Sleep(f.LinesDelay)
	}
	b := f.find(id)
	if b == nil {
		return nil, pluginapi.Errf(pluginapi.CodeClosed, "buffer %d is closed", id)
	}
	if first < 0 {
		first = 0
	}
	if first > len(b.Lines) {
		first = len(b.Lines)
	}
	end := len(b.Lines)
	if count >= 0 && first+count < end {
		end = first + count
	}
	return append([]string(nil), b.Lines[first:end]...), nil
}
