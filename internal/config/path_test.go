package config

import (
	"errors"
	"os/user"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolvePathTargetUserUsesLookupHome(t *testing.T) {
	lookupCalled := false
	path, err := resolvePath(
		"  alice  ",
		"",
		func(string) string {
			t.Fatalf("resolvePath() should not read XDG_CONFIG_HOME for explicit target user")
			return ""
		},
		func() (string, error) {
			t.Fatalf("resolvePath() should not resolve caller home for explicit target user")
			return "", nil
		},
		func(name string) (*user.User, error) {
			lookupCalled = true
			if name != "alice" {
				t.Fatalf("lookup user name = %q, want %q", name, "alice")
			}
			return &user.User{
				Username: "alice",
				Uid:      "1001",
				HomeDir:  "/home/alice",
			}, nil
		},
	)
	if err != nil {
		t.Fatalf("resolvePath() returned error: %v", err)
	}
	if !lookupCalled {
		t.Fatalf("resolvePath() did not call user lookup")
	}

	want := filepath.Join("/home/alice", ".config", "timertab", FileName)
	if path != want {
		t.Fatalf("resolvePath() = %q, want %q", path, want)
	}
}

func TestValidateTargetUserPermissionFlows(t *testing.T) {
	tests := []struct {
		name       string
		targetUser string
		euid       int
		lookupUser *user.User
		wantErr    string
	}{
		{
			name:       "current user is allowed",
			targetUser: "alice",
			euid:       1000,
			lookupUser: &user.User{Username: "alice", Uid: "1000"},
		},
		{
			name:       "root can target foreign user",
			targetUser: "bob",
			euid:       0,
			lookupUser: &user.User{Username: "bob", Uid: "1001"},
		},
		{
			name:       "foreign user is rejected for non-root",
			targetUser: "bob",
			euid:       1000,
			lookupUser: &user.User{Username: "bob", Uid: "1001"},
			wantErr:    "only root may target another user",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			lookupCalled := false
			err := validateTargetUserPermission(
				tc.targetUser,
				func() int { return tc.euid },
				func(name string) (*user.User, error) {
					lookupCalled = true
					if name != tc.targetUser {
						t.Fatalf("lookup user name = %q, want %q", name, tc.targetUser)
					}
					return tc.lookupUser, nil
				},
			)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("validateTargetUserPermission() returned error: %v", err)
				}
				return
			}

			if err == nil {
				t.Fatalf("validateTargetUserPermission() returned nil error")
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("error %q does not contain %q", err.Error(), tc.wantErr)
			}
			if !lookupCalled {
				t.Fatalf("validateTargetUserPermission() did not call user lookup")
			}
		})
	}
}

func TestValidateTargetUserPermissionEmptyTargetSkipsLookup(t *testing.T) {
	lookupCalled := false
	err := validateTargetUserPermission(
		"  ",
		func() int { return 1000 },
		func(string) (*user.User, error) {
			lookupCalled = true
			return nil, errors.New("lookup should not be called")
		},
	)
	if err != nil {
		t.Fatalf("validateTargetUserPermission() returned error: %v", err)
	}
	if lookupCalled {
		t.Fatalf("validateTargetUserPermission() called lookup for empty target")
	}
}

func TestResolveSystemdUserUnitDirUsesTargetUserHome(t *testing.T) {
	dir, err := resolveSystemdUserUnitDir(
		"  alice  ",
		func(string) string {
			t.Fatalf("resolveSystemdUserUnitDir() should not read XDG_CONFIG_HOME for explicit target user")
			return ""
		},
		func() (string, error) {
			t.Fatalf("resolveSystemdUserUnitDir() should not resolve caller home for explicit target user")
			return "", nil
		},
		func(name string) (*user.User, error) {
			if name != "alice" {
				t.Fatalf("lookup user name = %q, want %q", name, "alice")
			}
			return &user.User{
				Username: "alice",
				Uid:      "1001",
				HomeDir:  "/home/alice",
			}, nil
		},
	)
	if err != nil {
		t.Fatalf("resolveSystemdUserUnitDir() error = %v", err)
	}

	want := filepath.Join("/home/alice", ".config", "systemd", "user")
	if dir != want {
		t.Fatalf("resolveSystemdUserUnitDir() = %q, want %q", dir, want)
	}
}

func TestResolveTargetUIDCurrentAndTargetUser(t *testing.T) {
	t.Run("current uid when target user is empty", func(t *testing.T) {
		uid, err := resolveTargetUID(
			" ",
			func() int { return 1000 },
			func(string) (*user.User, error) {
				t.Fatalf("resolveTargetUID() should not call lookup for empty target user")
				return nil, nil
			},
		)
		if err != nil {
			t.Fatalf("resolveTargetUID() error = %v", err)
		}
		if uid != 1000 {
			t.Fatalf("resolveTargetUID() = %d, want %d", uid, 1000)
		}
	})

	t.Run("target uid from lookup", func(t *testing.T) {
		uid, err := resolveTargetUID(
			"bob",
			func() int {
				t.Fatalf("resolveTargetUID() should not call getuid for explicit target user")
				return 0
			},
			func(name string) (*user.User, error) {
				if name != "bob" {
					t.Fatalf("lookup user name = %q, want %q", name, "bob")
				}
				return &user.User{Username: "bob", Uid: "1001"}, nil
			},
		)
		if err != nil {
			t.Fatalf("resolveTargetUID() error = %v", err)
		}
		if uid != 1001 {
			t.Fatalf("resolveTargetUID() = %d, want %d", uid, 1001)
		}
	})
}
