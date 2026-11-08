package tui

import "github.com/tbxark/gvim/trust"

// TrustPort is the workspace-trust state of the open workspace and the
// recorded decisions. internal/app implements it over the trust store (see
// package trust); a test implements it directly.
//
// A nil port means workspace trust is unavailable: the Workspace Trust… command
// says so, and the status row has no trust pill. That is the case when gvim
// cannot find the user config directory, so no decision could be recorded.
type TrustPort interface {
	// State returns the trust state of the open workspace. It reads no files:
	// the app loads the decision when the editor starts and when a workspace
	// opens.
	State() trust.State
	// Decide records level for the open workspace and returns the state that
	// applies after it. It writes the store, so the root calls it from a
	// tea.Cmd, never from Update.
	Decide(level trust.Level) (trust.State, error)
	// Load reads the trust state of root, with the startup policy applied, and
	// makes it the port's current state. It reads the store and the folder, so
	// the root calls it from a tea.Cmd, never from Update.
	Load(root string) (trust.State, error)
}

// trustState is the model's copy of the workspace trust: the port that reads
// and records decisions, and the state of the open workspace as the last load
// or decision left it. Update reads state only; every load and every write runs
// in a tea.Cmd.
type trustState struct {
	port  TrustPort
	state trust.State
}

// newTrustState returns the trust state of a session over port. A nil port
// leaves trust unavailable.
func newTrustState(port TrustPort) trustState {
	if port == nil {
		return trustState{}
	}
	return trustState{port: port, state: port.State()}
}

// available reports whether the session has a trust port.
func (t trustState) available() bool { return t.port != nil }

// trustLevel returns the effective level of the open workspace, or
// trust.Undecided when trust is unavailable.
func (m *Model) trustLevel() trust.Level {
	if !m.trust.available() {
		return trust.Undecided
	}
	return trust.Effective(m.trust.state.Level)
}

// trustStatus returns the label and severity of the status-bar trust pill: the
// level's Fresh label (uiText("gvim.status.trusted", "Trusted"), uiText("gvim.status.restricted", "Restricted") or uiText("gvim.status.blocked", "Blocked")), and true when the
// pill is painted with the warning colour. Fresh paints Restricted and Blocked
// with the warning role. An unavailable, undecided or empty state returns "",
// so the row paints no pill.
func (m *Model) trustStatus() (label string, warn bool) {
	if !m.trust.available() || m.trust.state.Root == "" {
		return "", false
	}
	level := trust.Effective(m.trust.state.Level)
	return level.String(), level != trust.Trusted
}
