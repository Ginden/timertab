package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ginden/timertab/internal/config"
	"github.com/ginden/timertab/internal/systemd"
	"github.com/spf13/cobra"
)

const wantDefaultTemplateRun = "echo 'timertab executes commands via /bin/sh -lc'\necho 'direct executable mode is planned for v2'"

func TestAddCommandNoApplyAppendsJob(t *testing.T) {
	originalCheck := ensureSystemdBaseline
	originalApply := runSystemctlApply
	t.Cleanup(func() {
		ensureSystemdBaseline = originalCheck
		runSystemctlApply = originalApply
	})

	ensureSystemdBaseline = func() error {
		return errors.New("systemd check should not run for --no-apply")
	}
	runSystemctlApply = func(context.Context, *config.File, string) (applyReport, error) {
		return applyReport{}, errors.New("apply should not run for --no-apply")
	}

	cfgPath := filepath.Join(t.TempDir(), "timertab.yaml")
	cmd := NewRootCommand()
	stdout := &bytes.Buffer{}
	cmd.SetOut(stdout)
	cmd.SetErr(&bytes.Buffer{})
	t.Setenv("EDITOR", "true")
	cmd.SetArgs([]string{
		"add",
		"--config", cfgPath,
		"--no-apply",
	})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	loaded, err := config.LoadFromFile(cfgPath)
	if err != nil {
		t.Fatalf("LoadFromFile(%q) error = %v", cfgPath, err)
	}
	if len(loaded.Jobs) != 1 {
		t.Fatalf("len(jobs) = %d, want 1", len(loaded.Jobs))
	}
	if loaded.Jobs[0].Name != "example" {
		t.Fatalf("job name = %q, want %q", loaded.Jobs[0].Name, "example")
	}
	if loaded.Jobs[0].Run != wantDefaultTemplateRun {
		t.Fatalf("job run = %q, want %q", loaded.Jobs[0].Run, wantDefaultTemplateRun)
	}
	if len(loaded.Jobs[0].When) != 1 || loaded.Jobs[0].When[0] != "@daily" {
		t.Fatalf("job when = %#v, want [\"@daily\"]", loaded.Jobs[0].When)
	}
	if strings.TrimSpace(loaded.Jobs[0].ID) == "" {
		t.Fatalf("job id was not normalized")
	}
	if !strings.Contains(stdout.String(), "(no apply)") {
		t.Fatalf("stdout missing no-apply confirmation, got:\n%s", stdout.String())
	}
}

func TestAddCommandAppliesByDefault(t *testing.T) {
	originalCheck := ensureSystemdBaseline
	originalApply := runSystemctlApply
	t.Cleanup(func() {
		ensureSystemdBaseline = originalCheck
		runSystemctlApply = originalApply
	})

	var checkCalls int
	ensureSystemdBaseline = func() error {
		checkCalls++
		return nil
	}

	var applyCalls int
	runSystemctlApply = func(_ context.Context, loaded *config.File, _ string) (applyReport, error) {
		applyCalls++
		if loaded == nil {
			t.Fatalf("loaded config = nil")
		}
		return applyReport{
			Created: []string{"/tmp/test.timer"},
		}, nil
	}

	cfgPath := filepath.Join(t.TempDir(), "timertab.yaml")
	cmd := NewRootCommand()
	stdout := &bytes.Buffer{}
	cmd.SetOut(stdout)
	cmd.SetErr(&bytes.Buffer{})
	t.Setenv("EDITOR", "true")
	cmd.SetArgs([]string{
		"+1",
		"--config", cfgPath,
	})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if checkCalls != 1 {
		t.Fatalf("systemd check calls = %d, want 1", checkCalls)
	}
	if applyCalls != 1 {
		t.Fatalf("apply calls = %d, want 1", applyCalls)
	}
	if !strings.Contains(stdout.String(), "created /tmp/test.timer\n") {
		t.Fatalf("stdout missing apply report, got:\n%s", stdout.String())
	}
}

func TestEjectCommandRemovesJobAndManagedMarkers(t *testing.T) {
	originalResolveTargetUID := resolveTargetUID
	originalResolveSystemdUserUnitDir := resolveSystemdUserUnitDir
	t.Cleanup(func() {
		resolveTargetUID = originalResolveTargetUID
		resolveSystemdUserUnitDir = originalResolveSystemdUserUnitDir
	})

	targetUID := uint32(1000)
	unitDir := t.TempDir()
	resolveTargetUID = func(string) (uint32, error) { return targetUID, nil }
	resolveSystemdUserUnitDir = func(string) (string, error) { return unitDir, nil }

	cfgPath := filepath.Join(t.TempDir(), "timertab.yaml")
	cfg := &config.File{
		Version: 1,
		Jobs: []config.Job{
			{
				ID:   "demo",
				When: config.ScheduleList{"@daily"},
				Run:  "echo demo",
			},
		},
	}
	if err := saveConfig(cfgPath, cfg); err != nil {
		t.Fatalf("saveConfig() error = %v", err)
	}

	rendered, err := systemd.RenderJobUnits(targetUID, cfg.Jobs[0])
	if err != nil {
		t.Fatalf("RenderJobUnits() error = %v", err)
	}
	servicePath := filepath.Join(unitDir, rendered.ServiceName)
	timerPath := filepath.Join(unitDir, rendered.TimerName)
	if err := os.WriteFile(servicePath, []byte(rendered.ServiceContent), 0o644); err != nil {
		t.Fatalf("WriteFile(service) error = %v", err)
	}
	if err := os.WriteFile(timerPath, []byte(rendered.TimerContent), 0o644); err != nil {
		t.Fatalf("WriteFile(timer) error = %v", err)
	}

	cmd := NewRootCommand()
	stdout := &bytes.Buffer{}
	cmd.SetOut(stdout)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{
		"eject", "demo",
		"--config", cfgPath,
	})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	loaded, err := config.LoadFromFile(cfgPath)
	if err != nil {
		t.Fatalf("LoadFromFile(%q) error = %v", cfgPath, err)
	}
	if len(loaded.Jobs) != 0 {
		t.Fatalf("len(jobs) = %d, want 0", len(loaded.Jobs))
	}

	serviceAfter, err := os.ReadFile(servicePath)
	if err != nil {
		t.Fatalf("ReadFile(service) error = %v", err)
	}
	timerAfter, err := os.ReadFile(timerPath)
	if err != nil {
		t.Fatalf("ReadFile(timer) error = %v", err)
	}
	if strings.Contains(string(serviceAfter), "timertab-managed: true") {
		t.Fatalf("service still contains managed marker:\n%s", serviceAfter)
	}
	if strings.Contains(string(timerAfter), "timertab-managed: true") {
		t.Fatalf("timer still contains managed marker:\n%s", timerAfter)
	}
	if !strings.Contains(stdout.String(), "ejected "+servicePath+"\n") {
		t.Fatalf("stdout missing service ejected line, got:\n%s", stdout.String())
	}
	if !strings.Contains(stdout.String(), "ejected "+timerPath+"\n") {
		t.Fatalf("stdout missing timer ejected line, got:\n%s", stdout.String())
	}
}

func TestEjectCommandReturnsNotFound(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "timertab.yaml")
	cfg := &config.File{
		Version: 1,
		Jobs: []config.Job{
			{
				ID:   "existing",
				When: config.ScheduleList{"@daily"},
				Run:  "echo existing",
			},
		},
	}
	if err := saveConfig(cfgPath, cfg); err != nil {
		t.Fatalf("saveConfig() error = %v", err)
	}

	cmd := NewRootCommand()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{
		"eject", "missing",
		"--config", cfgPath,
	})

	err := cmd.Execute()
	if err == nil {
		t.Fatalf("Execute() error = nil, want non-nil")
	}
	if !strings.Contains(err.Error(), `job "missing" not found`) {
		t.Fatalf("error = %v, want not-found message", err)
	}
}

func TestEjectCommandCompletesKnownJobIDs(t *testing.T) {
	originalValidateTargetUserPermission := validateTargetUserPermission
	originalResolveConfigPath := resolveConfigPath
	t.Cleanup(func() {
		validateTargetUserPermission = originalValidateTargetUserPermission
		resolveConfigPath = originalResolveConfigPath
	})

	validateTargetUserPermission = func(string) error { return nil }

	cfgPath := filepath.Join(t.TempDir(), "timertab.yaml")
	if err := saveConfig(cfgPath, &config.File{
		Version: 1,
		Jobs: []config.Job{
			{ID: "alpha", When: config.ScheduleList{"@daily"}, Run: "echo alpha"},
			{ID: "beta", When: config.ScheduleList{"@hourly"}, Run: "echo beta"},
		},
	}); err != nil {
		t.Fatalf("saveConfig() error = %v", err)
	}

	resolveConfigPath = func(_, override string) (string, error) {
		if override != cfgPath {
			t.Fatalf("override = %q, want %q", override, cfgPath)
		}
		return cfgPath, nil
	}

	root := NewRootCommand()
	ejectCmd, _, err := root.Find([]string{"eject"})
	if err != nil {
		t.Fatalf("Find(eject) error = %v", err)
	}
	if err := ejectCmd.Flags().Set("config", cfgPath); err != nil {
		t.Fatalf("Flags().Set(config) error = %v", err)
	}

	completions, directive := ejectCmd.ValidArgsFunction(ejectCmd, []string{}, "b")
	if directive != cobra.ShellCompDirectiveNoFileComp {
		t.Fatalf("directive = %v, want NoFileComp", directive)
	}
	if len(completions) != 1 || completions[0] != "beta" {
		t.Fatalf("completions = %v, want [beta]", completions)
	}
}

func TestParseEditedJobAcceptsSingleJobConfigTemplate(t *testing.T) {
	job, err := parseEditedJob([]byte(defaultAddJobTemplate))
	if err != nil {
		t.Fatalf("parseEditedJob(defaultAddJobTemplate) error = %v", err)
	}
	if job.Name != "example" {
		t.Fatalf("job name = %q, want %q", job.Name, "example")
	}
	if len(job.When) != 1 || job.When[0] != "@daily" {
		t.Fatalf("job when = %#v, want [\"@daily\"]", job.When)
	}
	if job.Run != wantDefaultTemplateRun {
		t.Fatalf("job run = %q, want %q", job.Run, wantDefaultTemplateRun)
	}
	if strings.TrimSpace(job.ID) == "" {
		t.Fatalf("job id was not normalized")
	}
}

func TestParseEditedJobRejectsMultipleJobs(t *testing.T) {
	buf := []byte(fmt.Sprintf(`$schema: %q
version: 1
jobs:
  - name: one
    when: "@daily"
    run: "echo one"
  - name: two
    when: "@hourly"
    run: "echo two"
`, defaultSchemaURL))
	_, err := parseEditedJob(buf)
	if err == nil {
		t.Fatalf("parseEditedJob() error = nil, want non-nil")
	}
	if !strings.Contains(err.Error(), "add expects exactly one job in jobs, got 2") {
		t.Fatalf("parseEditedJob() error = %v, want single-job validation", err)
	}
}
