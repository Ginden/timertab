package systemctl

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// Executor abstracts systemctl operations so apply logic can be unit tested.
type Executor interface {
	DaemonReload(ctx context.Context) error
	EnableTimer(ctx context.Context, timerUnit string) error
	StartTimer(ctx context.Context, timerUnit string) error
	DisableTimer(ctx context.Context, timerUnit string) error
	StopTimer(ctx context.Context, timerUnit string) error
}

type invokeSystemctlFunc func(ctx context.Context, args ...string) (stderr string, err error)

// CommandExecutor is the production implementation that shells out to systemctl.
type CommandExecutor struct {
	invoke invokeSystemctlFunc
}

func NewCommandExecutor() *CommandExecutor {
	return &CommandExecutor{invoke: runSystemctl}
}

func (e *CommandExecutor) DaemonReload(ctx context.Context) error {
	return e.run(ctx, "daemon-reload")
}

func (e *CommandExecutor) EnableTimer(ctx context.Context, timerUnit string) error {
	return e.run(ctx, "enable", timerUnit)
}

func (e *CommandExecutor) StartTimer(ctx context.Context, timerUnit string) error {
	return e.run(ctx, "start", timerUnit)
}

func (e *CommandExecutor) DisableTimer(ctx context.Context, timerUnit string) error {
	return e.run(ctx, "disable", timerUnit)
}

func (e *CommandExecutor) StopTimer(ctx context.Context, timerUnit string) error {
	return e.run(ctx, "stop", timerUnit)
}

func (e *CommandExecutor) run(ctx context.Context, args ...string) error {
	invoke := e.invoke
	if invoke == nil {
		invoke = runSystemctl
	}

	stderr, err := invoke(ctx, args...)
	if err == nil {
		return nil
	}

	cmdText := "systemctl " + strings.Join(args, " ")
	msg := strings.TrimSpace(stderr)
	if msg == "" {
		return fmt.Errorf("%s failed: %w", cmdText, err)
	}
	return fmt.Errorf("%s failed: %w: %s", cmdText, err, msg)
}

func runSystemctl(ctx context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "systemctl", args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	err := cmd.Run()
	return stderr.String(), err
}
