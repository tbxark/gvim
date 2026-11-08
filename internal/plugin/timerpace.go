package plugin

import (
	"fmt"
	"time"

	"github.com/tbxark/gvim/internal/pluginapi"
)

// The timer pace budget. A plugin that loops on gvim.timer.sleep(0), or whose timer callback
// keeps the editor busy, never trips the time budget of a slice (every slice is short) and
// never grows memory, but it burns a core for ever. The budget watches two rates per plugin
// over one-second windows:
//
//   - fast arms: timers and sleeps asked for with a delay below minEveryMS (a timer of zero is
//     a yield to the event loop, and gvim.timer.every refuses anything below it);
//   - busy time: the wall time the plugin's timer callbacks ran.
//
// Two hot windows in a row throttle the plugin: its timers and sleeps wait at least the
// throttle floor, a warning goes to the log and the status row once, and the plugin keeps
// working at a pace the editor can afford. When the plugin stays hot for paceStrikeWindows
// more windows despite the throttle, it gets a strike (three suspend it, as for every other
// kind of runaway). A plugin that calms down for paceCalmWindows windows is released.
const (
	paceWindow           = time.Second
	paceFastArms         = 200                    // fast arms per window that make it hot
	paceBusy             = 400 * time.Millisecond // callback time per window that makes it hot
	paceHotWindows       = 2                      // hot windows in a row that throttle
	paceStrikeWindows    = 5                      // hot windows while throttled that give a strike
	paceCalmWindows      = 3                      // calm windows in a row that release
	paceThrottled        = 100 * time.Millisecond
	paceHotBusyThrottled = 150 * time.Millisecond
)

// timerPace is the pace state of one plugin.
type timerPace struct {
	start     time.Time // of the current window; zero before the first event
	fast      int       // fast arms in the window
	busy      time.Duration
	longest   time.Duration // the longest callback of the window
	hot, calm int           // consecutive hot and calm windows
	throttled bool
	hotLeft   int  // hot windows left before the next strike, while throttled
	warned    bool // the throttle was announced: once per plugin, not once per throttle
	floor     time.Duration
}

// roll closes the windows that ended before now and updates the hot and calm streaks. It
// returns what to do about the plugin: nothing, throttle it, or strike it.
func (tp *timerPace) roll(now time.Time) (throttle, strike bool) {
	if tp.start.IsZero() {
		tp.start = now
		return false, false
	}
	for now.Sub(tp.start) >= paceWindow {
		// While throttled only the busy time counts: a loop that waits out the floor between its
		// slices is calm, however often it arms, and one that keeps the core busy is hot.
		busy := paceBusy
		if tp.throttled {
			busy = paceHotBusyThrottled
		}
		hot := tp.busy >= busy || (!tp.throttled && tp.fast >= paceFastArms)
		longest := tp.longest
		tp.start = tp.start.Add(paceWindow)
		if now.Sub(tp.start) >= paceWindow {
			// a gap of whole windows with no event in it: they were all calm
			tp.start = now
		}
		tp.fast, tp.busy, tp.longest = 0, 0, 0
		switch {
		case hot && !tp.throttled:
			tp.calm = 0
			if tp.hot++; tp.hot >= paceHotWindows {
				tp.throttled, tp.hot, tp.hotLeft = true, 0, paceStrikeWindows
				tp.floor = max(paceThrottled, 4*longest)
				throttle = true
			}
		case hot:
			tp.calm = 0
			if tp.hotLeft--; tp.hotLeft <= 0 {
				tp.hotLeft = paceStrikeWindows
				strike = true
			}
		default:
			tp.hot = 0
			if tp.calm++; tp.throttled && tp.calm >= paceCalmWindows {
				tp.throttled, tp.calm = false, 0
			}
		}
	}
	return throttle, strike
}

// paceArm is called when p arms a timer or a sleep that asks for d. It returns the delay to
// use: d, or the throttle floor while the plugin is throttled.
func (p *plugin) paceArm(d time.Duration) time.Duration {
	tp := &p.pace
	throttle, strike := tp.roll(p.r.now())
	p.r.paceReact(p, throttle, strike)
	if d < minEveryMS*time.Millisecond {
		tp.fast++
	}
	if tp.throttled {
		return max(d, tp.floor)
	}
	return d
}

// paceRan is called when a timer callback of p finished after elapsed.
func (p *plugin) paceRan(elapsed time.Duration) {
	tp := &p.pace
	throttle, strike := tp.roll(p.r.now())
	p.r.paceReact(p, throttle, strike)
	tp.busy += elapsed
	tp.longest = max(tp.longest, elapsed)
}

// paceReact tells the user about what roll decided.
func (r *Runtime) paceReact(p *plugin, throttle, strike bool) {
	if throttle && !p.pace.warned {
		p.pace.warned = true
		msg := fmt.Sprintf("%s: its timers re-arm faster than the editor can afford; they now wait at least %d ms", p.name, p.pace.floor.Milliseconds())
		r.logf(p.name, levelWarn, "timer pace: "+msg)
		r.notify(p, pluginapi.LevelWarn, msg)
	}
	if strike && !p.dead && p.status != statusSuspended {
		r.logf(p.name, levelWarn, "timer pace: still looping on timers while throttled")
		if n, suspended := r.strike(p, "timer loop"); suspended {
			r.notify(p, pluginapi.LevelError, fmt.Sprintf("suspended after %d strikes (Plugin: Manager)", strikeLimit))
		} else {
			r.notify(p, pluginapi.LevelError, fmt.Sprintf("%s: still looping on timers, strike %d/%d", p.name, min(n, strikeLimit), strikeLimit))
		}
	}
}
