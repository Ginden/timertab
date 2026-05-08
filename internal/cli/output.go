package cli

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/alecthomas/chroma/v2/formatters"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/alecthomas/chroma/v2/styles"
	"github.com/spf13/cobra"
)

type colorMode string

const (
	colorAuto   colorMode = "auto"
	colorAlways colorMode = "always"
	colorNever  colorMode = "never"
)

type outputPolicy struct {
	verbosity int
	color     colorMode
}

type outputPolicyKey struct{}

func withOutputPolicy(ctx context.Context, policy outputPolicy) context.Context {
	return context.WithValue(ctx, outputPolicyKey{}, policy)
}

func commandOutputPolicy(cmd *cobra.Command) outputPolicy {
	if cmd != nil && cmd.Context() != nil {
		if policy, ok := cmd.Context().Value(outputPolicyKey{}).(outputPolicy); ok {
			return policy
		}
	}
	return outputPolicy{color: colorAuto}
}

func validateColorMode(value string) (colorMode, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", string(colorAuto):
		return colorAuto, nil
	case string(colorAlways):
		return colorAlways, nil
	case string(colorNever):
		return colorNever, nil
	default:
		return "", fmt.Errorf("--color must be one of auto, always, or never")
	}
}

func commandAllowsColor(cmd *cobra.Command, out io.Writer) bool {
	return outputAllowsColor(commandOutputPolicy(cmd), out)
}

func outputAllowsColor(policy outputPolicy, out io.Writer) bool {
	switch policy.color {
	case colorAlways:
		return true
	case colorNever:
		return false
	default:
		if _, disabled := os.LookupEnv("NO_COLOR"); disabled {
			return false
		}
		return writerIsTTY(out)
	}
}

func writerIsTTY(w io.Writer) bool {
	file, ok := w.(*os.File)
	if !ok {
		return false
	}
	info, err := file.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

func highlightForCommand(cmd *cobra.Command, language, text string) string {
	out := cmd.OutOrStdout()
	if !commandAllowsColor(cmd, out) {
		return text
	}
	return highlight(language, text)
}

func highlightForPolicy(policy outputPolicy, out io.Writer, language, text string) string {
	if !outputAllowsColor(policy, out) {
		return text
	}
	return highlight(language, text)
}

func highlight(language, text string) string {
	lexer := lexers.Get(language)
	if lexer == nil {
		lexer = lexers.Fallback
	}
	iterator, err := lexer.Tokenise(nil, text)
	if err != nil {
		return text
	}
	style := styles.Get("github")
	if style == nil {
		style = styles.Fallback
	}

	var out bytes.Buffer
	formatter := formatters.TTY256
	if err := formatter.Format(&out, style, iterator); err != nil {
		return text
	}
	return out.String()
}
