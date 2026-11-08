package app

import "testing"

// TestBuildRuntimeStartsTheWatcherOnlyWhenRunning checks that the watcher of the
// tree root is started for a run and is closed with the runtime, and that a build
// for a test does not start one.
func TestBuildRuntimeStartsTheWatcherOnlyWhenRunning(t *testing.T) {
	isolateConfig(t)
	dir := t.TempDir()
	quiet, err := buildRuntime(t.Context(), Options{Root: dir}, false)
	must(t, err)
	if quiet.watcher != nil {
		t.Fatal("a build without a run started a watcher")
	}
	quiet.close()

	running, err := buildRuntime(t.Context(), Options{Root: dir}, true)
	must(t, err)
	if running.watcher == nil {
		t.Fatal("a run did not start the watcher")
	}
	running.close()
}
