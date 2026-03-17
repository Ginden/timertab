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
	originalResolveConfigPath := resolveConfigPath
	originalResolveCurrentUID := resolveCurrentUID
	originalRunJournalctl := runJournalctl
	t.Cleanup(func() {
		resolveConfigPath = originalResolveConfigPath
		resolveCurrentUID = originalResolveCurrentUID
		runJournalctl = originalRunJournalctl
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
	originalResolveConfigPath := resolveConfigPath
	originalRunJournalctl := runJournalctl
	t.Cleanup(func() {
		resolveConfigPath = originalResolveConfigPath
		runJournalctl = originalRunJournalctl
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
