package reconcile

import (
	"context"
	"fmt"
)

// ApplyRequest carries all inputs required for a guarded reconcile pass.
// Validate and Compile are optional callbacks used as safety gates.
type ApplyRequest struct {
	TargetUID uint32
	Desired   []DesiredUnit
	Existing  []ExistingUnit
	Validate  func() error
	Compile   func() error
}

// Mutator performs filesystem and systemctl changes required by reconcile.
type Mutator interface {
	WriteUnit(ctx context.Context, unit DesiredUnit) error
	DisableUnit(ctx context.Context, unitName string) error
	StopUnit(ctx context.Context, unitName string) error
	RemoveUnitFile(unitName string) error
}

// Apply validates input, builds a deterministic plan, safely prunes stale units,
// then writes create/update units.
func Apply(ctx context.Context, req ApplyRequest, mutator Mutator) (Plan, error) {
	if mutator == nil {
		return Plan{}, fmt.Errorf("mutator is required")
	}

	if req.Validate != nil {
		if err := req.Validate(); err != nil {
			return Plan{}, fmt.Errorf("validate reconcile input: %w", err)
		}
	}

	if req.Compile != nil {
		if err := req.Compile(); err != nil {
			return Plan{}, fmt.Errorf("compile desired units: %w", err)
		}
	}

	plan, err := BuildPlan(req.TargetUID, req.Desired, req.Existing)
	if err != nil {
		return Plan{}, err
	}

	if err := ExecutePrune(ctx, req.TargetUID, plan.Remove, mutator); err != nil {
		return Plan{}, err
	}

	for _, unit := range plan.Create {
		if err := mutator.WriteUnit(ctx, unit); err != nil {
			return Plan{}, fmt.Errorf("write created unit %q: %w", unit.Name, err)
		}
	}
	for _, unit := range plan.Update {
		if err := mutator.WriteUnit(ctx, unit); err != nil {
			return Plan{}, fmt.Errorf("write updated unit %q: %w", unit.Name, err)
		}
	}

	return plan, nil
}

// ExecutePrune performs safe stale-unit removal in deterministic order.
func ExecutePrune(ctx context.Context, targetUID uint32, units []string, mutator Mutator) error {
	for _, unit := range units {
		if !IsManagedUnitForUID(targetUID, unit) {
			return fmt.Errorf("refusing to prune unmanaged or foreign unit %q", unit)
		}

		if err := mutator.DisableUnit(ctx, unit); err != nil {
			return fmt.Errorf("disable stale unit %q: %w", unit, err)
		}
		if err := mutator.StopUnit(ctx, unit); err != nil {
			return fmt.Errorf("stop stale unit %q: %w", unit, err)
		}
		if err := mutator.RemoveUnitFile(unit); err != nil {
			return fmt.Errorf("remove stale unit file %q: %w", unit, err)
		}
	}

	return nil
}
