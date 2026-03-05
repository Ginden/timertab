package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/ginden/timertab/internal/config"
)

func newValidateCommand() *cobra.Command {
	var path string

	cmd := &cobra.Command{
		Use:   "validate",
		Short: "Validate timertab YAML",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if path == "" {
				return fmt.Errorf("--config is required")
			}

			cfg, err := config.LoadFromFile(path)
			if err != nil {
				return err
			}

			if err := cfg.NormalizeIDs(); err != nil {
				return err
			}

			cmd.Println("ok")
			return nil
		},
	}

	cmd.Flags().StringVar(&path, "config", "", "Path to timertab YAML file")

	return cmd
}
