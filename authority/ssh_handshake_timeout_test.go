package authority_test

import (
	"context"
	"net"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/tbxark/gvim/authority"
)

// TestConnectSSHBoundsAStalledHandshake: a server that accepts the TCP connection but
// never speaks must fail the dial at SSHOptions.Timeout, not hang it.
func TestConnectSSHBoundsAStalledHandshake(t *testing.T) {
	t.Setenv("SSH_AUTH_SOCK", "")
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			t.Cleanup(func() { _ = c.Close() })
		}
	}()
	done := make(chan error, 1)
	go func() {
		_, err := authority.ConnectSSH(context.Background(), authority.SSHOptions{
			Spec:            authority.RemoteSpec{User: "u", Host: "127.0.0.1", Port: ln.Addr().(*net.TCPAddr).Port, Path: "/x"},
			Password:        func(context.Context) (string, error) { return "pw", nil },
			DisableAgent:    true,
			Timeout:         300 * time.Millisecond,
			HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		})
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("ConnectSSH succeeded against a silent server")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("ConnectSSH hung on a silent server: the handshake is not bounded by Timeout")
	}
}
