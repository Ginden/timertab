package cli

import (
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/ginden/timertab/internal/config"
	"github.com/ginden/timertab/internal/systemd"
	"github.com/ginden/timertab/internal/version"
)

var ensureSystemdBaseline = systemd.EnsureBaseline
var validateTargetUserPermission = config.ValidateTargetUserPermission
var resolveConfigPath = config.ResolvePath

func NewRootCommand() *cobra.Command {
	opts := &Options{}

	cmd := &cobra.Command{
		Use:   "timertab",
		Short: "Manage systemd timers using a crontab-like workflow",
		Long:  "timertab is a crontab-like CLI that manages systemd timer/service units from a YAML config file.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}

			if err := validateTargetUserPermission(opts.User); err != nil {
				return err
			}

			cfgPath, err := resolveConfigPath(opts.User, opts.Config)
			if err != nil {
				return err
			}

			switch {
			case opts.PrintPath:
				cmd.Println(cfgPath)
				return nil
			case opts.List:
				return listConfig(cmd, cfgPath)
			case opts.Edit:
				if !opts.NoApply && !opts.DryRun {
					if err := ensureSystemdBaseline(); err != nil {
						return err
					}
				}
				return editConfig(cmd, cfgPath, opts.User, opts.NoApply, opts.DryRun, opts.NoCommit)
			default:
				return cmd.Help()
			}
		},
	}

	cmd.SetErrPrefix("timertab")

	cmd.Flags().BoolVarP(&opts.List, "list", "l", false, "Print current timertab config")
	cmd.Flags().BoolVar(&opts.List, "print-config", false, "Print current timertab config")
	cmd.Flags().BoolVarP(&opts.Edit, "edit", "e", false, "Edit timertab config and apply")
	cmd.Flags().StringVarP(&opts.User, "user", "u", "", "Operate on a specific user")
	cmd.Flags().StringVar(&opts.Config, "config", "", "Override config path")
	cmd.Flags().BoolVar(&opts.NoApply, "no-apply", false, "Validate and save edits, but do not reconcile systemd units")
	cmd.Flags().BoolVar(&opts.DryRun, "dry-run", false, "Preview reconcile changes from edited config without writing anything")
	cmd.Flags().BoolVar(&opts.NoCommit, "no-commit", false, "Disable git auto-commit for this edit/apply run")
	cmd.Flags().BoolVar(&opts.PrintPath, "print-path", false, "Print resolved config path")

	cmd.Version = fmt.Sprintf("%s (%s, %s)", version.Version, version.Commit, version.Date)
	cmd.SetVersionTemplate("{{printf \"%s\\n\" .Version}}")

	cmd.AddCommand(newValidateCommand())
	cmd.AddCommand(newAddCommand())
	cmd.AddCommand(newEjectCommand())
	cmd.AddCommand(newStatusCommand())
	cmd.AddCommand(newLogsCommand())
	cmd.AddCommand(newDiffCommand())
	cmd.AddCommand(newImportCommand())

	return cmd
}

func Execute() {
	if err := NewRootCommand().Execute(); err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			_, _ = fmt.Fprintf(os.Stderr, "%s %v\n", errorPrefix, err)
		} else {
			_, _ = fmt.Fprintf(os.Stderr, "%s timertab: %v\n", errorPrefix, err)
		}
		os.Exit(1)
	}
}
