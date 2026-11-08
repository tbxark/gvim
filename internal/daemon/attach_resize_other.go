//go:build !unix

package daemon

// watchResize does nothing where the platform has no resize signal: the client sends
// the initial size in its hello and keeps it for the session.
func watchResize(*FrameWriter) func() { return func() {} }
