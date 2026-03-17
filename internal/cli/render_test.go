package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/ginden/timertab/internal/config"
	"github.com/ginden/timertab/internal/systemd"
)

func TestBuildRenderReportUsesReadableJobSections(t *testing.T) {
	t.Parallel()

	job := config.Job{
		ID:   "job-aaa9ac542175",
		When: config.ScheduleList{"*/5 * * * *"},
		Run:  config.ShellCommand("echo tick"),
	}
	cfg := &config.File{
		Version: 1,
		Jobs:    []config.Job{job},
	}

	units, err := systemd.RenderJobUnits(1000, config.DefaultInstanceID, job)
	if err != nil {
		t.Fatalf("RenderJobUnits() error = %v", err)
	}

	report := buildRenderReport(cfg, []renderedJob{{job: job, units: units}}, nil, nil, 1000, config.DefaultInstanceID)

	if strings.Contains(report, "| ID") {
		t.Fatalf("report should not use the old wide table format:\n%s", report)
	}
	if strings.Contains(report, "(none)") {
		t.Fatalf("report should not show placeholder names for unnamed jobs:\n%s", report)
	}
	if !strings.Contains(report, "### 1. echo tick") {
		t.Fatalf("report missing command-based section title:\n%s", report)
	}
	if !strings.Contains(report, "- Schedule: `*/5 * * * *`") {
		t.Fatalf("report missing schedule summary:\n%s", report)
	}
	if !strings.Contains(report, "- Command:\n\n```sh\necho tick\n```") {
		t.Fatalf("report missing command block:\n%s", report)
	}
	if !strings.Contains(report, "- Service unit: `"+units.ServiceName+"`") {
		t.Fatalf("report missing service unit reference:\n%s", report)
	}
	if !strings.Contains(report, "- Timer unit: `"+units.TimerName+"`") {
		t.Fatalf("report missing timer unit reference:\n%s", report)
	}
}

func TestReportJobTitlePrefersNameAndFallsBackToCommandPreview(t *testing.T) {
	t.Parallel()

	if got := reportJobTitle(config.Job{Name: "Nightly backup", Run: config.ShellCommand("echo ignored")}); got != "Nightly backup" {
		t.Fatalf("reportJobTitle() with name = %q, want %q", got, "Nightly backup")
	}

	got := reportJobTitle(config.Job{
		Run: config.ShellCommand("python /opt/scripts/very-long-task.py --flag one --flag two --flag three\nprintf done\n"),
	})
	if !strings.HasPrefix(got, "python /opt/scripts/very-long-task.py") {
		t.Fatalf("reportJobTitle() fallback = %q, want command preview", got)
	}
	if !strings.HasSuffix(got, "...") {
		t.Fatalf("reportJobTitle() fallback = %q, want ellipsis for multiline/long command", got)
	}
}

func TestDisplayCLIPath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input string
		want  string
	}{
		{input: "output", want: "./output"},
		{input: "./output", want: "./output"},
		{input: "../output", want: "../output"},
		{input: "/tmp/output", want: "/tmp/output"},
		{input: ".", want: "."},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			if got := displayCLIPath(tt.input); got != tt.want {
				t.Fatalf("displayCLIPath(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestColorizeRenderOutputPathWithoutTerminal(t *testing.T) {
	t.Parallel()

	if got := colorizeRenderOutputPath(&bytes.Buffer{}, "output"); got != "./output" {
		t.Fatalf("colorizeRenderOutputPath() = %q, want %q", got, "./output")
	}
}
