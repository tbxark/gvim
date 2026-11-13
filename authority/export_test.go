package authority

import "context"

// ConnectSSH dials the remote and mounts an authority slot over it. The app mounts with
// NewSSH and connects later; the tests of the transport want the connected slot at once.
func ConnectSSH(ctx context.Context, o SSHOptions) (*Authority, error) {
	b := &sshBackend{opts: o, spec: o.Spec}
	if err := b.connect(ctx); err != nil {
		return nil, err
	}
	return New(Parts{
		Files:       newSSHFiles(b),
		OneShot:     b,
		LongRunning: b,
		Terminal:    b,
		Label:       b.Label(),
		Remote:      b,
		RemoteSpec:  &b.spec,
	}), nil
}
