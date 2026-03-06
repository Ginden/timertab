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

func (e *CommandExecutor) EnableTimers(ctx context.Context, timerUnits []string) error {
	return e.runForTimers(ctx, "enable", timerUnits)
}

func (e *CommandExecutor) StartTimer(ctx context.Context, timerUnit string) error {
	return e.run(ctx, "start", timerUnit)
}

func (e *CommandExecutor) StartTimers(ctx context.Context, timerUnits []string) error {
	return e.runForTimers(ctx, "start", timerUnits)
}

func (e *CommandExecutor) DisableTimer(ctx context.Context, timerUnit string) error {
	return e.run(ctx, "disable", timerUnit)
}

func (e *CommandExecutor) DisableTimers(ctx context.Context, timerUnits []string) error {
	return e.runForTimers(ctx, "disable", timerUnits)
}

func (e *CommandExecutor) StopTimer(ctx context.Context, timerUnit string) error {
	return e.run(ctx, "stop", timerUnit)
}

func (e *CommandExecutor) StopTimers(ctx context.Context, timerUnits []string) error {
	return e.runForTimers(ctx, "stop", timerUnits)
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

	cmdText := "systemctl --user " + strings.Join(args, " ")
	msg := strings.TrimSpace(stderr)
	if msg == "" {
		return fmt.Errorf("%s failed: %w", cmdText, err)
	}
	return fmt.Errorf("%s failed: %w: %s", cmdText, err, msg)
}

func (e *CommandExecutor) runForTimers(ctx context.Context, action string, timerUnits []string) error {
	if len(timerUnits) == 0 {
		return nil
	}

	args := make([]string, 1, len(timerUnits)+1)
	args[0] = action
	args = append(args, timerUnits...)
	return e.run(ctx, args...)
}

func runSystemctl(ctx context.Context, args ...string) (string, error) {
	systemctlArgs := append([]string{"--user"}, args...)
	cmd := exec.CommandContext(ctx, "systemctl", systemctlArgs...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	err := cmd.Run()
	return stderr.String(), err
}
