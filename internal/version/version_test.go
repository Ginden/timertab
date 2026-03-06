package version

import "testing"

func TestMergeBuildMetadataUsesBuildInfoWhenUnset(t *testing.T) {
	settings := map[string]string{
		"vcs.revision": "0123456789abcdef0123456789abcdef01234567",
		"vcs.time":     "2026-03-06T12:34:56Z",
		"vcs.modified": "true",
	}

	gotVersion, gotCommit, gotDate := mergeBuildMetadata("dev", "unknown", "unknown", "v1.2.3", settings)

	if gotVersion != "v1.2.3" {
		t.Fatalf("version = %q, want %q", gotVersion, "v1.2.3")
	}
	if gotCommit != "0123456789ab-dirty" {
		t.Fatalf("commit = %q, want short hash with dirty marker", gotCommit)
	}
	if gotDate != "2026-03-06T12:34:56Z" {
		t.Fatalf("date = %q, want vcs.time", gotDate)
	}
}

func TestMergeBuildMetadataKeepsLdflagsValues(t *testing.T) {
	settings := map[string]string{
		"vcs.revision": "fedcba98765432100123456789abcdef01234567",
		"vcs.time":     "2026-03-06T12:34:56Z",
		"vcs.modified": "true",
	}

	gotVersion, gotCommit, gotDate := mergeBuildMetadata("v9.9.9", "abc123", "2025-01-01", "(devel)", settings)

	if gotVersion != "v9.9.9" {
		t.Fatalf("version = %q, want preserved value", gotVersion)
	}
	if gotCommit != "abc123" {
		t.Fatalf("commit = %q, want preserved value", gotCommit)
	}
	if gotDate != "2025-01-01" {
		t.Fatalf("date = %q, want preserved value", gotDate)
	}
}

func TestMergeBuildMetadataIgnoresDevelMainVersion(t *testing.T) {
	gotVersion, _, _ := mergeBuildMetadata("dev", "unknown", "unknown", "(devel)", map[string]string{})
	if gotVersion != "dev" {
		t.Fatalf("version = %q, want %q when main version is devel", gotVersion, "dev")
	}
}
