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
	"time"
	"unicode"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/ginden/timertab/internal/config"
)

var validImportedEnv = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

var importOutputIsTTY = writerIsTTY

// ignoredCrontabEnv lists global crontab variables that have no useful systemd equivalent.
// They are filtered out with a per-line warning instead of being propagated to job env.
var ignoredCrontabEnv = map[string]string{
	"MAILTO": "cron email output has no systemd equivalent",
	"SHELL":  "systemd does not use $SHELL for ExecStart; add an explicit shell invocation in run: if needed",
}

var runCrontabList = func(ctx context.Context) (string, error) {
	args := []string{"-l"}
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
		forceStdout  bool
		noApply      bool
		dryRun       bool
		noCommit     bool
		overridePath string
	)

	cmd := &cobra.Command{
		Use:   "import",
		Short: "Convert crontab entries into timertab YAML",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if dryRun && noApply {
				return fmt.Errorf("--dry-run cannot be combined with --no-apply")
			}
			if dryRun && forceStdout {
				return fmt.Errorf("--dry-run cannot be combined with --stdout")
			}

			rawCrontab, err := loadCrontabInput(cmd.Context(), cmd.InOrStdin(), fromStdin)
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

			// Stdout/pipe mode: no TTY on stdout, or forced via --stdout flag.
			if forceStdout || (!dryRun && !importOutputIsTTY(cmd.OutOrStdout())) {
				out, err := imported.MarshalYAML()
				if err != nil {
					return err
				}
				_, err = cmd.OutOrStdout().Write(out)
				return err
			}

			cfgPath, err := resolveConfigPath(overridePath)
			if err != nil {
				return err
			}

			if dryRun {
				return importDryRun(cmd, cfgPath, imported)
			}

			return withConfigLock(cfgPath, func() error {
				return importInteractive(cmd, cfgPath, imported, noApply, noCommit)
			})
		},
	}

	cmd.Flags().BoolVar(&fromStdin, "stdin", false, "Read crontab input from stdin")
	cmd.Flags().BoolVar(&forceStdout, "stdout", false, "Force YAML-to-stdout mode even on a TTY")
	cmd.Flags().BoolVar(&noApply, "no-apply", false, "Merge into config but skip systemd reconcile")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Preview what would be merged without writing anything")
	cmd.Flags().BoolVar(&noCommit, "no-commit", false, "Skip git auto-commit of the config change")
	cmd.Flags().StringVar(&overridePath, "config", "", "Override config path")
	_ = cmd.Flags().MarkDeprecated("dry-run", "use `--stdout` for YAML export today; review-bundle rendering will replace import-time dry runs")

	return cmd
}

func loadCrontabInput(ctx context.Context, in io.Reader, fromStdin bool) (string, error) {
	if fromStdin || stdinHasData(in) {
		buf, err := io.ReadAll(in)
		if err != nil {
			return "", fmt.Errorf("read stdin: %w", err)
		}
		return string(buf), nil
	}

	return runCrontabList(ctx)
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
	var prevComment string
	var currentTZ string

	scanner := bufio.NewScanner(strings.NewReader(raw))
	for lineNo := 1; scanner.Scan(); lineNo++ {
		line := strings.TrimSpace(scanner.Text())

		if line == "" {
			prevComment = ""
			continue
		}

		if strings.HasPrefix(line, "#") {
			prevComment = strings.TrimSpace(strings.TrimPrefix(line, "#"))
			continue
		}

		if key, value, ok := parseCrontabEnv(line); ok {
			prevComment = ""
			if key == "CRON_TZ" {
				if _, err := time.LoadLocation(value); err != nil {
					warnings = append(warnings, fmt.Sprintf("line %d: ignoring CRON_TZ=%q — invalid time zone", lineNo, value))
					currentTZ = ""
					continue
				}
				currentTZ = value
				continue
			}
			if reason, ignored := ignoredCrontabEnv[key]; ignored {
				warnings = append(warnings, fmt.Sprintf("line %d: ignoring %s=%q — %s", lineNo, key, value, reason))
				continue
			}
			globalEnv[key] = value
			continue
		}

		schedule, command, ok := parseCrontabEntry(line)
		if !ok {
			prevComment = ""
			warnings = append(warnings, fmt.Sprintf("line %d: unsupported crontab entry %q", lineNo, line))
			continue
		}

		// Strip cron % separator (text after % becomes stdin in cron; not supported in systemd).
		stripped, hadPercent := stripCronPercent(command)
		command = stripped
		if hadPercent {
			warnings = append(warnings, fmt.Sprintf("line %d: %% character stripped from command (cron stdin syntax has no systemd equivalent)", lineNo))
		}

		// Strip inline bash comments from the command.
		if cleaned, comment := stripInlineComment(command); comment != "" {
			warnings = append(warnings, fmt.Sprintf("line %d: inline comment %q stripped from command", lineNo, comment))
			command = cleaned
		}

		command = strings.TrimSpace(command)
		if command == "" {
			prevComment = ""
			warnings = append(warnings, fmt.Sprintf("line %d: command is empty after stripping", lineNo))
			continue
		}

		job := config.Job{
			Name: prevComment,
			When: config.ScheduleList{schedule},
			TZ:   currentTZ,
			Run:  config.ShellCommand(command),
		}
		if len(globalEnv) > 0 {
			job.Env = cloneEnv(globalEnv)
		}

		validationCfg := &config.File{Version: 1, Jobs: []config.Job{job}}
		if err := validationCfg.Validate(); err != nil {
			prevComment = ""
			warnings = append(warnings, fmt.Sprintf("line %d: %v", lineNo, err))
			continue
		}

		jobs = append(jobs, job)
		prevComment = ""
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

// stripCronPercent removes the cron stdin separator % and everything after it.
// In crontab, an unescaped % in a command becomes a newline; text after it is
// piped as stdin to the command. \% is a literal %. Returns the cleaned command
// and whether truncation occurred.
func stripCronPercent(command string) (string, bool) {
	i := 0
	for i < len(command) {
		if command[i] == '\\' && i+1 < len(command) && command[i+1] == '%' {
			i += 2 // skip \%, it's a literal percent
			continue
		}
		if command[i] == '%' {
			return unescapeCronLiteralPercent(strings.TrimRight(command[:i], " \t")), true
		}
		i++
	}
	return unescapeCronLiteralPercent(command), false
}

func unescapeCronLiteralPercent(command string) string {
	return strings.ReplaceAll(command, `\%`, `%`)
}

// stripInlineComment removes a trailing bash inline comment (space followed by #)
// from a command, respecting single- and double-quoted strings and backslash escaping.
// Returns (cleaned command, stripped comment). If no comment is found, comment is "".
func stripInlineComment(command string) (string, string) {
	inSingle := false
	inDouble := false
	i := 0
	for i < len(command) {
		c := command[i]
		// Backslash escaping: skip the next character when outside quotes or inside double quotes.
		if c == '\\' && !inSingle && i+1 < len(command) {
			i += 2
			continue
		}
		if c == '\'' && !inDouble {
			inSingle = !inSingle
		} else if c == '"' && !inSingle {
			inDouble = !inDouble
		} else if c == '#' && !inSingle && !inDouble {
			if i > 0 && (command[i-1] == ' ' || command[i-1] == '\t') {
				return strings.TrimRight(command[:i], " \t"), command[i:]
			}
		}
		i++
	}
	return command, ""
}

// importDryRun prints a preview of what would be merged into cfgPath without writing.
func importDryRun(cmd *cobra.Command, cfgPath string, imported *config.File) error {
	existing, err := loadOrCreateConfig(cfgPath)
	if err != nil {
		return err
	}

	merged := mergeImportedJobs(existing.Jobs, imported.Jobs)

	cmd.Printf("would merge %d new job(s) into %s (currently %d job(s), skipping %d duplicate import job(s)):\n",
		merged.Added, cfgPath, len(existing.Jobs), merged.Skipped)
	for _, job := range merged.AddedJobs {
		name := job.Name
		if name == "" {
			name = "(no name)"
		}
		schedule := ""
		if len(job.When) > 0 {
			schedule = job.When[0]
		}
		cmd.Printf("  + %-30s  %-20s  %s\n", name, schedule, job.Run.Display())
	}

	return nil
}

// importInteractive opens an editor pre-filled with the imported jobs, then merges
// the result into the main config and optionally reconciles systemd units.
func importInteractive(cmd *cobra.Command, cfgPath string, imported *config.File, noApply, noCommit bool) error {
	// Present jobs without pre-assigned IDs so they're regenerated conflict-free after merge.
	importedForEdit := &config.File{
		Schema:  imported.Schema,
		Version: imported.Version,
		Jobs:    make([]config.Job, len(imported.Jobs)),
	}
	copy(importedForEdit.Jobs, imported.Jobs)
	for i := range importedForEdit.Jobs {
		importedForEdit.Jobs[i].ID = ""
	}

	initialYAML, err := importedForEdit.MarshalYAML()
	if err != nil {
		return err
	}

	tmpFile, err := os.CreateTemp("", "timertab-import-*.yaml")
	if err != nil {
		return err
	}
	tmpName := tmpFile.Name()
	defer os.Remove(tmpName)

	if _, err := tmpFile.Write(initialYAML); err != nil {
		_ = tmpFile.Close()
		return err
	}
	if err := tmpFile.Close(); err != nil {
		return err
	}

	editor, err := resolveEditor()
	if err != nil {
		return err
	}

	var editedConfig *config.File
	for {
		editCmd := exec.CommandContext(cmd.Context(), "sh", "-lc", `$EDITOR_CMD "$1"`, "timertab-editor", tmpName)
		editCmd.Env = append(os.Environ(), "EDITOR_CMD="+editor)
		editCmd.Stdin = cmd.InOrStdin()
		editCmd.Stdout = cmd.OutOrStdout()
		editCmd.Stderr = cmd.ErrOrStderr()
		if err := editCmd.Run(); err != nil {
			return fmt.Errorf("editor failed: %w", err)
		}

		loaded, err := config.LoadFromFile(tmpName)
		if err != nil {
			printEditValidationError(cmd, err)
			continue
		}

		editedConfig = loaded
		break
	}

	if len(editedConfig.Jobs) == 0 {
		cmd.Println("timertab: no jobs to import")
		return nil
	}

	existing, err := loadOrCreateConfig(cfgPath)
	if err != nil {
		return err
	}
	// Raw bytes drive the comment-preserving node patch; a missing file simply
	// falls back to canonical marshaling inside savePatchedConfig.
	existingRaw, _ := os.ReadFile(cfgPath)
	existingCount := len(existing.Jobs)

	merged := mergeImportedJobs(existing.Jobs, editedConfig.Jobs)
	if merged.Skipped > 0 {
		_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "%s skipped %d duplicate import job(s)\n", warningPrefix, merged.Skipped)
	}
	if merged.Added == 0 {
		cmd.Println("timertab: no new jobs to import")
		return nil
	}

	existing.Jobs = merged.Jobs
	if err := existing.NormalizeIDs(); err != nil {
		return fmt.Errorf("merge failed: %w", err)
	}

	err = savePatchedConfig(cfgPath, existingRaw, existing, existing.Jobs[:existingCount], func(jobsNode *yaml.Node) error {
		return appendJobNodes(jobsNode, existing.Jobs[existingCount:])
	})
	if err != nil {
		return err
	}

	if noApply {
		cmd.Printf("timertab: saved %s (no apply)\n", cfgPath)
		return nil
	}

	if err := ensureSystemdBaseline(); err != nil {
		return err
	}

	report, err := runSystemctlApply(cmd.Context(), existing)
	if err != nil {
		return err
	}

	cmd.Printf("timertab: saved %s\n", cfgPath)
	printApplyReport(cmd, report)

	if !noCommit {
		message := fmt.Sprintf("timertab: import %d job(s)", merged.Added)
		maybeAutoCommitConfig(cmd.Context(), cmd.ErrOrStderr(), cfgPath, existing, message)
	}

	return nil
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
	value = stripMatchingCrontabEnvQuotes(value)
	return key, value, true
}

func stripMatchingCrontabEnvQuotes(value string) string {
	if len(value) < 2 {
		return value
	}

	first := value[0]
	last := value[len(value)-1]
	if (first == '"' || first == '\'') && first == last {
		return value[1 : len(value)-1]
	}

	return value
}

func parseCrontabEntry(line string) (string, string, bool) {
	fields := strings.Fields(line)
	if len(fields) < 2 {
		return "", "", false
	}

	if strings.HasPrefix(fields[0], "@") {
		schedule := fields[0]
		command, ok := commandAfterCrontabFields(line, 1)
		if !ok {
			return "", "", false
		}
		command = strings.TrimSpace(command)
		if command == "" {
			return "", "", false
		}
		return schedule, command, true
	}

	if len(fields) < 6 {
		return "", "", false
	}

	schedule := strings.Join(fields[:5], " ")
	command, ok := commandAfterCrontabFields(line, 5)
	if !ok {
		return "", "", false
	}
	command = strings.TrimSpace(command)
	if command == "" {
		return "", "", false
	}

	return schedule, command, true
}

func commandAfterCrontabFields(line string, fieldCount int) (string, bool) {
	if fieldCount <= 0 {
		return strings.TrimSpace(line), true
	}

	// Scan the original line instead of rebuilding it from Fields so repeated
	// spaces and tabs in the command area never get mistaken for schedule text.
	inField := false
	fieldsSeen := 0
	for idx, r := range line {
		if unicode.IsSpace(r) {
			if !inField {
				continue
			}
			fieldsSeen++
			if fieldsSeen == fieldCount {
				return line[idx:], true
			}
			inField = false
			continue
		}

		inField = true
	}

	if inField {
		fieldsSeen++
	}

	return "", fieldsSeen >= fieldCount
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

type importMergeResult struct {
	Jobs      []config.Job
	AddedJobs []config.Job
	Added     int
	Skipped   int
}

func mergeImportedJobs(existing []config.Job, imported []config.Job) importMergeResult {
	merged := make([]config.Job, 0, len(existing)+len(imported))
	merged = append(merged, existing...)

	seen := make(map[string]struct{}, len(existing)+len(imported))
	for _, job := range existing {
		seen[importJobIdentity(job)] = struct{}{}
	}

	addedJobs := make([]config.Job, 0, len(imported))
	skipped := 0
	for _, job := range imported {
		identity := importJobIdentity(job)
		if _, exists := seen[identity]; exists {
			skipped++
			continue
		}
		seen[identity] = struct{}{}
		merged = append(merged, job)
		addedJobs = append(addedJobs, job)
	}

	return importMergeResult{
		Jobs:      merged,
		AddedJobs: addedJobs,
		Added:     len(addedJobs),
		Skipped:   skipped,
	}
}

func importJobIdentity(job config.Job) string {
	schedules := make([]string, len(job.When))
	for idx, schedule := range job.When {
		schedules[idx] = strings.TrimSpace(schedule)
	}
	sort.Strings(schedules)

	envKeys := make([]string, 0, len(job.Env))
	for key := range job.Env {
		envKeys = append(envKeys, key)
	}
	sort.Strings(envKeys)

	// Duplicate detection intentionally ignores human-facing metadata like name/id;
	// import should dedupe by execution semantics, not by how the entry was labeled.
	var b strings.Builder
	b.Grow(128)
	b.WriteString("run:")
	b.WriteString(job.Run.DigestKey())
	b.WriteByte('\x1f')
	b.WriteString("cwd:")
	b.WriteString(strings.TrimSpace(job.Cwd))
	b.WriteByte('\x1f')
	b.WriteString("when:")
	for _, schedule := range schedules {
		b.WriteString(schedule)
		b.WriteByte('\x1e')
	}
	b.WriteByte('\x1f')
	b.WriteString("env:")
	for _, key := range envKeys {
		b.WriteString(key)
		b.WriteByte('=')
		b.WriteString(job.Env[key])
		b.WriteByte('\x1e')
	}

	return b.String()
}
