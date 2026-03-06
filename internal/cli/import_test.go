package cli

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/ginden/timertab/internal/config"
)

func TestImportCommandReadsFromStdinAndProducesConfig(t *testing.T) {
	stdin := strings.Join([]string{
		"# Existing crontab",
		"MAILTO=ops@example.com",
		"SHELL=/bin/bash",
		"PATH=/usr/local/bin:/usr/bin:/bin",
		"# Tick job",
		"*/5 * * * * echo tick # inline",
		"@daily /usr/local/bin/backup%mail body",
		"@every 5m unsupported",
	}, "\n")

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	cmd := NewRootCommand()
	cmd.SetIn(strings.NewReader(stdin))
	cmd.SetOut(stdout)
	cmd.SetErr(stderr)
	cmd.SetArgs([]string{"import", "--stdin", "--stdout"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	loaded, err := config.LoadFromBytes(stdout.Bytes())
	if err != nil {
		t.Fatalf("LoadFromBytes(stdout) error = %v\nstdout:\n%s", err, stdout.String())
	}

	if loaded.Version != 1 {
		t.Fatalf("version = %d, want 1", loaded.Version)
	}
	if loaded.Schema != defaultSchemaURL {
		t.Fatalf("$schema = %q, want %q", loaded.Schema, defaultSchemaURL)
	}
	if len(loaded.Jobs) != 2 {
		t.Fatalf("jobs count = %d, want 2", len(loaded.Jobs))
	}
	if got := loaded.Jobs[0].Name; got != "Tick job" {
		t.Fatalf("jobs[0].name = %q, want %q", got, "Tick job")
	}
	if got := loaded.Jobs[0].When[0]; got != "*/5 * * * *" {
		t.Fatalf("jobs[0].when = %q, want cron schedule", got)
	}
	if got := loaded.Jobs[0].Run; got != "echo tick" {
		t.Fatalf("jobs[0].run = %q, want %q", got, "echo tick")
	}
	if got := loaded.Jobs[1].When[0]; got != "@daily" {
		t.Fatalf("jobs[1].when = %q, want %q", got, "@daily")
	}
	if got := loaded.Jobs[1].Run; got != "/usr/local/bin/backupmail body" {
		t.Fatalf("jobs[1].run = %q, want stripped percent payload", got)
	}
	for idx, job := range loaded.Jobs {
		if strings.TrimSpace(job.ID) == "" {
			t.Fatalf("jobs[%d].id is empty", idx)
		}
		if job.Env["PATH"] != "/usr/local/bin:/usr/bin:/bin" {
			t.Fatalf("jobs[%d].env PATH = %q", idx, job.Env["PATH"])
		}
		if _, ok := job.Env["MAILTO"]; ok {
			t.Fatalf("jobs[%d].env contains filtered MAILTO", idx)
		}
		if _, ok := job.Env["SHELL"]; ok {
			t.Fatalf("jobs[%d].env contains filtered SHELL", idx)
		}
	}

	allWarnings := stderr.String()
	for _, part := range []string{
		"line 2: skipped MAILTO",
		"line 3: skipped SHELL",
		"line 6: stripped inline comment",
		"line 7: stripped % payload",
		"line 8:",
		"unsupported shorthand \"@every\"",
	} {
		if !strings.Contains(allWarnings, part) {
			t.Fatalf("stderr missing warning %q, got:\n%s", part, allWarnings)
		}
	}
}

func TestImportCommandReadsFromCrontabByDefault(t *testing.T) {
	originalRunCrontabList := runCrontabList
	originalValidateTargetUserPermission := validateTargetUserPermission
	t.Cleanup(func() {
		runCrontabList = originalRunCrontabList
		validateTargetUserPermission = originalValidateTargetUserPermission
	})

	validateTargetUserPermission = func(string) error { return nil }

	var callCount int
	runCrontabList = func(_ context.Context, targetUser string) (string, error) {
		callCount++
		if targetUser != "alice" {
			t.Fatalf("targetUser = %q, want %q", targetUser, "alice")
		}
		return "0 12 * * 1 /usr/bin/date\n", nil
	}

	stdout := &bytes.Buffer{}
	cmd := NewRootCommand()
	cmd.SetOut(stdout)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"import", "--user", "alice", "--stdout"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if callCount != 1 {
		t.Fatalf("runCrontabList call count = %d, want 1", callCount)
	}

	loaded, err := config.LoadFromBytes(stdout.Bytes())
	if err != nil {
		t.Fatalf("LoadFromBytes(stdout) error = %v", err)
	}
	if len(loaded.Jobs) != 1 {
		t.Fatalf("jobs count = %d, want 1", len(loaded.Jobs))
	}
	if loaded.Jobs[0].When[0] != "0 12 * * 1" {
		t.Fatalf("job.when = %q, want %q", loaded.Jobs[0].When[0], "0 12 * * 1")
	}
	if loaded.Jobs[0].Run != "/usr/bin/date" {
		t.Fatalf("job.run = %q, want %q", loaded.Jobs[0].Run, "/usr/bin/date")
	}
}

func TestImportCrontabSkipsInvalidCronAtImportTime(t *testing.T) {
	cfg, warnings, err := importCrontab("0 ab * * * /bin/backup.sh\n")
	if err != nil {
		t.Fatalf("importCrontab() error = %v", err)
	}
	if len(cfg.Jobs) != 0 {
		t.Fatalf("jobs count = %d, want 0", len(cfg.Jobs))
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], "line 1: invalid schedule") {
		t.Fatalf("warnings = %#v, want invalid schedule warning", warnings)
	}
}
