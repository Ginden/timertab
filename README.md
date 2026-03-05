# timertab

`timertab` is a `crontab`-like CLI for managing native `systemd` timers from one YAML file.

## Status

Bootstrap phase. Core scaffolding, v1 schema, and design contract are in place.

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

## CLI (current bootstrap)

- `timertab -l` list the source config content
- `timertab -e` edit + validate + normalize IDs, then save
- `timertab --print-path` print resolved config path
- `timertab validate --config <path>` validate file

Reconcile/apply to `systemd` is intentionally not implemented yet.

## Chosen Libraries

- `github.com/spf13/cobra`: CLI framework
- `gopkg.in/yaml.v3`: YAML parsing/marshalling
- `github.com/santhosh-tekuri/jsonschema/v5`: JSON Schema validation (planned wiring in next step)

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

1. Wire `schema/v1.json` validation into runtime loader.
2. Implement compiler from jobs -> `.service`/`.timer`.
3. Implement reconcile (create/update/prune) with safety markers.
4. Implement `-u` privilege checks and user scope behavior.
5. Add integration tests with `systemd-run`/containerized systemd harness.
