package cli

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func TestWriteConfigFileUsesPrivateMode(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "timertab.yaml")
	if err := os.WriteFile(cfgPath, []byte("version: 1\njobs: []\n"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	if err := writeConfigFile(cfgPath, []byte("version: 1\njobs: []\n")); err != nil {
		t.Fatalf("writeConfigFile() error = %v", err)
	}

	info, err := os.Stat(cfgPath)
	if err != nil {
		t.Fatalf("Stat() error = %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("mode = %v, want 0600", got)
	}
}

func TestWithConfigLockRejectsConcurrentOperation(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "timertab.yaml")
	lockPath := cfgPath + ".lock"
	if err := os.MkdirAll(filepath.Dir(cfgPath), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	file, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatalf("OpenFile() error = %v", err)
	}
	defer file.Close()
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		t.Fatalf("Flock() error = %v", err)
	}
	defer syscall.Flock(int(file.Fd()), syscall.LOCK_UN)

	err = withConfigLock(cfgPath, func() error {
		t.Fatalf("locked callback should not run")
		return nil
	})
	if err == nil {
		t.Fatalf("withConfigLock() error = nil, want contention")
	}
	if !strings.Contains(err.Error(), "another timertab operation is in progress") {
		t.Fatalf("withConfigLock() error = %v", err)
	}
}
