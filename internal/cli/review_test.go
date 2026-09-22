package cli

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/ginden/timertab/internal/config"
	"github.com/ginden/timertab/internal/systemctl"
	"github.com/ginden/timertab/internal/systemd"
	"github.com/spf13/cobra"
)

func TestNoApplyEditsAndImportsAutoCommit(t *testing.T) {
	for _, action := range []string{"edit", "import"} {
		for _, noCommit := range []bool{false, true} {
			t.Run(action+map[bool]string{true: "-no-commit"}[noCommit], func(t *testing.T) {
				message, calls := stubGitForCommitCapture(t)
				t.Setenv("VISUAL", writeEditorScript(t, "exit 0"))
				cfgPath := filepath.Join(t.TempDir(), "timertab.yaml")
				cmd := &cobra.Command{}
				cmd.SetContext(context.Background())
				cmd.SetOut(io.Discard)
				cmd.SetErr(io.Discard)
				var err error
				if action == "edit" {
					err = editConfig(cmd, cfgPath, true, false, noCommit)
				} else {
					imported, _, importErr := importCrontab("@daily echo imported\n")
					if importErr != nil {
						t.Fatal(importErr)
					}
					err = importInteractive(cmd, cfgPath, imported, true, noCommit)
				}
				if err != nil {
					t.Fatal(err)
				}
				if noCommit && *calls != 0 {
					t.Fatalf("unexpected git calls: %d", *calls)
				}
				if !noCommit && *message == "" {
					t.Fatal("saved config was not committed")
				}
			})
		}
	}
}

func TestInvalidEditorInputEOFAbortsEditAndImport(t *testing.T) {
	for _, action := range []string{"edit", "import"} {
		t.Run(action, func(t *testing.T) {
			cfgPath := filepath.Join(t.TempDir(), "timertab.yaml")
			original := []byte("version: 1\njobs: []\n")
			if err := os.WriteFile(cfgPath, original, 0o600); err != nil {
				t.Fatal(err)
			}
			countFile := filepath.Join(t.TempDir(), "editor-ran")
			t.Setenv("TIMERTAB_TEST_EDITOR_RAN", countFile)
			t.Setenv("VISUAL", writeEditorScript(t, `
if [ -f "$TIMERTAB_TEST_EDITOR_RAN" ]; then exit 91; fi
touch "$TIMERTAB_TEST_EDITOR_RAN"
printf 'version: invalid\njobs: []\n' > "$1"
`))
			cmd := &cobra.Command{}
			cmd.SetContext(context.Background())
			cmd.SetIn(strings.NewReader(""))
			cmd.SetOut(io.Discard)
			cmd.SetErr(io.Discard)
			var err error
			if action == "edit" {
				err = editConfig(cmd, cfgPath, true, false, true)
			} else {
				err = importInteractive(cmd, cfgPath, &config.File{Version: 1, Jobs: []config.Job{}}, true, true)
			}
			if err == nil || !strings.Contains(err.Error(), "aborted") {
				t.Fatalf("got %v; want abort on EOF", err)
			}
			after, err := os.ReadFile(cfgPath)
			if err != nil || !bytes.Equal(after, original) {
				t.Fatalf("config changed: %q, %v", after, err)
			}
		})
	}
}

func TestRemoveAnchorDefinitionKeepsRemainingConfigValid(t *testing.T) {
	path := filepath.Join(t.TempDir(), "timertab.yaml")
	raw := []byte("version: 1\njobs:\n  - id: first\n    when: &daily '@daily'\n    run: echo first\n  - id: second\n    when: *daily\n    run: echo second\n")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := NewRootCommand()
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"rm", "first", "--config", path, "--no-apply", "--no-commit"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	loaded, err := config.LoadFromFile(path)
	if err != nil {
		t.Fatalf("saved broken alias: %v", err)
	}
	if len(loaded.Jobs) != 1 || loaded.Jobs[0].When[0] != "@daily" {
		t.Fatalf("lost remaining schedule: %+v", loaded.Jobs)
	}
}

func TestShowUnitDetectsSuccessfulNotFoundResponse(t *testing.T) {
	original := runSystemctlShow
	t.Cleanup(func() { runSystemctlShow = original })
	runSystemctlShow = func(_ context.Context, args ...string) (string, string, error) {
		if !strings.Contains(strings.Join(args, " "), "--property=LoadState") {
			t.Fatal("LoadState was not requested")
		}
		return "LoadState=not-found\nActiveState=inactive\n", "", nil
	}
	_, missing, err := showUnitProperties(context.Background(), systemctl.UserScope, "missing.timer", "ActiveState")
	if err != nil || !missing {
		t.Fatalf("got missing=%v, err=%v", missing, err)
	}
}

func TestWriteConfigAtomicallyPreservesSymlink(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "target.yaml")
	link := filepath.Join(dir, "timertab.yaml")
	if err := os.WriteFile(path, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("target.yaml", link); err != nil {
		t.Fatal(err)
	}
	old, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer old.Close()
	if err := writeConfigFile(link, []byte("new")); err != nil {
		t.Fatal(err)
	}
	before, err := io.ReadAll(old)
	if err != nil || string(before) != "old" {
		t.Fatalf("overwrote open original: %q, %v", before, err)
	}
	after, err := os.ReadFile(link)
	if err != nil || string(after) != "new" {
		t.Fatalf("new content: %q, %v", after, err)
	}
	if _, err := os.Readlink(link); err != nil {
		t.Fatalf("symlink replaced: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("target mode: %v, %v", info, err)
	}
}

func TestLegacyRewritePreservesSubcommandArguments(t *testing.T) {
	for _, args := range [][]string{
		{"-v", "add", "--when", "@daily", "--", "echo", "-e"},
		{"--color", "always", "logs", "demo", "--", "-l"},
		{"-v", "--", "-e"},
	} {
		got, err := rewriteLegacyRootArgs(args)
		if err != nil || !reflect.DeepEqual(got, args) {
			t.Fatalf("rewrote %v to %v, %v", args, got, err)
		}
	}
	got, err := rewriteLegacyRootArgs([]string{"--config", "-l", "-e"})
	want := []string{"edit", "--config", "-l"}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("flag value treated as legacy switch: %v, %v", got, err)
	}
}

func TestMarkerCommandsPreflightBothUnits(t *testing.T) {
	for _, action := range []string{"eject", "adopt"} {
		for _, conflict := range []string{"uid", "instance", "job", "symlink", "missing"} {
			if action == "eject" && conflict == "missing" {
				continue
			}
			t.Run(action+"-"+conflict, func(t *testing.T) {
				oldUID, oldDir := resolveCurrentUID, resolveSystemdUnitDir
				t.Cleanup(func() { resolveCurrentUID, resolveSystemdUnitDir = oldUID, oldDir })
				dir := t.TempDir()
				resolveCurrentUID = func() (uint32, error) { return 1000, nil }
				resolveSystemdUnitDir = func(uint32) (string, error) { return dir, nil }
				cfgPath := filepath.Join(t.TempDir(), "timertab.yaml")
				cfg := &config.File{Version: 1, Jobs: []config.Job{{ID: "demo", When: config.ScheduleList{"@daily"}, Run: config.ShellCommand("true")}}}
				if err := saveConfig(cfgPath, cfg); err != nil {
					t.Fatal(err)
				}
				cfgBefore, err := os.ReadFile(cfgPath)
				if err != nil {
					t.Fatal(err)
				}
				units, err := systemd.RenderJobUnits(1000, "", cfg.Jobs[0])
				if err != nil {
					t.Fatal(err)
				}
				service := units.ServiceContent
				if action == "adopt" {
					service, _ = stripManagedMarkers(service, 1000, "", "demo")
				}
				servicePath := filepath.Join(dir, units.ServiceName)
				if err := os.WriteFile(servicePath, []byte(service), 0o644); err != nil {
					t.Fatal(err)
				}
				timerPath := filepath.Join(dir, units.TimerName)
				timer := units.TimerContent
				switch conflict {
				case "uid":
					timer = strings.ReplaceAll(timer, "# timertab-uid: 1000", "# timertab-uid: 2000")
				case "instance":
					timer = strings.ReplaceAll(timer, "# timertab-instance-id: timertab", "# timertab-instance-id: other")
				case "job":
					timer = strings.ReplaceAll(timer, "# timertab-job-id: demo", "# timertab-job-id: other")
				}
				if conflict == "symlink" {
					if err := os.Symlink(servicePath, timerPath); err != nil {
						t.Fatal(err)
					}
				} else if conflict != "missing" {
					if err := os.WriteFile(timerPath, []byte(timer), 0o644); err != nil {
						t.Fatal(err)
					}
				}
				cmd := NewRootCommand()
				cmd.SetOut(io.Discard)
				cmd.SetErr(io.Discard)
				args := []string{action, "demo", "--config", cfgPath}
				if action == "adopt" {
					args = append(args, "--no-apply")
				} else {
					args = append(args, "--no-commit")
				}
				cmd.SetArgs(args)
				if err := cmd.Execute(); err == nil {
					t.Fatal("accepted conflicting/missing unit")
				}
				after, err := os.ReadFile(servicePath)
				if err != nil || string(after) != service {
					t.Fatalf("changed first unit before checking second: %v", err)
				}
				cfgAfter, err := os.ReadFile(cfgPath)
				if err != nil || !bytes.Equal(cfgAfter, cfgBefore) {
					t.Fatalf("changed config on failure: %v", err)
				}
			})
		}
	}
}
