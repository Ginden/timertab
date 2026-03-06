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
	"github.com/spf13/cobra"
)

func TestEditConfigApplyRunsSystemctlPipeline(t *testing.T) {
	originalApply := runSystemctlApply
	t.Cleanup(func() {
		runSystemctlApply = originalApply
	})

	var callCount int
	runSystemctlApply = func(_ context.Context, loaded *config.File, targetUser string) (applyReport, error) {
		callCount++
		if loaded == nil {
			t.Fatalf("loaded config = nil")
		}
		if targetUser != "" {
			t.Fatalf("targetUser = %q, want empty", targetUser)
		}
		return applyReport{}, nil
	}

	t.Setenv("EDITOR", "true")

	cmd := &cobra.Command{}
	cmd.SetIn(bytes.NewBuffer(nil))
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})

	cfgPath := filepath.Join(t.TempDir(), "timertab.yaml")
	if err := editConfig(cmd, cfgPath, "", false); err != nil {
		t.Fatalf("editConfig() error = %v, want nil", err)
	}
	if callCount != 1 {
		t.Fatalf("runSystemctlApply call count = %d, want 1", callCount)
	}
}

func TestEditConfigApplyPrintsChangedOperationsOnly(t *testing.T) {
	originalApply := runSystemctlApply
	t.Cleanup(func() {
		runSystemctlApply = originalApply
	})

	runSystemctlApply = func(_ context.Context, _ *config.File, _ string) (applyReport, error) {
		return applyReport{
			Created:        []string{"/units/a.service"},
			Modified:       []string{"/units/b.service"},
			Deleted:        []string{"/units/c.timer"},
			ReloadedDaemon: true,
			DisabledTimers: []string{"old.timer"},
			StoppedTimers:  []string{"old.timer"},
			EnabledTimers:  []string{"new.timer"},
			StartedTimers:  []string{"new.timer"},
			Warnings:       []string{"warning: lingering is not enabled"},
		}, nil
	}

	t.Setenv("EDITOR", "true")

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	cmd := &cobra.Command{}
	cmd.SetIn(bytes.NewBuffer(nil))
	cmd.SetOut(stdout)
	cmd.SetErr(stderr)

	cfgPath := filepath.Join(t.TempDir(), "timertab.yaml")
	if err := editConfig(cmd, cfgPath, "", false); err != nil {
		t.Fatalf("editConfig() error = %v, want nil", err)
	}

	output := stdout.String()
	if !strings.Contains(output, "created /units/a.service\n") {
		t.Fatalf("stdout missing created entry, got:\n%s", output)
	}
	if !strings.Contains(output, "modified /units/b.service\n") {
		t.Fatalf("stdout missing modified entry, got:\n%s", output)
	}
	if !strings.Contains(output, "deleted /units/c.timer\n") {
		t.Fatalf("stdout missing deleted entry, got:\n%s", output)
	}
	if !strings.Contains(output, "disabled old.timer\n") {
		t.Fatalf("stdout missing disabled entry, got:\n%s", output)
	}
	if !strings.Contains(output, "stopped old.timer\n") {
		t.Fatalf("stdout missing stopped entry, got:\n%s", output)
	}
	if !strings.Contains(output, "reloaded systemd --user daemon\n") {
		t.Fatalf("stdout missing daemon-reload entry, got:\n%s", output)
	}
	if !strings.Contains(output, "enabled new.timer\n") {
		t.Fatalf("stdout missing enabled entry, got:\n%s", output)
	}
	if !strings.Contains(output, "started new.timer\n") {
		t.Fatalf("stdout missing started entry, got:\n%s", output)
	}
	if strings.Contains(output, "applied systemd reconcile") {
		t.Fatalf("stdout should not include generic apply line, got:\n%s", output)
	}
	if !strings.Contains(stderr.String(), "warning: lingering is not enabled\n") {
		t.Fatalf("stderr missing warning line, got:\n%s", stderr.String())
	}
}

func TestEditConfigApplyReturnsSystemctlPipelineErrors(t *testing.T) {
	originalApply := runSystemctlApply
	t.Cleanup(func() {
		runSystemctlApply = originalApply
	})

	pipelineErr := errors.New("systemctl apply failed")
	runSystemctlApply = func(_ context.Context, _ *config.File, _ string) (applyReport, error) {
		return applyReport{}, pipelineErr
	}

	t.Setenv("EDITOR", "true")

	cmd := &cobra.Command{}
	cmd.SetIn(bytes.NewBuffer(nil))
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})

	cfgPath := filepath.Join(t.TempDir(), "timertab.yaml")
	err := editConfig(cmd, cfgPath, "", false)
	if !errors.Is(err, pipelineErr) {
		t.Fatalf("editConfig() error = %v, want %v", err, pipelineErr)
	}
}

func TestEditConfigNoApplySkipsSystemctlPipeline(t *testing.T) {
	originalApply := runSystemctlApply
	t.Cleanup(func() {
		runSystemctlApply = originalApply
	})

	runSystemctlApply = func(_ context.Context, _ *config.File, _ string) (applyReport, error) {
		return applyReport{}, errors.New("systemctl pipeline should not run for --no-apply")
	}

	t.Setenv("EDITOR", "true")

	cmd := &cobra.Command{}
	cmd.SetIn(bytes.NewBuffer(nil))
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})

	cfgPath := filepath.Join(t.TempDir(), "timertab.yaml")
	if err := editConfig(cmd, cfgPath, "", true); err != nil {
		t.Fatalf("editConfig() error = %v, want nil", err)
	}
}

func TestEditConfigInvalidThenValidReopensEditorAndAppliesOnce(t *testing.T) {
	originalApply := runSystemctlApply
	t.Cleanup(func() {
		runSystemctlApply = originalApply
	})

	cfgPath := filepath.Join(t.TempDir(), "timertab.yaml")
	initial := []byte(`version: 1
jobs:
  - id: existing
    when: "@daily"
    run: "echo old"
`)
	if err := os.WriteFile(cfgPath, initial, 0o644); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", cfgPath, err)
	}

	var applyCallCount int
	runSystemctlApply = func(_ context.Context, _ *config.File, _ string) (applyReport, error) {
		applyCallCount++

		saved, err := os.ReadFile(cfgPath)
		if err != nil {
			t.Fatalf("ReadFile(%q) error = %v", cfgPath, err)
		}
		if !bytes.Contains(saved, []byte("id: sample-job")) {
			t.Fatalf("saved config missing normalized id before apply:\n%s", saved)
		}
		return applyReport{}, nil
	}

	countPath := filepath.Join(t.TempDir(), "editor-count")
	t.Setenv("TIMERTAB_EDITOR_COUNT_FILE", countPath)
	t.Setenv("TIMERTAB_CFG_PATH", cfgPath)
	t.Setenv("EDITOR", writeEditorScript(t, `
count_file="$TIMERTAB_EDITOR_COUNT_FILE"
cfg_path="$TIMERTAB_CFG_PATH"
count=0
if [ -f "$count_file" ]; then
	count=$(cat "$count_file")
fi
count=$((count + 1))
printf "%s" "$count" > "$count_file"

if [ "$count" -eq 1 ]; then
	cat > "$1" <<'EOF'
version: 1
jobs:
  - name: sample job
    when: "@daily"
EOF
	exit 0
fi

if ! grep -Fq 'run: "echo old"' "$cfg_path"; then
	echo "config mutated before valid save" >&2
	exit 91
fi

cat > "$1" <<'EOF'
version: 1
jobs:
  - name: sample job
    when: "@daily"
    run: "echo new"
EOF
`))

	cmd := &cobra.Command{}
	cmd.SetIn(bytes.NewBuffer(nil))
	cmd.SetOut(&bytes.Buffer{})
	stderr := &bytes.Buffer{}
	cmd.SetErr(stderr)

	if err := editConfig(cmd, cfgPath, "", false); err != nil {
		t.Fatalf("editConfig() error = %v, want nil", err)
	}

	if applyCallCount != 1 {
		t.Fatalf("runSystemctlApply call count = %d, want 1", applyCallCount)
	}

	editorRunsRaw, err := os.ReadFile(countPath)
	if err != nil {
		t.Fatalf("ReadFile(%q) error = %v", countPath, err)
	}
	if got := strings.TrimSpace(string(editorRunsRaw)); got != "2" {
		t.Fatalf("editor run count = %q, want %q", got, "2")
	}

	if got := stderr.String(); !strings.Contains(got, "timertab: config is invalid:") {
		t.Fatalf("stderr missing validation error, got:\n%s", got)
	}
	if got := stderr.String(); !strings.Contains(got, "timertab: reopen editor to fix validation errors") {
		t.Fatalf("stderr missing re-edit prompt, got:\n%s", got)
	}

	saved, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("ReadFile(%q) error = %v", cfgPath, err)
	}
	if !bytes.Contains(saved, []byte("id: sample-job")) {
		t.Fatalf("saved config missing normalized id:\n%s", saved)
	}
	if !bytes.Contains(saved, []byte("run: echo new")) {
		t.Fatalf("saved config missing valid edited job:\n%s", saved)
	}
}

func TestEditConfigInvalidConfigAbortDoesNotMutateOrApply(t *testing.T) {
	originalApply := runSystemctlApply
	t.Cleanup(func() {
		runSystemctlApply = originalApply
	})

	cfgPath := filepath.Join(t.TempDir(), "timertab.yaml")
	initial := []byte(`version: 1
jobs:
  - id: existing
    when: "@daily"
    run: "echo old"
`)
	if err := os.WriteFile(cfgPath, initial, 0o644); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", cfgPath, err)
	}

	var applyCallCount int
	runSystemctlApply = func(_ context.Context, _ *config.File, _ string) (applyReport, error) {
		applyCallCount++
		return applyReport{}, nil
	}

	countPath := filepath.Join(t.TempDir(), "editor-count")
	t.Setenv("TIMERTAB_EDITOR_COUNT_FILE", countPath)
	t.Setenv("TIMERTAB_CFG_PATH", cfgPath)
	t.Setenv("EDITOR", writeEditorScript(t, `
count_file="$TIMERTAB_EDITOR_COUNT_FILE"
cfg_path="$TIMERTAB_CFG_PATH"
count=0
if [ -f "$count_file" ]; then
	count=$(cat "$count_file")
fi
count=$((count + 1))
printf "%s" "$count" > "$count_file"

if [ "$count" -eq 1 ]; then
	cat > "$1" <<'EOF'
version: 1
jobs:
  - name: sample job
    when: "@daily"
EOF
	exit 0
fi

if ! grep -Fq 'run: "echo old"' "$cfg_path"; then
	echo "config mutated before valid save" >&2
	exit 92
fi

exit 1
`))

	cmd := &cobra.Command{}
	cmd.SetIn(bytes.NewBuffer(nil))
	cmd.SetOut(&bytes.Buffer{})
	stderr := &bytes.Buffer{}
	cmd.SetErr(stderr)

	err := editConfig(cmd, cfgPath, "", false)
	if err == nil {
		t.Fatalf("editConfig() error = nil, want non-nil")
	}
	if !strings.Contains(err.Error(), "editor failed:") {
		t.Fatalf("editConfig() error = %v, want editor failure", err)
	}
	if applyCallCount != 0 {
		t.Fatalf("runSystemctlApply call count = %d, want 0", applyCallCount)
	}

	editorRunsRaw, readErr := os.ReadFile(countPath)
	if readErr != nil {
		t.Fatalf("ReadFile(%q) error = %v", countPath, readErr)
	}
	if got := strings.TrimSpace(string(editorRunsRaw)); got != "2" {
		t.Fatalf("editor run count = %q, want %q", got, "2")
	}

	if got := stderr.String(); !strings.Contains(got, "timertab: config is invalid:") {
		t.Fatalf("stderr missing validation error, got:\n%s", got)
	}

	saved, readErr := os.ReadFile(cfgPath)
	if readErr != nil {
		t.Fatalf("ReadFile(%q) error = %v", cfgPath, readErr)
	}
	if !bytes.Equal(saved, initial) {
		t.Fatalf("config mutated while invalid; got:\n%s\nwant:\n%s", saved, initial)
	}
}

func writeEditorScript(t *testing.T, body string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "editor.sh")
	script := "#!/bin/sh\nset -eu\n" + body + "\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", path, err)
	}
	return path
}
