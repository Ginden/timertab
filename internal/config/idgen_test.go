package config

import (
	"regexp"
	"strings"
	"testing"
)

func generatedID(t *testing.T, job Job) string {
	t.Helper()

	job.ID = ""
	if job.When == nil {
		job.When = ScheduleList{"@daily"}
	}
	cfg := File{Version: 1, Jobs: []Job{job}}
	if err := cfg.NormalizeIDs(); err != nil {
		t.Fatalf("NormalizeIDs() error = %v", err)
	}
	return cfg.Jobs[0].ID
}

func TestGeneratedIDDerivesFromCommand(t *testing.T) {
	digestSuffix := regexp.MustCompile(`-[0-9a-f]{6}$`)

	tests := []struct {
		name string
		run  RunCommand
		want string
	}{{
		name: "single executable by path",
		run:  ExecCommand("/opt/publish-gitea-runner-vm-disk"),
		want: "publish-gitea-runner-vm-disk",
	}, {
		name: "script extension dropped",
		run:  ShellCommand("/home/me/scripts/publish-nvidia-gpu-stats.py"),
		want: "publish-nvidia-gpu-stats",
	}, {
		name: "path arguments ignored",
		run:  ExecCommand("/home/me/bin/symlink-dotfiles", "/home/me/dotfiles/bin", "/home/me/bin"),
		want: "symlink-dotfiles",
	}, {
		name: "flags ignored",
		run:  ExecCommand("/home/me/cron/delete-stale-node-modules", "--delete"),
		want: "delete-stale-node-modules",
	}, {
		name: "subcommands kept",
		run:  ExecCommand("/usr/bin/docker", "builder", "prune", "--force", "--all"),
		want: "docker-builder-prune",
	}, {
		name: "subcommands found past flags",
		run:  ExecCommand("/usr/bin/systemctl", "--user", "restart", "foo.service"),
		want: "systemctl-restart-foo-service",
	}, {
		name: "python module entrypoint trimmed",
		run:  ExecCommand("/usr/bin/python3", "-m", "llm_usage_reporter.cli", "collect", "--provider", "all"),
		want: "llm-usage-reporter",
	}, {
		name: "interpreter script name wins",
		run:  ExecCommand("node", "/srv/app/dist/worker.js"),
		want: "worker",
	}, {
		name: "shell -c unwrapped",
		run:  ShellCommand("/bin/bash -c /opt/scripts/rotate-logs.sh"),
		want: "rotate-logs",
	}, {
		name: "wrappers unwrapped",
		run:  ShellCommand("flock -n /var/lock/x.lock timeout 300 /opt/restic-backup"),
		want: "restic-backup",
	}, {
		name: "env assignments and nice unwrapped",
		run:  ExecCommand("/usr/bin/env", "TZ=UTC", "nice", "-n", "10", "/usr/local/bin/rotate-backups"),
		want: "rotate-backups",
	}, {
		name: "ssh named after remote command",
		run:  ExecCommand("/usr/bin/ssh", "runner@localhost", "-p", "20022", "sudo docker system prune -f"),
		want: "ssh-docker-system-prune",
	}, {
		name: "ssh in shell form named after remote command",
		run:  ShellCommand("ssh runner@localhost -p 20022 'sudo docker image prune -af'"),
		want: "ssh-docker-image-prune",
	}, {
		name: "ssh falls back to host",
		run:  ExecCommand("ssh", "-p", "20022", "runner@localhost"),
		want: "ssh-localhost",
	}}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := generatedID(t, Job{Run: tc.run}); got != tc.want {
				t.Fatalf("generated id = %q, want %q", got, tc.want)
			}
		})
	}

	weak := []struct {
		name string
		run  RunCommand
		base string
	}{{
		name: "bare generic tool",
		run:  ShellCommand("/usr/bin/rsync -a /src/ /dst/"),
		base: "rsync",
	}, {
		name: "generic tool without subcommand",
		run:  ExecCommand("/usr/bin/curl", "https://example.invalid/ping"),
		base: "curl",
	}}

	for _, tc := range weak {
		t.Run(tc.name, func(t *testing.T) {
			got := generatedID(t, Job{Run: tc.run})
			if !strings.HasPrefix(got, tc.base+"-") || !digestSuffix.MatchString(got) {
				t.Fatalf("generated id = %q, want %q plus a digest suffix", got, tc.base)
			}
		})
	}
}

func TestGeneratedIDFallsBackToDigestForShellSyntax(t *testing.T) {
	tests := []RunCommand{
		ShellCommand(`find "$HOME/.cache/npm-ci" -mindepth 1 -type d -exec rm -rf {} +`),
		ShellCommand("(mountpoint /mnt/backup && cp /a /b)"),
		ShellCommand("/opt/collect | /opt/publish"),
	}

	for _, run := range tests {
		t.Run(run.Display(), func(t *testing.T) {
			got := generatedID(t, Job{Run: run})
			if !regexp.MustCompile(`^job-[0-9a-f]{12}$`).MatchString(got) {
				t.Fatalf("generated id = %q, want a digest fallback", got)
			}
		})
	}
}

func TestGeneratedIDPrefersNameOverCommand(t *testing.T) {
	job := Job{Name: "Prune Docker build cache", Run: ExecCommand("/usr/bin/docker", "builder", "prune")}
	if got := generatedID(t, job); got != "prune-docker-build-cache" {
		t.Fatalf("generated id = %q, want name-derived id", got)
	}
}

func TestGeneratedIDsDisambiguateWithDigestNotPosition(t *testing.T) {
	cfg := File{
		Version: 1,
		Jobs: []Job{
			{When: ScheduleList{"@daily"}, Run: ExecCommand("/opt/sync-mirror", "/srv/alpha")},
			{When: ScheduleList{"@daily"}, Run: ExecCommand("/opt/sync-mirror", "/srv/beta")},
		},
	}
	if err := cfg.NormalizeIDs(); err != nil {
		t.Fatalf("NormalizeIDs() error = %v", err)
	}

	first, second := cfg.Jobs[0].ID, cfg.Jobs[1].ID
	digestSuffix := regexp.MustCompile(`^sync-mirror-[0-9a-f]{6}$`)
	if !digestSuffix.MatchString(first) || !digestSuffix.MatchString(second) {
		t.Fatalf("ids = %q, %q; want both disambiguated by digest", first, second)
	}

	// Reordering the jobs must not shuffle which identifier belongs to which job.
	swapped := File{Version: 1, Jobs: []Job{cfg.Jobs[1], cfg.Jobs[0]}}
	swapped.Jobs[0].ID = ""
	swapped.Jobs[1].ID = ""
	if err := swapped.NormalizeIDs(); err != nil {
		t.Fatalf("NormalizeIDs() error = %v", err)
	}
	if swapped.Jobs[0].ID != second || swapped.Jobs[1].ID != first {
		t.Fatalf("swapped ids = %q, %q; want %q, %q", swapped.Jobs[0].ID, swapped.Jobs[1].ID, second, first)
	}
}

func TestNormalizeIDsDoesNotStealAnExplicitIDFromALaterJob(t *testing.T) {
	cfg := File{
		Version: 1,
		Jobs: []Job{
			{Name: "Nightly backup", When: ScheduleList{"@daily"}, Run: ExecCommand("/opt/backup-a")},
			{ID: "nightly-backup", When: ScheduleList{"@daily"}, Run: ExecCommand("/opt/backup-b")},
		},
	}
	if err := cfg.NormalizeIDs(); err != nil {
		t.Fatalf("NormalizeIDs() error = %v", err)
	}
	if cfg.Jobs[0].ID == "nightly-backup" {
		t.Fatalf("generated id collided with the explicit id of a later job")
	}
}

func TestGeneratedIDIsValid(t *testing.T) {
	runs := []RunCommand{
		ExecCommand("/opt/" + strings.Repeat("very-long-name-", 8)),
		ExecCommand("/usr/bin/ssh", strings.Repeat("host.", 20)+"example", "true"),
		ShellCommand("/opt/ünïcödé-job"),
		ExecCommand("/opt/1234"),
	}

	for _, run := range runs {
		t.Run(run.Display(), func(t *testing.T) {
			got := generatedID(t, Job{Run: run})
			if !validID.MatchString(got) {
				t.Fatalf("generated id %q does not match %s", got, validID.String())
			}
		})
	}
}
