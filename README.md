# timertab

`timertab` is a `crontab -e` style CLI for native `systemd --user` timers.

It keeps jobs in one YAML file, renders `.service` and `.timer` units, and reconciles them safely.

## Why timertab

- Keep the simple edit/apply workflow from `crontab`.
- Use native `systemd` timers and journald logs.
- Keep generated units runnable without `timertab` installed.
- Reconcile only timertab-managed units for the target UID.

## Requirements

- Linux with `systemd >= 247`.
- Go `1.24+` (for building/installing from source).
- A working user manager (`systemctl --user`).
- For non-root users: enable lingering if jobs should run while logged out.

```bash
loginctl enable-linger "$USER"
```

## Install

Install from this repository clone:

```bash
make install
```

Equivalent direct Go command:

```bash
go install ./cmd/timertab
```

Install from module path:

```bash
go install github.com/ginden/timertab/cmd/timertab@latest
```

If needed, add Go bin directory to `PATH`:

```bash
export PATH="$(go env GOPATH)/bin:$PATH"
```

## Quick start

Print config path:

```bash
timertab --print-path
```

Edit config and apply:

```bash
timertab -e
```

Edit config without applying systemd changes:

```bash
timertab -e --no-apply
```

Validate a file:

```bash
timertab validate --config /path/to/timertab.yaml
```

## Example config

Default config path:

- `${XDG_CONFIG_HOME:-$HOME/.config}/timertab/timertab.yaml`

Example:

```yaml
$schema: "https://raw.githubusercontent.com/ginden/timertab/v1.0.0/schema/v1.json"
version: 1
jobs:
  - name: example hourly job
    when: "@hourly"
    run: "echo hello from timertab"
```

`id` is optional in input and is auto-generated/persisted on save.

## Command summary

- `timertab -l` show current config file contents.
- `timertab -e` edit, validate, persist normalized config, reconcile/apply.
- `timertab -e --no-apply` edit, validate, persist only.
- `timertab --print-path` print resolved config path.
- `timertab -u <user> ...` operate on specific user (root can target others).
- `timertab validate --config <path>` validate YAML against schema and semantics.

## Spec and schema

- [docs/spec-v1.md](docs/spec-v1.md)
- [schema/v1.json](schema/v1.json)
- [docs/libraries.md](docs/libraries.md)

## Development

Build:

```bash
make build
```

Run tests:

```bash
make test
```

Run help without building:

```bash
make run
```
