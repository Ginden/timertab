# timertab Agent Notes

## Project Intent

`timertab` is a `crontab`-style editor/manager for native `systemd` timers.

Use these docs as source of truth:

- `docs/spec-v1.md`
- `schema/v1.json`

## Locked v1 Decisions

- `systemd >= 247` is mandatory.
- Config file path: `${XDG_CONFIG_HOME:-$HOME/.config}/timertab/timertab.yaml`.
- YAML top-level is an object with `$schema`, `version`, `jobs`.
- `id` is optional in user input, auto-generated and persisted.
- `on_success` and `on_failure` are first-class in v1.
- Generated units must work without `timertab` installed.
- Hook routing is done in native service `ExecStopPost` using `SERVICE_RESULT`/`EXIT_CODE`/`EXIT_STATUS`.
- Do not require `OnSuccess=` (requires systemd 249+, baseline is 247).
- Managed unit naming prefix includes UID scope (`timertab-u<uid>-...`).
- Prune only timertab-managed units for target UID, and never prune when config is invalid.

## Current Implementation Status

- CLI skeleton exists for `-l`, `-e`, `--print-path`, and `validate`.
- YAML parse/validation/ID normalization is present.
- Reconcile to systemd units is not implemented yet.

## Coding Rules

- Prefer small, testable packages under `internal/`.
- Keep YAML and schema behavior aligned (no drift).
- Keep generated unit files human-readable.
- Do not introduce runtime dependency on `timertab` binary in generated units.

## Operational Safety

- Validate before any write/prune action.
- If validation fails: no apply, no prune, no partial writes.
- Preserve behavior for manual systemd operator workflows.