package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCompileTimerDirectivesGolden(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		when   ScheduleList
		golden string
	}{
		{
			name: "all_shorthands",
			when: ScheduleList{
				"@hourly",
				"@daily",
				"@weekly",
				"@monthly",
				"@yearly",
				"@annually",
				"@reboot",
			},
			golden: "all-shorthands.golden",
		},
		{
			name: "representative_cron_patterns",
			when: ScheduleList{
				"15 2 * * *",
				"*/5 9-17 * * 1-5",
				"0 0 1 */2 *",
				"30 6 * jan,apr mon-fri",
			},
			golden: "cron-representative.golden",
		},
		{
			name: "day_of_month_and_weekday_or_semantics",
			when: ScheduleList{
				"0 12 1 * 1",
			},
			golden: "cron-dom-dow-or.golden",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := CompileTimerDirectives(tt.when)
			if err != nil {
				t.Fatalf("CompileTimerDirectives() error = %v", err)
			}

			want := readScheduleGolden(t, tt.golden)
			gotJoined := strings.Join(got, "\n")
			if gotJoined != want {
				t.Fatalf("CompileTimerDirectives() mismatch\nwant:\n%s\n\ngot:\n%s", want, gotJoined)
			}
		})
	}
}

func TestCompileTimerDirectivesCronStarStepDayFieldsUseVixieSemantics(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		when ScheduleList
		want []string
	}{
		{
			name: "day of month star step combines with weekday",
			when: ScheduleList{"0 0 */2 * Mon"},
			want: []string{"OnCalendar=Mon *-*-01,03,05,07,09,11,13,15,17,19,21,23,25,27,29,31 00:00:00"},
		},
		{
			name: "weekday star step combines with day of month",
			when: ScheduleList{"0 0 15 * */2"},
			want: []string{"OnCalendar=Sun,Tue,Thu,Sat *-*-15 00:00:00"},
		},
		{
			name: "both explicitly restricted still use cron or semantics",
			when: ScheduleList{"0 0 15 * Mon"},
			want: []string{
				"OnCalendar=*-*-15 00:00:00",
				"OnCalendar=Mon *-*-* 00:00:00",
			},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := CompileTimerDirectives(tt.when)
			if err != nil {
				t.Fatalf("CompileTimerDirectives() error = %v", err)
			}
			if strings.Join(got, "\n") != strings.Join(tt.want, "\n") {
				t.Fatalf("CompileTimerDirectives() mismatch\nwant:\n%s\n\ngot:\n%s", strings.Join(tt.want, "\n"), strings.Join(got, "\n"))
			}
		})
	}
}

func TestCompileTimerDirectivesErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		when    ScheduleList
		wantErr string
	}{
		{
			name:    "unsupported shorthand",
			when:    ScheduleList{"@foobar"},
			wantErr: `unsupported shorthand "@foobar"`,
		},
		{
			name:    "invalid field count",
			when:    ScheduleList{"0 0 * *"},
			wantErr: "cron expression must have exactly 5 fields",
		},
		{
			name:    "invalid minute step",
			when:    ScheduleList{"*/0 * * * *"},
			wantErr: "invalid cron minute field",
		},
		{
			name:    "invalid month token",
			when:    ScheduleList{"0 0 * Foo *"},
			wantErr: "invalid cron month field",
		},
		{
			name:    "invalid weekday token",
			when:    ScheduleList{"0 0 * * 9"},
			wantErr: "invalid cron weekday field",
		},
		{
			name:    "no partial output on failure",
			when:    ScheduleList{"@daily", "@foobar"},
			wantErr: `unsupported shorthand "@foobar"`,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := CompileTimerDirectives(tt.when)
			if err == nil {
				t.Fatalf("CompileTimerDirectives() error = nil, want %q", tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("CompileTimerDirectives() error = %q, want substring %q", err.Error(), tt.wantErr)
			}
			if got != nil {
				t.Fatalf("CompileTimerDirectives() output = %v, want nil on error", got)
			}
		})
	}
}

func readScheduleGolden(t *testing.T, name string) string {
	t.Helper()

	path := filepath.Join("testdata", "schedule", name)
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden %q: %v", path, err)
	}

	return strings.TrimRight(string(b), "\n")
}
