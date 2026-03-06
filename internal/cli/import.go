package cli

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/ginden/timertab/internal/config"
)

var validImportedEnv = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

var runCrontabList = func(ctx context.Context, targetUser string) (string, error) {
	args := []string{"-l"}
	if strings.TrimSpace(targetUser) != "" {
		args = append([]string{"-u", strings.TrimSpace(targetUser)}, args...)
	}

	cmd := exec.CommandContext(ctx, "crontab", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		message := strings.TrimSpace(string(output))
		if strings.Contains(strings.ToLower(message), "no crontab for") {
			return "", nil
		}
		if message == "" {
			message = err.Error()
		}
		return "", fmt.Errorf("crontab %s failed: %s", strings.Join(args, " "), message)
	}

	return string(output), nil
}

func newImportCommand() *cobra.Command {
	var (
		fromStdin    bool
		targetUser   string
		forceStdout  bool
		noApply      bool
		dryRun       bool
		overridePath string
	)

	cmd := &cobra.Command{
		Use:   "import",
		Short: "Import crontab entries into timertab config",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := validateTargetUserPermission(targetUser); err != nil {
				return err
			}

			rawCrontab, err := loadCrontabInput(cmd.Context(), cmd.InOrStdin(), fromStdin, targetUser)
			if err != nil {
				return err
			}

			imported, warnings, err := importCrontab(rawCrontab)
			if err != nil {
				return err
			}

			for _, warning := range warnings {
				_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "%s %s\n", warningPrefix, warning)
			}

			stdoutMode := forceStdout || !stdoutIsTTY(cmd.OutOrStdout())
			if stdoutMode {
				out, err := imported.MarshalYAML()
				if err != nil {
					return err
				}
				_, err = cmd.OutOrStdout().Write(out)
				return err
			}

			cfgPath, err := resolveConfigPath(targetUser, overridePath)
			if err != nil {
				return err
			}

			edited, err := editImportedConfig(cmd.Context(), cmd, imported)
			if err != nil {
				return err
			}

			loaded, err := loadOrCreateConfig(cfgPath)
			if err != nil {
				return err
			}

			loaded.Jobs = append(loaded.Jobs, edited.Jobs...)
			if err := loaded.NormalizeIDs(); err != nil {
				return err
			}

			if dryRun {
				printImportDryRun(cmd, cfgPath, edited)
				return nil
			}

			if err := saveConfig(cfgPath, loaded); err != nil {
				return err
			}

			if noApply {
				cmd.Printf("timertab: saved %s (no apply)\n", cfgPath)
				return nil
			}

			report, err := runSystemctlApply(cmd.Context(), loaded, targetUser)
			if err != nil {
				return err
			}

			cmd.Printf("timertab: saved %s\n", cfgPath)
			printApplyReport(cmd, report)
			return nil
		},
	}

	cmd.Flags().BoolVar(&fromStdin, "stdin", false, "Read crontab input from stdin")
	cmd.Flags().StringVarP(&targetUser, "user", "u", "", "Import another user's crontab")
	cmd.Flags().BoolVar(&forceStdout, "stdout", false, "Emit generated YAML to stdout")
	cmd.Flags().BoolVar(&noApply, "no-apply", false, "Merge into config but do not reconcile systemd units")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Preview imported jobs without writing config")
	cmd.Flags().StringVar(&overridePath, "config", "", "Override config path")

	return cmd
}

func loadCrontabInput(ctx context.Context, in io.Reader, fromStdin bool, targetUser string) (string, error) {
	if fromStdin || stdinHasData(in) {
		buf, err := io.ReadAll(in)
		if err != nil {
			return "", fmt.Errorf("read stdin: %w", err)
		}
		return string(buf), nil
	}

	return runCrontabList(ctx, targetUser)
}

func stdinHasData(in io.Reader) bool {
	file, ok := in.(*os.File)
	if !ok {
		return false
	}

	info, err := file.Stat()
	if err != nil {
		return false
	}

	return info.Mode()&os.ModeCharDevice == 0
}

func stdoutIsTTY(out io.Writer) bool {
	file, ok := out.(*os.File)
	if !ok {
		return false
	}

	info, err := file.Stat()
	if err != nil {
		return false
	}

	return info.Mode()&os.ModeCharDevice != 0
}

func importCrontab(raw string) (*config.File, []string, error) {
	jobs := make([]config.Job, 0)
	globalEnv := make(map[string]string)
	warnings := make([]string, 0)
	pendingComment := ""

	scanner := bufio.NewScanner(strings.NewReader(raw))
	for lineNo := 1; scanner.Scan(); lineNo++ {
		rawLine := scanner.Text()
		line := strings.TrimSpace(rawLine)
		if line == "" {
			pendingComment = ""
			continue
		}

		if strings.HasPrefix(line, "#") {
			pendingComment = strings.TrimSpace(strings.TrimPrefix(line, "#"))
			continue
		}

		if key, value, ok := parseCrontabEnv(line); ok {
			switch key {
			case "MAILTO", "SHELL":
				warnings = append(warnings, fmt.Sprintf("line %d: skipped %s (no systemd equivalent)", lineNo, key))
			default:
				globalEnv[key] = value
			}
			pendingComment = ""
			continue
		}

		schedule, command, ok := parseCrontabEntry(line)
		if !ok {
			warnings = append(warnings, fmt.Sprintf("line %d: unsupported crontab entry %q", lineNo, line))
			pendingComment = ""
			continue
		}

		strippedCommand, inlineCommentRemoved := stripInlineComment(command)
		if inlineCommentRemoved {
			warnings = append(warnings, fmt.Sprintf("line %d: stripped inline comment from command", lineNo))
		}
		strippedCommand, hadPercent := stripPercentPayload(strippedCommand)
		if hadPercent {
			warnings = append(warnings, fmt.Sprintf("line %d: stripped %% payload from command (cron stdin syntax is unsupported)", lineNo))
		}
		if strings.TrimSpace(strippedCommand) == "" {
			warnings = append(warnings, fmt.Sprintf("line %d: command is empty after import cleanup", lineNo))
			pendingComment = ""
			continue
		}

		if _, err := config.CompileTimerDirectives(config.ScheduleList{schedule}); err != nil {
			warnings = append(warnings, fmt.Sprintf("line %d: invalid schedule %q: %v", lineNo, schedule, err))
			pendingComment = ""
			continue
		}

		job := config.Job{
			When: config.ScheduleList{schedule},
			Run:  strings.TrimSpace(strippedCommand),
		}
		if pendingComment != "" {
			job.Name = pendingComment
		}
		if len(globalEnv) > 0 {
			job.Env = cloneEnv(globalEnv)
		}
		pendingComment = ""

		validationCfg := &config.File{Version: 1, Jobs: []config.Job{job}}
		if err := validationCfg.Validate(); err != nil {
			warnings = append(warnings, fmt.Sprintf("line %d: %v", lineNo, err))
			continue
		}

		jobs = append(jobs, job)
	}
	if err := scanner.Err(); err != nil {
		return nil, nil, fmt.Errorf("read crontab lines: %w", err)
	}

	cfg := &config.File{Schema: defaultSchemaURL, Version: 1, Jobs: jobs}
	if err := cfg.NormalizeIDs(); err != nil {
		return nil, nil, err
	}

	return cfg, warnings, nil
}

func parseCrontabEnv(line string) (string, string, bool) {
	index := strings.IndexByte(line, '=')
	if index <= 0 {
		return "", "", false
	}

	key := strings.TrimSpace(line[:index])
	if !validImportedEnv.MatchString(key) {
		return "", "", false
	}

	value := strings.TrimSpace(line[index+1:])
	return key, value, true
}

func parseCrontabEntry(line string) (string, string, bool) {
	fields := strings.Fields(line)
	if len(fields) < 2 {
		return "", "", false
	}

	if strings.HasPrefix(fields[0], "@") {
		schedule := fields[0]
		command := strings.TrimSpace(strings.TrimPrefix(line, schedule))
		if command == "" {
			return "", "", false
		}
		return schedule, command, true
	}

	if len(fields) < 6 {
		return "", "", false
	}

	schedule := strings.Join(fields[:5], " ")
	command := strings.TrimSpace(strings.TrimPrefix(line, schedule))
	if command == "" {
		return "", "", false
	}

	return schedule, command, true
}

func stripInlineComment(command string) (string, bool) {
	idx := strings.Index(command, " #")
	if idx < 0 {
		return command, false
	}
	return strings.TrimSpace(command[:idx]), true
}

func stripPercentPayload(command string) (string, bool) {
	if !strings.Contains(command, "%") {
		return command, false
	}
	parts := strings.Split(command, "%")
	joined := strings.Join(parts, "")
	return strings.TrimSpace(joined), true
}

func editImportedConfig(ctx context.Context, cmd *cobra.Command, imported *config.File) (*config.File, error) {
	if imported == nil {
		return nil, fmt.Errorf("imported config is required")
	}

	tmpFile, err := os.CreateTemp("", "timertab-import-*.yaml")
	if err != nil {
		return nil, err
	}
	tmpName := tmpFile.Name()
	defer os.Remove(tmpName)

	initial, err := imported.MarshalYAML()
	if err != nil {
		_ = tmpFile.Close()
		return nil, err
	}
	if _, err := tmpFile.Write(initial); err != nil {
		_ = tmpFile.Close()
		return nil, err
	}
	if err := tmpFile.Close(); err != nil {
		return nil, err
	}

	editor, err := resolveEditor()
	if err != nil {
		return nil, err
	}

	for {
		editCmd := exec.CommandContext(ctx, "sh", "-lc", `$EDITOR_CMD "$1"`, "timertab-editor", tmpName)
		editCmd.Env = append(os.Environ(), "EDITOR_CMD="+editor)
		editCmd.Stdin = cmd.InOrStdin()
		editCmd.Stdout = cmd.OutOrStdout()
		editCmd.Stderr = cmd.ErrOrStderr()
		if err := editCmd.Run(); err != nil {
			return nil, fmt.Errorf("editor failed: %w", err)
		}

		edited, err := config.LoadFromFile(tmpName)
		if err != nil {
			printEditValidationError(cmd, err)
			continue
		}
		if err := edited.NormalizeIDs(); err != nil {
			printEditValidationError(cmd, err)
			continue
		}

		return edited, nil
	}
}

func printImportDryRun(cmd *cobra.Command, cfgPath string, imported *config.File) {
	cmd.Printf("would merge %d imported job(s) into %s\n", len(imported.Jobs), cfgPath)
	for _, job := range imported.Jobs {
		name := strings.TrimSpace(job.Name)
		if name == "" {
			name = "(unnamed)"
		}
		when := ""
		if len(job.When) > 0 {
			when = job.When[0]
		}
		cmd.Printf("- %s\tid=%s\twhen=%s\n", name, job.ID, when)
	}

	envKeys := make(map[string]struct{})
	for _, job := range imported.Jobs {
		for key := range job.Env {
			envKeys[key] = struct{}{}
		}
	}
	if len(envKeys) == 0 {
		return
	}

	keys := make([]string, 0, len(envKeys))
	for key := range envKeys {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	cmd.Printf("preserved env keys: %s\n", strings.Join(keys, ", "))
}

func cloneEnv(values map[string]string) map[string]string {
	if len(values) == 0 {
		return nil
	}

	cloned := make(map[string]string, len(values))
	for key, value := range values {
		cloned[key] = value
	}

	return cloned
}
