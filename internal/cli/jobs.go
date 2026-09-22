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

			return withConfigLock(cfgPath, func() error {
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

				serviceResult, err := prepareUnitMarkerChange(servicePath, targetUID, instanceID, job.ID, false)
				if err != nil {
					return err
				}
				timerResult, err := prepareUnitMarkerChange(timerPath, targetUID, instanceID, job.ID, false)
				if err != nil {
					return err
				}
				if err := writeUnitMarkerChanges(serviceResult, timerResult); err != nil {
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
			})
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
			return withConfigLock(cfgPath, func() error {
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

				if !noApply {
					if err := ensureSystemdBaseline(); err != nil {
						return err
					}
				}

				serviceChange, err := prepareUnitMarkerChange(servicePath, targetUID, instanceID, job.ID, true)
				if err != nil {
					return err
				}
				timerChange, err := prepareUnitMarkerChange(timerPath, targetUID, instanceID, job.ID, true)
				if err != nil {
					return err
				}

				if err := writeUnitMarkerChanges(serviceChange, timerChange); err != nil {
					return err
				}
				if serviceChange.Changed {
					cmd.Printf("adopted %s\n", servicePath)
				}
				if timerChange.Changed {
					cmd.Printf("adopted %s\n", timerPath)
				}
				if !serviceChange.Changed && !timerChange.Changed {
					cmd.Println("timertab: units already carry timertab management markers")
				}

				if noApply {
					cmd.Println("timertab: adopted markers (no apply)")
					return nil
				}

				report, err := runSystemctlApply(cmd.Context(), loaded)
				if err != nil {
					return err
				}
				printApplyReport(cmd, report)
				return nil
			})
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
	if _, err := config.LoadFromBytes(out); err != nil {
		return err
	}

	return writeConfigFile(path, out)
}

func writeConfigFile(path string, data []byte) error {
	// Preserve explicit config symlinks while replacing the target atomically.
	if info, err := os.Lstat(path); err == nil && info.Mode()&os.ModeSymlink != 0 {
		resolved, err := filepath.EvalSymlinks(path)
		if err != nil {
			return err
		}
		path = resolved
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return writeFileAtomically(path, data, 0o600)
}

func writeFileAtomically(path string, data []byte, mode os.FileMode) error {
	file, err := os.CreateTemp(filepath.Dir(path), ".timertab-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	if err := file.Chmod(mode); err != nil {
		_ = file.Close()
		return err
	}
	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return os.Rename(file.Name(), path)
}

func indexOfJobID(jobs []config.Job, id string) int {
	for idx, job := range jobs {
		if job.ID == id {
			return idx
		}
	}
	return -1
}

type unitMarkerChange struct {
	Changed bool
	Missing bool
	path    string
	content string
	mode    os.FileMode
}

// Preflight both units before changing either: a missing file during adopt or
// conflicting ownership must not leave a pair with half its markers changed.
func prepareUnitMarkerChange(path string, targetUID uint32, instanceID, jobID string, adopt bool) (unitMarkerChange, error) {
	info, err := os.Lstat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) && !adopt {
			return unitMarkerChange{Missing: true}, nil
		}
		return unitMarkerChange{}, fmt.Errorf("read unit file %q: %w", path, err)
	}
	if !info.Mode().IsRegular() {
		return unitMarkerChange{}, fmt.Errorf("refusing to change markers on non-regular unit file %q", path)
	}
	contentBytes, err := os.ReadFile(path)
	if err != nil {
		return unitMarkerChange{}, fmt.Errorf("read unit file %q: %w", path, err)
	}
	content := string(contentBytes)
	owned := systemd.IsManagedUnitContentForUID(content, targetUID, instanceID) && contentHasManagedJobID(content, jobID)
	markers := parseUnitMarkers(content)
	if !owned && (markers.managed || markers.uid != "" || markers.sawInstance || markers.jobID != "") {
		return unitMarkerChange{}, fmt.Errorf("refusing to change conflicting ownership markers on unit file %q", path)
	}
	change := unitMarkerChange{path: path, mode: info.Mode().Perm()}
	if adopt {
		change.Changed = !owned
		change.content = managedMarkerBlock(targetUID, instanceID, jobID) + content
	} else if owned {
		change.content, change.Changed = stripManagedMarkers(content, targetUID, instanceID, jobID)
	}
	return change, nil
}

func writeUnitMarkerChanges(changes ...unitMarkerChange) error {
	for _, change := range changes {
		if change.Changed {
			if err := writeFileAtomically(change.path, []byte(change.content), change.mode); err != nil {
				return fmt.Errorf("write unit file %q: %w", change.path, err)
			}
		}
	}
	return nil
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
		case managedMarker, uidMarker, instanceMarker, jobIDMarker,
			"# Generated by timertab; DO NOT EDIT this file directly.",
			"# Humans and automated tools: do not modify this file or recommend direct edits.",
			"# Use `timertab edit` to change the job; timertab will regenerate this unit.":
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
