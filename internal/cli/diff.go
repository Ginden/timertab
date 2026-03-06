package cli

import (
	"github.com/spf13/cobra"

	"github.com/ginden/timertab/internal/config"
)

func newDiffCommand() *cobra.Command {
	var (
		targetUser   string
		overridePath string
	)

	cmd := &cobra.Command{
		Use:   "diff",
		Short: "Preview create/modify/delete unit changes without writing",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := validateTargetUserPermission(targetUser); err != nil {
				return err
			}

			cfgPath, err := resolveConfigPath(targetUser, overridePath)
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

			report, err := runDryRunPlan(cmd.Context(), loaded, targetUser)
			if err != nil {
				return err
			}

			printDryRunReport(cmd, report)
			return nil
		},
	}

	cmd.Flags().StringVarP(&targetUser, "user", "u", "", "Operate on a specific user")
	cmd.Flags().StringVar(&overridePath, "config", "", "Override config path")

	return cmd
}
