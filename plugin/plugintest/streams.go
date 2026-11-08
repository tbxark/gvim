package plugintest

import (
	"context"
	"time"

	"github.com/tbxark/gvim/pluginapi"
)

// foreignIdle is how long pollStreams waits for a stream of an OS that is not a FakeOS to
// produce something before it calls the stream idle.
const foreignIdle = 25 * time.Millisecond

// stream is a running Work.Stream: its goroutine calls Stream until the last item and
// queues the items for the harness to resolve, the way the ui re-arms a stream command.
type stream struct{ ch chan pluginapi.Completion }

// startStream runs w.Stream on its own goroutine. The stream may block (a process that
// keeps running, a watcher waiting for changes): Settle does not wait for it, it only
// waits until every stream goroutine is either blocked inside the FakeOS or finished.
func (h *Harness) startStream(w pluginapi.Work, ctx context.Context) {
	s := &stream{ch: make(chan pluginapi.Completion, 4096)}
	h.streams = append(h.streams, s)
	h.active.Add(1)
	go func() {
		defer h.active.Add(-1)
		defer close(s.ch)
		for {
			c, ok := w.Stream(ctx)
			if c.Token == 0 {
				c.Token = w.Token // as the ui does
			}
			if !ok {
				// The ui delivers the last completion of a stream that ended, if it carries data.
				if c.Values != nil || c.Err != "" || c.Kind != "" {
					c.More = false
					s.ch <- c
				}
				return
			}
			s.ch <- c
			if !c.More {
				return
			}
		}
	}()
}

// pollStreams resolves the items the streams produced and waits until every stream
// goroutine is blocked in the FakeOS (or finished). With another OS it cannot tell blocked
// from busy, so it waits until the streams were quiet for foreignIdle. It reports whether it
// resolved anything.
func (h *Harness) pollStreams() bool {
	delivered := false
	deadline := time.Now().Add(10 * time.Second)
	lastItem := time.Now()
	for {
		// Read the counts before draining: whatever a goroutine sent before it was counted
		// idle is in the channel when the drain below looks.
		active := int(h.active.Load())
		blocked := 0
		if h.OS != nil {
			blocked = h.OS.BlockedWaiters()
		}
		for i := 0; i < len(h.streams); i++ {
		drain:
			for {
				select {
				case c, ok := <-h.streams[i].ch:
					if !ok {
						h.streams = append(h.streams[:i], h.streams[i+1:]...)
						i--
						break drain
					}
					h.RT.Resolve(c)
					delivered = true
					lastItem = time.Now()
				default:
					break drain
				}
			}
		}
		switch {
		case active == 0:
			return delivered
		case h.OS != nil && active <= blocked:
			return delivered
		case h.OS == nil && time.Since(lastItem) > foreignIdle:
			return delivered
		}
		if time.Now().After(deadline) {
			h.T.Fatal("plugintest: a stream neither blocked nor finished")
		}
		time.Sleep(20 * time.Microsecond)
	}
}
