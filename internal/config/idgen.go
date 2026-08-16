package config

import (
	"path"
	"strings"
)

// Generated IDs end up in unit file names and in every `timertab status` line, so
// they are optimized for a human reading them later: a job whose command is a
// single script gets that script's name, wrapper and interpreter layers are peeled
// away to reach the interesting program, and only genuinely opaque commands fall
// back to a content digest.

const (
	maxIDLength     = 64
	runSlugMaxWords = 3
	unwrapMaxDepth  = 4
	digestSuffixLen = 6
)

// scriptExtensions are dropped from a command basename because they describe the
// implementation language, not the job.
var scriptExtensions = map[string]struct{}{
	".sh": {}, ".bash": {}, ".zsh": {}, ".fish": {},
	".py": {}, ".pl": {}, ".rb": {}, ".php": {}, ".lua": {},
	".js": {}, ".mjs": {}, ".cjs": {}, ".ts": {},
}

// wrappers run another command and carry no meaning of their own. positionals is
// the number of non-flag operands consumed before the wrapped command starts
// (`timeout 30 cmd`, `flock /var/lock/x cmd`).
var wrappers = map[string]int{
	"env":         0,
	"nohup":       0,
	"setsid":      0,
	"stdbuf":      0,
	"nice":        0,
	"ionice":      0,
	"chrt":        0,
	"sudo":        0,
	"doas":        0,
	"timeout":     1,
	"flock":       1,
	"systemd-cat": 0,
}

// interpreters execute a script or module named by a later argument.
var interpreters = map[string]struct{}{
	"python": {}, "node": {}, "deno": {}, "bun": {},
	"ruby": {}, "perl": {}, "php": {}, "lua": {}, "tclsh": {},
}

var shells = map[string]struct{}{
	"sh": {}, "bash": {}, "zsh": {}, "dash": {}, "ksh": {}, "fish": {},
}

// genericCommands are real programs whose bare name says nothing about the job, so
// an ID derived from one alone is disambiguated with a digest instead of being
// presented as if it were descriptive.
var genericCommands = map[string]struct{}{
	"ssh": {}, "scp": {}, "sftp": {}, "rsync": {}, "curl": {}, "wget": {},
	"find": {}, "xargs": {}, "cp": {}, "mv": {}, "rm": {}, "ln": {}, "mkdir": {},
	"cat": {}, "echo": {}, "tee": {}, "sleep": {}, "test": {}, "true": {},
	"tar": {}, "gzip": {}, "zip": {}, "unzip": {}, "dd": {}, "mount": {},
	"docker": {}, "podman": {}, "git": {}, "systemctl": {}, "journalctl": {},
	"kubectl": {}, "helm": {}, "make": {}, "npm": {}, "npx": {}, "yarn": {},
	"pnpm": {}, "pip": {}, "apt": {}, "apt-get": {}, "dnf": {}, "flatpak": {},
	"snap": {}, "java": {}, "go": {}, "cargo": {},
}

// shellMetaChars mark a shell command as more than a plain invocation. Pipelines,
// redirections, substitutions and globs cannot be reduced to a name without lying
// about what the job does, so those keep the digest fallback. Quoting is fine and
// is resolved by lexShellWords.
const shellMetaChars = "|&;<>()$`*?[]{}~\n\r"

// runSlug derives an ID candidate from a job's command. weak reports that the slug
// is too generic to identify the job on its own and needs a digest suffix.
func runSlug(run RunCommand) (slug string, weak bool) {
	if shell, ok := run.Shell(); ok {
		return slugFromShell(shell, 0)
	}
	return slugFromArgv(run.Argv(), 0)
}

func slugFromShell(command string, depth int) (string, bool) {
	if depth > unwrapMaxDepth {
		return "", false
	}
	words, ok := lexShellWords(command)
	if !ok {
		return "", false
	}
	return slugFromArgv(words, depth)
}

// lexShellWords splits a shell command into words, resolving quotes but refusing
// anything with real shell syntax in it. ok is false when the command does more
// than invoke a program with literal arguments.
func lexShellWords(command string) (words []string, ok bool) {
	var current strings.Builder
	started := false
	flush := func() {
		if started {
			words = append(words, current.String())
			current.Reset()
			started = false
		}
	}

	runes := []rune(command)
	for idx := 0; idx < len(runes); idx++ {
		r := runes[idx]
		switch {
		case r == ' ' || r == '\t':
			flush()
		case r == '\'':
			end := indexRune(runes, idx+1, '\'')
			if end < 0 {
				return nil, false
			}
			current.WriteString(string(runes[idx+1 : end]))
			started = true
			idx = end
		case r == '"':
			end := indexRune(runes, idx+1, '"')
			if end < 0 {
				return nil, false
			}
			inner := string(runes[idx+1 : end])
			// A double-quoted word may still expand or escape; treat those as syntax.
			if strings.ContainsAny(inner, "$`\\") {
				return nil, false
			}
			current.WriteString(inner)
			started = true
			idx = end
		case strings.ContainsRune(shellMetaChars, r), r == '\\', r == '#', r == '=' && !started:
			return nil, false
		default:
			current.WriteRune(r)
			started = true
		}
	}
	flush()

	if len(words) == 0 {
		return nil, false
	}
	return words, true
}

func indexRune(runes []rune, from int, target rune) int {
	for idx := from; idx < len(runes); idx++ {
		if runes[idx] == target {
			return idx
		}
	}
	return -1
}

// slugFromCommandTokens interprets already-split tokens, falling back to shell
// parsing when a single token still holds a whole command line (`ssh host "a b"`).
func slugFromCommandTokens(tokens []string, depth int) (string, bool) {
	if len(tokens) == 1 && strings.ContainsAny(tokens[0], " \t") {
		return slugFromShell(tokens[0], depth)
	}
	return slugFromArgv(tokens, depth)
}

func slugFromArgv(argv []string, depth int) (string, bool) {
	if depth > unwrapMaxDepth {
		return "", false
	}

	// Leading VAR=VAL pairs are environment, not the command.
	for len(argv) > 0 && isAssignment(argv[0]) {
		argv = argv[1:]
	}
	if len(argv) == 0 || strings.TrimSpace(argv[0]) == "" {
		return "", false
	}

	base := commandBase(argv[0])
	rest := argv[1:]

	if skip, ok := wrappers[base]; ok {
		return slugFromArgv(dropWrapperArgs(rest, skip), depth+1)
	}
	if _, ok := shells[base]; ok {
		return slugFromShellFlag(rest, depth)
	}
	if _, ok := interpreters[base]; ok {
		if slug, weak, ok := slugFromInterpreter(base, rest); ok {
			return slug, weak
		}
	}
	if base == "ssh" {
		if slug, ok := slugFromSSH(rest, depth); ok {
			return slug, false
		}
	}

	words := append([]string{base}, subcommandWords(rest)...)
	slug := slugify(strings.Join(words, "-"))
	if slug == "" {
		return "", false
	}
	return slug, isWeakSlug(words)
}

// slugFromShellFlag handles `sh -c "..."` and `sh script.sh` alike.
func slugFromShellFlag(rest []string, depth int) (string, bool) {
	for idx, arg := range rest {
		if strings.HasPrefix(arg, "-") {
			// -c, -lc, -euc and friends all take the command as the next word.
			if strings.HasSuffix(arg, "c") && idx+1 < len(rest) {
				return slugFromShell(rest[idx+1], depth+1)
			}
			continue
		}
		return slugify(commandBase(arg)), false
	}
	return "", false
}

// slugFromInterpreter names the script or module the interpreter was pointed at.
// The bool reports whether a name could be found at all.
func slugFromInterpreter(base string, rest []string) (string, bool, bool) {
	for idx := 0; idx < len(rest); idx++ {
		arg := rest[idx]
		switch {
		case arg == "-m" && idx+1 < len(rest):
			if slug := slugify(trimModuleEntrypoint(rest[idx+1])); slug != "" {
				return slug, false, true
			}
			return "", false, false
		case strings.HasPrefix(arg, "-"):
			continue
		case (base == "deno" || base == "bun") && isRunnerSubcommand(arg):
			continue
		default:
			if slug := slugify(commandBase(arg)); slug != "" {
				return slug, false, true
			}
			return "", false, false
		}
	}
	return "", false, false
}

// sshValueFlags take a separate operand, so the operand must not be mistaken for
// the target host.
var sshValueFlags = map[string]struct{}{
	"-p": {}, "-i": {}, "-l": {}, "-o": {}, "-F": {}, "-c": {},
	"-b": {}, "-D": {}, "-E": {}, "-e": {}, "-I": {}, "-J": {},
	"-L": {}, "-m": {}, "-O": {}, "-Q": {}, "-R": {}, "-S": {}, "-W": {}, "-w": {},
}

// slugFromSSH names a remote job after the command it runs remotely, because that
// is what the job is about; the host is the fallback when the remote command is
// too shell-heavy to name.
func slugFromSSH(rest []string, depth int) (string, bool) {
	host := ""
	for idx := 0; idx < len(rest); idx++ {
		arg := rest[idx]
		if strings.HasPrefix(arg, "-") {
			if _, ok := sshValueFlags[arg]; ok {
				idx++
			}
			continue
		}
		if host == "" {
			host = arg
			continue
		}
		if slug, _ := slugFromCommandTokens(rest[idx:], depth+1); slug != "" {
			return "ssh-" + slug, true
		}
		break
	}

	if slug := slugify(hostLabel(host)); slug != "" {
		return "ssh-" + slug, true
	}
	return "", false
}

// hostLabel drops the user and port from an ssh destination: `runner@localhost`
// and `ssh://runner@localhost:22` both reduce to `localhost`.
func hostLabel(destination string) string {
	destination = strings.TrimPrefix(destination, "ssh://")
	if idx := strings.LastIndex(destination, "@"); idx >= 0 {
		destination = destination[idx+1:]
	}
	if idx := strings.LastIndex(destination, ":"); idx >= 0 {
		destination = destination[:idx]
	}
	return destination
}

func isRunnerSubcommand(arg string) bool {
	switch arg {
	case "run", "task", "exec", "x":
		return true
	}
	return false
}

// dropWrapperArgs removes a wrapper's own flags and operands, leaving the wrapped
// command. Flags that take a separate value (`nice -n 10`) consume a numeric word.
func dropWrapperArgs(argv []string, positionals int) []string {
	for len(argv) > 0 {
		arg := argv[0]
		switch {
		case isAssignment(arg):
			argv = argv[1:]
		case strings.HasPrefix(arg, "-"):
			argv = argv[1:]
			if !strings.Contains(arg, "=") && len(argv) > 0 && isFlagValue(argv[0]) {
				argv = argv[1:]
			}
		case positionals > 0:
			positionals--
			argv = argv[1:]
		default:
			return argv
		}
	}
	return nil
}

// subcommandWords collects the leading verb-like arguments that distinguish
// multi-command tools (`docker builder prune`, `systemctl --user restart foo`).
// Flags are stepped over, but anything path-like or otherwise unnameable ends the
// scan: those identify data rather than the action.
func subcommandWords(rest []string) []string {
	words := make([]string, 0, runSlugMaxWords-1)
	for _, arg := range rest {
		if len(words) >= runSlugMaxWords-1 {
			break
		}
		if strings.HasPrefix(arg, "-") {
			continue
		}
		if !isWordLike(arg) {
			break
		}
		words = append(words, arg)
	}
	return words
}

// commandBase reduces an executable reference to its bare name: `/usr/bin/docker`
// and `/home/me/bin/publish-metrics.py` become `docker` and `publish-metrics`.
func commandBase(command string) string {
	base := path.Base(strings.TrimSpace(command))
	if ext := path.Ext(base); ext != "" {
		if _, ok := scriptExtensions[strings.ToLower(ext)]; ok {
			base = strings.TrimSuffix(base, ext)
		}
	}
	return normalizeVersionedName(strings.ToLower(base))
}

// normalizeVersionedName folds `python3`, `python3.11` and `php8` onto the family
// name so version bumps do not rewrite IDs.
func normalizeVersionedName(base string) string {
	trimmed := strings.TrimRight(base, "0123456789.")
	if trimmed == base || trimmed == "" {
		return base
	}
	if _, ok := interpreters[trimmed]; ok {
		return trimmed
	}
	return base
}

// trimModuleEntrypoint turns `llm_usage_reporter.cli` into `llm_usage_reporter`:
// the trailing entrypoint component is boilerplate shared by every such module.
func trimModuleEntrypoint(module string) string {
	for {
		idx := strings.LastIndex(module, ".")
		if idx <= 0 {
			return module
		}
		switch module[idx+1:] {
		case "cli", "main", "__main__", "app":
			module = module[:idx]
		default:
			return module
		}
	}
}

func isAssignment(arg string) bool {
	idx := strings.Index(arg, "=")
	if idx <= 0 || strings.HasPrefix(arg, "-") || strings.ContainsAny(arg[:idx], "/ ") {
		return false
	}
	return validEnv.MatchString(arg[:idx])
}

func isFlagValue(arg string) bool {
	if arg == "" || strings.HasPrefix(arg, "-") {
		return false
	}
	return strings.IndexFunc(arg, func(r rune) bool {
		return !(r >= '0' && r <= '9') && r != '.' && r != ':'
	}) == -1
}

func isWordLike(arg string) bool {
	if arg == "" || len(arg) > 24 || strings.HasPrefix(arg, "-") {
		return false
	}
	for _, r := range arg {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-', r == '_', r == '.':
		default:
			return false
		}
	}
	return true
}

// isWeakSlug reports that the derived words identify a generic tool rather than
// this job, e.g. a bare `ssh` or `docker` with no distinguishing subcommand.
func isWeakSlug(words []string) bool {
	if len(words) > 1 {
		return false
	}
	if len(words[0]) <= 2 {
		return true
	}
	_, generic := genericCommands[words[0]]
	return generic
}

// withDigestSuffix appends a short content digest, trimming the base so the result
// still fits the ID length limit.
func withDigestSuffix(base, digest string) string {
	if len(digest) > digestSuffixLen {
		digest = digest[:digestSuffixLen]
	}
	if max := maxIDLength - len(digest) - 1; len(base) > max {
		base = strings.TrimRight(base[:max], "-")
	}
	if base == "" {
		return "job-" + digest
	}
	return base + "-" + digest
}
