package cli

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
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
		fromStdin  bool
		targetUser string
	)

	cmd := &cobra.Command{
		Use:   "import",
		Short: "Convert crontab entries into timertab YAML",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := validateTargetUserPermission(targetUser); err != nil {
				return err
			}

			rawCrontab, err := loadCrontabInput(cmd.Context(), cmd.InOrStdin(), fromStdin, targetUser)
			if err != nil {
				return err
			}

			cfg, warnings, err := importCrontab(rawCrontab)
			if err != nil {
				return err
			}

			for _, warning := range warnings {
				_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "%s %s\n", warningPrefix, warning)
			}

			out, err := cfg.MarshalYAML()
			if err != nil {
				return err
			}

			_, err = cmd.OutOrStdout().Write(out)
			if err != nil {
				return err
			}

			return nil
		},
	}

	cmd.Flags().BoolVar(&fromStdin, "stdin", false, "Read crontab input from stdin")
	cmd.Flags().StringVarP(&targetUser, "user", "u", "", "Import another user's crontab")

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

func importCrontab(raw string) (*config.File, []string, error) {
	jobs := make([]config.Job, 0)
	globalEnv := make(map[string]string)
	warnings := make([]string, 0)

	scanner := bufio.NewScanner(strings.NewReader(raw))
	for lineNo := 1; scanner.Scan(); lineNo++ {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		if key, value, ok := parseCrontabEnv(line); ok {
			globalEnv[key] = value
			continue
		}

		schedule, command, ok := parseCrontabEntry(line)
		if !ok {
			warnings = append(warnings, fmt.Sprintf("line %d: unsupported crontab entry %q", lineNo, line))
			continue
		}

		job := config.Job{
			When: config.ScheduleList{schedule},
			Run:  command,
		}
		if len(globalEnv) > 0 {
			job.Env = cloneEnv(globalEnv)
		}

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

	cfg := &config.File{
		Schema:  defaultSchemaURL,
		Version: 1,
		Jobs:    jobs,
	}
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
