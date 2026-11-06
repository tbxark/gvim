// Package plugin hosts user plugins written in JavaScript or TypeScript.
//
// Each plugin file runs in its own goja runtime and sees only the gvim global.
// TypeScript files are transpiled with esbuild before they run. Plugin JavaScript
// is interrupted once a call outlives the host timeout. A native Go call made from
// JavaScript, such as an Editor method, is not interrupted: the call returns when
// that Go code returns. Until then the plugin is busy, and further calls into it
// fail at once.
package plugin

import (
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/dop251/goja"
	"github.com/evanw/esbuild/pkg/api"
)

const (
	defaultTimeout    = 500 * time.Millisecond
	defaultLoadBudget = 2 * time.Second
)

// Editor is the set of editor operations a plugin may call through the gvim
// global. Its methods run on the goroutine that called Run or Load, while the
// calling plugin is busy. They must not call back into the Host.
type Editor interface {
	Notify(msg string)
	InsertText(text string) error
	ActivePath() string
	SetStatus(text string)
}

// Options configures a Host.
type Options struct {
	// Dir holds the plugin files. Only .js and .ts files directly in Dir are loaded.
	Dir    string
	Editor Editor
	// Timeout bounds each plugin call, and the top-level code of each file in Load.
	// Zero means 500 milliseconds.
	Timeout time.Duration
	// LoadBudget bounds the top-level code of all the files in Load together. Zero
	// means 2 seconds.
	LoadBudget time.Duration
}

// Command is a command registered by a plugin.
type Command struct {
	Name  string
	Label string
}

// Host loads plugin files and runs the commands they register.
type Host struct {
	dir        string
	editor     Editor
	timeout    time.Duration
	loadBudget time.Duration

	// mu guards commands and each plugin's busy flag. It is held only for short
	// reads and updates, never while plugin code runs, so Commands and Close never
	// wait for a running command.
	mu       sync.Mutex
	commands map[string]*command
}

// plugin is one loaded file and the runtime it runs in.
type plugin struct {
	file string
	vm   *goja.Runtime
	// errorCtor is the Error constructor as it was before plugin code ran, so a
	// plugin that replaces the global Error does not change what is thrown.
	errorCtor goja.Value
	// busy is set while a call (a command, or the file's top-level code) runs on
	// vm. Guarded by Host.mu.
	busy bool
}

type command struct {
	Command
	owner *plugin
	fn    goja.Callable
}

// New creates a Host. It does not read Dir; call Load for that.
func New(opts Options) (*Host, error) {
	if opts.Editor == nil {
		return nil, errors.New("plugin: Options.Editor is nil")
	}
	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	budget := opts.LoadBudget
	if budget <= 0 {
		budget = defaultLoadBudget
	}
	return &Host{
		dir:        opts.Dir,
		editor:     opts.Editor,
		timeout:    timeout,
		loadBudget: budget,
		commands:   map[string]*command{},
	}, nil
}

// Load runs every .js and .ts file directly in Dir and returns one error per
// file that failed. A failed file registers no commands; other files still
// load. The top-level code of all the files shares one load budget. A file that
// starts after the budget is spent is not run, and its error says so. Load is
// meant to be called once, before Run.
func (h *Host) Load() []error {
	entries, err := os.ReadDir(h.dir)
	if err != nil {
		return []error{fmt.Errorf("plugin: read dir: %w", err)}
	}
	deadline := time.Now().Add(h.loadBudget)
	var errs []error
	for _, e := range entries {
		if e.IsDir() || !isPluginFile(e.Name()) {
			continue
		}
		path := filepath.Join(h.dir, e.Name())
		left := time.Until(deadline)
		if left <= 0 {
			errs = append(errs, fmt.Errorf("plugin %s: not loaded, the load budget of %s ran out", path, h.loadBudget))
			continue
		}
		if err := h.loadFile(path, min(h.timeout, left)); err != nil {
			errs = append(errs, fmt.Errorf("plugin %s: %w", path, err))
		}
	}
	return errs
}

// Commands returns the registered commands sorted by name.
func (h *Host) Commands() []Command {
	h.mu.Lock()
	defer h.mu.Unlock()

	out := make([]Command, 0, len(h.commands))
	for _, c := range h.commands {
		out = append(out, c.Command)
	}
	slices.SortFunc(out, func(a, b Command) int { return strings.Compare(a.Name, b.Name) })
	return out
}

// Run calls the command registered under name. A JavaScript command is
// interrupted at the host timeout, and Run then returns a timed out error. A
// native Go call inside the command is not interrupted, so Run returns only when
// that call returns. Until then the plugin is busy, and a second Run into the
// same plugin returns a busy error at once.
func (h *Host) Run(name string) error {
	h.mu.Lock()
	c, ok := h.commands[name]
	h.mu.Unlock()
	if !ok {
		return fmt.Errorf("plugin: unknown command %q", name)
	}
	if !h.claim(c.owner) {
		return fmt.Errorf("plugin %s: command %q: busy, an earlier call into this plugin has not returned", c.owner.file, name)
	}
	defer h.release(c.owner)

	err := h.call(c.owner, h.timeout, func() error {
		_, err := c.fn(goja.Undefined())
		return err
	})
	if err != nil {
		return fmt.Errorf("plugin %s: command %q: %w", c.owner.file, name, err)
	}
	return nil
}

// Close drops all registered commands. It does not wait for a running command:
// that call finishes on its own, and its Run returns the result. A plugin
// runtime is released once no call refers to it.
func (h *Host) Close() {
	h.mu.Lock()
	defer h.mu.Unlock()

	clear(h.commands)
}

// claim marks p busy for one call. It reports false when p already is busy.
func (h *Host) claim(p *plugin) bool {
	h.mu.Lock()
	defer h.mu.Unlock()

	if p.busy {
		return false
	}
	p.busy = true
	return true
}

// release clears the busy mark that claim set.
func (h *Host) release(p *plugin) {
	h.mu.Lock()
	defer h.mu.Unlock()

	p.busy = false
}

func isPluginFile(name string) bool {
	return strings.HasSuffix(name, ".js") || strings.HasSuffix(name, ".ts")
}

// loadFile runs one plugin file, with its top-level code bounded by timeout. On
// failure it removes the commands the file registered before failing.
func (h *Host) loadFile(path string, timeout time.Duration) error {
	src, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	code := string(src)
	if strings.HasSuffix(path, ".ts") {
		if code, err = transpile(code); err != nil {
			return err
		}
	}

	// The file is busy until its top-level code returns, so a command it has
	// already registered cannot run on its runtime at the same time.
	p := &plugin{file: path, vm: goja.New(), busy: true}
	if err := h.installAPI(p); err != nil {
		return err
	}
	err = h.call(p, timeout, func() error {
		_, err := p.vm.RunScript(path, code)
		return err
	})
	h.release(p)
	if err != nil {
		h.mu.Lock()
		maps.DeleteFunc(h.commands, func(_ string, c *command) bool { return c.owner == p })
		h.mu.Unlock()
	}
	return err
}

// installAPI sets the gvim global, the only host object a plugin can reach.
func (h *Host) installAPI(p *plugin) error {
	p.errorCtor = p.vm.Get("Error")
	gv := p.vm.NewObject()
	fns := map[string]func(goja.FunctionCall) goja.Value{
		"registerCommand": func(c goja.FunctionCall) goja.Value { return h.registerCommand(p, c) },
		"notify": func(c goja.FunctionCall) goja.Value {
			h.editor.Notify(c.Argument(0).String())
			return goja.Undefined()
		},
		"insertText": func(c goja.FunctionCall) goja.Value {
			if err := h.editor.InsertText(c.Argument(0).String()); err != nil {
				p.throw(err.Error())
			}
			return goja.Undefined()
		},
		"activePath": func(goja.FunctionCall) goja.Value {
			return p.vm.ToValue(h.editor.ActivePath())
		},
		"setStatus": func(c goja.FunctionCall) goja.Value {
			h.editor.SetStatus(c.Argument(0).String())
			return goja.Undefined()
		},
	}
	for name, fn := range fns {
		if err := gv.Set(name, fn); err != nil {
			return err
		}
	}
	return p.vm.Set("gvim", gv)
}

// registerCommand implements gvim.registerCommand(name, label, fn). Invalid
// arguments and duplicate names throw into the calling plugin.
func (h *Host) registerCommand(p *plugin, c goja.FunctionCall) goja.Value {
	name, ok := c.Argument(0).Export().(string)
	if !ok || name == "" {
		panic(p.vm.NewTypeError("registerCommand: name must be a non-empty string"))
	}
	label, ok := c.Argument(1).Export().(string)
	if !ok {
		panic(p.vm.NewTypeError("registerCommand: label must be a string"))
	}
	fn, ok := goja.AssertFunction(c.Argument(2))
	if !ok {
		panic(p.vm.NewTypeError("registerCommand: fn must be a function"))
	}
	h.mu.Lock()
	_, dup := h.commands[name]
	if !dup {
		h.commands[name] = &command{Name: name, Label: label, owner: p, fn: fn}
	}
	h.mu.Unlock()
	if dup {
		p.throw(fmt.Sprintf("registerCommand: command %q is already registered", name))
	}
	return goja.Undefined()
}

// throw raises a JavaScript Error whose message is msg. Only the message crosses
// into JavaScript. The Go error that produced it is not attached, so plugin code
// cannot reach a Go value through the caught error.
func (p *plugin) throw(msg string) {
	e, err := p.vm.New(p.errorCtor, p.vm.ToValue(msg))
	if err != nil {
		e = p.vm.NewTypeError(msg)
	}
	panic(e)
}

// call runs fn on p's runtime. If fn outlives timeout, the runtime is interrupted
// and call reports a timeout instead of fn's error. A native Go call inside fn is
// not interrupted, so call returns only after fn does.
func (h *Host) call(p *plugin, timeout time.Duration, fn func() error) error {
	var (
		mu    sync.Mutex
		done  bool
		fired bool
	)
	timer := time.AfterFunc(timeout, func() {
		mu.Lock()
		defer mu.Unlock()
		if !done {
			fired = true
			p.vm.Interrupt("timeout")
		}
	})
	err := fn()
	timer.Stop()

	// Once done is set under mu, a late timer callback does nothing, so
	// ClearInterrupt below cannot race with an Interrupt.
	mu.Lock()
	done = true
	timedOut := fired
	mu.Unlock()
	p.vm.ClearInterrupt()

	if timedOut {
		return fmt.Errorf("timed out after %s", timeout)
	}
	return err
}

// transpile strips TypeScript types and lowers the source to ES2020.
func transpile(src string) (string, error) {
	res := api.Transform(src, api.TransformOptions{
		Loader: api.LoaderTS,
		Target: api.ES2020,
	})
	if len(res.Errors) > 0 {
		msgs := make([]string, 0, len(res.Errors))
		for _, m := range res.Errors {
			if m.Location != nil {
				msgs = append(msgs, fmt.Sprintf("line %d:%d: %s", m.Location.Line, m.Location.Column+1, m.Text))
			} else {
				msgs = append(msgs, m.Text)
			}
		}
		return "", errors.New(strings.Join(msgs, "; "))
	}
	return string(res.Code), nil
}
