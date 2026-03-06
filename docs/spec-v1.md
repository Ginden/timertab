# timertab v1 Specification

## 1. Goals

`timertab` provides a `crontab`-like editing workflow while managing native `systemd` timers and services.

v1 focuses on:

- `timertab -l` (list)
- `timertab -e` (edit + validate + apply)
- `timertab -u <user>` (target another user when privileges allow)
- `on_success` and `on_failure` hooks

## 2. Compatibility Baseline

- Minimum supported `systemd` version: **247**
- On startup/apply, `timertab` MUST check `systemd` version and fail fast on `<247`.

## 3. Source of Truth

Config path:

- `${XDG_CONFIG_HOME:-$HOME/.config}/timertab/timertab.yaml`

The YAML config file is the source of truth. Generated unit files are derived artifacts.

## 4. YAML Schema Shape

Top-level is an object (not an array), to support `$schema`:

```yaml
$schema: "https://raw.githubusercontent.com/ginden/timertab/v1.0.0/schema/v1.json"
version: 1
jobs:
  - id: "npm-cache-verify"
    name: "NPM cache verify"
    when: "@hourly"
    run: "npm --global cache verify"
    env:
      NPM_CONFIG_PREFIX: "/home/user/.npm-global"
    on_success:
      command: "echo ok"
    on_failure:
      command: "journalctl -u \"$TIMERTAB_UNIT\" -n 100 --no-pager"
```

Job fields:

- required: `when`, `run`
- optional: `id`, `name`, `env`, `cwd`, `enabled`, `on_success`, `on_failure`

### 4.1 `id`

- Input: optional
- Persisted config: required (auto-generated when missing)
- Pattern: `^[a-z0-9][a-z0-9._-]{0,63}$`
- Must be unique in `jobs[]`

ID generation:

1. `slug(name)` when available and unique
2. `job-<shortsha256(canonical-job-without-id)>`
3. numeric suffix on collision

### 4.2 `when`

`when` accepts either:

- single string
- list of strings

Supported values:

- shorthands: `@hourly`, `@daily`, `@weekly`, `@monthly`, `@yearly`, `@annually`
- 5-field cron expressions

Unsupported in v1:

- cron seconds/year extensions
- `CRON_TZ`

## 5. Hook Semantics

Both hooks are first-class in v1.

- `on_success.command`: executed only when main job succeeds
- `on_failure.command`: executed only when main job fails

Both can define extra `env`.

Hook environment includes:

- `TIMERTAB_JOB_ID`
- `TIMERTAB_UNIT`
- `SERVICE_RESULT`
- `EXIT_CODE`
- `EXIT_STATUS`

## 6. Native systemd Integration Model

Generated units are fully native and do not require `timertab` at runtime.

Per job:

- `timertab-u<uid>-<idslug>-<short>.service`
- `timertab-u<uid>-<idslug>-<short>.timer`

Service shape:

- `ExecStart=/bin/sh -lc '<run>'`
- `ExecStopPost=/bin/sh -lc '<hook-dispatch>'`

Hook dispatch uses `SERVICE_RESULT`/`EXIT_CODE`/`EXIT_STATUS` provided by `systemd`.

`OnSuccess=` is intentionally not used because it requires systemd 249+, while v1 baseline is 247.

## 7. Logging and Failures

- Primary output is native journald logging.
- Hooks should inspect logs through `journalctl` when needed.
- Main service success/failure is based on main `run` command status.

## 8. Reconcile and Prune

`timertab` manages only its own units.

Ownership rules:

- unit name prefix: `timertab-u<uid>-`
- generated units contain managed marker comments/metadata

Apply algorithm:

1. Parse + validate config
2. Auto-generate/persist missing IDs
3. Render desired units
4. Discover existing managed units for target UID
5. Stop/disable/delete stale managed units
6. Write desired units
7. `daemon-reload`
8. enable/start desired timers

Safety rule:

- If validation fails, do not write units and do not prune.

## 9. CLI Behavior

- `timertab -l`: list current jobs from source-of-truth config
- `timertab -e`: open editor, validate, persist IDs, apply on success
- `timertab -u <user>`: target another user (privilege gated)
- `sudo timertab -e`: root context by default
