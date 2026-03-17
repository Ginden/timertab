package config

import (
	"encoding/json"
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

type runSyntax uint8

const (
	runSyntaxUnset runSyntax = iota
	runSyntaxShell
	runSyntaxArgv
)

// RunCommand stores a job command in either shell-shorthand or explicit argv form.
type RunCommand struct {
	shell  string
	argv   []string
	syntax runSyntax
}

func ShellCommand(command string) RunCommand {
	return RunCommand{
		shell:  command,
		argv:   []string{"/bin/sh", "-lc", command},
		syntax: runSyntaxShell,
	}
}

func ExecCommand(argv ...string) RunCommand {
	out := make([]string, len(argv))
	copy(out, argv)
	return RunCommand{
		argv:   out,
		syntax: runSyntaxArgv,
	}
}

func (r *RunCommand) UnmarshalYAML(value *yaml.Node) error {
	switch value.Kind {
	case yaml.ScalarNode:
		var command string
		if err := value.Decode(&command); err != nil {
			return err
		}
		*r = ShellCommand(command)
		return nil
	case yaml.SequenceNode:
		var argv []string
		if err := value.Decode(&argv); err != nil {
			return err
		}
		*r = ExecCommand(argv...)
		return nil
	default:
		return fmt.Errorf("must be a string or array")
	}
}

func (r RunCommand) MarshalYAML() (any, error) {
	switch {
	case r.syntax == runSyntaxShell:
		return r.shell, nil
	case r.syntax == runSyntaxArgv:
		return r.Argv(), nil
	default:
		if shell, ok := r.Shell(); ok {
			return shell, nil
		}
		if len(r.argv) > 0 {
			return r.Argv(), nil
		}
		return nil, nil
	}
}

func (r RunCommand) Argv() []string {
	if len(r.argv) == 0 {
		if r.syntax == runSyntaxShell || r.shell != "" {
			return []string{"/bin/sh", "-lc", r.shell}
		}
		return nil
	}

	out := make([]string, len(r.argv))
	copy(out, r.argv)
	return out
}

func (r RunCommand) Shell() (string, bool) {
	if r.syntax == runSyntaxShell {
		return r.shell, true
	}
	if len(r.argv) == 3 && r.argv[0] == "/bin/sh" && r.argv[1] == "-lc" {
		return r.argv[2], true
	}
	if r.syntax == runSyntaxUnset && r.shell != "" {
		return r.shell, true
	}
	return "", false
}

func (r RunCommand) IsZero() bool {
	return len(r.argv) == 0 && strings.TrimSpace(r.shell) == ""
}

func (r RunCommand) Validate() error {
	if shell, ok := r.Shell(); ok {
		if strings.TrimSpace(shell) == "" {
			return fmt.Errorf("run is required")
		}
		return nil
	}

	argv := r.Argv()
	if len(argv) == 0 {
		return fmt.Errorf("run is required")
	}
	if strings.TrimSpace(argv[0]) == "" {
		return fmt.Errorf("run executable is required")
	}
	return nil
}

func (r RunCommand) DigestKey() string {
	data, err := json.Marshal(r.Argv())
	if err != nil {
		return "[]"
	}
	return string(data)
}

func (r RunCommand) Display() string {
	if shell, ok := r.Shell(); ok {
		return shell
	}

	argv := r.Argv()
	if len(argv) == 0 {
		return ""
	}

	parts := make([]string, 0, len(argv))
	for _, arg := range argv {
		parts = append(parts, shellDisplayQuoted(arg))
	}
	return strings.Join(parts, " ")
}

func (r RunCommand) CanonicalArgv() RunCommand {
	if argv := r.Argv(); len(argv) > 0 {
		return ExecCommand(argv...)
	}
	return RunCommand{}
}

func shellDisplayQuoted(value string) string {
	if value == "" {
		return "''"
	}
	if strings.IndexFunc(value, func(r rune) bool {
		switch r {
		case ' ', '\t', '\n', '\r', '\'', '"', '\\':
			return true
		}
		return false
	}) == -1 {
		return value
	}
	return "'" + strings.ReplaceAll(value, "'", `'"'"'`) + "'"
}
