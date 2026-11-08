//go:build unix

package app

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/creack/pty"
)

// TestStdinPTYHelper is the child half of TestStdinPipedPipeUnderAPTY: it runs the real
// Run with --stdin under a pty whose standard input is a pipe.
func TestStdinPTYHelper(t *testing.T) {
	if os.Getenv("GVIM_STDIN_PTY") != "1" {
		t.Skip("helper for TestStdinPipedPipeUnderAPTY")
	}
	opts := Options{
		ReadStdin:  true,
		StdinName:  "piped.txt",
		Root:       os.Getenv("GVIM_STDIN_ROOT"),
		SessionDir: os.Getenv("GVIM_STDIN_SESSION"),
		Simple:     true,
		NoPlugins:  true,
	}
	if err := Run(context.Background(), opts); err != nil {
		fmt.Fprintln(os.Stderr, "stdin helper:", err)
		os.Exit(3)
	}
}

// TestStdinPipedPipeUnderAPTY is the end-to-end piping test: a pty gives the editor its
// terminal (stdout and the /dev/tty keyboard), while standard input is a pipe carrying
// the buffer text. gvim must show the [stdin] buffer with that text and accept keys from
// the terminal again (ctrl+q quits cleanly).
func TestStdinPipedPipeUnderAPTY(t *testing.T) {
	home := t.TempDir()
	root := t.TempDir()
	session := t.TempDir()
	cmd := exec.Command(os.Args[0], "-test.run=^TestStdinPTYHelper$")
	cmd.Env = append(os.Environ(),
		"GVIM_STDIN_PTY=1",
		"GVIM_STDIN_ROOT="+root,
		"GVIM_STDIN_SESSION="+session,
		"HOME="+home,
		"XDG_CONFIG_HOME="+home+"/.config",
	)
	pr, pw, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	cmd.Stdin = pr
	t.Cleanup(func() { _ = pr.Close() })
	// The child gets the pty as its terminal (stdout/stderr and, through ExtraFiles +
	// Setctty, a controlling terminal so /dev/tty opens) while its stdin is the pipe.
	ptmx, tty, err := pty.Open()
	if err != nil {
		t.Skipf("no pty: %v", err)
	}
	t.Cleanup(func() { _ = ptmx.Close() })
	if err := pty.Setsize(ptmx, &pty.Winsize{Cols: 100, Rows: 30}); err != nil {
		t.Skipf("no pty size: %v", err)
	}
	cmd.ExtraFiles = []*os.File{tty}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true, Setctty: true, Ctty: 3}
	cmd.Stdout, cmd.Stderr = tty, tty
	if err := cmd.Start(); err != nil {
		_ = tty.Close()
		t.Fatalf("start: %v", err)
	}
	_ = tty.Close()
	const piped = "hello from the pipe\nsecond line\n"
	go func() {
		_, _ = pw.WriteString(piped)
		_ = pw.Close()
	}()

	var mu sync.Mutex
	var out bytes.Buffer
	readDone := make(chan struct{})
	go func() {
		defer close(readDone)
		buf := make([]byte, 4096)
		for {
			n, err := ptmx.Read(buf)
			if n > 0 {
				mu.Lock()
				out.Write(buf[:n])
				mu.Unlock()
			}
			if err != nil {
				return
			}
		}
	}()
	waitFor := func(patterns ...string) bool {
		deadline := time.Now().Add(15 * time.Second)
		for time.Now().Before(deadline) {
			mu.Lock()
			text := out.String()
			mu.Unlock()
			all := true
			for _, p := range patterns {
				if !strings.Contains(text, p) {
					all = false
					break
				}
			}
			if all {
				return true
			}
			time.Sleep(20 * time.Millisecond)
		}
		return false
	}
	if !waitFor("[stdin]", "hello from the pipe", "second line") {
		mu.Lock()
		text := out.String()
		mu.Unlock()
		t.Fatalf("the piped buffer never showed; screen had:\n%s", text)
	}
	// The keyboard is the terminal again: quit with the normal chord.
	if _, err := ptmx.Write([]byte{0x11}); err != nil {
		t.Fatalf("write quit: %v", err)
	}
	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()
	select {
	case err := <-exited:
		mu.Lock()
		text := out.String()
		mu.Unlock()
		if err != nil {
			t.Fatalf("child exited with %v; screen:\n%s", err, text)
		}
		if strings.Contains(text, "stdin helper:") {
			t.Fatalf("helper failed:\n%s", text)
		}
	case <-time.After(15 * time.Second):
		_ = cmd.Process.Kill()
		t.Fatalf("gvim did not quit on ctrl+q (wait: %v)", <-exited)
	}
	<-readDone
}
