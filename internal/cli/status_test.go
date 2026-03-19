package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ginden/timertab/internal/config"
	"github.com/ginden/timertab/internal/systemd"
)

func TestStatusCommandPrintsRowsAndHandlesMissingUnits(t *testing.T) {
	originalResolveConfigPath := resolveConfigPath
	originalResolveCurrentUID := resolveCurrentUID
	originalRunSystemctlShow := runSystemctlShow
	t.Cleanup(func() {
		resolveConfigPath = originalResolveConfigPath
		resolveCurrentUID = originalResolveCurrentUID
		runSystemctlShow = originalRunSystemctlShow
	})

	resolveCurrentUID = func() (uint32, error) { return 1000, nil }

	cfgPath := filepath.Join(t.TempDir(), "timertab.yaml")
	cfg := &config.File{
		Version: 1,
		Jobs: []config.Job{
			{ID: "alpha", When: config.ScheduleList{"@hourly"}, Run: config.ShellCommand("echo alpha")},
			{ID: "beta", When: config.ScheduleList{"@daily"}, Run: config.ShellCommand("echo beta")},
		},
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

	alphaRendered, err := systemd.RenderJobUnits(1000, config.DefaultInstanceID, cfg.Jobs[0])
	if err != nil {
		t.Fatalf("RenderJobUnits() error = %v", err)
	}
	betaRendered, err := systemd.RenderJobUnits(1000, config.DefaultInstanceID, cfg.Jobs[1])
	if err != nil {
		t.Fatalf("RenderJobUnits() error = %v", err)
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

func TestStatusCommandForRootUsesSystemManager(t *testing.T) {
	originalResolveConfigPath := resolveConfigPath
	originalResolveCurrentUID := resolveCurrentUID
	originalRunSystemctlShow := runSystemctlShow
	t.Cleanup(func() {
		resolveConfigPath = originalResolveConfigPath
		resolveCurrentUID = originalResolveCurrentUID
		runSystemctlShow = originalRunSystemctlShow
	})

	resolveCurrentUID = func() (uint32, error) { return 0, nil }

	cfgPath := filepath.Join(t.TempDir(), "timertab.yaml")
	cfg := &config.File{
		Version: 1,
		Jobs: []config.Job{{
			ID:   "alpha",
			When: config.ScheduleList{"@hourly"},
			Run:  config.ShellCommand("echo alpha"),
		}},
	}
	if err := saveConfig(cfgPath, cfg); err != nil {
		t.Fatalf("saveConfig() error = %v", err)
	}

	resolveConfigPath = func(string) (string, error) { return cfgPath, nil }

	rendered, err := systemd.RenderJobUnits(0, config.DefaultInstanceID, cfg.Jobs[0])
	if err != nil {
		t.Fatalf("RenderJobUnits() error = %v", err)
	}

	calls := make([][]string, 0, 2)
	runSystemctlShow = func(_ context.Context, args ...string) (string, string, error) {
		calls = append(calls, append([]string(nil), args...))
		if len(args) < 2 {
			return "", "", fmt.Errorf("unexpected args: %v", args)
		}
		unit := args[1]
		switch unit {
		case rendered.TimerName:
			return "LastTriggerUSec=n/a\nNextElapseUSecRealtime=n/a\n", "", nil
		case rendered.ServiceName:
			return "Result=success\n", "", nil
		default:
			return "", "", fmt.Errorf("unexpected unit %q", unit)
		}
	}

	cmd := NewRootCommand()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"status", "--config", cfgPath})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	want := [][]string{
		{"show", rendered.TimerName, "--property=LastTriggerUSec", "--property=NextElapseUSecRealtime"},
		{"show", rendered.ServiceName, "--property=Result"},
	}
	if len(calls) != len(want) {
		t.Fatalf("runSystemctlShow call count = %d, want %d (%v)", len(calls), len(want), calls)
	}
	for idx := range want {
		if strings.Join(calls[idx], "\x00") != strings.Join(want[idx], "\x00") {
			t.Fatalf("call[%d] = %v, want %v", idx, calls[idx], want[idx])
		}
	}
}

func TestStatusPrintableWidthIgnoresANSIEscapes(t *testing.T) {
	value := "\x1b[1;34malpha\x1b[0m"
	if got := statusPrintableWidth(value); got != len("alpha") {
		t.Fatalf("statusPrintableWidth(%q) = %d, want %d", value, got, len("alpha"))
	}
}

func TestStatusCommandPrintsDetailedStatusForJob(t *testing.T) {
	originalResolveConfigPath := resolveConfigPath
	originalResolveSystemdUnitDir := resolveSystemdUnitDir
	originalResolveCurrentUID := resolveCurrentUID
	originalRunSystemctlShow := runSystemctlShow
	originalRunJournalctl := runJournalctl
	t.Cleanup(func() {
		resolveConfigPath = originalResolveConfigPath
		resolveSystemdUnitDir = originalResolveSystemdUnitDir
		resolveCurrentUID = originalResolveCurrentUID
		runSystemctlShow = originalRunSystemctlShow
		runJournalctl = originalRunJournalctl
	})

	resolveCurrentUID = func() (uint32, error) { return 1000, nil }

	tempDir := t.TempDir()
	cfgPath := filepath.Join(tempDir, "timertab.yaml")
	unitDir := filepath.Join(tempDir, "systemd-user")
	cfg := &config.File{
		Version: 1,
		Jobs: []config.Job{{
			ID:         "alpha",
			Name:       "Alpha job",
			When:       config.ScheduleList{"@hourly"},
			Run:        config.ShellCommand("echo alpha"),
			Cwd:        "/srv/alpha",
			OnFailure:  &config.Hook{Command: "echo failed"},
			Persistent: func() *bool { v := true; return &v }(),
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
	resolveSystemdUnitDir = func(gotUID uint32) (string, error) {
		if gotUID != 1000 {
			t.Fatalf("resolveSystemdUnitDir() uid = %d, want %d", gotUID, 1000)
		}
		return unitDir, nil
	}

	rendered, err := systemd.RenderJobUnits(1000, config.DefaultInstanceID, cfg.Jobs[0])
	if err != nil {
		t.Fatalf("RenderJobUnits() error = %v", err)
	}

	runSystemctlShow = func(_ context.Context, args ...string) (string, string, error) {
		if len(args) < 3 {
			return "", "", fmt.Errorf("unexpected args: %v", args)
		}
		unit := args[2]
		switch unit {
		case rendered.ServiceName:
			return strings.Join([]string{
				"LoadState=loaded",
				"ActiveState=failed",
				"SubState=failed",
				"Result=exit-code",
				"FragmentPath=" + filepath.Join(unitDir, rendered.ServiceName),
				"UnitFileState=static",
			}, "\n") + "\n", "", nil
		case rendered.TimerName:
			return strings.Join([]string{
				"LoadState=loaded",
				"ActiveState=active",
				"SubState=waiting",
				"LastTriggerUSec=Fri 2026-03-06 10:00:00 CET",
				"NextElapseUSecRealtime=Fri 2026-03-06 11:00:00 CET",
				"FragmentPath=" + filepath.Join(unitDir, rendered.TimerName),
				"UnitFileState=enabled",
			}, "\n") + "\n", "", nil
		default:
			return "", "", fmt.Errorf("unexpected unit %q", unit)
		}
	}
	runJournalctl = func(_ context.Context, _ io.Reader, stdout, _ io.Writer, args ...string) error {
		want := []string{"--user", "-u", rendered.ServiceName, "-n", "20", "--no-pager"}
		if strings.Join(args, "\x00") != strings.Join(want, "\x00") {
			t.Fatalf("journalctl args = %v, want %v", args, want)
		}
		_, err := stdout.Write([]byte("Mar 17 10:00:00 host echo alpha\nMar 17 10:00:01 host done\n"))
		return err
	}

	stdout := &bytes.Buffer{}
	cmd := NewRootCommand()
	cmd.SetOut(stdout)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"status", "alpha", "--config", cfgPath})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	out := stdout.String()
	for _, needle := range []string{
		"STATUS alpha - Alpha job",
		"Result: FAIL",
		"Overview",
		"field         value",
		"job           alpha",
		"name          Alpha job",
		"last run      Fri 2026-03-06 10:00:00 CET",
		"next trigger  Fri 2026-03-06 11:00:00 CET",
		"config        " + cfgPath,
		"unit dir      " + unitDir,
		"Units",
		"kind     unit",
		"service  " + rendered.ServiceName,
		"timer    " + rendered.TimerName,
		filepath.Join(unitDir, rendered.ServiceName),
		filepath.Join(unitDir, rendered.TimerName),
		"Job YAML",
		"  id: alpha",
		"  name: Alpha job",
		"Service Unit",
		"  ExecStart=/bin/sh -lc \"echo alpha\"",
		"Timer Unit",
		"  Persistent=true",
		"Recent Logs",
		"  Mar 17 10:00:00 host echo alpha",
		"  Mar 17 10:00:01 host done",
		"Diagnostics",
		"Start with the first command, then continue only if you need more detail.",
		"1. Check the last service run",
		"Shows whether the job failed, the recent exit summary, and the most relevant status lines.",
		"systemctl --user status " + rendered.ServiceName,
		"2. Check whether the timer is armed",
		"systemctl --user status " + rendered.TimerName,
		"3. Read recent logs",
		"journalctl --user -u " + rendered.ServiceName + " -n 100 --no-pager",
		"5. View the loaded service unit",
		"systemctl --user cat " + rendered.ServiceName,
		"6. View the loaded timer unit",
		"systemctl --user cat " + rendered.TimerName,
	} {
		if !strings.Contains(out, needle) {
			t.Fatalf("detail output missing %q, got:\n%s", needle, out)
		}
	}
}

func TestStatusCommandShowsLogPeekFallbackWhenJournalctlFails(t *testing.T) {
	originalResolveConfigPath := resolveConfigPath
	originalResolveSystemdUnitDir := resolveSystemdUnitDir
	originalResolveCurrentUID := resolveCurrentUID
	originalRunSystemctlShow := runSystemctlShow
	originalRunJournalctl := runJournalctl
	t.Cleanup(func() {
		resolveConfigPath = originalResolveConfigPath
		resolveSystemdUnitDir = originalResolveSystemdUnitDir
		resolveCurrentUID = originalResolveCurrentUID
		runSystemctlShow = originalRunSystemctlShow
		runJournalctl = originalRunJournalctl
	})

	resolveCurrentUID = func() (uint32, error) { return 1000, nil }

	tempDir := t.TempDir()
	cfgPath := filepath.Join(tempDir, "timertab.yaml")
	unitDir := filepath.Join(tempDir, "systemd-user")
	cfg := &config.File{
		Version: 1,
		Jobs: []config.Job{{
			ID:   "alpha",
			When: config.ScheduleList{"@hourly"},
			Run:  config.ShellCommand("echo alpha"),
		}},
	}
	if err := saveConfig(cfgPath, cfg); err != nil {
		t.Fatalf("saveConfig() error = %v", err)
	}

	resolveConfigPath = func(string) (string, error) { return cfgPath, nil }
	resolveSystemdUnitDir = func(gotUID uint32) (string, error) {
		if gotUID != 1000 {
			t.Fatalf("resolveSystemdUnitDir() uid = %d, want %d", gotUID, 1000)
		}
		return unitDir, nil
	}

	rendered, err := systemd.RenderJobUnits(1000, config.DefaultInstanceID, cfg.Jobs[0])
	if err != nil {
		t.Fatalf("RenderJobUnits() error = %v", err)
	}

	runSystemctlShow = func(_ context.Context, args ...string) (string, string, error) {
		unit := args[2]
		switch unit {
		case rendered.ServiceName:
			return "LoadState=loaded\nActiveState=active\nSubState=exited\nResult=success\nFragmentPath=/tmp/service\nUnitFileState=static\n", "", nil
		case rendered.TimerName:
			return "LoadState=loaded\nActiveState=active\nSubState=waiting\nLastTriggerUSec=n/a\nNextElapseUSecRealtime=n/a\nFragmentPath=/tmp/timer\nUnitFileState=enabled\n", "", nil
		default:
			return "", "", fmt.Errorf("unexpected unit %q", unit)
		}
	}
	runJournalctl = func(_ context.Context, _ io.Reader, _ io.Writer, stderr io.Writer, _ ...string) error {
		_, _ = stderr.Write([]byte("journal unavailable"))
		return errors.New("exit status 1")
	}

	stdout := &bytes.Buffer{}
	cmd := NewRootCommand()
	cmd.SetOut(stdout)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"status", "alpha", "--config", cfgPath})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	out := stdout.String()
	if !strings.Contains(out, "Recent Logs") {
		t.Fatalf("detail output missing Recent Logs section, got:\n%s", out)
	}
	if !strings.Contains(out, "log preview unavailable: journal unavailable") {
		t.Fatalf("detail output missing log peek fallback, got:\n%s", out)
	}
}

func TestStatusCommandReturnsErrorForSystemctlFailure(t *testing.T) {
	originalResolveConfigPath := resolveConfigPath
	originalResolveCurrentUID := resolveCurrentUID
	originalRunSystemctlShow := runSystemctlShow
	t.Cleanup(func() {
		resolveConfigPath = originalResolveConfigPath
		resolveCurrentUID = originalResolveCurrentUID
		runSystemctlShow = originalRunSystemctlShow
	})

	resolveCurrentUID = func() (uint32, error) { return 1000, nil }

	cfgPath := filepath.Join(t.TempDir(), "timertab.yaml")
	if err := saveConfig(cfgPath, &config.File{
		Version: 1,
		Jobs: []config.Job{{
			ID:   "alpha",
			When: config.ScheduleList{"@hourly"},
			Run:  config.ShellCommand("echo alpha"),
		}},
	}); err != nil {
		t.Fatalf("saveConfig() error = %v", err)
	}

	resolveConfigPath = func(string) (string, error) {
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

func TestStatusCommandJSONOutput(t *testing.T) {
	originalResolveConfigPath := resolveConfigPath
	originalResolveCurrentUID := resolveCurrentUID
	originalRunSystemctlShow := runSystemctlShow
	t.Cleanup(func() {
		resolveConfigPath = originalResolveConfigPath
		resolveCurrentUID = originalResolveCurrentUID
		runSystemctlShow = originalRunSystemctlShow
	})

	resolveCurrentUID = func() (uint32, error) { return 1000, nil }

	cfgPath := filepath.Join(t.TempDir(), "timertab.yaml")
	cfg := &config.File{
		Version: 1,
		Jobs: []config.Job{{
			ID:   "alpha",
			When: config.ScheduleList{"@hourly"},
			Run:  config.ShellCommand("echo alpha"),
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

	rendered, err := systemd.RenderJobUnits(1000, config.DefaultInstanceID, cfg.Jobs[0])
	if err != nil {
		t.Fatalf("RenderJobUnits() error = %v", err)
	}

	runSystemctlShow = func(_ context.Context, args ...string) (string, string, error) {
		unit := args[2]
		switch unit {
		case rendered.TimerName:
			return "LastTriggerUSec=Fri 2026-03-06 10:00:00 CET\nNextElapseUSecRealtime=Fri 2026-03-06 11:00:00 CET\n", "", nil
		case rendered.ServiceName:
			return "Result=success\n", "", nil
		default:
			return "", "", fmt.Errorf("unexpected unit %q", unit)
		}
	}

	stdout := &bytes.Buffer{}
	cmd := NewRootCommand()
	cmd.SetOut(stdout)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"status", "--json", "--config", cfgPath})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	var payload statusJSONPayload
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("json.Unmarshal(stdout) error = %v\nstdout:\n%s", err, stdout.String())
	}
	if len(payload.Jobs) != 1 {
		t.Fatalf("jobs count = %d, want 1", len(payload.Jobs))
	}
	if payload.Jobs[0].ID != "alpha" {
		t.Fatalf("job id = %q, want %q", payload.Jobs[0].ID, "alpha")
	}
	if payload.Jobs[0].LastRun != "Fri 2026-03-06 10:00:00 CET" {
		t.Fatalf("last_run = %q", payload.Jobs[0].LastRun)
	}
	if payload.Jobs[0].NextTrigger != "Fri 2026-03-06 11:00:00 CET" {
		t.Fatalf("next_trigger = %q", payload.Jobs[0].NextTrigger)
	}
	if payload.Jobs[0].Result != "pass" {
		t.Fatalf("result = %q, want %q", payload.Jobs[0].Result, "pass")
	}
}

func TestStatusCommandRejectsJSONForDetailedView(t *testing.T) {
	originalResolveConfigPath := resolveConfigPath
	originalResolveCurrentUID := resolveCurrentUID
	t.Cleanup(func() {
		resolveConfigPath = originalResolveConfigPath
		resolveCurrentUID = originalResolveCurrentUID
	})

	resolveCurrentUID = func() (uint32, error) { return 1000, nil }

	cfgPath := filepath.Join(t.TempDir(), "timertab.yaml")
	if err := saveConfig(cfgPath, &config.File{
		Version: 1,
		Jobs: []config.Job{{
			ID:   "alpha",
			When: config.ScheduleList{"@hourly"},
			Run:  config.ShellCommand("echo alpha"),
		}},
	}); err != nil {
		t.Fatalf("saveConfig() error = %v", err)
	}

	resolveConfigPath = func(string) (string, error) { return cfgPath, nil }

	cmd := NewRootCommand()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"status", "alpha", "--json", "--config", cfgPath})

	err := cmd.Execute()
	if err == nil {
		t.Fatalf("Execute() error = nil, want non-nil")
	}
	if !strings.Contains(err.Error(), "--json is only supported for summary status") {
		t.Fatalf("error = %q", err.Error())
	}
}
