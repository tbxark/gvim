package plugin

import (
	"context"
	"errors"
	"runtime"
	"runtime/metrics"
	"sync/atomic"
	"time"
)

// Cancellation causes of a slice. The runtime reads context.Cause after a failed resume
// and ignores gopher-lua's own message for these (AMENDMENTS C10).
var (
	errSliceDeadline = errors.New("slice deadline exceeded")
	errHardDeadline  = errors.New("hard deadline exceeded")
	errMemory        = errors.New("memory limit")
	errCancelled     = errors.New("cancelled")
	errSliceDone     = errors.New("slice finished")
)

// slice is the time and memory envelope of one resume of a root task.
//
// The deadline is a manual timer, not context.WithDeadline: while a heavy Host call runs
// the timer is stopped (pause), so slow editor work is not charged to the plugin; resume
// re-arms it with the remaining budget. The hard timer is never paused, so a loop of heavy
// Host calls still ends at hardFactor x budget.
type slice struct {
	r      *Runtime
	ctx    context.Context
	cancel context.CancelCauseFunc

	budget time.Duration
	start  time.Time

	timer, hard, guardStart *time.Timer
	armedAt                 time.Time
	used                    time.Duration // Lua time spent before the last pause
	paused                  bool
	pausedAt                time.Time
	hostTime                time.Duration // wall time spent inside paused Host calls

	pausedFlag atomic.Bool // read by the heap guard goroutine
	done       chan struct{}
}

// newSlice starts a slice under parent with the given Lua-time budget.
func (r *Runtime) newSlice(parent context.Context, budget time.Duration) *slice {
	ctx, cancel := context.WithCancelCause(parent)
	now := time.Now()
	s := &slice{r: r, ctx: ctx, cancel: cancel, budget: budget, start: now, armedAt: now, done: make(chan struct{})}
	s.timer = time.AfterFunc(budget, func() { cancel(errSliceDeadline) })
	s.hard = time.AfterFunc(budget*hardFactor, func() { cancel(errHardDeadline) })
	s.guardStart = time.AfterFunc(guardStartAfter, s.runGuard)
	return s
}

// pause stops charging time to the plugin (a heavy Host call begins). Idempotent.
func (s *slice) pause() {
	if s.paused {
		return
	}
	now := time.Now()
	s.timer.Stop()
	s.used += now.Sub(s.armedAt)
	s.paused = true
	s.pausedAt = now
	s.pausedFlag.Store(true)
}

// resume charges time again. When the budget is already used up the slice is cancelled.
func (s *slice) resume() {
	if !s.paused {
		return
	}
	now := time.Now()
	s.hostTime += now.Sub(s.pausedAt)
	s.paused = false
	s.pausedFlag.Store(false)
	s.armedAt = now
	if remaining := s.budget - s.used; remaining > 0 {
		s.timer.Reset(remaining)
	} else {
		s.cancel(errSliceDeadline)
	}
}

// elapsed is the wall time since the slice began.
func (s *slice) elapsed() time.Duration { return time.Since(s.start) }

// finish releases the timers and the guard. The context is cancelled; read the cause
// before calling it.
func (s *slice) finish() {
	s.timer.Stop()
	s.hard.Stop()
	s.guardStart.Stop()
	close(s.done)
	s.cancel(errSliceDone)
}

// ---------- heap guard (AMENDMENTS C10) ----------

var heapNames = [...]string{"/memory/classes/heap/objects:bytes", "/gc/heap/live:bytes"}

// readHeap returns the bytes of allocated heap objects (live and garbage) and the live
// bytes marked by the last GC.
func readHeap() (objects, live uint64) {
	samples := []metrics.Sample{{Name: heapNames[0]}, {Name: heapNames[1]}}
	metrics.Read(samples)
	if samples[0].Value.Kind() == metrics.KindUint64 {
		objects = samples[0].Value.Uint64()
	}
	if samples[1].Value.Kind() == metrics.KindUint64 {
		live = samples[1].Value.Uint64()
	}
	return
}

// maxGuardGCs bounds the forced collections of one slice.
const maxGuardGCs = 8

// runGuard is the circuit breaker against runaway allocation. It is started lazily, after
// guardStartAfter of slice time, and samples the heap every guardSampleEvery. A candidate
// is "allocated objects grew by more than T since the baseline"; it then forces a GC
// (blocking only this goroutine) and trips only when the LIVE heap grew by more than T, so
// garbage churn beside a large live heap does not cancel an innocent plugin. T is
// max(HeapLimit, live/2).
//
// The first baseline uses the live size of the last GC, which can be stale (high) when much
// was freed since. A forced GC that does not trip therefore lowers the live baseline to its
// own accurate number (never raises it, so retained growth keeps adding up across cycles),
// restarts the allocation count and guarding goes on, at most maxGuardGCs times.
// LState.SetMx is never used (it calls os.Exit).
func (s *slice) runGuard() {
	select {
	case <-s.done:
		return
	default:
	}
	obj0, live0 := readHeap()
	threshold := func() uint64 { return max(uint64(s.r.heapLimit), live0/2) }
	tick := time.NewTicker(guardSampleEvery)
	defer tick.Stop()
	for gcs := 0; ; {
		select {
		case <-s.done:
			return
		case <-tick.C:
		}
		if s.pausedFlag.Load() {
			continue
		}
		obj, _ := readHeap()
		limit := threshold()
		if obj <= obj0 || obj-obj0 <= limit {
			continue
		}
		if gcs++; gcs > maxGuardGCs {
			return
		}
		runtime.GC()
		obj1, live := readHeap()
		if live > live0 && live-live0 > limit {
			s.cancel(errMemory)
			return
		}
		obj0 = obj1
		live0 = min(live0, live)
	}
}
