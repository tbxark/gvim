package tui

// Cross-workspace plugin UI waits. One plugin runtime serves every daemon
// workspace, and a plugin UI wait is answered by the root model that opened its overlay.
// The registry keeps the token -> owning model mapping for shared-runtime daemons, so a
// wait opened in workspace A stays with A across a switch (its overlay and plug.waits live
// in A's model and it resumes when A is active again), and closing A explicitly resolves
// its waits with a cancelled completion and a notice instead of leaking a plugin task that
// waits forever. Direct launches have no registry: the one model owns everything.

import (
	"fmt"
	"sync"

	"github.com/tbxark/gvim/pluginapi"
)

// waitRegistry is the cross-workspace wait ownership map of each shared host. It lives
// beside PluginShared rather than inside its struct so the daemon-wide host and the
// workspace-scoped chrome work can edit plugin_shared.go independently.
var waitRegistry = struct {
	mu   sync.Mutex
	byID map[*PluginShared]map[pluginapi.Token]*Model
}{byID: map[*PluginShared]map[pluginapi.Token]*Model{}}

// sharedWaits returns the ownership map of s, creating it on first use.
func sharedWaits(s *PluginShared) map[pluginapi.Token]*Model {
	waitRegistry.mu.Lock()
	defer waitRegistry.mu.Unlock()
	m := waitRegistry.byID[s]
	if m == nil {
		m = map[pluginapi.Token]*Model{}
		waitRegistry.byID[s] = m
	}
	return m
}

// registerWait records that t is open in m. A token may be open in only one workspace: the
// runtime's tokens are global, so a second owner is a plugin bug, not a new wait.
func (s *PluginShared) registerWait(t pluginapi.Token, m *Model) error {
	waits := sharedWaits(s)
	waitRegistry.mu.Lock()
	defer waitRegistry.mu.Unlock()
	if owner, ok := waits[t]; ok && owner != m {
		return pluginapi.Errf(pluginapi.CodeInvalid, "token %d is already waiting in another workspace", t)
	}
	waits[t] = m
	return nil
}

// unregisterWait forgets t if m is its owner (a stale completion from another workspace
// must not drop a wait of the real owner).
func (s *PluginShared) unregisterWait(t pluginapi.Token, m *Model) {
	waits := sharedWaits(s)
	waitRegistry.mu.Lock()
	defer waitRegistry.mu.Unlock()
	if waits[t] == m {
		delete(waits, t)
	}
}

// takeWaitsFor removes and returns the waits owned by m: the workspace is closing, so no
// completion can ever come from its overlays.
func (s *PluginShared) takeWaitsFor(m *Model) []pluginapi.Token {
	waits := sharedWaits(s)
	waitRegistry.mu.Lock()
	defer waitRegistry.mu.Unlock()
	var out []pluginapi.Token
	for t, owner := range waits {
		if owner == m {
			out = append(out, t)
			delete(waits, t)
		}
	}
	return out
}

// plugWaitRegister records a wait against the shared registry when this model is bound to
// the daemon-wide host.
func (m *Model) plugWaitRegister(t pluginapi.Token) error {
	if m.plug.shared == nil {
		return nil
	}
	return m.plug.shared.registerWait(t, m)
}

// plugWaitForget drops t from the shared registry (only when this model owns it).
func (m *Model) plugWaitForget(t pluginapi.Token) {
	if m.plug.shared != nil {
		m.plug.shared.unregisterWait(t, m)
	}
}

// plugForgetWait removes t from this model's wait table and from the shared registry. It is
// for the paths where a prompt is closed without a completion (the runtime closed it
// itself).
func (m *Model) plugForgetWait(t pluginapi.Token) {
	delete(m.plug.waits, t)
	m.plugWaitForget(t)
}

// cancelSharedPluginWaits resolves every wait this model still owns with a cancelled
// completion and says so. It runs when a shared-runtime workspace closes (Model.Close): the
// workspace's overlays are gone, so the plugin task must not wait forever. The notice goes
// through the host mux, i.e. to the workspace that is active at close time (the daemon
// switches away before closing), so it is visible.
func (m *Model) cancelSharedPluginWaits() {
	if m.plug.shared == nil || m.plug.rt == nil {
		return
	}
	toks := m.plug.shared.takeWaitsFor(m)
	if len(toks) == 0 {
		return
	}
	for _, t := range toks {
		if _, ok := m.plug.waits[t]; !ok {
			continue
		}
		// Close has no Update loop left to pump a command, so resolve synchronously: the
		// completion lets the plugin task end instead of waiting forever.
		delete(m.plug.waits, t)
		m.plug.rt.Resolve(pluginapi.Completion{Token: t, Err: string(pluginapi.CodeCancelled) + ": the plugin prompt's workspace closed"})
	}
	if m.plug.host != nil {
		m.plug.host.Notify("", pluginapi.LevelWarn, fmt.Sprintf("Plugin prompt%s cancelled: its workspace closed", waitCountText(len(toks))))
	}
}

func waitCountText(n int) string {
	if n == 1 {
		return ""
	}
	return fmt.Sprintf("s (%d)", n)
}
