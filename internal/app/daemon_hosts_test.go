package app

import (
	"net"
	"os"
	"testing"

	"github.com/tbxark/gvim/internal/daemon"
)

// The daemon host marks: a daemon claims its workspace root, a direct launch sees the live
// claims (including explicitly named daemons), and stale marks disappear on read.

// liveTestSocket binds a real Unix socket at the daemon's path for name and returns a
// cleanup that removes it.
func liveTestSocket(t *testing.T, name string) {
	t.Helper()
	path, err := daemon.SocketPath(name)
	if err != nil {
		t.Fatal(err)
	}
	ln, err := net.Listen("unix", path)
	if err != nil {
		t.Fatalf("listen %s: %v", path, err)
	}
	t.Cleanup(func() {
		_ = ln.Close()
		_ = os.Remove(path)
	})
}

func TestDaemonHostMarks(t *testing.T) {
	isolateConfig(t)
	daemonTestHome(t, "hostmarks")
	root := t.TempDir()
	canonical, err := daemon.CanonicalRoot(root)
	if err != nil {
		t.Fatal(err)
	}

	// No marks: nothing live.
	if got := liveHostsFor(canonical); len(got) != 0 {
		t.Fatalf("liveHostsFor with no marks = %+v", got)
	}

	// A mark without a live socket is stale: ignored and removed.
	if err := writeHostMark(canonical, "named-a"); err != nil {
		t.Fatal(err)
	}
	if got := liveHostsFor(canonical); len(got) != 0 {
		t.Fatalf("a dead daemon's mark counted as live: %+v", got)
	}
	if path, err := markPath(canonical, "named-a"); err == nil {
		if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
			t.Errorf("the stale mark was not removed (stat %v)", statErr)
		}
	}

	// A live (named) daemon is visible, and clearing the mark hides it.
	liveTestSocket(t, "named-a")
	if err := writeHostMark(canonical, "named-a"); err != nil {
		t.Fatal(err)
	}
	got := liveHostsFor(canonical)
	if len(got) != 1 || got[0].Name != "named-a" || got[0].Root != canonical {
		t.Fatalf("liveHostsFor = %+v", got)
	}
	clearHostMark(canonical, "named-a")
	if got := liveHostsFor(canonical); len(got) != 0 {
		t.Fatalf("a cleared mark is still live: %+v", got)
	}

	// Several named daemons on one root are all visible, sorted by name.
	liveTestSocket(t, "named-b")
	liveTestSocket(t, "named-c")
	for _, name := range []string{"named-c", "named-b"} {
		if err := writeHostMark(canonical, name); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() { clearHostMark(canonical, "named-b"); clearHostMark(canonical, "named-c") })
	got = liveHostsFor(canonical)
	if len(got) != 2 || got[0].Name != "named-b" || got[1].Name != "named-c" {
		t.Fatalf("multiple marks = %+v", got)
	}
}
