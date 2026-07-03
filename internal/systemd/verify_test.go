package systemd

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ginden/timertab/internal/config"
)

func TestRenderedUnitsVerifyWithSystemdAnalyze(t *testing.T) {
	if _, err := exec.LookPath("systemd-analyze"); err != nil {
		t.Skip("systemd-analyze not found")
	}

	jobs := []config.Job{
		{
			ID:   "percent-and-hooks",
			When: config.ScheduleList{"0 9 * * Mon"},
			TZ:   "UTC",
			Run:  config.ShellCommand(`date +%F && printf '%s\n' "$HOME"`),
			Env: map[string]string{
				"DATE_FORMAT": "%Y-%m-%d",
			},
			Cwd: "/tmp",
			OnSuccess: &config.Hook{
				Command: `printf '%s\n' ok`,
				Env: map[string]string{
					"HOOK_FORMAT": "%s",
				},
			},
			OnFailure: &config.Hook{
				Command: `printf '%s\n' failed`,
			},
		},
		{
			ID:   "argv-and-raw",
			When: config.ScheduleList{"@hourly", "@reboot"},
			Run:  config.ExecCommand("/usr/bin/env", "bash", "-lc", `echo "quoted % value"`),
			Systemd: &config.Systemd{
				Service: &config.SystemdDirectiveSet{
					Items: []config.SystemdDirective{
						{Name: "SyslogIdentifier", Value: "timertab-%n"},
					},
				},
				Timer: &config.SystemdDirectiveSet{
					Map: map[string]string{"AccuracySec": "1m"},
				},
			},
		},
	}

	unitPaths := make([]string, 0, len(jobs)*2)
	dir := t.TempDir()
	for _, job := range jobs {
		units, err := RenderJobUnits(1000, config.DefaultInstanceID, job)
		if err != nil {
			t.Fatalf("RenderJobUnits(%s) error = %v", job.ID, err)
		}

		servicePath := filepath.Join(dir, units.ServiceName)
		timerPath := filepath.Join(dir, units.TimerName)
		if err := os.WriteFile(servicePath, []byte(units.ServiceContent), 0o644); err != nil {
			t.Fatalf("WriteFile(%s) error = %v", servicePath, err)
		}
		if err := os.WriteFile(timerPath, []byte(units.TimerContent), 0o644); err != nil {
			t.Fatalf("WriteFile(%s) error = %v", timerPath, err)
		}
		unitPaths = append(unitPaths, servicePath, timerPath)
	}

	cmd := exec.Command("systemd-analyze", append([]string{"verify"}, unitPaths...)...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("systemd-analyze verify failed: %v\n%s", err, strings.TrimSpace(string(output)))
	}
}
