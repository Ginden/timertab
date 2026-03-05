package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/ginden/timertab/internal/config"
)

var runSystemctlApply = applyEditedConfig

func listConfig(cmd *cobra.Command, cfgPath string) error {
	data, err := os.ReadFile(cfgPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			cmd.Println("# no timertab file found")
			return nil
		}
		return err
	}

	cmd.Printf("# %s\n", cfgPath)
	_, err = cmd.OutOrStdout().Write(data)
	if err != nil {
		return err
	}

	if len(data) == 0 || data[len(data)-1] != '\n' {
		cmd.Println("")
	}

	return nil
}

func editConfig(cmd *cobra.Command, cfgPath, targetUser string, noApply bool) error {
	if err := os.MkdirAll(filepath.Dir(cfgPath), 0o755); err != nil {
		return err
	}

	tmpFile, err := os.CreateTemp("", "timertab-edit-*.yaml")
	if err != nil {
		return err
	}
	tmpName := tmpFile.Name()
	defer os.Remove(tmpName)

	existing, err := os.ReadFile(cfgPath)
	if err == nil {
		if _, err := tmpFile.Write(existing); err != nil {
			_ = tmpFile.Close()
			return err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		_ = tmpFile.Close()
		return err
	} else {
		if _, err := io.WriteString(tmpFile, config.DefaultTemplate); err != nil {
			_ = tmpFile.Close()
			return err
		}
	}

	if err := tmpFile.Close(); err != nil {
		return err
	}

	editor, err := resolveEditor()
	if err != nil {
		return err
	}

	for {
		// Run via shell so values like EDITOR=\"code --wait\" work.
		editCmd := exec.Command("sh", "-lc", `$EDITOR_CMD "$1"`, "timertab-editor", tmpName)
		editCmd.Env = append(os.Environ(), "EDITOR_CMD="+editor)
		editCmd.Stdin = cmd.InOrStdin()
		editCmd.Stdout = cmd.OutOrStdout()
		editCmd.Stderr = cmd.ErrOrStderr()
		if err := editCmd.Run(); err != nil {
			return fmt.Errorf("editor failed: %w", err)
		}

		loaded, err := config.LoadFromFile(tmpName)
		if err != nil {
			printEditValidationError(cmd, err)
			continue
		}

		if err := loaded.NormalizeIDs(); err != nil {
			printEditValidationError(cmd, err)
			continue
		}

		out, err := loaded.MarshalYAML()
		if err != nil {
			return err
		}

		if err := os.WriteFile(cfgPath, out, 0o644); err != nil {
			return err
		}

		if noApply {
			cmd.Printf("timertab: saved %s (no apply)\n", cfgPath)
			return nil
		}

		if err := runSystemctlApply(cmd.Context(), loaded, targetUser); err != nil {
			return err
		}

		cmd.Printf("timertab: saved %s\n", cfgPath)
		cmd.Println("timertab: applied systemd reconcile")
		return nil
	}
}

func printEditValidationError(cmd *cobra.Command, err error) {
	_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "timertab: config is invalid: %v\n", err)
	_, _ = fmt.Fprintln(cmd.ErrOrStderr(), "timertab: reopen editor to fix validation errors")
}

func resolveEditor() (string, error) {
	for _, key := range []string{"VISUAL", "EDITOR"} {
		value := strings.TrimSpace(os.Getenv(key))
		if value != "" {
			return value, nil
		}
	}

	if _, err := exec.LookPath("editor"); err == nil {
		return "editor", nil
	}
	if _, err := exec.LookPath("vi"); err == nil {
		return "vi", nil
	}

	return "", errors.New("no editor found; set $VISUAL or $EDITOR")
}
