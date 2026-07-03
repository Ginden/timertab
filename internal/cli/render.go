package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/spf13/cobra"

	"github.com/ginden/timertab/internal/config"
	"github.com/ginden/timertab/internal/systemd"
)

const defaultRenderUID uint32 = 1000

func newRenderCommand() *cobra.Command {
	var (
		fromStdin bool
		outputDir string
		uid       uint32
	)

	cmd := &cobra.Command{
		Use:   "render",
		Short: "Import crontab and render a reviewable bundle of systemd units",
		Long: `Render converts crontab input into a directory of systemd unit files,
the generated timertab.yaml source, and a human-readable report.

This command never touches live systemd unit directories or calls systemctl.
It works without systemd installed at all.

Example (Docker):
  crontab -l | docker run --rm -i -v "$PWD/output:/output" ghcr.io/ginden/timertab-import`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			rawCrontab, err := loadCrontabInput(cmd.Context(), cmd.InOrStdin(), fromStdin)
			if err != nil {
				return err
			}

			imported, warnings, err := importCrontab(rawCrontab)
			if err != nil {
				return err
			}

			return renderBundle(cmd, imported, warnings, outputDir, uid)
		},
	}

	cmd.Flags().BoolVar(&fromStdin, "stdin", false, "Read crontab input from stdin")
	cmd.Flags().StringVarP(&outputDir, "output", "o", "output", "Output directory for the rendered bundle")
	cmd.Flags().Uint32Var(&uid, "uid", defaultRenderUID, "UID to embed in generated unit names")

	return cmd
}

func renderBundle(cmd *cobra.Command, cfg *config.File, importWarnings []string, outputDir string, uid uint32) error {
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return fmt.Errorf("create output directory: %w", err)
	}

	instanceID := cfg.EffectiveInstanceID()

	// Write timertab.yaml
	yamlBytes, err := cfg.MarshalYAML()
	if err != nil {
		return fmt.Errorf("marshal yaml: %w", err)
	}
	yamlPath := filepath.Join(outputDir, "timertab.yaml")
	if err := os.WriteFile(yamlPath, yamlBytes, 0o644); err != nil {
		return err
	}

	// Render and write unit files
	rendered := make([]renderedJob, 0, len(cfg.Jobs))
	var renderWarnings []string

	for idx, job := range cfg.Jobs {
		units, err := systemd.RenderJobUnits(uid, instanceID, job)
		if err != nil {
			renderWarnings = append(renderWarnings, fmt.Sprintf("jobs[%d] %q: render failed: %v", idx, job.ID, err))
			continue
		}
		rendered = append(rendered, renderedJob{job: job, units: units})

		servicePath := filepath.Join(outputDir, units.ServiceName)
		if err := os.WriteFile(servicePath, []byte(units.ServiceContent), 0o644); err != nil {
			return err
		}

		timerPath := filepath.Join(outputDir, units.TimerName)
		if err := os.WriteFile(timerPath, []byte(units.TimerContent), 0o644); err != nil {
			return err
		}
	}

	// Generate and write report
	report := buildRenderReport(cfg, rendered, importWarnings, renderWarnings, uid, instanceID)
	reportPath := filepath.Join(outputDir, "REPORT.md")
	if err := os.WriteFile(reportPath, []byte(report), 0o644); err != nil {
		return err
	}

	// Print summary to stderr
	cmd.Printf("rendered %d job(s) to %s\n", len(rendered), colorizeRenderOutputPath(cmd, outputDir))
	for _, w := range importWarnings {
		_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "%s %s\n", warningPrefix, w)
	}
	for _, w := range renderWarnings {
		_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "%s %s\n", warningPrefix, w)
	}

	return nil
}

func buildRenderReport(
	cfg *config.File,
	rendered []renderedJob,
	importWarnings []string,
	renderWarnings []string,
	uid uint32,
	instanceID string,
) string {
	var b strings.Builder

	b.WriteString("# timertab import review bundle\n\n")

	// Summary
	b.WriteString("## Summary\n\n")
	fmt.Fprintf(&b, "- **Jobs imported:** %d\n", len(rendered))
	if len(importWarnings) > 0 || len(renderWarnings) > 0 {
		fmt.Fprintf(&b, "- **Warnings:** %d\n", len(importWarnings)+len(renderWarnings))
	}
	fmt.Fprintf(&b, "- **Target UID:** %d\n", uid)
	fmt.Fprintf(&b, "- **Instance ID:** %s\n", instanceID)
	b.WriteString("\n")

	// Imported jobs sections
	if len(rendered) > 0 {
		b.WriteString("## Imported Jobs\n\n")

		for idx, r := range rendered {
			fmt.Fprintf(&b, "### %d. %s\n\n", idx+1, reportJobTitle(r.job))
			fmt.Fprintf(&b, "- Schedule: %s\n", markdownScheduleList(r.job.When))
			fmt.Fprintf(&b, "- Job ID: `%s`\n", r.job.ID)
			fmt.Fprintf(&b, "- Service unit: `%s`\n", r.units.ServiceName)
			fmt.Fprintf(&b, "- Timer unit: `%s`\n", r.units.TimerName)
			b.WriteString("- Command:\n\n```sh\n")
			b.WriteString(strings.TrimSpace(r.job.Run.Display()))
			b.WriteString("\n```\n\n")
		}
	}

	// Files listing
	b.WriteString("## Generated Files\n\n")
	b.WriteString("- `timertab.yaml` — YAML source used to render units\n")
	b.WriteString("- One `.service` and one `.timer` file per imported job (see job sections above for exact filenames)\n")
	b.WriteString("- `REPORT.md` — this report\n")
	b.WriteString("\n")

	// Warnings
	allWarnings := make([]string, 0, len(importWarnings)+len(renderWarnings))
	allWarnings = append(allWarnings, importWarnings...)
	allWarnings = append(allWarnings, renderWarnings...)
	if len(allWarnings) > 0 {
		b.WriteString("## Warnings\n\n")
		for _, w := range allWarnings {
			fmt.Fprintf(&b, "- %s\n", w)
		}
		b.WriteString("\n")
	}

	// Cron vs systemd caveats
	b.WriteString("## Cron vs systemd.timer Differences\n\n")
	b.WriteString("The following semantic differences between cron and systemd timers may affect behavior:\n\n")

	caveats := collectCaveats(cfg)
	for _, caveat := range caveats {
		fmt.Fprintf(&b, "- **%s:** %s\n", caveat.title, caveat.detail)
	}
	b.WriteString("\n")

	return b.String()
}

type renderedJob struct {
	job   config.Job
	units systemd.RenderedUnits
}

type caveat struct {
	title  string
	detail string
}

func collectCaveats(cfg *config.File) []caveat {
	out := []caveat{
		{
			title:  "Environment",
			detail: "Cron sources the user's crontab env (MAILTO, SHELL, etc). systemd services run with a minimal environment. Variables are set explicitly via `Environment=` directives.",
		},
		{
			title:  "Shell",
			detail: "Cron uses `/bin/sh` (or `$SHELL`). timertab string `run` values use `/bin/sh -lc`, while list-form `run` values execute exact argv without an extra shell.",
		},
		{
			title:  "Output handling",
			detail: "Cron emails stdout/stderr via `MAILTO`. systemd captures output in the journal (`journalctl --user -u <unit>`).",
		},
		{
			title:  "Missed runs",
			detail: "Cron does not catch up on missed runs. systemd timers with `Persistent=true` will fire once on next boot if a scheduled run was missed while the machine was off.",
		},
	}

	hasReboot := false
	hasDOWAndDOM := false
	for _, job := range cfg.Jobs {
		for _, schedule := range job.When {
			if schedule == "@reboot" {
				hasReboot = true
			}
			fields := strings.Fields(schedule)
			if len(fields) == 5 {
				dom := fields[2]
				dow := fields[4]
				if dom != "*" && dow != "*" {
					hasDOWAndDOM = true
				}
			}
		}
	}

	if hasReboot {
		out = append(out, caveat{
			title:  "@reboot semantics",
			detail: "`@reboot` in cron runs once per system boot. The systemd equivalent (`OnBootSec=0`) fires once after the user session starts, which requires `loginctl enable-linger` for headless operation. timertab enables @reboot-only timers during apply but does not start them immediately; use `timertab trigger <id>` when you want an immediate run.",
		})
	}

	if hasDOWAndDOM {
		out = append(out, caveat{
			title:  "Day-of-month AND day-of-week",
			detail: "Cron ORs day-of-month and day-of-week when both are specified. timertab emits two separate `OnCalendar=` lines to preserve this OR semantic, but the resulting behavior may differ in edge cases.",
		})
	}

	sort.Slice(out, func(i, j int) bool { return out[i].title < out[j].title })

	return out
}

func reportJobTitle(job config.Job) string {
	if name := strings.TrimSpace(job.Name); name != "" {
		return name
	}

	command := job.Run.Display()
	summary := strings.Join(strings.Fields(firstLine(command)), " ")
	if summary == "" {
		return "(unnamed job)"
	}

	if strings.Contains(strings.TrimSpace(command), "\n") {
		summary += " ..."
	}

	return truncateRunes(summary, 72)
}

func markdownScheduleList(schedules config.ScheduleList) string {
	if len(schedules) == 0 {
		return "(none)"
	}

	quoted := make([]string, 0, len(schedules))
	for _, schedule := range schedules {
		quoted = append(quoted, fmt.Sprintf("`%s`", schedule))
	}

	return strings.Join(quoted, ", ")
}

func firstLine(text string) string {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return ""
	}

	if idx := strings.IndexByte(trimmed, '\n'); idx >= 0 {
		return trimmed[:idx]
	}

	return trimmed
}

func truncateRunes(text string, max int) string {
	if max <= 0 || utf8.RuneCountInString(text) <= max {
		return text
	}
	if max <= 3 {
		return strings.Repeat(".", max)
	}

	runes := []rune(text)
	return string(runes[:max-3]) + "..."
}

func colorizeRenderOutputPath(cmd *cobra.Command, path string) string {
	display := displayCLIPath(path)
	if !commandAllowsColor(cmd, cmd.OutOrStdout()) {
		return display
	}
	return "\x1b[1;36m" + display + "\x1b[0m"
}

func displayCLIPath(path string) string {
	cleaned := filepath.Clean(strings.TrimSpace(path))
	if cleaned == "." || cleaned == ".." {
		return cleaned
	}
	if filepath.IsAbs(cleaned) {
		return cleaned
	}
	if strings.HasPrefix(cleaned, "."+string(filepath.Separator)) || strings.HasPrefix(cleaned, ".."+string(filepath.Separator)) {
		return cleaned
	}
	return "." + string(filepath.Separator) + cleaned
}
