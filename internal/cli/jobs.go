package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/ginden/timertab/internal/config"
)

const defaultSchemaURL = "https://raw.githubusercontent.com/ginden/timertab/v1.0.0/schema/v1.json"

func newAddCommand() *cobra.Command {
	var (
		targetUser    string
		overridePath  string
		name          string
		id            string
		noApply       bool
		disabledTimer bool
	)

	cmd := &cobra.Command{
		Use:     "add <when> <run>",
		Aliases: []string{"+1"},
		Short:   "Append one job to timertab config",
		Args:    cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
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

			job := config.Job{
				ID:   strings.TrimSpace(id),
				Name: strings.TrimSpace(name),
				When: config.ScheduleList{args[0]},
				Run:  args[1],
			}
			if disabledTimer {
				enabled := false
				job.Enabled = &enabled
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
	cmd.Flags().StringVar(&name, "name", "", "Optional job name")
	cmd.Flags().StringVar(&id, "id", "", "Optional job id")
	cmd.Flags().BoolVar(&noApply, "no-apply", false, "Validate and save, but do not reconcile systemd units")
	cmd.Flags().BoolVar(&disabledTimer, "disabled", false, "Create job disabled")

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
				_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "warning: unit file missing: %s\n", servicePath)
			}
			if timerResult.Missing {
				_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "warning: unit file missing: %s\n", timerPath)
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
