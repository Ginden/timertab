package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/ginden/timertab/internal/config"
)

const defaultSchemaURL = "https://raw.githubusercontent.com/ginden/timertab/v1.0.0/schema/v1.json"
const defaultAddJobTemplate = `$schema: "https://raw.githubusercontent.com/ginden/timertab/v1.0.0/schema/v1.json"
version: 1
jobs:
  - name: "example"
    when: "@daily"
    run: |-
      echo 'timertab executes commands via /bin/sh -lc'
      echo 'direct executable mode is planned for v2'
`

func newAddCommand() *cobra.Command {
	var (
		targetUser   string
		overridePath string
		noApply      bool
	)

	cmd := &cobra.Command{
		Use:     "add",
		Aliases: []string{"+1"},
		Short:   "Open editor for one new job and append it to timertab config",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := validateTargetUserPermission(targetUser); err != nil {
				return err
			}

			cfgPath, err := resolveConfigPath(targetUser, overridePath)
			if err != nil {
				return err
			}

			loaded, err := loadOrCreateConfig(cfgPath)
			if err != nil {
				return err
			}

			job, err := editSingleJob(cmd.Context(), cmd)
			if err != nil {
				return err
			}
			loaded.Jobs = append(loaded.Jobs, job)

			if err := loaded.NormalizeIDs(); err != nil {
				return err
			}

			if err := saveConfig(cfgPath, loaded); err != nil {
				return err
			}

			if noApply {
				cmd.Printf("timertab: saved %s (no apply)\n", cfgPath)
				return nil
			}

			if err := ensureSystemdBaseline(); err != nil {
				return err
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

	cmd.Flags().StringVarP(&targetUser, "user", "u", "", "Operate on a specific user")
	cmd.Flags().StringVar(&overridePath, "config", "", "Override config path")
	cmd.Flags().BoolVar(&noApply, "no-apply", false, "Validate and save, but do not reconcile systemd units")

	return cmd
}

func newEjectCommand() *cobra.Command {
	var (
		targetUser   string
		overridePath string
	)

	cmd := &cobra.Command{
		Use:   "eject <id>",
		Short: "Stop managing a job while keeping generated systemd units",
		Args:  cobra.ExactArgs(1),
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
			job := loaded.Jobs[jobIndex]

			targetUID, err := resolveTargetUID(targetUser)
			if err != nil {
				return err
			}
			unitDir, err := resolveSystemdUserUnitDir(targetUser)
			if err != nil {
				return err
			}

			rendered, err := renderJobUnits(targetUID, job)
			if err != nil {
				return err
			}

			servicePath := filepath.Join(unitDir, rendered.ServiceName)
			timerPath := filepath.Join(unitDir, rendered.TimerName)

			serviceResult, err := stripManagedMarkersFromUnitFile(servicePath, targetUID, job.ID)
			if err != nil {
				return err
			}
			timerResult, err := stripManagedMarkersFromUnitFile(timerPath, targetUID, job.ID)
			if err != nil {
				return err
			}

			loaded.Jobs = append(loaded.Jobs[:jobIndex], loaded.Jobs[jobIndex+1:]...)
			if err := saveConfig(cfgPath, loaded); err != nil {
				return err
			}

			cmd.Printf("timertab: saved %s\n", cfgPath)
			if serviceResult.Changed {
				cmd.Printf("ejected %s\n", servicePath)
			}
			if timerResult.Changed {
				cmd.Printf("ejected %s\n", timerPath)
			}
			if serviceResult.Missing {
				_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "%s unit file missing: %s\n", warningPrefix, servicePath)
			}
			if timerResult.Missing {
				_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "%s unit file missing: %s\n", warningPrefix, timerPath)
			}

			return nil
		},
	}

	cmd.Flags().StringVarP(&targetUser, "user", "u", "", "Operate on a specific user")
	cmd.Flags().StringVar(&overridePath, "config", "", "Override config path")

	return cmd
}

func loadOrCreateConfig(path string) (*config.File, error) {
	loaded, err := config.LoadFromFile(path)
	if err == nil {
		return loaded, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}

	return &config.File{
		Schema:  defaultSchemaURL,
		Version: 1,
		Jobs:    []config.Job{},
	}, nil
}

func saveConfig(path string, loaded *config.File) error {
	if loaded == nil {
		return fmt.Errorf("config is required")
	}

	out, err := loaded.MarshalYAML()
	if err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}

	if err := os.WriteFile(path, out, 0o644); err != nil {
		return err
	}

	return nil
}

func indexOfJobID(jobs []config.Job, id string) int {
	for idx, job := range jobs {
		if job.ID == id {
			return idx
		}
	}
	return -1
}

type markerStripResult struct {
	Changed bool
	Missing bool
}

func stripManagedMarkersFromUnitFile(path string, targetUID uint32, jobID string) (markerStripResult, error) {
	contentBytes, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return markerStripResult{Missing: true}, nil
		}
		return markerStripResult{}, fmt.Errorf("read unit file %q: %w", path, err)
	}

	content := string(contentBytes)
	updatedContent, changed := stripManagedMarkers(content, targetUID, jobID)
	if !changed {
		return markerStripResult{}, nil
	}

	fileInfo, err := os.Stat(path)
	if err != nil {
		return markerStripResult{}, fmt.Errorf("stat unit file %q: %w", path, err)
	}

	if err := os.WriteFile(path, []byte(updatedContent), fileInfo.Mode().Perm()); err != nil {
		return markerStripResult{}, fmt.Errorf("write unit file %q: %w", path, err)
	}

	return markerStripResult{Changed: true}, nil
}

func stripManagedMarkers(content string, targetUID uint32, jobID string) (string, bool) {
	managedMarker := "# timertab-managed: true"
	uidMarker := fmt.Sprintf("# timertab-uid: %d", targetUID)
	jobIDMarker := "# timertab-job-id: " + jobID

	hasTrailingNewline := strings.HasSuffix(content, "\n")
	lines := strings.Split(content, "\n")

	filtered := make([]string, 0, len(lines))
	changed := false
	for _, line := range lines {
		switch strings.TrimSpace(line) {
		case managedMarker, uidMarker, jobIDMarker:
			changed = true
			continue
		}
		filtered = append(filtered, line)
	}
	if !changed {
		return content, false
	}

	for len(filtered) > 0 && filtered[len(filtered)-1] == "" {
		filtered = filtered[:len(filtered)-1]
	}

	out := strings.Join(filtered, "\n")
	if hasTrailingNewline {
		out += "\n"
	}

	return out, true
}

func editSingleJob(ctx context.Context, cmd *cobra.Command) (config.Job, error) {
	tmpFile, err := os.CreateTemp("", "timertab-add-*.yaml")
	if err != nil {
		return config.Job{}, err
	}
	tmpName := tmpFile.Name()
	defer os.Remove(tmpName)

	if _, err := io.WriteString(tmpFile, defaultAddJobTemplate); err != nil {
		_ = tmpFile.Close()
		return config.Job{}, err
	}
	if err := tmpFile.Close(); err != nil {
		return config.Job{}, err
	}

	editor, err := resolveEditor()
	if err != nil {
		return config.Job{}, err
	}

	for {
		editCmd := exec.CommandContext(ctx, "sh", "-lc", `$EDITOR_CMD "$1"`, "timertab-editor", tmpName)
		editCmd.Env = append(os.Environ(), "EDITOR_CMD="+editor)
		editCmd.Stdin = cmd.InOrStdin()
		editCmd.Stdout = cmd.OutOrStdout()
		editCmd.Stderr = cmd.ErrOrStderr()
		if err := editCmd.Run(); err != nil {
			return config.Job{}, fmt.Errorf("editor failed: %w", err)
		}

		edited, err := os.ReadFile(tmpName)
		if err != nil {
			return config.Job{}, err
		}

		job, err := parseEditedJob(edited)
		if err == nil {
			return job, nil
		}
		printEditValidationError(cmd, err)
	}
}

func parseEditedJob(buf []byte) (config.Job, error) {
	cfg, err := config.LoadFromBytes(buf)
	if err != nil {
		return config.Job{}, err
	}
	if len(cfg.Jobs) != 1 {
		return config.Job{}, fmt.Errorf("add expects exactly one job in jobs, got %d", len(cfg.Jobs))
	}
	if err := cfg.NormalizeIDs(); err != nil {
		return config.Job{}, err
	}

	return cfg.Jobs[0], nil
}
