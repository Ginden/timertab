# CLI Reference

This document describes the current `timertab` command-line interface as implemented in the codebase.

## Root Command

`timertab` manages native `systemd` timers from a YAML config file.

Running `timertab` with no arguments prints help.

Global flags:

- `-h`, `--help`: show help
- `-V`, `--version`: print the build version
- `-v`, `--verbose`: increase verbosity; repeat as `-vv` or `-vvv` for more detail
- `--color auto|always|never`: control ANSI color and syntax highlighting; defaults to `auto`

Verbosity:

- Default output is quiet: primary command output, warnings, and errors.
- `-v` prints high-level progress phases such as validation, save, reconcile, and auto-commit.
- `-vv` also prints lower-level reconcile/apply phases such as rendering desired units, scanning existing unit files, and applying systemd manager operations.
- `-vvv` is reserved for future debug-level diagnostics.

Color:

- `--color=auto` colorizes terminal output and disables color when stdout is redirected.
- `--color=always` forces ANSI color even when output is captured.
- `--color=never` disables ANSI color.
- `NO_COLOR` disables color when `--color=auto` is used.
- Machine-readable output such as `status --json` is not colorized.

Legacy root shorthands are still accepted:

- `timertab -e` or `timertab --edit` -> `timertab edit`
- `timertab -l`, `timertab --list`, or `timertab --print-config` -> `timertab list`
- `timertab --print-path` -> `timertab print-path`

These legacy shorthands are mutually exclusive.

## Config Path Resolution

Commands that work with the main config file resolve its path in this order:

1. `--config <path>`
2. `${TIMERTAB_CONFIG_DIR}/timertab.yaml`
3. `${XDG_CONFIG_HOME}/timertab/timertab.yaml`
4. `$HOME/.config/timertab/timertab.yaml`

Generated unit files live under the target manager's unit directory:

- for non-root users: `${XDG_CONFIG_HOME}/systemd/user`
- for non-root users when `XDG_CONFIG_HOME` is unset: `$HOME/.config/systemd/user`
- for root: `/etc/systemd/system`

## Command Summary

| Command | Purpose |
| --- | --- |
| `timertab list` | Print the current config file |
| `timertab print-path` | Print the resolved config path |
| `timertab validate --config <path>` | Validate a config file and print `ok` on success |
| `timertab edit` | Open the config in an editor, validate it, save it, and usually apply it |
| `timertab diff` | Preview unit file creates/modifies/deletes without writing |
| `timertab status` | Show summary status for configured jobs |
| `timertab status <id>` | Show a detailed report for one job |
| `timertab logs <id>` | Show `journalctl` output for one job |
| `timertab trigger <id>` | Start one job's generated service immediately |
| `timertab enable <id>` | Mark a job enabled, save config, and apply |
| `timertab disable <id>` | Mark a job disabled, save config, and apply |
| `timertab eject <id>` | Stop managing a job but leave its unit files in place |
| `timertab import` | Convert crontab entries into timertab jobs |
| `timertab render` | Render a crontab review bundle without touching `systemd` |
| `timertab completion <shell>` | Generate shell completion scripts |

## `timertab list`

Usage:

```bash
timertab list [--config <path>]
```

Aliases:

- `timertab print-config`
- legacy root forms: `timertab -l`, `timertab --list`, `timertab --print-config`

Behavior:

- Prints a header line with the resolved config path.
- Prints the raw YAML file contents as stored on disk.
- Syntax-highlights the YAML when color is enabled.
- If the file does not exist, prints `# no timertab file found`.

## `timertab print-path`

Usage:

```bash
timertab print-path [--config <path>]
```

Behavior:

- Resolves the config path using the standard path rules.
- Prints that path and exits.

## `timertab validate`

Usage:

```bash
timertab validate --config <path>
```

Behavior:

- `--config` is required.
- Loads the YAML file, validates it against the embedded schema and semantic rules, and normalizes missing job IDs in memory.
- Prints `ok` on success.
- Does not write files, call `systemctl`, or modify the config.

## `timertab edit`

Usage:

```bash
timertab edit [--config <path>] [--no-apply] [--no-commit]
```

Legacy forms:

- `timertab -e`
- `timertab --edit`

Behavior:

- Resolves the config path and opens a temporary editing buffer.
- Uses the existing config file when present.
- Uses the built-in default template when the file does not yet exist.
- Resolves the editor in this order: `VISUAL`, `EDITOR`, `editor`, `vi`.
- Reopens the editor when validation fails, instead of saving an invalid config.
- Persists auto-generated job IDs back into the YAML when needed.
- Preserves the original YAML formatting/comments when normalization does not require rewriting.

Apply behavior:

- Without `--no-apply`, `edit` checks the `systemd >= 247` baseline first.
- On successful validation it writes the config file, reconciles unit files, reloads the target systemd manager when unit files changed, and enables/starts or disables/stops timers only when runtime state needs reconciliation.
- `@reboot`-only timers are enabled but not started during apply, so they do not fire immediately. Use `timertab trigger <id>` for an intentional immediate run.
- If validation fails, nothing is written or pruned.
- During successful `edit` runs, `-v` prints progress lines such as validation, save, reconcile, reload, and auto-commit phases to stderr.

Git behavior:

- Successful `edit` runs auto-commit the config by default.
- If the config directory is not already inside a git work tree, `timertab` initializes one first.
- `--no-commit` disables auto-commit for that run only.
- The config can also disable auto-commit persistently with `git.auto_commit: false`.

Flags:

- `--config <path>`: override the resolved config path
- `--no-apply`: save validated YAML but do not touch `systemd`
- `--no-commit`: skip the git auto-commit step for this run

Notes:

- A deprecated hidden `--dry-run` flag still exists in code. `timertab diff` is the preferred preview flow.

## `timertab diff`

Usage:

```bash
timertab diff [--config <path>]
```

Behavior:

- Loads the config, normalizes IDs, renders the desired unit set, and compares it with existing managed unit files.
- Prints lines such as `would create ...`, `would modify ...`, and `would delete ...`.
- Ends with a summary line like `summary: create=1 modify=0 delete=2`.
- Does not write files or call `systemctl`.

## `timertab status`

Usage:

```bash
timertab status [id] [--config <path>] [--json]
```

Summary mode:

- `timertab status` prints one row per configured job.
- Columns are `id`, `last_run`, `next_trigger`, and `result`.
- `result` is normalized to `pass`, `fail`, or `unknown`.
- `--json` is only supported in summary mode.

Detail mode:

- `timertab status <id>` prints a detailed report for one job.
- Includes overview fields, current unit names/paths/states, the job YAML, rendered service and timer unit bodies, recent log lines, and suggested diagnostic commands.
- The detailed view does not support `--json`.
- When color is enabled, the detailed view highlights statuses, job YAML, rendered unit snippets, and example diagnostic commands.

Behavior notes:

- Non-root mode inspects units with `systemctl --user show`; root mode uses `systemctl show`.
- Missing units are reported as `unknown` or `missing` instead of hard-failing.
- The detailed log preview uses `journalctl --user -u <service> -n 20 --no-pager` for non-root users and `journalctl -u <service> -n 20 --no-pager` for root.

## `timertab logs`

Usage:

```bash
timertab logs <id> [--config <path>] [-n <lines>] [-f] [--since <date>] [--until <date>] [--no-pager]
```

Behavior:

- Resolves the service unit name for the given job ID.
- Runs `journalctl --user -u <service unit>` for non-root users and `journalctl -u <service unit>` for root.
- Streams output directly from `journalctl`.

Flags:

- `--config <path>`: override the config path
- `-n`, `--lines <N>`: show the most recent N lines
- `-f`, `--follow`: follow the journal
- `--since <DATE>`: pass a `--since` filter to `journalctl`
- `--until <DATE>`: pass an `--until` filter to `journalctl`
- `--no-pager`: pass `--no-pager` to `journalctl`

## `timertab trigger`

Usage:

```bash
timertab trigger <id> [--config <path>]
```

Behavior:

- Loads the config, normalizes IDs, and resolves the generated service unit name for the selected job.
- Runs `systemctl --user start <service unit>` for non-root users and `systemctl start <service unit>` for root.
- Does not modify the config file.
- Prints a success line with the job ID and resolved service unit name.

## `timertab enable`

Usage:

```bash
timertab enable <id> [--config <path>] [--no-commit]
```

Behavior:

- Loads the config and sets `jobs[].enabled` to `true` for the selected job.
- Saves the config file, preserving user comments and formatting.
- Checks the `systemd >= 247` baseline.
- Applies the resulting reconcile plan.
- Auto-commits the config change (`timertab: enable job <id>`) unless `--no-commit`
  is set or `git.auto_commit` is disabled.

Output includes the saved config path and the same apply report used by `edit`.

## `timertab disable`

Usage:

```bash
timertab disable <id> [--config <path>] [--no-commit]
```

Behavior:

- Loads the config and sets `jobs[].enabled` to `false` for the selected job.
- Saves the config file, preserving user comments and formatting.
- Checks the `systemd >= 247` baseline.
- Applies the resulting reconcile plan.
- Auto-commits the config change (`timertab: disable job <id>`) unless `--no-commit`
  is set or `git.auto_commit` is disabled.

Output includes the saved config path and the same apply report used by `edit`.

## `timertab eject`

Usage:

```bash
timertab eject <id> [--config <path>] [--no-commit]
```

Behavior:

- Resolves the current generated service and timer names for the job.
- Removes timertab ownership markers from those unit files if they exist.
- Removes the job from the config file and saves the updated config, preserving
  user comments and formatting.
- Does not delete the unit files.
- Does not call `systemctl`.
- Auto-commits the config change (`timertab: eject job <id>`) unless `--no-commit`
  is set or `git.auto_commit` is disabled.

Warnings:

- If the expected service or timer file is missing, `eject` warns but still removes the job from the config.

Use `eject` when you want the units to remain as normal standalone `systemd` units that `timertab` no longer manages.

## `timertab import`

Usage:

```bash
timertab import [--stdin] [--stdout] [--config <path>] [--no-apply] [--no-commit]
```

Input behavior:

- If `--stdin` is set, reads crontab input from stdin.
- If stdin is piped, reads from stdin automatically.
- Otherwise runs `crontab -l`.

Output modes:

- If `--stdout` is set, writes imported YAML to stdout.
- If stdout is not a TTY, also writes YAML to stdout automatically.
- Otherwise opens an editor with the imported jobs, then merges the edited result into the main config.

Import behavior:

- Converts supported cron entries into timertab jobs.
- Carries over supported environment variables into `env`.
- Ignores unsupported global variables such as `MAILTO` and `SHELL` with warnings.
- Strips cron `%` stdin syntax and inline shell comments with warnings.
- Skips unsupported or invalid entries with warnings instead of aborting the whole import.
- Generates a v1 config with normalized job IDs.

Merge behavior:

- Interactive import merges imported jobs into the destination config.
- Duplicate detection is based on execution semantics, not on human-facing fields.
- Duplicates are detected from `run`, `cwd`, normalized schedules, and `env`.
- Duplicate `name` or `id` values alone do not make entries distinct.

Apply behavior:

- In interactive mode, `--no-apply` saves the merged config without reconciling `systemd`.
- Without `--no-apply`, import saves the config and applies it, then auto-commits the
  config change (`timertab: import N job(s)`) unless `--no-commit` is set or
  `git.auto_commit` is disabled.
- In stdout mode, import does not touch the config file or `systemd`.

Notes:

- A deprecated hidden `--dry-run` flag still exists in code for merge previews.

## `timertab render`

Usage:

```bash
timertab render [--stdin] [-o <dir>] [--uid <uid>]
```

Behavior:

- Reads crontab input using the same stdin-or-`crontab -l` rules as `import`.
- Converts the input to timertab jobs.
- Renders service and timer units for each imported job.
- Writes a review bundle to the output directory.

Files written:

- `timertab.yaml`
- one `.service` file per imported job
- one `.timer` file per imported job
- `REPORT.md`

Flags:

- `--stdin`: read crontab input from stdin
- `-o`, `--output <dir>`: output directory, default `output`
- `--uid <uid>`: UID embedded into generated unit names, default `1000`

Important properties:

- `render` never touches live systemd unit directories.
- `render` never calls `systemctl`.
- `render` works even when `systemd` is not installed.
- `REPORT.md` includes imported jobs, warnings, and cron-vs-systemd caveats.

## `timertab completion`

Usage:

```bash
timertab completion <shell>
```

Available shells:

- `bash`
- `zsh`
- `fish`
- `powershell`

Behavior:

- Prints a completion script to stdout.
- Intended to be redirected into the appropriate completion directory for the shell.

## Operational Notes

Commands that modify managed units:

- `edit` without `--no-apply`
- `trigger`
- `enable`
- `disable`
- interactive `import` without `--no-apply`

Commands that inspect but do not modify `systemd`:

- `status`
- `logs`

Commands that do not touch `systemd` at all:

- `list`
- `print-path`
- `validate`
- `diff`
- `eject`
- `import --stdout`
- `render`
- `completion`
