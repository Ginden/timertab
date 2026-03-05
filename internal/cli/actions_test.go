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
	runSystemctlApply = func(_ context.Context, loaded *config.File, targetUser string) error {
		callCount++
		if loaded == nil {
			t.Fatalf("loaded config = nil")
		}
		if targetUser != "" {
			t.Fatalf("targetUser = %q, want empty", targetUser)
		}
		return nil
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

func TestEditConfigApplyReturnsSystemctlPipelineErrors(t *testing.T) {
	originalApply := runSystemctlApply
	t.Cleanup(func() {
		runSystemctlApply = originalApply
	})

	pipelineErr := errors.New("systemctl apply failed")
	runSystemctlApply = func(_ context.Context, _ *config.File, _ string) error {
		return pipelineErr
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

	runSystemctlApply = func(_ context.Context, _ *config.File, _ string) error {
		return errors.New("systemctl pipeline should not run for --no-apply")
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
	runSystemctlApply = func(_ context.Context, _ *config.File, _ string) error {
		applyCallCount++

		saved, err := os.ReadFile(cfgPath)
		if err != nil {
			t.Fatalf("ReadFile(%q) error = %v", cfgPath, err)
		}
		if !bytes.Contains(saved, []byte("id: sample-job")) {
			t.Fatalf("saved config missing normalized id before apply:\n%s", saved)
		}
		return nil
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
	runSystemctlApply = func(_ context.Context, _ *config.File, _ string) error {
		applyCallCount++
		return nil
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
