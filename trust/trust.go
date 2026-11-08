// Package trust is the workspace-trust model of gvim: the trust level of a
// workspace, the per-user store of the decisions the user has taken, and the
// detection of the content whose presence means a folder can execute code.
//
// It is the Go counterpart of Fresh's services/workspace_trust.rs. A workspace
// is at one of three levels:
//
//   - Trusted: everything runs. The project's config file applies and language
//     servers start.
//   - Restricted: no repository-controlled tooling runs. The project's config
//     file is ignored and language servers do not start on their own.
//   - Blocked: the strict level. It restricts what Restricted restricts, and
//     the dialog tells the user that nothing runs.
//
// A fourth, empty Level value, Undecided, means no decision has been recorded
// for the workspace. Its effective level is Restricted, Fresh's safe default.
//
// The package is TUI-free and does no other IO than reading and writing the
// store file it is given, so it can be tested without an editor.
package trust

import "strings"

// Level is the trust level of a workspace.
//
// The zero value is Undecided, so a Level that has not been set reads the safe
// way: the effective level of Undecided is Restricted.
type Level string

// The trust levels. The names are the kebab-case values Fresh stores and shows
// in the status bar.
const (
	// Undecided is the zero Level: no decision is recorded for the workspace.
	// Its effective level is Restricted (see Effective).
	Undecided Level = ""
	// Trusted runs everything: the project's config file applies and language
	// servers start.
	Trusted Level = "trusted"
	// Restricted runs no repository-controlled tooling: the project's config
	// file is ignored and language servers do not start on their own.
	Restricted Level = "restricted"
	// Blocked is Restricted plus the promise that no processes run. gvim
	// enforces the same restrictions as Restricted (see docs/trust.md).
	Blocked Level = "blocked"
)

// String returns the label Fresh shows for the level in the status bar:
// "Trusted", "Restricted" or "Blocked". Undecided has no label, so it returns
// the empty string.
func (l Level) String() string {
	switch l {
	case Trusted:
		return "Trusted"
	case Restricted:
		return "Restricted"
	case Blocked:
		return "Blocked"
	}
	return ""
}

// Valid reports whether l is one of the three decided levels.
func (l Level) Valid() bool {
	return l == Trusted || l == Restricted || l == Blocked
}

// ParseLevel parses a stored level name. The match ignores case and surrounding
// blanks. ok is false for an unknown name and for the empty string, which is
// how a corrupt or missing decision reads.
func ParseLevel(s string) (level Level, ok bool) {
	l := Level(strings.ToLower(strings.TrimSpace(s)))
	if !l.Valid() {
		return Undecided, false
	}
	return l, true
}

// Effective returns the level a workspace runs at: the recorded level, or
// Restricted when no decision is recorded. A value that is not a known level
// reads as Restricted too, so corrupt input falls back to the safe side.
func Effective(l Level) Level {
	if l.Valid() {
		return l
	}
	return Restricted
}

// State is the trust state of one workspace: the effective level, whether a
// decision is recorded, and the executable-content markers found in the folder.
// It is the value the editor and the trust dialog read.
type State struct {
	// Root is the canonical workspace root the state describes.
	Root string
	// Level is the effective level: the recorded decision, or Restricted while
	// the workspace is undecided.
	Level Level
	// Decided reports whether the store holds a decision for Root.
	Decided bool
	// Markers are the executable-content marker names found at Root, in the
	// order the scan reports them. They explain why the workspace needs a
	// decision, and the dialog lists them.
	Markers []string
}

// NeedsPrompt reports whether opening the workspace should ask the user: no
// decision is recorded and the folder holds content that can execute code.
// Fresh's policy is the same: a plain folder is trusted silently, a folder
// that can execute code waits for the user.
func (s State) NeedsPrompt() bool { return !s.Decided && len(s.Markers) > 0 }

// AllowsProjectConfig reports whether the project's .gvim/config.json layer
// applies at this level. A workspace that is not Trusted is configured by the
// user's own file only.
func (s State) AllowsProjectConfig() bool { return s.Level == Trusted }

// AllowsTooling reports whether project-controlled tools may start on their
// own. Language servers load and execute project files, so they start only in
// a Trusted workspace; the manual Start/Restart LSP Server still works, as in
// Fresh.
func (s State) AllowsTooling() bool { return s.Level == Trusted }
