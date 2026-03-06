package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ginden/timertab/internal/config"
	"github.com/ginden/timertab/internal/systemd"
)

func TestStatusCommandPrintsRowsAndHandlesMissingUnits(t *testing.T) {
	originalValidateTargetUserPermission := validateTargetUserPermission
	originalResolveConfigPath := resolveConfigPath
	originalResolveTargetUID := resolveTargetUID
	originalRunSystemctlShow := runSystemctlShow
	t.Cleanup(func() {
		validateTargetUserPermission = originalValidateTargetUserPermission
		resolveConfigPath = originalResolveConfigPath
		resolveTargetUID = originalResolveTargetUID
		runSystemctlShow = originalRunSystemctlShow
	})

	validateTargetUserPermission = func(string) error { return nil }
	resolveTargetUID = func(string) (uint32, error) { return 1000, nil }

	cfgPath := filepath.Join(t.TempDir(), "timertab.yaml")
	cfg := &config.File{
		Version: 1,
		Jobs: []config.Job{
			{ID: "alpha", When: config.ScheduleList{"@hourly"}, Run: "echo alpha"},
			{ID: "beta", When: config.ScheduleList{"@daily"}, Run: "echo beta"},
		},
	}
	if err := saveConfig(cfgPath, cfg); err != nil {
		t.Fatalf("saveConfig() error = %v", err)
	}

	resolveConfigPath = func(targetUser, override string) (string, error) {
		if targetUser != "" {
			t.Fatalf("targetUser = %q, want empty", targetUser)
		}
		if override != cfgPath {
			t.Fatalf("override = %q, want %q", override, cfgPath)
		}
		return cfgPath, nil
	}

	alphaRendered, err := systemd.RenderJobUnits(1000, cfg.Jobs[0])
	if err != nil {
		t.Fatalf("RenderJobUnits(alpha) error = %v", err)
	}
	betaRendered, err := systemd.RenderJobUnits(1000, cfg.Jobs[1])
	if err != nil {
		t.Fatalf("RenderJobUnits(beta) error = %v", err)
	}

	runSystemctlShow = func(_ context.Context, args ...string) (string, string, error) {
		if len(args) < 3 {
			return "", "", fmt.Errorf("unexpected args: %v", args)
		}
		unit := args[2]
		switch unit {
		case alphaRendered.TimerName:
			return strings.Join([]string{
				"LastTriggerUSec=Fri 2026-03-06 10:00:00 CET",
				"NextElapseUSecRealtime=Fri 2026-03-06 11:00:00 CET",
			}, "\n"), "", nil
		case alphaRendered.ServiceName:
			return "Result=success\n", "", nil
		case betaRendered.TimerName, betaRendered.ServiceName:
			return "", "Unit " + unit + " could not be found.", errors.New("exit status 1")
		default:
			return "", "", fmt.Errorf("unexpected unit %q", unit)
		}
	}

	stdout := &bytes.Buffer{}
	cmd := NewRootCommand()
	cmd.SetOut(stdout)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"status", "--config", cfgPath})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	out := stdout.String()
	if !strings.Contains(out, "id") || !strings.Contains(out, "last_run") || !strings.Contains(out, "next_trigger") || !strings.Contains(out, "result") {
		t.Fatalf("status output missing header, got:\n%s", out)
	}
	if !strings.Contains(out, "alpha") ||
		!strings.Contains(out, "Fri 2026-03-06 10:00:00 CET") ||
		!strings.Contains(out, "Fri 2026-03-06 11:00:00 CET") ||
		!strings.Contains(out, "pass") {
		t.Fatalf("status output missing alpha row, got:\n%s", out)
	}
	betaLine := ""
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "beta") {
			betaLine = line
			break
		}
	}
	if betaLine == "" || strings.Count(betaLine, "unknown") < 3 {
		t.Fatalf("status output missing beta row, got:\n%s", out)
	}
}

func TestStatusCommandReturnsErrorForSystemctlFailure(t *testing.T) {
	originalValidateTargetUserPermission := validateTargetUserPermission
	originalResolveConfigPath := resolveConfigPath
	originalResolveTargetUID := resolveTargetUID
	originalRunSystemctlShow := runSystemctlShow
	t.Cleanup(func() {
		validateTargetUserPermission = originalValidateTargetUserPermission
		resolveConfigPath = originalResolveConfigPath
		resolveTargetUID = originalResolveTargetUID
		runSystemctlShow = originalRunSystemctlShow
	})

	validateTargetUserPermission = func(string) error { return nil }
	resolveTargetUID = func(string) (uint32, error) { return 1000, nil }

	cfgPath := filepath.Join(t.TempDir(), "timertab.yaml")
	if err := saveConfig(cfgPath, &config.File{
		Version: 1,
		Jobs: []config.Job{{
			ID:   "alpha",
			When: config.ScheduleList{"@hourly"},
			Run:  "echo alpha",
		}},
	}); err != nil {
		t.Fatalf("saveConfig() error = %v", err)
	}

	resolveConfigPath = func(_, _ string) (string, error) {
		return cfgPath, nil
	}

	runSystemctlShow = func(_ context.Context, _ ...string) (string, string, error) {
		return "", "transport endpoint down", errors.New("exit status 1")
	}

	cmd := NewRootCommand()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"status", "--config", cfgPath})

	err := cmd.Execute()
	if err == nil {
		t.Fatalf("Execute() error = nil, want non-nil")
	}
	if !strings.Contains(err.Error(), "transport endpoint down") {
		t.Fatalf("error = %q, want systemctl stderr details", err.Error())
	}
}
