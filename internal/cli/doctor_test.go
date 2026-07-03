package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ginden/timertab/internal/config"
	"github.com/ginden/timertab/internal/systemd"
)

func TestCollectDoctorRowsClassifiesTimertabUnits(t *testing.T) {
	unitDir := t.TempDir()
	targetUID := uint32(1000)

	activeJob := config.Job{ID: "active", When: config.ScheduleList{"@daily"}, Run: config.ShellCommand("echo active")}
	orphanJob := config.Job{ID: "orphan", When: config.ScheduleList{"@daily"}, Run: config.ShellCommand("echo orphan")}
	ejectedJob := config.Job{ID: "ejected", When: config.ScheduleList{"@daily"}, Run: config.ShellCommand("echo ejected")}
	otherJob := config.Job{ID: "other", When: config.ScheduleList{"@daily"}, Run: config.ShellCommand("echo other")}

	writeRenderedService := func(instanceID string, job config.Job, mutate func(string) string) string {
		t.Helper()
		units, err := systemd.RenderJobUnits(targetUID, instanceID, job)
		if err != nil {
			t.Fatalf("RenderJobUnits(%s) error = %v", job.ID, err)
		}
		content := units.ServiceContent
		if mutate != nil {
			content = mutate(content)
		}
		path := filepath.Join(unitDir, units.ServiceName)
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatalf("WriteFile(%s) error = %v", path, err)
		}
		return units.ServiceName
	}

	activeName := writeRenderedService(config.DefaultInstanceID, activeJob, nil)
	orphanName := writeRenderedService(config.DefaultInstanceID, orphanJob, nil)
	ejectedName := writeRenderedService(config.DefaultInstanceID, ejectedJob, func(content string) string {
		out, changed := stripManagedMarkers(content, targetUID, config.DefaultInstanceID, ejectedJob.ID)
		if !changed {
			t.Fatalf("stripManagedMarkers() changed = false")
		}
		return out
	})
	otherName := writeRenderedService("work", otherJob, nil)

	rows, err := collectDoctorRows(unitDir, targetUID, config.DefaultInstanceID, []config.Job{activeJob})
	if err != nil {
		t.Fatalf("collectDoctorRows() error = %v", err)
	}

	got := make(map[string]string, len(rows))
	for _, row := range rows {
		got[row.Unit] = row.Class
	}
	want := map[string]string{
		activeName:  "active-config",
		orphanName:  "orphan-managed",
		ejectedName: "ejected-or-foreign",
		otherName:   "managed-other-instance",
	}
	for unit, wantClass := range want {
		if got[unit] != wantClass {
			t.Fatalf("class[%s] = %q, want %q; rows=%#v", unit, got[unit], wantClass, rows)
		}
	}
}
