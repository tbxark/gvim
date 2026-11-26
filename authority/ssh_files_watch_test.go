package authority

import (
	"context"
	"testing"
	"time"

	"github.com/tbxark/gvim/plugin/hostos"
)

func TestSSHWatchPaceBacksOff(t *testing.T) {
	t.Parallel()
	p := defaultSSHWatchPace
	t.Run("a changing path keeps the base interval", func(t *testing.T) {
		t.Parallel()
		for quiet := 1; quiet <= p.idlePolls; quiet++ {
			if got := p.afterQuiet(p.base, quiet); got != p.base {
				t.Fatalf("quiet poll %d: delay %v, want %v", quiet, got, p.base)
			}
		}
	})
	t.Run("an idle path doubles up to the idle cap", func(t *testing.T) {
		t.Parallel()
		delay := p.base
		var seen []time.Duration
		for quiet := p.idlePolls + 1; quiet < p.idlePolls+10; quiet++ {
			delay = p.afterQuiet(delay, quiet)
			seen = append(seen, delay)
		}
		if seen[0] != 2*p.base {
			t.Errorf("first idle delay = %v, want %v", seen[0], 2*p.base)
		}
		if last := seen[len(seen)-1]; last != p.idleMax {
			t.Errorf("idle delay settled at %v, want %v", last, p.idleMax)
		}
		for _, d := range seen {
			if d > p.idleMax {
				t.Errorf("idle delay %v is over the cap %v", d, p.idleMax)
			}
		}
	})
	t.Run("a down carrier doubles up to the offline cap", func(t *testing.T) {
		t.Parallel()
		delay := p.base
		for range 10 {
			next := p.afterFailure(delay)
			if next <= delay && delay < p.offlineMax {
				t.Fatalf("delay %v did not grow to %v", delay, next)
			}
			delay = next
		}
		if delay != p.offlineMax {
			t.Errorf("offline delay settled at %v, want %v", delay, p.offlineMax)
		}
	})
}

// TestSSHPollWatchPausesWhileDisconnected checks a watcher whose carrier is down runs no
// poll, so it neither issues commands nor emits error events, and polls again once the
// carrier is back.
func TestSSHPollWatchPausesWhileDisconnected(t *testing.T) {
	t.Parallel()
	b := &sshBackend{} // never connected: every poll would fail
	w := &sshPollWatch{
		files: newSSHFiles(b), root: "/remote", dir: true, debounce: time.Millisecond,
		events: make(chan hostos.WatchEvent, 32),
		stop:   make(chan struct{}),
		done:   make(chan struct{}),
		pace:   sshWatchPace{base: 2 * time.Millisecond, idlePolls: 2, idleMax: 8 * time.Millisecond, offlineMax: 8 * time.Millisecond},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	go w.loop(ctx)
	defer w.Stop()

	time.Sleep(150 * time.Millisecond)
	if n := len(w.events); n != 0 {
		t.Fatalf("a watcher of a down carrier emitted %d events", n)
	}

	b.mu.Lock()
	b.connected = true
	b.mu.Unlock()
	select {
	case ev := <-w.events:
		if ev.Kind != "error" {
			t.Fatalf("event after reconnect = %+v, want the failed poll's error", ev)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the watcher did not poll again after the carrier came back")
	}
}
