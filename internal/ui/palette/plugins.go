package palette

import model "github.com/tbxark/gvim/internal/palette"

// PluginRunner is the plugin host as the palette and the root see it. The app
// adapter implements it over internal/plugin, so this package does not import the
// plugin runtime.
type PluginRunner interface {
	// Commands returns one palette entry per plugin command, with the label the
	// plugin gave it. Each entry's action is the one Run takes, through
	// model.PluginEntry.
	Commands() []model.Entry
	// Run runs the plugin command called name on the calling goroutine, which is
	// the UI goroutine. It returns the text the command showed with Notify or
	// SetStatus, and the error that stopped it.
	Run(name string) (notice string, err error)
}
