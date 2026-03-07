package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/ginden/timertab/internal/config"
)

func newEnableCommand() *cobra.Command {
	return newSetEnabledCommand("enable", true, "Enable a configured job and apply")
}

func newDisableCommand() *cobra.Command {
	return newSetEnabledCommand("disable", false, "Disable a configured job and apply")
}

func newSetEnabledCommand(use string, enabled bool, short string) *cobra.Command {
	var overridePath string

	cmd := &cobra.Command{
		Use:   use + " <id>",
		Short: short,
		Args:  cobra.ExactArgs(1),
		ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			if len(args) > 0 {
				return nil, cobra.ShellCompDirectiveNoFileComp
			}
			return completeJobIDs(overridePath, toComplete)
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			jobID := strings.TrimSpace(args[0])
			if jobID == "" {
				return fmt.Errorf("job id cannot be empty")
			}

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

			jobIndex := indexOfJobID(loaded.Jobs, jobID)
			if jobIndex < 0 {
				return fmt.Errorf("job %q not found", jobID)
			}

			loaded.Jobs[jobIndex].Enabled = boolPtr(enabled)

			if err := saveConfig(cfgPath, loaded); err != nil {
				return err
			}

			if err := ensureSystemdBaseline(); err != nil {
				return err
			}

			report, err := runSystemctlApply(cmd.Context(), loaded)
			if err != nil {
				return err
			}

			cmd.Printf("timertab: saved %s\n", cfgPath)
			printApplyReport(cmd, report)
			return nil
		},
	}

	cmd.Flags().StringVar(&overridePath, "config", "", "Override config path")

	return cmd
}

func boolPtr(value bool) *bool {
	copyValue := value
	return &copyValue
}
