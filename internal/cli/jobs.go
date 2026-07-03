package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/ginden/timertab/internal/config"
	"github.com/ginden/timertab/internal/systemd"
)

const defaultSchemaURL = "https://raw.githubusercontent.com/ginden/timertab/v1.1.0/schema/v1.json"

func newEjectCommand() *cobra.Command {
	var (
		overridePath string
		noCommit     bool
	)

	cmd := &cobra.Command{
		Use:   "eject <id>",
		Short: "Stop managing a job while keeping generated systemd units",
		Args:  cobra.ExactArgs(1),
		ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			if len(args) > 0 {
				return nil, cobra.ShellCompDirectiveNoFileComp
			}
			return completeJobIDs(overridePath, toComplete)
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			jobID := strings.TrimSpace(args[0])
			if jobID == "" {
				return fmt.Errorf("job id cannot be empty")
			}

			cfgPath, err := resolveConfigPath(overridePath)
			if err != nil {
				return err
			}

			raw, err := os.ReadFile(cfgPath)
			if err != nil {
				return err
			}
			loaded, err := config.LoadFromBytes(raw)
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
			instanceID := loaded.EffectiveInstanceID()

			targetUID, err := resolveCurrentUID()
			if err != nil {
				return err
			}
			unitDir, err := resolveSystemdUnitDir(targetUID)
			if err != nil {
				return err
			}

			rendered, err := renderJobUnits(targetUID, instanceID, job)
			if err != nil {
				return err
			}

			servicePath := filepath.Join(unitDir, rendered.ServiceName)
			timerPath := filepath.Join(unitDir, rendered.TimerName)

			serviceResult, err := stripManagedMarkersFromUnitFile(servicePath, targetUID, instanceID, job.ID)
			if err != nil {
				return err
			}
			timerResult, err := stripManagedMarkersFromUnitFile(timerPath, targetUID, instanceID, job.ID)
			if err != nil {
				return err
			}

			preJobs := make([]config.Job, len(loaded.Jobs))
			copy(preJobs, loaded.Jobs)

			loaded.Jobs = append(loaded.Jobs[:jobIndex], loaded.Jobs[jobIndex+1:]...)
			err = savePatchedConfig(cfgPath, raw, loaded, preJobs, func(jobsNode *yaml.Node) error {
				return removeJobNode(jobsNode, jobIndex)
			})
			if err != nil {
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
			cmd.Println("timertab: ejected units are still installed and may keep running; use `timertab rm` to delete a job and prune its units")

			if !noCommit {
				maybeAutoCommitConfig(cmd.Context(), cmd.ErrOrStderr(), cfgPath, loaded, "timertab: eject job "+jobID)
			}

			return nil
		},
	}

	cmd.Flags().StringVar(&overridePath, "config", "", "Override config path")
	cmd.Flags().BoolVar(&noCommit, "no-commit", false, "Skip git auto-commit of the config change")

	return cmd
}

func newAdoptCommand() *cobra.Command {
	var (
		overridePath string
		noApply      bool
	)

	cmd := &cobra.Command{
		Use:   "adopt <id>",
		Short: "Resume timertab management of previously ejected unit files",
		Args:  cobra.ExactArgs(1),
		ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			if len(args) > 0 {
				return nil, cobra.ShellCompDirectiveNoFileComp
			}
			return completeJobIDs(overridePath, toComplete)
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			jobID := strings.TrimSpace(args[0])
			if jobID == "" {
				return fmt.Errorf("job id cannot be empty")
			}

			cfgPath, err := resolveConfigPath(overridePath)
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

			targetUID, err := resolveCurrentUID()
			if err != nil {
				return err
			}
			unitDir, err := resolveSystemdUnitDir(targetUID)
			if err != nil {
				return err
			}
			instanceID := loaded.EffectiveInstanceID()
			job := loaded.Jobs[jobIndex]

			rendered, err := renderJobUnits(targetUID, instanceID, job)
			if err != nil {
				return err
			}
			servicePath := filepath.Join(unitDir, rendered.ServiceName)
			timerPath := filepath.Join(unitDir, rendered.TimerName)

			serviceChanged, err := addManagedMarkersToUnitFile(servicePath, targetUID, instanceID, job.ID)
			if err != nil {
				return err
			}
			timerChanged, err := addManagedMarkersToUnitFile(timerPath, targetUID, instanceID, job.ID)
			if err != nil {
				return err
			}

			if serviceChanged {
				cmd.Printf("adopted %s\n", servicePath)
			}
			if timerChanged {
				cmd.Printf("adopted %s\n", timerPath)
			}
			if !serviceChanged && !timerChanged {
				cmd.Println("timertab: units already carry timertab management markers")
			}

			if noApply {
				cmd.Println("timertab: adopted markers (no apply)")
				return nil
			}

			if err := ensureSystemdBaseline(); err != nil {
				return err
			}
			report, err := runSystemctlApply(cmd.Context(), loaded)
			if err != nil {
				return err
			}
			printApplyReport(cmd, report)
			return nil
		},
	}

	cmd.Flags().StringVar(&overridePath, "config", "", "Override config path")
	cmd.Flags().BoolVar(&noApply, "no-apply", false, "Restore markers but skip systemd reconcile")

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

	return writeConfigFile(path, out)
}

func writeConfigFile(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}

	return os.WriteFile(path, data, 0o644)
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

func stripManagedMarkersFromUnitFile(path string, targetUID uint32, instanceID, jobID string) (markerStripResult, error) {
	contentBytes, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return markerStripResult{Missing: true}, nil
		}
		return markerStripResult{}, fmt.Errorf("read unit file %q: %w", path, err)
	}

	content := string(contentBytes)
	updatedContent, changed := stripManagedMarkers(content, targetUID, instanceID, jobID)
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

func stripManagedMarkers(content string, targetUID uint32, instanceID, jobID string) (string, bool) {
	managedMarker := "# timertab-managed: true"
	uidMarker := fmt.Sprintf("# timertab-uid: %d", targetUID)
	instanceMarker := "# timertab-instance-id: " + config.DefaultInstanceID
	if strings.TrimSpace(instanceID) != "" {
		instanceMarker = "# timertab-instance-id: " + strings.TrimSpace(instanceID)
	}
	jobIDMarker := "# timertab-job-id: " + jobID

	hasTrailingNewline := strings.HasSuffix(content, "\n")
	lines := strings.Split(content, "\n")

	filtered := make([]string, 0, len(lines))
	changed := false
	for _, line := range lines {
		switch strings.TrimSpace(line) {
		case managedMarker, uidMarker, instanceMarker, jobIDMarker:
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

func addManagedMarkersToUnitFile(path string, targetUID uint32, instanceID, jobID string) (bool, error) {
	contentBytes, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, fmt.Errorf("unit file %q does not exist; cannot adopt", path)
		}
		return false, fmt.Errorf("read unit file %q: %w", path, err)
	}

	content := string(contentBytes)
	if systemd.IsManagedUnitContentForUID(content, targetUID, instanceID) {
		if !contentHasManagedJobID(content, jobID) {
			return false, fmt.Errorf("unit file %q is already managed for another job", path)
		}
		return false, nil
	}

	markers := managedMarkerBlock(targetUID, instanceID, jobID)
	if strings.HasPrefix(content, markers) {
		return false, nil
	}

	fileInfo, err := os.Stat(path)
	if err != nil {
		return false, fmt.Errorf("stat unit file %q: %w", path, err)
	}
	if err := os.WriteFile(path, []byte(markers+content), fileInfo.Mode().Perm()); err != nil {
		return false, fmt.Errorf("write unit file %q: %w", path, err)
	}

	return true, nil
}

func contentHasManagedJobID(content, jobID string) bool {
	want := "# timertab-job-id: " + jobID
	for _, line := range strings.Split(content, "\n") {
		if strings.TrimSpace(line) == want {
			return true
		}
	}
	return false
}

func managedMarkerBlock(targetUID uint32, instanceID, jobID string) string {
	effectiveInstanceID := config.DefaultInstanceID
	if strings.TrimSpace(instanceID) != "" {
		effectiveInstanceID = strings.TrimSpace(instanceID)
	}
	return strings.Join([]string{
		"# timertab-managed: true",
		fmt.Sprintf("# timertab-uid: %d", targetUID),
		"# timertab-instance-id: " + effectiveInstanceID,
		"# timertab-job-id: " + jobID,
		"",
	}, "\n")
}
