package systemd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ginden/timertab/internal/config"
)

func TestRenderJobUnitsGolden(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name              string
		uid               uint32
		job               config.Job
		wantBaseName      string
		wantServiceGolden string
		wantTimerGolden   string
	}{
		{
			name: "hookless job",
			uid:  1000,
			job: config.Job{
				ID:   "npm-cache-verify",
				When: config.ScheduleList{"@hourly"},
				Run:  "npm --global cache verify",
			},
			wantBaseName:      "timertab-u1000-npm-cache-verify-3e70124b9a",
			wantServiceGolden: "hookless.service.golden",
			wantTimerGolden:   "hookless.timer.golden",
		},
		{
			name: "hook-enabled job",
			uid:  1001,
			job: config.Job{
				ID:   "journal-scan",
				When: config.ScheduleList{"15 2 * * 1-5"},
				Run:  "echo run && date",
				Env: map[string]string{
					"LANG": "C.UTF-8",
					"TZ":   "UTC",
				},
				Cwd: "/var/lib/timertab jobs",
				OnSuccess: &config.Hook{
					Command: "echo success",
					Env: map[string]string{
						"OK": "yes",
					},
				},
				OnFailure: &config.Hook{
					Command: `journalctl -u "$TIMERTAB_UNIT" -n 100 --no-pager`,
					Env: map[string]string{
						"FAIL_REASON": "job failed",
					},
				},
			},
			wantBaseName:      "timertab-u1001-journal-scan-c03dc4fac9",
			wantServiceGolden: "hooks-enabled.service.golden",
			wantTimerGolden:   "hooks-enabled.timer.golden",
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := RenderJobUnits(tc.uid, tc.job)
			if err != nil {
				t.Fatalf("RenderJobUnits() error = %v", err)
			}

			if got.BaseName != tc.wantBaseName {
				t.Fatalf("BaseName = %q, want %q", got.BaseName, tc.wantBaseName)
			}

			if got.ServiceName != tc.wantBaseName+".service" {
				t.Fatalf("ServiceName = %q, want %q", got.ServiceName, tc.wantBaseName+".service")
			}

			if got.TimerName != tc.wantBaseName+".timer" {
				t.Fatalf("TimerName = %q, want %q", got.TimerName, tc.wantBaseName+".timer")
			}

			wantService := readRenderGolden(t, tc.wantServiceGolden)
			if strings.TrimRight(got.ServiceContent, "\n") != wantService {
				t.Fatalf("ServiceContent mismatch\nwant:\n%s\n\ngot:\n%s", wantService, strings.TrimRight(got.ServiceContent, "\n"))
			}

			wantTimer := readRenderGolden(t, tc.wantTimerGolden)
			if strings.TrimRight(got.TimerContent, "\n") != wantTimer {
				t.Fatalf("TimerContent mismatch\nwant:\n%s\n\ngot:\n%s", wantTimer, strings.TrimRight(got.TimerContent, "\n"))
			}
		})
	}
}

func TestRenderJobUnitsErrors(t *testing.T) {
	t.Parallel()

	_, err := RenderJobUnits(1000, config.Job{
		When: config.ScheduleList{"@hourly"},
		Run:  "echo hi",
	})
	if err == nil {
		t.Fatalf("RenderJobUnits() error = nil for empty id")
	}

	_, err = RenderJobUnits(1000, config.Job{
		ID:   "ok-id",
		When: config.ScheduleList{"@every-second"},
		Run:  "echo hi",
	})
	if err == nil {
		t.Fatalf("RenderJobUnits() error = nil for unsupported shorthand")
	}
}

func TestRenderJobUnitsUsesOnBootSecForRebootSchedule(t *testing.T) {
	t.Parallel()

	units, err := RenderJobUnits(1000, config.Job{
		ID:   "reboot-job",
		When: config.ScheduleList{"@reboot"},
		Run:  "echo hi",
	})
	if err != nil {
		t.Fatalf("RenderJobUnits() error = %v", err)
	}

	if !strings.Contains(units.TimerContent, "OnBootSec=0\n") {
		t.Fatalf("TimerContent missing OnBootSec=0:\n%s", units.TimerContent)
	}
	if strings.Contains(units.TimerContent, "OnCalendar=@reboot") {
		t.Fatalf("TimerContent should not use OnCalendar for @reboot:\n%s", units.TimerContent)
	}
}

func TestRenderJobUnitsIncludesPersistentForEnabledPersistentTimers(t *testing.T) {
	t.Parallel()

	persistent := true
	units, err := RenderJobUnits(1000, config.Job{
		ID:         "persistent-job",
		When:       config.ScheduleList{"@daily"},
		Run:        "echo hi",
		Persistent: &persistent,
	})
	if err != nil {
		t.Fatalf("RenderJobUnits() error = %v", err)
	}

	if !strings.Contains(units.TimerContent, "Persistent=true\n") {
		t.Fatalf("TimerContent missing Persistent=true:\n%s", units.TimerContent)
	}
}

func TestRenderJobUnitsOmitsPersistentByDefault(t *testing.T) {
	t.Parallel()

	units, err := RenderJobUnits(1000, config.Job{
		ID:   "default-persistent",
		When: config.ScheduleList{"@daily"},
		Run:  "echo hi",
	})
	if err != nil {
		t.Fatalf("RenderJobUnits() error = %v", err)
	}

	if strings.Contains(units.TimerContent, "Persistent=true") {
		t.Fatalf("TimerContent should not include Persistent=true by default:\n%s", units.TimerContent)
	}
}

func TestRenderJobUnitsIncludesRandomizedDelaySecWhenJitterIsSet(t *testing.T) {
	t.Parallel()

	units, err := RenderJobUnits(1000, config.Job{
		ID:     "jitter-job",
		When:   config.ScheduleList{"@daily"},
		Run:    "echo hi",
		Jitter: "5m",
	})
	if err != nil {
		t.Fatalf("RenderJobUnits() error = %v", err)
	}

	if !strings.Contains(units.TimerContent, "RandomizedDelaySec=5m\n") {
		t.Fatalf("TimerContent missing RandomizedDelaySec=5m:\n%s", units.TimerContent)
	}
}

func TestRenderJobUnitsIncludesServiceLimits(t *testing.T) {
	t.Parallel()

	ioWeight := 500
	units, err := RenderJobUnits(1000, config.Job{
		ID:   "limited-job",
		When: config.ScheduleList{"@daily"},
		Run:  "echo hi",
		Limits: &config.Limits{
			MemoryMax: "512M",
			CPUQuota:  "60%",
			IOWeight:  &ioWeight,
		},
	})
	if err != nil {
		t.Fatalf("RenderJobUnits() error = %v", err)
	}

	if !strings.Contains(units.ServiceContent, "MemoryMax=512M\n") {
		t.Fatalf("ServiceContent missing MemoryMax=512M:\n%s", units.ServiceContent)
	}
	if !strings.Contains(units.ServiceContent, "CPUQuota=60%\n") {
		t.Fatalf("ServiceContent missing CPUQuota=60%%:\n%s", units.ServiceContent)
	}
	if !strings.Contains(units.ServiceContent, "IOWeight=500\n") {
		t.Fatalf("ServiceContent missing IOWeight=500:\n%s", units.ServiceContent)
	}
}

func TestIsManagedUnitContentForUID(t *testing.T) {
	t.Parallel()

	content := strings.Join([]string{
		"# timertab-managed: true",
		"# timertab-uid: 1000",
		"# timertab-job-id: sample",
		"[Unit]",
		"Description=sample",
	}, "\n")

	if !IsManagedUnitContentForUID(content, 1000) {
		t.Fatalf("IsManagedUnitContentForUID() = false, want true")
	}
	if IsManagedUnitContentForUID(content, 1001) {
		t.Fatalf("IsManagedUnitContentForUID() = true for wrong uid")
	}
	if IsManagedUnitContentForUID(strings.Replace(content, "# timertab-job-id: sample", "# timertab-job-id: ", 1), 1000) {
		t.Fatalf("IsManagedUnitContentForUID() = true for empty job id marker")
	}
}

func TestRenderJobUnitsSupportsMultilineCommandWithoutBreakingDirectiveLine(t *testing.T) {
	t.Parallel()

	units, err := RenderJobUnits(1000, config.Job{
		ID:   "trim-test",
		When: config.ScheduleList{"@hourly"},
		Run:  "echo hello\n",
		OnSuccess: &config.Hook{
			Command: "echo ok\n",
		},
	})
	if err != nil {
		t.Fatalf("RenderJobUnits() error = %v", err)
	}

	if strings.Contains(units.ServiceContent, "ExecStart=/bin/sh -lc \"echo hello\n\"") {
		t.Fatalf("service content contains raw newline in ExecStart directive: %q", units.ServiceContent)
	}
	if !strings.Contains(units.ServiceContent, "ExecStart=/bin/sh -lc \"echo hello\\n\"") {
		t.Fatalf("service content missing escaped multiline ExecStart command: %q", units.ServiceContent)
	}
	if !strings.Contains(units.ServiceContent, "echo ok\\n") {
		t.Fatalf("service content missing escaped multiline hook command: %q", units.ServiceContent)
	}
}

func TestSystemdQuotedEscapesControlCharacters(t *testing.T) {
	t.Parallel()

	got := systemdQuoted("line1\nline2\ttab")
	want := "\"line1\\nline2\\ttab\""
	if got != want {
		t.Fatalf("systemdQuoted() = %q, want %q", got, want)
	}
}

func readRenderGolden(t *testing.T, name string) string {
	t.Helper()

	path := filepath.Join("testdata", "render", name)
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden %q: %v", path, err)
	}

	return strings.TrimRight(string(b), "\n")
}
