package cli

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ginden/timertab/internal/config"
	"github.com/ginden/timertab/internal/systemd"
)

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
	if loaded.Jobs[0].Run != "echo hello from timertab" {
		t.Fatalf("job run = %q, want %q", loaded.Jobs[0].Run, "echo hello from timertab")
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
