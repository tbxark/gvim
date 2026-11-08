package app

import (
	"os"
	"strings"
	"testing"

	"github.com/tbxark/gvim/internal/daemon"
)

// autoAttachTestDaemon starts a bare listener and writes its host mark so a plan can see
// a live daemon without starting a full editor.
func autoAttachTestDaemon(t *testing.T, root, name string) {
	t.Helper()
	ln, _, err := daemon.Listen(name)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	canonical := root
	if resolved, cerr := daemon.CanonicalRoot(root); cerr == nil {
		canonical = resolved
	}
	if err := writeHostMark(canonical, name); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { clearHostMark(canonical, name) })
}

func TestAutoAttachPlan(t *testing.T) {
	daemonTestHome(t, "autoattach")
	root := t.TempDir()
	cfgPath := writeAutoAttachConfig(t, true)
	opts := Options{Root: root, Config: cfgPath}
	if !autoAttachEligible(opts) {
		t.Fatal("a bare launch is not auto-attach eligible")
	}
	// No daemon: nothing to attach to.
	if _, _, ok := autoAttachPlan(opts); ok {
		t.Fatal("a plan found a daemon before one started")
	}
	name := daemon.DefaultName(root)
	autoAttachTestDaemon(t, root, name)
	gotName, notice, ok := autoAttachPlan(opts)
	if !ok || gotName != name || !strings.Contains(notice, "auto-attach") {
		t.Fatalf("plan = %q, %q, %v; want name %q and a notice", gotName, notice, ok, name)
	}
	// The setting is off: no attach, even with a live daemon.
	off := Options{Root: root, Config: writeAutoAttachConfig(t, false)}
	if _, _, ok := autoAttachPlan(off); ok {
		t.Fatal("auto_attach false still planned an attach")
	}
	// Direct-mode shapes never auto attach.
	for _, mutate := range []func(*Options){
		func(o *Options) { o.Files = []FileTarget{{Path: "a.txt"}} },
		func(o *Options) { o.Diff = true },
		func(o *Options) { o.Simple = true },
		func(o *Options) { o.ReadStdin = true },
		func(o *Options) { o.NoRestore = true },
	} {
		direct := opts
		mutate(&direct)
		if autoAttachEligible(direct) {
			t.Fatalf("direct-mode launch is auto-attach eligible: %+v", direct)
		}
	}
}

func writeAutoAttachConfig(t *testing.T, on bool) string {
	t.Helper()
	body := `{"auto_attach": false}`
	if on {
		body = `{"auto_attach": true}`
	}
	path := t.TempDir() + "/config.json"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}
