# timertab

`timertab` is `crontab` ergonomics on top of native `systemd --user` timers.

You keep jobs in one YAML file, `timertab` generates normal `.service` and `.timer` units.

## Why timertab

- Thin wrapper, no lock-in: output is just standard systemd units.
- Near-zero sunk cost: eject a job and keep running it without `timertab`.
- Modern scheduling model: no 40-year cron compatibility baggage.
- Safer reconcile: mutates only timertab-managed units for the target UID.

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

Open editor and apply:

```bash
timertab -e
```

Append one job (alias: `+1`):

```bash
timertab add "@hourly" "echo hello"
```

```bash
timertab +1 "0 9 * * 1-5" "notify-send 'standup'"
```

Stop managing a job, keep its units:

```bash
timertab eject <id>
```

Edit config without applying:

```bash
timertab -e --no-apply
```

Print config:

```bash
timertab -l
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
- `timertab add <when> <run>` append one job to config and apply.
- `timertab +1 <when> <run>` alias for `add`.
- `timertab eject <id>` remove one job from config and unmanage its generated units.
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
