package cli

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/ginden/timertab/internal/config"
	"github.com/ginden/timertab/internal/progress"
	"github.com/ginden/timertab/internal/systemctl"
)

var runSystemctlApply = applyEditedConfig
var runDryRunPlan = previewEditedConfig

const warningPrefix = "⚠️"
const errorPrefix = "🚨"

func listConfig(cmd *cobra.Command, cfgPath string) error {
	data, err := os.ReadFile(cfgPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			cmd.Println("# no timertab file found")
			return nil
		}
		return err
	}

	var out bytes.Buffer
	fmt.Fprintf(&out, "# %s\n", cfgPath)
	out.Write(data)
	if len(data) == 0 || data[len(data)-1] != '\n' {
		out.WriteString("\n")
	}

	_, err = io.WriteString(cmd.OutOrStdout(), highlightForCommand(cmd, "yaml", out.String()))
	if err != nil {
		return err
	}

	return nil
}

func editConfig(cmd *cobra.Command, cfgPath string, noApply, dryRun, noCommit bool) error {
	if err := os.MkdirAll(filepath.Dir(cfgPath), 0o755); err != nil {
		return err
	}

	tmpFile, err := os.CreateTemp("", "timertab-edit-*.yaml")
	if err != nil {
		return err
	}
	tmpName := tmpFile.Name()
	defer os.Remove(tmpName)

	var beforeConfig *config.File
	existing, err := os.ReadFile(cfgPath)
	if err == nil {
		beforeConfig = parseConfigForAutoCommit(existing)
		if _, err := tmpFile.Write(existing); err != nil {
			_ = tmpFile.Close()
			return err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		_ = tmpFile.Close()
		return err
	} else {
		if _, err := io.WriteString(tmpFile, config.DefaultTemplate); err != nil {
			_ = tmpFile.Close()
			return err
		}
	}

	if err := tmpFile.Close(); err != nil {
		return err
	}

	editor, err := resolveEditor()
	if err != nil {
		return err
	}

	for {
		// Run via shell so values like EDITOR=\"code --wait\" work.
		editCmd := exec.Command("sh", "-lc", `$EDITOR_CMD "$1"`, "timertab-editor", tmpName)
		editCmd.Env = append(os.Environ(), "EDITOR_CMD="+editor)
		editCmd.Stdin = cmd.InOrStdin()
		editCmd.Stdout = cmd.OutOrStdout()
		editCmd.Stderr = cmd.ErrOrStderr()
		if err := editCmd.Run(); err != nil {
			return fmt.Errorf("editor failed: %w", err)
		}

		editedRaw, err := os.ReadFile(tmpName)
		if err != nil {
			return err
		}

		progress.Printf(cmd.Context(), "timertab: validating edited config")
		loaded, out, err := prepareEditedConfigForSave(editedRaw)
		if err != nil {
			printEditValidationError(cmd, err)
			continue
		}

		configChanged := !bytes.Equal(existing, out)

		if dryRun {
			progress.Printf(cmd.Context(), "timertab: building reconcile preview")
			report, err := runDryRunPlan(cmd.Context(), loaded)
			if err != nil {
				return err
			}
			printDryRunReport(cmd, report)
			return nil
		}

		progress.Printf(cmd.Context(), "timertab: saving config to %s", cfgPath)
		if err := os.WriteFile(cfgPath, out, 0o644); err != nil {
			return err
		}

		if noApply {
			cmd.Printf("timertab: saved %s (no apply)\n", cfgPath)
			return nil
		}

		progress.Printf(cmd.Context(), "timertab: reconciling systemd units")
		report, err := runSystemctlApply(cmd.Context(), loaded)
		if err != nil {
			return err
		}

		cmd.Printf("timertab: saved %s\n", cfgPath)
		printApplyReport(cmd, report)

		if !noCommit {
			maybeAutoCommitEditedConfig(cmd.Context(), cmd.ErrOrStderr(), cfgPath, beforeConfig, loaded, configChanged)
		}

		return nil
	}
}

func prepareEditedConfigForSave(raw []byte) (*config.File, []byte, error) {
	loaded, err := config.LoadFromBytes(raw)
	if err != nil {
		return nil, nil, err
	}

	originalIDs := snapshotJobIDs(loaded.Jobs)
	if err := loaded.NormalizeIDs(); err != nil {
		return nil, nil, err
	}

	// Preserve the user's hand-edited layout whenever normalization is a no-op.
	// Reformatting only to prove validity would make repeated edit cycles noisy.
	if !jobIDsChanged(originalIDs, loaded.Jobs) {
		// Keep exact user formatting/comments when IDs are already present.
		return loaded, raw, nil
	}

	out, err := injectGeneratedIDsIntoYAML(raw, loaded)
	if err == nil {
		return loaded, out, nil
	}

	// Fallback to canonical marshaling if node-based patching fails.
	out, marshalErr := loaded.MarshalYAML()
	if marshalErr != nil {
		return nil, nil, marshalErr
	}

	return loaded, out, nil
}

func snapshotJobIDs(jobs []config.Job) []string {
	ids := make([]string, len(jobs))
	for idx, job := range jobs {
		ids[idx] = job.ID
	}
	return ids
}

func jobIDsChanged(before []string, after []config.Job) bool {
	if len(before) != len(after) {
		return true
	}
	for idx, job := range after {
		if before[idx] != job.ID {
			return true
		}
	}
	return false
}

func injectGeneratedIDsIntoYAML(raw []byte, normalized *config.File) ([]byte, error) {
	// Patch only the missing ids back into the original node tree so comments and
	// field ordering survive automatic ID generation.
	return patchConfigYAML(raw, func(doc *yaml.Node) error {
		jobsNode, err := jobsSequenceNode(doc)
		if err != nil {
			return err
		}
		return patchMissingJobIDs(jobsNode, normalized.Jobs)
	})
}

func mappingNodeValue(node *yaml.Node, key string) *yaml.Node {
	if node == nil || node.Kind != yaml.MappingNode {
		return nil
	}
	for idx := 0; idx+1 < len(node.Content); idx += 2 {
		keyNode := node.Content[idx]
		if keyNode.Kind == yaml.ScalarNode && keyNode.Value == key {
			return node.Content[idx+1]
		}
	}
	return nil
}

func parseConfigForAutoCommit(buf []byte) *config.File {
	if len(buf) == 0 {
		return nil
	}

	loaded, err := config.LoadFromBytes(buf)
	if err != nil {
		return nil
	}
	if err := loaded.NormalizeIDs(); err != nil {
		return nil
	}

	return loaded
}

func printDryRunReport(cmd *cobra.Command, report applyReport) {
	for _, path := range report.Created {
		cmd.Printf("would create %s\n", path)
	}
	for _, path := range report.Modified {
		cmd.Printf("would modify %s\n", path)
	}
	for _, path := range report.Deleted {
		cmd.Printf("would delete %s\n", path)
	}

	cmd.Printf("summary: create=%d modify=%d delete=%d\n", len(report.Created), len(report.Modified), len(report.Deleted))
}

func printApplyReport(cmd *cobra.Command, report applyReport) {
	for _, path := range report.Created {
		cmd.Printf("created %s\n", path)
	}
	for _, path := range report.Modified {
		cmd.Printf("modified %s\n", path)
	}
	for _, path := range report.Deleted {
		cmd.Printf("deleted %s\n", path)
	}
	for _, unit := range report.DisabledTimers {
		cmd.Printf("disabled %s\n", unit)
	}
	for _, unit := range report.StoppedTimers {
		cmd.Printf("stopped %s\n", unit)
	}
	if report.ReloadedDaemon {
		label := strings.TrimSpace(report.DaemonLabel)
		if label == "" {
			label = systemctl.UserScope.DaemonLabel()
		}
		cmd.Printf("reloaded %s\n", label)
	}
	for _, unit := range report.EnabledTimers {
		cmd.Printf("enabled %s\n", unit)
	}
	for _, unit := range report.StartedTimers {
		cmd.Printf("started %s\n", unit)
	}
	for _, warning := range report.Warnings {
		_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "%s %s\n", warningPrefix, warning)
	}
}

func printEditValidationError(cmd *cobra.Command, err error) {
	_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "%s timertab: config is invalid: %v\n", errorPrefix, err)
	_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "%s timertab: reopen editor to fix validation errors\n", errorPrefix)
}

func resolveEditor() (string, error) {
	for _, key := range []string{"VISUAL", "EDITOR"} {
		value := strings.TrimSpace(os.Getenv(key))
		if value != "" {
			return value, nil
		}
	}

	if _, err := exec.LookPath("editor"); err == nil {
		return "editor", nil
	}
	if _, err := exec.LookPath("vi"); err == nil {
		return "vi", nil
	}

	return "", errors.New("no editor found; set $VISUAL or $EDITOR")
}
