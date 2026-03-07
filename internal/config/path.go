package config

import (
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
)

const FileName = "timertab.yaml"
const DefaultInstanceID = "timertab"
const ConfigDirEnv = "TIMERTAB_CONFIG_DIR"

func ResolvePath(targetUser, override string) (string, error) {
	return resolvePath(targetUser, override, os.Getenv, os.UserHomeDir, user.Lookup)
}

func ResolveSystemdUserUnitDir(targetUser string) (string, error) {
	return resolveSystemdUserUnitDir(targetUser, os.Getenv, os.UserHomeDir, user.Lookup)
}

func ResolveTargetUID(targetUser string) (uint32, error) {
	return resolveTargetUID(targetUser, os.Getuid, user.Lookup)
}

func ValidateTargetUserPermission(targetUser string) error {
	return validateTargetUserPermission(targetUser, os.Geteuid, user.Lookup)
}

func resolvePath(
	targetUser, override string,
	getenv func(string) string,
	resolveHomeDir func() (string, error),
	lookupUser func(string) (*user.User, error),
) (string, error) {
	if override != "" {
		return override, nil
	}

	normalizedTargetUser := strings.TrimSpace(targetUser)
	if normalizedTargetUser != "" {
		u, err := lookupUser(normalizedTargetUser)
		if err != nil {
			return "", fmt.Errorf("lookup user %q: %w", normalizedTargetUser, err)
		}
		if strings.TrimSpace(u.HomeDir) == "" {
			return "", fmt.Errorf("user %q has no home directory", normalizedTargetUser)
		}
		return filepath.Join(u.HomeDir, ".config", "timertab", FileName), nil
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
	targetUser string,
	getenv func(string) string,
	resolveHomeDir func() (string, error),
	lookupUser func(string) (*user.User, error),
) (string, error) {
	normalizedTargetUser := strings.TrimSpace(targetUser)
	if normalizedTargetUser != "" {
		u, err := lookupUser(normalizedTargetUser)
		if err != nil {
			return "", fmt.Errorf("lookup user %q: %w", normalizedTargetUser, err)
		}
		if strings.TrimSpace(u.HomeDir) == "" {
			return "", fmt.Errorf("user %q has no home directory", normalizedTargetUser)
		}
		return filepath.Join(u.HomeDir, ".config", "systemd", "user"), nil
	}

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

func resolveTargetUID(
	targetUser string,
	getuid func() int,
	lookupUser func(string) (*user.User, error),
) (uint32, error) {
	normalizedTargetUser := strings.TrimSpace(targetUser)
	if normalizedTargetUser == "" {
		uid := getuid()
		if uid < 0 {
			return 0, fmt.Errorf("current uid is invalid: %d", uid)
		}
		return uint32(uid), nil
	}

	target, err := lookupUser(normalizedTargetUser)
	if err != nil {
		return 0, fmt.Errorf("lookup user %q: %w", normalizedTargetUser, err)
	}

	uid, err := strconv.ParseUint(target.Uid, 10, 32)
	if err != nil {
		return 0, fmt.Errorf("parse uid for user %q: %w", normalizedTargetUser, err)
	}

	return uint32(uid), nil
}

func validateTargetUserPermission(
	targetUser string,
	effectiveUID func() int,
	lookupUser func(string) (*user.User, error),
) error {
	normalizedTargetUser := strings.TrimSpace(targetUser)
	if normalizedTargetUser == "" {
		return nil
	}

	target, err := lookupUser(normalizedTargetUser)
	if err != nil {
		return fmt.Errorf("lookup user %q: %w", normalizedTargetUser, err)
	}

	euid := effectiveUID()
	if euid == 0 {
		return nil
	}

	euidString := strconv.Itoa(euid)
	if target.Uid == euidString {
		return nil
	}

	return fmt.Errorf(
		"-u/--user %q is not permitted for effective uid %s (target uid %s): only root may target another user",
		normalizedTargetUser,
		euidString,
		target.Uid,
	)
}
