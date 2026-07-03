package cli

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/ginden/timertab/internal/config"
)

func newAddCommand() *cobra.Command {
	var (
		overridePath string
		id           string
		name         string
		when         []string
		env          []string
		noApply      bool
		noCommit     bool
	)

	cmd := &cobra.Command{
		Use:   "add --when <schedule> [--name <name>] [--id <id>] [--env K=V]... -- <command...>",
		Short: "Add a job non-interactively",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(when) == 0 {
				return fmt.Errorf("--when is required")
			}

			cfgPath, err := resolveConfigPath(overridePath)
			if err != nil {
				return err
			}

			jobEnv, err := parseEnvFlags(env)
			if err != nil {
				return err
			}

			job := config.Job{
				ID:   strings.TrimSpace(id),
				Name: strings.TrimSpace(name),
				When: config.ScheduleList(when),
				Env:  jobEnv,
				Run:  runCommandFromArgs(args),
			}

			loaded, raw, err := loadConfigWithRaw(cfgPath)
			if err != nil {
				return err
			}
			preJobs := append([]config.Job(nil), loaded.Jobs...)
			loaded.Jobs = append(loaded.Jobs, job)
			if err := loaded.NormalizeIDs(); err != nil {
				return err
			}
			addedJob := loaded.Jobs[len(loaded.Jobs)-1]

			if !noApply {
				if err := ensureSystemdBaseline(); err != nil {
					return err
				}
			}

			if err := savePatchedConfig(cfgPath, raw, loaded, loaded.Jobs[:len(preJobs)], func(jobsNode *yaml.Node) error {
				return appendJobNodes(jobsNode, []config.Job{addedJob})
			}); err != nil {
				return err
			}

			if noApply {
				cmd.Printf("timertab: saved %s (no apply)\n", cfgPath)
			} else {
				report, err := runSystemctlApply(cmd.Context(), loaded)
				if err != nil {
					return err
				}
				cmd.Printf("timertab: saved %s\n", cfgPath)
				printApplyReport(cmd, report)
			}

			if !noCommit {
				maybeAutoCommitConfig(cmd.Context(), cmd.ErrOrStderr(), cfgPath, loaded, "timertab: add job "+addedJob.ID)
			}

			return nil
		},
	}

	cmd.Flags().StringVar(&overridePath, "config", "", "Override config path")
	cmd.Flags().StringVar(&id, "id", "", "Job ID (optional; generated when omitted)")
	cmd.Flags().StringVar(&name, "name", "", "Human-readable job name")
	cmd.Flags().StringArrayVar(&when, "when", nil, "Schedule expression; repeat for multiple schedules")
	cmd.Flags().StringArrayVar(&env, "env", nil, "Environment variable assignment K=V; repeat as needed")
	cmd.Flags().BoolVar(&noApply, "no-apply", false, "Save config but skip systemd reconcile")
	cmd.Flags().BoolVar(&noCommit, "no-commit", false, "Skip git auto-commit of the config change")

	return cmd
}

func newRemoveCommand() *cobra.Command {
	var (
		overridePath string
		noApply      bool
		noCommit     bool
	)

	cmd := &cobra.Command{
		Use:     "rm <id>",
		Aliases: []string{"remove"},
		Short:   "Remove a job from config and apply pruning",
		Args:    cobra.ExactArgs(1),
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

			raw, err := os.ReadFile(cfgPath)
			if err != nil {
				return err
			}
			loaded, err := config.LoadFromBytes(raw)
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
			preJobs := append([]config.Job(nil), loaded.Jobs...)
			loaded.Jobs = append(loaded.Jobs[:jobIndex], loaded.Jobs[jobIndex+1:]...)

			if !noApply {
				if err := ensureSystemdBaseline(); err != nil {
					return err
				}
			}

			if err := savePatchedConfig(cfgPath, raw, loaded, preJobs, func(jobsNode *yaml.Node) error {
				return removeJobNode(jobsNode, jobIndex)
			}); err != nil {
				return err
			}

			if noApply {
				cmd.Printf("timertab: saved %s (no apply)\n", cfgPath)
			} else {
				report, err := runSystemctlApply(cmd.Context(), loaded)
				if err != nil {
					return err
				}
				cmd.Printf("timertab: saved %s\n", cfgPath)
				printApplyReport(cmd, report)
			}

			if !noCommit {
				maybeAutoCommitConfig(cmd.Context(), cmd.ErrOrStderr(), cfgPath, loaded, "timertab: remove job "+jobID)
			}

			return nil
		},
	}

	cmd.Flags().StringVar(&overridePath, "config", "", "Override config path")
	cmd.Flags().BoolVar(&noApply, "no-apply", false, "Save config but skip systemd reconcile")
	cmd.Flags().BoolVar(&noCommit, "no-commit", false, "Skip git auto-commit of the config change")

	return cmd
}

func loadConfigWithRaw(path string) (*config.File, []byte, error) {
	raw, err := os.ReadFile(path)
	if err == nil {
		loaded, err := config.LoadFromBytes(raw)
		return loaded, raw, err
	}
	if !os.IsNotExist(err) {
		return nil, nil, err
	}
	loaded, err := loadOrCreateConfig(path)
	return loaded, nil, err
}

func parseEnvFlags(values []string) (map[string]string, error) {
	if len(values) == 0 {
		return nil, nil
	}
	out := make(map[string]string, len(values))
	for _, value := range values {
		key, val, ok := strings.Cut(value, "=")
		if !ok || strings.TrimSpace(key) == "" {
			return nil, fmt.Errorf("--env must be K=V, got %q", value)
		}
		out[key] = val
	}
	return out, nil
}

func runCommandFromArgs(args []string) config.RunCommand {
	if len(args) == 1 {
		return config.ShellCommand(args[0])
	}
	return config.ExecCommand(args[0], args[1:]...)
}
