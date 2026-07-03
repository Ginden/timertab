package cli

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ginden/timertab/internal/config"
)

func TestValidateCommandUsesResolvedConfigPathByDefault(t *testing.T) {
	originalResolveConfigPath := resolveConfigPath
	t.Cleanup(func() { resolveConfigPath = originalResolveConfigPath })

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

	resolveConfigPath = func(override string) (string, error) {
		if override != "" {
			t.Fatalf("override = %q, want empty default", override)
		}
		return cfgPath, nil
	}

	stdout := &bytes.Buffer{}
	cmd := NewRootCommand()
	cmd.SetOut(stdout)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"validate"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if got := strings.TrimSpace(stdout.String()); got != "ok" {
		t.Fatalf("stdout = %q, want ok", stdout.String())
	}
}
