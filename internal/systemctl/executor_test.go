package systemctl

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestCommandExecutorEnableTimerIncludesActionableError(t *testing.T) {
	expectedArgs := []string{"enable", "sample.timer"}

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
	if !strings.Contains(msg, "systemctl enable sample.timer failed") {
		t.Fatalf("error %q does not contain command context", msg)
	}
	if !strings.Contains(msg, "permission denied") {
		t.Fatalf("error %q does not contain stderr context", msg)
	}
}

func TestCommandExecutorStopTimerWithoutStderrKeepsErrorReadable(t *testing.T) {
	executor := &CommandExecutor{
		invoke: func(_ context.Context, args ...string) (string, error) {
			return "", errors.New("exit status 1")
		},
	}

	err := executor.StopTimer(context.Background(), "sample.timer")
	if err == nil {
		t.Fatalf("StopTimer() error = nil, want non-nil")
	}

	if got, want := err.Error(), "systemctl stop sample.timer failed: exit status 1"; got != want {
		t.Fatalf("StopTimer() error = %q, want %q", got, want)
	}
}
