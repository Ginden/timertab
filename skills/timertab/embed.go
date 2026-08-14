// Package timertabskill exposes the repository's timertab skill to the CLI.
package timertabskill

import _ "embed"

// Content is the complete timertab skill definition.
//
//go:embed SKILL.md
var Content string
