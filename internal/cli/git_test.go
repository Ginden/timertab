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
	"github.com/spf13/cobra"
)

func TestEditConfigApplyAutoCommitsChangedConfig(t *testing.T) {
	originalApply := runSystemctlApply
	originalFindGitBinary := findGitBinary
	originalRunGitCommand := runGitCommand
	t.Cleanup(func() {
		runSystemctlApply = originalApply
		findGitBinary = originalFindGitBinary
		runGitCommand = originalRunGitCommand
	})

	runSystemctlApply = func(_ context.Context, _ *config.File, _ string) (applyReport, error) {
		return applyReport{}, nil
	}
	findGitBinary = func(string) (string, error) {
		return "/usr/bin/git", nil
	}

	var gitCalls [][]string
	var commitMessage string
	runGitCommand = func(_ context.Context, _ string, args ...string) (string, error) {
		gitCalls = append(gitCalls, append([]string(nil), args...))
		switch args[0] {
		case "rev-parse":
			return "", errors.New("not a repository")
		case "init", "add":
			return "", nil
		case "commit":
			for idx := range args {
				if args[idx] == "-m" && idx+1 < len(args) {
					commitMessage = args[idx+1]
				}
			}
			return "", nil
		default:
			return "", fmt.Errorf("unexpected git args: %v", args)
		}
	}

	cfgPath := filepath.Join(t.TempDir(), "timertab.yaml")
	if err := saveConfig(cfgPath, &config.File{
		Version: 1,
		Jobs: []config.Job{{
			ID:   "existing",
			When: config.ScheduleList{"@daily"},
			Run:  "echo old",
		}},
	}); err != nil {
		t.Fatalf("saveConfig() error = %v", err)
	}

	t.Setenv("EDITOR", writeEditorScript(t, `
cat > "$1" <<'EOF'
version: 1
jobs:
  - id: existing
    when: "@daily"
    run: "echo new"
  - id: added
    when: "@hourly"
    run: "echo added"
EOF
`))

	cmd := &cobra.Command{}
	cmd.SetIn(bytes.NewBuffer(nil))
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})

	if err := editConfig(cmd, cfgPath, "", false, false, false); err != nil {
		t.Fatalf("editConfig() error = %v", err)
	}

	if len(gitCalls) == 0 {
		t.Fatalf("expected git commands to be invoked")
	}
	if got := gitCalls[0][0]; got != "rev-parse" {
		t.Fatalf("first git command = %q, want rev-parse", got)
	}
	if got := gitCalls[1][0]; got != "init" {
		t.Fatalf("second git command = %q, want init", got)
	}
	if got := gitCalls[2][0]; got != "add" {
		t.Fatalf("third git command = %q, want add", got)
	}
	if got := gitCalls[3][0]; got != "commit" {
		t.Fatalf("fourth git command = %q, want commit", got)
	}

	if !strings.Contains(commitMessage, "add job added") || !strings.Contains(commitMessage, "edit job existing") {
		t.Fatalf("commit message = %q, want add/edit job details", commitMessage)
	}
}

func TestEditConfigNoCommitSkipsAutoCommit(t *testing.T) {
	originalApply := runSystemctlApply
	originalFindGitBinary := findGitBinary
	originalRunGitCommand := runGitCommand
	t.Cleanup(func() {
		runSystemctlApply = originalApply
		findGitBinary = originalFindGitBinary
		runGitCommand = originalRunGitCommand
	})

	runSystemctlApply = func(_ context.Context, _ *config.File, _ string) (applyReport, error) {
		return applyReport{}, nil
	}

	called := false
	findGitBinary = func(string) (string, error) {
		called = true
		return "/usr/bin/git", nil
	}
	runGitCommand = func(_ context.Context, _ string, _ ...string) (string, error) {
		called = true
		return "", nil
	}

	cfgPath := filepath.Join(t.TempDir(), "timertab.yaml")
	t.Setenv("EDITOR", "true")

	cmd := &cobra.Command{}
	cmd.SetIn(bytes.NewBuffer(nil))
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})

	if err := editConfig(cmd, cfgPath, "", false, false, true); err != nil {
		t.Fatalf("editConfig() error = %v", err)
	}
	if called {
		t.Fatalf("expected git integration to be skipped with --no-commit")
	}
}

func TestEditConfigAutoCommitDisabledByConfig(t *testing.T) {
	originalApply := runSystemctlApply
	originalFindGitBinary := findGitBinary
	originalRunGitCommand := runGitCommand
	t.Cleanup(func() {
		runSystemctlApply = originalApply
		findGitBinary = originalFindGitBinary
		runGitCommand = originalRunGitCommand
	})

	runSystemctlApply = func(_ context.Context, _ *config.File, _ string) (applyReport, error) {
		return applyReport{}, nil
	}

	called := false
	findGitBinary = func(string) (string, error) {
		called = true
		return "/usr/bin/git", nil
	}
	runGitCommand = func(_ context.Context, _ string, _ ...string) (string, error) {
		called = true
		return "", nil
	}

	cfgPath := filepath.Join(t.TempDir(), "timertab.yaml")
	t.Setenv("EDITOR", writeEditorScript(t, `
cat > "$1" <<'EOF'
version: 1
git:
  auto_commit: false
jobs:
  - id: a
    when: "@daily"
    run: "echo a"
EOF
`))

	cmd := &cobra.Command{}
	cmd.SetIn(bytes.NewBuffer(nil))
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})

	if err := editConfig(cmd, cfgPath, "", false, false, false); err != nil {
		t.Fatalf("editConfig() error = %v", err)
	}
	if called {
		t.Fatalf("expected git integration to be skipped by config git.auto_commit=false")
	}
}

func TestEditConfigWarnsWhenGitIsUnavailable(t *testing.T) {
	originalApply := runSystemctlApply
	originalFindGitBinary := findGitBinary
	t.Cleanup(func() {
		runSystemctlApply = originalApply
		findGitBinary = originalFindGitBinary
	})

	runSystemctlApply = func(_ context.Context, _ *config.File, _ string) (applyReport, error) {
		return applyReport{}, nil
	}
	findGitBinary = func(string) (string, error) {
		return "", errors.New("not found")
	}

	cfgPath := filepath.Join(t.TempDir(), "timertab.yaml")
	t.Setenv("EDITOR", "true")

	stderr := &bytes.Buffer{}
	cmd := &cobra.Command{}
	cmd.SetIn(bytes.NewBuffer(nil))
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(stderr)

	if err := editConfig(cmd, cfgPath, "", false, false, false); err != nil {
		t.Fatalf("editConfig() error = %v", err)
	}

	if !strings.Contains(stderr.String(), "git binary is unavailable") {
		t.Fatalf("stderr missing git warning, got:\n%s", stderr.String())
	}
}

func TestBuildAutoCommitMessageIncludesAddRemoveEditDetails(t *testing.T) {
	before := &config.File{Version: 1, Jobs: []config.Job{
		{ID: "a", When: config.ScheduleList{"@daily"}, Run: "echo a"},
		{ID: "b", When: config.ScheduleList{"@daily"}, Run: "echo b"},
	}}
	after := &config.File{Version: 1, Jobs: []config.Job{
		{ID: "a", When: config.ScheduleList{"@daily"}, Run: "echo changed"},
		{ID: "c", When: config.ScheduleList{"@hourly"}, Run: "echo c"},
	}}

	message := buildAutoCommitMessage(before, after)
	if !strings.Contains(message, "add job c") || !strings.Contains(message, "remove job b") || !strings.Contains(message, "edit job a") {
		t.Fatalf("buildAutoCommitMessage() = %q, want add/remove/edit details", message)
	}
}
