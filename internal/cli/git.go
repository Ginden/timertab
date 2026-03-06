package cli

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strings"

	"github.com/ginden/timertab/internal/config"
)

var findGitBinary = exec.LookPath

var runGitCommand = func(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...)
	out, err := cmd.CombinedOutput()
	output := strings.TrimSpace(string(out))
	if err != nil {
		if output == "" {
			output = err.Error()
		}
		return "", fmt.Errorf("git %s failed: %s", strings.Join(args, " "), output)
	}
	return output, nil
}

func maybeAutoCommitEditedConfig(
	ctx context.Context,
	stderr io.Writer,
	cfgPath string,
	before, after *config.File,
	changed bool,
) {
	if !changed || after == nil || !after.AutoCommitEnabled() {
		return
	}

	if _, err := findGitBinary("git"); err != nil {
		_, _ = fmt.Fprintf(stderr, "%s git binary is unavailable; skipping config auto-commit\n", warningPrefix)
		return
	}

	dir := filepath.Dir(cfgPath)
	fileName := filepath.Base(cfgPath)

	if err := ensureGitRepo(ctx, dir); err != nil {
		_, _ = fmt.Fprintf(stderr, "%s failed to initialize git repository for auto-commit: %v\n", warningPrefix, err)
		return
	}

	if _, err := runGitCommand(ctx, dir, "add", "--", fileName); err != nil {
		_, _ = fmt.Fprintf(stderr, "%s failed to stage config for auto-commit: %v\n", warningPrefix, err)
		return
	}

	message := buildAutoCommitMessage(before, after)
	if _, err := runGitCommand(ctx, dir, "commit", "-m", message, "--", fileName); err != nil {
		if isNothingToCommitError(err) {
			return
		}
		_, _ = fmt.Fprintf(stderr, "%s failed to auto-commit config change: %v\n", warningPrefix, err)
	}
}

func ensureGitRepo(ctx context.Context, dir string) error {
	if _, err := runGitCommand(ctx, dir, "rev-parse", "--is-inside-work-tree"); err == nil {
		return nil
	}

	_, err := runGitCommand(ctx, dir, "init")
	return err
}

func isNothingToCommitError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "nothing to commit") || strings.Contains(msg, "no changes added to commit")
}

func buildAutoCommitMessage(before, after *config.File) string {
	afterByID := jobsByID(after)
	beforeByID := jobsByID(before)

	added := make([]string, 0)
	edited := make([]string, 0)
	removed := make([]string, 0)

	for id, afterJob := range afterByID {
		beforeJob, exists := beforeByID[id]
		if !exists {
			added = append(added, id)
			continue
		}
		if !reflect.DeepEqual(beforeJob, afterJob) {
			edited = append(edited, id)
		}
	}

	for id := range beforeByID {
		if _, exists := afterByID[id]; !exists {
			removed = append(removed, id)
		}
	}

	sort.Strings(added)
	sort.Strings(edited)
	sort.Strings(removed)

	parts := make([]string, 0, 3)
	if len(added) > 0 {
		parts = append(parts, "add "+formatJobIDs(added))
	}
	if len(removed) > 0 {
		parts = append(parts, "remove "+formatJobIDs(removed))
	}
	if len(edited) > 0 {
		parts = append(parts, "edit "+formatJobIDs(edited))
	}

	if len(parts) == 0 {
		return "timertab: update config"
	}

	return "timertab: " + strings.Join(parts, "; ")
}

func jobsByID(file *config.File) map[string]config.Job {
	if file == nil {
		return map[string]config.Job{}
	}

	jobs := make(map[string]config.Job, len(file.Jobs))
	for _, job := range file.Jobs {
		if strings.TrimSpace(job.ID) == "" {
			continue
		}
		jobs[job.ID] = job
	}

	return jobs
}

func formatJobIDs(ids []string) string {
	if len(ids) == 1 {
		return "job " + ids[0]
	}
	return "jobs " + strings.Join(ids, ",")
}
