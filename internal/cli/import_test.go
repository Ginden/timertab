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
		"*/5 * * * * echo tick",
		"@daily /usr/local/bin/backup",
		"@every 5m unsupported",
	}, "\n")

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	cmd := NewRootCommand()
	cmd.SetIn(strings.NewReader(stdin))
	cmd.SetOut(stdout)
	cmd.SetErr(stderr)
	cmd.SetArgs([]string{"import", "--stdin"})

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
	if got := loaded.Jobs[0].When[0]; got != "*/5 * * * *" {
		t.Fatalf("jobs[0].when = %q, want cron schedule", got)
	}
	if got := loaded.Jobs[0].Run.Display(); got != "echo tick" {
		t.Fatalf("jobs[0].run = %q, want %q", got, "echo tick")
	}
	if got := loaded.Jobs[1].When[0]; got != "@daily" {
		t.Fatalf("jobs[1].when = %q, want %q", got, "@daily")
	}
	for idx, job := range loaded.Jobs {
		if strings.TrimSpace(job.ID) == "" {
			t.Fatalf("jobs[%d].id is empty", idx)
		}
		// MAILTO and SHELL must be filtered out — they have no systemd equivalent.
		if _, ok := job.Env["MAILTO"]; ok {
			t.Fatalf("jobs[%d].env should not contain MAILTO", idx)
		}
		if _, ok := job.Env["SHELL"]; ok {
			t.Fatalf("jobs[%d].env should not contain SHELL", idx)
		}
	}

	stderrStr := stderr.String()
	if !strings.Contains(stderrStr, "MAILTO") {
		t.Fatalf("stderr missing warning for MAILTO, got:\n%s", stderrStr)
	}
	if !strings.Contains(stderrStr, "SHELL") {
		t.Fatalf("stderr missing warning for SHELL, got:\n%s", stderrStr)
	}
	if !strings.Contains(stderrStr, "line 6:") || !strings.Contains(stderrStr, "unsupported shorthand \"@every\"") {
		t.Fatalf("stderr missing warning for unsupported entry, got:\n%s", stderrStr)
	}
}

func TestImportCommandReadsFromCrontabByDefault(t *testing.T) {
	originalRunCrontabList := runCrontabList
	t.Cleanup(func() {
		runCrontabList = originalRunCrontabList
	})

	var callCount int
	runCrontabList = func(_ context.Context) (string, error) {
		callCount++
		return "0 12 * * 1 /usr/bin/date\n", nil
	}

	stdout := &bytes.Buffer{}
	cmd := NewRootCommand()
	cmd.SetOut(stdout)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"import"})

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
	if got := loaded.Jobs[0].Run.Display(); got != "/usr/bin/date" {
		t.Fatalf("job.run = %q, want %q", got, "/usr/bin/date")
	}
}

func TestStripInlineComment(t *testing.T) {
	tests := []struct {
		input   string
		command string
		comment string
	}{
		{"/backup.sh # morning backup", "/backup.sh", "# morning backup"},
		{"/backup.sh", "/backup.sh", ""},
		{`echo "hello # world"`, `echo "hello # world"`, ""},
		{`echo 'it'\''s cool' # comment`, `echo 'it'\''s cool'`, "# comment"},
		{"/cmd --flag #value", "/cmd --flag", "#value"},
		{"/cmd\t# tab before hash", "/cmd", "# tab before hash"},
		{"echo $# args", "echo $# args", ""},
		{`echo "quoted \" # not a comment"`, `echo "quoted \" # not a comment"`, ""},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			gotCmd, gotComment := stripInlineComment(tt.input)
			if gotCmd != tt.command {
				t.Errorf("command = %q, want %q", gotCmd, tt.command)
			}
			if gotComment != tt.comment {
				t.Errorf("comment = %q, want %q", gotComment, tt.comment)
			}
		})
	}
}

func TestStripCronPercent(t *testing.T) {
	tests := []struct {
		input    string
		stripped string
		had      bool
	}{
		{"mail user@host%Body here", "mail user@host", true},
		{"/bin/backup.sh", "/bin/backup.sh", false},
		{`echo "percentage: 50\%"`, `echo "percentage: 50%"`, false},
		{"cmd arg%stdin text%more", "cmd arg", true},
		{"cmd arg %-", "cmd arg", true},
		{`echo 100\% done % stdin`, `echo 100% done`, true},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			gotStripped, gotHad := stripCronPercent(tt.input)
			if gotStripped != tt.stripped {
				t.Errorf("stripped = %q, want %q", gotStripped, tt.stripped)
			}
			if gotHad != tt.had {
				t.Errorf("had = %v, want %v", gotHad, tt.had)
			}
		})
	}
}

func TestImportCrontabEnvStripsMatchingQuotes(t *testing.T) {
	input := strings.Join([]string{
		`FOO="a b"`,
		`BAR='c d'`,
		`BAZ=unquoted`,
		`QUX="  spaced  "`,
		`0 9 * * * echo env`,
	}, "\n")

	cfg, warnings, err := importCrontab(input)
	if err != nil {
		t.Fatalf("importCrontab error = %v", err)
	}
	if len(warnings) != 0 {
		t.Fatalf("unexpected warnings: %v", warnings)
	}
	if len(cfg.Jobs) != 1 {
		t.Fatalf("jobs count = %d, want 1", len(cfg.Jobs))
	}

	wantEnv := map[string]string{
		"FOO": "a b",
		"BAR": "c d",
		"BAZ": "unquoted",
		"QUX": "  spaced  ",
	}
	for key, want := range wantEnv {
		if got := cfg.Jobs[0].Env[key]; got != want {
			t.Errorf("env[%s] = %q, want %q", key, got, want)
		}
	}
}

func TestImportCrontabMapsCRONTZToJobTimezone(t *testing.T) {
	input := strings.Join([]string{
		`CRON_TZ="America/New_York"`,
		`0 9 * * * echo east`,
		`CRON_TZ=UTC`,
		`0 10 * * * echo utc`,
	}, "\n")

	cfg, warnings, err := importCrontab(input)
	if err != nil {
		t.Fatalf("importCrontab error = %v", err)
	}
	if len(warnings) != 0 {
		t.Fatalf("unexpected warnings: %v", warnings)
	}
	if len(cfg.Jobs) != 2 {
		t.Fatalf("jobs count = %d, want 2", len(cfg.Jobs))
	}
	if got := cfg.Jobs[0].TZ; got != "America/New_York" {
		t.Fatalf("jobs[0].tz = %q, want America/New_York", got)
	}
	if got := cfg.Jobs[1].TZ; got != "UTC" {
		t.Fatalf("jobs[1].tz = %q, want UTC", got)
	}
	for idx, job := range cfg.Jobs {
		if _, ok := job.Env["CRON_TZ"]; ok {
			t.Fatalf("jobs[%d].env should not contain CRON_TZ", idx)
		}
	}
}

func TestImportCrontabWarnsForInvalidCRONTZ(t *testing.T) {
	input := strings.Join([]string{
		`CRON_TZ=No/Such_Zone`,
		`0 9 * * * echo local`,
	}, "\n")

	cfg, warnings, err := importCrontab(input)
	if err != nil {
		t.Fatalf("importCrontab error = %v", err)
	}
	if len(cfg.Jobs) != 1 {
		t.Fatalf("jobs count = %d, want 1", len(cfg.Jobs))
	}
	if cfg.Jobs[0].TZ != "" {
		t.Fatalf("jobs[0].tz = %q, want empty", cfg.Jobs[0].TZ)
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], "CRON_TZ") {
		t.Fatalf("warnings = %v, want CRON_TZ warning", warnings)
	}
}

func TestImportCrontabPercentSyntax(t *testing.T) {
	input := strings.Join([]string{
		`0 9 * * * echo 100\% done`,
		`0 10 * * * echo ok % this becomes stdin`,
	}, "\n")

	cfg, warnings, err := importCrontab(input)
	if err != nil {
		t.Fatalf("importCrontab error = %v", err)
	}
	if len(cfg.Jobs) != 2 {
		t.Fatalf("jobs count = %d, want 2", len(cfg.Jobs))
	}
	if got := cfg.Jobs[0].Run.Display(); got != `echo 100% done` {
		t.Errorf("jobs[0].run = %q, want literal percent unescaped", got)
	}
	if got := cfg.Jobs[1].Run.Display(); got != `echo ok` {
		t.Errorf("jobs[1].run = %q, want command before stdin separator", got)
	}

	percentWarnings := 0
	for _, w := range warnings {
		if strings.Contains(w, "%") {
			percentWarnings++
		}
	}
	if percentWarnings != 1 {
		t.Errorf("percent warnings = %d, want 1; warnings: %v", percentWarnings, warnings)
	}
}

func TestParseCrontabEntryPreservesCommandAfterRepeatedWhitespace(t *testing.T) {
	tests := []struct {
		name         string
		line         string
		wantSchedule string
		wantCommand  string
	}{
		{
			name:         "multiple spaces in classic entry",
			line:         "0  12 * * * /bin/date",
			wantSchedule: "0 12 * * *",
			wantCommand:  "/bin/date",
		},
		{
			name:         "tabs before command",
			line:         "0\t12\t* * *\t/usr/bin/date",
			wantSchedule: "0 12 * * *",
			wantCommand:  "/usr/bin/date",
		},
		{
			name:         "multiple spaces after shorthand",
			line:         "@daily    /usr/local/bin/backup",
			wantSchedule: "@daily",
			wantCommand:  "/usr/local/bin/backup",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotSchedule, gotCommand, ok := parseCrontabEntry(tt.line)
			if !ok {
				t.Fatalf("parseCrontabEntry(%q) = not ok, want ok", tt.line)
			}
			if gotSchedule != tt.wantSchedule {
				t.Fatalf("schedule = %q, want %q", gotSchedule, tt.wantSchedule)
			}
			if gotCommand != tt.wantCommand {
				t.Fatalf("command = %q, want %q", gotCommand, tt.wantCommand)
			}
		})
	}
}

func TestImportCrontabCommentAssociation(t *testing.T) {
	input := strings.Join([]string{
		"# Nightly database backup",
		"0 2 * * * /usr/local/bin/db-backup.sh",
		"",
		"# Weekly cleanup",
		"@weekly /usr/local/bin/cleanup.sh",
		"@daily /usr/local/bin/other.sh",
	}, "\n")

	cfg, warnings, err := importCrontab(input)
	if err != nil {
		t.Fatalf("importCrontab error = %v", err)
	}
	if len(warnings) != 0 {
		t.Fatalf("unexpected warnings: %v", warnings)
	}
	if len(cfg.Jobs) != 3 {
		t.Fatalf("jobs count = %d, want 3", len(cfg.Jobs))
	}

	// Comment "Nightly database backup" → name of first job
	if cfg.Jobs[0].Name != "Nightly database backup" {
		t.Errorf("jobs[0].name = %q, want %q", cfg.Jobs[0].Name, "Nightly database backup")
	}
	// Comment "Weekly cleanup" → name of second job
	if cfg.Jobs[1].Name != "Weekly cleanup" {
		t.Errorf("jobs[1].name = %q, want %q", cfg.Jobs[1].Name, "Weekly cleanup")
	}
	// Blank line breaks comment association; third job has no comment
	if cfg.Jobs[2].Name != "" {
		t.Errorf("jobs[2].name = %q, want empty (blank line broke association)", cfg.Jobs[2].Name)
	}
}

func TestImportCrontabInlineCommentWarning(t *testing.T) {
	input := "0 9 * * * /backup.sh # morning backup\n"

	cfg, warnings, err := importCrontab(input)
	if err != nil {
		t.Fatalf("importCrontab error = %v", err)
	}
	if len(cfg.Jobs) != 1 {
		t.Fatalf("jobs count = %d, want 1", len(cfg.Jobs))
	}
	if got := cfg.Jobs[0].Run.Display(); got != "/backup.sh" {
		t.Errorf("run = %q, want %q", got, "/backup.sh")
	}
	found := false
	for _, w := range warnings {
		if strings.Contains(w, "inline comment") && strings.Contains(w, "# morning backup") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected inline comment warning, got: %v", warnings)
	}
}

func TestImportCrontabPercentWarning(t *testing.T) {
	input := `0 9 * * * mail -s "Report" user@host%Body of email` + "\n"

	cfg, warnings, err := importCrontab(input)
	if err != nil {
		t.Fatalf("importCrontab error = %v", err)
	}
	if len(cfg.Jobs) != 1 {
		t.Fatalf("jobs count = %d, want 1", len(cfg.Jobs))
	}
	if got := cfg.Jobs[0].Run.Display(); got != `mail -s "Report" user@host` {
		t.Errorf("run = %q, want command without %%", got)
	}
	found := false
	for _, w := range warnings {
		if strings.Contains(w, "%") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected percent warning, got: %v", warnings)
	}
}

func TestImportCrontabInvalidScheduleWarning(t *testing.T) {
	input := "0 ab * * * /bin/backup.sh\n"

	cfg, warnings, err := importCrontab(input)
	if err != nil {
		t.Fatalf("importCrontab error = %v", err)
	}
	if len(cfg.Jobs) != 0 {
		t.Fatalf("jobs count = %d, want 0 (invalid schedule should be skipped)", len(cfg.Jobs))
	}
	if len(warnings) == 0 {
		t.Fatal("expected at least one warning for invalid schedule")
	}
	if !strings.Contains(warnings[0], "line 1") {
		t.Errorf("warning should contain line number, got: %q", warnings[0])
	}
}

func TestImportCrontabFiltersMATTOAndSHELL(t *testing.T) {
	input := strings.Join([]string{
		"MAILTO=ops@example.com",
		"SHELL=/bin/bash",
		"PATH=/usr/local/bin:/usr/bin",
		"*/5 * * * * echo tick",
	}, "\n")

	cfg, warnings, err := importCrontab(input)
	if err != nil {
		t.Fatalf("importCrontab error = %v", err)
	}
	if len(cfg.Jobs) != 1 {
		t.Fatalf("jobs count = %d, want 1", len(cfg.Jobs))
	}

	// PATH should be kept; MAILTO and SHELL should be dropped.
	if cfg.Jobs[0].Env["PATH"] != "/usr/local/bin:/usr/bin" {
		t.Errorf("PATH env = %q, want preserved", cfg.Jobs[0].Env["PATH"])
	}
	if _, ok := cfg.Jobs[0].Env["MAILTO"]; ok {
		t.Error("MAILTO should be filtered from env")
	}
	if _, ok := cfg.Jobs[0].Env["SHELL"]; ok {
		t.Error("SHELL should be filtered from env")
	}

	// Two warnings: one for MAILTO, one for SHELL.
	var mailtoWarn, shellWarn bool
	for _, w := range warnings {
		if strings.Contains(w, "MAILTO") {
			mailtoWarn = true
		}
		if strings.Contains(w, "SHELL") {
			shellWarn = true
		}
	}
	if !mailtoWarn {
		t.Errorf("expected warning for MAILTO, got: %v", warnings)
	}
	if !shellWarn {
		t.Errorf("expected warning for SHELL, got: %v", warnings)
	}
}

func TestImportCommandForceStdout(t *testing.T) {
	originalRunCrontabList := runCrontabList
	t.Cleanup(func() {
		runCrontabList = originalRunCrontabList
	})

	runCrontabList = func(_ context.Context) (string, error) {
		return "@daily /usr/bin/backup\n", nil
	}

	stdout := &bytes.Buffer{}
	cmd := NewRootCommand()
	cmd.SetOut(stdout)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"import", "--stdout"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	loaded, err := config.LoadFromBytes(stdout.Bytes())
	if err != nil {
		t.Fatalf("LoadFromBytes(stdout) error = %v\nstdout:\n%s", err, stdout.String())
	}
	if len(loaded.Jobs) != 1 {
		t.Fatalf("jobs count = %d, want 1", len(loaded.Jobs))
	}
}

func TestImportCommandDryRunAndNoApplyMutuallyExclusive(t *testing.T) {
	cmd := NewRootCommand()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"import", "--stdin", "--dry-run", "--no-apply"})
	cmd.SetIn(strings.NewReader("@daily /bin/true\n"))

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error combining --dry-run and --no-apply, got nil")
	}
	if !strings.Contains(err.Error(), "--dry-run") {
		t.Errorf("error = %v, want mention of --dry-run", err)
	}
}

func TestImportCommandDryRunAndStdoutMutuallyExclusive(t *testing.T) {
	cmd := NewRootCommand()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"import", "--stdin", "--dry-run", "--stdout"})
	cmd.SetIn(strings.NewReader("@daily /bin/true\n"))

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error combining --dry-run and --stdout, got nil")
	}
	if !strings.Contains(err.Error(), "--dry-run") {
		t.Errorf("error = %v, want mention of --dry-run", err)
	}
}

func TestMergeImportedJobsSkipsDuplicatesAgainstExisting(t *testing.T) {
	existing := []config.Job{
		{
			ID:   "nightly",
			Name: "Nightly backup",
			When: config.ScheduleList{"@daily"},
			Run:  config.ShellCommand("/usr/local/bin/backup"),
			Env: map[string]string{
				"PATH": "/usr/local/bin:/usr/bin",
			},
		},
	}

	imported := []config.Job{
		{
			ID:   "ignored-id",
			Name: "Same job, different label",
			When: config.ScheduleList{"@daily"},
			Run:  config.ShellCommand("/usr/local/bin/backup"),
			Env: map[string]string{
				"PATH": "/usr/local/bin:/usr/bin",
			},
		},
		{
			When: config.ScheduleList{"0 12 * * 1"},
			Run:  config.ShellCommand("/usr/bin/date"),
		},
	}

	merged := mergeImportedJobs(existing, imported)

	if merged.Added != 1 {
		t.Fatalf("Added = %d, want 1", merged.Added)
	}
	if merged.Skipped != 1 {
		t.Fatalf("Skipped = %d, want 1", merged.Skipped)
	}
	if len(merged.Jobs) != 2 {
		t.Fatalf("merged jobs = %d, want 2", len(merged.Jobs))
	}
	if len(merged.AddedJobs) != 1 {
		t.Fatalf("added jobs = %d, want 1", len(merged.AddedJobs))
	}
	if got := merged.AddedJobs[0].Run.Display(); got != "/usr/bin/date" {
		t.Fatalf("added job run = %q, want %q", got, "/usr/bin/date")
	}
}

func TestMergeImportedJobsSkipsDuplicatesInsideImportedBatch(t *testing.T) {
	imported := []config.Job{
		{
			Name: "tick-1",
			When: config.ScheduleList{"*/5 * * * *"},
			Run:  config.ShellCommand("echo tick"),
		},
		{
			Name: "tick-2",
			When: config.ScheduleList{"*/5 * * * *"},
			Run:  config.ShellCommand("echo tick"),
		},
	}

	merged := mergeImportedJobs(nil, imported)

	if merged.Added != 1 {
		t.Fatalf("Added = %d, want 1", merged.Added)
	}
	if merged.Skipped != 1 {
		t.Fatalf("Skipped = %d, want 1", merged.Skipped)
	}
	if len(merged.Jobs) != 1 {
		t.Fatalf("merged jobs = %d, want 1", len(merged.Jobs))
	}
}

func TestMergeImportedJobsTreatsShellShorthandAndExplicitShellArgvAsDuplicates(t *testing.T) {
	existing := []config.Job{{
		When: config.ScheduleList{"@daily"},
		Run:  config.ShellCommand("echo tick"),
	}}
	imported := []config.Job{{
		When: config.ScheduleList{"@daily"},
		Run:  config.ExecCommand("/bin/sh", "-lc", "echo tick"),
	}}

	merged := mergeImportedJobs(existing, imported)

	if merged.Added != 0 || merged.Skipped != 1 {
		t.Fatalf("merge result = %#v, want explicit shell argv treated as duplicate shorthand", merged)
	}
}
