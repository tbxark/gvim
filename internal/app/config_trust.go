package app

import (
	"github.com/tbxark/gvim/trust"
)

// ProjectConfigTrusted reports whether the workspace's recorded trust level lets the
// project config layer be merged. It is read-only on purpose: an undecided workspace
// answers false and no open-time decision is recorded, so a command-line diagnostic
// cannot change what the editor will trust. The editor's open path resolves the same
// store through trust.Store.Open before it loads the config.
func ProjectConfigTrusted(root string) (bool, error) {
	store, err := trust.DefaultStore()
	if err != nil {
		return false, err
	}
	level, decided := store.LevelFor(root)
	return decided && level == trust.Trusted, nil
}
