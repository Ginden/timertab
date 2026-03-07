package cli

import (
	"github.com/spf13/cobra"

	"github.com/ginden/timertab/internal/config"
)

func newDiffCommand() *cobra.Command {
	var overridePath string

	cmd := &cobra.Command{
		Use:   "diff",
		Short: "Preview create/modify/delete unit changes without writing",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfgPath, err := resolveConfigPath(overridePath)
			if err != nil {
				return err
			}

			loaded, err := config.LoadFromFile(cfgPath)
			if err != nil {
				return err
			}
			if err := loaded.NormalizeIDs(); err != nil {
				return err
			}

			report, err := runDryRunPlan(cmd.Context(), loaded)
			if err != nil {
				return err
			}

			printDryRunReport(cmd, report)
			return nil
		},
	}

	cmd.Flags().StringVar(&overridePath, "config", "", "Override config path")

	return cmd
}
