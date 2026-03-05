package systemd

import (
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
)

const MinimumSupportedVersion = 247

var versionLinePattern = regexp.MustCompile(`^systemd\s+([0-9]+)`)

type versionCommandRunner func(ctx context.Context) ([]byte, error)

var runSystemctlVersion versionCommandRunner = func(ctx context.Context) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "systemctl", "--version")
	return cmd.CombinedOutput()
}

// UnsupportedVersionError indicates that the detected systemd version is below the required baseline.
type UnsupportedVersionError struct {
	Detected int
	Minimum  int
}

func (e *UnsupportedVersionError) Error() string {
	return fmt.Sprintf(
		"timertab requires systemd >= %d, but detected %d. Upgrade systemd and retry, or use --no-apply to edit config without applying.",
		e.Minimum,
		e.Detected,
	)
}

func EnsureBaseline() error {
	return EnsureMinimumVersion(MinimumSupportedVersion)
}

func EnsureMinimumVersion(minimum int) error {
	return ensureMinimumVersion(context.Background(), minimum, runSystemctlVersion)
}

func ensureMinimumVersion(ctx context.Context, minimum int, run versionCommandRunner) error {
	version, err := detectVersion(ctx, run)
	if err != nil {
		return err
	}

	if version < minimum {
		return &UnsupportedVersionError{
			Detected: version,
			Minimum:  minimum,
		}
	}

	return nil
}

func detectVersion(ctx context.Context, run versionCommandRunner) (int, error) {
	output, err := run(ctx)
	if err != nil {
		msg := strings.TrimSpace(string(output))
		if msg == "" {
			return 0, fmt.Errorf("failed to run 'systemctl --version': %w", err)
		}
		return 0, fmt.Errorf("failed to run 'systemctl --version': %w: %s", err, msg)
	}

	version, err := ParseMajorVersion(string(output))
	if err != nil {
		return 0, fmt.Errorf("failed to parse 'systemctl --version' output: %w", err)
	}

	return version, nil
}

func ParseMajorVersion(output string) (int, error) {
	firstLine := strings.TrimSpace(output)
	if firstLine == "" {
		return 0, fmt.Errorf("empty output")
	}

	if idx := strings.IndexByte(firstLine, '\n'); idx >= 0 {
		firstLine = strings.TrimSpace(firstLine[:idx])
	}

	match := versionLinePattern.FindStringSubmatch(firstLine)
	if len(match) != 2 {
		return 0, fmt.Errorf("unexpected first line %q", firstLine)
	}

	version, err := strconv.Atoi(match[1])
	if err != nil {
		return 0, fmt.Errorf("invalid version token %q: %w", match[1], err)
	}

	return version, nil
}
