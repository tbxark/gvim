package terminal

import (
	"testing"
	"time"
)

func TestInboxQueuesUntilClosed(t *testing.T) {
	q := newInbox()
	if !q.push([]byte("ab")) {
		t.Fatal("push on an open inbox = false, want true")
	}
	if !q.push([]byte("cd")) {
		t.Fatal("second push on an open inbox = false, want true")
	}
	got, ok := q.take()
	if !ok || string(got) != "abcd" {
		t.Fatalf("take() = %q, %v; want \"abcd\", true", got, ok)
	}

	q.close()
	if _, ok := q.take(); ok {
		t.Fatal("take() after close ok = true, want false")
	}
	if q.push([]byte("x")) {
		t.Fatal("push after close = true, want false")
	}
}

func TestInboxTakeBlocksUntilPushOrClose(t *testing.T) {
	tests := []struct {
		name   string
		finish func(q *inbox)
		wantOK bool
		want   string
	}{
		{
			name:   "push wakes take",
			finish: func(q *inbox) { q.push([]byte("x")) },
			wantOK: true,
			want:   "x",
		},
		{
			name:   "close wakes take",
			finish: func(q *inbox) { q.close() },
			wantOK: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			q := newInbox()
			type result struct {
				b  []byte
				ok bool
			}
			done := make(chan result, 1)
			go func() {
				b, ok := q.take()
				done <- result{b, ok}
			}()
			tt.finish(q)
			select {
			case r := <-done:
				if r.ok != tt.wantOK || string(r.b) != tt.want {
					t.Fatalf("take() = %q, %v; want %q, %v", r.b, r.ok, tt.want, tt.wantOK)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("take() did not return within 3s")
			}
		})
	}
}

func TestInboxBounded(t *testing.T) {
	q := newInbox()
	chunk := make([]byte, maxInboxBytes/2)
	for range 2 {
		if !q.push(chunk) {
			t.Fatal("pushes up to maxInboxBytes failed")
		}
	}
	if err := q.tryPush([]byte("overflow")); err != errInboxFull {
		t.Fatalf("tryPush past cap = %v, want %v", err, errInboxFull)
	}
	if q.push([]byte("overflow")) {
		t.Fatal("push past cap = true, want false")
	}
	got, ok := q.take()
	if !ok || len(got) != maxInboxBytes {
		t.Fatalf("take() len = %d ok = %v, want %d true", len(got), ok, maxInboxBytes)
	}
	if !q.push([]byte("after")) {
		t.Fatal("push after drain = false, want true")
	}
}
