package plugin

import (
	"context"
	"errors"
	"testing"
	"time"
)

// waitCause waits up to d for the slice context to end and returns its cause.
func waitCause(s *slice, d time.Duration) error {
	select {
	case <-s.ctx.Done():
		return context.Cause(s.ctx)
	case <-time.After(d):
		return nil
	}
}

func TestSliceDeadlines(t *testing.T) {
	const budget = 20 * time.Millisecond
	tests := []struct {
		name string
		run  func(s *slice)
		want error
	}{
		{"soft deadline", func(s *slice) {}, errSliceDeadline},
		{"pause suspends the soft deadline until the hard one", func(s *slice) { s.pause() }, errHardDeadline},
		{"resume with budget left re-arms the soft deadline", func(s *slice) {
			s.pause()
			s.resume()
		}, errSliceDeadline},
		{"resume with the budget used up cancels at once", func(s *slice) {
			time.Sleep(budget + 5*time.Millisecond)
			s.pause()
			s.resume()
		}, errSliceDeadline},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := New(Options{})
			s := r.newSlice(context.Background(), budget)
			defer s.finish()
			tt.run(s)
			if got := waitCause(s, 5*time.Second); !errors.Is(got, tt.want) {
				t.Fatalf("cause = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSlicePausedTimeIsNotCharged(t *testing.T) {
	const budget = 60 * time.Millisecond
	r := New(Options{})
	s := r.newSlice(context.Background(), budget)
	s.pause()
	time.Sleep(2 * budget) // past the soft deadline, inside the hard one
	if s.ctx.Err() != nil {
		t.Fatalf("slice ended while paused: %v", context.Cause(s.ctx))
	}
	s.resume()
	if s.hostTime < 2*budget {
		t.Errorf("hostTime = %v, want at least %v", s.hostTime, 2*budget)
	}
	if s.ctx.Err() != nil {
		t.Fatalf("slice ended right after resume: %v", context.Cause(s.ctx))
	}
	s.finish()
	if got := context.Cause(s.ctx); !errors.Is(got, errSliceDone) {
		t.Errorf("cause after finish = %v, want %v", got, errSliceDone)
	}
}

func TestSliceFinishStopsTheTimer(t *testing.T) {
	r := New(Options{})
	s := r.newSlice(context.Background(), 10*time.Millisecond)
	s.finish()
	time.Sleep(40 * time.Millisecond)
	if got := context.Cause(s.ctx); !errors.Is(got, errSliceDone) {
		t.Errorf("cause = %v, want %v: a deadline fired after finish", got, errSliceDone)
	}
	s.finish() // a second finish is harmless
}

func TestSliceStartsGuardOnlyWhenLong(t *testing.T) {
	r := New(Options{})
	s := r.newSlice(context.Background(), time.Second)
	s.mu.Lock()
	pending := !s.guardAt.IsZero()
	s.mu.Unlock()
	if !pending {
		t.Fatal("the guard start is not scheduled")
	}
	s.finish()
	time.Sleep(2 * guardStartAfter)
	s.mu.Lock()
	started := s.guardAt.IsZero()
	s.mu.Unlock()
	if started {
		t.Error("a slice that ended before guardStartAfter started the heap guard")
	}
}
