package reconcile

import (
	"context"
	"errors"
	"slices"
	"testing"
)

func TestApplyPrunesOnlyManagedUnitsForTargetUID(t *testing.T) {
	mutator := &recordingMutator{}

	_, err := Apply(context.Background(), ApplyRequest{
		TargetUID: 1000,
		Desired: []DesiredUnit{
			{Name: "timertab-u1000-keep.timer", Content: "same"},
		},
		Existing: []ExistingUnit{
			{Name: "timertab-u1000-keep.timer", Content: "same", Managed: true},
			{Name: "timertab-u1000-stale-a.timer", Content: "old", Managed: true},
			{Name: "timertab-u1000-stale-b.service", Content: "old", Managed: true},
			{Name: "timertab-u1001-stale.timer", Content: "other-uid", Managed: true},
			{Name: "timertab-u1000-foreign.timer", Content: "foreign", Managed: false},
			{Name: "dbus.service", Content: "foreign", Managed: true},
		},
	}, mutator)
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}

	wantCalls := []string{
		"disable:timertab-u1000-stale-a.timer",
		"stop:timertab-u1000-stale-a.timer",
		"remove:timertab-u1000-stale-a.timer",
		"disable:timertab-u1000-stale-b.service",
		"stop:timertab-u1000-stale-b.service",
		"remove:timertab-u1000-stale-b.service",
	}
	if !slices.Equal(mutator.calls, wantCalls) {
		t.Fatalf("mutator calls = %v, want %v", mutator.calls, wantCalls)
	}
}

func TestApplyValidationFailureAbortsBeforeMutation(t *testing.T) {
	mutator := &recordingMutator{}
	validateErr := errors.New("invalid config")

	_, err := Apply(context.Background(), ApplyRequest{
		TargetUID: 1000,
		Desired: []DesiredUnit{
			{Name: "timertab-u1000-a.timer", Content: "A"},
		},
		Validate: func() error {
			return validateErr
		},
		Compile: func() error {
			t.Fatalf("compile should not be called after validation failure")
			return nil
		},
	}, mutator)
	if !errors.Is(err, validateErr) {
		t.Fatalf("Apply() error = %v, want %v", err, validateErr)
	}
	if len(mutator.calls) != 0 {
		t.Fatalf("mutator calls = %v, want none", mutator.calls)
	}
}

func TestApplyCompileFailureAbortsBeforeMutation(t *testing.T) {
	mutator := &recordingMutator{}
	compileErr := errors.New("compile failed")
	var validateCalled bool
	var compileCalled bool

	_, err := Apply(context.Background(), ApplyRequest{
		TargetUID: 1000,
		Desired: []DesiredUnit{
			{Name: "timertab-u1000-a.timer", Content: "A"},
		},
		Validate: func() error {
			validateCalled = true
			return nil
		},
		Compile: func() error {
			compileCalled = true
			return compileErr
		},
	}, mutator)
	if !errors.Is(err, compileErr) {
		t.Fatalf("Apply() error = %v, want %v", err, compileErr)
	}
	if !validateCalled {
		t.Fatalf("expected validate to be called")
	}
	if !compileCalled {
		t.Fatalf("expected compile to be called")
	}
	if len(mutator.calls) != 0 {
		t.Fatalf("mutator calls = %v, want none", mutator.calls)
	}
}

func TestExecutePruneRejectsForeignUnit(t *testing.T) {
	mutator := &recordingMutator{}

	err := ExecutePrune(context.Background(), 1000, []string{"timertab-u1001-foreign.timer"}, mutator)
	if err == nil {
		t.Fatalf("expected prune safety error")
	}
	if len(mutator.calls) != 0 {
		t.Fatalf("mutator calls = %v, want none", mutator.calls)
	}
}

type recordingMutator struct {
	calls []string
}

func (m *recordingMutator) WriteUnit(_ context.Context, unit DesiredUnit) error {
	m.calls = append(m.calls, "write:"+unit.Name)
	return nil
}

func (m *recordingMutator) DisableUnit(_ context.Context, unitName string) error {
	m.calls = append(m.calls, "disable:"+unitName)
	return nil
}

func (m *recordingMutator) StopUnit(_ context.Context, unitName string) error {
	m.calls = append(m.calls, "stop:"+unitName)
	return nil
}

func (m *recordingMutator) RemoveUnitFile(unitName string) error {
	m.calls = append(m.calls, "remove:"+unitName)
	return nil
}
