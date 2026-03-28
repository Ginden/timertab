# timertab Agent Notes

## Project Intent

`timertab` is a CLI for managing native `systemd` timers from a crontab-like YAML config, with import and review tooling for cron migration.

Use these as source of truth, in this order:

- `README.md`
- current code under `internal/` and `cmd/`
- `schema/v1.json`
- `docs/spec-v1.md`

If the README/spec drift from the code, follow the code and update docs to match.

## Locked v1 Decisions

- `systemd >= 247` is mandatory.
- Config file path resolution order is:
  `--config` -> `${TIMERTAB_CONFIG_DIR}/timertab.yaml` -> `${XDG_CONFIG_HOME}/timertab/timertab.yaml` -> `$HOME/.config/timertab/timertab.yaml`.
- YAML top-level is an object with `$schema`, `version`, optional `instance_id`, optional `git`, and `jobs`.
- `id` is optional in user input, auto-generated and persisted.
- `on_success` and `on_failure` are first-class in v1.
- `instance_id` is first-class in v1 and isolates multiple logical namespaces for the same UID.
- `git.auto_commit` defaults to enabled unless explicitly disabled.
- Generated units must work without `timertab` installed.
- Hook routing is done in native service `ExecStopPost` using `SERVICE_RESULT`/`EXIT_CODE`/`EXIT_STATUS`.
- Do not require `OnSuccess=` (requires systemd 249+, baseline is 247).
- Managed unit naming includes UID scope and instance scoping:
  `timertab-u<uid>-...` for the default instance and `timertab-<instance_id>-u<uid>-...` for custom instances.
- Prune only timertab-managed units for target UID, and never prune when config is invalid.

## Current Implementation Status

- CLI is implemented for `edit`, `list`/`print-config`, `print-path`, `validate`, `diff`, `status`, `logs`, `trigger`, `enable`, `disable`, `eject`, `import`, `render`, and shell completion.
- Legacy root shorthands `-e`, `-l`, and `--print-path` are still supported by argument rewriting.
- YAML load/schema validation/semantic validation/ID normalization are implemented and covered by tests.
- Reconcile is implemented: render desired units, detect existing managed units, build deterministic create/update/keep/remove plans, prune stale managed units, write unit files, `daemon-reload`, and enable/start or disable/stop timers as needed.
- `edit` preserves user formatting when possible, injects generated IDs back into the original YAML node tree, and auto-commits config changes by default.
- `status` supports both table/JSON summary output and per-job detailed diagnostics.
- `import` converts crontab input into timertab YAML; `render` produces a review bundle (`timertab.yaml`, rendered units, `REPORT.md`) without touching systemd.
- `eject` removes timertab ownership markers from existing unit files and removes the job from config without deleting the units.

## Coding Rules

- Prefer small, testable packages under `internal/`.
- Keep README, YAML behavior, and schema aligned with the code.
- When adding, removing, renaming, or changing command behavior/flags/output, update `docs/cli-reference.md` and any affected CLI notes in this file in the same change.
- Keep generated unit files human-readable.
- Do not introduce runtime dependency on `timertab` binary in generated units.

## Release Flow

- Releasing is tag-driven: pushing a `v*` tag triggers GitHub Actions release automation.
- Current release automation publishes Linux binaries, the `ghcr.io/ginden/timertab-import` image, release notes, checksums, and provenance attestations.
- Example:
  `git tag v1.2.3`
  `git push origin v1.2.3`

## Operational Safety

- Validate and normalize config before any write/prune action.
- If validation fails: no apply, no prune, no partial writes.
- Only mutate/prune units that are both in the timertab namespace and carry timertab managed markers for the target UID/instance.
- `render` and `import --stdout` are non-systemd workflows and must not touch `~/.config/systemd/user` or call `systemctl`.
- Preserve behavior for manual systemd operator workflows.
