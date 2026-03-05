package systemd

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestParseMajorVersion(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		input  string
		expect int
	}{
		{
			name: "standard output",
			input: `systemd 247 (247.3-7)
+PAM +AUDIT +SELINUX`,
			expect: 247,
		},
		{
			name: "dotted version token",
			input: `systemd 252.16-1
+PAM +AUDIT`,
			expect: 252,
		},
		{
			name: "rc style token",
			input: `systemd 260~rc2
+PAM`,
			expect: 260,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := ParseMajorVersion(tc.input)
			if err != nil {
				t.Fatalf("ParseMajorVersion() returned error: %v", err)
			}
			if got != tc.expect {
				t.Fatalf("ParseMajorVersion() = %d, want %d", got, tc.expect)
			}
		})
	}
}

func TestParseMajorVersionErrors(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		input string
	}{
		{
			name:  "empty output",
			input: "",
		},
		{
			name:  "invalid prefix",
			input: "dbus 1.12.2",
		},
		{
			name:  "missing version token",
			input: "systemd foo",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			_, err := ParseMajorVersion(tc.input)
			if err == nil {
				t.Fatalf("ParseMajorVersion() returned nil error for input %q", tc.input)
			}
		})
	}
}

func TestEnsureMinimumVersionUnsupported(t *testing.T) {
	t.Parallel()

	err := ensureMinimumVersion(context.Background(), MinimumSupportedVersion, func(context.Context) ([]byte, error) {
		return []byte("systemd 246 (246.10-1)"), nil
	})
	if err == nil {
		t.Fatalf("ensureMinimumVersion() returned nil error")
	}

	var unsupportedErr *UnsupportedVersionError
	if !errors.As(err, &unsupportedErr) {
		t.Fatalf("ensureMinimumVersion() error %T, want UnsupportedVersionError", err)
	}
	if unsupportedErr.Detected != 246 {
		t.Fatalf("Detected = %d, want 246", unsupportedErr.Detected)
	}
	if unsupportedErr.Minimum != MinimumSupportedVersion {
		t.Fatalf("Minimum = %d, want %d", unsupportedErr.Minimum, MinimumSupportedVersion)
	}
	if !strings.Contains(err.Error(), "--no-apply") {
		t.Fatalf("error message %q does not include remediation", err.Error())
	}
}

func TestEnsureMinimumVersionSupported(t *testing.T) {
	t.Parallel()

	err := ensureMinimumVersion(context.Background(), MinimumSupportedVersion, func(context.Context) ([]byte, error) {
		return []byte("systemd 247 (247.3-7)"), nil
	})
	if err != nil {
		t.Fatalf("ensureMinimumVersion() returned error: %v", err)
	}
}

func TestEnsureMinimumVersionParseFailure(t *testing.T) {
	t.Parallel()

	err := ensureMinimumVersion(context.Background(), MinimumSupportedVersion, func(context.Context) ([]byte, error) {
		return []byte("not-systemd"), nil
	})
	if err == nil {
		t.Fatalf("ensureMinimumVersion() returned nil error")
	}
	if !strings.Contains(err.Error(), "failed to parse 'systemctl --version' output") {
		t.Fatalf("error %q does not describe parse failure", err.Error())
	}
}
