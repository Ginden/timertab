package cli

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ginden/timertab/internal/config"
)

func TestDiffCommandPrintsDryRunReport(t *testing.T) {
	originalValidateTargetUserPermission := validateTargetUserPermission
	originalResolveConfigPath := resolveConfigPath
	originalRunDryRunPlan := runDryRunPlan
	t.Cleanup(func() {
		validateTargetUserPermission = originalValidateTargetUserPermission
		resolveConfigPath = originalResolveConfigPath
		runDryRunPlan = originalRunDryRunPlan
	})

	validateTargetUserPermission = func(string) error { return nil }

	cfgPath := filepath.Join(t.TempDir(), "timertab.yaml")
	if err := saveConfig(cfgPath, &config.File{
		Version: 1,
		Jobs: []config.Job{{
			ID:   "job-1",
			When: config.ScheduleList{"@hourly"},
			Run:  "echo run",
		}},
	}); err != nil {
		t.Fatalf("saveConfig() error = %v", err)
	}

	resolveConfigPath = func(_, _ string) (string, error) {
		return cfgPath, nil
	}

	runDryRunPlan = func(_ context.Context, loaded *config.File, _ string) (applyReport, error) {
		if loaded == nil || len(loaded.Jobs) != 1 || loaded.Jobs[0].ID != "job-1" {
			t.Fatalf("loaded config not passed to dry-run plan: %#v", loaded)
		}
		return applyReport{
			Created:  []string{"/tmp/a.service"},
			Modified: []string{"/tmp/b.timer"},
			Deleted:  []string{"/tmp/c.service"},
		}, nil
	}

	stdout := &bytes.Buffer{}
	cmd := NewRootCommand()
	cmd.SetOut(stdout)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"diff", "--config", cfgPath})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	out := stdout.String()
	if !strings.Contains(out, "would create /tmp/a.service\n") ||
		!strings.Contains(out, "would modify /tmp/b.timer\n") ||
		!strings.Contains(out, "would delete /tmp/c.service\n") ||
		!strings.Contains(out, "summary: create=1 modify=1 delete=1\n") {
		t.Fatalf("stdout missing diff output, got:\n%s", out)
	}
}
