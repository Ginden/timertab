package cli

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ginden/timertab/internal/config"
	"github.com/ginden/timertab/internal/systemd"
)

const commentedConfig = `# main timertab config
version: 1
jobs:
  # TODO: raise to 2h after migration
  - id: target
    name: "target job"
    when: "@daily" # quoted on purpose
    run: echo target
  - id: other
    when: "@hourly"
    run: echo other
`

func TestDisableCommandPreservesCommentsAndFormatting(t *testing.T) {
	originalApply := runSystemctlApply
	originalEnsure := ensureSystemdBaseline
	t.Cleanup(func() {
		runSystemctlApply = originalApply
		ensureSystemdBaseline = originalEnsure
	})
	ensureSystemdBaseline = func() error { return nil }
	runSystemctlApply = func(_ context.Context, _ *config.File) (applyReport, error) {
		return applyReport{}, nil
	}

	cfgPath := filepath.Join(t.TempDir(), "timertab.yaml")
	if err := os.WriteFile(cfgPath, []byte(commentedConfig), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	cmd := NewRootCommand()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"disable", "target", "--config", cfgPath})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	after, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	saved := string(after)

	for _, want := range []string{
		"# main timertab config",
		"# TODO: raise to 2h after migration",
		`when: "@daily" # quoted on purpose`,
	} {
		if !strings.Contains(saved, want) {
			t.Errorf("saved config lost %q:\n%s", want, saved)
		}
	}

	loaded, err := config.LoadFromFile(cfgPath)
	if err != nil {
		t.Fatalf("LoadFromFile() error = %v", err)
	}
	if loaded.Jobs[0].Enabled == nil || *loaded.Jobs[0].Enabled {
		t.Fatalf("target enabled = %#v, want false", loaded.Jobs[0].Enabled)
	}
	if loaded.Jobs[1].Enabled != nil {
		t.Fatalf("other enabled = %#v, want nil", loaded.Jobs[1].Enabled)
	}
}

func TestEnableCommandPatchesExistingEnabledScalar(t *testing.T) {
	originalApply := runSystemctlApply
	originalEnsure := ensureSystemdBaseline
	t.Cleanup(func() {
		runSystemctlApply = originalApply
		ensureSystemdBaseline = originalEnsure
	})
	ensureSystemdBaseline = func() error { return nil }
	runSystemctlApply = func(_ context.Context, _ *config.File) (applyReport, error) {
		return applyReport{}, nil
	}

	input := `version: 1
jobs:
  - id: target
    when: "@daily"
    run: echo target
    enabled: false # paused during migration
`
	cfgPath := filepath.Join(t.TempDir(), "timertab.yaml")
	if err := os.WriteFile(cfgPath, []byte(input), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	cmd := NewRootCommand()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"enable", "target", "--config", cfgPath})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	after, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	saved := string(after)
	if !strings.Contains(saved, "enabled: true # paused during migration") {
		t.Fatalf("enabled scalar not patched in place:\n%s", saved)
	}
}

func TestEjectCommandPreservesCommentsOfRemainingJobs(t *testing.T) {
	originalResolveCurrentUID := resolveCurrentUID
	originalResolveSystemdUnitDir := resolveSystemdUnitDir
	t.Cleanup(func() {
		resolveCurrentUID = originalResolveCurrentUID
		resolveSystemdUnitDir = originalResolveSystemdUnitDir
	})

	targetUID := uint32(1000)
	unitDir := t.TempDir()
	resolveCurrentUID = func() (uint32, error) { return targetUID, nil }
	resolveSystemdUnitDir = func(uint32) (string, error) { return unitDir, nil }

	cfgPath := filepath.Join(t.TempDir(), "timertab.yaml")
	if err := os.WriteFile(cfgPath, []byte(commentedConfig), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	job := config.Job{ID: "other", When: config.ScheduleList{"@hourly"}, Run: config.ShellCommand("echo other")}
	rendered, err := systemd.RenderJobUnits(targetUID, config.DefaultInstanceID, job)
	if err != nil {
		t.Fatalf("RenderJobUnits() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(unitDir, rendered.ServiceName), []byte(rendered.ServiceContent), 0o644); err != nil {
		t.Fatalf("WriteFile(service) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(unitDir, rendered.TimerName), []byte(rendered.TimerContent), 0o644); err != nil {
		t.Fatalf("WriteFile(timer) error = %v", err)
	}

	cmd := NewRootCommand()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"eject", "other", "--config", cfgPath})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	after, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	saved := string(after)

	for _, want := range []string{
		"# main timertab config",
		"# TODO: raise to 2h after migration",
		`when: "@daily" # quoted on purpose`,
	} {
		if !strings.Contains(saved, want) {
			t.Errorf("saved config lost %q:\n%s", want, saved)
		}
	}
	if strings.Contains(saved, "echo other") {
		t.Fatalf("ejected job still present:\n%s", saved)
	}

	loaded, err := config.LoadFromFile(cfgPath)
	if err != nil {
		t.Fatalf("LoadFromFile() error = %v", err)
	}
	if len(loaded.Jobs) != 1 || loaded.Jobs[0].ID != "target" {
		t.Fatalf("jobs after eject = %#v, want only target", loaded.Jobs)
	}
}

func TestImportCommandPreservesCommentsOfExistingConfig(t *testing.T) {
	originalApply := runSystemctlApply
	originalEnsure := ensureSystemdBaseline
	t.Cleanup(func() {
		runSystemctlApply = originalApply
		ensureSystemdBaseline = originalEnsure
	})
	originalIsTTY := importOutputIsTTY
	t.Cleanup(func() { importOutputIsTTY = originalIsTTY })
	importOutputIsTTY = func(io.Writer) bool { return true }
	ensureSystemdBaseline = func() error { return nil }
	runSystemctlApply = func(_ context.Context, _ *config.File) (applyReport, error) {
		return applyReport{}, nil
	}
	t.Setenv("EDITOR", "true")

	cfgPath := filepath.Join(t.TempDir(), "timertab.yaml")
	if err := os.WriteFile(cfgPath, []byte(commentedConfig), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	cmd := NewRootCommand()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetIn(strings.NewReader("30 6 * * * /usr/local/bin/backup.sh\n"))
	cmd.SetArgs([]string{"import", "--stdin", "--no-apply", "--config", cfgPath})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	after, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	saved := string(after)

	for _, want := range []string{
		"# main timertab config",
		"# TODO: raise to 2h after migration",
		`when: "@daily" # quoted on purpose`,
	} {
		if !strings.Contains(saved, want) {
			t.Errorf("saved config lost %q:\n%s", want, saved)
		}
	}

	loaded, err := config.LoadFromFile(cfgPath)
	if err != nil {
		t.Fatalf("LoadFromFile() error = %v", err)
	}
	if len(loaded.Jobs) != 3 {
		t.Fatalf("jobs after import = %d, want 3", len(loaded.Jobs))
	}
	if got := loaded.Jobs[2].Run.Display(); got != "/usr/local/bin/backup.sh" {
		t.Fatalf("imported job run = %q", got)
	}
}
