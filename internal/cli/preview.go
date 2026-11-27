package cli

import (
	"github.com/spf13/cobra"

	"github.com/tbxark/gvim/internal/app"
)

func newPreviewCommand(configPath, themeName *string) *cobra.Command {
	var opts app.PreviewOptions
	cmd := &cobra.Command{
		Use:     "preview FILE",
		Short:   "Print a file preview to standard output",
		Example: "  gvim preview --skip 10 --lines 20 --line-numbers main.go",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cmd.SilenceUsage = true
			opts.ConfigPath = *configPath
			opts.Theme = *themeName
			return app.PrintPreview(cmd.Context(), cmd.OutOrStdout(), args[0], opts)
		},
	}
	flags := cmd.Flags()
	flags.IntVar(&opts.Skip, "skip", 0, "skip this many source lines")
	flags.IntVar(&opts.Lines, "lines", 0, "maximum source lines to print (0 prints through EOF)")
	flags.IntVar(&opts.Width, "width", 0, "maximum display columns including line numbers (0 is unlimited)")
	flags.IntVar(&opts.TabSize, "tab-size", 4, "spaces to display for each tab (1 to 32)")
	flags.BoolVar(&opts.LineNumbers, "line-numbers", false, "show original source line numbers")
	flags.StringVar(&opts.Color, "color", "auto", "syntax color: auto, always, or never")
	return cmd
}
