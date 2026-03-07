package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/ginden/timertab/internal/config"
)

type statusRow struct {
	ID          string `json:"id"`
	LastRun     string `json:"last_run"`
	NextTrigger string `json:"next_trigger"`
	Result      string `json:"result"`
}

type statusJSONPayload struct {
	Jobs []statusRow `json:"jobs"`
}

type statusDetail struct {
	Job            config.Job
	ConfigPath     string
	UnitDir        string
	ServiceName    string
	TimerName      string
	ServicePath    string
	TimerPath      string
	ServiceProps   map[string]string
	TimerProps     map[string]string
	ServiceMissing bool
	TimerMissing   bool
	ServiceBody    string
	TimerBody      string
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
		outputJSON   bool
	)

	cmd := &cobra.Command{
		Use:   "status [id]",
		Short: "Show status for configured jobs or one detailed job report",
		Args:  cobra.MaximumNArgs(1),
		ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			if len(args) > 0 {
				return nil, cobra.ShellCompDirectiveNoFileComp
			}
			return completeJobIDs(targetUser, overridePath, toComplete)
		},
		RunE: func(cmd *cobra.Command, args []string) error {
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

			if len(args) == 0 {
				rows, err := collectStatusRows(cmd.Context(), targetUID, loaded.Jobs)
				if err != nil {
					return err
				}

				if outputJSON {
					return printStatusJSON(cmd, rows)
				}

				printStatusTable(cmd, rows)
				return nil
			}

			if outputJSON {
				return fmt.Errorf("--json is only supported for summary status")
			}

			jobID := strings.TrimSpace(args[0])
			if jobID == "" {
				return fmt.Errorf("job id cannot be empty")
			}

			jobIndex := indexOfJobID(loaded.Jobs, jobID)
			if jobIndex < 0 {
				return fmt.Errorf("job %q not found", jobID)
			}

			unitDir, err := resolveSystemdUserUnitDir(targetUser)
			if err != nil {
				return err
			}

			detail, err := collectStatusDetail(cmd.Context(), cfgPath, unitDir, targetUID, loaded.Jobs[jobIndex])
			if err != nil {
				return err
			}

			printStatusDetail(cmd, detail)
			return nil
		},
	}

	cmd.Flags().StringVarP(&targetUser, "user", "u", "", "Operate on a specific user")
	cmd.Flags().StringVar(&overridePath, "config", "", "Override config path")
	cmd.Flags().BoolVar(&outputJSON, "json", false, "Print machine-readable JSON output")

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

func collectStatusDetail(ctx context.Context, cfgPath, unitDir string, targetUID uint32, job config.Job) (statusDetail, error) {
	rendered, err := renderJobUnits(targetUID, job)
	if err != nil {
		return statusDetail{}, err
	}

	serviceProps, serviceMissing, err := showUnitProperties(ctx, rendered.ServiceName,
		"LoadState",
		"ActiveState",
		"SubState",
		"Result",
		"FragmentPath",
		"UnitFileState",
	)
	if err != nil {
		return statusDetail{}, err
	}

	timerProps, timerMissing, err := showUnitProperties(ctx, rendered.TimerName,
		"LoadState",
		"ActiveState",
		"SubState",
		"LastTriggerUSec",
		"NextElapseUSecRealtime",
		"FragmentPath",
		"UnitFileState",
	)
	if err != nil {
		return statusDetail{}, err
	}

	return statusDetail{
		Job:            job,
		ConfigPath:     cfgPath,
		UnitDir:        unitDir,
		ServiceName:    rendered.ServiceName,
		TimerName:      rendered.TimerName,
		ServicePath:    statusFilePath(serviceProps["FragmentPath"], filepath.Join(unitDir, rendered.ServiceName), serviceMissing),
		TimerPath:      statusFilePath(timerProps["FragmentPath"], filepath.Join(unitDir, rendered.TimerName), timerMissing),
		ServiceProps:   serviceProps,
		TimerProps:     timerProps,
		ServiceMissing: serviceMissing,
		TimerMissing:   timerMissing,
		ServiceBody:    rendered.ServiceContent,
		TimerBody:      rendered.TimerContent,
	}, nil
}

func printStatusTable(cmd *cobra.Command, rows []statusRow) {
	tw := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "id\tlast_run\tnext_trigger\tresult")
	for _, row := range rows {
		_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", row.ID, row.LastRun, row.NextTrigger, row.Result)
	}
	_ = tw.Flush()
}

func printStatusJSON(cmd *cobra.Command, rows []statusRow) error {
	encoder := json.NewEncoder(cmd.OutOrStdout())
	encoder.SetEscapeHTML(false)
	return encoder.Encode(statusJSONPayload{Jobs: rows})
}

func printStatusDetail(cmd *cobra.Command, detail statusDetail) {
	out := cmd.OutOrStdout()

	_, _ = fmt.Fprintf(out, "job: %s\n", detail.Job.ID)
	if strings.TrimSpace(detail.Job.Name) != "" {
		_, _ = fmt.Fprintf(out, "name: %s\n", detail.Job.Name)
	}
	_, _ = fmt.Fprintf(out, "config: %s\n", detail.ConfigPath)
	_, _ = fmt.Fprintf(out, "unit_dir: %s\n", detail.UnitDir)
	_, _ = fmt.Fprintf(out, "service: %s\n", detail.ServiceName)
	_, _ = fmt.Fprintf(out, "timer: %s\n", detail.TimerName)
	_, _ = fmt.Fprintf(out, "\n")

	printStatusPropertyTable(out, "runtime", [][2]string{
		{"service_load", statusValue(detail.ServiceProps["LoadState"], detail.ServiceMissing)},
		{"service_active", statusValue(detail.ServiceProps["ActiveState"], detail.ServiceMissing)},
		{"service_sub", statusValue(detail.ServiceProps["SubState"], detail.ServiceMissing)},
		{"service_result", statusResultValue(detail.ServiceProps["Result"], detail.ServiceMissing)},
		{"timer_load", statusValue(detail.TimerProps["LoadState"], detail.TimerMissing)},
		{"timer_active", statusValue(detail.TimerProps["ActiveState"], detail.TimerMissing)},
		{"timer_sub", statusValue(detail.TimerProps["SubState"], detail.TimerMissing)},
		{"last_run", statusTimeValue(detail.TimerProps["LastTriggerUSec"], detail.TimerMissing)},
		{"next_trigger", statusTimeValue(detail.TimerProps["NextElapseUSecRealtime"], detail.TimerMissing)},
		{"timer_enabled", statusValue(detail.TimerProps["UnitFileState"], detail.TimerMissing)},
	})
	_, _ = fmt.Fprintln(out)

	printStatusPropertyTable(out, "files", [][2]string{
		{"service_path", detail.ServicePath},
		{"timer_path", detail.TimerPath},
	})
	_, _ = fmt.Fprintln(out)

	jobYAML := mustMarshalStatusYAML(detail.Job)
	_, _ = fmt.Fprintln(out, "job_definition:")
	_, _ = fmt.Fprint(out, indentBlock(jobYAML, "  "))
	if !strings.HasSuffix(jobYAML, "\n") {
		_, _ = fmt.Fprintln(out)
	}
	_, _ = fmt.Fprintln(out)

	_, _ = fmt.Fprintln(out, "service_definition:")
	_, _ = fmt.Fprint(out, indentBlock(detail.ServiceBody, "  "))
	if !strings.HasSuffix(detail.ServiceBody, "\n") {
		_, _ = fmt.Fprintln(out)
	}
	_, _ = fmt.Fprintln(out)

	_, _ = fmt.Fprintln(out, "timer_definition:")
	_, _ = fmt.Fprint(out, indentBlock(detail.TimerBody, "  "))
	if !strings.HasSuffix(detail.TimerBody, "\n") {
		_, _ = fmt.Fprintln(out)
	}
	_, _ = fmt.Fprintln(out)

	_, _ = fmt.Fprintln(out, "diagnostics:")
	for _, command := range statusDiagnosticCommands(detail) {
		_, _ = fmt.Fprintln(out, "  "+colorizeStatusCommand(out, command))
	}
}

func printStatusPropertyTable(out io.Writer, title string, rows [][2]string) {
	_, _ = fmt.Fprintf(out, "%s:\n", title)
	tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	for _, row := range rows {
		_, _ = fmt.Fprintf(tw, "  %s\t%s\n", row[0], row[1])
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

func statusValue(raw string, missing bool) string {
	if missing {
		return "missing"
	}
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" || trimmed == "n/a" {
		return "unknown"
	}
	return trimmed
}

func statusFilePath(fragmentPath, fallback string, missing bool) string {
	if missing {
		return fallback + " (missing)"
	}
	trimmed := strings.TrimSpace(fragmentPath)
	if trimmed == "" || trimmed == "n/a" {
		return fallback
	}
	return trimmed
}

func mustMarshalStatusYAML(job config.Job) string {
	data, err := yaml.Marshal(job)
	if err != nil {
		return fmt.Sprintf("marshal error: %v\n", err)
	}
	return string(data)
}

func indentBlock(content, prefix string) string {
	if content == "" {
		return prefix + "\n"
	}
	lines := strings.SplitAfter(content, "\n")
	var b strings.Builder
	for _, line := range lines {
		if line == "" {
			continue
		}
		b.WriteString(prefix)
		b.WriteString(line)
	}
	if !strings.HasSuffix(content, "\n") {
		b.WriteString("\n")
	}
	return b.String()
}

func statusDiagnosticCommands(detail statusDetail) []string {
	return []string{
		fmt.Sprintf("systemctl --user status %s", detail.ServiceName),
		fmt.Sprintf("systemctl --user status %s", detail.TimerName),
		fmt.Sprintf("systemctl --user cat %s", detail.ServiceName),
		fmt.Sprintf("systemctl --user cat %s", detail.TimerName),
		fmt.Sprintf("systemctl --user show %s --property=Result,ExecMainStatus,FragmentPath", detail.ServiceName),
		fmt.Sprintf("systemctl --user list-timers %s", detail.TimerName),
		fmt.Sprintf("journalctl --user -u %s -n 100 --no-pager", detail.ServiceName),
	}
}

func colorizeStatusCommand(out io.Writer, command string) string {
	if !statusWriterSupportsANSI(out) {
		return command
	}
	const (
		ansiCommand = "\x1b[1;36m"
		ansiReset   = "\x1b[0m"
	)
	return ansiCommand + command + ansiReset
}

func statusWriterSupportsANSI(out io.Writer) bool {
	file, ok := out.(*os.File)
	if !ok {
		return false
	}
	info, err := file.Stat()
	if err != nil {
		return false
	}
	return (info.Mode() & os.ModeCharDevice) != 0
}
