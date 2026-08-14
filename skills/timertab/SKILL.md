---
name: timertab
description: Manage native systemd timers through timertab's crontab-like YAML workflow. Use when a user wants to create, edit, validate, preview, apply, inspect, trigger, enable, disable, import, migrate, troubleshoot, or remove scheduled jobs managed by timertab.
---

# Use timertab

Manage scheduled jobs through `timertab`; do not hand-edit its generated `.timer` or `.service` files.

## Guide the user

Explain the shortest safe workflow for the requested task and show the commands before running any mutating command.

- Create or edit interactively with `timertab edit`. It validates, saves, applies, and auto-commits the config by default.
- Add without an editor with `timertab add --name "NAME" --when "SCHEDULE" -- COMMAND ARG...`.
- Inspect configuration with `timertab list`, its location with `timertab print-path`, and job IDs with `timertab status`.
- Validate without changes with `timertab validate`; preview reconciliation with `timertab diff`.
- Apply an already edited config with `timertab apply`.
- Inspect a job with `timertab status ID` and `timertab logs ID`; run it now with `timertab trigger ID`.
- Pause or resume jobs with `timertab disable ID` and `timertab enable ID`.
- Remove a job and its managed units with `timertab rm ID`.
- Stop timertab management while retaining live units with `timertab eject ID`; restore management with `timertab adopt ID`.
- Convert cron safely with `timertab import --stdout` or create a no-systemd review bundle with `crontab -l | timertab render --stdin --output output`.

Mention that timertab requires Linux with systemd 247 or newer. For user timers that must fire while logged out, tell the user to enable lingering with `loginctl enable-linger "$USER"`.

## Author configuration

Use this minimal structure:

```yaml
version: 1
jobs:
  - name: daily backup
    when: "@daily"
    run:
      - /usr/bin/rsync
      - -a
      - /home/user/Documents/
      - /mnt/backup/
```

Use cron expressions or shorthands such as `@hourly`, `@daily`, and `@reboot`. Use a list under `when` for multiple schedules and `tz: Area/Location` for a calendar time zone. Prefer list-form `run` for exact argv execution; string-form `run` executes through `/bin/sh -lc`. Optional job controls include `env`, `cwd`, `enabled`, `persistent`, `jitter`, `limits`, `systemd`, `on_success`, and `on_failure`. Omit `id` for a new job; timertab generates and persists it.

Use `instance_id` to isolate independent namespaces for the same UID. Set `git.auto_commit: false` to disable automatic config commits persistently, or pass `--no-commit` to supported mutating commands for one run.

## Operate as an AI agent

1. Inspect before changing: run `timertab print-path`, `timertab list`, and the relevant `timertab status` or `timertab doctor` command.
2. Prefer timertab subcommands for mutations. If a requested edit cannot be expressed by a focused command, edit the resolved YAML config rather than generated units.
3. Run `timertab validate` after editing YAML, then show `timertab diff` before `timertab apply` unless the user already requested immediate application.
4. Obtain confirmation before commands that apply, remove, trigger, eject, enable, or disable jobs when the user has not already authorized that state change.
5. Preserve unknown fields, comments, job IDs, `instance_id`, and existing formatting when editing YAML.
6. Never prune or apply when validation fails. Never delete or alter foreign, ejected, other-instance, or non-timertab units.
7. Use `--config PATH` when operating on a non-default file. Resolution otherwise is `TIMERTAB_CONFIG_DIR`, then `XDG_CONFIG_HOME`, then `$HOME/.config/timertab/timertab.yaml`.
8. Report the job ID and summarize config and unit changes after a mutation. Use `timertab status ID` to verify runtime state where systemd is available.

For exact flags and advanced fields, consult `timertab --help`, `timertab COMMAND --help`, the repository `README.md`, `docs/cli-reference.md`, and `schema/v1.json`.
