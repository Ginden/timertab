package systemctl

import (
	"bytes"
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/ginden/timertab/internal/progress"
)

type fakeExecutor struct {
	calls    []string
	failures map[string]error
}

func (f *fakeExecutor) DaemonReload(_ context.Context) error {
	return f.record("daemon-reload")
}

func (f *fakeExecutor) EnableTimer(_ context.Context, timerUnit string) error {
	return f.record("enable " + timerUnit)
}

func (f *fakeExecutor) StartTimer(_ context.Context, timerUnit string) error {
	return f.record("start " + timerUnit)
}

func (f *fakeExecutor) StartService(_ context.Context, serviceUnit string) error {
	return f.record("start-service " + serviceUnit)
}

func (f *fakeExecutor) DisableTimer(_ context.Context, timerUnit string) error {
	return f.record("disable " + timerUnit)
}

func (f *fakeExecutor) StopTimer(_ context.Context, timerUnit string) error {
	return f.record("stop " + timerUnit)
}

func (f *fakeExecutor) record(call string) error {
	f.calls = append(f.calls, call)
	if f.failures == nil {
		return nil
	}
	return f.failures[call]
}

type fakeBatchExecutor struct {
	fakeExecutor
}

func (f *fakeBatchExecutor) EnableTimers(_ context.Context, timerUnits []string) error {
	return f.record("enable-batch " + strings.Join(timerUnits, " "))
}

func (f *fakeBatchExecutor) StartTimers(_ context.Context, timerUnits []string) error {
	return f.record("start-batch " + strings.Join(timerUnits, " "))
}

func (f *fakeBatchExecutor) DisableTimers(_ context.Context, timerUnits []string) error {
	return f.record("disable-batch " + strings.Join(timerUnits, " "))
}

func (f *fakeBatchExecutor) StopTimers(_ context.Context, timerUnits []string) error {
	return f.record("stop-batch " + strings.Join(timerUnits, " "))
}

func TestEnableAndStartTimersCommandOrder(t *testing.T) {
	executor := &fakeExecutor{}

	err := EnableAndStartTimers(context.Background(), executor, []string{"alpha.timer", "beta.timer"})
	if err != nil {
		t.Fatalf("EnableAndStartTimers() error = %v, want nil", err)
	}

	wantCalls := []string{
		"enable alpha.timer",
		"start alpha.timer",
		"enable beta.timer",
		"start beta.timer",
	}
	if !reflect.DeepEqual(executor.calls, wantCalls) {
		t.Fatalf("calls = %v, want %v", executor.calls, wantCalls)
	}
}

func TestDisableAndStopTimersCommandOrder(t *testing.T) {
	executor := &fakeExecutor{}

	err := DisableAndStopTimers(context.Background(), executor, []string{"old-a.timer", "old-b.timer"})
	if err != nil {
		t.Fatalf("DisableAndStopTimers() error = %v, want nil", err)
	}

	wantCalls := []string{
		"disable old-a.timer",
		"stop old-a.timer",
		"disable old-b.timer",
		"stop old-b.timer",
	}
	if !reflect.DeepEqual(executor.calls, wantCalls) {
		t.Fatalf("calls = %v, want %v", executor.calls, wantCalls)
	}
}

func TestRunPlanStopsOnFailureAndReturnsActionableError(t *testing.T) {
	executor := &fakeExecutor{
		failures: map[string]error{
			"enable beta.timer": errors.New("exit status 1"),
		},
	}

	err := RunPlan(
		context.Background(),
		executor,
		Plan{
			TimersToDisable: []string{"old.timer"},
			TimersToEnable:  []string{"alpha.timer", "beta.timer", "gamma.timer"},
			TimersToStart:   []string{"alpha.timer", "beta.timer", "gamma.timer"},
			ReloadDaemon:    true,
		},
	)
	if err == nil {
		t.Fatalf("RunPlan() error = nil, want non-nil")
	}

	wantCalls := []string{
		"disable old.timer",
		"stop old.timer",
		"daemon-reload",
		"enable alpha.timer",
		"start alpha.timer",
		"enable beta.timer",
	}
	if !reflect.DeepEqual(executor.calls, wantCalls) {
		t.Fatalf("calls = %v, want %v", executor.calls, wantCalls)
	}

	msg := err.Error()
	if !strings.Contains(msg, `failed to enable timer "beta.timer"`) {
		t.Fatalf("error %q missing operation context", msg)
	}
	if !strings.Contains(msg, "exit status 1") {
		t.Fatalf("error %q missing underlying failure", msg)
	}
}

func TestEnableAndStartTimersUsesBatchExecutorWhenAvailable(t *testing.T) {
	executor := &fakeBatchExecutor{}

	err := EnableAndStartTimers(context.Background(), executor, []string{"alpha.timer", "beta.timer"})
	if err != nil {
		t.Fatalf("EnableAndStartTimers() error = %v, want nil", err)
	}

	wantCalls := []string{
		"enable-batch alpha.timer beta.timer",
		"start-batch alpha.timer beta.timer",
	}
	if !reflect.DeepEqual(executor.calls, wantCalls) {
		t.Fatalf("calls = %v, want %v", executor.calls, wantCalls)
	}
}

func TestRunPlanSkipsReloadWhenOnlyTimerStateNeedsReconcile(t *testing.T) {
	executor := &fakeBatchExecutor{}

	err := RunPlan(
		context.Background(),
		executor,
		Plan{
			TimersToEnable: []string{"alpha.timer", "beta.timer"},
			TimersToStart:  []string{"alpha.timer", "beta.timer"},
		},
	)
	if err != nil {
		t.Fatalf("RunPlan() error = %v, want nil", err)
	}

	wantCalls := []string{
		"enable-batch alpha.timer beta.timer",
		"start-batch alpha.timer beta.timer",
	}
	if !reflect.DeepEqual(executor.calls, wantCalls) {
		t.Fatalf("calls = %v, want %v", executor.calls, wantCalls)
	}
}

func TestRunPlanCanEnableTimersWithoutStartingThem(t *testing.T) {
	executor := &fakeExecutor{}

	err := RunPlan(
		context.Background(),
		executor,
		Plan{
			TimersToEnable: []string{"alpha.timer", "reboot.timer"},
			TimersToStart:  []string{"alpha.timer"},
		},
	)
	if err != nil {
		t.Fatalf("RunPlan() error = %v, want nil", err)
	}

	wantCalls := []string{
		"enable alpha.timer",
		"enable reboot.timer",
		"start alpha.timer",
	}
	if !reflect.DeepEqual(executor.calls, wantCalls) {
		t.Fatalf("calls = %v, want %v", executor.calls, wantCalls)
	}
}

func TestDisableAndStopTimersUsesBatchExecutorWhenAvailable(t *testing.T) {
	executor := &fakeBatchExecutor{}

	err := DisableAndStopTimers(context.Background(), executor, []string{"old-a.timer", "old-b.timer"})
	if err != nil {
		t.Fatalf("DisableAndStopTimers() error = %v, want nil", err)
	}

	wantCalls := []string{
		"disable-batch old-a.timer old-b.timer",
		"stop-batch old-a.timer old-b.timer",
	}
	if !reflect.DeepEqual(executor.calls, wantCalls) {
		t.Fatalf("calls = %v, want %v", executor.calls, wantCalls)
	}
}

func TestRunPlanReloadsDaemonWhenUnitsChangedWithoutTimersToEnable(t *testing.T) {
	executor := &fakeExecutor{}

	err := RunPlan(
		context.Background(),
		executor,
		Plan{
			TimersToDisable: []string{"old.timer"},
			ReloadDaemon:    true,
		},
	)
	if err != nil {
		t.Fatalf("RunPlan() error = %v, want nil", err)
	}

	wantCalls := []string{
		"disable old.timer",
		"stop old.timer",
		"daemon-reload",
	}
	if !reflect.DeepEqual(executor.calls, wantCalls) {
		t.Fatalf("calls = %v, want %v", executor.calls, wantCalls)
	}
}

func TestRunPlanEmitsProgressMessages(t *testing.T) {
	executor := &fakeBatchExecutor{}
	stderr := &bytes.Buffer{}
	ctx := progress.WithWriter(context.Background(), stderr)

	err := RunPlan(
		ctx,
		executor,
		Plan{
			TimersToDisable: []string{"old.timer"},
			TimersToEnable:  []string{"alpha.timer", "beta.timer"},
			TimersToStart:   []string{"alpha.timer", "beta.timer"},
			ReloadDaemon:    true,
		},
	)
	if err != nil {
		t.Fatalf("RunPlan() error = %v, want nil", err)
	}

	output := stderr.String()
	for _, needle := range []string{
		"timertab: disabling 1 stale timer(s)\n",
		"timertab: stopping 1 stale timer(s)\n",
		"timertab: reloading systemd state\n",
		"timertab: enabling 2 timer(s)\n",
		"timertab: starting 2 timer(s)\n",
	} {
		if !strings.Contains(output, needle) {
			t.Fatalf("progress output missing %q, got:\n%s", needle, output)
		}
	}
}
