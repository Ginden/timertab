package cli

import (
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
			cfgPath, err := resolveConfigPath(path)
			if err != nil {
				return err
			}

			cfg, err := config.LoadFromFile(cfgPath)
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
