# timertab

`timertab` is a `crontab`-like CLI for managing native `systemd` timers from one YAML file.

## Status

Core v1 pipeline is implemented:

- runtime schema + semantic validation
- schedule compiler (`when` -> `OnCalendar`)
- native unit renderer (`.service` / `.timer`)
- reconcile planner with managed-only prune safety
- target-user permission checks (`-u`)
- end-to-end `timertab -e` flow (edit, validate, persist IDs, reconcile units, run `systemctl --user`)

## Why

- Keep the simple `crontab -e` workflow
- Use native `systemd` timers/services
- Preserve proper journald logs and `systemd` failure states
- Allow generated units to keep working even if `timertab` is uninstalled

## Compatibility

- Linux with `systemd >= 247`
- Mainstream architectures (x86_64, arm64, riscv64 expected)
- Single static binary is a first-class packaging target

## Config

Default config path:

- `${XDG_CONFIG_HOME:-$HOME/.config}/timertab/timertab.yaml`

Minimal config:

```yaml
$schema: "https://raw.githubusercontent.com/ginden/timertab/v1.0.0/schema/v1.json"
version: 1
jobs:
  - when: "@hourly"
    run: "npm --global cache verify"
```

Full v1 contract:

- [docs/spec-v1.md](docs/spec-v1.md)
- [schema/v1.json](schema/v1.json)
- [docs/libraries.md](docs/libraries.md)

## CLI (current state)

- `timertab -l` list the source config content
- `timertab -e` edit + validate + normalize IDs, then reconcile/apply
- `timertab -e --no-apply` edit + validate + normalize IDs, save only
- `timertab --print-path` print resolved config path
- `timertab validate --config <path>` validate file

## Chosen Libraries

- `github.com/spf13/cobra`: CLI framework
- `gopkg.in/yaml.v3`: YAML parsing/marshalling
- `github.com/santhosh-tekuri/jsonschema/v6`: JSON Schema validation

## Development

Requirements:

- Go 1.24+

Compile:

```bash
# from repository root
make build
```

Binary output:

- `./bin/timertab`

Compile without Makefile:

```bash
mkdir -p ./bin
go build -o ./bin/timertab ./cmd/timertab
```

Test:

```bash
make test
```

Run compiled binary:

```bash
./bin/timertab --help
```

Quick run without compiling:

```bash
make run
```

## Planned Milestones

1. Add integration tests with `systemd-run`/containerized systemd harness.
2. Harden cross-user apply behavior for privileged `-u` operations in environments without active user sessions.
