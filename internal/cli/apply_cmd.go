package cli

import (
	"bytes"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/ginden/timertab/internal/progress"
)

func newApplyCommand() *cobra.Command {
	var (
		overridePath string
		noCommit     bool
	)

	cmd := &cobra.Command{
		Use:   "apply",
		Short: "Reconcile systemd units to match the config without opening an editor",
		Long: "Load and validate the config file, then reconcile systemd units to match it. " +
			"This is the non-interactive counterpart to edit: use it after restoring the config " +
			"from git or a backup, or after edit --no-apply. Applying a config with an empty " +
			"jobs list removes all timertab-managed units.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfgPath, err := resolveConfigPath(overridePath)
			if err != nil {
				return err
			}

			return withConfigLock(cfgPath, func() error {
				return runApplyCommand(cmd, cfgPath, noCommit)
			})
		},
	}

	cmd.Flags().StringVar(&overridePath, "config", "", "Override config path")
	cmd.Flags().BoolVar(&noCommit, "no-commit", false, "Skip git auto-commit of the config change")

	return cmd
}

func runApplyCommand(cmd *cobra.Command, cfgPath string, noCommit bool) error {
	raw, err := os.ReadFile(cfgPath)
	if err != nil {
		return err
	}

	progress.Printf(cmd.Context(), "timertab: validating config")
	loaded, out, err := prepareEditedConfigForSave(raw)
	if err != nil {
		return fmt.Errorf("config is invalid: %w", err)
	}

	beforeConfig := parseConfigForAutoCommit(raw)
	configChanged := !bytes.Equal(raw, out)
	if configChanged {
		// Persist generated ids the same way edit does so the next run is stable.
		progress.Printf(cmd.Context(), "timertab: saving config to %s", cfgPath)
		if err := writeConfigFile(cfgPath, out); err != nil {
			return err
		}
	}

	if err := ensureSystemdBaseline(); err != nil {
		return err
	}

	progress.Printf(cmd.Context(), "timertab: reconciling systemd units")
	report, err := runSystemctlApply(cmd.Context(), loaded)
	if err != nil {
		return err
	}

	cmd.Printf("timertab: applied %s\n", cfgPath)
	printApplyReport(cmd, report)

	if !noCommit {
		maybeAutoCommitEditedConfig(cmd.Context(), cmd.ErrOrStderr(), cfgPath, beforeConfig, loaded, configChanged)
	}

	return nil
}
