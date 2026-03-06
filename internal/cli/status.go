package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/ginden/timertab/internal/config"
)

type statusRow struct {
	ID          string
	LastRun     string
	NextTrigger string
	Result      string
}

var runSystemctlShow = func(ctx context.Context, args ...string) (string, string, error) {
	cmd := exec.CommandContext(ctx, "systemctl", args...)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	return stdout.String(), stderr.String(), err
}

func newStatusCommand() *cobra.Command {
	var (
		targetUser   string
		overridePath string
	)

	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show last run, next trigger, and result for configured jobs",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := validateTargetUserPermission(targetUser); err != nil {
				return err
			}

			cfgPath, err := resolveConfigPath(targetUser, overridePath)
			if err != nil {
				return err
			}

			loaded, err := config.LoadFromFile(cfgPath)
			if err != nil {
				if errors.Is(err, os.ErrNotExist) {
					return nil
				}
				return err
			}

			if err := loaded.NormalizeIDs(); err != nil {
				return err
			}

			targetUID, err := resolveTargetUID(targetUser)
			if err != nil {
				return err
			}

			rows, err := collectStatusRows(cmd.Context(), targetUID, loaded.Jobs)
			if err != nil {
				return err
			}

			printStatusTable(cmd, rows)
			return nil
		},
	}

	cmd.Flags().StringVarP(&targetUser, "user", "u", "", "Operate on a specific user")
	cmd.Flags().StringVar(&overridePath, "config", "", "Override config path")

	return cmd
}

func collectStatusRows(ctx context.Context, targetUID uint32, jobs []config.Job) ([]statusRow, error) {
	rows := make([]statusRow, 0, len(jobs))
	for _, job := range jobs {
		rendered, err := renderJobUnits(targetUID, job)
		if err != nil {
			return nil, err
		}

		timerProps, timerMissing, err := showUnitProperties(ctx, rendered.TimerName, "LastTriggerUSec", "NextElapseUSecRealtime")
		if err != nil {
			return nil, err
		}

		serviceProps, serviceMissing, err := showUnitProperties(ctx, rendered.ServiceName, "Result")
		if err != nil {
			return nil, err
		}

		rows = append(rows, statusRow{
			ID:          job.ID,
			LastRun:     statusTimeValue(timerProps["LastTriggerUSec"], timerMissing),
			NextTrigger: statusTimeValue(timerProps["NextElapseUSecRealtime"], timerMissing),
			Result:      statusResultValue(serviceProps["Result"], serviceMissing),
		})
	}

	return rows, nil
}

func printStatusTable(cmd *cobra.Command, rows []statusRow) {
	tw := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "id\tlast_run\tnext_trigger\tresult")
	for _, row := range rows {
		_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", row.ID, row.LastRun, row.NextTrigger, row.Result)
	}
	_ = tw.Flush()
}

func showUnitProperties(ctx context.Context, unit string, properties ...string) (map[string]string, bool, error) {
	args := make([]string, 0, len(properties)+4)
	args = append(args, "--user", "show", unit)
	for _, property := range properties {
		args = append(args, "--property="+property)
	}

	stdout, stderr, err := runSystemctlShow(ctx, args...)
	if err != nil {
		if isMissingUnitError(stderr) {
			return nil, true, nil
		}
		message := strings.TrimSpace(stderr)
		if message == "" {
			message = err.Error()
		}
		return nil, false, fmt.Errorf("systemctl --user show %s failed: %s", unit, message)
	}

	values := make(map[string]string, len(properties))
	for _, line := range strings.Split(stdout, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		values[parts[0]] = strings.TrimSpace(parts[1])
	}

	return values, false, nil
}

func isMissingUnitError(stderr string) bool {
	trimmed := strings.TrimSpace(stderr)
	if trimmed == "" {
		return false
	}
	return strings.Contains(trimmed, "could not be found") ||
		strings.Contains(trimmed, "not loaded")
}

func statusTimeValue(raw string, missing bool) string {
	if missing {
		return "unknown"
	}
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" || trimmed == "n/a" {
		return "unknown"
	}
	return trimmed
}

func statusResultValue(raw string, missing bool) string {
	if missing {
		return "unknown"
	}
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" || trimmed == "n/a" {
		return "unknown"
	}
	if trimmed == "success" {
		return "pass"
	}
	return "fail"
}
