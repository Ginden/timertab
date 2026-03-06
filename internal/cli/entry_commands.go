package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newListCommand() *cobra.Command {
	var (
		targetUser   string
		overridePath string
	)

	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"print-config"},
		Short:   "Print current timertab config",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := validateTargetUserPermission(targetUser); err != nil {
				return err
			}

			cfgPath, err := resolveConfigPath(targetUser, overridePath)
			if err != nil {
				return err
			}

			return listConfig(cmd, cfgPath)
		},
	}

	cmd.Flags().StringVarP(&targetUser, "user", "u", "", "Operate on a specific user")
	cmd.Flags().StringVar(&overridePath, "config", "", "Override config path")

	return cmd
}

func newEditCommand() *cobra.Command {
	var (
		targetUser   string
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

			if err := validateTargetUserPermission(targetUser); err != nil {
				return err
			}

			cfgPath, err := resolveConfigPath(targetUser, overridePath)
			if err != nil {
				return err
			}

			if !noApply && !dryRun {
				if err := ensureSystemdBaseline(); err != nil {
					return err
				}
			}

			return editConfig(cmd, cfgPath, targetUser, noApply, dryRun, noCommit)
		},
	}

	cmd.Flags().StringVarP(&targetUser, "user", "u", "", "Operate on a specific user")
	cmd.Flags().StringVar(&overridePath, "config", "", "Override config path")
	cmd.Flags().BoolVar(&noApply, "no-apply", false, "Validate and save edits, but do not reconcile systemd units")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Preview reconcile changes from edited config without writing anything")
	cmd.Flags().BoolVar(&noCommit, "no-commit", false, "Disable git auto-commit for this edit/apply run")

	return cmd
}

func newPrintPathCommand() *cobra.Command {
	var (
		targetUser   string
		overridePath string
	)

	cmd := &cobra.Command{
		Use:   "print-path",
		Short: "Print resolved config path",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := validateTargetUserPermission(targetUser); err != nil {
				return err
			}

			cfgPath, err := resolveConfigPath(targetUser, overridePath)
			if err != nil {
				return err
			}

			cmd.Println(cfgPath)
			return nil
		},
	}

	cmd.Flags().StringVarP(&targetUser, "user", "u", "", "Operate on a specific user")
	cmd.Flags().StringVar(&overridePath, "config", "", "Override config path")

	return cmd
}
