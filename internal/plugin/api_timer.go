package plugin

import (
	"fmt"
	"math"
	"time"

	lua "github.com/yuin/gopher-lua"

	"github.com/tbxark/gvim/internal/pluginapi"
)

// gvim.timer (FINAL 5.9). A timer is Work{After}: the ui (or the test harness's manual
// clock) answers it with a completion, and the callback runs as an event-class task. A
// repeating timer re-arms after its callback finished, so slow callbacks never pile up.

const (
	kindTimer   = "Timer"
	minEveryMS  = 10
	maxTimerMS  = 1 << 31 // about 24 days
	timerPeriod = time.Millisecond
)

func init() {
	registerAwait("timer_sleep", awaitSleep)
}

// luaTimer is the Timer handle.
type luaTimer struct {
	p      *plugin
	id     int
	fn     *lua.LFunction
	period time.Duration
	every  bool
	active bool
	stop   func()
	live   *int
}

func (t *luaTimer) handleString() string { return fmt.Sprint(t.id) }

func (t *luaTimer) arm() { t.stop = t.p.afterTimer(t.p.paceArm(t.period), t.fire) }

// finish ends the timer: it no longer counts against the quota.
func (t *luaTimer) finish() {
	if t.active {
		t.active = false
		*t.live--
	}
}

func (t *luaTimer) cancel() bool {
	was := t.active
	if was {
		t.stop()
		t.finish()
	}
	return was
}

// fire runs when the period is over: start the callback; a repeating timer waits for it to
// finish before it re-arms.
func (t *luaTimer) fire() {
	p := t.p
	if !t.active {
		return
	}
	if p.muted() {
		t.finish()
		return
	}
	task, err := p.timerTask("timer", t.fn)
	if err != nil {
		p.r.logf(p.name, levelWarn, "dropped timer callback: "+err.Error())
		if t.every {
			t.arm()
		}
	}
	switch {
	case !t.every:
		t.finish()
	case err == nil:
		p.afterTask(task, func() {
			if t.active {
				t.arm()
			}
		})
	}
}

// debouncer is the function gvim.timer.debounce returns: a call restarts the timer, and
// the callback runs once, with the arguments of the last call, after a quiet period.
type debouncer struct {
	p      *plugin
	fn     *lua.LFunction
	period time.Duration
	args   []lua.LValue
	armed  bool
	stop   func()
	live   *int
}

func (d *debouncer) fire() {
	d.armed = false
	*d.live--
	if d.p.muted() {
		return
	}
	if _, err := d.p.timerTask("debounced call", d.fn, d.args...); err != nil {
		d.p.r.logf(d.p.name, levelWarn, "dropped debounced call: "+err.Error())
	}
	d.args = nil
}

// timerMS reads a millisecond argument, raising a bad-argument error below min or above
// the cap.
func timerMS(L *lua.LState, n int, lo float64) time.Duration {
	ms := float64(L.CheckNumber(n))
	if math.IsNaN(ms) || ms < lo || ms > maxTimerMS {
		L.ArgError(n, fmt.Sprintf("ms must be between %g and %d", lo, int64(maxTimerMS)))
	}
	return time.Duration(ms * float64(time.Millisecond))
}

func awaitSleep(c *awaitCall) {
	ms, ok := argNumber(c.args, 0)
	if !ok || math.IsNaN(ms) || ms < 0 || ms > maxTimerMS {
		c.Bad(1, "sleep", fmt.Sprintf("ms must be a number between 0 and %d", int64(maxTimerMS)))
		return
	}
	if err := c.CheckPark(true); err != nil {
		c.Fail(err)
		return
	}
	c.Park(&parkSpec{
		Work:    &pluginapi.Work{After: max(c.p.paceArm(time.Duration(ms*float64(time.Millisecond))), timerPeriod)},
		Convert: func(pluginapi.Completion) []lua.LValue { return []lua.LValue{lua.LTrue, lua.LTrue} },
	})
}

func installTimer(e *apiEnv) {
	p := e.p
	area := e.area("timer")
	area.RawSetString("sleep", e.async("timer_sleep"))

	live, seq := 0, 0
	methods := e.kind(kindTimer).methods
	e.set(methods, "cancel", func(L *lua.LState) int {
		L.Push(lua.LBool(checkObject(L, 1, kindTimer).(*luaTimer).cancel()))
		return 1
	})
	e.set(methods, "active", func(L *lua.LState) int {
		L.Push(lua.LBool(checkObject(L, 1, kindTimer).(*luaTimer).active))
		return 1
	})
	quota := func(L *lua.LState) bool {
		if live < QuotaTimers {
			return true
		}
		v, s := errTuple(pluginapi.Errf(pluginapi.CodeLimit, "timer quota (%d)", QuotaTimers))
		L.Push(v)
		L.Push(s)
		return false
	}
	start := func(every bool, lo float64) lua.LGFunction {
		return func(L *lua.LState) int {
			d := timerMS(L, 1, lo)
			fn := L.CheckFunction(2)
			if !quota(L) {
				return 2
			}
			live++
			seq++
			t := &luaTimer{p: p, id: seq, fn: fn, period: d, every: every, active: true, live: &live}
			t.arm()
			L.Push(p.newObject(kindTimer, t))
			return 1
		}
	}
	e.set(area, "after", start(false, 1))
	e.set(area, "every", start(true, minEveryMS))
	e.set(area, "debounce", func(L *lua.LState) int {
		d := timerMS(L, 1, 1)
		fn := L.CheckFunction(2)
		db := &debouncer{p: p, fn: fn, period: d, live: &live}
		L.Push(L.NewFunction(p.guarded(func(L *lua.LState) int {
			db.args = argsFrom(L, 1)
			if db.armed {
				db.stop()
			} else {
				if live >= QuotaTimers {
					L.RaiseError("limit: timer quota (%d)", QuotaTimers)
				}
				live++
				db.armed = true
			}
			db.stop = p.afterTimer(p.paceArm(db.period), db.fire)
			return 0
		})))
		return 1
	})
}
