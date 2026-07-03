package config

import (
	"strings"
	"testing"
)

func TestScheduleValidationRejected(t *testing.T) {
	invalid := []struct {
		name    string
		pattern string
	}{
		{"@reboot with command", "@reboot command"},
		{"missing field", "0 12 * *"},
		{"too many fields", "0 12 * * * extra"},
		{"invalid month value", "0 0 * 13 *"},
		{"out of hour range", "0 24 * * *"},
		{"negative minute", "60 * * * *"},
		{"unsupported shorthand", "@every 5m"},
		{"double dash", "0 0-0- * * *"},
		{"whitespace in token", "0 12 * * mon tue"},
	}

	for _, p := range invalid {
		t.Run(p.name, func(t *testing.T) {
			err := validateSchedule(p.pattern)
			if err == nil {
				t.Fatalf("Expected error for %q but got none", p.pattern)
			}
			t.Logf("Correctly rejected: %q -> %v", p.pattern, err)
		})
	}
}

func TestValidateSystemdOverridesRejectsBadDirectives(t *testing.T) {
	base := func(systemd *Systemd) *File {
		return &File{
			Version: 1,
			Jobs: []Job{{
				ID:      "demo",
				When:    ScheduleList{"@daily"},
				Run:     ShellCommand("echo demo"),
				Systemd: systemd,
			}},
		}
	}

	cases := []struct {
		name    string
		systemd *Systemd
		wantErr string
	}{
		{
			name: "newline injection in map value",
			systemd: &Systemd{Service: &SystemdDirectiveSet{
				Map: map[string]string{"Nice": "10\nExecStartPre=/bin/rm -rf x"},
			}},
			wantErr: "must not contain newlines",
		},
		{
			name: "carriage return in list value",
			systemd: &Systemd{Timer: &SystemdDirectiveSet{
				Items: []SystemdDirective{{Name: "AccuracySec", Value: "1m\r"}},
			}},
			wantErr: "must not contain newlines",
		},
		{
			name: "directive name with space",
			systemd: &Systemd{Service: &SystemdDirectiveSet{
				Map: map[string]string{"Bad Name": "1"},
			}},
			wantErr: "directive name",
		},
		{
			name: "directive name with equals",
			systemd: &Systemd{Service: &SystemdDirectiveSet{
				Items: []SystemdDirective{{Name: "Nice=10", Value: "x"}},
			}},
			wantErr: "directive name",
		},
		{
			name: "directive name starting with bracket",
			systemd: &Systemd{Timer: &SystemdDirectiveSet{
				Map: map[string]string{"[Service]": "x"},
			}},
			wantErr: "directive name",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := base(tc.systemd).Validate()
			if err == nil {
				t.Fatalf("Validate() error = nil, want %q", tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("Validate() error = %q, want substring %q", err.Error(), tc.wantErr)
			}
		})
	}
}

func TestValidateSystemdOverridesAcceptsSpecifiersAndDuplicates(t *testing.T) {
	cfg := &File{
		Version: 1,
		Jobs: []Job{{
			ID:   "demo",
			When: ScheduleList{"@daily"},
			Run:  ShellCommand("echo demo"),
			Systemd: &Systemd{Service: &SystemdDirectiveSet{
				Items: []SystemdDirective{
					{Name: "ExecStartPre", Value: "/bin/echo %h"},
					{Name: "ExecStartPre", Value: "/bin/echo again"},
				},
			}},
		}},
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() error = %v, want nil", err)
	}
}

func TestLoadFromBytesRejectsInvalidDirectiveNameViaSchema(t *testing.T) {
	input := `version: 1
jobs:
  - id: demo
    when: "@daily"
    run: echo demo
    systemd:
      service:
        "Bad Name": "1"
`
	if _, err := LoadFromBytes([]byte(input)); err == nil {
		t.Fatalf("LoadFromBytes() error = nil, want schema violation")
	}
}

func TestScheduleValidationAccepted(t *testing.T) {
	valid := []struct {
		name    string
		pattern string
	}{
		{"@hourly", "@hourly"},
		{"@daily", "@daily"},
		{"@weekly", "@weekly"},
		{"@monthly", "@monthly"},
		{"@yearly", "@yearly"},
		{"@annually", "@annually"},
		{"@reboot", "@reboot"},
		{"simple cron", "0 12 * * *"},
		{"step values", "*/15 * * * *"},
		{"ranges", "1,3,5-7 * * * *"},
		{"weekday range", "0 9 * * 1-5"},
		{"month names", "0 0 1 jan *"},
		{"weekday names", "0 0 * * mon"},
	}

	for _, p := range valid {
		t.Run(p.name, func(t *testing.T) {
			err := validateSchedule(p.pattern)
			if err != nil {
				t.Fatalf("Expected no error for %q but got: %v", p.pattern, err)
			}
			t.Logf("Correctly accepted: %q", p.pattern)
		})
	}
}
