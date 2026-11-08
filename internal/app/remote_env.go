package app

// The remote environment provider's host seam (#R13-13). The provider is backend-agnostic
// (env.Remote): this file supplies the run closure that executes its probe and capture on
// the REMOTE host through the workspace authority's one-shot surface. The commands run with
// NO Env overlay: the capture must see the remote login environment, not this provider's
// own delta (that would recurse) and never the local process environment (that is the data
// leak the provider replaces). Only the root path, the command and the output bound are
// passed; every environment entry the provider later hands to a spawn was captured on the
// remote host.

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/tbxark/gvim/authority"
	"github.com/tbxark/gvim/env"
	"github.com/tbxark/gvim/plugin/hostos"
)

const (
	// remoteEnvCommandTimeout bounds one remote env probe or capture command.
	remoteEnvCommandTimeout = 15 * time.Second
	// remoteEnvMaxOutput matches the env package's capture bound.
	remoteEnvMaxOutput = 8 << 20
)

// newRemoteEnvRunner returns the env.RemoteRunner of a remote session. slot and root are
// called per command because the authority connects after the provider is built and the
// remote root may be expanded from ~ once the connection exists.
func newRemoteEnvRunner(slot func() authority.Slot, root func() string) env.RemoteRunner {
	return func(ctx context.Context, command string) (string, error) {
		s := slot()
		if s == nil {
			return "", errors.New("remote environment: the remote authority is not mounted")
		}
		one := s.OneShot()
		if one == nil {
			return "", errors.New("remote environment: the remote authority has no one-shot surface")
		}
		ctx, cancel := context.WithTimeout(ctx, remoteEnvCommandTimeout)
		defer cancel()
		res, err := one.Run(ctx, []string{"sh", "-c", command}, hostos.ProcOptions{
			Cwd: root(), MaxOutput: remoteEnvMaxOutput,
		})
		if err != nil {
			return "", err
		}
		if res.Code != 0 {
			msg := strings.TrimSpace(string(res.Stderr))
			if msg == "" {
				return "", fmt.Errorf("remote environment command failed (exit %d)", res.Code)
			}
			return "", fmt.Errorf("remote environment command failed (exit %d): %s", res.Code, msg)
		}
		return string(res.Stdout), nil
	}
}

// remoteEnvMap converts set-only environment entries ("K=V") into the map the authority's
// ProcOptions takes. A malformed entry is dropped (it cannot name a variable).
func remoteEnvMap(entries []string) map[string]string {
	out := make(map[string]string, len(entries))
	for _, entry := range entries {
		if k, v, ok := strings.Cut(entry, "="); ok && k != "" {
			out[k] = v
		}
	}
	return out
}
