package version

import (
	"runtime/debug"
	"strings"
)

var (
	// Version is set at build time using -ldflags.
	Version = "dev"
	// Commit is set at build time using -ldflags.
	Commit = "unknown"
	// Date is set at build time using -ldflags.
	Date = "unknown"
)

func init() {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return
	}

	settings := make(map[string]string, len(info.Settings))
	for _, item := range info.Settings {
		settings[item.Key] = item.Value
	}

	Version, Commit, Date = mergeBuildMetadata(Version, Commit, Date, info.Main.Version, settings)
}

func mergeBuildMetadata(
	version string,
	commit string,
	date string,
	mainVersion string,
	settings map[string]string,
) (string, string, string) {
	if isVersionUnset(version) {
		candidate := strings.TrimSpace(mainVersion)
		if candidate != "" && candidate != "(devel)" {
			version = candidate
		}
	}

	if isMetadataUnset(commit) {
		if revision := strings.TrimSpace(settings["vcs.revision"]); revision != "" {
			commit = shortenRevision(revision)
			if strings.EqualFold(strings.TrimSpace(settings["vcs.modified"]), "true") {
				commit += "-dirty"
			}
		}
	}

	if isMetadataUnset(date) {
		if vcsTime := strings.TrimSpace(settings["vcs.time"]); vcsTime != "" {
			date = vcsTime
		}
	}

	return version, commit, date
}

func isVersionUnset(value string) bool {
	switch strings.TrimSpace(strings.ToLower(value)) {
	case "", "dev":
		return true
	default:
		return false
	}
}

func isMetadataUnset(value string) bool {
	switch strings.TrimSpace(strings.ToLower(value)) {
	case "", "unknown":
		return true
	default:
		return false
	}
}

func shortenRevision(value string) string {
	const shortHashLength = 12

	trimmed := strings.TrimSpace(value)
	if len(trimmed) <= shortHashLength {
		return trimmed
	}

	return trimmed[:shortHashLength]
}
