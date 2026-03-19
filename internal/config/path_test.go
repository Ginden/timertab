package config

import (
	"path/filepath"
	"testing"
)

func TestResolvePathUsesExplicitOverride(t *testing.T) {
	path, err := resolvePath(
		"/tmp/override/timertab.yaml",
		func(string) string {
			t.Fatalf("resolvePath() should not read env for explicit override")
			return ""
		},
		func() (string, error) {
			t.Fatalf("resolvePath() should not resolve home for explicit override")
			return "", nil
		},
	)
	if err != nil {
		t.Fatalf("resolvePath() error = %v", err)
	}
	if path != "/tmp/override/timertab.yaml" {
		t.Fatalf("resolvePath() = %q, want %q", path, "/tmp/override/timertab.yaml")
	}
}

func TestResolvePathPrefersTimertabConfigDirEnv(t *testing.T) {
	path, err := resolvePath(
		"",
		func(key string) string {
			switch key {
			case ConfigDirEnv:
				return "/tmp/custom-timertab"
			case "XDG_CONFIG_HOME":
				return "/tmp/xdg"
			default:
				return ""
			}
		},
		func() (string, error) {
			t.Fatalf("resolvePath() should not resolve home when %s is set", ConfigDirEnv)
			return "", nil
		},
	)
	if err != nil {
		t.Fatalf("resolvePath() error = %v", err)
	}

	want := filepath.Join("/tmp/custom-timertab", FileName)
	if path != want {
		t.Fatalf("resolvePath() = %q, want %q", path, want)
	}
}

func TestResolvePathFallsBackToXDGConfigHome(t *testing.T) {
	path, err := resolvePath(
		"",
		func(key string) string {
			if key == "XDG_CONFIG_HOME" {
				return "/tmp/xdg"
			}
			return ""
		},
		func() (string, error) {
			t.Fatalf("resolvePath() should not resolve home when XDG_CONFIG_HOME is set")
			return "", nil
		},
	)
	if err != nil {
		t.Fatalf("resolvePath() error = %v", err)
	}

	want := filepath.Join("/tmp/xdg", "timertab", FileName)
	if path != want {
		t.Fatalf("resolvePath() = %q, want %q", path, want)
	}
}

func TestResolvePathFallsBackToHome(t *testing.T) {
	path, err := resolvePath(
		"",
		func(string) string { return "" },
		func() (string, error) { return "/home/alice", nil },
	)
	if err != nil {
		t.Fatalf("resolvePath() error = %v", err)
	}

	want := filepath.Join("/home/alice", ".config", "timertab", FileName)
	if path != want {
		t.Fatalf("resolvePath() = %q, want %q", path, want)
	}
}

func TestResolveSystemdUserUnitDirFallsBackToXDGConfigHome(t *testing.T) {
	dir, err := resolveSystemdUserUnitDir(
		func(key string) string {
			if key == "XDG_CONFIG_HOME" {
				return "/tmp/xdg"
			}
			return ""
		},
		func() (string, error) {
			t.Fatalf("resolveSystemdUserUnitDir() should not resolve home when XDG_CONFIG_HOME is set")
			return "", nil
		},
	)
	if err != nil {
		t.Fatalf("resolveSystemdUserUnitDir() error = %v", err)
	}

	want := filepath.Join("/tmp/xdg", "systemd", "user")
	if dir != want {
		t.Fatalf("resolveSystemdUserUnitDir() = %q, want %q", dir, want)
	}
}

func TestResolveSystemdUserUnitDirFallsBackToHome(t *testing.T) {
	dir, err := resolveSystemdUserUnitDir(
		func(string) string { return "" },
		func() (string, error) { return "/home/alice", nil },
	)
	if err != nil {
		t.Fatalf("resolveSystemdUserUnitDir() error = %v", err)
	}

	want := filepath.Join("/home/alice", ".config", "systemd", "user")
	if dir != want {
		t.Fatalf("resolveSystemdUserUnitDir() = %q, want %q", dir, want)
	}
}

func TestResolveSystemdUnitDirForRootUsesSystemUnitDirectory(t *testing.T) {
	dir, err := resolveSystemdUnitDirForUID(
		0,
		func(string) string {
			t.Fatalf("resolveSystemdUnitDirForUID() should not read env for root")
			return ""
		},
		func() (string, error) {
			t.Fatalf("resolveSystemdUnitDirForUID() should not resolve home for root")
			return "", nil
		},
	)
	if err != nil {
		t.Fatalf("resolveSystemdUnitDirForUID() error = %v", err)
	}
	if dir != "/etc/systemd/system" {
		t.Fatalf("resolveSystemdUnitDirForUID() = %q, want %q", dir, "/etc/systemd/system")
	}
}

func TestResolveSystemdUnitDirForNonRootUsesUserUnitDirectory(t *testing.T) {
	dir, err := resolveSystemdUnitDirForUID(
		1000,
		func(key string) string {
			if key == "XDG_CONFIG_HOME" {
				return "/tmp/xdg"
			}
			return ""
		},
		func() (string, error) {
			t.Fatalf("resolveSystemdUnitDirForUID() should not resolve home when XDG_CONFIG_HOME is set")
			return "", nil
		},
	)
	if err != nil {
		t.Fatalf("resolveSystemdUnitDirForUID() error = %v", err)
	}

	want := filepath.Join("/tmp/xdg", "systemd", "user")
	if dir != want {
		t.Fatalf("resolveSystemdUnitDirForUID() = %q, want %q", dir, want)
	}
}

func TestResolveCurrentUID(t *testing.T) {
	uid, err := resolveCurrentUID(func() int { return 1000 })
	if err != nil {
		t.Fatalf("resolveCurrentUID() error = %v", err)
	}
	if uid != 1000 {
		t.Fatalf("resolveCurrentUID() = %d, want %d", uid, 1000)
	}
}

func TestResolveCurrentUIDRejectsNegativeValue(t *testing.T) {
	_, err := resolveCurrentUID(func() int { return -1 })
	if err == nil {
		t.Fatalf("resolveCurrentUID() error = nil, want non-nil")
	}
}
