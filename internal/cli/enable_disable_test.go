package cli

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ginden/timertab/internal/config"
	"github.com/spf13/cobra"
)

func TestDisableCommandSetsEnabledFalseAndApplies(t *testing.T) {
	originalApply := runSystemctlApply
	originalEnsure := ensureSystemdBaseline
	t.Cleanup(func() {
		runSystemctlApply = originalApply
		ensureSystemdBaseline = originalEnsure
	})

	var ensureCalls int
	ensureSystemdBaseline = func() error {
		ensureCalls++
		return nil
	}

	var applyCalls int
	runSystemctlApply = func(_ context.Context, loaded *config.File, _ string) (applyReport, error) {
		applyCalls++
		if loaded == nil {
			t.Fatalf("loaded config = nil")
		}
		if len(loaded.Jobs) != 2 {
			t.Fatalf("jobs count = %d, want 2", len(loaded.Jobs))
		}
		if loaded.Jobs[0].ID != "target" || loaded.Jobs[0].Enabled == nil || *loaded.Jobs[0].Enabled {
			t.Fatalf("target job enabled = %#v, want false", loaded.Jobs[0].Enabled)
		}
		if loaded.Jobs[1].ID != "other" || loaded.Jobs[1].Enabled == nil || !*loaded.Jobs[1].Enabled {
			t.Fatalf("other job enabled = %#v, want true", loaded.Jobs[1].Enabled)
		}
		return applyReport{}, nil
	}

	trueValue := true
	cfgPath := filepath.Join(t.TempDir(), "timertab.yaml")
	if err := saveConfig(cfgPath, &config.File{
		Version: 1,
		Jobs: []config.Job{
			{ID: "target", When: config.ScheduleList{"@daily"}, Run: "echo target"},
			{ID: "other", When: config.ScheduleList{"@hourly"}, Run: "echo other", Enabled: &trueValue},
		},
	}); err != nil {
		t.Fatalf("saveConfig() error = %v", err)
	}

	cmd := NewRootCommand()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"disable", "target", "--config", cfgPath})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if ensureCalls != 1 {
		t.Fatalf("ensureSystemdBaseline calls = %d, want 1", ensureCalls)
	}
	if applyCalls != 1 {
		t.Fatalf("runSystemctlApply calls = %d, want 1", applyCalls)
	}

	loaded, err := config.LoadFromFile(cfgPath)
	if err != nil {
		t.Fatalf("LoadFromFile(%q) error = %v", cfgPath, err)
	}
	if loaded.Jobs[0].Enabled == nil || *loaded.Jobs[0].Enabled {
		t.Fatalf("saved target enabled = %#v, want false", loaded.Jobs[0].Enabled)
	}
	if loaded.Jobs[1].Enabled == nil || !*loaded.Jobs[1].Enabled {
		t.Fatalf("saved other enabled = %#v, want true", loaded.Jobs[1].Enabled)
	}
}

func TestEnableCommandSetsEnabledTrueAndApplies(t *testing.T) {
	originalApply := runSystemctlApply
	originalEnsure := ensureSystemdBaseline
	t.Cleanup(func() {
		runSystemctlApply = originalApply
		ensureSystemdBaseline = originalEnsure
	})

	ensureSystemdBaseline = func() error { return nil }
	runSystemctlApply = func(_ context.Context, loaded *config.File, _ string) (applyReport, error) {
		if loaded.Jobs[0].Enabled == nil || !*loaded.Jobs[0].Enabled {
			t.Fatalf("target enabled = %#v, want true", loaded.Jobs[0].Enabled)
		}
		return applyReport{}, nil
	}

	falseValue := false
	cfgPath := filepath.Join(t.TempDir(), "timertab.yaml")
	if err := saveConfig(cfgPath, &config.File{
		Version: 1,
		Jobs: []config.Job{{
			ID:      "target",
			When:    config.ScheduleList{"@daily"},
			Run:     "echo target",
			Enabled: &falseValue,
		}},
	}); err != nil {
		t.Fatalf("saveConfig() error = %v", err)
	}

	cmd := NewRootCommand()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"enable", "target", "--config", cfgPath})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	loaded, err := config.LoadFromFile(cfgPath)
	if err != nil {
		t.Fatalf("LoadFromFile(%q) error = %v", cfgPath, err)
	}
	if loaded.Jobs[0].Enabled == nil || !*loaded.Jobs[0].Enabled {
		t.Fatalf("saved target enabled = %#v, want true", loaded.Jobs[0].Enabled)
	}
}

func TestEnableDisableCommandsFailForUnknownID(t *testing.T) {
	originalApply := runSystemctlApply
	originalEnsure := ensureSystemdBaseline
	t.Cleanup(func() {
		runSystemctlApply = originalApply
		ensureSystemdBaseline = originalEnsure
	})

	runSystemctlApply = func(_ context.Context, _ *config.File, _ string) (applyReport, error) {
		return applyReport{}, errors.New("apply should not run")
	}
	ensureSystemdBaseline = func() error {
		return errors.New("systemd check should not run")
	}

	cfgPath := filepath.Join(t.TempDir(), "timertab.yaml")
	if err := saveConfig(cfgPath, &config.File{
		Version: 1,
		Jobs: []config.Job{{
			ID:   "existing",
			When: config.ScheduleList{"@daily"},
			Run:  "echo existing",
		}},
	}); err != nil {
		t.Fatalf("saveConfig() error = %v", err)
	}

	cmd := NewRootCommand()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"disable", "missing", "--config", cfgPath})

	err := cmd.Execute()
	if err == nil {
		t.Fatalf("Execute() error = nil, want non-nil")
	}
	if !strings.Contains(err.Error(), `job "missing" not found`) {
		t.Fatalf("error = %q, want not-found message", err.Error())
	}
}

func TestEnableDisableCommandsCompleteKnownJobIDs(t *testing.T) {
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
	for _, name := range []string{"enable", "disable"} {
		command, _, err := root.Find([]string{name})
		if err != nil {
			t.Fatalf("Find(%s) error = %v", name, err)
		}
		if err := command.Flags().Set("config", cfgPath); err != nil {
			t.Fatalf("Flags().Set(config) for %s error = %v", name, err)
		}

		matches, directive := command.ValidArgsFunction(command, []string{}, "b")
		if directive != cobra.ShellCompDirectiveNoFileComp {
			t.Fatalf("%s completion directive = %v, want NoFileComp", name, directive)
		}
		if len(matches) != 1 || matches[0] != "beta" {
			t.Fatalf("%s completions = %v, want [beta]", name, matches)
		}
	}
}
