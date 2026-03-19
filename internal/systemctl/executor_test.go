package systemctl

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestCommandExecutorEnableTimerIncludesActionableError(t *testing.T) {
	expectedArgs := []string{"--user", "enable", "sample.timer"}

	executor := &CommandExecutor{
		invoke: func(_ context.Context, args ...string) (string, error) {
			if !reflect.DeepEqual(args, expectedArgs) {
				t.Fatalf("args = %v, want %v", args, expectedArgs)
			}
			return "permission denied", errors.New("exit status 1")
		},
	}

	err := executor.EnableTimer(context.Background(), "sample.timer")
	if err == nil {
		t.Fatalf("EnableTimer() error = nil, want non-nil")
	}

	msg := err.Error()
	if !strings.Contains(msg, "systemctl --user enable sample.timer failed") {
		t.Fatalf("error %q does not contain command context", msg)
	}
	if !strings.Contains(msg, "permission denied") {
		t.Fatalf("error %q does not contain stderr context", msg)
	}
}

func TestCommandExecutorStopTimerWithoutStderrKeepsErrorReadable(t *testing.T) {
	executor := &CommandExecutor{
		invoke: func(_ context.Context, args ...string) (string, error) {
			expectedArgs := []string{"--user", "stop", "sample.timer"}
			if !reflect.DeepEqual(args, expectedArgs) {
				t.Fatalf("args = %v, want %v", args, expectedArgs)
			}
			return "", errors.New("exit status 1")
		},
	}

	err := executor.StopTimer(context.Background(), "sample.timer")
	if err == nil {
		t.Fatalf("StopTimer() error = nil, want non-nil")
	}

	if got, want := err.Error(), "systemctl --user stop sample.timer failed: exit status 1"; got != want {
		t.Fatalf("StopTimer() error = %q, want %q", got, want)
	}
}

func TestCommandExecutorEnableTimersRunsSingleCommand(t *testing.T) {
	expectedArgs := []string{"--user", "enable", "sample-a.timer", "sample-b.timer"}
	invoked := 0

	executor := &CommandExecutor{
		invoke: func(_ context.Context, args ...string) (string, error) {
			invoked++
			if !reflect.DeepEqual(args, expectedArgs) {
				t.Fatalf("args = %v, want %v", args, expectedArgs)
			}
			return "", nil
		},
	}

	if err := executor.EnableTimers(context.Background(), []string{"sample-a.timer", "sample-b.timer"}); err != nil {
		t.Fatalf("EnableTimers() error = %v, want nil", err)
	}
	if invoked != 1 {
		t.Fatalf("invoke count = %d, want 1", invoked)
	}
}

func TestCommandExecutorForRootOmitsUserScope(t *testing.T) {
	expectedArgs := []string{"daemon-reload"}

	executor := &CommandExecutor{
		scope: SystemScope,
		invoke: func(_ context.Context, args ...string) (string, error) {
			if !reflect.DeepEqual(args, expectedArgs) {
				t.Fatalf("args = %v, want %v", args, expectedArgs)
			}
			return "", errors.New("exit status 1")
		},
	}

	err := executor.DaemonReload(context.Background())
	if err == nil {
		t.Fatalf("DaemonReload() error = nil, want non-nil")
	}
	if got, want := err.Error(), "systemctl daemon-reload failed: exit status 1"; got != want {
		t.Fatalf("DaemonReload() error = %q, want %q", got, want)
	}
}
