package plugin

import (
	"fmt"
	"strconv"
	"time"

	lua "github.com/yuin/gopher-lua"

	"github.com/tbxark/gvim/pluginapi"
)

// gvim.async: spawn, all, suspend, and the Task handle. The primitives live here because
// the task and yield protocol belongs to the runtime core. suspend and Task:wait take an
// optional timeout_ms: a parked task that is not woken in time resumes
// with nil, "timeout: ...".

func init() {
	registerAwaitV("suspend", awaitSuspend)
	registerAwaitV("task_wait", awaitTaskWait)
}

// suspender is the rendezvous behind gvim.async.suspend: resume() fills it, the awaiting
// task takes the values.
type suspender struct {
	done     bool
	vals     []lua.LValue
	tok      pluginapi.Token
	parked   bool
	timedOut bool
	stop     func() // abandons the timeout timer
}

// suspenders of one plugin, by id. They live on the plugin so the await handler can find them.
type suspendTable struct {
	next int
	m    map[int]*suspender
}

func (t *task) handleString() string { return strconv.FormatUint(t.id, 10) }

func installAsync(e *apiEnv) {
	p, r := e.p, e.r
	area := e.area("async")
	susp := &suspendTable{m: map[int]*suspender{}}
	p.susp = susp

	spawn := e.fn(func(L *lua.LState) int {
		fn := L.CheckFunction(1)
		o := originCallback
		if ct := r.curTask; ct != nil && ct.p == p && ct.origin != originLoad {
			o = ct.origin
		}
		t, err := r.newTask(p, o, fn, "task")
		if err != nil {
			L.Push(lua.LNil)
			L.Push(lua.LString(errString(err)))
			return 2
		}
		r.enqueue(item{t: t, args: argsFrom(L, 2)})
		L.Push(p.newObject(kindTask, t))
		return 1
	})
	suspendNew := e.fn(func(L *lua.LState) int {
		susp.next++
		id := susp.next
		s := &suspender{}
		susp.m[id] = s
		L.Push(lua.LNumber(id))
		L.Push(L.NewFunction(func(L *lua.LState) int {
			if s.done {
				return 0 // a second resume is ignored
			}
			s.done = true
			s.vals = argsFrom(L, 1)
			if s.stop != nil {
				s.stop()
			}
			if s.parked {
				r.inbox = append(r.inbox, pluginapi.Completion{Token: s.tok})
			}
			return 0
		}))
		return 2
	})
	suspendDrop := e.fn(func(L *lua.LState) int {
		delete(susp.m, L.CheckInt(1))
		return 0
	})

	methods := e.kind(kindTask).methods
	e.set(methods, "cancel", func(L *lua.LState) int {
		r.cancelTask(checkObject(L, 1, kindTask).(*task))
		L.Push(lua.LTrue)
		return 1
	})
	e.set(methods, "done", func(L *lua.LState) int {
		L.Push(lua.LBool(checkObject(L, 1, kindTask).(*task).done))
		return 1
	})

	res := e.runLua("gvim/async.lua", asyncLua, 1,
		spawn, suspendNew, suspendDrop, e.asyncV("suspend"), e.asyncV("task_wait"), methods,
		p.pre.RawGetString("pcall"), e.L.GetGlobal("unpack"), e.L.GetGlobal("select"), e.L.GetGlobal("error"), e.L.GetGlobal("ipairs"))
	mod := res[0].(*lua.LTable)
	area.RawSetString("spawn", spawn)
	area.RawSetString("suspend", mod.RawGetString("suspend"))
	area.RawSetString("all", mod.RawGetString("all"))
}

const asyncLua = `
local spawn, suspend_new, suspend_drop, suspend_await, task_wait, methods, pcall, unpack, select, error, ipairs = ...
local function pack(...) return { n = select("#", ...), ... } end

-- Task:wait() returns the task's results and re-raises its error.
methods.wait = function(self, opts) return task_wait(self, opts and opts.timeout_ms) end

local M = {}

-- suspend(fn): fn(resume) runs at once; the task parks until resume(...) is called from a
-- callback, timer or event; the values passed to resume are returned. A second resume is
-- ignored. opts.timeout_ms bounds the wait: nil, "timeout: ..." when resume was not called.
function M.suspend(fn, opts)
  local id, resume = suspend_new()
  local ok, err = pcall(fn, resume)
  if not ok then
    suspend_drop(id)
    error(err, 0)
  end
  return suspend_await(id, opts and opts.timeout_ms)
end

-- all(fns): run all functions concurrently, wait for all, re-raise the first error after
-- every one has settled. Returns { {n=, ...}, ... } in order.
function M.all(fns)
  local n = #fns
  if n > 16 then error("gvim.async.all: at most 16 functions", 2) end
  local tasks = {}
  for i = 1, n do tasks[i] = spawn(fns[i]) end
  local out, failed, first = {}, false, nil
  for i = 1, n do
    local r = pack(pcall(tasks[i].wait, tasks[i]))
    if r[1] then
      out[i] = { n = r.n - 1, unpack(r, 2, r.n) }
    else
      out[i] = { n = 0 }
      if not failed then failed, first = true, r[2] end
    end
  end
  if failed then error(first, 0) end
  return out
end

return M
`

// timeoutArg reads the optional timeout (milliseconds) at args[i]: 0 when absent. A bad
// value finishes c with a bad-argument error and reports false.
func timeoutArg(c *awaitCall, i int, fname string) (time.Duration, bool) {
	if i >= len(c.args) || c.args[i] == lua.LNil {
		return 0, true
	}
	ms, ok := argNumber(c.args, i)
	if !ok || ms != ms || ms <= 0 || ms > maxTimerMS {
		c.Bad(i+1, fname, "timeout_ms must be a positive number")
		return 0, false
	}
	return time.Duration(ms * float64(time.Millisecond)), true
}

func timeoutErr(what string, d time.Duration) string {
	return fmt.Sprintf("%s: %s timed out after %d ms", pluginapi.CodeTimeout, what, d.Milliseconds())
}

// awaitSuspend parks until the matching resume function is called (or the timeout passes).
func awaitSuspend(c *awaitCall) {
	id, ok := argNumber(c.args, 0)
	s := c.p.susp.m[int(id)]
	if !ok || s == nil {
		c.Bad(1, "suspend", "unknown suspension")
		return
	}
	if s.done {
		delete(c.p.susp.m, int(id))
		c.Return(s.vals...)
		return
	}
	timeout, ok := timeoutArg(c, 1, "suspend")
	if !ok {
		delete(c.p.susp.m, int(id))
		return
	}
	if err := c.CheckPark(false); err != nil {
		c.Fail(err)
		delete(c.p.susp.m, int(id))
		return
	}
	s.tok = c.NewToken()
	s.parked = true
	if timeout > 0 {
		s.stop = c.p.afterTimer(timeout, func() {
			if !s.done && !s.timedOut {
				s.timedOut = true
				c.r.inbox = append(c.r.inbox, pluginapi.Completion{Token: s.tok})
			}
		})
	}
	stop := func() {
		if s.stop != nil {
			s.stop()
		}
		delete(c.p.susp.m, int(id))
	}
	c.Park(&parkSpec{
		Token: s.tok,
		Convert: func(pluginapi.Completion) []lua.LValue {
			stop()
			if s.timedOut {
				return []lua.LValue{lua.LTrue, lua.LNil, lua.LString(timeoutErr("suspend", timeout))}
			}
			return append([]lua.LValue{lua.LTrue}, s.vals...)
		},
		Cancel: stop,
	})
}

// awaitTaskWait parks until the task argument is done, then returns its results or
// re-raises its error.
func awaitTaskWait(c *awaitCall) {
	var target *task
	if len(c.args) > 0 {
		if ud, ok := c.args[0].(*lua.LUserData); ok {
			if o, ok := ud.Value.(*object); ok && o.kind == kindTask {
				target, _ = o.value.(*task)
			}
		}
	}
	switch {
	case target == nil:
		c.Bad(1, "wait", "Task expected")
		return
	case target == c.t:
		c.Bad(1, "wait", "a task cannot wait for itself")
		return
	case target.p != c.p:
		c.Bad(1, "wait", "Task of another plugin")
		return
	}
	if target.done {
		settleResult(c, target)
		return
	}
	if err := c.CheckPark(false); err != nil {
		c.Fail(err)
		return
	}
	timeout, ok := timeoutArg(c, 1, "wait")
	if !ok {
		return
	}
	tok := c.NewToken()
	target.waiters = append(target.waiters, tok)
	var stop func()
	if timeout > 0 {
		stop = c.p.afterTimer(timeout, func() {
			for i, x := range target.waiters {
				if x == tok {
					target.waiters = append(target.waiters[:i:i], target.waiters[i+1:]...)
					c.r.inbox = append(c.r.inbox, pluginapi.Completion{Token: tok, Err: timeoutErr("wait", timeout)})
					return
				}
			}
		})
	}
	c.Park(&parkSpec{
		Token: tok,
		Convert: func(done pluginapi.Completion) []lua.LValue {
			if stop != nil {
				stop()
			}
			if done.Err != "" {
				return []lua.LValue{lua.LTrue, lua.LNil, lua.LString(done.Err)}
			}
			return waitResult(target)
		},
		Cancel: func() {
			if stop != nil {
				stop()
			}
		},
	})
}

func settleResult(c *awaitCall, t *task) {
	r := waitResult(t)
	if r[0] == lua.LFalse {
		c.Throw(r[1])
		return
	}
	c.Return(r[1:]...)
}

// waitResult is the resume arguments for Task:wait: ok, results... or false, error.
func waitResult(t *task) []lua.LValue {
	switch {
	case t.failed:
		return []lua.LValue{lua.LFalse, t.errv}
	case t.cancelled:
		return []lua.LValue{lua.LFalse, lua.LString("task cancelled")}
	}
	return append([]lua.LValue{lua.LTrue}, t.results...)
}

func argNumber(args []lua.LValue, i int) (float64, bool) {
	if i < len(args) {
		if n, ok := args[i].(lua.LNumber); ok {
			return float64(n), true
		}
	}
	return 0, false
}
