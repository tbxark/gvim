package tui

import (
	"context"
	"slices"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/internal/tui/palette"
	"github.com/tbxark/gvim/internal/uitest"
	"github.com/tbxark/gvim/pluginapi"
)

// workMsgs returns the pluginWorkMsgs among msgs.
func workMsgs(msgs []tea.Msg) []pluginWorkMsg {
	var out []pluginWorkMsg
	for _, m := range msgs {
		if w, ok := m.(pluginWorkMsg); ok {
			out = append(out, w)
		}
	}
	return out
}

// hasDrainMsg reports whether msgs holds a pluginDrainMsg.
func hasDrainMsg(msgs []tea.Msg) bool {
	return slices.ContainsFunc(msgs, func(m tea.Msg) bool { _, ok := m.(pluginDrainMsg); return ok })
}

// TestNilRuntimeIsInert checks that with no runtime every hook of the host does nothing
// and answers "not taken", whatever message reaches it.
func TestNilRuntimeIsInert(t *testing.T) {
	t.Parallel()
	m, ed := newPluginModel(t, nil)
	if m.plug.host != nil {
		t.Fatal("a model with no runtime built a plugin host")
	}
	m.Init()
	for name, msg := range map[string]tea.Msg{
		"a stray work message":  pluginWorkMsg{c: pluginapi.Completion{Token: 1}},
		"a stray drain message": pluginDrainMsg{},
	} {
		if cmd, ok := m.pluginMessage(msg); cmd != nil || !ok {
			t.Fatalf("%s: pluginMessage = (cmd %v, taken %v), want it swallowed", name, cmd != nil, ok)
		}
	}
	m.plugEmit("buffer_changed", nil)
	if len(m.plug.events) != 0 {
		t.Fatalf("plugEmit queued %d events with no runtime", len(m.plug.events))
	}
	if cmd := m.pluginTail(m.pluginSnapshot(), tea.WindowSizeMsg{}); cmd != nil {
		t.Fatal("pluginTail started a command with no runtime")
	}
	if cmd, ok := m.pluginAction("x.cmd", nil); ok || cmd != nil {
		t.Fatal("pluginAction took an action with no runtime")
	}
	if cancel, _, cmd := m.pluginSaveGate("manual"); cancel || cmd != nil {
		t.Fatal("the save gate vetoed or started a command with no runtime")
	}
	if got := m.allCommands(); len(got) != len(m.commands) {
		t.Fatalf("allCommands lists %d rows, want the %d built-in ones", len(got), len(m.commands))
	}
	m.pluginStop()
	m.Close()
	if err := m.Run("x.cmd"); err != nil || !slices.Equal(actionsOf(ed), []string{"x.cmd"}) {
		t.Fatalf("Run(x.cmd) = %v with actions %v, want the name to reach the editor", err, actionsOf(ed))
	}
}

// TestInitStartsRuntimeAndPumps checks that Init starts the runtime with the host and
// pumps once, and that the Work Start returns reaches Resolve through pluginWorkMsg.
func TestInitStartsRuntimeAndPumps(t *testing.T) {
	t.Parallel()
	rt := newFakeRuntime(t)
	rt.onStart = func() []pluginapi.Work {
		return []pluginapi.Work{{Token: 9, Plugin: "x", Run: func(context.Context) pluginapi.Completion {
			return pluginapi.Completion{Values: []any{"found"}}
		}}}
	}
	m, _ := newPluginModel(t, rt)
	rt.host = nil
	m.Init()
	if want := []string{"start", "drain"}; !slices.Equal(rt.log, want) {
		t.Fatalf("Init called %v, want %v", rt.log, want)
	}
	if rt.host == nil || rt.host != hostOf(m) {
		t.Fatalf("Start got host %v, want the model's host", rt.host)
	}
	msgs := runCmd(m.pluginInit())
	works := workMsgs(msgs)
	if len(works) != 1 {
		t.Fatalf("Work became %d messages %v, want 1 pluginWorkMsg", len(works), msgs)
	}
	if got := works[0].c; got.Token != 9 || len(got.Values) != 1 || got.Values[0] != "found" {
		t.Fatalf("completion = %+v, want token 9 (filled from the Work) and the value", got)
	}
	rt.log = nil
	m.Update(works[0])
	if want := []string{"resolve", "drain"}; !slices.Equal(rt.log, want) {
		t.Fatalf("a work message called %v, want %v", rt.log, want)
	}
	if rt.resolved[0].Token != 9 {
		t.Fatalf("resolved %+v, want token 9", rt.resolved[0])
	}
}

// hostOf returns the Host the model gives the runtime.
func hostOf(m *Model) pluginapi.Host { return m.plug.host }

// TestCloseStopsRuntime checks that Close stops the runtime (C25: the shutdown handlers
// run there, not on quit).
func TestCloseStopsRuntime(t *testing.T) {
	t.Parallel()
	rt := newFakeRuntime(t)
	m, _ := newPluginModel(t, rt)
	m.Close()
	if rt.stops != 1 {
		t.Fatalf("Close called Stop %d times, want 1", rt.stops)
	}
}

// TestWorkKinds checks how each kind of Work becomes messages: a one-shot, a stream that
// re-arms for each item, a stream that ends without a last item, a timer, and a Work
// that panics.
func TestWorkKinds(t *testing.T) {
	t.Parallel()
	item := func(n int, more bool) pluginapi.Completion {
		return pluginapi.Completion{Token: 5, Values: []any{n}, More: more}
	}
	tests := []struct {
		name string
		work pluginapi.Work
		want []pluginapi.Completion // the completions in order, following next
	}{
		{
			name: "one-shot",
			work: pluginapi.Work{Token: 5, Run: func(context.Context) pluginapi.Completion { return item(1, false) }},
			want: []pluginapi.Completion{item(1, false)},
		},
		{
			name: "one-shot without a token takes the Work's",
			work: pluginapi.Work{Token: 5, Run: func(context.Context) pluginapi.Completion {
				return pluginapi.Completion{Values: []any{1}}
			}},
			want: []pluginapi.Completion{item(1, false)},
		},
		{
			name: "panic becomes an error completion",
			work: pluginapi.Work{Token: 5, Run: func(context.Context) pluginapi.Completion { panic("boom") }},
			want: []pluginapi.Completion{{Token: 5, Err: "io: work panicked: boom"}},
		},
		{
			name: "timer",
			work: pluginapi.Work{Token: 5, After: time.Millisecond},
			want: []pluginapi.Completion{{Token: 5}},
		},
		{
			name: "stream re-arms until the last item",
			work: pluginapi.Work{Token: 5, Stream: streamOf(item(1, true), item(2, true), item(3, false))},
			want: []pluginapi.Completion{item(1, true), item(2, true), item(3, false)},
		},
		{
			name: "stream that ends with nothing delivers nothing more",
			work: pluginapi.Work{Token: 5, Stream: streamOf(item(1, true))},
			want: []pluginapi.Completion{item(1, true)},
		},
		{
			name: "stream panic ends the stream with an error",
			work: pluginapi.Work{Token: 5, Stream: func(context.Context) (pluginapi.Completion, bool) { panic("bad") }},
			want: []pluginapi.Completion{{Token: 5, Err: "io: stream panicked: bad"}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var got []pluginapi.Completion
			cmd := workCmd(tt.work)
			for cmd != nil {
				msg, _ := cmd().(pluginWorkMsg)
				if msg.c.Token == 0 && msg.c.Values == nil && msg.c.Err == "" {
					break
				}
				got = append(got, msg.c)
				cmd = nil
				if msg.next != nil {
					cmd = msg.next()
				}
			}
			if len(got) != len(tt.want) {
				t.Fatalf("got %d completions %+v, want %d %+v", len(got), got, len(tt.want), tt.want)
			}
			for i := range got {
				if got[i].Token != tt.want[i].Token || got[i].Err != tt.want[i].Err || got[i].More != tt.want[i].More || !slices.Equal(got[i].Values, tt.want[i].Values) {
					t.Fatalf("completion %d = %+v, want %+v", i, got[i], tt.want[i])
				}
			}
		})
	}
}

// streamOf returns a Stream that yields items in order and then ends.
func streamOf(items ...pluginapi.Completion) func(context.Context) (pluginapi.Completion, bool) {
	i := 0
	return func(context.Context) (pluginapi.Completion, bool) {
		if i >= len(items) {
			return pluginapi.Completion{}, false
		}
		i++
		return items[i-1], true
	}
}

// TestDrainTick checks that a runtime with queued work gets another Drain by a tick, that
// one tick is in flight at a time, and that the tick message pumps again.
func TestDrainTick(t *testing.T) {
	t.Parallel()
	rt := newFakeRuntime(t)
	more := true
	rt.onDrain = func() ([]pluginapi.Work, bool) { return nil, more }
	m, _ := newPluginModel(t, rt)

	first := runCmd(m.pumpAfter(func() {}))
	if !hasDrainMsg(first) {
		t.Fatalf("a Drain with more queued started %v, want a pluginDrainMsg tick", first)
	}
	if second := runCmd(m.pumpAfter(func() {})); hasDrainMsg(second) {
		t.Fatal("a second tick was started while the first was in flight")
	}
	rt.drains = 0
	more = false
	_, cmd := m.Update(pluginDrainMsg{})
	if rt.drains != 1 {
		t.Fatalf("the tick message drained %d times, want 1", rt.drains)
	}
	if hasDrainMsg(runCmd(cmd)) {
		t.Fatal("the tick re-armed itself although nothing was left")
	}
	more = true
	if !hasDrainMsg(runCmd(m.pumpAfter(func() {}))) {
		t.Fatal("no tick after the previous one was received")
	}
}

// TestLaterRunsInOrderAfterTheSlice checks the deferral queue: closures run after the
// Lua slice returned (so the runtime is free), in the order they were queued, before
// the drain, and one pump runs at most pumpLater of them; the rest wait for the tick.
func TestLaterRunsInOrderAfterTheSlice(t *testing.T) {
	t.Parallel()
	rt := newFakeRuntime(t, "x.cmd")
	rt.strictSlice = true
	var order []int
	var inSliceAt []bool
	m, _ := newPluginModel(t, rt)
	rt.onRun = func(string, map[string]any) {
		for i := range 3 {
			m.plug.later = append(m.plug.later, func() tea.Cmd {
				order = append(order, i)
				inSliceAt = append(inSliceAt, rt.InSlice())
				return nil
			})
		}
		if len(order) != 0 {
			t.Error("a deferred closure ran inside the slice")
		}
	}
	must(t, m.Run("x.cmd"))
	if !slices.Equal(order, []int{0, 1, 2}) {
		t.Fatalf("closures ran in order %v, want 0 1 2", order)
	}
	if slices.Contains(inSliceAt, true) {
		t.Fatalf("a closure ran with InSlice true: %v", inSliceAt)
	}
	if want := []string{"run:x.cmd", "drain"}; !slices.Equal(rt.log, want) {
		t.Fatalf("calls = %v, want %v", rt.log, want)
	}

	ran := 0
	for range pumpLater + 500 {
		m.plug.later = append(m.plug.later, func() tea.Cmd { ran++; return nil })
	}
	msgs := runCmd(m.pluginPump())
	if ran != pumpLater {
		t.Fatalf("one pump ran %d closures, want the cap %d", ran, pumpLater)
	}
	if !hasDrainMsg(msgs) {
		t.Fatal("closures were left but no tick was started")
	}
	m.Update(pluginDrainMsg{})
	if ran != pumpLater+500 || len(m.plug.later) != 0 {
		t.Fatalf("after the tick %d closures ran and %d are left, want all run", ran, len(m.plug.later))
	}
}

// TestNestedPumpDoesNotRecurse checks that a closure that calls into the runtime again
// (a deferred command) does not start a pump inside the pump: the outer loop picks up
// what the nested call queued.
func TestNestedPumpDoesNotRecurse(t *testing.T) {
	t.Parallel()
	rt := newFakeRuntime(t, "x.cmd", "y.cmd")
	m, _ := newPluginModel(t, rt)
	rt.onRun = func(action string, _ map[string]any) {
		if action == "x.cmd" {
			if err := m.plug.host.Do("y.cmd", nil, 1); err != nil {
				t.Error(err)
			}
		}
	}
	must(t, m.Run("x.cmd"))
	want := []string{"run:x.cmd", "run:y.cmd", "drain"}
	if !slices.Equal(rt.log, want) {
		t.Fatalf("calls = %v, want %v: the deferred command runs in the pump, one drain at the end", rt.log, want)
	}
}

// TestPluginEmitQueuesUntilPumped checks that plugEmit never calls the runtime, drops an
// event nobody wants, and that the next tail hands the events to Emit before the drain.
func TestPluginEmitQueuesUntilPumped(t *testing.T) {
	t.Parallel()
	rt := newFakeRuntime(t)
	rt.wants["buffer_changed"] = true
	m, _ := newPluginModel(t, rt)
	m.plugEmit("buffer_changed", map[string]any{"n": 1})
	m.plugEmit("cursor_moved", nil)
	if len(rt.log) != 0 {
		t.Fatalf("plugEmit called the runtime: %v", rt.log)
	}
	if len(m.plug.events) != 1 {
		t.Fatalf("queued %d events, want the one that is wanted", len(m.plug.events))
	}
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	if want := []string{"emit:buffer_changed", "drain"}; !slices.Equal(rt.log, want) {
		t.Fatalf("calls = %v, want %v", rt.log, want)
	}
}

// TestPumpRunsAfterEveryRuntimeCall checks the C26 rule: after Start, Run, Resolve,
// Control, Emit and Veto the root pumps, so the runtime is drained once after each.
func TestPumpRunsAfterEveryRuntimeCall(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		do   func(m *Model)
		want []string
	}{
		{"start", func(m *Model) { m.Init() }, []string{"start", "drain"}},
		{"run", func(m *Model) { must(t, m.Run("x.cmd")) }, []string{"run:x.cmd", "drain"}},
		{"resolve", func(m *Model) { m.Update(pluginWorkMsg{c: pluginapi.Completion{Token: 3}}) }, []string{"resolve", "drain"}},
		{"control", func(m *Model) { m.pluginControl(pluginapi.Control{Op: "reload", Name: "x"}) }, []string{"control:reload", "drain"}},
		{"emit", func(m *Model) {
			m.plugEmit("buffer_changed", nil)
			m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
		}, []string{"emit:buffer_changed", "drain"}},
		{"veto", func(m *Model) { must(t, m.Run("save")) }, []string{"veto:before_file_save", "drain"}},
		{"drain tick", func(m *Model) { m.Update(pluginDrainMsg{}) }, []string{"drain"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			rt := newFakeRuntime(t, "x.cmd")
			rt.wants["buffer_changed"] = true
			rt.wants["before_file_save"] = true
			m, _ := newPluginModel(t, rt)
			tt.do(m)
			if !slices.Equal(rt.log, tt.want) {
				t.Fatalf("calls = %v, want %v", rt.log, tt.want)
			}
		})
	}
}

// TestControlReloadSchedulesWork checks that Control's Work becomes a command whose
// completion reaches Resolve, that its notices show on the status row, and that the
// control call is followed by a drain.
func TestControlReloadSchedulesWork(t *testing.T) {
	t.Parallel()
	rt := newFakeRuntime(t)
	rt.onControl = func(c pluginapi.Control) ([]pluginapi.Work, []pluginapi.Notice) {
		return []pluginapi.Work{{Token: 7, Run: func(context.Context) pluginapi.Completion {
			return pluginapi.Completion{Values: []any{"sources"}}
		}}}, []pluginapi.Notice{{Text: "Reloading " + c.Name}}
	}
	m, _ := newPluginModel(t, rt)
	cmd := m.pluginControl(pluginapi.Control{Op: "reload", Name: "git"})
	if m.notice != "Reloading git" {
		t.Fatalf("notice = %q, want the runtime's", m.notice)
	}
	if want := []pluginapi.Control{{Op: "reload", Name: "git"}}; !slices.Equal(rt.controls, want) {
		t.Fatalf("controls = %v, want %v", rt.controls, want)
	}
	works := workMsgs(runCmd(cmd))
	if len(works) != 1 {
		t.Fatalf("Control's Work became %d messages, want 1", len(works))
	}
	m.Update(works[0])
	if len(rt.resolved) != 1 || rt.resolved[0].Token != 7 || rt.resolved[0].Values[0] != "sources" {
		t.Fatalf("resolved %+v, want the completion of token 7", rt.resolved)
	}
}

// TestCancelWaitDropsResult checks that a wait the plugin cancelled has its result
// dropped, and that a token cannot wait twice.
func TestCancelWaitDropsResult(t *testing.T) {
	t.Parallel()
	rt := newFakeRuntime(t)
	m, _ := newPluginModel(t, rt)
	if err := m.plugWait(4, "open"); err != nil {
		t.Fatal(err)
	}
	if err := m.plugWait(4, "open"); pluginapi.CodeOf(err) != pluginapi.CodeInvalid {
		t.Fatalf("a second wait on token 4 = %v, want invalid", err)
	}
	m.plug.host.CancelWait(4)
	if cmd := m.plugFinish(pluginapi.Completion{Token: 4}); cmd != nil || len(rt.resolved) != 0 {
		t.Fatal("a cancelled wait was resolved")
	}
	if err := m.plugWait(5, "open"); err != nil {
		t.Fatal(err)
	}
	m.plugFinish(pluginapi.Completion{Token: 5, Values: []any{true}})
	if len(rt.resolved) != 1 || rt.resolved[0].Token != 5 {
		t.Fatalf("resolved %+v, want the live wait", rt.resolved)
	}
	if _, still := m.plug.waits[5]; still {
		t.Fatal("a finished wait is still recorded")
	}
}

// TestPluginWorkRoundTripUnderRealProgram runs a real Bubble Tea program: a plugin
// command starts, the runtime hands back Work, the Work runs on a command goroutine, and
// its completion returns to Resolve through Update. View is called by the program on the
// same goroutine as Update. The test is for the race detector: the Work goroutine shares
// nothing with the model.
func TestPluginWorkRoundTripUnderRealProgram(t *testing.T) {
	t.Parallel()
	rt := newFakeRuntime(t, "x.cmd")
	resolved := make(chan pluginapi.Completion, 4)
	started := false
	rt.onRun = func(string, map[string]any) { started = true }
	rt.onDrain = func() ([]pluginapi.Work, bool) {
		if !started {
			return nil, false
		}
		started = false
		return []pluginapi.Work{{Token: 11, Run: func(ctx context.Context) pluginapi.Completion {
			return pluginapi.Completion{Values: []any{"done"}}
		}}}, false
	}
	rt.onResolve = func(c pluginapi.Completion) { resolved <- c }
	m, _ := newPluginModel(t, rt)

	s := uitest.Start(t, m, 80, 24)
	s.Send(palette.RunCommandMsg{Action: "x.cmd"})
	s.Settle()
	select {
	case c := <-resolved:
		if c.Token != 11 || c.Values[0] != "done" {
			t.Fatalf("resolved %+v, want token 11 with the Work's value", c)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("the Work's completion never reached Resolve")
	}
	s.Settle()
}

// TestPumpSharesOneDrainBudget checks D16: all event and timer work of one Update shares
// one budget. The rounds only cap the cascade, so a runtime that keeps reporting more
// work gets one Drain and a tick, and a cascade of deferred closures gets the remainder
// of the one budget for each of its Drains.
func TestPumpSharesOneDrainBudget(t *testing.T) {
	t.Parallel()
	t.Run("more work waits for the tick", func(t *testing.T) {
		t.Parallel()
		rt := newFakeRuntime(t, "x.cmd")
		m, _ := newPluginModel(t, rt)
		rt.onDrain = func() ([]pluginapi.Work, bool) {
			if err := rt.host.Do("save", nil, 1); err != nil {
				t.Error(err)
			}
			return nil, true
		}
		_, cmd := m.Update(pluginDrainMsg{})
		if rt.drains != 1 {
			t.Fatalf("one Update drained %d times, want 1 while the runtime reports more", rt.drains)
		}
		if !hasDrainMsg(runCmd(cmd)) {
			t.Fatal("no tick was left for the remaining work")
		}
	})
	t.Run("a cascade gets the remainder", func(t *testing.T) {
		t.Parallel()
		rt := newFakeRuntime(t, "x.cmd")
		m, _ := newPluginModel(t, rt)
		rounds := 0
		rt.onDrain = func() ([]pluginapi.Work, bool) {
			time.Sleep(20 * time.Millisecond)
			if rounds++; rounds < 3 {
				m.plug.later = append(m.plug.later, func() tea.Cmd { return nil })
			}
			return nil, false
		}
		m.Update(pluginDrainMsg{})
		if len(rt.budgets) != 3 {
			t.Fatalf("drained %d times, want the 3 rounds of the cascade", len(rt.budgets))
		}
		if rt.budgets[0] > pumpBudget || rt.budgets[1] >= rt.budgets[0]-10*time.Millisecond || rt.budgets[2] >= rt.budgets[1]-10*time.Millisecond {
			t.Fatalf("budgets %v, want each Drain to get what is left of %v", rt.budgets, pumpBudget)
		}
	})
}

// TestLifecycleEventsIgnoreWants checks that ready and resize are queued for the runtime
// although nothing subscribed to them yet (the plugins load later), and that an ordinary
// event nobody wants is still dropped. shutdown is one of those: the root raises none and
// Runtime.Stop runs the handlers (C25), so it is no longer queued unconditionally.
func TestLifecycleEventsIgnoreWants(t *testing.T) {
	t.Parallel()
	rt := newFakeRuntime(t)
	m, _ := newPluginModel(t, rt)
	for _, name := range []string{"ready", "resize", "shutdown", "buffer_changed"} {
		m.plugEmit(name, nil)
	}
	var got []string
	for _, e := range m.plug.events {
		got = append(got, e.Name)
	}
	if want := []string{"ready", "resize"}; !slices.Equal(got, want) {
		t.Fatalf("queued %v, want %v", got, want)
	}
	m.pluginPump()
	if want := []string{"emit:ready", "emit:resize"}; !slices.Equal(rt.log[:2], want) {
		t.Fatalf("runtime saw %v, want the two lifecycle events first", rt.log)
	}
}

// TestPanicHookRunsOnceForEveryKeyPath checks that the panic-chord hook sees a key press
// that an open overlay takes (handleKey never does) and one that goes to handleKey, exactly
// once each, and that a chord it takes does not reach the overlay or the editor.
func TestPanicHookRunsOnceForEveryKeyPath(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name    string
		overlay bool
	}{{"overlay open", true}, {"no overlay", false}} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			rt := newFakeRuntime(t)
			m, ed := newPluginModel(t, rt)
			if tt.overlay {
				m.openPalette("")
				if len(m.overlays) == 0 {
					t.Fatal("no overlay opened")
				}
			}
			var seen []rune
			m.plug.panicKey = func(k tea.Key) bool { seen = append(seen, k.Code); return k.Code == 'z' }
			overlays := len(m.overlays)
			before := len(actionsOf(ed))
			m.Update(tea.KeyPressMsg{Code: 'a', Text: "a"})
			m.Update(tea.KeyPressMsg{Code: 'z', Text: "z"})
			if !slices.Equal(seen, []rune{'a', 'z'}) {
				t.Fatalf("the hook saw %q, want each key once", string(seen))
			}
			if len(m.overlays) == 0 && tt.overlay {
				t.Fatalf("the chord reached the overlay: overlays %d -> %d", overlays, len(m.overlays))
			}
			if got := len(actionsOf(ed)); !tt.overlay && got != before+1 {
				t.Fatalf("the editor got %d actions, want only the one for the key that was not a chord", got-before)
			}
		})
	}
}

// TestReadyIsRaisedOnceOnTheFirstSizeAfterInit: Bubble Tea calls Init before it delivers the
// first window size, and `ready` follows that size, once. A size that arrives with no Init
// (a model built for a test) raises nothing.
func TestReadyIsRaisedOnceOnTheFirstSizeAfterInit(t *testing.T) {
	t.Parallel()
	rt := newFakeRuntime(t)
	m, _ := newPluginModel(t, rt) // sized, never started
	if slices.Contains(rt.log, "emit:ready") {
		t.Fatalf("ready was raised before Init: %v", rt.log)
	}
	m.Init()
	rt.log = nil
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m.Update(tea.WindowSizeMsg{Width: 90, Height: 30})
	count := 0
	for _, e := range rt.emitted {
		if e.Name == "ready" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("ready raised %d times in %v, want once", count, rt.log)
	}
	if rt.log[0] != "emit:ready" {
		t.Fatalf("calls = %v, want ready emitted first", rt.log)
	}
}

// TestTimerWorkEndsWhenItsContextIsCancelled: the command of a timer must not outlive the
// plugin that set it (a reload or disable cancels the plugin context); it returns no message
// at once instead of waiting out the duration.
func TestTimerWorkEndsWhenItsContextIsCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cmd := workCmd(pluginapi.Work{Token: 7, After: time.Hour, Ctx: ctx})
	done := make(chan tea.Msg, 1)
	go func() { done <- cmd() }()
	select {
	case msg := <-done:
		t.Fatalf("the timer returned %v before its time", msg)
	case <-time.After(20 * time.Millisecond):
	}
	cancel()
	select {
	case msg := <-done:
		if msg != nil {
			t.Fatalf("a cancelled timer returned %v, want no message", msg)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the cancelled timer is still waiting")
	}
}
