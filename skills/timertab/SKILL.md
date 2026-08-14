---
name: timertab
description: Create, migrate, operate, and troubleshoot scheduled jobs with timertab, a crontab-like YAML frontend for native systemd timers. Use for questions or hands-on work involving timertab setup, job configuration, cron schedules, timer reliability, hooks, logs, status, safe reconciliation, cron migration, multiple instances, generated systemd units, or handing units off from timertab.
---

# Use timertab

Use this guide in either of two ways:

- If you are a person, start with **Quick start**, choose a recipe under **Use cases**, and consult **FAQ and troubleshooting** when something is unclear.
- If you are an AI agent, follow **AI operating procedure** before using the recipes. Explain what will happen in user terms, not as a dump of flags.

## Mental model

Treat the YAML file as the source of truth. Each job becomes one native `.service` plus one native `.timer`. `timertab apply` reconciles those derived units with the YAML; it is not a daemon and generated units do not need the `timertab` binary at runtime.

Expect Linux with systemd 247 or newer. Non-root invocation manages the current user's systemd manager; root invocation manages the system manager.

### Choose user or root scope first

The invoking UID selects both the config and systemd manager. Treat `timertab` and `sudo timertab` as two separate installations:

| Intent | Timertab command | Native systemd command | Typical config |
| --- | --- | --- | --- |
| Run for the current user | `timertab ...` | `systemctl --user ...` | `$HOME/.config/timertab/timertab.yaml` |
| Run as a machine service | `sudo timertab ...` | `sudo systemctl ...` | `/root/.config/timertab/timertab.yaml` |

Use the same scope for edit, list, status, trigger, logs, and native diagnostics. A job shown by `sudo timertab -l` will normally not appear in `timertab status`, and vice versa. Do not add `sudo` merely because systemd is involved; use it only when the job is intentionally root-owned.

Use this default path unless overridden:

1. `--config PATH`
2. `$TIMERTAB_CONFIG_DIR/timertab.yaml`
3. `$XDG_CONFIG_HOME/timertab/timertab.yaml`
4. `$HOME/.config/timertab/timertab.yaml`

Never hand-edit a timertab-managed unit. Change YAML and apply, or eject the job first when the goal is to own the units manually.

## Quick start

Most day-to-day use is this small loop:

```bash
timertab -l                 # read the source-of-truth YAML
timertab status             # scan every job
timertab status JOB_ID      # investigate one job
timertab -e                 # edit, validate, and apply
```

For root-owned machine jobs, keep the whole loop in root scope:

```bash
sudo timertab -l
sudo timertab status
sudo timertab status JOB_ID
sudo EDITOR=nano timertab -e
```

Running bare `timertab` prints command help. `-l` is shorthand for `list`; `-e` is shorthand for `edit`.

Create the first job interactively:

```bash
timertab edit
```

Use a minimal config like:

```yaml
version: 1
jobs:
  - name: daily backup
    when: "@daily"
    run:
      - /usr/bin/rsync
      - -a
      - /home/alice/Documents/
      - /mnt/backup/
```

Saving validates the YAML, generates a stable job ID, writes native units, and starts the timer. Then inspect it:

```bash
timertab status
timertab status daily-backup
timertab logs daily-backup -n 50 --no-pager
```

For a cautious edit/apply cycle:

```bash
timertab edit --no-apply
timertab validate
timertab diff
timertab apply
```

Validation and diff are read-only. If validation fails, apply performs no writes or pruning.

## Guide the user through common choices

### Shell command or argv?

Use a string when shell syntax is intentional:

```yaml
run: |
  set -eu
  pg_dump app | gzip > "$HOME/backups/app.sql.gz"
```

String commands run as `/bin/sh -lc`. Shell expansion, pipes, redirects, and `$HOME` work.

Use a list when exact arguments matter:

```yaml
run: [/usr/bin/rsync, -a, --delete, /srv/source/, /srv/backup/]
```

List commands run directly without a shell. Do not expect `$HOME`, globs, pipes, or redirects to expand in list form.

### What schedule should I use?

Use one shorthand or a standard five-field cron expression:

```yaml
when: "@hourly"
when: "30 2 * * *"
```

Supported shorthands are `@hourly`, `@daily`, `@weekly`, `@monthly`, `@yearly`, `@annually`, and `@reboot`. Seconds and year fields are not supported. Month and weekday names are accepted. When both day-of-month and day-of-week are restricted, timertab preserves cron's OR behavior.

Use a list to run the same command on several schedules:

```yaml
when:
  - "0 9 * * 1-5"
  - "0 18 * * 1-5"
tz: Europe/Warsaw
```

Use an IANA zone in `tz`. It affects calendar schedules, not `@reboot`.

### Should missed runs catch up?

Set `persistent: true` when a calendar run missed during sleep or shutdown should run after the timer becomes available again:

```yaml
persistent: true
```

Use `jitter` to spread simultaneous jobs:

```yaml
jitter: 15m
```

### Where should configuration and secrets go?

Set the working directory and environment explicitly:

```yaml
cwd: /srv/reporting
env:
  REPORT_FORMAT: json
  API_TOKEN: "replace-me"
```

Timertab writes config files with mode `0600`, but environment values also appear in generated units and may be visible through systemd inspection. Prefer a credential file or systemd credential mechanism for high-value secrets.

## Use cases

### Add a simple job without opening an editor

```bash
timertab add --name "refresh cache" --when "*/15 * * * *" -- \
  /usr/local/bin/refresh-cache --quiet
```

Repeat `--when` for multiple schedules and `--env K=V` for environment entries. One command argument is stored as shell shorthand; multiple arguments become explicit argv. Add `--no-apply` to save without changing systemd and `--no-commit` to skip the automatic Git commit once.

### Run a backup reliably

Use direct argv, catch-up, a working directory, resource limits, and a failure hook:

```yaml
- name: documents backup
  when: "30 2 * * *"
  run: [/usr/bin/rsync, -a, --delete, /home/alice/Documents/, /mnt/backup/Documents/]
  cwd: /home/alice
  persistent: true
  jitter: 10m
  limits:
    MemoryMax: 1G
    CPUQuota: 50%
    IOWeight: 100
  on_failure:
    command: 'logger -t timertab "backup failed: $SERVICE_RESULT/$EXIT_STATUS"'
```

`MemoryMax`, `CPUQuota`, and `IOWeight` map to native systemd service controls.

### Notify or recover after a run

Use first-class hooks:

```yaml
on_success:
  command: 'notify-send "Backup complete"'
on_failure:
  command: 'journalctl --user -u "$TIMERTAB_UNIT" -n 100 --no-pager | mail -s "Backup failed" ops@example.com'
  env:
    TEAM: operations
```

Hooks receive `TIMERTAB_JOB_ID`, `TIMERTAB_UNIT`, `SERVICE_RESULT`, `EXIT_CODE`, and `EXIT_STATUS`. Success means the main command exited zero. Hook output goes to journald with the service.

### Use native systemd behavior

Prefer high-level fields first. Add raw directives only when needed:

```yaml
systemd:
  service:
    Restart: on-failure
    RestartSec: 30s
  timer:
    AccuracySec: 1m
```

Use ordered list form when the same directive must appear more than once:

```yaml
systemd:
  service:
    - name: ReadWritePaths
      value: /srv/a
    - name: ReadWritePaths
      value: /srv/b
```

Raw directive values pass through unchanged. In raw values, use `%%` for a literal percent because systemd interprets `%` specifiers. Timertab escapes literal percent characters in its own generated fields.

### Inspect, debug, and run a job now

Start broad, then narrow down:

```bash
timertab status
timertab status JOB_ID
timertab logs JOB_ID -n 100 --no-pager
timertab logs JOB_ID --since today
timertab doctor
```

`status JOB_ID` includes config, unit names and paths, rendered units, recent logs, and diagnostic commands. `doctor` classifies unit files as active, orphaned, other-instance, or ejected/foreign without changing them.

When the combined view is not enough, copy the exact service unit name from `timertab status JOB_ID` and ask systemd directly:

```bash
systemctl --user status UNIT.service   # user-owned job
sudo systemctl status UNIT.service    # root-owned job
```

Use the `.service` unit to inspect the command's latest run. Use the corresponding `.timer` unit to inspect scheduling and activation. Prefer copying names from detailed status over reconstructing generated names by hand.

Run the service immediately without changing its schedule:

```bash
timertab trigger JOB_ID
```

This is a state-changing operation. It starts the generated service, not the timer.

### Pause, resume, remove, or take ownership

- Pause while keeping configuration and units: `timertab disable JOB_ID`.
- Resume: `timertab enable JOB_ID`.
- Delete config and prune managed units: `timertab rm JOB_ID`.
- Keep the native units running but stop timertab management: `timertab eject JOB_ID`.
- Restore management of previously ejected units for a configured job: `timertab adopt JOB_ID`.

Eject removes ownership markers and the config entry but neither deletes units nor calls systemctl; ejected timers may keep running. Use eject only when intentionally switching to manual systemd ownership.

To remove every managed job, set `jobs: []`, validate, inspect `timertab diff`, and apply.

### Migrate an existing crontab safely

Review converted YAML without touching config or systemd:

```bash
timertab import --stdout > timertab.yaml
```

Or render a full offline review bundle:

```bash
crontab -l | timertab render --stdin --output output
```

The bundle contains `timertab.yaml`, `.service`/`.timer` pairs, and `REPORT.md`. Render does not require systemd and never touches live unit directories.

Interactive `timertab import` opens converted jobs for review, merges non-duplicates into the active config, applies, and normally auto-commits. Import warns about unsupported cron constructs such as `MAILTO`, cron `%` stdin syntax, and invalid entries. Review every warning before replacing the original crontab.

### Manage several independent job sets

Give each config a distinct namespace:

```yaml
version: 1
instance_id: work
jobs: []
```

Then target its path explicitly or through `TIMERTAB_CONFIG_DIR`. Ownership and pruning are scoped to both UID and `instance_id`, so `work` and `personal` instances do not prune each other's units. Do not point two different configs at the same instance ID for the same UID.

### Automate and integrate

- Use `timertab status --json` for machine-readable summary status.
- Use `--color=never` or `NO_COLOR` for captured human output.
- Use `-v` or `-vv` for progress and reconcile detail.
- Use `timertab completion bash|zsh|fish|powershell` for shell integration.
- Use `timertab --show-ai-skill` to print this bundled guide.

Mutating config commands use a non-blocking sibling lock file and private `0600` writes. Successful changes auto-commit only the config file by default. The Git repository is selected from the config directory, not the shell's current directory. Disable once with `--no-commit` or persistently:

```yaml
git:
  auto_commit: false
```

## AI operating procedure

Translate the user's goal into a recipe above. Ask only for missing facts that materially affect the result: command, schedule/time zone, user versus root scope, catch-up preference, paths, and whether to apply now.

Before changing anything:

1. Determine scope from the user's established commands and job ownership. Preserve `sudo` when the user is operating root-owned jobs; preserve non-root scope otherwise. If unclear, inspect both scopes read-only before asking.
2. Run `timertab print-path` and `timertab list` in the selected scope for the target config.
3. Run `timertab status` when runtime state matters and `timertab doctor` when ownership or orphaning matters.
4. Use `--config PATH` consistently when the user names a non-default config.
5. Explain whether the proposed action changes YAML, unit files, runtime state, or Git history.

Choose the least surprising interface:

- Use `timertab add`, `rm`, `enable`, `disable`, `trigger`, `eject`, or `adopt` for focused operations.
- Use `timertab edit` for a human-driven edit.
- Edit the resolved YAML directly for a structured automated change that focused commands cannot express. Preserve comments, job IDs, `instance_id`, ordering, and unrelated jobs.
- Never edit generated units while they remain managed.

After editing YAML, run this safety sequence:

```bash
timertab validate --config PATH
timertab diff --config PATH
timertab apply --config PATH
```

Omit apply when the user requested only authoring, review, validation, or preview. Obtain confirmation before apply, trigger, remove, enable, disable, eject, or adopt unless the user already authorized that exact state change. Never apply or prune after validation failure.

After mutation, report the job ID, config path, whether systemd was reconciled, the create/modify/delete summary, and whether Git auto-commit ran. Verify with `timertab status JOB_ID` when systemd is available. Do not treat missing systemd as a failure for `validate`, `diff`, `import --stdout`, or `render` workflows.

Respect ownership boundaries. Mutate only units that match the target UID and instance namespace and carry timertab ownership markers. Never remove foreign, ejected, or other-instance units merely because their names contain `timertab`.

## FAQ and troubleshooting

### Does timertab need to keep running?

No. Timertab writes native systemd units and exits. The systemd manager schedules and runs them. Generated units keep working if the timertab binary is removed.

### Where are my files?

Run `timertab print-path` for YAML. User units normally live in `${XDG_CONFIG_HOME:-$HOME/.config}/systemd/user`; root-managed units live in `/etc/systemd/system`. Run `timertab status JOB_ID` for exact paths.

### Should I use `sudo timertab`?

Use it for jobs that genuinely need root privileges or machine-level service semantics. Root and non-root invocations use different configs, unit directories, managers, journals, and job sets. Once a job is created in one scope, use that same scope for every operation:

```bash
sudo timertab status JOB_ID
sudo timertab trigger JOB_ID
sudo timertab logs JOB_ID
```

For a user-owned job, omit `sudo` from all three. If unsure where a job lives, compare `timertab -l` with `sudo timertab -l` or compare both status summaries before changing anything.

### How do I choose an editor with sudo?

Pass the variable through sudo's command environment:

```bash
sudo EDITOR=nano timertab -e
```

Timertab checks `VISUAL`, then `EDITOR`, then falls back to `editor` and `vi`.

### Why did timertab create a Git repository or commit?

Auto-commit defaults to enabled and operates in the resolved config directory. Use `--no-commit` once or `git.auto_commit: false`. A Git failure is a warning; it does not roll back an otherwise successful apply.

### Why does a user timer not run while I am logged out?

The user systemd manager may stop without a session. Enable lingering:

```bash
loginctl enable-linger "$USER"
```

For `@reboot`, a user timer means user-manager startup, not necessarily machine boot. Timertab enables an `@reboot`-only timer without starting it during apply so it does not fire immediately; use `timertab trigger JOB_ID` for an intentional run now.

### Will a missed run execute later?

Only calendar timers with `persistent: true` catch up after downtime. Use it for backups and maintenance where one delayed run is better than a skipped run.

### Why does my command work in a terminal but fail as a timer?

Timers do not inherit an interactive shell environment. Use absolute executable paths, set `env` and `cwd`, choose string-form `run` when shell features are required, and inspect `timertab logs JOB_ID`. Do not assume shell startup files are loaded.

### How do I preview changes safely?

Run `timertab validate` and `timertab diff`. Neither writes files nor calls systemctl. `edit --no-apply` saves validated YAML but intentionally does not reconcile systemd.

### What happens if YAML is invalid?

Apply fails before unit writes or pruning. Interactive edit reopens the editor. Fix the reported field and run validate again.

### Why is a job shown as `never ran`, `unknown`, or `missing`?

`never ran` means the timer exists but has no recorded invocation. `unknown` or `missing` usually means the unit is absent or systemd state could not be read. Run `timertab status JOB_ID`, then `timertab doctor`, and compare with `timertab diff`.

### How do I inspect failures?

Use `timertab status JOB_ID` for the combined diagnostic view, then `timertab logs JOB_ID -n 100 --no-pager`. Add `--since` and `--until` to narrow journald output. A failed main command triggers `on_failure`; hook failures do not change which hook was selected.

### Can two configs manage timers for the same user?

Yes, if each uses a distinct `instance_id`. Without distinct IDs, both configs claim the same logical ownership namespace and may reconcile each other's jobs.

### Can I edit the generated service or timer?

Not while timertab owns it; the next apply may replace the edit. Use high-level fields or raw `systemd` directives, or run `timertab eject JOB_ID` before taking manual ownership.

### How do I recover an ejected job?

Ensure the job exists in config and both expected unit files still exist, then run `timertab adopt JOB_ID`. Use `--no-apply` if you only want to restore markers before reviewing a diff.

### How do I completely remove timertab-managed timers?

Remove jobs with `timertab rm`, or set `jobs: []` and apply after reviewing the diff. Timertab prunes only marked units for the target UID and instance. Ejected and foreign units remain.

For exact command flags, consult `timertab COMMAND --help`. In the repository, use `README.md`, `docs/cli-reference.md`, `docs/caveats.md`, and `schema/v1.json` as deeper references.
