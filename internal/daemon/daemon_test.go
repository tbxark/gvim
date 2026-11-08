package daemon

import (
	"bytes"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFrameRoundTripAndBounds(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteJSON(&buf, TypeHello, Hello{Cols: 120, Rows: 40, Args: []string{"a.go"}}); err != nil {
		t.Fatal(err)
	}
	if err := WriteFrame(&buf, TypeInput, []byte("jk")); err != nil {
		t.Fatal(err)
	}
	f, err := ReadFrame(&buf)
	if err != nil || f.Type != TypeHello {
		t.Fatalf("frame = %+v, %v", f, err)
	}
	var hello Hello
	if err := DecodeJSON(f, &hello); err != nil || hello.Cols != 120 || hello.Rows != 40 || hello.Args[0] != "a.go" {
		t.Fatalf("hello = %+v, %v", hello, err)
	}
	f, err = ReadFrame(&buf)
	if err != nil || f.Type != TypeInput || string(f.Payload) != "jk" {
		t.Fatalf("frame = %+v, %v", f, err)
	}
	if _, err := ReadFrame(&buf); !errors.Is(err, io.EOF) {
		t.Fatalf("EOF error = %v", err)
	}
	if err := WriteFrame(io.Discard, TypeInput, make([]byte, MaxPayload+1)); !errors.Is(err, ErrFrameTooLarge) {
		t.Fatalf("oversize write = %v", err)
	}
	// A frame announcing an oversized payload fails on read.
	var bad bytes.Buffer
	hdr := []byte{0x00, 0x10, 0x00, 0x01, TypeInput}
	bad.Write(hdr)
	if _, err := ReadFrame(&bad); !errors.Is(err, ErrFrameTooLarge) {
		t.Fatalf("oversize read = %v", err)
	}
}

func TestListenRefusesALiveDaemon(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	name := "live-" + filepath.Base(t.TempDir())
	ln, path, err := Listen(name)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ln.Close(); _ = os.Remove(path) }()
	if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("socket mode = %v, %v; want 0600", info, err)
	}
	if !IsLive(path) {
		t.Fatal("the daemon socket is not live")
	}
	if _, _, err := Listen(name); err == nil || !strings.Contains(err.Error(), "another daemon") {
		t.Fatalf("second Listen error = %v, want a live-daemon refusal", err)
	}
	// Two clients can dial; the daemon owns the single-attached policy.
	c1, err := Attach(name)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c1.Close() })
	go func() {
		conn, _ := ln.Accept()
		if conn != nil {
			_ = conn.Close()
		}
	}()
}

func TestStaleSocketIsUnlinked(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	name := "stale-" + filepath.Base(t.TempDir())
	path, err := SocketPath(name)
	if err != nil {
		t.Fatal(err)
	}
	// A socket left by a dead daemon is stale and is replaced. (A plain file at the
	// path is the user's and is refused; TestListenRefusesToReplaceNonSocket.)
	_ = os.Remove(path) // a previous run may have left the regular file this test used to seed
	old, err := net.Listen("unix", path)
	if err != nil {
		t.Fatalf("seed stale socket: %v", err)
	}
	if err := old.Close(); err != nil {
		t.Fatal(err)
	}
	ln, got, err := Listen(name)
	if err != nil {
		t.Fatalf("Listen over a stale socket: %v", err)
	}
	if got != path {
		t.Fatalf("path = %q, want %q", got, path)
	}
	c, err := net.DialTimeout("unix", path, probeDialTimeout)
	if err != nil {
		t.Fatalf("dial fresh listener: %v", err)
	}
	_ = c.Close()
	_ = ln.Close()
}

func TestDefaultNameIsPerRoot(t *testing.T) {
	a := DefaultName(filepath.Join("x", "proj"))
	b := DefaultName(filepath.Join("y", "proj"))
	if a == b {
		t.Fatalf("different roots share the name %q", a)
	}
	if !strings.HasPrefix(a, "proj-") {
		t.Fatalf("name = %q, want a proj- prefix", a)
	}
}
