package plugin

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"time"

	lua "github.com/yuin/gopher-lua"

	"github.com/tbxark/gvim/plugin/hostos"
	"github.com/tbxark/gvim/pluginapi"
)

// gvim.proc and gvim.env. proc.run waits for the process in a
// Work. proc.spawn returns a Process at once: starting the child happens in a Work, its
// output and exit come back as stream items that run the callbacks as event-class tasks.

const (
	kindProcess = "Process"

	defaultRunTimeout = 60 * time.Second
	maxRunTimeout     = 600 * time.Second
	maxProcOutput     = 8 << 20
	maxArgv           = 1024
	maxArgvBytes      = 1 << 20
	maxEnvEntries     = 256
	procWriteQueue    = 8 << 20 // bytes queued for stdin before the process started
	procKillGrace     = 2 * time.Second
	maxLineBuffer     = 1 << 20 // a line longer than this is delivered in pieces
)

func init() {
	registerAwait("proc_run", awaitProcRun)
	registerIO("proc_which", prepProcWhich)
	registerAwait("proc_wait", awaitProcWait)
	registerAwait("proc_wait_started", awaitProcWaitStarted)
}

// procSpec is a validated proc.run / proc.spawn argument list.
type procSpec struct {
	argv     []string
	po       hostos.ProcOptions
	onStart  *lua.LFunction
	onStdout *lua.LFunction
	onStderr *lua.LFunction
	onExit   *lua.LFunction
	lines    bool
	stdin    bool
}

// parseProc validates argv (args[0]) and options (args[1]). The text it returns is a
// bad-argument message ("" when fine).
func parseProc(args []lua.LValue, spawn bool) (procSpec, int, string) {
	var s procSpec
	t, ok := argTable(args, 0)
	if !ok {
		return s, 1, "table (argv) expected"
	}
	total := 0
	for i := 1; i <= t.Len(); i++ {
		a, ok := stringArg([]lua.LValue{t.RawGetInt(i)}, 0)
		if !ok {
			return s, 1, "argv must be a list of strings"
		}
		if strings.ContainsRune(a, 0) {
			return s, 1, "argv contains a NUL byte"
		}
		total += len(a)
		if i > maxArgv || total > maxArgvBytes {
			return s, 1, "argv is too long"
		}
		s.argv = append(s.argv, a)
	}
	if len(s.argv) == 0 || s.argv[0] == "" {
		return s, 1, "argv must name a program"
	}
	o := optionsArg(args, 1)
	if o.bad != "" {
		return s, 2, o.bad
	}
	s.po.Cwd = o.str("cwd")
	if s.po.Cwd != "" {
		if err := checkPathText(s.po.Cwd); err != nil {
			return s, 2, "cwd: " + err.Error()
		}
	}
	if env, ok := o.get("env").(*lua.LTable); ok {
		s.po.Env = map[string]string{}
		var bad string
		env.ForEach(func(k, v lua.LValue) {
			ks, kok := k.(lua.LString)
			vs, vok := stringArg([]lua.LValue{v}, 0)
			switch {
			case !kok || !vok:
				bad = "env must map strings to strings"
			case ks == "" || strings.ContainsAny(string(ks), "=\x00") || strings.ContainsRune(vs, 0):
				bad = "env has an invalid name or value"
			case len(s.po.Env) >= maxEnvEntries:
				bad = "env has too many entries"
			default:
				s.po.Env[string(ks)] = vs
			}
		})
		if bad != "" {
			return s, 2, bad
		}
	} else if o.get("env") != lua.LNil {
		o.fail("option env must be a table")
	}
	switch v := o.get("stdin").(type) {
	case lua.LString:
		if len(v) > MaxString {
			return s, 2, "stdin is too large"
		}
		s.po.Stdin = []byte(v)
	case lua.LBool:
		if !spawn && bool(v) {
			return s, 2, "option stdin must be a string"
		}
		s.po.OpenStdin = bool(v)
		s.stdin = bool(v)
	case *lua.LNilType:
	default:
		o.fail("option stdin must be a string or boolean")
	}
	def := float64(0)
	if !spawn {
		def = float64(defaultRunTimeout.Milliseconds())
	}
	ms := o.number("timeout_ms", def, 0, float64(maxRunTimeout.Milliseconds()))
	if ms <= 0 && !spawn { // run always has a timeout: zero or negative means the default
		ms = def
	}
	if ms > 0 {
		s.po.Timeout = time.Duration(max(ms, 1)) * time.Millisecond
	}
	s.po.MaxOutput = int(o.number("max_output", maxProcOutput, 1, maxProcOutput))
	s.po.MergeStderr = o.boolean("merge_stderr", false)
	if spawn {
		for name, dst := range map[string]**lua.LFunction{"on_start": &s.onStart, "on_stdout": &s.onStdout, "on_stderr": &s.onStderr, "on_exit": &s.onExit} {
			switch v := o.get(name).(type) {
			case *lua.LFunction:
				*dst = v
			case *lua.LNilType:
			default:
				o.fail("option %s must be a function", name)
			}
		}
		s.lines = o.boolean("lines", false)
	}
	if o.bad != "" {
		return s, 2, o.bad
	}
	return s, 0, ""
}

// argTable returns args[i] as a table.
func argTable(args []lua.LValue, i int) (*lua.LTable, bool) {
	if i >= len(args) {
		return nil, false
	}
	t, ok := args[i].(*lua.LTable)
	return t, ok
}

// ---------- proc.run ----------

func awaitProcRun(c *awaitCall) {
	spec, n, bad := parseProc(c.args, false)
	if bad != "" {
		c.Bad(n, "run", bad)
		return
	}
	if err := c.CheckPark(true); err != nil {
		c.Fail(err)
		return
	}
	o := c.r.os
	c.Park(&parkSpec{Work: &pluginapi.Work{Run: func(ctx context.Context) pluginapi.Completion {
		res, err := o.Run(ctx, spec.argv, spec.po)
		if err != nil {
			return pluginapi.Completion{Err: errString(err)}
		}
		return pluginapi.Completion{Values: []any{runResult(res)}}
	}}})
}

// runResult is the Result table of proc.run.
func runResult(r hostos.ProcResult) map[string]any {
	m := exitResult(r)
	m["stdout"], m["stderr"] = string(r.Stdout), string(r.Stderr)
	m["truncated"] = r.Truncated
	return m
}

// exitResult is the part of a result a spawned process has too.
func exitResult(r hostos.ProcResult) map[string]any {
	m := map[string]any{"code": int64(r.Code), "ok": r.Code == 0 && r.Signal == "" && !r.TimedOut, "timed_out": r.TimedOut}
	if r.Signal != "" {
		m["signal"] = r.Signal
	}
	return m
}

func prepProcWhich(c *awaitCall) ioJob {
	name, ok := stringArg(c.args, 0)
	if !ok || name == "" || strings.ContainsRune(name, 0) {
		c.Bad(1, "which", "program name expected")
		return nil
	}
	return func(_ context.Context, o hostos.OS) ([]any, error) {
		p, found := o.LookPath(name)
		if !found {
			return []any{nil}, nil
		}
		return []any{p}, nil
	}
}

// ---------- Process ----------

// process is the Process handle.
type process struct {
	p    *plugin
	id   int
	spec procSpec
	live *int

	osp     hostos.Proc // set by the "start" item
	pid     int
	started bool
	exited  bool
	err     string         // the process could not start (or be waited for): "<code>: <msg>"
	result  map[string]any // the exit result when it ran

	// requests made before the child started, applied when it does
	queued     [][]byte
	queuedN    int
	closeStdin bool
	killSig    string
	stdinOff   bool // close_stdin was called

	waiters  []pluginapi.Token // Process:wait
	swaiters []pluginapi.Token // Process:wait_started
	partial  [2][]byte         // lines mode: the unfinished line of stdout and stderr
	dropped  int

	cancel context.CancelFunc
	w      *wait
}

func (pr *process) handleString() string {
	if pr.started {
		return fmt.Sprint(pr.pid)
	}
	return fmt.Sprintf("#%d", pr.id)
}

// terminate is the unload policy for a process that is still alive.
func (pr *process) terminate() {
	if pr.cancel != nil {
		pr.cancel()
	}
	if pr.osp != nil && !pr.exited {
		pr.osp.Terminate(procKillGrace)
	}
	pr.release()
}

// release forgets the process: it no longer counts against the quota or the stop hook.
func (pr *process) release() {
	if pr.live != nil {
		*pr.live--
		pr.live = nil
		delete(pr.p.r.io().procs, pr)
	}
}

func (pr *process) write(data string) error {
	switch {
	case pr.exited:
		return pluginapi.Errf(pluginapi.CodeClosed, "process has exited")
	case !pr.spec.stdin:
		return pluginapi.Errf(pluginapi.CodeClosed, "process was spawned without stdin = true")
	case pr.stdinOff:
		return pluginapi.Errf(pluginapi.CodeClosed, "stdin is closed")
	case pr.osp != nil:
		return pr.osp.Write([]byte(data))
	case pr.queuedN+len(data) > procWriteQueue:
		return pluginapi.Errf(pluginapi.CodeBusy, "stdin buffer full")
	}
	pr.queued = append(pr.queued, []byte(data))
	pr.queuedN += len(data)
	return nil
}

func (pr *process) kill(sig string) error {
	if pr.exited {
		return pluginapi.Errf(pluginapi.CodeClosed, "process has exited")
	}
	if pr.osp == nil {
		if pr.killSig != "KILL" {
			pr.killSig = sig
		}
		return nil
	}
	return pr.osp.Kill(sig)
}

// callback queues fn(arg) as an event-class task.
func (pr *process) callback(fn *lua.LFunction, label string, arg lua.LValue) {
	if fn == nil {
		return
	}
	if _, err := pr.p.timerTask(label, fn, arg); err != nil {
		if pr.dropped++; pr.dropped == 1 {
			pr.p.r.logf(pr.p.name, levelWarn, "process output dropped: the plugin's callbacks are too slow ("+err.Error()+")")
		}
	}
}

// output delivers a chunk to the callback of its stream; in lines mode one call per line.
func (pr *process) output(stderr bool, data string) {
	i := 0
	fn, label := pr.spec.onStdout, "process stdout"
	if stderr {
		i, fn, label = 1, pr.spec.onStderr, "process stderr"
	}
	if fn == nil {
		return
	}
	if !pr.spec.lines {
		pr.callback(fn, label, lua.LString(data))
		return
	}
	buf := append(pr.partial[i], data...)
	for {
		nl := bytes.IndexByte(buf, '\n')
		if nl < 0 {
			break
		}
		line := strings.TrimSuffix(string(buf[:nl]), "\r")
		pr.callback(fn, label, lua.LString(line))
		buf = buf[nl+1:]
	}
	for len(buf) > maxLineBuffer {
		pr.callback(fn, label, lua.LString(buf[:maxLineBuffer]))
		buf = buf[maxLineBuffer:]
	}
	pr.partial[i] = append([]byte(nil), buf...)
}

// onItem handles one item of the process stream on the UI goroutine.
func (pr *process) onItem(c pluginapi.Completion) {
	p := pr.p
	switch c.Kind {
	case "start":
		osp, _ := c.Values[1].(hostos.Proc)
		pid, _ := wholeNumber(c.Values[0])
		pr.osp, pr.pid, pr.started = osp, int(pid), true
		for _, d := range pr.queued {
			_ = osp.Write(d)
		}
		pr.queued, pr.queuedN = nil, 0
		if pr.closeStdin {
			osp.CloseStdin()
		}
		if pr.killSig != "" {
			_ = osp.Kill(pr.killSig)
		}
		pr.callback(pr.spec.onStart, "process start", lua.LNumber(pr.pid))
		p.wake(&pr.swaiters)
	case "out":
		stderr, _ := c.Values[0].(bool)
		data, _ := c.Values[1].(string)
		pr.output(stderr, data)
	case "exit":
		m, _ := c.Values[0].(map[string]any)
		pr.exited = true
		if e, _ := m["err"].(string); e != "" {
			pr.err = e
		} else {
			pr.result = m
		}
		for i, fn := range []*lua.LFunction{pr.spec.onStdout, pr.spec.onStderr} {
			if len(pr.partial[i]) > 0 && fn != nil {
				pr.callback(fn, "process output", lua.LString(pr.partial[i]))
			}
			pr.partial[i] = nil
		}
		pr.release()
		pr.cancel()
		pr.callback(pr.spec.onExit, "process exit", pr.exitTable())
		p.wake(&pr.swaiters)
		p.wake(&pr.waiters)
	}
}

// wake resolves the parked waits in list.
func (p *plugin) wake(list *[]pluginapi.Token) {
	for _, tok := range *list {
		p.r.inbox = append(p.r.inbox, pluginapi.Completion{Token: tok})
	}
	*list = nil
}

// exitTable is the argument of on_exit and the result of Process:wait.
func (pr *process) exitTable() lua.LValue {
	if pr.err != "" {
		return pr.p.toLua(map[string]any{"err": pr.err})
	}
	return pr.p.toLua(pr.result)
}

// waitValues are the resume arguments of Process:wait.
func (pr *process) waitValues() []lua.LValue {
	if pr.err != "" {
		return []lua.LValue{lua.LTrue, lua.LNil, lua.LString(pr.err)}
	}
	return []lua.LValue{lua.LTrue, pr.exitTable()}
}

// startedValues are the resume arguments of Process:wait_started.
func (pr *process) startedValues() []lua.LValue {
	if pr.err != "" {
		return []lua.LValue{lua.LTrue, lua.LNil, lua.LString(pr.err)}
	}
	return []lua.LValue{lua.LTrue, lua.LNumber(pr.pid)}
}

// procStream is the Work of a spawned process: start it, hand out its output, then its
// exit. Every item is returned with ok = true; the exit item has More unset. A cancelled
// ctx (unload, Process handle gone) terminates the child.
func procStream(o hostos.OS, spec procSpec) func(context.Context) (pluginapi.Completion, bool) {
	var proc hostos.Proc
	state := 0
	exit := func(m map[string]any) (pluginapi.Completion, bool) {
		state = 2
		return pluginapi.Completion{Kind: "exit", Values: []any{m}}, true
	}
	cancelled := func() (pluginapi.Completion, bool) {
		if proc != nil {
			proc.Terminate(procKillGrace)
		}
		return exit(map[string]any{"err": string(pluginapi.CodeCancelled) + ": process cancelled"})
	}
	return func(ctx context.Context) (pluginapi.Completion, bool) {
		switch {
		case state == 2:
			return pluginapi.Completion{Kind: "exit", Values: []any{map[string]any{"err": "closed: process stream ended"}}}, true
		case ctx.Err() != nil:
			return cancelled()
		case state == 0:
			state = 1
			pr, err := o.Start(ctx, spec.argv, spec.po)
			if err != nil {
				return exit(map[string]any{"err": errString(err)})
			}
			proc = pr
			return pluginapi.Completion{Kind: "start", More: true, Values: []any{int64(pr.PID()), pr}}, true
		}
		if ch, more := proc.Output(ctx); more {
			return pluginapi.Completion{Kind: "out", More: true, Values: []any{ch.Stderr, string(ch.Data)}}, true
		}
		if ctx.Err() != nil {
			return cancelled()
		}
		res, err := proc.Wait(ctx)
		if err != nil {
			return exit(map[string]any{"err": errString(err)})
		}
		return exit(exitResult(res))
	}
}

func checkSignal(s string) (string, bool) {
	switch strings.ToUpper(strings.TrimPrefix(strings.ToUpper(s), "SIG")) {
	case "", "TERM":
		return "TERM", true
	case "KILL":
		return "KILL", true
	case "INT":
		return "INT", true
	case "HUP":
		return "HUP", true
	}
	return "", false
}

// processArg returns the Process handle at args[0] for the await handlers.
func processArg(c *awaitCall, fname string) *process {
	if len(c.args) > 0 {
		if ud, ok := c.args[0].(*lua.LUserData); ok {
			if o, ok := ud.Value.(*object); ok && o.kind == kindProcess {
				if pr, ok := o.value.(*process); ok && pr.p == c.p {
					return pr
				}
			}
		}
	}
	c.Bad(1, fname, "Process expected")
	return nil
}

func awaitProcWait(c *awaitCall) {
	pr := processArg(c, "wait")
	if pr == nil {
		return
	}
	if pr.exited {
		parkNow(c, pr.waitValues())
		return
	}
	parkOn(c, &pr.waiters, pr.waitValues)
}

func awaitProcWaitStarted(c *awaitCall) {
	pr := processArg(c, "wait_started")
	if pr == nil {
		return
	}
	if pr.started || pr.exited {
		parkNow(c, pr.startedValues())
		return
	}
	parkOn(c, &pr.swaiters, pr.startedValues)
}

// parkNow completes the call at once with resume arguments (ok flag first).
func parkNow(c *awaitCall, vals []lua.LValue) {
	c.Return(vals[1:]...)
}

// parkOn parks the task until the list is woken; convert makes the result then.
func parkOn(c *awaitCall, list *[]pluginapi.Token, convert func() []lua.LValue) {
	if err := c.CheckPark(false); err != nil {
		c.Fail(err)
		return
	}
	tok := c.NewToken()
	*list = append(*list, tok)
	c.Park(&parkSpec{
		Token:   tok,
		Convert: func(pluginapi.Completion) []lua.LValue { return convert() },
		Cancel: func() {
			for i, x := range *list {
				if x == tok {
					*list = append((*list)[:i:i], (*list)[i+1:]...)
					return
				}
			}
		},
	})
}

func installProc(e *apiEnv) {
	p, r := e.p, e.r
	area := e.area("proc")
	env := e.area("env")

	e.set(env, "get", func(L *lua.LState) int {
		name := L.CheckString(1)
		if v, ok := r.os.Getenv(name); ok {
			L.Push(lua.LString(v))
		} else {
			L.Push(lua.LNil)
		}
		return 1
	})

	// The project environment API (detection, trust level, activation). The host
	// owns detection, the trust gate and spawn application; these functions are
	// the seam. gvim.env.set never bypasses the gate: an untrusted workspace gets
	// nil, "invalid: ..." from the host.
	envInfo := func(L *lua.LState, info pluginapi.EnvInfo) *lua.LTable {
		t := L.NewTable()
		t.RawSetString("name", lua.LString(info.Name))
		t.RawSetString("kind", lua.LString(info.Kind))
		t.RawSetString("snippet", lua.LString(info.Snippet))
		return t
	}
	e.set(env, "detected", func(L *lua.LState) int {
		if info, ok := r.host.DetectedEnv(); ok {
			L.Push(envInfo(L, info))
		} else {
			L.Push(lua.LNil)
		}
		return 1
	})
	e.set(env, "active", func(L *lua.LState) int {
		if info, ok := r.host.ActiveEnv(); ok {
			L.Push(envInfo(L, info))
		} else {
			L.Push(lua.LNil)
		}
		return 1
	})
	e.set(env, "set", func(L *lua.LState) int {
		snippet := L.CheckString(1)
		dir, kind := "", "shell"
		if L.GetTop() >= 2 && L.Get(2) != lua.LNil {
			opts, ok := L.Get(2).(*lua.LTable)
			if !ok {
				L.Push(lua.LNil)
				L.Push(lua.LString("invalid: gvim.env.set options must be a table"))
				return 2
			}
			if v := opts.RawGetString("dir"); v != lua.LNil {
				text, ok := v.(lua.LString)
				if !ok {
					L.Push(lua.LNil)
					L.Push(lua.LString("invalid: opts.dir must be a string"))
					return 2
				}
				dir = string(text)
			}
			if v := opts.RawGetString("kind"); v != lua.LNil {
				text, ok := v.(lua.LString)
				if !ok {
					L.Push(lua.LNil)
					L.Push(lua.LString("invalid: opts.kind must be a string"))
					return 2
				}
				kind = string(text)
			}
		}
		return bufTrue(L, r.host.SetEnv(p.name, snippet, dir, kind))
	})
	e.set(env, "clear", func(L *lua.LState) int {
		return bufTrue(L, r.host.ClearEnv(p.name))
	})
	e.set(env, "trust_level", func(L *lua.LState) int {
		L.Push(lua.LString(r.host.EnvTrustLevel()))
		return 1
	})

	area.RawSetString("run", e.async("proc_run"))
	area.RawSetString("which", e.async("proc_which"))

	k := e.kind(kindProcess)
	methods := k.methods
	live, seq := 0, 0

	// Process.pid is nil until the child has started.
	k.mt.RawSetString("__index", e.fn(func(L *lua.LState) int {
		ud := L.CheckUserData(1)
		key, _ := L.Get(2).(lua.LString)
		if key == "pid" {
			if o, ok := ud.Value.(*object); ok {
				if pr, ok := o.value.(*process); ok && pr.started {
					L.Push(lua.LNumber(pr.pid))
					return 1
				}
			}
			L.Push(lua.LNil)
			return 1
		}
		L.Push(methods.RawGetString(string(key)))
		return 1
	}))
	self := func(L *lua.LState) *process { return checkObject(L, 1, kindProcess).(*process) }
	methods.RawSetString("wait", e.async("proc_wait"))
	methods.RawSetString("wait_started", e.async("proc_wait_started"))
	e.set(methods, "write", func(L *lua.LState) int {
		pr := self(L)
		data, ok := stringArg(argsFrom(L, 1), 1)
		if !ok {
			L.ArgError(2, "string expected")
		}
		if err := pr.write(data); err != nil {
			return bufRet(L, err)
		}
		L.Push(lua.LTrue)
		return 1
	})
	e.set(methods, "close_stdin", func(L *lua.LState) int {
		pr := self(L)
		switch {
		case pr.exited:
		case !pr.spec.stdin:
		case pr.osp != nil:
			pr.stdinOff = true
			pr.osp.CloseStdin()
		default:
			pr.stdinOff, pr.closeStdin = true, true
		}
		L.Push(lua.LTrue)
		return 1
	})
	e.set(methods, "kill", func(L *lua.LState) int {
		pr := self(L)
		sig, ok := checkSignal(L.OptString(2, ""))
		if !ok {
			L.ArgError(2, `signal must be "TERM", "KILL", "INT" or "HUP"`)
		}
		if err := pr.kill(sig); err != nil {
			return bufRet(L, err)
		}
		L.Push(lua.LTrue)
		return 1
	})
	e.set(methods, "is_running", func(L *lua.LState) int {
		L.Push(lua.LBool(!self(L).exited))
		return 1
	})

	e.set(area, "spawn", func(L *lua.LState) int {
		spec, n, bad := parseProc(argsFrom(L, 1), true)
		if bad != "" {
			L.ArgError(n, bad)
		}
		if live >= QuotaProcesses {
			return bufRet(L, pluginapi.Errf(pluginapi.CodeLimit, "process quota (%d)", QuotaProcesses))
		}
		live++
		seq++
		wctx, cancel := context.WithCancel(p.pctx)
		pr := &process{p: p, id: seq, spec: spec, live: &live, cancel: cancel}
		r.io().procs[pr] = struct{}{}
		tok, w := p.newWait(pr.onItem, pr.terminate)
		pr.w = w
		p.queueWork(tok, pluginapi.Work{Ctx: wctx, Stream: procStream(r.os, spec)})
		L.Push(p.newObject(kindProcess, pr))
		return 1
	})
}
