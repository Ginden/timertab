# Technical Details

This document collects implementation, release, and maintenance notes that are useful to reviewers and contributors, but not essential for day-to-day `timertab` usage.

## Implementation Notes

### Config validation

- Runtime validation uses the embedded [schema/v1.json](../schema/v1.json) rather than loading a schema file from disk.
- Validation has two layers:
  - JSON Schema validation for structure and basic field constraints.
  - Semantic validation in Go for cron subset support, limits, hook rules, and ID normalization.

### Reconcile model

- The YAML config file is the only source of truth.
- Generated unit files are treated as derived artifacts.
- `timertab` only mutates units that match both:
  - the target UID + instance naming prefix
  - embedded `timertab` managed markers inside the unit contents
- This double check is intentional: filename patterns narrow discovery, but content markers are the actual ownership guard before prune or update.

### Instance identity

- Top-level `instance_id` is optional.
- The default logical instance is `timertab`.
- Default instance keeps the historical unit prefix `timertab-u<uid>-...`.
- Custom instances use `timertab-<instance_id>-u<uid>-...`.
- Ownership checks and prune safety are scoped by both UID and instance, not UID alone.

### Generated units

- Each job renders to one `.service` and one `.timer`.
- Generated units are meant to remain understandable and operable without `timertab` installed.
- Success and failure hooks are dispatched from `ExecStopPost`, not `OnSuccess=` / `OnFailure=`, to preserve compatibility with the v1 `systemd >= 247` baseline.

### ID generation

- User-supplied IDs are preserved as-is when valid.
- Missing IDs are generated only after the config fully validates.
- Generation prefers human-readable slugs from `name`, then falls back to a stable digest of job content.
- For digest generation, schedule order is normalized first so reordering equivalent `when` entries does not churn IDs.

### Edit workflow

- `timertab edit` tries to preserve original YAML formatting and comments when validation succeeds and no IDs need to be inserted.
- If IDs must be generated, the tool patches them back into the parsed YAML node tree before falling back to canonical re-marshalling.
- The goal is to avoid unnecessary diff churn during repeated edit/apply cycles.

### Import behavior

- `timertab import` intentionally deduplicates by execution semantics, not by job name or ID.
- Imported entries are compared using command, schedule, cwd, and environment.
- This avoids creating duplicate timers when the same crontab entry is imported multiple times under different labels.

### Render behavior

- `timertab render` reuses the import pipeline, then renders a review bundle instead of touching live `systemd --user` state.
- The bundle contains generated unit files, generated `timertab.yaml`, and `REPORT.md`.
- This path never writes to live systemd unit directories, never calls `systemctl`, and is intended to work even when `systemd` is not installed.

### Auto-commit behavior

- Successful `timertab edit` apply runs auto-commit the config by default.
- If the config directory is not inside a Git work tree, `timertab` initializes one first.
- Git failures are warnings, not fatal errors. The config edit/apply path must not depend on Git success.
- The Git scope is the directory that contains the resolved config file.
- `--config` or `TIMERTAB_CONFIG_DIR` can therefore move the auto-commit boundary into a different repository.
- Disable auto-commit:

```yaml
git:
  auto_commit: false
```

- Or per run:

```bash
timertab edit --no-commit
```

### Config path resolution

- Precedence is:
  - `--config`
  - `TIMERTAB_CONFIG_DIR`
  - `XDG_CONFIG_HOME`
  - `~/.config`
- `TIMERTAB_CONFIG_DIR` only changes where `timertab.yaml` lives.
- It does not change the systemd user unit directory, which remains under the user's standard config home.

## Development

```bash
make build    # compile
make test     # run tests
make run      # run without building
make install-hooks  # enable local git hooks (.githooks)
```

Git hooks include:

- `pre-commit`: checks staged Go files with `gofmt` and lints changed workflow files with `actionlint`
- `pre-push`: runs `go vet`, `go build ./cmd/timertab`, and `go test ./...`

Install `actionlint` once if you edit workflows:

```bash
go install github.com/rhysd/actionlint/cmd/actionlint@v1.7.4
```

## Release Notes

Tagging `v*` triggers GitHub Actions release automation.

Current release flow:

- Linux binaries via GoReleaser (`amd64`, `arm64`)
- Linux `.deb` and `.rpm` packages via GoReleaser (`amd64`, `arm64`)
- Multi-arch import/render container image published to `ghcr.io/ginden/timertab-import`
- GitHub-generated release notes/changelog
- `checksums.txt`
- GitHub artifact provenance attestations

Example:

```bash
git tag v1.2.3
git push origin v1.2.3
```

## Reviewability and Disclosure

Large parts of this project were written with OpenAI Codex.

That is not meant as an appeal to trust. The project is intended to stay small enough that the core logic can be reviewed end-to-end, and release artifacts ship with provenance attestations so published binaries can be tied back to the public build process.
