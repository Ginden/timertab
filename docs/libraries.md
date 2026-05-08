# Library Choices

## Selected for v1 bootstrap

- `github.com/spf13/cobra`
  - CLI flags/subcommands, help output, shell completions.
- `gopkg.in/yaml.v3`
  - YAML parsing and marshalling for the timertab file.
- `github.com/santhosh-tekuri/jsonschema/v6`
  - Runtime JSON Schema validation against `schema/v1.json`.
- `github.com/alecthomas/chroma/v2`
  - ANSI syntax highlighting for human-oriented CLI output.
- `golang.org/x/text`
  - Localized formatting for schema validation errors.

## Not selected (intentional)

- `go-systemd` dbus client
  - v1 implementation is expected to shell out to `systemctl` for broad compatibility and easier operator debugging.
- Cron parser libraries
  - v1 keeps a narrow supported expression set, so custom validation/mapping is acceptable.
