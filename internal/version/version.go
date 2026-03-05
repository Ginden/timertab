package version

var (
	// Version is set at build time using -ldflags.
	Version = "dev"
	// Commit is set at build time using -ldflags.
	Commit = "unknown"
	// Date is set at build time using -ldflags.
	Date = "unknown"
)
