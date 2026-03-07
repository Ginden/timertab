package cli

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"strings"

	"github.com/spf13/cobra"

	"github.com/ginden/timertab/internal/config"
)

var runJournalctl = func(ctx context.Context, stdin io.Reader, stdout, stderr io.Writer, args ...string) error {
	cmd := exec.CommandContext(ctx, "journalctl", args...)
	cmd.Stdin = stdin
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	return cmd.Run()
}

func newLogsCommand() *cobra.Command {
	var (
		targetUser   string
		overridePath string
		lines        string
		since        string
		until        string
		follow       bool
		noPager      bool
	)

	cmd := &cobra.Command{
		Use:   "logs <id>",
		Short: "Show journal logs for a configured job",
		Args:  cobra.ExactArgs(1),
		ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			if len(args) > 0 {
				return nil, cobra.ShellCompDirectiveNoFileComp
			}
			return completeJobIDs(targetUser, overridePath, toComplete)
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			jobID := strings.TrimSpace(args[0])
			if jobID == "" {
				return fmt.Errorf("job id cannot be empty")
			}

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

			jobIndex := indexOfJobID(loaded.Jobs, jobID)
			if jobIndex < 0 {
				return fmt.Errorf("job %q not found", jobID)
			}

			targetUID, err := resolveTargetUID(targetUser)
			if err != nil {
				return err
			}
			instanceID := loaded.EffectiveInstanceID()

			rendered, err := renderJobUnits(targetUID, instanceID, loaded.Jobs[jobIndex])
			if err != nil {
				return err
			}

			journalctlArgs := []string{"--user", "-u", rendered.ServiceName}
			if lines != "" {
				journalctlArgs = append(journalctlArgs, "-n", lines)
			}
			if follow {
				journalctlArgs = append(journalctlArgs, "-f")
			}
			if since != "" {
				journalctlArgs = append(journalctlArgs, "--since", since)
			}
			if until != "" {
				journalctlArgs = append(journalctlArgs, "--until", until)
			}
			if noPager {
				journalctlArgs = append(journalctlArgs, "--no-pager")
			}

			if err := runJournalctl(cmd.Context(), cmd.InOrStdin(), cmd.OutOrStdout(), cmd.ErrOrStderr(), journalctlArgs...); err != nil {
				return fmt.Errorf("journalctl failed: %w", err)
			}

			return nil
		},
	}

	cmd.Flags().StringVarP(&targetUser, "user", "u", "", "Operate on a specific user")
	cmd.Flags().StringVar(&overridePath, "config", "", "Override config path")
	cmd.Flags().StringVarP(&lines, "lines", "n", "", "Show the most recent N lines")
	cmd.Flags().BoolVarP(&follow, "follow", "f", false, "Follow the journal output")
	cmd.Flags().StringVar(&since, "since", "", "Show entries newer than DATE")
	cmd.Flags().StringVar(&until, "until", "", "Show entries older than DATE")
	cmd.Flags().BoolVar(&noPager, "no-pager", false, "Do not pipe output into a pager")

	return cmd
}
