# Library Choices

## Selected for v1 bootstrap

- `github.com/spf13/cobra`
  - CLI flags/subcommands, help output, shell completions.
- `gopkg.in/yaml.v3`
  - YAML parsing and marshalling for the timertab file.

## Deferred but planned

- `github.com/santhosh-tekuri/jsonschema/v5`
  - Runtime JSON Schema validation against `schema/v1.json`.
  - Deferred from bootstrap to keep early implementation simple.

## Not selected (intentional)

- `go-systemd` dbus client
  - v1 implementation is expected to shell out to `systemctl` for broad compatibility and easier operator debugging.
- Cron parser libraries
  - v1 keeps a narrow supported expression set, so custom validation/mapping is acceptable.
