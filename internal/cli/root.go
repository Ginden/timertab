package cli

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/ginden/timertab/internal/config"
	"github.com/ginden/timertab/internal/progress"
	"github.com/ginden/timertab/internal/systemd"
	"github.com/ginden/timertab/internal/version"
)

var ensureSystemdBaseline = systemd.EnsureBaseline
var resolveConfigPath = config.ResolvePath

func NewRootCommand() *cobra.Command {
	var (
		verbosity int
		color     string
	)

	cmd := &cobra.Command{
		Use:   "timertab",
		Short: "Manage systemd timers using a crontab-like workflow",
		Long:  "timertab is a crontab-like CLI that manages systemd timer/service units from a YAML config file.",
		Args:  cobra.NoArgs,
		PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
			colorMode, err := validateColorMode(color)
			if err != nil {
				return err
			}
			ctx := withOutputPolicy(cmd.Context(), outputPolicy{
				verbosity: verbosity,
				color:     colorMode,
			})
			ctx = progress.WithReporter(ctx, cmd.ErrOrStderr(), verbosity)
			cmd.SetContext(ctx)
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}

	cmd.SetErrPrefix("timertab")
	cmd.CompletionOptions.DisableDefaultCmd = false

	cmd.Version = fmt.Sprintf("%s (%s, %s)", version.Version, version.Commit, version.Date)
	cmd.SetVersionTemplate("{{printf \"%s\\n\" .Version}}")
	cmd.Flags().BoolP("version", "V", false, "Print the build version")
	cmd.PersistentFlags().CountVarP(&verbosity, "verbose", "v", "Increase verbosity; repeat for more detail")
	cmd.PersistentFlags().StringVar(&color, "color", string(colorAuto), "Colorize output: auto, always, or never")

	cmd.AddCommand(newListCommand())
	cmd.AddCommand(newEditCommand())
	cmd.AddCommand(newApplyCommand())
	cmd.AddCommand(newPrintPathCommand())
	cmd.AddCommand(newValidateCommand())
	cmd.AddCommand(newEjectCommand())
	cmd.AddCommand(newStatusCommand())
	cmd.AddCommand(newLogsCommand())
	cmd.AddCommand(newTriggerCommand())
	cmd.AddCommand(newEnableCommand())
	cmd.AddCommand(newDisableCommand())
	cmd.AddCommand(newAddCommand())
	cmd.AddCommand(newRemoveCommand())
	cmd.AddCommand(newDiffCommand())
	cmd.AddCommand(newImportCommand())
	cmd.AddCommand(newRenderCommand())
	cmd.InitDefaultCompletionCmd()

	return cmd
}

func rewriteLegacyRootArgs(args []string) ([]string, error) {
	if len(args) == 0 || !strings.HasPrefix(args[0], "-") {
		return args, nil
	}

	out := make([]string, 0, len(args)+1)
	var (
		hasEdit      bool
		hasList      bool
		hasPrintPath bool
	)

	for _, arg := range args {
		switch arg {
		case "-e", "--edit":
			hasEdit = true
		case "-l", "--list", "--print-config":
			hasList = true
		case "--print-path":
			hasPrintPath = true
		default:
			out = append(out, arg)
		}
	}

	selected := 0
	if hasEdit {
		selected++
	}
	if hasList {
		selected++
	}
	if hasPrintPath {
		selected++
	}
	if selected > 1 {
		return nil, errors.New("flags -e/--edit, -l/--list/--print-config, and --print-path are mutually exclusive")
	}

	switch {
	case hasEdit:
		return append([]string{"edit"}, out...), nil
	case hasList:
		return append([]string{"list"}, out...), nil
	case hasPrintPath:
		return append([]string{"print-path"}, out...), nil
	default:
		return args, nil
	}
}

func Execute() {
	cmd := NewRootCommand()
	rewrittenArgs, rewriteErr := rewriteLegacyRootArgs(os.Args[1:])
	if rewriteErr != nil {
		_, _ = fmt.Fprintf(os.Stderr, "%s %v\n", errorPrefix, rewriteErr)
		os.Exit(1)
	}
	cmd.SetArgs(rewrittenArgs)

	if err := cmd.Execute(); err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			_, _ = fmt.Fprintf(os.Stderr, "%s %v\n", errorPrefix, err)
		} else {
			_, _ = fmt.Fprintf(os.Stderr, "%s timertab: %v\n", errorPrefix, err)
		}
		os.Exit(1)
	}
}
