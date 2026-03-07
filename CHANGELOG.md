# Changelog

All notable changes to this project will be documented in this file.

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


