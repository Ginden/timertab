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
			Run:  ShellCommand("npm --global cache verify"),
		}},
	}

	if err := cfg.NormalizeIDs(); err != nil {
		t.Fatalf("NormalizeIDs() error = %v", err)
	}

	if cfg.Jobs[0].ID == "" {
		t.Fatalf("expected generated ID")
	}
}

func TestNormalizeIDsTreatsShellShorthandAndExplicitShellArgvAsSameJobDigest(t *testing.T) {
	shorthand := File{
		Version: 1,
		Jobs: []Job{{
			When: ScheduleList{"@hourly"},
			Run:  ShellCommand("echo ok"),
		}},
	}
	explicit := File{
		Version: 1,
		Jobs: []Job{{
			When: ScheduleList{"@hourly"},
			Run:  ExecCommand("/bin/sh", "-lc", "echo ok"),
		}},
	}

	if err := shorthand.NormalizeIDs(); err != nil {
		t.Fatalf("shorthand NormalizeIDs() error = %v", err)
	}
	if err := explicit.NormalizeIDs(); err != nil {
		t.Fatalf("explicit NormalizeIDs() error = %v", err)
	}
	if shorthand.Jobs[0].ID != explicit.Jobs[0].ID {
		t.Fatalf("generated ids differ for equivalent shell commands: %q vs %q", shorthand.Jobs[0].ID, explicit.Jobs[0].ID)
	}
}

func TestLoadFromBytesSchemaValidSample(t *testing.T) {
	input := strings.Join([]string{
		`$schema: "https://raw.githubusercontent.com/ginden/timertab/v1.0.0/schema/v1.json"`,
		"version: 1",
		"instance_id: work",
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
	if cfg.InstanceID != "work" {
		t.Fatalf("instance_id = %q, want %q", cfg.InstanceID, "work")
	}
}

func TestLoadFromBytesSchemaValidRunArgv(t *testing.T) {
	input := strings.Join([]string{
		"version: 1",
		"jobs:",
		"  - id: exec-job",
		"    when: '@daily'",
		"    run:",
		`      - /usr/bin/env`,
		`      - bash`,
		`      - -lc`,
		`      - echo ok`,
	}, "\n")

	cfg, err := LoadFromBytes([]byte(input))
	if err != nil {
		t.Fatalf("unexpected error for argv run: %v", err)
	}

	if got := cfg.Jobs[0].Run.Argv(); len(got) != 4 || got[0] != "/usr/bin/env" || got[3] != "echo ok" {
		t.Fatalf("jobs[0].run argv = %#v, want explicit argv", got)
	}
	if _, ok := cfg.Jobs[0].Run.Shell(); ok {
		t.Fatalf("jobs[0].run unexpectedly reported shell shorthand")
	}
}

func TestLoadFromBytesUsesEmbeddedSchemaOutsideRepoRoot(t *testing.T) {
	t.Chdir(t.TempDir())

	input := strings.Join([]string{
		"version: 1",
		"jobs:",
		"  - when: '@daily'",
		"    run: 'echo hi'",
	}, "\n")

	cfg, err := LoadFromBytes([]byte(input))
	if err != nil {
		t.Fatalf("LoadFromBytes() error = %v, want nil", err)
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
			name: "invalid instance id",
			input: strings.Join([]string{
				"version: 1",
				"instance_id: bad value",
				"jobs:",
				"  - when: '@daily'",
				"    run: 'echo ok'",
			}, "\n"),
			expectPath:  "$.instance_id",
			expectInMsg: "does not match pattern",
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

func TestLoadFromBytesSchemaValidJitter(t *testing.T) {
	input := strings.Join([]string{
		"version: 1",
		"jobs:",
		"  - id: jittered",
		"    when: '@hourly'",
		"    run: 'echo run'",
		"    jitter: '5m'",
	}, "\n")

	loaded, err := LoadFromBytes([]byte(input))
	if err != nil {
		t.Fatalf("LoadFromBytes() error = %v", err)
	}
	if len(loaded.Jobs) != 1 {
		t.Fatalf("jobs count = %d, want 1", len(loaded.Jobs))
	}
	if got := loaded.Jobs[0].Jitter; got != "5m" {
		t.Fatalf("jobs[0].jitter = %q, want %q", got, "5m")
	}
}

func TestLoadFromBytesRejectsInvalidJitter(t *testing.T) {
	input := strings.Join([]string{
		"version: 1",
		"jobs:",
		"  - id: jittered",
		"    when: '@hourly'",
		"    run: 'echo run'",
		"    jitter: 'bad-duration'",
	}, "\n")

	_, err := LoadFromBytes([]byte(input))
	if err == nil {
		t.Fatalf("LoadFromBytes() error = nil, want non-nil")
	}
	if !strings.Contains(err.Error(), "jitter") || !strings.Contains(err.Error(), "valid duration") {
		t.Fatalf("error = %q, want actionable jitter validation", err.Error())
	}
}

func TestLoadFromBytesSchemaValidLimits(t *testing.T) {
	input := strings.Join([]string{
		"version: 1",
		"jobs:",
		"  - id: bounded",
		"    when: '@daily'",
		"    run: 'echo run'",
		"    limits:",
		"      MemoryMax: '1G'",
		"      CPUQuota: '75%'",
		"      IOWeight: 600",
	}, "\n")

	loaded, err := LoadFromBytes([]byte(input))
	if err != nil {
		t.Fatalf("LoadFromBytes() error = %v", err)
	}
	if len(loaded.Jobs) != 1 || loaded.Jobs[0].Limits == nil {
		t.Fatalf("expected one job with limits, got %#v", loaded.Jobs)
	}
	if loaded.Jobs[0].Limits.MemoryMax != "1G" {
		t.Fatalf("MemoryMax = %q, want %q", loaded.Jobs[0].Limits.MemoryMax, "1G")
	}
	if loaded.Jobs[0].Limits.CPUQuota != "75%" {
		t.Fatalf("CPUQuota = %q, want %q", loaded.Jobs[0].Limits.CPUQuota, "75%")
	}
	if loaded.Jobs[0].Limits.IOWeight == nil || *loaded.Jobs[0].Limits.IOWeight != 600 {
		t.Fatalf("IOWeight = %#v, want 600", loaded.Jobs[0].Limits.IOWeight)
	}
}

func TestLoadFromBytesRejectsInvalidLimits(t *testing.T) {
	tests := []struct {
		name    string
		yaml    string
		wantErr string
	}{
		{
			name: "invalid MemoryMax",
			yaml: strings.Join([]string{
				"version: 1",
				"jobs:",
				"  - id: x",
				"    when: '@daily'",
				"    run: 'echo run'",
				"    limits:",
				"      MemoryMax: 'xx'",
			}, "\n"),
			wantErr: "MemoryMax",
		},
		{
			name: "invalid CPUQuota",
			yaml: strings.Join([]string{
				"version: 1",
				"jobs:",
				"  - id: x",
				"    when: '@daily'",
				"    run: 'echo run'",
				"    limits:",
				"      CPUQuota: 'foo'",
			}, "\n"),
			wantErr: "CPUQuota",
		},
		{
			name: "invalid IOWeight",
			yaml: strings.Join([]string{
				"version: 1",
				"jobs:",
				"  - id: x",
				"    when: '@daily'",
				"    run: 'echo run'",
				"    limits:",
				"      IOWeight: 0",
			}, "\n"),
			wantErr: "IOWeight",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := LoadFromBytes([]byte(tt.yaml))
			if err == nil {
				t.Fatalf("LoadFromBytes() error = nil, want non-nil")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("error = %q, want to contain %q", err.Error(), tt.wantErr)
			}
		})
	}
}

func TestLoadFromBytesGitAutoCommitToggle(t *testing.T) {
	input := strings.Join([]string{
		"version: 1",
		"git:",
		"  auto_commit: false",
		"jobs:",
		"  - id: a",
		"    when: '@daily'",
		"    run: 'echo a'",
	}, "\n")

	loaded, err := LoadFromBytes([]byte(input))
	if err != nil {
		t.Fatalf("LoadFromBytes() error = %v", err)
	}
	if loaded.AutoCommitEnabled() {
		t.Fatalf("AutoCommitEnabled() = true, want false")
	}
}

func TestLoadFromBytesSchemaValidSystemdOverridesAsMap(t *testing.T) {
	input := strings.Join([]string{
		"version: 1",
		"jobs:",
		"  - id: map-systemd",
		"    when: '@daily'",
		"    run: 'echo run'",
		"    systemd:",
		"      service:",
		"        Restart: on-failure",
		"        RestartSec: 30s",
		"      timer:",
		"        AccuracySec: 1m",
	}, "\n")

	loaded, err := LoadFromBytes([]byte(input))
	if err != nil {
		t.Fatalf("LoadFromBytes() error = %v", err)
	}

	systemd := loaded.Jobs[0].Systemd
	if systemd == nil {
		t.Fatalf("jobs[0].systemd = nil")
	}
	if systemd.Service == nil {
		t.Fatalf("jobs[0].systemd.service = nil")
	}
	if got := systemd.Service.Map["Restart"]; got != "on-failure" {
		t.Fatalf("jobs[0].systemd.service.Restart = %q, want %q", got, "on-failure")
	}
	if got := systemd.Service.Map["RestartSec"]; got != "30s" {
		t.Fatalf("jobs[0].systemd.service.RestartSec = %q, want %q", got, "30s")
	}
	if systemd.Timer == nil {
		t.Fatalf("jobs[0].systemd.timer = nil")
	}
	if got := systemd.Timer.Map["AccuracySec"]; got != "1m" {
		t.Fatalf("jobs[0].systemd.timer.AccuracySec = %q, want %q", got, "1m")
	}
}

func TestLoadFromBytesSchemaValidSystemdOverridesAsList(t *testing.T) {
	input := strings.Join([]string{
		"version: 1",
		"jobs:",
		"  - id: list-systemd",
		"    when: '@daily'",
		"    run: 'echo run'",
		"    systemd:",
		"      service:",
		"        - name: Restart",
		"          value: on-failure",
		"        - name: RestartSec",
		"          value: 30s",
		"      timer:",
		"        - name: AccuracySec",
		"          value: 1m",
	}, "\n")

	loaded, err := LoadFromBytes([]byte(input))
	if err != nil {
		t.Fatalf("LoadFromBytes() error = %v", err)
	}

	systemd := loaded.Jobs[0].Systemd
	if systemd == nil {
		t.Fatalf("jobs[0].systemd = nil")
	}
	if systemd.Service == nil {
		t.Fatalf("jobs[0].systemd.service = nil")
	}
	if len(systemd.Service.Items) != 2 {
		t.Fatalf("len(jobs[0].systemd.service) = %d, want 2", len(systemd.Service.Items))
	}
	if systemd.Service.Items[0].Name != "Restart" || systemd.Service.Items[0].Value != "on-failure" {
		t.Fatalf("jobs[0].systemd.service[0] = %#v, want Restart=on-failure", systemd.Service.Items[0])
	}
	if systemd.Service.Items[1].Name != "RestartSec" || systemd.Service.Items[1].Value != "30s" {
		t.Fatalf("jobs[0].systemd.service[1] = %#v, want RestartSec=30s", systemd.Service.Items[1])
	}
	if systemd.Timer == nil || len(systemd.Timer.Items) != 1 {
		t.Fatalf("jobs[0].systemd.timer = %#v, want one item", systemd.Timer)
	}
	if systemd.Timer.Items[0].Name != "AccuracySec" || systemd.Timer.Items[0].Value != "1m" {
		t.Fatalf("jobs[0].systemd.timer[0] = %#v, want AccuracySec=1m", systemd.Timer.Items[0])
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
