package systemctl

import (
	"context"
	"errors"
	"fmt"
)

var errMissingExecutor = errors.New("systemctl executor is required")

// Plan captures ordered timer operations that must be executed via systemctl.
type Plan struct {
	TimersToDisable []string
	TimersToEnable  []string
}

// RunPlan executes disable/stop operations first, then daemon-reload/enable/start.
func RunPlan(ctx context.Context, executor Executor, plan Plan) error {
	if err := DisableAndStopTimers(ctx, executor, plan.TimersToDisable); err != nil {
		return err
	}
	if err := EnableAndStartTimers(ctx, executor, plan.TimersToEnable); err != nil {
		return err
	}
	return nil
}

// EnableAndStartTimers reloads systemd state and then enables/starts timers.
func EnableAndStartTimers(ctx context.Context, executor Executor, timerUnits []string) error {
	if executor == nil {
		return errMissingExecutor
	}
	if len(timerUnits) == 0 {
		return nil
	}

	if err := executor.DaemonReload(ctx); err != nil {
		return fmt.Errorf("failed to daemon-reload before enabling timers: %w", err)
	}

	for _, timerUnit := range timerUnits {
		if err := executor.EnableTimer(ctx, timerUnit); err != nil {
			return fmt.Errorf("failed to enable timer %q: %w", timerUnit, err)
		}
		if err := executor.StartTimer(ctx, timerUnit); err != nil {
			return fmt.Errorf("failed to start timer %q: %w", timerUnit, err)
		}
	}

	return nil
}

// DisableAndStopTimers disables and then stops stale timers.
func DisableAndStopTimers(ctx context.Context, executor Executor, timerUnits []string) error {
	if executor == nil {
		return errMissingExecutor
	}

	for _, timerUnit := range timerUnits {
		if err := executor.DisableTimer(ctx, timerUnit); err != nil {
			return fmt.Errorf("failed to disable timer %q: %w", timerUnit, err)
		}
		if err := executor.StopTimer(ctx, timerUnit); err != nil {
			return fmt.Errorf("failed to stop timer %q: %w", timerUnit, err)
		}
	}

	return nil
}
