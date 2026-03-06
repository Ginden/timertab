# timertab

**Cron jobs done right — one YAML file, real systemd timers under the hood.**

If you've ever wished `crontab -e` gave you systemd timers instead of 1970s cron, this is that tool. You write a simple YAML config, `timertab` turns it into proper `.service` and `.timer` units. No daemon, no lock-in — just plain systemd.

## Why not just use crontab?

Systemd timers are better than cron in almost every way: structured logging through journald, resource controls, dependency ordering, per-user isolation. But managing them by hand means writing two unit files per job and juggling `systemctl enable`, `daemon-reload`, and cleanup yourself.

`timertab` takes care of all of that. You get the simplicity of crontab with the power of systemd timers.

## Why not just write unit files?

You absolutely can — and `timertab` won't stop you. In fact, that's the point: the generated units are standard systemd, human-readable, and work without `timertab` installed. If you ever want to stop using this tool, run `timertab eject <id>` and your timer keeps running on its own.

## Features

- **One YAML file** — all your scheduled jobs in one place, version-control friendly.
- **Success and failure hooks** — run a command when a job succeeds or fails (send a notification, dump logs, trigger another script).
- **Cron syntax you already know** — `@hourly`, `@daily`, or standard 5-field cron expressions.
- **Multiple schedules per job** — `when` accepts a list, so one job can fire at different times.
- **Zero lock-in** — eject any job and it keeps running as a standalone systemd timer.
- **Per-user isolation** — units are scoped to your UID; `timertab` never touches units it didn't create.
- **Raw systemd overrides when needed** — set extra `[Service]` / `[Timer]` directives per job.
- **JSON Schema** — get autocomplete and validation in editors that support it.
- **Safe reconcile** — if your config is invalid, nothing gets written or pruned. No partial state.

## Quick start

### Install

```bash
go install github.com/ginden/timertab/cmd/timertab@latest
```

Or from a local clone:

```bash
make install
```

### Create your first job

```bash
timertab -e
```

This opens your `$EDITOR` with the config file. Add a job:

```yaml
$schema: "https://raw.githubusercontent.com/ginden/timertab/v1.0.0/schema/v1.json"
version: 1
jobs:
  - name: clean temp files
    when: "@daily"
    run: "find /tmp -user $USER -mtime +7 -delete"
```

Save and close — `timertab` validates the config, generates the systemd units, and starts the timer. That's it.

### A more complete example

```yaml
$schema: "https://raw.githubusercontent.com/ginden/timertab/v1.0.0/schema/v1.json"
version: 1
jobs:
  - name: NPM cache verify
    when: "@hourly"
    run: "npm --global cache verify"
    env:
      NPM_CONFIG_PREFIX: "/home/user/.npm-global"
    on_success:
      command: "echo ok"
    on_failure:
      command: 'journalctl -u "$TIMERTAB_UNIT" -n 100 --no-pager'

  - name: backup documents
    when:
      - "0 9 * * *"
      - "0 18 * * *"
    run: "rsync -a ~/Documents /mnt/backup/"
    cwd: "/home/user"
    systemd:
      service:
        Restart: "on-failure"
        RestartSec: "30s"
      timer:
        AccuracySec: "1m"
    on_failure:
      command: 'notify-send "Backup failed"'
```

## Usage

| Command | What it does |
|---|---|
| `timertab edit` (or `timertab -e`) | Edit config, validate, and apply (generate and start timers) |
| `timertab edit --no-apply` (or `timertab -e --no-apply`) | Edit and validate only, don't touch systemd |
| `timertab edit --no-commit` | Apply without creating/updating the git history entry for that edit run |
| `timertab list` / `timertab print-config` (or `timertab -l`) | Print current config |
| `timertab status` / `timertab status --json` | Show last run, next trigger, and result for each job |
| `timertab add` (or `+1`) | Append a single new job through your editor |
| `timertab eject <id>` | Stop managing a job — its units stay and keep running |
| `timertab enable <id>` / `timertab disable <id>` | Toggle one job on/off without removing it |
| `timertab logs <id>` | Tail/query journald logs for one job |
| `timertab diff` | Preview create/modify/delete reconcile operations |
| `timertab import` | Convert crontab entries into timertab YAML |
| `timertab validate --config <path>` | Validate a config file without applying |
| `timertab print-path` (or `timertab --print-path`) | Show where the config file lives |
| `timertab <command> -u <user>` | Operate on another user's timers (requires privileges) |

Config file location: `${XDG_CONFIG_HOME:-$HOME/.config}/timertab/timertab.yaml`

### Shell Completions

Generate completions with `timertab completion <shell>`.

**bash**

```bash
mkdir -p ~/.local/share/bash-completion/completions
timertab completion bash > ~/.local/share/bash-completion/completions/timertab
```

**zsh**

```bash
mkdir -p ~/.zfunc
timertab completion zsh > ~/.zfunc/_timertab
```

Then make sure `~/.zfunc` is on your `fpath`.

**fish**

```bash
mkdir -p ~/.config/fish/completions
timertab completion fish > ~/.config/fish/completions/timertab.fish
```

## Requirements

- Linux with **systemd ≥ 247**
- **Go 1.24+** (build-time only)
- A running user session (`systemctl --user` must work)

If you need timers to fire while you're logged out:

```bash
loginctl enable-linger "$USER"
```

## How it works

When you run `timertab edit` (or `timertab -e`), here's what happens:

1. Your editor opens the YAML config.
2. On save, `timertab` validates the config against the schema and semantic rules.
3. Missing job `id` fields are auto-generated and persisted back to the file.
4. For each job, a `.service` and `.timer` unit is rendered.
5. Stale units (from removed jobs) are stopped, disabled, and deleted.
6. New/changed units are written, `daemon-reload` is called, and timers are started.

If validation fails at step 2, nothing else happens — no partial writes, no orphaned units.

Successful `timertab edit` apply runs also auto-commit the config file by default. If the config directory is not already inside a git work tree, `timertab` initializes one first, then stages and commits the config change. Disable that once with `timertab edit --no-commit`, or persistently in config:

```yaml
git:
  auto_commit: false
```

## Spec and schema

- [v1 Specification](docs/spec-v1.md) — full behavioral spec
- [JSON Schema](schema/v1.json) — for editor integration and validation
- [Libraries](docs/libraries.md) — third-party dependencies

## Development

```bash
make build    # compile
make test     # run tests
make run      # run without building
make install-hooks  # enable local git hooks (.githooks)
```

Git hooks include:
- `pre-commit`: checks staged Go files with `gofmt` and lints changed workflow files with `actionlint`.
- `pre-push`: runs `go vet`, `go build ./cmd/timertab`, and `go test ./...`.

Install `actionlint` once if you edit workflows:

```bash
go install github.com/rhysd/actionlint/cmd/actionlint@v1.7.4
```

## Releases

Tagging `v*` now triggers GitHub Actions release automation:

- Linux binaries via GoReleaser (`amd64`, `arm64`)
- GitHub-native generated release notes/changelog
- `checksums.txt` plus GitHub artifact provenance attestations

Example:

```bash
git tag v1.2.3
git push origin v1.2.3
```

## License

[MIT](LICENSE) © Michał Wadas
