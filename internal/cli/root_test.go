package cli

import (
	"bytes"
	"errors"
	"path/filepath"
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
