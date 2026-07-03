package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/ginden/timertab/internal/config"
)

type doctorRow struct {
	Unit     string
	Class    string
	Instance string
	JobID    string
	Path     string
}

func newDoctorCommand() *cobra.Command {
	var overridePath string

	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Show orphaned, ejected, and foreign timertab unit files",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfgPath, err := resolveConfigPath(overridePath)
			if err != nil {
				return err
			}

			loaded, err := config.LoadFromFile(cfgPath)
			if err != nil {
				if errors.Is(err, os.ErrNotExist) {
					loaded = &config.File{Version: 1, Jobs: []config.Job{}}
				} else {
					return err
				}
			}
			if err := loaded.NormalizeIDs(); err != nil {
				return err
			}

			targetUID, err := resolveCurrentUID()
			if err != nil {
				return err
			}
			unitDir, err := resolveSystemdUnitDir(targetUID)
			if err != nil {
				return err
			}

			rows, err := collectDoctorRows(unitDir, targetUID, loaded.EffectiveInstanceID(), loaded.Jobs)
			if err != nil {
				return err
			}
			printDoctorRows(cmd, rows)
			return nil
		},
	}

	cmd.Flags().StringVar(&overridePath, "config", "", "Override config path")
	return cmd
}

func collectDoctorRows(unitDir string, targetUID uint32, instanceID string, jobs []config.Job) ([]doctorRow, error) {
	active := make(map[string]struct{})
	desired, err := buildDesiredState(targetUID, instanceID, jobs)
	if err != nil {
		return nil, err
	}
	for _, unit := range desired.units {
		active[unit.Name] = struct{}{}
	}

	entries, err := os.ReadDir(unitDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("read unit directory %q: %w", unitDir, err)
	}

	rows := make([]doctorRow, 0)
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !isTimertabUnitForUID(name, targetUID) {
			continue
		}

		path := filepath.Join(unitDir, name)
		contentBytes, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read unit %q: %w", path, err)
		}
		markers := parseUnitMarkers(string(contentBytes))
		class := classifyDoctorUnit(name, active, markers, targetUID, instanceID)
		rows = append(rows, doctorRow{
			Unit:     name,
			Class:    class,
			Instance: markers.instanceID(),
			JobID:    markers.jobID,
			Path:     path,
		})
	}

	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Class == rows[j].Class {
			return rows[i].Unit < rows[j].Unit
		}
		return rows[i].Class < rows[j].Class
	})
	return rows, nil
}

func printDoctorRows(cmd *cobra.Command, rows []doctorRow) {
	if len(rows) == 0 {
		cmd.Println("No timertab unit files found for this UID.")
		return
	}

	tableRows := make([][]string, 0, len(rows)+1)
	tableRows = append(tableRows, []string{"class", "instance", "job_id", "unit", "path"})
	for _, row := range rows {
		tableRows = append(tableRows, []string{row.Class, row.Instance, row.JobID, row.Unit, row.Path})
	}
	printStatusAlignedTable(cmd.OutOrStdout(), tableRows, 2)
}

type unitMarkers struct {
	managed     bool
	uid         string
	instance    string
	sawInstance bool
	jobID       string
}

func parseUnitMarkers(content string) unitMarkers {
	var markers unitMarkers
	for _, rawLine := range strings.Split(content, "\n") {
		line := strings.TrimSpace(rawLine)
		switch {
		case line == "# timertab-managed: true":
			markers.managed = true
		case strings.HasPrefix(line, "# timertab-uid: "):
			markers.uid = strings.TrimSpace(strings.TrimPrefix(line, "# timertab-uid: "))
		case strings.HasPrefix(line, "# timertab-instance-id: "):
			markers.sawInstance = true
			markers.instance = strings.TrimSpace(strings.TrimPrefix(line, "# timertab-instance-id: "))
		case strings.HasPrefix(line, "# timertab-job-id: "):
			markers.jobID = strings.TrimSpace(strings.TrimPrefix(line, "# timertab-job-id: "))
		}
	}
	return markers
}

func (m unitMarkers) instanceID() string {
	if !m.sawInstance || strings.TrimSpace(m.instance) == "" {
		return config.DefaultInstanceID
	}
	return m.instance
}

func classifyDoctorUnit(name string, active map[string]struct{}, markers unitMarkers, targetUID uint32, instanceID string) string {
	if _, ok := active[name]; ok {
		if markers.managed && markers.uid == fmt.Sprintf("%d", targetUID) && markers.jobID != "" && markers.instanceID() == normalizeDoctorInstanceID(instanceID) {
			return "active-config"
		}
		return "ejected-or-foreign"
	}
	if !markers.managed || markers.uid != fmt.Sprintf("%d", targetUID) || markers.jobID == "" {
		return "ejected-or-foreign"
	}
	if markers.instanceID() != normalizeDoctorInstanceID(instanceID) {
		return "managed-other-instance"
	}
	return "orphan-managed"
}

func isTimertabUnitForUID(name string, targetUID uint32) bool {
	if !(strings.HasSuffix(name, ".service") || strings.HasSuffix(name, ".timer")) {
		return false
	}
	if strings.HasPrefix(name, fmt.Sprintf("timertab-u%d-", targetUID)) {
		return true
	}
	return strings.HasPrefix(name, "timertab-") && strings.Contains(name, fmt.Sprintf("-u%d-", targetUID))
}

func normalizeDoctorInstanceID(instanceID string) string {
	if strings.TrimSpace(instanceID) == "" {
		return config.DefaultInstanceID
	}
	return strings.TrimSpace(instanceID)
}
