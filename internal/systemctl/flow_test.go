package systemctl

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
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

func TestEnableAndStartTimersCommandOrder(t *testing.T) {
	executor := &fakeExecutor{}

	err := EnableAndStartTimers(context.Background(), executor, []string{"alpha.timer", "beta.timer"})
	if err != nil {
		t.Fatalf("EnableAndStartTimers() error = %v, want nil", err)
	}

	wantCalls := []string{
		"daemon-reload",
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
