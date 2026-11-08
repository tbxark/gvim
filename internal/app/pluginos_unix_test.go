//go:build !windows

package app

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/tbxark/gvim/plugin"
	"github.com/tbxark/gvim/plugin/plugintest"
	"github.com/tbxark/gvim/pluginapi"
)

// alive reports whether a process with this pid exists (the test binary reaps its own
// children, so a killed one is gone).
func alive(pid int) bool {
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}

func requireProgram(t *testing.T, name string) {
	t.Helper()
	if _, ok := newPluginOS(nil).LookPath(name); !ok {
		t.Skipf("%s is not installed", name)
	}
}

// collect drains a started process: all chunks, then the result.
func collect(t *testing.T, p plugin.Proc) (stdout, stderr string, res plugin.ProcResult) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	var so, se bytes.Buffer
	for {
		c, ok := p.Output(ctx)
		if !ok {
			break
		}
		if c.Stderr {
			se.Write(c.Data)
		} else {
			so.Write(c.Data)
		}
	}
	res, err := p.Wait(ctx)
	if err != nil {
		t.Fatal(err)
	}
	return so.String(), se.String(), res
}

func TestPluginOSRun(t *testing.T) {
	requireProgram(t, "sh")
	o, dir := newTestOS(t)
	ctx := context.Background()
	sh := func(script string, opts plugin.ProcOptions) (plugin.ProcResult, error) {
		return o.Run(ctx, []string{"sh", "-c", script}, opts)
	}
	t.Run("output and exit code", func(t *testing.T) {
		res, err := sh("echo out; echo err >&2; exit 3", plugin.ProcOptions{})
		if err != nil || res.Code != 3 || string(res.Stdout) != "out\n" || string(res.Stderr) != "err\n" || res.Signal != "" || res.TimedOut {
			t.Fatalf("res = %+v, err = %v", res, err)
		}
	})
	t.Run("merged stderr", func(t *testing.T) {
		res, err := sh("echo out; echo err >&2", plugin.ProcOptions{MergeStderr: true})
		if err != nil || string(res.Stdout) != "out\nerr\n" || len(res.Stderr) != 0 {
			t.Fatalf("res = %+v, err = %v", res, err)
		}
	})
	t.Run("environment cwd and stdin", func(t *testing.T) {
		res, err := sh(`pwd; echo "$GVIM_X"; cat`, plugin.ProcOptions{Env: map[string]string{"GVIM_X": "added"}, Stdin: []byte("from stdin"), Cwd: "."})
		want := dir + "\nadded\nfrom stdin"
		if err != nil || string(res.Stdout) != want {
			t.Fatalf("stdout = %q, err = %v; want %q", res.Stdout, err, want)
		}
	})
	t.Run("output limit", func(t *testing.T) {
		res, err := sh("head -c 100000 /dev/zero", plugin.ProcOptions{MaxOutput: 1000})
		if err != nil || len(res.Stdout) != 1000 || !res.Truncated || res.Code != 0 {
			t.Fatalf("len = %d truncated = %v code = %d err = %v signal=%q timedout=%v", len(res.Stdout), res.Truncated, res.Code, err, res.Signal, res.TimedOut)
		}
	})
	t.Run("timeout kills the whole group", func(t *testing.T) {
		begin := time.Now()
		res, err := sh("sleep 30 & echo $!; wait", plugin.ProcOptions{Timeout: 200 * time.Millisecond})
		if err != nil || !res.TimedOut || res.Signal != "KILL" {
			t.Fatalf("res = %+v, err = %v", res, err)
		}
		if d := time.Since(begin); d > 5*time.Second {
			t.Fatalf("took %v", d)
		}
		pid, perr := strconv.Atoi(strings.TrimSpace(string(res.Stdout)))
		if perr != nil {
			t.Fatalf("stdout = %q", res.Stdout)
		}
		eventually(t, "the grandchild to die", func() bool { return !alive(pid) })
	})
	t.Run("could not start", func(t *testing.T) {
		if _, err := o.Run(ctx, []string{"gvim-no-such-program"}, plugin.ProcOptions{}); codeOf(err) != pluginapi.CodeExec {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("cancel terminates", func(t *testing.T) {
		c, cancel := context.WithCancel(ctx)
		go func() { time.Sleep(100 * time.Millisecond); cancel() }()
		begin := time.Now()
		_, err := o.Run(c, []string{"sleep", "30"}, plugin.ProcOptions{})
		if codeOf(err) != pluginapi.CodeCancelled || time.Since(begin) > 5*time.Second {
			t.Fatalf("err = %v after %v", err, time.Since(begin))
		}
	})
}

func TestPluginOSStart(t *testing.T) {
	requireProgram(t, "sh")
	o, _ := newTestOS(t)
	ctx := context.Background()

	t.Run("streams both pipes", func(t *testing.T) {
		p, err := o.Start(ctx, []string{"sh", "-c", "echo out; echo err >&2; exit 4"}, plugin.ProcOptions{})
		if err != nil {
			t.Fatal(err)
		}
		if p.PID() <= 0 {
			t.Fatalf("pid = %d", p.PID())
		}
		so, se, res := collect(t, p)
		if so != "out\n" || se != "err\n" || res.Code != 4 {
			t.Fatalf("stdout %q stderr %q res %+v", so, se, res)
		}
	})
	t.Run("batches output", func(t *testing.T) {
		p, err := o.Start(ctx, []string{"sh", "-c", "i=0; while [ $i -lt 200 ]; do echo line$i; i=$((i+1)); done"}, plugin.ProcOptions{})
		if err != nil {
			t.Fatal(err)
		}
		chunks, total := 0, 0
		for {
			c, ok := p.Output(ctx)
			if !ok {
				break
			}
			chunks++
			total += len(c.Data)
		}
		if chunks > 20 || total < 1000 {
			t.Fatalf("%d chunks for %d bytes: output is not batched", chunks, total)
		}
	})
	t.Run("stdin through the queue", func(t *testing.T) {
		requireProgram(t, "cat")
		p, err := o.Start(ctx, []string{"cat"}, plugin.ProcOptions{OpenStdin: true})
		if err != nil {
			t.Fatal(err)
		}
		if err := p.Write([]byte("hel")); err != nil {
			t.Fatal(err)
		}
		if err := p.Write([]byte("lo")); err != nil {
			t.Fatal(err)
		}
		p.CloseStdin()
		if err := p.Write([]byte("late")); codeOf(err) != pluginapi.CodeClosed {
			t.Fatalf("write after close: %v", err)
		}
		so, _, res := collect(t, p)
		if so != "hello" || res.Code != 0 {
			t.Fatalf("stdout %q res %+v", so, res)
		}
	})
	t.Run("write without a pipe", func(t *testing.T) {
		p, err := o.Start(ctx, []string{"sleep", "30"}, plugin.ProcOptions{})
		if err != nil {
			t.Fatal(err)
		}
		defer p.Terminate(time.Millisecond)
		if err := p.Write([]byte("x")); codeOf(err) != pluginapi.CodeClosed {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("a child that never reads makes Write busy, never block", func(t *testing.T) {
		p, err := o.Start(ctx, []string{"sleep", "30"}, plugin.ProcOptions{OpenStdin: true})
		if err != nil {
			t.Fatal(err)
		}
		defer p.Terminate(time.Millisecond)
		chunk := bytes.Repeat([]byte("x"), 1<<20)
		begin := time.Now()
		ok, busy := 0, 0
		for range 20 {
			switch err := p.Write(chunk); {
			case err == nil:
				ok++
			case codeOf(err) == pluginapi.CodeBusy:
				busy++
			default:
				t.Fatalf("err = %v", err)
			}
		}
		if d := time.Since(begin); d > 3*time.Second {
			t.Fatalf("writes took %v", d)
		}
		if busy == 0 || ok < 7 || ok > 9 {
			t.Fatalf("ok = %d busy = %d, want about 8 MiB accepted", ok, busy)
		}
	})
	t.Run("kill reaches the group", func(t *testing.T) {
		p, err := o.Start(ctx, []string{"sh", "-c", "sleep 30 & echo $!; wait"}, plugin.ProcOptions{})
		if err != nil {
			t.Fatal(err)
		}
		c, ok := p.Output(ctx)
		pid, perr := strconv.Atoi(strings.TrimSpace(string(c.Data)))
		if !ok || perr != nil {
			t.Fatalf("chunk = %q", c.Data)
		}
		if err := p.Kill("KILL"); err != nil {
			t.Fatal(err)
		}
		_, _, res := collect(t, p)
		if res.Signal != "KILL" || res.Code != -1 {
			t.Fatalf("res = %+v", res)
		}
		eventually(t, "the grandchild to die", func() bool { return !alive(pid) })
		if err := p.Kill(""); codeOf(err) != pluginapi.CodeClosed {
			t.Fatalf("kill after exit: %v", err)
		}
	})
	t.Run("terminate sends TERM, then KILL after the grace period", func(t *testing.T) {
		// the shell ignores TERM, so only the second signal can end it
		p, err := o.Start(ctx, []string{"sh", "-c", "trap '' TERM; echo ready; while :; do sleep 0.05; done"}, plugin.ProcOptions{})
		if err != nil {
			t.Fatal(err)
		}
		if _, ok := p.Output(ctx); !ok {
			t.Fatal("no output")
		}
		begin := time.Now()
		p.Terminate(300 * time.Millisecond)
		p.Terminate(300 * time.Millisecond) // idempotent
		_, _, res := collect(t, p)
		if res.Signal != "KILL" {
			t.Fatalf("res = %+v", res)
		}
		if d := time.Since(begin); d < 250*time.Millisecond || d > 5*time.Second {
			t.Fatalf("ended after %v", d)
		}
	})
	t.Run("timeout", func(t *testing.T) {
		p, err := o.Start(ctx, []string{"sleep", "30"}, plugin.ProcOptions{Timeout: 150 * time.Millisecond})
		if err != nil {
			t.Fatal(err)
		}
		_, _, res := collect(t, p)
		if !res.TimedOut {
			t.Fatalf("res = %+v", res)
		}
	})
	t.Run("could not start", func(t *testing.T) {
		if _, err := o.Start(ctx, []string{"gvim-no-such-program"}, plugin.ProcOptions{OpenStdin: true}); codeOf(err) != pluginapi.CodeExec {
			t.Fatalf("err = %v", err)
		}
	})
}

// TestOSShutdownKillsEverythingAtOnce: Shutdown is SIGKILL for every group, no grace.
func TestOSShutdownKillsEverythingAtOnce(t *testing.T) {
	o := newPluginOS(nil)
	ctx := context.Background()
	trap := []string{"sh", "-c", "trap '' TERM; while :; do sleep 0.05; done"}
	p1, err := o.Start(ctx, trap, plugin.ProcOptions{})
	if err != nil {
		t.Fatal(err)
	}
	p2, err := o.Start(ctx, []string{"sleep", "30"}, plugin.ProcOptions{})
	if err != nil {
		t.Fatal(err)
	}
	begin := time.Now()
	o.Shutdown()
	for _, p := range []plugin.Proc{p1, p2} {
		_, _, res := collect(t, p)
		if res.Signal != "KILL" {
			t.Fatalf("res = %+v", res)
		}
	}
	if d := time.Since(begin); d > time.Second {
		t.Fatalf("Shutdown took %v to kill", d)
	}
	if _, err := o.Start(ctx, []string{"sleep", "1"}, plugin.ProcOptions{}); codeOf(err) != pluginapi.CodeClosed {
		t.Fatalf("Start after Shutdown: %v", err)
	}
	if _, err := o.Run(ctx, []string{"true"}, plugin.ProcOptions{}); codeOf(err) != pluginapi.CodeClosed {
		t.Fatalf("Run after Shutdown: %v", err)
	}
	o.Shutdown() // idempotent
}

// TestOSKillOnStopIsImmediate: with the real runtime, Runtime.Stop kills a plugin's child
// at once (SIGKILL to the group), it does not wait for a grace period.
func TestOSKillOnStopIsImmediate(t *testing.T) {
	requireProgram(t, "sh")
	o, _ := newTestOS(t)
	h := plugintest.New(t, plugintest.Files{"t.lua": `
local gvim = require "gvim"
-- ignores SIGTERM: only SIGKILL ends it
local p = gvim.proc.spawn({"sh", "-c", "trap '' TERM; while :; do sleep 0.05; done"})
gvim.notify("pid " .. tostring(p:wait_started()))`}, plugintest.Options{OS: o})
	h.Start()
	var pid int
	eventually(t, "the child to start", func() bool {
		h.Settle()
		for _, n := range h.NoticeTexts() {
			if p, ok := strings.CutPrefix(n, "pid "); ok {
				pid, _ = strconv.Atoi(p)
			}
		}
		return pid > 0
	})
	if !alive(pid) {
		t.Fatal("the child is not running")
	}
	begin := time.Now()
	h.RT.Stop()
	eventually(t, "the child to die", func() bool { return !alive(pid) })
	if d := time.Since(begin); d > 2*time.Second {
		t.Fatalf("Stop took %v to kill the child", d)
	}
}

// TestPluginUnloadTerminatesChildren: unloading a plugin sends TERM now and KILL after the
// grace period, asynchronously.
func TestPluginUnloadTerminatesChildren(t *testing.T) {
	requireProgram(t, "sh")
	o, _ := newTestOS(t)
	h := plugintest.New(t, plugintest.Files{"t.lua": `
local gvim = require "gvim"
local p = gvim.proc.spawn({"sleep", "30"})
gvim.notify("pid " .. tostring(p:wait_started()))`}, plugintest.Options{OS: o})
	h.Start()
	var pid int
	eventually(t, "the child to start", func() bool {
		h.Settle()
		for _, n := range h.NoticeTexts() {
			if p, ok := strings.CutPrefix(n, "pid "); ok {
				pid, _ = strconv.Atoi(p)
			}
		}
		return pid > 0
	})
	begin := time.Now()
	h.Unload("t")
	if d := time.Since(begin); d > time.Second {
		t.Fatalf("Unload blocked for %v", d)
	}
	eventually(t, "the child to die", func() bool { return !alive(pid) })
}

// TestOSTablesRaceFree hammers the process and watcher tables from many goroutines; it is
// meaningful under -race.
func TestOSTablesRaceFree(t *testing.T) {
	requireProgram(t, "sh")
	o, dir := newTestOS(t)
	ctx := context.Background()
	var wg sync.WaitGroup
	for i := range 12 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			switch i % 3 {
			case 0:
				_, _ = o.Run(ctx, []string{"sh", "-c", "echo hi"}, plugin.ProcOptions{})
			case 1:
				if p, err := o.Start(ctx, []string{"sh", "-c", "echo hi; sleep 0.1"}, plugin.ProcOptions{OpenStdin: true}); err == nil {
					_ = p.Write([]byte("x"))
					p.CloseStdin()
					for {
						if _, ok := p.Output(ctx); !ok {
							break
						}
					}
					_, _ = p.Wait(ctx)
				}
			case 2:
				if w, err := o.Watch(ctx, dir, plugin.WatchOptions{Debounce: 10 * time.Millisecond}); err == nil {
					go func() { _ = writePluginFile(dir, "w"+strconv.Itoa(i)) }()
					c, cancel := context.WithTimeout(ctx, 200*time.Millisecond)
					_, _ = w.Next(c)
					cancel()
					w.Stop()
				}
			}
		}()
	}
	wg.Add(1)
	go func() { defer wg.Done(); time.Sleep(50 * time.Millisecond); o.Shutdown() }()
	wg.Wait()
}

// TestTerminateKillsGroupAfterLeaderExit: the delayed KILL of Terminate reaches a grandchild
// that ignores TERM and holds no pipe, even though the direct child is long gone (S8-3).
func TestTerminateKillsGroupAfterLeaderExit(t *testing.T) {
	requireProgram(t, "sh")
	o, dir := newTestOS(t)
	pidfile := filepath.Join(dir, "grandchild.pid")
	script := "sh -c 'trap \"\" TERM; echo $$ > " + pidfile + "; sleep 30' >/dev/null 2>&1 & wait"
	p, err := o.Start(context.Background(), []string{"sh", "-c", script}, plugin.ProcOptions{})
	if err != nil {
		t.Fatal(err)
	}
	var gpid int
	eventually(t, "the grandchild to start", func() bool {
		b, err := os.ReadFile(pidfile)
		if err != nil {
			return false
		}
		gpid, _ = strconv.Atoi(strings.TrimSpace(string(b)))
		return gpid > 0
	})
	t.Cleanup(func() { _ = syscall.Kill(gpid, syscall.SIGKILL) })
	p.Terminate(300 * time.Millisecond)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := p.Wait(ctx); err != nil {
		t.Fatal(err)
	}
	if !alive(gpid) {
		t.Fatal("the grandchild died on TERM; the test needs one that ignores it")
	}
	eventually(t, "the grandchild to be killed after the grace period", func() bool { return !alive(gpid) })
}

// TestReadFileSpecialFile: a FIFO is refused without blocking (S8-6).
func TestReadFileSpecialFile(t *testing.T) {
	o, dir := newTestOS(t)
	fifo := filepath.Join(dir, "ff")
	if err := syscall.Mkfifo(fifo, 0o644); err != nil {
		t.Skipf("mkfifo: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := o.ReadFile(ctx, fifo, 100)
		done <- err
	}()
	select {
	case err := <-done:
		if codeOf(err) != pluginapi.CodeInvalid {
			t.Fatalf("err = %v, want invalid", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("ReadFile blocked on a FIFO")
	}
}

// TestCopyOntoItself: copying a file onto itself, a symlink to it or a hard link to it
// refuses and leaves the data alone (S8-1).
func TestCopyOntoItself(t *testing.T) {
	o, dir := newTestOS(t)
	ctx := context.Background()
	src := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(src, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("a.txt", filepath.Join(dir, "sym")); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(src, filepath.Join(dir, "hard")); err != nil {
		t.Fatal(err)
	}
	for _, dst := range []string{"a.txt", "./a.txt", "sym", "hard"} {
		t.Run(dst, func(t *testing.T) {
			if err := o.Copy(ctx, "a.txt", dst); codeOf(err) != pluginapi.CodeInvalid {
				t.Fatalf("err = %v, want invalid", err)
			}
			if b, _ := os.ReadFile(src); string(b) != "hello" {
				t.Fatalf("source = %q", b)
			}
		})
	}
	// Through a symlinked source as well.
	if err := o.Copy(ctx, "sym", "a.txt"); codeOf(err) != pluginapi.CodeInvalid {
		t.Fatalf("err = %v, want invalid", err)
	}
	if b, _ := os.ReadFile(src); string(b) != "hello" {
		t.Fatalf("source = %q", b)
	}
}

// TestRemoveRefusesProtectedByIdentity: the workspace, home and config directories (and what
// contains them) are recognised by file identity, not by spelling, so a case variant on a
// case-insensitive file system or a symlinked spelling does not slip past (S8-5).
func TestRemoveRefusesProtectedByIdentity(t *testing.T) {
	setup := func(t *testing.T) (o *pluginOS, dir string) {
		dir, err := filepath.EvalSymlinks(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		work := filepath.Join(dir, "work")
		home := filepath.Join(dir, "home")
		cfg := filepath.Join(home, ".config", "gvim")
		for _, d := range []string{work, cfg, filepath.Join(dir, "other", "x")} {
			if err := os.MkdirAll(d, 0o755); err != nil {
				t.Fatal(err)
			}
		}
		if err := os.Symlink(work, filepath.Join(dir, "wlink")); err != nil {
			t.Fatal(err)
		}
		o = newPluginOS(func() plugin.Dirs {
			return plugin.Dirs{Config: cfg, Home: home, Temp: dir, Cwd: dir, Root: filepath.Join(dir, "wlink")}
		})
		t.Cleanup(o.Shutdown)
		return o, dir
	}
	rows := []struct{ name, path string }{
		{"root named directly", "work"},
		{"config parent", "home/.config"},
		{"config", "home/.config/gvim"},
		{"home", "home"},
		{"cwd", "."},
	}
	if _, err := os.Stat(strings.ToUpper(t.TempDir())); err == nil { // case-insensitive file system
		rows = append(rows, struct{ name, path string }{"case variant of root", "WORK"},
			struct{ name, path string }{"case variant of home", "HOME"},
			struct{ name, path string }{"case variant of config", "home/.CONFIG/Gvim"})
	}
	for _, r := range rows {
		for _, rec := range []bool{true, false} {
			t.Run(fmt.Sprintf("%s/%v", r.name, rec), func(t *testing.T) {
				o, dir := setup(t)
				err := o.Remove(context.Background(), filepath.Join(dir, r.path), rec)
				if codeOf(err) != pluginapi.CodeInvalid {
					t.Fatalf("Remove(%q, %v) = %v, want invalid", r.path, rec, err)
				}
				for _, d := range []string{"work", "home/.config/gvim"} {
					if _, err := os.Stat(filepath.Join(dir, d)); err != nil {
						t.Fatalf("%s is gone", d)
					}
				}
			})
		}
	}
	t.Run("a sibling and a link go", func(t *testing.T) {
		o, dir := setup(t)
		if err := o.Remove(context.Background(), filepath.Join(dir, "other"), true); err != nil {
			t.Fatal(err)
		}
		if err := o.Remove(context.Background(), filepath.Join(dir, "wlink"), false); err != nil {
			t.Fatalf("removing the link itself: %v", err)
		}
		if _, err := os.Stat(filepath.Join(dir, "work")); err != nil {
			t.Fatal("the link target went with the link")
		}
	})
}

// A FIFO has no writer, so opening it for reading never returns. Copy of one must be
// refused like ReadFile refuses it, and not park its Work goroutine for good.
func TestCopyRefusesAFIFOSource(t *testing.T) {
	o, dir := newTestOS(t)
	fifo := filepath.Join(dir, "pipe")
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Skipf("mkfifo: %v", err)
	}
	// A writer end frees a blocked open, so a failure of the test does not leak the goroutine.
	t.Cleanup(func() {
		if f, err := os.OpenFile(fifo, os.O_RDWR, 0); err == nil {
			_ = f.Close()
		}
	})
	dst := filepath.Join(dir, "out")
	done := make(chan error, 1)
	go func() { done <- o.Copy(context.Background(), fifo, dst) }()
	select {
	case err := <-done:
		if pluginapi.CodeOf(err) != pluginapi.CodeInvalid || !strings.Contains(err.Error(), "not a regular file") {
			t.Fatalf("Copy(fifo) = %v, want invalid: not a regular file", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Copy blocked on a FIFO")
	}
	if _, err := os.Stat(dst); err == nil {
		t.Fatal("Copy created the destination of a refused copy")
	}
}
