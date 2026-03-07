package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const FileName = "timertab.yaml"
const DefaultInstanceID = "timertab"
const ConfigDirEnv = "TIMERTAB_CONFIG_DIR"

func ResolvePath(override string) (string, error) {
	return resolvePath(override, os.Getenv, os.UserHomeDir)
}

func ResolveSystemdUserUnitDir() (string, error) {
	return resolveSystemdUserUnitDir(os.Getenv, os.UserHomeDir)
}

func ResolveCurrentUID() (uint32, error) {
	return resolveCurrentUID(os.Getuid)
}

func resolvePath(
	override string,
	getenv func(string) string,
	resolveHomeDir func() (string, error),
) (string, error) {
	if override != "" {
		return override, nil
	}

	configDir := strings.TrimSpace(getenv(ConfigDirEnv))
	if configDir != "" {
		return filepath.Join(configDir, FileName), nil
	}
	xdg := strings.TrimSpace(getenv("XDG_CONFIG_HOME"))
	if xdg != "" {
		return filepath.Join(xdg, "timertab", FileName), nil
	}

	home, err := resolveHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}

	return filepath.Join(home, ".config", "timertab", FileName), nil
}

func resolveSystemdUserUnitDir(
	getenv func(string) string,
	resolveHomeDir func() (string, error),
) (string, error) {
	xdg := strings.TrimSpace(getenv("XDG_CONFIG_HOME"))
	if xdg != "" {
		return filepath.Join(xdg, "systemd", "user"), nil
	}

	home, err := resolveHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}

	return filepath.Join(home, ".config", "systemd", "user"), nil
}

func resolveCurrentUID(getuid func() int) (uint32, error) {
	uid := getuid()
	if uid < 0 {
		return 0, fmt.Errorf("current uid is invalid: %d", uid)
	}
	return uint32(uid), nil
}
