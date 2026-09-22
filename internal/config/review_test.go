package config

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func TestLoadRejectsTrailingYAMLDocuments(t *testing.T) {
	for _, tail := range []string{"---\nversion: 1\njobs: []\n", "---\n", "---\njobs: ["} {
		if _, err := LoadFromBytes([]byte("version: 1\njobs: []\n" + tail)); err == nil {
			t.Fatalf("accepted trailing document %q", tail)
		}
	}
	if _, err := LoadFromBytes([]byte("version: 1\njobs: []\n...\n# trailing comment\n")); err != nil {
		t.Fatalf("valid document end rejected: %v", err)
	}
}

func TestCronWildcardDaysPreserveSteps(t *testing.T) {
	cases := []struct {
		cron string
		want []string
	}{
		{"0 0 */15 * *", []string{"OnCalendar=*-*-01,16,31 00:00:00"}},
		{"0 0 * * */2", []string{"OnCalendar=Sun,Tue,Thu,Sat *-*-* 00:00:00"}},
		{"0 0 */15 * */2", []string{"OnCalendar=Sun,Tue,Thu,Sat *-*-01,16,31 00:00:00"}},
		{"0 0 15 * 0-6", []string{"OnCalendar=*-*-15 00:00:00", "OnCalendar=*-*-* 00:00:00"}},
	}
	for _, tc := range cases {
		t.Run(tc.cron, func(t *testing.T) {
			got, err := CompileTimerDirectives(ScheduleList{tc.cron})
			if err != nil || !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got %v, %v; want %v", got, err, tc.want)
			}
		})
	}
}

func TestCronLargeStepDoesNotOverflow(t *testing.T) {
	cron := fmt.Sprintf("1/%d * * * *", int(^uint(0)>>1))
	got, err := CompileTimerDirectives(ScheduleList{cron})
	want := []string{"OnCalendar=*-*-* *:01:00"}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, %v; want %v", got, err, want)
	}
}

func TestConfigRejectsNULInUnitValues(t *testing.T) {
	for _, field := range []string{
		`run: "echo\0bad"`,
		`run: ["/bin/echo", "\0bad"]`,
		"run: echo ok\n    env: {VALUE: \"bad\\0value\"}",
		"run: echo ok\n    cwd: \"/tmp/\\0bad\"",
		"run: echo ok\n    on_success: {command: \"echo\\0bad\"}",
		"run: echo ok\n    on_failure: {command: echo ok, env: {VALUE: \"bad\\0value\"}}",
		"run: echo ok\n    systemd: {service: {SyslogIdentifier: \"bad\\0value\"}}",
	} {
		raw := "version: 1\njobs:\n  - when: '@daily'\n    " + field + "\n"
		if _, err := LoadFromBytes([]byte(raw)); err == nil {
			t.Fatalf("accepted NUL: %s", raw)
		}
	}
	cfg := &File{Version: 1, Jobs: []Job{{When: ScheduleList{"@daily"}, Run: ExecCommand("/bin/echo", "\x00")}}}
	if err := cfg.NormalizeIDs(); err == nil {
		t.Fatal("programmatic argv bypassed NUL validation")
	}
	cfg.Jobs[0].Run = ShellCommand("echo ok")
	cfg.Jobs[0].Name = strings.Repeat("x", 121)
	if err := cfg.NormalizeIDs(); err == nil {
		t.Fatal("programmatic name bypassed schema length limit")
	}
}
