# timertab

**Cron jobs done right — one YAML file, real systemd timers under the hood.**

If you've ever wished `crontab -e` gave you systemd timers instead of 1970s cron, this is that tool. You write a simple YAML config, `timertab` turns it into proper `.service` and `.timer` units. No daemon, no lock-in — just plain systemd.

## See what your crontab looks like as systemd timers

No install needed. Pipe your crontab in, get rendered systemd units out:

```bash
crontab -l | docker run --network none --rm -i -v "$PWD/output:/output" ghcr.io/ginden/timertab-import
```

This produces a full review bundle — `timertab.yaml`, rendered `.service` and `.timer` files, and a `REPORT.md` with migration notes — all without touching your system. Runs sandboxed with `--network none`.

## Why timertab?

### 🔓 True zero lock-in

Generated units are standard, human-readable systemd files. They work without `timertab` installed — your timers keep running whether `timertab` is present or not. When you outgrow YAML and want to manage units directly:

```bash
timertab eject <id>
```

This removes ownership markers and leaves a standalone systemd timer. No proprietary format, no runtime dependency.

### 🪝 Success and failure hooks

Run a command when a job succeeds or fails — send a notification, dump logs, trigger another script. Hooks are first-class, not an afterthought:

```yaml
on_success:
  command: "echo ok"
on_failure:
  command: 'notify-send "Backup failed"'
```

Hooks receive rich context: `TIMERTAB_JOB_ID`, `TIMERTAB_UNIT`, `SERVICE_RESULT`, `EXIT_CODE`, `EXIT_STATUS`.

### 🛡️ Atomic reconcile

If your config is invalid, **nothing gets written or pruned**. No partial state, no orphaned units. Ever.

### 🔀 Multiple schedules per job

One job can fire at different times — no need to duplicate entries:

```yaml
when:
  - "0 9 * * *"
  - "0 18 * * *"
```

### 📦 Multiple instances per user

Run separate job namespaces (work, personal, project-specific) on the same machine. Each instance manages its own units and never touches the others:

```yaml
instance_id: work
```

### ⚙️ Raw systemd when you need it

Sensible defaults for everything, full systemd control when you want it:

```yaml
systemd:
  service:
    Restart: "on-failure"
    RestartSec: "30s"
  timer:
    AccuracySec: "1m"
```

### 📝 Git auto-commit

Every successful edit is automatically committed to a local git repo. Full audit trail of every change, with no extra effort. Disable with `--no-commit` or in config.

## All features

- **One YAML file** — all your scheduled jobs in one place, version-control friendly.
- **Cron syntax you already know** — `@hourly`, `@daily`, or standard 5-field cron expressions.
- **Shell shorthand or explicit argv** — use a string for `/bin/sh -lc`, or a YAML list for direct execution.
- **Multiline scripts** — these just work in string mode, run multiple commands without `&&` abuse.
- **Per-user isolation** — units are scoped to your UID; `timertab` never touches units it didn't create.
- **JSON Schema** — get autocomplete and validation in editors that support it.
- **Shell completions** — bash, zsh, and fish.

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
$schema: "https://raw.githubusercontent.com/ginden/timertab/v1.1.0/schema/v1.json"
version: 1
instance_id: work
jobs:
  - name: clean temp files
    when: "@daily"
    run: "find /tmp -user $USER -mtime +7 -delete"
```

Save and close — `timertab` validates the config, generates the systemd units, and starts the timer. That's it.

### A more complete example

```yaml
$schema: "https://raw.githubusercontent.com/ginden/timertab/v1.1.0/schema/v1.json"
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
    run:
      - /usr/bin/rsync
      - -a
      - /home/user/Documents/
      - /mnt/backup/
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

String `run` values are shorthand for `["/bin/sh", "-lc", "..."]`. Use the list form when you want exact argv execution without an extra shell.

## Usage

| Command | What it does |
|---|---|
| `timertab edit` (or `timertab -e`) | Edit config, validate, and apply (generate and start timers) |
| `timertab edit --no-apply` (or `timertab -e --no-apply`) | Edit and validate only, don't touch systemd |
| `timertab edit --no-commit` | Apply without creating/updating the git history entry for that edit run |
| `timertab list` / `timertab print-config` (or `timertab -l`) | Print current config |
| `timertab status` / `timertab status --json` | Show last run, next trigger, and result for each job |
| `timertab status <id>` | Show detailed runtime state, generated unit definitions, file locations, and diagnostic commands |
| `timertab eject <id>` | Stop managing a job — its units stay and keep running |
| `timertab enable <id>` / `timertab disable <id>` | Toggle one job on/off without removing it |
| `timertab logs <id>` | Tail/query journald logs for one job |
| `timertab diff` | Preview create/modify/delete reconcile operations |
| `timertab import` | Convert crontab entries into timertab YAML |
| `timertab render` | Convert crontab input into a review bundle with `timertab.yaml`, rendered units, and `REPORT.md` |
| `timertab validate --config <path>` | Validate a config file without applying |
| `timertab print-path` (or `timertab --print-path`) | Show where the config file lives |

Config file location:
- `--config <path>` if provided
- `${TIMERTAB_CONFIG_DIR}/timertab.yaml` if `TIMERTAB_CONFIG_DIR` is set
- otherwise `${XDG_CONFIG_HOME:-$HOME/.config}/timertab/timertab.yaml`

### Import or Review an Existing Crontab

To convert your current crontab into `timertab` YAML without applying anything:

```bash
timertab import --stdout > timertab.yaml
```

To generate a review bundle with rendered `systemd` units:

```bash
crontab -l | timertab render --stdin --output output
```

`render` writes:

- `output/timertab.yaml`
- one `.service` and one `.timer` file per imported job
- `output/REPORT.md` with imported jobs, warnings, and cron-vs-systemd caveats

Unlike `edit`, `render` never touches live systemd unit directories, never calls `systemctl`, and does not require `systemd` to be installed.

If you want the same review flow without installing `timertab` locally, use the published container image:

```bash
crontab -l | docker run --network none --rm -i -v "$PWD/output:/output" ghcr.io/ginden/timertab-import
```

### Shell Completions

Generate completions with `timertab completion <shell>`.

**bash**

```bash
timertab completion bash > ~/.local/share/bash-completion/completions/timertab
```

**zsh**

```zsh
timertab completion zsh > ~/.zfunc/_timertab
```

Then make sure `~/.zfunc` is on your `fpath`.

**fish**

```bash
timertab completion fish > ~/.config/fish/completions/timertab.fish
```

## Requirements

- Linux with **systemd ≥ 247**
- For non-root use: a running user session (`systemctl --user` must work)
- For root use: access to the system manager (`systemctl` without `--user`)

If you need timers to fire while you're logged out:

```bash
loginctl enable-linger "$USER"
```

`timertab` will automatically detect this and print instructions if it's not set up.

## How it works

When you run `timertab edit` (or `timertab -e`), here's what happens:

1. Your editor opens the YAML config.
2. On save, `timertab` validates the config against the schema and semantic rules.
3. Missing job `id` fields are auto-generated and persisted back to the file.
4. For each job, a `.service` and `.timer` unit is rendered.
5. Stale units (from removed jobs) are stopped, disabled, and deleted.
6. New/changed units are written, `daemon-reload` is called when unit files changed, and timers are enabled/started or disabled/stopped only when needed to match the config.

If validation fails at step 2, nothing else happens — no partial writes, no orphaned units.

Successful `timertab edit` apply runs also auto-commit the config file by default. If the config directory is not already inside a git work tree, `timertab` initializes one first, then stages and commits the config change. Disable that once with `timertab edit --no-commit`, or persistently in config:

```yaml
git:
  auto_commit: false
```

## Spec and schema

- [v1 Specification](docs/spec-v1.md) — spec created for LLMs
- [JSON Schema](schema/v1.json) — for editor integration and validation
- [CLI Reference](docs/cli-reference.md) — command-by-command behavior and flags
- [Technical Details](docs/technical-details.md) — implementation, release, and maintenance notes
- [Caveats and Design Choices](docs/caveats.md) — weird edges and opinionated behavior
- [Libraries](docs/libraries.md) — third-party dependencies

## License

[MIT](LICENSE) © Michał Wadas
