package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/ginden/timertab/internal/config"
)

func newTriggerCommand() *cobra.Command {
	var overridePath string

	cmd := &cobra.Command{
		Use:   "trigger <id>",
		Short: "Trigger a configured job service immediately",
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

			targetUID, err := resolveCurrentUID()
			if err != nil {
				return err
			}
			instanceID := loaded.EffectiveInstanceID()

			rendered, err := renderJobUnits(targetUID, instanceID, loaded.Jobs[jobIndex])
			if err != nil {
				return err
			}

			if err := newSystemctlExecutor(targetUID).StartService(cmd.Context(), rendered.ServiceName); err != nil {
				return fmt.Errorf("trigger service %q: %w", rendered.ServiceName, err)
			}

			cmd.Printf("triggered %s (%s)\n", jobID, rendered.ServiceName)
			return nil
		},
	}

	cmd.Flags().StringVar(&overridePath, "config", "", "Override config path")

	return cmd
}
