package plugin

import (
	"errors"
	"slices"
	"sort"
	"strconv"
	"strings"

	lua "github.com/yuin/gopher-lua"

	"github.com/tbxark/gvim/internal/pluginapi"
)

// gvim.command: register, unregister, list, run, run_all, actions, palette, and the Command
// handle. Keys given to register are sugar for a binding of the new command (bindings.go).

const kindCommand = "Command"

// Limits of command registration and of run/run_all. Repeating an action runs in the editor,
// where no deadline can interrupt it, so the repeat counts are bounded here.
const (
	maxActionCount    = 10_000  // count of one action
	maxStepsCount     = 100_000 // sum of the counts of one run_all
	maxSteps          = 10_000  // steps of one run_all
	maxCommandTitle   = 200
	maxCommandDesc    = 1000
	maxCommandWhen    = 16
	maxWhenName       = 64
	maxCommandKeys    = 16
	maxActionName     = 128
	maxPaletteQuery   = 256
	maxListedCommands = 5000
)

// cmdHandle is the payload of a Command handle. It names the command by identity, so a
// handle of a command that was unregistered and registered again does nothing to the new one.
type cmdHandle struct {
	p   *plugin
	cmd *command
}

func (h *cmdHandle) handleString() string { return h.cmd.id }

// installCmd installs gvim.command.
func installCmd(e *apiEnv) {
	p, r := e.p, e.r
	area := e.area("command")

	methods := e.kind(kindCommand).methods
	e.set(methods, "unregister", func(L *lua.LState) int {
		h := checkObject(L, 1, kindCommand).(*cmdHandle)
		if h.p.dead || h.p.commands[h.cmd.name] != h.cmd {
			v, msg := errTuple(pluginapi.Errf(pluginapi.CodeNotFound, "command %q is not registered", h.cmd.name))
			L.Push(v)
			L.Push(msg)
			return 2
		}
		h.p.unregisterCommand(h.cmd.name)
		L.Push(lua.LTrue)
		return 1
	})

	e.set(area, "register", func(L *lua.LState) int {
		spec := L.CheckTable(1)
		c, keys, err := parseCommandSpec(L, p, spec)
		if err != nil {
			v, msg := errTuple(err)
			L.Push(v)
			L.Push(msg)
			return 2
		}
		if _, dup := p.commands[c.name]; dup {
			L.RaiseError("command %q is already registered", c.name)
		}
		bs := p.bindings()
		fresh := 0
		for _, k := range keys {
			if !bs.has(k) {
				fresh++
			}
		}
		if err := bs.checkRoom(fresh); err != nil {
			v, msg := errTuple(err)
			L.Push(v)
			L.Push(msg)
			return 2
		}
		if err := p.addCommand(c); err != nil {
			v, msg := errTuple(err)
			L.Push(v)
			L.Push(msg)
			return 2
		}
		for _, k := range keys {
			_ = bs.put(pluginapi.Binding{Chord: k, Action: c.id})
		}
		if len(keys) > 0 {
			bs.publish()
		}
		L.Push(p.newObject(kindCommand, &cmdHandle{p: p, cmd: c}))
		return 1
	})

	e.set(area, "unregister", func(L *lua.LState) int {
		name := L.CheckString(1)
		if _, ok := p.commands[name]; !ok || strings.HasPrefix(name, hiddenCommandPrefix) {
			v, msg := errTuple(pluginapi.Errf(pluginapi.CodeNotFound, "command %q is not registered", name))
			L.Push(v)
			L.Push(msg)
			return 2
		}
		p.unregisterCommand(name)
		L.Push(lua.LTrue)
		return 1
	})

	e.set(area, "list", func(L *lua.LState) int {
		f := checkListFilter(L, 1)
		out := L.NewTable()
		n := 0
		add := func(id, title, plugin string, builtin bool) {
			if n >= maxListedCommands || !f.match(id, title, plugin, builtin) {
				return
			}
			row := L.CreateTable(0, 4)
			row.RawSetString("id", lua.LString(id))
			row.RawSetString("title", lua.LString(title))
			row.RawSetString("plugin", lua.LString(plugin))
			row.RawSetString("builtin", lua.LBool(builtin))
			out.Append(row)
			n++
		}
		actions := append([]string(nil), r.guard.Actions()...)
		sort.Strings(actions)
		for _, a := range actions {
			add(a, a, "", true)
		}
		for _, q := range r.order {
			if q.dead || q.L == nil {
				continue
			}
			cmds := make([]*command, 0, len(q.commands))
			for _, c := range q.commands {
				if !c.info.Hidden {
					cmds = append(cmds, c)
				}
			}
			sort.Slice(cmds, func(i, j int) bool { return cmds[i].id < cmds[j].id })
			for _, c := range cmds {
				add(c.id, c.info.Title, q.name, false)
			}
		}
		L.Push(out)
		return 1
	})

	e.set(area, "run", func(L *lua.LState) int {
		action := checkActionName(L, 1)
		args, err := optArgs(L, p, 2)
		if err == nil {
			err = r.runAction(action, args)
		}
		if err != nil {
			v, msg := errTuple(err)
			L.Push(v)
			L.Push(msg)
			return 2
		}
		L.Push(lua.LTrue)
		return 1
	})

	e.set(area, "run_all", func(L *lua.LState) int {
		steps, err := checkSteps(L, p, 1)
		if err == nil && len(steps) > 0 {
			err = r.guard.DoAll(steps)
		}
		if err != nil {
			v, msg := errTuple(err)
			L.Push(v)
			L.Push(msg)
			return 2
		}
		L.Push(lua.LTrue)
		return 1
	})

	e.set(area, "actions", func(L *lua.LState) int {
		actions := append([]string(nil), r.guard.Actions()...)
		sort.Strings(actions)
		t := L.CreateTable(len(actions), 0)
		for _, a := range actions {
			t.Append(lua.LString(a))
		}
		L.Push(t)
		return 1
	})

	e.set(area, "palette", func(L *lua.LState) int {
		q := L.OptString(1, "")
		if len(q) > maxPaletteQuery {
			q = clipMsg(q, maxPaletteQuery)
		}
		r.guard.OpenPalette(q)
		L.Push(lua.LTrue)
		return 1
	})
}

// unregisterCommand removes a command and the key bindings that run it.
func (p *plugin) unregisterCommand(name string) {
	c := p.commands[name]
	if c == nil {
		return
	}
	p.removeCommand(name)
	p.bindings().dropAction(c.id)
}

// runAction is gvim.command.run: a plugin command starts as a new task (queued, because
// the caller is in a slice); anything else is an editor or ui action the host runs on the
// active document, count times. args.count is the repeat count for both.
func (r *Runtime) runAction(action string, args map[string]any) error {
	if c, _ := r.findCommand(action); c != nil {
		if _, _, err := splitCount(args); err != nil {
			return err
		}
		return r.Run(action, args, pluginapi.SrcAPI)
	}
	rest, count, err := splitCount(args)
	if err != nil {
		return err
	}
	return r.guard.Do(action, rest, count)
}

// splitCount takes args.count out of args (which is not modified) and validates it. A
// missing count is 1.
func splitCount(args map[string]any) (rest map[string]any, count int, err error) {
	v, ok := args["count"]
	if !ok {
		return args, 1, nil
	}
	n, whole := wholeNumber(v)
	switch {
	case !whole || n < 1:
		return nil, 0, pluginapi.Errf(pluginapi.CodeInvalid, "count must be a positive integer")
	case n > maxActionCount:
		return nil, 0, pluginapi.Errf(pluginapi.CodeLimit, "count too large (max %d)", maxActionCount)
	}
	rest = make(map[string]any, len(args))
	for k, x := range args {
		if k != "count" {
			rest[k] = x
		}
	}
	return rest, int(n), nil
}

// checkActionName reads an action id argument.
func checkActionName(L *lua.LState, n int) string {
	s := L.CheckString(n)
	if s == "" || len(s) > maxActionName {
		L.ArgError(n, "action name expected")
	}
	return s
}

// optArgs reads an optional args table (string keys). A table that cannot go to the host
// (functions, cycles) is an expected failure, not a raise.
func optArgs(L *lua.LState, p *plugin, n int) (map[string]any, error) {
	v := L.Get(n)
	if v == lua.LNil {
		return nil, nil
	}
	t, ok := v.(*lua.LTable)
	if !ok {
		L.ArgError(n, "table expected, got "+v.Type().String())
	}
	return argsFromTable(L, p, n, t)
}

func argsFromTable(L *lua.LState, p *plugin, n int, t *lua.LTable) (map[string]any, error) {
	x, err := p.fromLua(t)
	if err != nil {
		return nil, err
	}
	m, ok := x.(map[string]any)
	if !ok {
		L.ArgError(n, "table with string keys expected")
	}
	return m, nil
}

// checkSteps reads the steps argument of run_all: strings, {action=, args=, count=} tables
// and {action, args} arrays.
func checkSteps(L *lua.LState, p *plugin, n int) ([]pluginapi.Step, error) {
	t := L.CheckTable(n)
	count := t.Len()
	if count > maxSteps {
		return nil, pluginapi.Errf(pluginapi.CodeLimit, "too many steps (max %d)", maxSteps)
	}
	steps := make([]pluginapi.Step, 0, count)
	total := 0
	for i := 1; i <= count; i++ {
		var s pluginapi.Step
		argv := lua.LValue(lua.LNil)
		cv := lua.LValue(lua.LNil)
		switch v := t.RawGetInt(i).(type) {
		case lua.LString:
			s.Action = string(v)
		case *lua.LTable:
			if a, ok := v.RawGetString("action").(lua.LString); ok {
				s.Action, argv, cv = string(a), v.RawGetString("args"), v.RawGetString("count")
			} else if a, ok := v.RawGetInt(1).(lua.LString); ok {
				s.Action, argv = string(a), v.RawGetInt(2)
			} else {
				L.ArgError(n, "step "+strconv.Itoa(i)+": action name expected")
			}
		default:
			L.ArgError(n, "step "+strconv.Itoa(i)+": string or table expected, got "+v.Type().String())
		}
		if s.Action == "" || len(s.Action) > maxActionName {
			L.ArgError(n, "step "+strconv.Itoa(i)+": action name expected")
		}
		if argv != lua.LNil {
			at, ok := argv.(*lua.LTable)
			if !ok {
				L.ArgError(n, "step "+strconv.Itoa(i)+": args must be a table")
			}
			m, err := argsFromTable(L, p, n, at)
			if err != nil {
				return nil, err
			}
			s.Args = m
		}
		if cv != lua.LNil {
			if s.Args == nil {
				s.Args = map[string]any{}
			}
			f, ok := cv.(lua.LNumber)
			if !ok {
				L.ArgError(n, "step "+strconv.Itoa(i)+": count must be a number")
			}
			s.Args["count"] = float64(f)
		}
		rest, c, err := splitCount(s.Args)
		if err != nil {
			var pe *pluginapi.Error
			errors.As(err, &pe)
			return nil, pluginapi.Errf(pe.Code, "step %d: %s", i, pe.Msg)
		}
		s.Args, s.Count = rest, c
		if total += c; total > maxStepsCount {
			return nil, pluginapi.Errf(pluginapi.CodeLimit, "too many repeats in the steps (max %d)", maxStepsCount)
		}
		steps = append(steps, s)
	}
	return steps, nil
}

// listFilter is the optional argument of command.list.
type listFilter struct {
	query   string
	plugin  *string
	builtin *bool
}

func (f listFilter) match(id, title, plugin string, builtin bool) bool {
	if f.plugin != nil && *f.plugin != plugin {
		return false
	}
	if f.builtin != nil && *f.builtin != builtin {
		return false
	}
	if f.query == "" {
		return true
	}
	return strings.Contains(strings.ToLower(id), f.query) || strings.Contains(strings.ToLower(title), f.query)
}

// checkListFilter reads a query string or a {query=, plugin=, builtin=} table.
func checkListFilter(L *lua.LState, n int) listFilter {
	var f listFilter
	switch v := L.Get(n).(type) {
	case *lua.LNilType:
	case lua.LString:
		f.query = strings.ToLower(string(v))
	case *lua.LTable:
		if s, ok := v.RawGetString("query").(lua.LString); ok {
			f.query = strings.ToLower(string(s))
		}
		if s, ok := v.RawGetString("plugin").(lua.LString); ok {
			name := string(s)
			f.plugin = &name
		}
		if b, ok := v.RawGetString("builtin").(lua.LBool); ok {
			on := bool(b)
			f.builtin = &on
		}
	default:
		L.ArgError(n, "string or table expected, got "+v.Type().String())
	}
	return f
}

// parseCommandSpec validates the table given to command.register. A field of the wrong
// type raises; a value that is not acceptable is an expected failure.
func parseCommandSpec(L *lua.LState, p *plugin, spec *lua.LTable) (*command, []string, error) {
	field := func(name string, want lua.LValueType) lua.LValue {
		v := spec.RawGetString(name)
		if v != lua.LNil && v.Type() != want {
			L.RaiseError("gvim.command.register: field %q must be a %s, got %s", name, want, v.Type())
		}
		return v
	}
	nameV := field("name", lua.LTString)
	name, _ := nameV.(lua.LString)
	if nameV == lua.LNil {
		L.RaiseError("gvim.command.register: field %q is required", "name")
	}
	fn, _ := field("run", lua.LTFunction).(*lua.LFunction)
	if fn == nil {
		L.RaiseError("gvim.command.register: field %q must be a function", "run")
	}
	if !nameRE.MatchString(string(name)) {
		return nil, nil, pluginapi.Errf(pluginapi.CodeInvalid, "invalid command name %q (want [a-z][a-z0-9_]*, at most 40 characters)", clipMsg(string(name), 48))
	}
	title, _ := field("title", lua.LTString).(lua.LString)
	desc, _ := field("description", lua.LTString).(lua.LString)
	if len(title) > maxCommandTitle || len(desc) > maxCommandDesc {
		return nil, nil, pluginapi.Errf(pluginapi.CodeInvalid, "title or description is too long")
	}
	info := pluginapi.CommandInfo{
		ID: p.name + "." + string(name), Plugin: p.name, Record: true,
		Title: oneLine(string(title)), Description: oneLine(string(desc)),
	}
	if info.Title == "" {
		info.Title = string(name)
	}
	if b, ok := field("record", lua.LTBool).(lua.LBool); ok {
		info.Record = bool(b)
	}
	if b, ok := field("hidden", lua.LTBool).(lua.LBool); ok {
		info.Hidden = bool(b)
	}
	if t, ok := field("when", lua.LTTable).(*lua.LTable); ok {
		list, err := stringList(L, t, "when", maxCommandWhen)
		if err != nil {
			return nil, nil, err
		}
		for _, w := range list {
			if !validWhen(w) {
				return nil, nil, pluginapi.Errf(pluginapi.CodeInvalid, "invalid when condition %q", clipMsg(w, 48))
			}
		}
		info.When = list
	}
	var keys []string
	if t, ok := field("keys", lua.LTTable).(*lua.LTable); ok {
		list, err := stringList(L, t, "keys", maxCommandKeys)
		if err != nil {
			return nil, nil, err
		}
		for _, k := range list {
			chord, err := p.checkChord(k)
			if err != nil {
				return nil, nil, err
			}
			if !slices.ContainsFunc(keys, func(x string) bool { return bindingKey(x) == bindingKey(chord) }) {
				keys = append(keys, chord)
			}
		}
	}
	return &command{id: info.ID, name: string(name), fn: fn, info: info}, keys, nil
}

// stringList reads an array of strings with at most max entries.
func stringList(L *lua.LState, t *lua.LTable, what string, max int) ([]string, error) {
	n := t.Len()
	total := 0
	t.ForEach(func(lua.LValue, lua.LValue) { total++ })
	if total != n {
		L.RaiseError("gvim.command.register: field %q must be an array of strings", what)
	}
	if n > max {
		return nil, pluginapi.Errf(pluginapi.CodeInvalid, "%s has more than %d entries", what, max)
	}
	out := make([]string, 0, n)
	for i := 1; i <= n; i++ {
		s, ok := t.RawGetInt(i).(lua.LString)
		if !ok {
			L.RaiseError("gvim.command.register: field %q must be an array of strings", what)
		}
		out = append(out, string(s))
	}
	return out, nil
}

// validWhen accepts a context name with an optional leading "!": letters, digits and the
// punctuation of built-in names ("language:go", "read_only", "!large").
func validWhen(w string) bool {
	w = strings.TrimPrefix(w, "!")
	if w == "" || len(w) > maxWhenName {
		return false
	}
	for _, r := range w {
		ok := r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("_:.-+#", r)
		if !ok {
			return false
		}
	}
	return true
}

// oneLine makes text fit a single palette row: invalid UTF-8 and control characters become
// spaces.
func oneLine(s string) string {
	s = strings.ToValidUTF8(s, "�")
	return strings.TrimSpace(strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return ' '
		}
		return r
	}, s))
}
