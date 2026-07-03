# Caveats and Design Choices

This document records behavior that is intentional, but easy to misread if you only skim the CLI or generated files.

## Config Path Resolution

Resolution order is:

1. `--config <path>`
2. `TIMERTAB_CONFIG_DIR`
3. `XDG_CONFIG_HOME`
4. `~/.config`

`TIMERTAB_CONFIG_DIR` replaces only the `timertab` config directory. It does not move the `systemd` user unit directory.

## Instance Identity

`timertab` supports multiple logical instances per user through top-level `instance_id`.

- Omitted `instance_id` means the default instance `timertab`.
- The default instance keeps the historical unit prefix `timertab-u<uid>-...`.
- A custom instance such as `instance_id: work` uses `timertab-work-u<uid>-...`.

This is not just naming. Ownership and prune safety are scoped by both:

- target UID
- effective instance ID

That means two configs for the same user do not prune each other's units as long as they use different `instance_id` values.

## Auto-Commit Scope

Auto-commit is tied to the resolved config path, not the current working directory.

Behavior:

- `timertab` runs Git commands with `git -C <config-dir>`
- if `<config-dir>` is already inside a Git work tree, that repository is used
- otherwise `timertab` initializes a new repository in `<config-dir>`

This is convenient for the default config location, but it is opinionated for `--config` overrides: pointing the config into some existing repository also moves the auto-commit boundary into that repository.

Only the config file itself is staged and committed by `timertab`, but the repository choice follows the config path.

If `git` is missing or Git operations fail, edit/apply still succeeds and the failure degrades to a warning.

## Generated Units Are Native

Generated units are meant to work without `timertab` installed.

That is why:

- no runtime dependency on the `timertab` binary is allowed in units
- hooks are dispatched through shell inside the generated service itself
- `timertab eject <id>` removes ownership markers but leaves the units in place

## `@reboot` Timers

`@reboot` compiles to `OnBootSec=0`.

For user timers, this means the job is tied to the user systemd manager. It runs after the user session starts, not necessarily at machine boot unless lingering is enabled with `loginctl enable-linger <user>`.

Starting a systemd timer with only `OnBootSec=0` after boot makes systemd consider the timer elapsed immediately. To preserve cron-like `@reboot` behavior, `timertab` enables `@reboot`-only timers during apply but does not start them immediately. They are armed for the next boot or user-manager start.

Use `timertab trigger <id>` when you intentionally want to run an `@reboot` job right now.

## Hook Execution Model

Success and failure hooks are dispatched from `ExecStopPost`.

This is deliberate. `OnSuccess=` and `OnFailure=` would be cleaner conceptually, but the v1 baseline is `systemd >= 247`, and relying on newer unit features would make generated units less portable.

## Status Output

Human-oriented `status` output uses color according to the global `--color=auto|always|never` policy.

- JSON output stays machine-readable and uncolored.
- With the default `--color=auto`, piped or redirected text stays plain.
- `NO_COLOR` disables color when `--color=auto` is used.
- The summary view computes its own column widths because standard tab alignment breaks once ANSI color sequences are present.

## YAML Preservation

`timertab edit` tries to preserve the user's formatting and comments.

That preservation is best-effort:

- if validation succeeds and no IDs need to be inserted, the original YAML bytes are preserved
- if IDs must be generated, `timertab` patches them into the parsed YAML tree
- if that patching fails, it falls back to canonical re-marshalling

So comment/layout preservation is intentional, but not a hard guarantee under every mutation path.
