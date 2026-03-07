package cli

import (
	"bytes"
	"context"
	"errors"
	"io"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/ginden/timertab/internal/config"
	"github.com/ginden/timertab/internal/systemd"
	"github.com/spf13/cobra"
)

func TestLogsCommandResolvesJobAndRunsJournalctl(t *testing.T) {
	originalValidateTargetUserPermission := validateTargetUserPermission
	originalResolveConfigPath := resolveConfigPath
	originalResolveTargetUID := resolveTargetUID
	originalRunJournalctl := runJournalctl
	t.Cleanup(func() {
		validateTargetUserPermission = originalValidateTargetUserPermission
		resolveConfigPath = originalResolveConfigPath
		resolveTargetUID = originalResolveTargetUID
		runJournalctl = originalRunJournalctl
	})

	validateTargetUserPermission = func(string) error { return nil }
	resolveTargetUID = func(string) (uint32, error) { return 1000, nil }

	cfgPath := filepath.Join(t.TempDir(), "timertab.yaml")
	cfg := &config.File{
		Version: 1,
		Jobs: []config.Job{{
			ID:   "job-a",
			When: config.ScheduleList{"@hourly"},
			Run:  "echo hi",
		}},
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

	rendered, err := systemd.RenderJobUnits(1000, config.DefaultInstanceID, cfg.Jobs[0])
	if err != nil {
		t.Fatalf("RenderJobUnits() error = %v", err)
	}

	var gotArgs []string
	runJournalctl = func(_ context.Context, _ io.Reader, _ io.Writer, _ io.Writer, args ...string) error {
		gotArgs = append([]string(nil), args...)
		return nil
	}

	cmd := NewRootCommand()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetIn(bytes.NewBuffer(nil))
	cmd.SetArgs([]string{
		"logs", "job-a",
		"--config", cfgPath,
		"-n", "30",
		"--since", "today",
		"--until", "now",
		"--follow",
		"--no-pager",
	})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	want := []string{"--user", "-u", rendered.ServiceName, "-n", "30", "-f", "--since", "today", "--until", "now", "--no-pager"}
	if !reflect.DeepEqual(gotArgs, want) {
		t.Fatalf("journalctl args = %v, want %v", gotArgs, want)
	}
}

func TestLogsCommandReturnsClearErrorForUnknownID(t *testing.T) {
	originalValidateTargetUserPermission := validateTargetUserPermission
	originalResolveConfigPath := resolveConfigPath
	originalRunJournalctl := runJournalctl
	t.Cleanup(func() {
		validateTargetUserPermission = originalValidateTargetUserPermission
		resolveConfigPath = originalResolveConfigPath
		runJournalctl = originalRunJournalctl
	})

	validateTargetUserPermission = func(string) error { return nil }

	cfgPath := filepath.Join(t.TempDir(), "timertab.yaml")
	if err := saveConfig(cfgPath, &config.File{
		Version: 1,
		Jobs: []config.Job{{
			ID:   "existing",
			When: config.ScheduleList{"@daily"},
			Run:  "echo ok",
		}},
	}); err != nil {
		t.Fatalf("saveConfig() error = %v", err)
	}

	resolveConfigPath = func(_, _ string) (string, error) { return cfgPath, nil }
	runJournalctl = func(_ context.Context, _ io.Reader, _ io.Writer, _ io.Writer, _ ...string) error {
		return errors.New("journalctl should not run for unknown id")
	}

	cmd := NewRootCommand()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"logs", "missing", "--config", cfgPath})

	err := cmd.Execute()
	if err == nil {
		t.Fatalf("Execute() error = nil, want non-nil")
	}
	if !strings.Contains(err.Error(), `job "missing" not found`) {
		t.Fatalf("error = %q, want not-found message", err.Error())
	}
}

func TestLogsCommandCompletesKnownJobIDs(t *testing.T) {
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
	logsCmd, _, err := root.Find([]string{"logs"})
	if err != nil {
		t.Fatalf("Find(logs) error = %v", err)
	}
	if err := logsCmd.Flags().Set("config", cfgPath); err != nil {
		t.Fatalf("Flags().Set(config) error = %v", err)
	}

	completions, directive := logsCmd.ValidArgsFunction(logsCmd, []string{}, "a")
	if directive != cobra.ShellCompDirectiveNoFileComp {
		t.Fatalf("directive = %v, want NoFileComp", directive)
	}
	if len(completions) != 1 || completions[0] != "alpha" {
		t.Fatalf("completions = %v, want [alpha]", completions)
	}
}
