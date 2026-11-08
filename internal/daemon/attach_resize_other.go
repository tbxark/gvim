//go:build !unix

package daemon

import "net"

// watchResize does nothing where the platform has no resize signal: the client sends
// the initial size in its hello and keeps it for the session.
func watchResize(net.Conn) func() { return func() {} }
