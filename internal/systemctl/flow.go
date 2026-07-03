package systemctl

import (
	"context"
	"errors"
	"fmt"

	"github.com/ginden/timertab/internal/progress"
)

var errMissingExecutor = errors.New("systemctl executor is required")

type batchExecutor interface {
	EnableTimers(ctx context.Context, timerUnits []string) error
	StartTimers(ctx context.Context, timerUnits []string) error
	DisableTimers(ctx context.Context, timerUnits []string) error
	StopTimers(ctx context.Context, timerUnits []string) error
}

// Plan captures ordered timer operations that must be executed via systemctl.
type Plan struct {
	TimersToDisable []string
	TimersToEnable  []string
	TimersToStart   []string
	ReloadDaemon    bool
}

// RunPlan executes disable/stop operations first, then daemon-reload if needed,
// then enable/start operations.
func RunPlan(ctx context.Context, executor Executor, plan Plan) error {
	if err := DisableAndStopTimers(ctx, executor, plan.TimersToDisable); err != nil {
		return err
	}
	if plan.ReloadDaemon {
		progress.Printf(ctx, "timertab: reloading systemd state")
		if err := executor.DaemonReload(ctx); err != nil {
			return fmt.Errorf("failed to daemon-reload: %w", err)
		}
	}
	if sameStringSlice(plan.TimersToEnable, plan.TimersToStart) {
		if err := EnableAndStartTimers(ctx, executor, plan.TimersToEnable); err != nil {
			return err
		}
		return nil
	}
	if err := EnableTimers(ctx, executor, plan.TimersToEnable); err != nil {
		return err
	}
	if err := StartTimers(ctx, executor, plan.TimersToStart); err != nil {
		return err
	}
	return nil
}

// EnableAndStartTimers enables and then starts timers.
func EnableAndStartTimers(ctx context.Context, executor Executor, timerUnits []string) error {
	if executor == nil {
		return errMissingExecutor
	}
	if len(timerUnits) == 0 {
		return nil
	}

	if batch, ok := executor.(batchExecutor); ok {
		progress.Printf(ctx, "timertab: enabling %d timer(s)", len(timerUnits))
		if err := batch.EnableTimers(ctx, timerUnits); err != nil {
			return fmt.Errorf("failed to enable timers: %w", err)
		}
		progress.Printf(ctx, "timertab: starting %d timer(s)", len(timerUnits))
		if err := batch.StartTimers(ctx, timerUnits); err != nil {
			return fmt.Errorf("failed to start timers: %w", err)
		}
		return nil
	}

	progress.Printf(ctx, "timertab: enabling and starting %d timer(s)", len(timerUnits))
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

// EnableTimers enables timers without starting them.
func EnableTimers(ctx context.Context, executor Executor, timerUnits []string) error {
	if executor == nil {
		return errMissingExecutor
	}
	if len(timerUnits) == 0 {
		return nil
	}

	if batch, ok := executor.(batchExecutor); ok {
		progress.Printf(ctx, "timertab: enabling %d timer(s)", len(timerUnits))
		if err := batch.EnableTimers(ctx, timerUnits); err != nil {
			return fmt.Errorf("failed to enable timers: %w", err)
		}
		return nil
	}

	progress.Printf(ctx, "timertab: enabling %d timer(s)", len(timerUnits))
	for _, timerUnit := range timerUnits {
		if err := executor.EnableTimer(ctx, timerUnit); err != nil {
			return fmt.Errorf("failed to enable timer %q: %w", timerUnit, err)
		}
	}

	return nil
}

// StartTimers starts timers without changing enablement.
func StartTimers(ctx context.Context, executor Executor, timerUnits []string) error {
	if executor == nil {
		return errMissingExecutor
	}
	if len(timerUnits) == 0 {
		return nil
	}

	if batch, ok := executor.(batchExecutor); ok {
		progress.Printf(ctx, "timertab: starting %d timer(s)", len(timerUnits))
		if err := batch.StartTimers(ctx, timerUnits); err != nil {
			return fmt.Errorf("failed to start timers: %w", err)
		}
		return nil
	}

	progress.Printf(ctx, "timertab: starting %d timer(s)", len(timerUnits))
	for _, timerUnit := range timerUnits {
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

	if len(timerUnits) == 0 {
		return nil
	}

	if batch, ok := executor.(batchExecutor); ok {
		progress.Printf(ctx, "timertab: disabling %d stale timer(s)", len(timerUnits))
		if err := batch.DisableTimers(ctx, timerUnits); err != nil {
			return fmt.Errorf("failed to disable timers: %w", err)
		}
		progress.Printf(ctx, "timertab: stopping %d stale timer(s)", len(timerUnits))
		if err := batch.StopTimers(ctx, timerUnits); err != nil {
			return fmt.Errorf("failed to stop timers: %w", err)
		}
		return nil
	}

	progress.Printf(ctx, "timertab: disabling and stopping %d stale timer(s)", len(timerUnits))
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

func sameStringSlice(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for idx := range a {
		if a[idx] != b[idx] {
			return false
		}
	}
	return true
}
