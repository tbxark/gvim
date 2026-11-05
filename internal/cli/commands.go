package cli

import (
	"fmt"
	"runtime"
	"runtime/debug"

	"github.com/spf13/cobra"
)

// versionString returns the module version from the build info. A binary built
// from a working tree reports "(devel)".
func versionString() string {
	info, ok := debug.ReadBuildInfo()
	if !ok || info.Main.Version == "" {
		return "unknown"
	}
	return info.Main.Version
}

// buildInfoLines describes how the binary was built: the Go toolchain, the
// platform, and the VCS state when the build recorded it.
func buildInfoLines() []string {
	lines := []string{
		"go " + runtime.Version(),
		"platform " + runtime.GOOS + "/" + runtime.GOARCH,
	}
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return lines
	}
	for _, s := range info.Settings {
		switch s.Key {
		case "vcs.revision", "vcs.time", "vcs.modified":
			lines = append(lines, s.Key+" "+s.Value)
		}
	}
	return lines
}

// newVersionCommand prints the version and the build information.
func newVersionCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the version and build information",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			w := cmd.OutOrStdout()
			if _, err := fmt.Fprintf(w, "gvim version %s\n", versionString()); err != nil {
				return err
			}
			for _, line := range buildInfoLines() {
				if _, err := fmt.Fprintln(w, line); err != nil {
					return err
				}
			}
			return nil
		},
	}
}
