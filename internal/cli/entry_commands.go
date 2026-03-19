package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/ginden/timertab/internal/progress"
)

func newListCommand() *cobra.Command {
	var overridePath string

	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"print-config"},
		Short:   "Print current timertab config",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfgPath, err := resolveConfigPath(overridePath)
			if err != nil {
				return err
			}

			return listConfig(cmd, cfgPath)
		},
	}

	cmd.Flags().StringVar(&overridePath, "config", "", "Override config path")

	return cmd
}

func newEditCommand() *cobra.Command {
	var (
		overridePath string
		noApply      bool
		dryRun       bool
		noCommit     bool
	)

	cmd := &cobra.Command{
		Use:   "edit",
		Short: "Edit timertab config and apply",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if dryRun && noApply {
				return fmt.Errorf("--dry-run cannot be combined with --no-apply")
			}

			ctx := progress.WithWriter(cmd.Context(), cmd.ErrOrStderr())
			cmd.SetContext(ctx)

			cfgPath, err := resolveConfigPath(overridePath)
			if err != nil {
				return err
			}

			if !noApply && !dryRun {
				progress.Printf(ctx, "timertab: checking systemd baseline")
				if err := ensureSystemdBaseline(); err != nil {
					return err
				}
			}

			return editConfig(cmd, cfgPath, noApply, dryRun, noCommit)
		},
	}

	cmd.Flags().StringVar(&overridePath, "config", "", "Override config path")
	cmd.Flags().BoolVar(&noApply, "no-apply", false, "Validate and save edits, but do not reconcile systemd units")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Preview reconcile changes from edited config without writing anything")
	cmd.Flags().BoolVar(&noCommit, "no-commit", false, "Disable git auto-commit for this edit/apply run")
	_ = cmd.Flags().MarkDeprecated("dry-run", "use `timertab diff` for reconcile previews; review-bundle rendering will replace edit-time dry runs")

	return cmd
}

func newPrintPathCommand() *cobra.Command {
	var overridePath string

	cmd := &cobra.Command{
		Use:   "print-path",
		Short: "Print resolved config path",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfgPath, err := resolveConfigPath(overridePath)
			if err != nil {
				return err
			}

			cmd.Println(cfgPath)
			return nil
		},
	}

	cmd.Flags().StringVar(&overridePath, "config", "", "Override config path")

	return cmd
}
