package config

import (
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
