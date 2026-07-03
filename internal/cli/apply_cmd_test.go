package cli

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ginden/timertab/internal/config"
)

func TestApplyCommandRunsReconcilePipeline(t *testing.T) {
	originalApply := runSystemctlApply
	originalEnsure := ensureSystemdBaseline
	t.Cleanup(func() {
		runSystemctlApply = originalApply
		ensureSystemdBaseline = originalEnsure
	})

	var ensureCalls, applyCalls int
	ensureSystemdBaseline = func() error {
		ensureCalls++
		return nil
	}
	runSystemctlApply = func(_ context.Context, loaded *config.File) (applyReport, error) {
		applyCalls++
		if len(loaded.Jobs) != 1 || loaded.Jobs[0].ID != "demo" {
			t.Fatalf("apply received unexpected config: %#v", loaded.Jobs)
		}
		return applyReport{Created: []string{"/tmp/unit.service"}}, nil
	}

	cfgPath := filepath.Join(t.TempDir(), "timertab.yaml")
	if err := saveConfig(cfgPath, &config.File{
		Version: 1,
		Jobs: []config.Job{{
			ID:   "demo",
			When: config.ScheduleList{"@daily"},
			Run:  config.ShellCommand("echo demo"),
		}},
	}); err != nil {
		t.Fatalf("saveConfig() error = %v", err)
	}
	before, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}

	cmd := NewRootCommand()
	stdout := &bytes.Buffer{}
	cmd.SetOut(stdout)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"apply", "--config", cfgPath})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	if ensureCalls != 1 || applyCalls != 1 {
		t.Fatalf("ensure/apply calls = %d/%d, want 1/1", ensureCalls, applyCalls)
	}
	if !strings.Contains(stdout.String(), "timertab: applied "+cfgPath) {
		t.Fatalf("stdout missing applied line:\n%s", stdout.String())
	}
	if !strings.Contains(stdout.String(), "created /tmp/unit.service") {
		t.Fatalf("stdout missing apply report:\n%s", stdout.String())
	}

	after, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if !bytes.Equal(before, after) {
		t.Fatalf("apply rewrote an already-normalized config")
	}
}

func TestApplyCommandFailsFastOnInvalidConfig(t *testing.T) {
	originalApply := runSystemctlApply
	originalEnsure := ensureSystemdBaseline
	t.Cleanup(func() {
		runSystemctlApply = originalApply
		ensureSystemdBaseline = originalEnsure
	})
	ensureSystemdBaseline = func() error {
		t.Fatalf("baseline check should not run for invalid config")
		return nil
	}
	runSystemctlApply = func(_ context.Context, _ *config.File) (applyReport, error) {
		return applyReport{}, errors.New("apply should not run")
	}

	cfgPath := filepath.Join(t.TempDir(), "timertab.yaml")
	if err := os.WriteFile(cfgPath, []byte("version: 1\njobs:\n  - run: echo x\n"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	cmd := NewRootCommand()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"apply", "--config", cfgPath})

	err := cmd.Execute()
	if err == nil {
		t.Fatalf("Execute() error = nil, want validation failure")
	}
	if !strings.Contains(err.Error(), "config is invalid") {
		t.Fatalf("error = %q, want config-is-invalid message", err.Error())
	}
}

func TestApplyCommandPersistsGeneratedIDsAndAutoCommits(t *testing.T) {
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
	message, _ := stubGitForCommitCapture(t)

	input := `# keep this comment
version: 1
jobs:
  - name: "demo job"
    when: "@daily"
    run: echo demo
`
	cfgPath := filepath.Join(t.TempDir(), "timertab.yaml")
	if err := os.WriteFile(cfgPath, []byte(input), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	cmd := NewRootCommand()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"apply", "--config", cfgPath})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	after, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	saved := string(after)
	if !strings.Contains(saved, "# keep this comment") {
		t.Fatalf("comment lost during id injection:\n%s", saved)
	}
	if !strings.Contains(saved, "id:") {
		t.Fatalf("generated id not persisted:\n%s", saved)
	}
	if *message == "" {
		t.Fatalf("expected auto-commit for persisted id injection")
	}
}

func TestApplyCommandNoCommitSkipsAutoCommit(t *testing.T) {
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
	_, calls := stubGitForCommitCapture(t)

	cfgPath := filepath.Join(t.TempDir(), "timertab.yaml")
	input := "version: 1\njobs:\n  - name: demo\n    when: \"@daily\"\n    run: echo demo\n"
	if err := os.WriteFile(cfgPath, []byte(input), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	cmd := NewRootCommand()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"apply", "--config", cfgPath, "--no-commit"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	if *calls != 0 {
		t.Fatalf("git calls = %d, want 0 with --no-commit", *calls)
	}
}

func TestApplyCommandErrorsWhenConfigMissing(t *testing.T) {
	originalApply := runSystemctlApply
	t.Cleanup(func() { runSystemctlApply = originalApply })
	runSystemctlApply = func(_ context.Context, _ *config.File) (applyReport, error) {
		return applyReport{}, errors.New("apply should not run")
	}

	cmd := NewRootCommand()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"apply", "--config", filepath.Join(t.TempDir(), "missing.yaml")})

	if err := cmd.Execute(); err == nil {
		t.Fatalf("Execute() error = nil, want missing-file error")
	}
}
