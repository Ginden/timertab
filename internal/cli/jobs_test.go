package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ginden/timertab/internal/config"
	"github.com/ginden/timertab/internal/systemd"
	"github.com/spf13/cobra"
)

func TestEjectCommandRemovesJobAndManagedMarkers(t *testing.T) {
	originalResolveCurrentUID := resolveCurrentUID
	originalResolveSystemdUnitDir := resolveSystemdUnitDir
	t.Cleanup(func() {
		resolveCurrentUID = originalResolveCurrentUID
		resolveSystemdUnitDir = originalResolveSystemdUnitDir
	})

	targetUID := uint32(1000)
	unitDir := t.TempDir()
	resolveCurrentUID = func() (uint32, error) { return targetUID, nil }
	resolveSystemdUnitDir = func(gotUID uint32) (string, error) {
		if gotUID != targetUID {
			t.Fatalf("resolveSystemdUnitDir() uid = %d, want %d", gotUID, targetUID)
		}
		return unitDir, nil
	}

	cfgPath := filepath.Join(t.TempDir(), "timertab.yaml")
	cfg := &config.File{
		Version: 1,
		Jobs: []config.Job{{
			ID:   "demo",
			When: config.ScheduleList{"@daily"},
			Run:  config.ShellCommand("echo demo"),
		}},
	}
	if err := saveConfig(cfgPath, cfg); err != nil {
		t.Fatalf("saveConfig() error = %v", err)
	}

	rendered, err := systemd.RenderJobUnits(targetUID, config.DefaultInstanceID, cfg.Jobs[0])
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
	cmd.SetArgs([]string{"eject", "demo", "--config", cfgPath})

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
		Jobs: []config.Job{{
			ID:   "existing",
			When: config.ScheduleList{"@daily"},
			Run:  config.ShellCommand("echo existing"),
		}},
	}
	if err := saveConfig(cfgPath, cfg); err != nil {
		t.Fatalf("saveConfig() error = %v", err)
	}

	cmd := NewRootCommand()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"eject", "missing", "--config", cfgPath})

	err := cmd.Execute()
	if err == nil {
		t.Fatalf("Execute() error = nil, want non-nil")
	}
	if !strings.Contains(err.Error(), `job "missing" not found`) {
		t.Fatalf("error = %v, want not-found message", err)
	}
}

func TestEjectCommandCompletesKnownJobIDs(t *testing.T) {
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
