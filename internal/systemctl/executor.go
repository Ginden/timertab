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
	StartService(ctx context.Context, serviceUnit string) error
	DisableTimer(ctx context.Context, timerUnit string) error
	StopTimer(ctx context.Context, timerUnit string) error
}

type invokeSystemctlFunc func(ctx context.Context, args ...string) (stderr string, err error)

type Scope int

const (
	UserScope Scope = iota
	SystemScope
)

func ScopeForUID(targetUID uint32) Scope {
	if targetUID == 0 {
		return SystemScope
	}
	return UserScope
}

func (s Scope) ScopedArgs(args ...string) []string {
	if s == UserScope {
		return append([]string{"--user"}, args...)
	}
	return append([]string(nil), args...)
}

func (s Scope) CommandString(binary string, args ...string) string {
	scopedArgs := s.ScopedArgs(args...)
	if len(scopedArgs) == 0 {
		return binary
	}
	return binary + " " + strings.Join(scopedArgs, " ")
}

func (s Scope) DaemonLabel() string {
	if s == UserScope {
		return "systemd --user daemon"
	}
	return "systemd daemon"
}

// CommandExecutor is the production implementation that shells out to systemctl.
type CommandExecutor struct {
	invoke invokeSystemctlFunc
	scope  Scope
}

func NewCommandExecutor() *CommandExecutor {
	return &CommandExecutor{invoke: runSystemctl, scope: UserScope}
}

func NewCommandExecutorForUID(targetUID uint32) *CommandExecutor {
	return &CommandExecutor{
		invoke: runSystemctl,
		scope:  ScopeForUID(targetUID),
	}
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

func (e *CommandExecutor) StartService(ctx context.Context, serviceUnit string) error {
	return e.run(ctx, "start", serviceUnit)
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

	scopedArgs := e.scope.ScopedArgs(args...)
	stderr, err := invoke(ctx, scopedArgs...)
	if err == nil {
		return nil
	}

	cmdText := e.scope.CommandString("systemctl", args...)
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
	cmd := exec.CommandContext(ctx, "systemctl", args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	err := cmd.Run()
	return stderr.String(), err
}
