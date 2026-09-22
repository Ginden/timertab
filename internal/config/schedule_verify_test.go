package config

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestCompiledCalendarsWithSystemdAnalyze(t *testing.T) {
	if _, err := exec.LookPath("systemd-analyze"); err != nil {
		t.Skip("systemd-analyze not found")
	}
	for _, tc := range []struct {
		schedule string
		next     string
	}{
		{"0 0 */15 * *", "Fri 2026-01-16 00:00:00 UTC"},
		{"0 0 * * */2", "Sat 2026-01-03 00:00:00 UTC"},
		{"0 0 */15 * */2", "Sat 2026-01-31 00:00:00 UTC"},
		{"@weekly", "Sun 2026-01-04 00:00:00 UTC"},
		{"0 0 15 * 0-6", ""},
	} {
		t.Run(tc.schedule, func(t *testing.T) {
			directives, err := CompileTimerDirectivesInLocation(ScheduleList{tc.schedule}, "UTC")
			if err != nil {
				t.Fatal(err)
			}
			for _, directive := range directives {
				calendar := strings.TrimPrefix(directive, "OnCalendar=")
				cmd := exec.Command("systemd-analyze", "calendar", "--base-time=2026-01-01 00:00:00 UTC", calendar)
				cmd.Env = append(os.Environ(), "LC_ALL=C", "TZ=UTC")
				out, err := cmd.CombinedOutput()
				if err != nil {
					t.Fatalf("invalid calendar %q: %v\n%s", calendar, err, out)
				}
				if tc.next != "" && !strings.Contains(string(out), tc.next) {
					t.Fatalf("wrong next run; want %s:\n%s", tc.next, out)
				}
			}
		})
	}
}
