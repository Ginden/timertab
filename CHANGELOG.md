# Changelog

All notable changes to this project will be documented in this file.

## [1.1.1] - 2026-03-19

### Miscellaneous Tasks

- Update CHANGELOG.md for v1.1.0

### Other

- AI description: Update schema references to v1.1.0.
- AI description: Add root‑aware systemd scope handling, refactor unit‑directory resolution, update docs, tests, and CLI commands to use the new scope logic.

## [1.1.0] - 2026-03-17

### Bug Fixes

- Missing render

### Documentation

- Rewrite README a bit

### Miscellaneous Tasks

- Update CHANGELOG.md for v1.0.4

### Other

- AI description: Introduce RunCommand abstraction with shell‑shorthand and explicit argv support, updating config, rendering, docs, schema, and related tests.
- AI description: Add LogPeek field to statusDetail, display recent logs in status output, and implement journalctl fallback handling.
- AI description: WIP: broaden CLI commands, add instance_id handling and config path hierarchy, align README and schema, implement unit reconciliation and import/render workflows, and tighten naming and safety rules.
- AI description: Add CLI reference documentation and update the README with a link to it.
- AI description: Update docs/cli-reference.md and related CLI notes when adding, removing, renaming, or changing command flags or behavior.

## [1.0.4] - 2026-03-07

### Bug Fixes

- Use supported goreleaser changelog mode

## [1.0.3] - 2026-03-07

### Bug Fixes

- Keep release checkout clean

## [1.0.2] - 2026-03-07

### Bug Fixes

- Github CI

### Miscellaneous Tasks

- Update CHANGELOG.md for v1.0.0

## [1.0.0] - 2026-03-07

### Bug Fixes

- Validation
- CI
- .gitignore
- Lol
- Duplcates in import from crontab
- Embed runtime schema validation
- Preserve import commands after cron whitespace
- Reload systemd after reconcile changes
- Align colored status columns

### Documentation

- Refresh README and add install target
- Remove low-level apply output details from README
- Describe edit auto-commit behavior
- Add high-level code comments
- Add authorship disclosure

### Features

- Add timertab status command
- Add job-specific logs command
- Add dry-run preview and diff command
- Support @reboot schedules
- Add crontab import command
- Add persistent timer support
- Add jitter support for randomized delay
- Add per-job service resource limits
- Add git auto-commit for edit/apply
- Add shell completion wiring and docs
- Add enable and disable commands
- Add json output mode for status
- Add raw systemd unit/timer directive overrides
- [**breaking**] Rename systemd.unit overrides to systemd.service
- Make edit/list commands first-class with legacy shorthands
- Hooks
- Add detailed status view
- Improve status presentation
- Support multiple timertab instances

### Miscellaneous Tasks

- Use multiline shell example for default job run
- Deprecate dry-run flags

### Other

- Initial commit
- Implement v1  (thx Codex)
- Work in progress
- OK, maybe it works
- Print created/modified/deleted unit operations
- Report systemctl actions and lingering warning
- Add add/+1 and eject commands
- Make add/+1 editor-driven single-job flow
- Prefix warnings/errors and improve +1 template
- Add --print-config alias for list output
- Some changes, I guess?
- Import improvements
- Speed it up
- Use Go build info as metadata fallback
- Preserve YAML comments when saving config
- Some docs changes
- Render
- Remove cross-user mode and add command


