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
