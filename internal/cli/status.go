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
	"github.com/ginden/timertab/internal/systemctl"
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
	LogPeek        string
	Scope          systemctl.Scope
}

type statusDiagnosticStep struct {
	Title   string
	Why     string
	Command string
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
			return completeJobIDs(overridePath, toComplete)
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			cfgPath, err := resolveConfigPath(overridePath)
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

			targetUID, err := resolveCurrentUID()
			if err != nil {
				return err
			}
			instanceID := loaded.EffectiveInstanceID()
			scope := systemctl.ScopeForUID(targetUID)

			if len(args) == 0 {
				rows, err := collectStatusRows(cmd.Context(), scope, targetUID, instanceID, loaded.Jobs)
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

			unitDir, err := resolveSystemdUnitDir(targetUID)
			if err != nil {
				return err
			}

			detail, err := collectStatusDetail(cmd.Context(), cfgPath, unitDir, scope, targetUID, instanceID, loaded.Jobs[jobIndex])
			if err != nil {
				return err
			}

			printStatusDetail(cmd, detail)
			return nil
		},
	}

	cmd.Flags().StringVar(&overridePath, "config", "", "Override config path")
	cmd.Flags().BoolVar(&outputJSON, "json", false, "Print machine-readable JSON output")

	return cmd
}

func collectStatusRows(ctx context.Context, scope systemctl.Scope, targetUID uint32, instanceID string, jobs []config.Job) ([]statusRow, error) {
	rows := make([]statusRow, 0, len(jobs))
	for _, job := range jobs {
		rendered, err := renderJobUnits(targetUID, instanceID, job)
		if err != nil {
			return nil, err
		}

		timerProps, timerMissing, err := showUnitProperties(ctx, scope, rendered.TimerName, "LastTriggerUSec", "NextElapseUSecRealtime")
		if err != nil {
			return nil, err
		}

		serviceProps, serviceMissing, err := showUnitProperties(ctx, scope, rendered.ServiceName, "Result")
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

func collectStatusDetail(ctx context.Context, cfgPath, unitDir string, scope systemctl.Scope, targetUID uint32, instanceID string, job config.Job) (statusDetail, error) {
	rendered, err := renderJobUnits(targetUID, instanceID, job)
	if err != nil {
		return statusDetail{}, err
	}

	serviceProps, serviceMissing, err := showUnitProperties(ctx, scope, rendered.ServiceName,
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

	timerProps, timerMissing, err := showUnitProperties(ctx, scope, rendered.TimerName,
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
		LogPeek:        collectStatusLogPeek(ctx, scope, rendered.ServiceName),
		Scope:          scope,
	}, nil
}

func printStatusTable(cmd *cobra.Command, rows []statusRow) {
	out := cmd.OutOrStdout()
	tableRows := make([][]string, 0, len(rows)+1)
	tableRows = append(tableRows, []string{"id", "last_run", "next_trigger", "result"})
	for _, row := range rows {
		tableRows = append(tableRows, []string{
			colorizeStatusJobID(out, row.ID),
			row.LastRun,
			row.NextTrigger,
			colorizeStatusSummaryResult(out, row.Result),
		})
	}
	printStatusAlignedTable(out, tableRows, 2)
}

func printStatusJSON(cmd *cobra.Command, rows []statusRow) error {
	encoder := json.NewEncoder(cmd.OutOrStdout())
	encoder.SetEscapeHTML(false)
	return encoder.Encode(statusJSONPayload{Jobs: rows})
}

func printStatusDetail(cmd *cobra.Command, detail statusDetail) {
	out := cmd.OutOrStdout()
	jobYAML := mustMarshalStatusYAML(detail.Job)
	result := statusResultValue(detail.ServiceProps["Result"], detail.ServiceMissing)

	printStatusHeadline(out, detail.Job.ID, detail.Job.Name, result)
	printStatusTableSection(out, "Overview", []string{"field", "value"}, [][]string{
		{"job", detail.Job.ID},
		{"name", statusDisplayName(detail.Job.Name)},
		{"result", colorizeStatusResult(out, result)},
		{"last run", statusTimeValue(detail.TimerProps["LastTriggerUSec"], detail.TimerMissing)},
		{"next trigger", statusTimeValue(detail.TimerProps["NextElapseUSecRealtime"], detail.TimerMissing)},
		{"config", detail.ConfigPath},
		{"unit dir", detail.UnitDir},
	})
	printStatusTableSection(out, "Units", []string{"kind", "unit", "load", "active", "sub", "enabled", "path"}, [][]string{
		{
			"service",
			detail.ServiceName,
			statusValue(detail.ServiceProps["LoadState"], detail.ServiceMissing),
			statusValue(detail.ServiceProps["ActiveState"], detail.ServiceMissing),
			statusValue(detail.ServiceProps["SubState"], detail.ServiceMissing),
			statusValue(detail.ServiceProps["UnitFileState"], detail.ServiceMissing),
			detail.ServicePath,
		},
		{
			"timer",
			detail.TimerName,
			statusValue(detail.TimerProps["LoadState"], detail.TimerMissing),
			statusValue(detail.TimerProps["ActiveState"], detail.TimerMissing),
			statusValue(detail.TimerProps["SubState"], detail.TimerMissing),
			statusValue(detail.TimerProps["UnitFileState"], detail.TimerMissing),
			detail.TimerPath,
		},
	})
	printStatusBlockSection(out, "Job YAML", jobYAML)
	printStatusBlockSection(out, "Service Unit", detail.ServiceBody)
	printStatusBlockSection(out, "Timer Unit", detail.TimerBody)
	printStatusBlockSection(out, "Recent Logs", detail.LogPeek)
	printStatusCommandsSection(out, statusDiagnosticSteps(detail))
}

func printStatusHeadline(out io.Writer, jobID, name, result string) {
	label := "STATUS " + jobID
	if trimmed := strings.TrimSpace(name); trimmed != "" {
		label += " - " + trimmed
	}
	_, _ = fmt.Fprintln(out, label)
	_, _ = fmt.Fprintln(out, strings.Repeat("=", len(label)))
	_, _ = fmt.Fprintf(out, "Result: %s\n\n", colorizeStatusResult(out, result))
}

func printStatusTableSection(out io.Writer, title string, headers []string, rows [][]string) {
	printStatusSectionTitle(out, title)
	tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, strings.Join(headers, "\t"))
	for _, row := range rows {
		_, _ = fmt.Fprintln(tw, strings.Join(row, "\t"))
	}
	_ = tw.Flush()
	_, _ = fmt.Fprintln(out)
}

func printStatusBlockSection(out io.Writer, title, content string) {
	printStatusSectionTitle(out, title)
	_, _ = fmt.Fprint(out, indentBlock(content, "  "))
	if !strings.HasSuffix(content, "\n") {
		_, _ = fmt.Fprintln(out)
	}
	_, _ = fmt.Fprintln(out)
}

func printStatusCommandsSection(out io.Writer, steps []statusDiagnosticStep) {
	printStatusSectionTitle(out, "Diagnostics")
	_, _ = fmt.Fprintln(out, "  Start with the first command, then continue only if you need more detail.")
	_, _ = fmt.Fprintln(out)
	for idx, step := range steps {
		_, _ = fmt.Fprintf(out, "  %d. %s\n", idx+1, step.Title)
		_, _ = fmt.Fprintf(out, "     %s\n", step.Why)
		_, _ = fmt.Fprintf(out, "     %s\n", colorizeStatusCommand(out, step.Command))
		_, _ = fmt.Fprintln(out)
	}
}

func printStatusSectionTitle(out io.Writer, title string) {
	_, _ = fmt.Fprintln(out, colorizeStatusSectionTitle(out, title))
	_, _ = fmt.Fprintln(out, strings.Repeat("-", len(title)))
}

func printStatusAlignedTable(out io.Writer, rows [][]string, gap int) {
	if len(rows) == 0 {
		return
	}

	widths := make([]int, len(rows[0]))
	for _, row := range rows {
		for idx, cell := range row {
			if idx >= len(widths) {
				break
			}
			width := statusPrintableWidth(cell)
			if width > widths[idx] {
				widths[idx] = width
			}
		}
	}

	for _, row := range rows {
		for idx, cell := range row {
			_, _ = io.WriteString(out, cell)
			if idx == len(row)-1 || idx >= len(widths)-1 {
				continue
			}
			padding := widths[idx] - statusPrintableWidth(cell) + gap
			if padding < gap {
				padding = gap
			}
			_, _ = io.WriteString(out, strings.Repeat(" ", padding))
		}
		_, _ = io.WriteString(out, "\n")
	}
}

func showUnitProperties(ctx context.Context, scope systemctl.Scope, unit string, properties ...string) (map[string]string, bool, error) {
	args := make([]string, 0, len(properties)+4)
	args = append(args, scope.ScopedArgs("show", unit)...)
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
		return nil, false, fmt.Errorf("%s failed: %s", scope.CommandString("systemctl", "show", unit), message)
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

func collectStatusLogPeek(ctx context.Context, scope systemctl.Scope, serviceName string) string {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	err := runJournalctl(ctx, bytes.NewReader(nil), &stdout, &stderr, scope.ScopedArgs("-u", serviceName, "-n", "20", "--no-pager")...)
	if err != nil {
		message := strings.TrimSpace(stderr.String())
		if message == "" {
			message = err.Error()
		}
		return "log preview unavailable: " + message + "\n"
	}

	output := strings.TrimRight(stdout.String(), "\n")
	if strings.TrimSpace(output) == "" {
		return "(no recent logs)\n"
	}
	return output + "\n"
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

func statusPrintableWidth(value string) int {
	width := 0
	for idx := 0; idx < len(value); {
		if value[idx] == '\x1b' {
			end := strings.IndexByte(value[idx:], 'm')
			if end < 0 {
				break
			}
			idx += end + 1
			continue
		}
		width++
		idx++
	}
	return width
}

func statusDiagnosticSteps(detail statusDetail) []statusDiagnosticStep {
	return []statusDiagnosticStep{
		{
			Title:   "Check the last service run",
			Why:     "Shows whether the job failed, the recent exit summary, and the most relevant status lines.",
			Command: detail.Scope.CommandString("systemctl", "status", detail.ServiceName),
		},
		{
			Title:   "Check whether the timer is armed",
			Why:     "Confirms that the timer unit is loaded and whether systemd believes it is waiting for the next trigger.",
			Command: detail.Scope.CommandString("systemctl", "status", detail.TimerName),
		},
		{
			Title:   "Read recent logs",
			Why:     "Best next step when the service failed or exited unexpectedly; journal output usually contains the real error.",
			Command: detail.Scope.CommandString("journalctl", "-u", detail.ServiceName, "-n", "100", "--no-pager"),
		},
		{
			Title:   "Inspect service metadata only",
			Why:     "Useful when you want the exact result code and the resolved unit file path without the extra prose from status.",
			Command: detail.Scope.CommandString("systemctl", "show", detail.ServiceName, "--property=Result,ExecMainStatus,FragmentPath"),
		},
		{
			Title:   "View the loaded service unit",
			Why:     "Shows the final unit content that systemd sees, including drop-ins and the generated ExecStart/ExecStopPost lines.",
			Command: detail.Scope.CommandString("systemctl", "cat", detail.ServiceName),
		},
		{
			Title:   "View the loaded timer unit",
			Why:     "Lets you confirm the real OnCalendar schedule, persistence, and timer-specific overrides after generation.",
			Command: detail.Scope.CommandString("systemctl", "cat", detail.TimerName),
		},
		{
			Title:   "List timer scheduling details",
			Why:     "Shows the next and previous trigger times in the context of the active timers managed by this systemd instance.",
			Command: detail.Scope.CommandString("systemctl", "list-timers", detail.TimerName),
		},
	}
}

func colorizeStatusResult(out io.Writer, result string) string {
	if !statusWriterSupportsANSI(out) {
		return strings.ToUpper(result)
	}
	const ansiReset = "\x1b[0m"
	switch result {
	case "pass":
		return "\x1b[1;32mPASS" + ansiReset
	case "fail":
		return "\x1b[1;31mFAIL" + ansiReset
	default:
		return "\x1b[1;33mUNKNOWN" + ansiReset
	}
}

func colorizeStatusSummaryResult(out io.Writer, result string) string {
	if !statusWriterSupportsANSI(out) {
		return result
	}
	return colorizeStatusResult(out, result)
}

func colorizeStatusJobID(out io.Writer, jobID string) string {
	if !statusWriterSupportsANSI(out) {
		return jobID
	}
	return "\x1b[1;34m" + jobID + "\x1b[0m"
}

func colorizeStatusSectionTitle(out io.Writer, title string) string {
	if !statusWriterSupportsANSI(out) {
		return title
	}
	return "\x1b[1m" + title + "\x1b[0m"
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

func statusDisplayName(name string) string {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return "-"
	}
	return trimmed
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
