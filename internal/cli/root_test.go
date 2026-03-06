package cli

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRootCommandEditApplyChecksSystemdVersionOnce(t *testing.T) {
	originalCheck := ensureSystemdBaseline
	t.Cleanup(func() {
		ensureSystemdBaseline = originalCheck
	})

	var callCount int
	checkErr := errors.New("unsupported systemd")
	ensureSystemdBaseline = func() error {
		callCount++
		return checkErr
	}

	cmd := NewRootCommand()
	cmd.SetArgs([]string{"--edit", "--config", filepath.Join(t.TempDir(), "timertab.yaml")})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})

	err := cmd.Execute()
	if !errors.Is(err, checkErr) {
		t.Fatalf("Execute() error = %v, want %v", err, checkErr)
	}
	if callCount != 1 {
		t.Fatalf("systemd check call count = %d, want 1", callCount)
	}
}

func TestRootCommandEditNoApplySkipsSystemdCheck(t *testing.T) {
	originalCheck := ensureSystemdBaseline
	t.Cleanup(func() {
		ensureSystemdBaseline = originalCheck
	})

	ensureSystemdBaseline = func() error {
		return errors.New("systemd check should not run for --no-apply")
	}

	t.Setenv("EDITOR", "true")

	cmd := NewRootCommand()
	cmd.SetArgs([]string{"--edit", "--no-apply", "--config", filepath.Join(t.TempDir(), "timertab.yaml")})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() returned error for --no-apply: %v", err)
	}
}

func TestRootCommandRejectsUnauthorizedTargetUser(t *testing.T) {
	originalValidateTargetUserPermission := validateTargetUserPermission
	originalResolveConfigPath := resolveConfigPath
	t.Cleanup(func() {
		validateTargetUserPermission = originalValidateTargetUserPermission
		resolveConfigPath = originalResolveConfigPath
	})

	deniedErr := errors.New("permission denied for target user")
	validateTargetUserPermission = func(targetUser string) error {
		if targetUser != "alice" {
			t.Fatalf("targetUser = %q, want %q", targetUser, "alice")
		}
		return deniedErr
	}

	resolveCalled := false
	resolveConfigPath = func(string, string) (string, error) {
		resolveCalled = true
		return "", nil
	}

	cmd := NewRootCommand()
	cmd.SetArgs([]string{"--print-path", "--user", "alice"})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})

	err := cmd.Execute()
	if !errors.Is(err, deniedErr) {
		t.Fatalf("Execute() error = %v, want %v", err, deniedErr)
	}
	if resolveCalled {
		t.Fatalf("ResolvePath was called despite target user validation failure")
	}
}

func TestRootCommandPrintConfigAliasListsConfig(t *testing.T) {
	originalResolveConfigPath := resolveConfigPath
	t.Cleanup(func() {
		resolveConfigPath = originalResolveConfigPath
	})

	cfgPath := filepath.Join(t.TempDir(), "timertab.yaml")
	content := []byte("$schema: test\nversion: 1\njobs: []\n")
	if err := os.WriteFile(cfgPath, content, 0o644); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", cfgPath, err)
	}

	resolveConfigPath = func(string, string) (string, error) {
		return cfgPath, nil
	}

	stdout := &bytes.Buffer{}
	cmd := NewRootCommand()
	cmd.SetArgs([]string{"--print-config"})
	cmd.SetOut(stdout)
	cmd.SetErr(&bytes.Buffer{})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	out := stdout.String()
	if !strings.Contains(out, "# "+cfgPath+"\n") {
		t.Fatalf("stdout missing config header, got:\n%s", out)
	}
	if !strings.Contains(out, string(content)) {
		t.Fatalf("stdout missing config body, got:\n%s", out)
	}
}
