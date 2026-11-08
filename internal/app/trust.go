package app

import (
	"fmt"
	"sync"

	"github.com/tbxark/gvim/config"
	"github.com/tbxark/gvim/internal/tui"
	"github.com/tbxark/gvim/trust"
)

// newTrustStore builds the user's workspace-trust store. It is a variable so a
// test can point the session at a temporary file instead of the user's config
// directory.
var newTrustStore = trust.DefaultStore

// trustController is the TUI's workspace-trust port (tui.TrustPort): the state
// of the open workspace and the store that records the user's decisions. The
// update goroutine reads the state; the commands that write the store run off
// it, so the two sides take a lock.
type trustController struct {
	mu    sync.Mutex
	store *trust.Store
	state trust.State
	// recordKey, when set, is the explicit store key of a non-filesystem workspace (a
	// remote target). Decide writes the record key instead of canonicalizing a root.
	recordKey string
}

var _ tui.TrustPort = (*trustController)(nil)

// openTrust resolves the trust state of root for a session. It returns the
// store behind the decision (nil when trust is unavailable), the state, and a
// warning for the status row ("" when there is nothing to report).
//
// A user config directory that cannot be found leaves trust unavailable: there
// is nowhere to record a decision, and restricting the editor with no way to
// release it would be worse than the risk. The state that comes back is then
// trusted for every gate, while the nil store leaves the TUI without a port (no
// prompt, no pill, and the command says trust is unavailable). Every other
// failure is reported as a warning, and the state that resolved is used.
func openTrust(root string) (*trust.Store, trust.State, string) {
	store, err := newTrustStore()
	if err != nil {
		return nil, trust.State{Level: trust.Trusted, Decided: true}, "workspace trust: " + err.Error()
	}
	state, err := store.Open(root)
	if err != nil {
		return store, state, "workspace trust: " + err.Error()
	}
	return store, state, ""
}

// newTrustController returns the TUI port over store and state.
func newTrustController(store *trust.Store, state trust.State) *trustController {
	return &trustController{store: store, state: state}
}

// State returns the trust state of the open workspace.
func (c *trustController) State() trust.State {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.state
}

// Decide records level for the open workspace. The state changes even when the
// write fails, as Fresh's set_level does: the level applies to the session, and
// the error says the decision was not saved. The markers are kept, so the
// dialog can still explain why the workspace asked.
func (c *trustController) Decide(level trust.Level) (trust.State, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !level.Valid() {
		return c.state, fmt.Errorf("trust: invalid level %q", string(level))
	}
	var err error
	if c.recordKey != "" {
		err = c.store.SetRecord(c.recordKey, level)
	} else {
		err = c.store.Set(c.state.Root, level)
	}
	c.state.Level = level
	c.state.Decided = true
	return c.state, err
}

// Load resolves the trust state of root, with the startup policy applied, and
// makes it the current state, so a project switch shows the new workspace's
// level. It keeps the current state when the load fails.
func (c *trustController) Load(root string) (trust.State, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	state, err := c.store.Open(root)
	if state.Root != "" {
		c.state = state
	}
	return c.state, err
}

// lspConfig returns the config the language-server port is built from. Fresh
// gates language-server auto-start on trust: starting a server runs
// project-controlled code, so only a Trusted workspace starts one on its own.
// gvim's switch for that is lsp_enabled, which is exactly "no server starts by
// itself"; Start/Restart LSP Server still starts one, as Fresh's manual start
// does in a restricted workspace. The copy is changed, never the user's file.
func lspConfig(cfg config.Config, state trust.State) config.Config {
	if !state.AllowsTooling() {
		cfg.LSPEnabled = false
	}
	return cfg
}
