package systemd

import (
	"strings"
	"testing"

	"github.com/ginden/timertab/internal/config"
)

func TestExecDollarsSurviveSystemdExpansion(t *testing.T) {
	cases := []struct {
		run  config.RunCommand
		want string
	}{
		{config.ShellCommand(`value=ok; echo "${value}" '$HOME' $$`), `ExecStart=/bin/sh -lc "value=ok; echo \"$${value}\" '$$HOME' $$$$"`},
		{config.ExecCommand("/bin/echo", "${HOME}", "$HOME"), `ExecStart="/bin/echo" "$${HOME}" "$$HOME"`},
	}
	for _, tc := range cases {
		units, err := RenderJobUnits(1000, "", config.Job{
			ID: "dollars", When: config.ScheduleList{"@daily"}, Run: tc.run,
			Env:       map[string]string{"LITERAL": "$HOME"},
			OnSuccess: &config.Hook{Command: `echo "${TIMERTAB_JOB_ID}"`, Env: map[string]string{"LITERAL": "$HOME"}},
		})
		if err != nil {
			t.Fatal(err)
		}
		for _, want := range []string{tc.want + "\n", `Environment="LITERAL=$HOME"`, `$${SERVICE_RESULT:-}`, `$${TIMERTAB_JOB_ID}`, `LITERAL='$$HOME'`} {
			if !strings.Contains(units.ServiceContent, want) {
				t.Errorf("missing %q in service:\n%s", want, units.ServiceContent)
			}
		}
	}
}
