package plugintest_test

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/tbxark/gvim/plugin"
	"github.com/tbxark/gvim/plugin/plugintest"
	"github.com/tbxark/gvim/pluginapi"
)

func TestFakeOSFiles(t *testing.T) {
	ctx := context.Background()
	f := plugintest.NewFakeOS()
	f.SetFile("/work/a/b.txt", "hello")
	f.SetFile("/work/a/.hidden", "h")
	if got, err := f.ReadFile(ctx, "a/b.txt", 100); err != nil || string(got) != "hello" {
		t.Fatalf("ReadFile = %q, %v", got, err)
	}
	if _, err := f.ReadFile(ctx, "a/b.txt", 3); pluginapi.CodeOf(err) != pluginapi.CodeTooLarge {
		t.Fatalf("limit: %v", err)
	}
	if _, err := f.ReadFile(ctx, "nope", 3); pluginapi.CodeOf(err) != pluginapi.CodeNotFound {
		t.Fatalf("missing: %v", err)
	}
	if err := f.WriteFile(ctx, "/work/x/y", []byte("1"), plugin.WriteOptions{}); pluginapi.CodeOf(err) != pluginapi.CodeNotFound {
		t.Fatalf("write without parents: %v", err)
	}
	if err := f.WriteFile(ctx, "/work/x/y", []byte("1"), plugin.WriteOptions{Parents: true}); err != nil {
		t.Fatal(err)
	}
	if err := f.WriteFile(ctx, "/work/x/y", []byte("2"), plugin.WriteOptions{Append: true}); err != nil {
		t.Fatal(err)
	}
	if got, _ := f.File("/work/x/y"); got != "12" {
		t.Fatalf("append = %q", got)
	}
	ents, _ := f.ReadDir(ctx, "/work/a", false)
	if len(ents) != 1 || ents[0].Name != "b.txt" || ents[0].Kind != "file" || ents[0].Size != 5 {
		t.Fatalf("ReadDir = %+v", ents)
	}
	if err := f.Rename(ctx, "/work/a", "/work/z"); err != nil {
		t.Fatal(err)
	}
	if got, ok := f.File("/work/z/b.txt"); !ok || got != "hello" || f.Exists("/work/a/b.txt") {
		t.Fatal("rename of a directory did not move its content")
	}
	if err := f.Copy(ctx, "/work/z", "/work/c"); err != nil || !f.Exists("/work/c/b.txt") {
		t.Fatalf("copy: %v", err)
	}
	if err := f.Remove(ctx, "/work/z", false); pluginapi.CodeOf(err) != pluginapi.CodeIO {
		t.Fatalf("non-recursive remove of a full directory: %v", err)
	}
	if err := f.Remove(ctx, "/work/z", true); err != nil || f.Exists("/work/z") {
		t.Fatalf("recursive remove: %v", err)
	}
	res, err := f.Walk(ctx, "/work", plugin.WalkOptions{})
	if err != nil {
		t.Fatal(err)
	}
	var rels []string
	for _, e := range res.Entries {
		rels = append(rels, e.Rel)
	}
	if want := []string{"c", "c/b.txt", "x", "x/y"}; !reflect.DeepEqual(rels, want) {
		t.Fatalf("Walk = %v, want %v", rels, want)
	}
}

func TestFakeOSProcesses(t *testing.T) {
	ctx := context.Background()
	f := plugintest.NewFakeOS()
	f.Script([]string{"git", "show"}, plugintest.ProcScript{Stdout: "blob", Code: 0})
	f.Script([]string{"git"}, plugintest.ProcScript{Stderr: "usage", Code: 129})
	res, err := f.Run(ctx, []string{"git", "show", "HEAD:x"}, plugin.ProcOptions{})
	if err != nil || string(res.Stdout) != "blob" {
		t.Fatalf("Run = %+v, %v", res, err)
	}
	if res, _ := f.Run(ctx, []string{"git", "status"}, plugin.ProcOptions{}); res.Code != 129 || string(res.Stderr) != "usage" {
		t.Fatalf("the first matching script must win by order: %+v", res)
	}
	if _, err := f.Run(ctx, []string{"other"}, plugin.ProcOptions{}); pluginapi.CodeOf(err) != pluginapi.CodeExec {
		t.Fatalf("unscripted: %v", err)
	}

	f.Script([]string{"srv"}, plugintest.ProcScript{Hold: true, Chunks: []plugin.ProcChunk{{Data: []byte("up")}}})
	p, err := f.Start(ctx, []string{"srv"}, plugin.ProcOptions{OpenStdin: true})
	if err != nil {
		t.Fatal(err)
	}
	if c, ok := p.Output(ctx); !ok || string(c.Data) != "up" {
		t.Fatalf("Output = %+v, %v", c, ok)
	}
	// a held process blocks Output until it ends; the fake counts the blocked goroutine
	got := make(chan bool)
	go func() { _, ok := p.Output(ctx); got <- ok }()
	for f.BlockedWaiters() != 1 {
		time.Sleep(time.Millisecond)
	}
	if err := p.Kill("KILL"); err != nil {
		t.Fatal(err)
	}
	if ok := <-got; ok {
		t.Fatal("Output after the process ended returned data")
	}
	if n := f.BlockedWaiters(); n != 0 {
		t.Fatalf("blocked = %d after the wake-up", n)
	}
	r, err := p.Wait(ctx)
	if err != nil || r.Signal != "KILL" || r.Code != -1 {
		t.Fatalf("Wait = %+v, %v", r, err)
	}
	if err := p.Write([]byte("x")); pluginapi.CodeOf(err) != pluginapi.CodeClosed {
		t.Fatalf("Write after exit: %v", err)
	}
}

func TestFakeOSWatch(t *testing.T) {
	f := plugintest.NewFakeOS()
	f.SetFile("/work/a.txt", "1")
	w, err := f.Watch(context.Background(), "/work", plugin.WatchOptions{Debounce: 7 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	f.Emit("/work/a.txt", "write")
	e, ok := w.Next(context.Background())
	if !ok || e.Path != "/work/a.txt" || e.Kind != "write" {
		t.Fatalf("Next = %+v, %v", e, ok)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, ok := w.Next(ctx); ok {
		t.Fatal("event from nowhere")
	}
	if n := f.BlockedWaiters(); n != 0 {
		t.Fatalf("a cancelled waiter is still counted: %d", n)
	}
	w.Stop()
	if _, ok := w.Next(context.Background()); ok {
		t.Fatal("Next after Stop")
	}
	if _, err := f.Watch(context.Background(), "/missing", plugin.WatchOptions{}); pluginapi.CodeOf(err) != pluginapi.CodeNotFound {
		t.Fatalf("watch of a missing path: %v", err)
	}
	f.FailWatch("/work", pluginapi.Errf(pluginapi.CodeLimit, "too big"))
	if _, err := f.Watch(context.Background(), "/work", plugin.WatchOptions{}); pluginapi.CodeOf(err) != pluginapi.CodeLimit {
		t.Fatalf("scripted failure: %v", err)
	}
}
