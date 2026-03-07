package reconcile

import (
	"slices"
	"testing"
)

func TestBuildPlanDeterministicSets(t *testing.T) {
	targetUID := uint32(1000)
	instanceID := "timertab"

	desired := []DesiredUnit{
		{Name: "timertab-u1000-gamma.service", Content: "gamma-new"},
		{Name: "timertab-u1000-alpha.timer", Content: "alpha-same"},
		{Name: "timertab-u1000-beta.timer", Content: "beta-new"},
	}
	existing := []ExistingUnit{
		{Name: "timertab-u1000-delta.timer", Content: "stale", Managed: true},
		{Name: "timertab-u1000-alpha.timer", Content: "alpha-same", Managed: true},
		{Name: "timertab-u1000-gamma.service", Content: "gamma-old", Managed: true},
		{Name: "timertab-u1001-other.timer", Content: "foreign-uid", Managed: true},
		{Name: "timertab-u1000-unmanaged.service", Content: "foreign", Managed: false},
		{Name: "ssh.service", Content: "foreign", Managed: true},
	}

	plan, err := BuildPlan(targetUID, instanceID, desired, existing)
	if err != nil {
		t.Fatalf("BuildPlan() error = %v", err)
	}

	if got, want := unitNames(plan.Create), []string{"timertab-u1000-beta.timer"}; !slices.Equal(got, want) {
		t.Fatalf("create names = %v, want %v", got, want)
	}
	if got, want := unitNames(plan.Update), []string{"timertab-u1000-gamma.service"}; !slices.Equal(got, want) {
		t.Fatalf("update names = %v, want %v", got, want)
	}
	if got, want := plan.Keep, []string{"timertab-u1000-alpha.timer"}; !slices.Equal(got, want) {
		t.Fatalf("keep names = %v, want %v", got, want)
	}
	if got, want := plan.Remove, []string{"timertab-u1000-delta.timer"}; !slices.Equal(got, want) {
		t.Fatalf("remove names = %v, want %v", got, want)
	}

	reversedDesired := slices.Clone(desired)
	slices.Reverse(reversedDesired)
	reversedExisting := slices.Clone(existing)
	slices.Reverse(reversedExisting)

	planFromReversed, err := BuildPlan(targetUID, instanceID, reversedDesired, reversedExisting)
	if err != nil {
		t.Fatalf("BuildPlan() with reversed input error = %v", err)
	}

	if got, want := unitNames(planFromReversed.Create), unitNames(plan.Create); !slices.Equal(got, want) {
		t.Fatalf("deterministic create names = %v, want %v", got, want)
	}
	if got, want := unitNames(planFromReversed.Update), unitNames(plan.Update); !slices.Equal(got, want) {
		t.Fatalf("deterministic update names = %v, want %v", got, want)
	}
	if got, want := planFromReversed.Keep, plan.Keep; !slices.Equal(got, want) {
		t.Fatalf("deterministic keep names = %v, want %v", got, want)
	}
	if got, want := planFromReversed.Remove, plan.Remove; !slices.Equal(got, want) {
		t.Fatalf("deterministic remove names = %v, want %v", got, want)
	}
}

func TestBuildPlanRejectsForeignConflicts(t *testing.T) {
	_, err := BuildPlan(1000, "timertab",
		[]DesiredUnit{
			{Name: "timertab-u1000-job.timer", Content: "desired"},
		},
		[]ExistingUnit{
			{Name: "timertab-u1000-job.timer", Content: "foreign", Managed: false},
		},
	)
	if err == nil {
		t.Fatalf("expected conflict error")
	}
}

func unitNames(units []DesiredUnit) []string {
	out := make([]string, len(units))
	for idx, unit := range units {
		out[idx] = unit.Name
	}
	return out
}
