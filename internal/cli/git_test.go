package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ginden/timertab/internal/config"
	"github.com/ginden/timertab/internal/progress"
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

	runSystemctlApply = func(_ context.Context, _ *config.File) (applyReport, error) {
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
			Run:  config.ShellCommand("echo old"),
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
	stderr := &bytes.Buffer{}
	cmd.SetErr(stderr)
	cmd.SetContext(progress.WithWriter(context.Background(), stderr))

	if err := editConfig(cmd, cfgPath, false, false, false); err != nil {
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
	if !strings.Contains(stderr.String(), "timertab: auto-committing config change\n") {
		t.Fatalf("stderr missing auto-commit progress, got:\n%s", stderr.String())
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

	runSystemctlApply = func(_ context.Context, _ *config.File) (applyReport, error) {
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

	if err := editConfig(cmd, cfgPath, false, false, true); err != nil {
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

	runSystemctlApply = func(_ context.Context, _ *config.File) (applyReport, error) {
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

	if err := editConfig(cmd, cfgPath, false, false, false); err != nil {
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

	runSystemctlApply = func(_ context.Context, _ *config.File) (applyReport, error) {
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

	if err := editConfig(cmd, cfgPath, false, false, false); err != nil {
		t.Fatalf("editConfig() error = %v", err)
	}

	if !strings.Contains(stderr.String(), "git binary is unavailable") {
		t.Fatalf("stderr missing git warning, got:\n%s", stderr.String())
	}
}

// stubGitForCommitCapture routes git calls into a recorder and returns pointers to
// the captured commit message and a call counter.
func stubGitForCommitCapture(t *testing.T) (*string, *int) {
	t.Helper()

	originalFindGitBinary := findGitBinary
	originalRunGitCommand := runGitCommand
	t.Cleanup(func() {
		findGitBinary = originalFindGitBinary
		runGitCommand = originalRunGitCommand
	})

	message := new(string)
	calls := new(int)
	findGitBinary = func(string) (string, error) { return "/usr/bin/git", nil }
	runGitCommand = func(_ context.Context, _ string, args ...string) (string, error) {
		*calls++
		switch args[0] {
		case "rev-parse", "init", "add":
			return "", nil
		case "commit":
			for idx := range args {
				if args[idx] == "-m" && idx+1 < len(args) {
					*message = args[idx+1]
				}
			}
			return "", nil
		default:
			return "", fmt.Errorf("unexpected git args: %v", args)
		}
	}

	return message, calls
}

func TestDisableCommandAutoCommitsWithJobMessage(t *testing.T) {
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

	cfgPath := filepath.Join(t.TempDir(), "timertab.yaml")
	if err := saveConfig(cfgPath, &config.File{
		Version: 1,
		Jobs: []config.Job{{
			ID:   "target",
			When: config.ScheduleList{"@daily"},
			Run:  config.ShellCommand("echo target"),
		}},
	}); err != nil {
		t.Fatalf("saveConfig() error = %v", err)
	}

	cmd := NewRootCommand()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"disable", "target", "--config", cfgPath})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	if *message != "timertab: disable job target" {
		t.Fatalf("commit message = %q, want disable job message", *message)
	}
}

func TestDisableCommandNoCommitSkipsAutoCommit(t *testing.T) {
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
	if err := saveConfig(cfgPath, &config.File{
		Version: 1,
		Jobs: []config.Job{{
			ID:   "target",
			When: config.ScheduleList{"@daily"},
			Run:  config.ShellCommand("echo target"),
		}},
	}); err != nil {
		t.Fatalf("saveConfig() error = %v", err)
	}

	cmd := NewRootCommand()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"disable", "target", "--config", cfgPath, "--no-commit"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	if *calls != 0 {
		t.Fatalf("git calls = %d, want 0 with --no-commit", *calls)
	}
}

func TestEjectCommandAutoCommitsWithJobMessage(t *testing.T) {
	originalResolveCurrentUID := resolveCurrentUID
	originalResolveSystemdUnitDir := resolveSystemdUnitDir
	t.Cleanup(func() {
		resolveCurrentUID = originalResolveCurrentUID
		resolveSystemdUnitDir = originalResolveSystemdUnitDir
	})
	resolveCurrentUID = func() (uint32, error) { return 1000, nil }
	unitDir := t.TempDir()
	resolveSystemdUnitDir = func(uint32) (string, error) { return unitDir, nil }
	message, _ := stubGitForCommitCapture(t)

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

	cmd := NewRootCommand()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"eject", "demo", "--config", cfgPath})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	if *message != "timertab: eject job demo" {
		t.Fatalf("commit message = %q, want eject job message", *message)
	}
}

func TestImportCommandAutoCommitsWithCountMessage(t *testing.T) {
	originalApply := runSystemctlApply
	originalEnsure := ensureSystemdBaseline
	originalIsTTY := importOutputIsTTY
	t.Cleanup(func() {
		runSystemctlApply = originalApply
		ensureSystemdBaseline = originalEnsure
		importOutputIsTTY = originalIsTTY
	})
	ensureSystemdBaseline = func() error { return nil }
	runSystemctlApply = func(_ context.Context, _ *config.File) (applyReport, error) {
		return applyReport{}, nil
	}
	importOutputIsTTY = func(io.Writer) bool { return true }
	message, _ := stubGitForCommitCapture(t)
	t.Setenv("EDITOR", "true")

	cfgPath := filepath.Join(t.TempDir(), "timertab.yaml")

	cmd := NewRootCommand()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetIn(strings.NewReader("30 6 * * * /usr/local/bin/backup.sh\n"))
	cmd.SetArgs([]string{"import", "--stdin", "--config", cfgPath})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	if *message != "timertab: import 1 job(s)" {
		t.Fatalf("commit message = %q, want import count message", *message)
	}
}

func TestBuildAutoCommitMessageIncludesAddRemoveEditDetails(t *testing.T) {
	before := &config.File{Version: 1, Jobs: []config.Job{
		{ID: "a", When: config.ScheduleList{"@daily"}, Run: config.ShellCommand("echo a")},
		{ID: "b", When: config.ScheduleList{"@daily"}, Run: config.ShellCommand("echo b")},
	}}
	after := &config.File{Version: 1, Jobs: []config.Job{
		{ID: "a", When: config.ScheduleList{"@daily"}, Run: config.ShellCommand("echo changed")},
		{ID: "c", When: config.ScheduleList{"@hourly"}, Run: config.ShellCommand("echo c")},
	}}

	message := buildAutoCommitMessage(before, after)
	if !strings.Contains(message, "add job c") || !strings.Contains(message, "remove job b") || !strings.Contains(message, "edit job a") {
		t.Fatalf("buildAutoCommitMessage() = %q, want add/remove/edit details", message)
	}
}
