package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ginden/timertab/internal/config"
)

func TestAddCommandAppendsJobWithoutApply(t *testing.T) {
	originalEnsure := ensureSystemdBaseline
	originalApply := runSystemctlApply
	t.Cleanup(func() {
		ensureSystemdBaseline = originalEnsure
		runSystemctlApply = originalApply
	})
	ensureSystemdBaseline = func() error {
		t.Fatalf("ensureSystemdBaseline should not run with --no-apply")
		return nil
	}
	runSystemctlApply = func(context.Context, *config.File) (applyReport, error) {
		t.Fatalf("runSystemctlApply should not run with --no-apply")
		return applyReport{}, nil
	}

	cfgPath := filepath.Join(t.TempDir(), "timertab.yaml")
	initial := []byte(`version: 1
jobs:
  # keep comment
  - id: existing
    when: "@daily"
    run: echo existing
`)
	if err := writeConfigFile(cfgPath, initial); err != nil {
		t.Fatalf("writeConfigFile() error = %v", err)
	}

	stdout := &bytes.Buffer{}
	cmd := NewRootCommand()
	cmd.SetOut(stdout)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"add", "--config", cfgPath, "--no-apply", "--id", "new-job", "--name", "new job", "--when", "@hourly", "--env", "FOO=bar", "--", "echo new"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !strings.Contains(stdout.String(), "no apply") {
		t.Fatalf("stdout missing no-apply message:\n%s", stdout.String())
	}

	raw, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if !bytes.Contains(raw, []byte("# keep comment")) {
		t.Fatalf("existing comment was not preserved:\n%s", raw)
	}

	loaded, err := config.LoadFromBytes(raw)
	if err != nil {
		t.Fatalf("LoadFromBytes() error = %v\n%s", err, raw)
	}
	if len(loaded.Jobs) != 2 {
		t.Fatalf("jobs count = %d, want 2", len(loaded.Jobs))
	}
	added := loaded.Jobs[1]
	if added.ID != "new-job" || added.Name != "new job" || added.When[0] != "@hourly" {
		t.Fatalf("added job metadata = %#v", added)
	}
	if got := added.Env["FOO"]; got != "bar" {
		t.Fatalf("added env FOO = %q, want bar", got)
	}
	if got := added.Run.Display(); got != "echo new" {
		t.Fatalf("added run = %q, want shell command", got)
	}
}

func TestAddCommandUsesArgvForMultipleCommandArgsAndApplies(t *testing.T) {
	originalEnsure := ensureSystemdBaseline
	originalApply := runSystemctlApply
	t.Cleanup(func() {
		ensureSystemdBaseline = originalEnsure
		runSystemctlApply = originalApply
	})
	ensureSystemdBaseline = func() error { return nil }
	var applyCalls int
	runSystemctlApply = func(_ context.Context, loaded *config.File) (applyReport, error) {
		applyCalls++
		if len(loaded.Jobs) != 1 {
			t.Fatalf("jobs count = %d, want 1", len(loaded.Jobs))
		}
		if got := loaded.Jobs[0].Run.Argv(); strings.Join(got, "\x00") != "/usr/bin/env\x00bash\x00-lc\x00echo hi" {
			t.Fatalf("argv = %v", got)
		}
		return applyReport{Created: []string{"/tmp/demo.service"}}, nil
	}

	cfgPath := filepath.Join(t.TempDir(), "timertab.yaml")
	stdout := &bytes.Buffer{}
	cmd := NewRootCommand()
	cmd.SetOut(stdout)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"add", "--config", cfgPath, "--when", "@daily", "--", "/usr/bin/env", "bash", "-lc", "echo hi"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if applyCalls != 1 {
		t.Fatalf("apply calls = %d, want 1", applyCalls)
	}
	if !strings.Contains(stdout.String(), "created /tmp/demo.service") {
		t.Fatalf("stdout missing apply report:\n%s", stdout.String())
	}
}

func TestRemoveCommandDeletesJobAndApplies(t *testing.T) {
	originalEnsure := ensureSystemdBaseline
	originalApply := runSystemctlApply
	t.Cleanup(func() {
		ensureSystemdBaseline = originalEnsure
		runSystemctlApply = originalApply
	})
	ensureSystemdBaseline = func() error { return nil }
	var applyCalls int
	runSystemctlApply = func(_ context.Context, loaded *config.File) (applyReport, error) {
		applyCalls++
		if len(loaded.Jobs) != 1 || loaded.Jobs[0].ID != "keep" {
			t.Fatalf("applied jobs = %#v, want only keep", loaded.Jobs)
		}
		return applyReport{Deleted: []string{"/tmp/remove.timer"}}, nil
	}

	cfgPath := filepath.Join(t.TempDir(), "timertab.yaml")
	if err := saveConfig(cfgPath, &config.File{
		Version: 1,
		Jobs: []config.Job{
			{ID: "remove", When: config.ScheduleList{"@daily"}, Run: config.ShellCommand("echo remove")},
			{ID: "keep", When: config.ScheduleList{"@hourly"}, Run: config.ShellCommand("echo keep")},
		},
	}); err != nil {
		t.Fatalf("saveConfig() error = %v", err)
	}

	stdout := &bytes.Buffer{}
	cmd := NewRootCommand()
	cmd.SetOut(stdout)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"rm", "remove", "--config", cfgPath})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if applyCalls != 1 {
		t.Fatalf("apply calls = %d, want 1", applyCalls)
	}

	loaded, err := config.LoadFromFile(cfgPath)
	if err != nil {
		t.Fatalf("LoadFromFile() error = %v", err)
	}
	if len(loaded.Jobs) != 1 || loaded.Jobs[0].ID != "keep" {
		t.Fatalf("saved jobs = %#v, want only keep", loaded.Jobs)
	}
	if !strings.Contains(stdout.String(), "deleted /tmp/remove.timer") {
		t.Fatalf("stdout missing apply report:\n%s", stdout.String())
	}
}
