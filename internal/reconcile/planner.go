package reconcile

import (
	"fmt"
	"sort"
)

// BuildPlan computes deterministic create/update/keep/remove sets.
// Existing units are considered for management only when they are both:
// - marked as timertab-managed metadata (ExistingUnit.Managed == true)
// - in the timertab namespace for targetUID
func BuildPlan(targetUID uint32, instanceID string, desired []DesiredUnit, existing []ExistingUnit) (Plan, error) {
	desiredByName := make(map[string]DesiredUnit, len(desired))
	for _, unit := range desired {
		if err := validateDesiredUnitName(unit.Name, targetUID, instanceID); err != nil {
			return Plan{}, err
		}
		if _, exists := desiredByName[unit.Name]; exists {
			return Plan{}, fmt.Errorf("duplicate desired unit %q", unit.Name)
		}
		desiredByName[unit.Name] = unit
	}

	existingByName := make(map[string]ExistingUnit, len(existing))
	managedExistingByName := make(map[string]ExistingUnit, len(existing))
	for _, unit := range existing {
		if _, exists := existingByName[unit.Name]; exists {
			return Plan{}, fmt.Errorf("duplicate existing unit %q", unit.Name)
		}
		existingByName[unit.Name] = unit

		if unit.Managed && IsManagedUnitForUID(targetUID, instanceID, unit.Name) {
			managedExistingByName[unit.Name] = unit
		}
	}

	desiredNames := sortedDesiredNames(desiredByName)
	for _, name := range desiredNames {
		existingUnit, exists := existingByName[name]
		if !exists {
			continue
		}
		if existingUnit.Managed {
			continue
		}
		return Plan{}, fmt.Errorf("refusing to mutate foreign unit %q without timertab managed marker; it may have been ejected or created outside timertab: run `timertab adopt <id>`, delete the unit file, or choose another job id", name)
	}

	plan := Plan{
		Create: make([]DesiredUnit, 0, len(desiredByName)),
		Update: make([]DesiredUnit, 0, len(desiredByName)),
		Keep:   make([]string, 0, len(desiredByName)),
		Remove: make([]string, 0, len(managedExistingByName)),
	}

	for _, name := range desiredNames {
		desiredUnit := desiredByName[name]
		existingUnit, exists := managedExistingByName[name]
		if !exists {
			plan.Create = append(plan.Create, desiredUnit)
			continue
		}

		if existingUnit.Content == desiredUnit.Content {
			plan.Keep = append(plan.Keep, name)
			continue
		}
		plan.Update = append(plan.Update, desiredUnit)
	}

	for name := range managedExistingByName {
		if _, keep := desiredByName[name]; !keep {
			plan.Remove = append(plan.Remove, name)
		}
	}
	sort.Strings(plan.Remove)

	return plan, nil
}

func sortedDesiredNames(desiredByName map[string]DesiredUnit) []string {
	out := make([]string, 0, len(desiredByName))
	for name := range desiredByName {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}
