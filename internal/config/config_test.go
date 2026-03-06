package config

import (
	"errors"
	"strings"
	"testing"
)

func TestNormalizeIDsGeneratesMissingID(t *testing.T) {
	cfg := File{
		Version: 1,
		Jobs: []Job{{
			Name: "NPM cache verify",
			When: ScheduleList{"@hourly"},
			Run:  "npm --global cache verify",
		}},
	}

	if err := cfg.NormalizeIDs(); err != nil {
		t.Fatalf("NormalizeIDs() error = %v", err)
	}

	if cfg.Jobs[0].ID == "" {
		t.Fatalf("expected generated ID")
	}
}

func TestLoadFromBytesSchemaValidSample(t *testing.T) {
	input := strings.Join([]string{
		`$schema: "https://raw.githubusercontent.com/ginden/timertab/v1.0.0/schema/v1.json"`,
		"version: 1",
		"jobs:",
		"  - id: nightly",
		"    name: Nightly backup",
		"    when: '@daily'",
		"    run: 'echo ok'",
		"    env:",
		"      PATH_SUFFIX: /usr/local/bin",
	}, "\n")

	cfg, err := LoadFromBytes([]byte(input))
	if err != nil {
		t.Fatalf("unexpected error for valid sample: %v", err)
	}
	if cfg == nil {
		t.Fatalf("expected parsed config")
	}
	if len(cfg.Jobs) != 1 {
		t.Fatalf("expected one job, got %d", len(cfg.Jobs))
	}
}

func TestLoadFromBytesSchemaInvalidSamples(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		expectPath  string
		expectInMsg string
	}{
		{
			name:        "missing jobs",
			input:       "version: 1\n",
			expectPath:  "$",
			expectInMsg: `missing property 'jobs'`,
		},
		{
			name: "invalid version const",
			input: strings.Join([]string{
				"version: 2",
				"jobs:",
				"  - when: '@daily'",
				"    run: 'echo ok'",
			}, "\n"),
			expectPath:  "$.version",
			expectInMsg: "value must be 1",
		},
		{
			name: "invalid env key",
			input: strings.Join([]string{
				"version: 1",
				"jobs:",
				"  - when: '@daily'",
				"    run: 'echo ok'",
				"    env:",
				"      bad-key: value",
			}, "\n"),
			expectPath:  "$.jobs[0].env",
			expectInMsg: `'bad-key' does not match pattern`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := LoadFromBytes([]byte(tc.input))
			schemaErr := requireSchemaValidationError(t, err)
			assertSchemaViolation(t, schemaErr, tc.expectPath, tc.expectInMsg)
		})
	}
}

func TestLoadFromBytesStillRunsSemanticValidation(t *testing.T) {
	input := strings.Join([]string{
		"version: 1",
		"jobs:",
		"  - id: same-id",
		"    when: '@daily'",
		"    run: 'echo first'",
		"  - id: same-id",
		"    when: '@weekly'",
		"    run: 'echo second'",
	}, "\n")

	_, err := LoadFromBytes([]byte(input))
	if err == nil {
		t.Fatalf("expected duplicate ID validation error")
	}

	var schemaErr *SchemaValidationError
	if errors.As(err, &schemaErr) {
		t.Fatalf("expected semantic validation error, got schema error: %v", schemaErr)
	}
	if !strings.Contains(err.Error(), `duplicate id "same-id"`) {
		t.Fatalf("expected duplicate id error, got: %v", err)
	}
}

func TestLoadFromBytesSchemaValidPersistentJob(t *testing.T) {
	input := strings.Join([]string{
		"version: 1",
		"jobs:",
		"  - id: wakeup-sync",
		"    when: '@daily'",
		"    run: 'echo sync'",
		"    persistent: true",
	}, "\n")

	loaded, err := LoadFromBytes([]byte(input))
	if err != nil {
		t.Fatalf("LoadFromBytes() error = %v", err)
	}
	if len(loaded.Jobs) != 1 {
		t.Fatalf("jobs count = %d, want 1", len(loaded.Jobs))
	}
	if loaded.Jobs[0].Persistent == nil || !*loaded.Jobs[0].Persistent {
		t.Fatalf("jobs[0].persistent = %#v, want true", loaded.Jobs[0].Persistent)
	}
}

func requireSchemaValidationError(t *testing.T, err error) *SchemaValidationError {
	t.Helper()
	if err == nil {
		t.Fatalf("expected schema validation error, got nil")
	}

	var schemaErr *SchemaValidationError
	if !errors.As(err, &schemaErr) {
		t.Fatalf("expected SchemaValidationError, got %T (%v)", err, err)
	}
	if len(schemaErr.Violations) == 0 {
		t.Fatalf("expected at least one schema violation")
	}
	return schemaErr
}

func assertSchemaViolation(t *testing.T, err *SchemaValidationError, wantPath, wantMsgPart string) {
	t.Helper()
	for _, violation := range err.Violations {
		if violation.Path != wantPath {
			continue
		}
		if strings.Contains(violation.Message, wantMsgPart) {
			return
		}
		t.Fatalf("violation for path %s has unexpected message %q, expected to contain %q", wantPath, violation.Message, wantMsgPart)
	}

	paths := make([]string, 0, len(err.Violations))
	for _, violation := range err.Violations {
		paths = append(paths, violation.Path)
	}
	t.Fatalf("expected violation at path %s; got paths %v with violations %+v", wantPath, paths, err.Violations)
}
