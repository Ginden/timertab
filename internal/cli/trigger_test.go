package cli

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ginden/timertab/internal/config"
	"github.com/ginden/timertab/internal/systemctl"
	"github.com/spf13/cobra"
)

type stubTriggerExecutor struct {
	startService func(ctx context.Context, serviceUnit string) error
}

func (s stubTriggerExecutor) DaemonReload(context.Context) error {
	return errors.New("DaemonReload should not be called")
}

func (s stubTriggerExecutor) EnableTimer(context.Context, string) error {
	return errors.New("EnableTimer should not be called")
}

func (s stubTriggerExecutor) StartTimer(context.Context, string) error {
	return errors.New("StartTimer should not be called")
}

func (s stubTriggerExecutor) StartService(ctx context.Context, serviceUnit string) error {
	if s.startService == nil {
		return nil
	}
	return s.startService(ctx, serviceUnit)
}

func (s stubTriggerExecutor) DisableTimer(context.Context, string) error {
	return errors.New("DisableTimer should not be called")
}

func (s stubTriggerExecutor) StopTimer(context.Context, string) error {
	return errors.New("StopTimer should not be called")
}

func TestTriggerCommandStartsRenderedService(t *testing.T) {
	originalResolveConfigPath := resolveConfigPath
	originalResolveCurrentUID := resolveCurrentUID
	originalNewSystemctlExecutor := newSystemctlExecutor
	t.Cleanup(func() {
		resolveConfigPath = originalResolveConfigPath
		resolveCurrentUID = originalResolveCurrentUID
		newSystemctlExecutor = originalNewSystemctlExecutor
	})

	resolveCurrentUID = func() (uint32, error) { return 1000, nil }

	cfgPath := filepath.Join(t.TempDir(), "timertab.yaml")
	cfg := &config.File{
		Version: 1,
		Jobs: []config.Job{{
			ID:   "job-a",
			When: config.ScheduleList{"@hourly"},
			Run:  config.ShellCommand("echo hi"),
		}},
	}
	if err := saveConfig(cfgPath, cfg); err != nil {
		t.Fatalf("saveConfig() error = %v", err)
	}

	resolveConfigPath = func(override string) (string, error) {
		if override != cfgPath {
			t.Fatalf("override = %q, want %q", override, cfgPath)
		}
		return cfgPath, nil
	}

	var gotUID uint32
	var gotService string
	newSystemctlExecutor = func(targetUID uint32) systemctl.Executor {
		gotUID = targetUID
		return stubTriggerExecutor{
			startService: func(_ context.Context, serviceUnit string) error {
				gotService = serviceUnit
				return nil
			},
		}
	}

	cmd := NewRootCommand()
	stdout := &bytes.Buffer{}
	cmd.SetOut(stdout)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"trigger", "job-a", "--config", cfgPath})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	if gotUID != 1000 {
		t.Fatalf("target uid = %d, want %d", gotUID, 1000)
	}
	if gotService == "" {
		t.Fatalf("StartService() did not receive a service name")
	}
	if !strings.HasSuffix(gotService, ".service") {
		t.Fatalf("service unit = %q, want .service suffix", gotService)
	}

	out := stdout.String()
	if !strings.Contains(out, "triggered job-a (") {
		t.Fatalf("stdout = %q, want success message", out)
	}
	if !strings.Contains(out, gotService) {
		t.Fatalf("stdout = %q, want rendered service name %q", out, gotService)
	}
}

func TestTriggerCommandReturnsClearErrorForUnknownID(t *testing.T) {
	originalResolveConfigPath := resolveConfigPath
	originalNewSystemctlExecutor := newSystemctlExecutor
	t.Cleanup(func() {
		resolveConfigPath = originalResolveConfigPath
		newSystemctlExecutor = originalNewSystemctlExecutor
	})

	cfgPath := filepath.Join(t.TempDir(), "timertab.yaml")
	if err := saveConfig(cfgPath, &config.File{
		Version: 1,
		Jobs: []config.Job{{
			ID:   "existing",
			When: config.ScheduleList{"@daily"},
			Run:  config.ShellCommand("echo ok"),
		}},
	}); err != nil {
		t.Fatalf("saveConfig() error = %v", err)
	}

	resolveConfigPath = func(string) (string, error) { return cfgPath, nil }
	newSystemctlExecutor = func(uint32) systemctl.Executor {
		return stubTriggerExecutor{
			startService: func(context.Context, string) error {
				return errors.New("StartService should not run for unknown id")
			},
		}
	}

	cmd := NewRootCommand()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"trigger", "missing", "--config", cfgPath})

	err := cmd.Execute()
	if err == nil {
		t.Fatalf("Execute() error = nil, want non-nil")
	}
	if !strings.Contains(err.Error(), `job "missing" not found`) {
		t.Fatalf("error = %q, want not-found message", err.Error())
	}
}

func TestTriggerCommandUsesSystemScopeForRoot(t *testing.T) {
	originalResolveConfigPath := resolveConfigPath
	originalResolveCurrentUID := resolveCurrentUID
	originalNewSystemctlExecutor := newSystemctlExecutor
	t.Cleanup(func() {
		resolveConfigPath = originalResolveConfigPath
		resolveCurrentUID = originalResolveCurrentUID
		newSystemctlExecutor = originalNewSystemctlExecutor
	})

	resolveCurrentUID = func() (uint32, error) { return 0, nil }

	cfgPath := filepath.Join(t.TempDir(), "timertab.yaml")
	if err := saveConfig(cfgPath, &config.File{
		Version: 1,
		Jobs: []config.Job{{
			ID:   "root-job",
			When: config.ScheduleList{"@daily"},
			Run:  config.ShellCommand("echo ok"),
		}},
	}); err != nil {
		t.Fatalf("saveConfig() error = %v", err)
	}

	resolveConfigPath = func(string) (string, error) { return cfgPath, nil }

	var gotUID uint32
	newSystemctlExecutor = func(targetUID uint32) systemctl.Executor {
		gotUID = targetUID
		return stubTriggerExecutor{}
	}

	cmd := NewRootCommand()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"trigger", "root-job", "--config", cfgPath})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if gotUID != 0 {
		t.Fatalf("target uid = %d, want 0", gotUID)
	}
}

func TestTriggerCommandCompletesKnownJobIDs(t *testing.T) {
	originalResolveConfigPath := resolveConfigPath
	t.Cleanup(func() {
		resolveConfigPath = originalResolveConfigPath
	})

	cfgPath := filepath.Join(t.TempDir(), "timertab.yaml")
	if err := saveConfig(cfgPath, &config.File{
		Version: 1,
		Jobs: []config.Job{
			{ID: "alpha", When: config.ScheduleList{"@daily"}, Run: config.ShellCommand("echo alpha")},
			{ID: "beta", When: config.ScheduleList{"@hourly"}, Run: config.ShellCommand("echo beta")},
		},
	}); err != nil {
		t.Fatalf("saveConfig() error = %v", err)
	}

	resolveConfigPath = func(override string) (string, error) {
		if override != cfgPath {
			t.Fatalf("override = %q, want %q", override, cfgPath)
		}
		return cfgPath, nil
	}

	root := NewRootCommand()
	triggerCmd, _, err := root.Find([]string{"trigger"})
	if err != nil {
		t.Fatalf("Find(trigger) error = %v", err)
	}
	if err := triggerCmd.Flags().Set("config", cfgPath); err != nil {
		t.Fatalf("Flags().Set(config) error = %v", err)
	}

	completions, directive := triggerCmd.ValidArgsFunction(triggerCmd, []string{}, "b")
	if directive != cobra.ShellCompDirectiveNoFileComp {
		t.Fatalf("directive = %v, want NoFileComp", directive)
	}
	if len(completions) != 1 || completions[0] != "beta" {
		t.Fatalf("completions = %v, want [beta]", completions)
	}
}
