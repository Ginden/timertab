package cli

import (
	"context"
	"os"
	"os/user"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/ginden/timertab/internal/config"
	"github.com/ginden/timertab/internal/reconcile"
	"github.com/ginden/timertab/internal/systemctl"
	"github.com/ginden/timertab/internal/systemd"
)

func TestApplyEditedConfigReconcilesUnitsAndRunsSystemctl(t *testing.T) {
	unitDir := t.TempDir()
	targetUID := uint32(1000)

	keepJob := config.Job{
		ID:   "job-keep",
		When: config.ScheduleList{"@hourly"},
		Run:  "echo keep",
	}
	rendered, err := systemd.RenderJobUnits(targetUID, keepJob)
	if err != nil {
		t.Fatalf("RenderJobUnits() error = %v", err)
	}

	staleTimer := "timertab-u1000-stale-a.timer"
	staleService := "timertab-u1000-stale-a.service"
	if err := os.WriteFile(filepath.Join(unitDir, staleTimer), []byte(managedUnitContent(targetUID, "stale-a")), 0o644); err != nil {
		t.Fatalf("WriteFile(stale timer) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(unitDir, staleService), []byte(managedUnitContent(targetUID, "stale-a")), 0o644); err != nil {
		t.Fatalf("WriteFile(stale service) error = %v", err)
	}

	foreignUnmanaged := "timertab-u1000-foreign.timer"
	if err := os.WriteFile(filepath.Join(unitDir, foreignUnmanaged), []byte("[Unit]\nDescription=foreign"), 0o644); err != nil {
		t.Fatalf("WriteFile(foreign unit) error = %v", err)
	}

	fakeExecutor := &recordingExecutor{}
	restore := stubApplyDeps(t, targetUID, unitDir, fakeExecutor)
	defer restore()

	cfg := &config.File{
		Version: 1,
		Jobs:    []config.Job{keepJob},
	}
	report, err := applyEditedConfig(context.Background(), cfg, "")
	if err != nil {
		t.Fatalf("applyEditedConfig() error = %v", err)
	}

	wantReport := applyReport{
		Created: []string{
			filepath.Join(unitDir, rendered.ServiceName),
			filepath.Join(unitDir, rendered.TimerName),
		},
		Modified: []string{},
		Deleted: []string{
			filepath.Join(unitDir, staleService),
			filepath.Join(unitDir, staleTimer),
		},
		ReloadedDaemon: true,
		DisabledTimers: nil,
		StoppedTimers:  nil,
		EnabledTimers:  []string{rendered.TimerName},
		StartedTimers:  []string{rendered.TimerName},
	}
	if !reflect.DeepEqual(report, wantReport) {
		t.Fatalf("apply report = %#v, want %#v", report, wantReport)
	}

	wantCalls := []string{
		"disable " + staleTimer,
		"stop " + staleTimer,
		"daemon-reload",
		"enable " + rendered.TimerName,
		"start " + rendered.TimerName,
	}
	if !reflect.DeepEqual(fakeExecutor.calls, wantCalls) {
		t.Fatalf("systemctl calls = %v, want %v", fakeExecutor.calls, wantCalls)
	}

	assertFileExists(t, filepath.Join(unitDir, rendered.ServiceName))
	assertFileExists(t, filepath.Join(unitDir, rendered.TimerName))
	assertFileMissing(t, filepath.Join(unitDir, staleTimer))
	assertFileMissing(t, filepath.Join(unitDir, staleService))
	assertFileExists(t, filepath.Join(unitDir, foreignUnmanaged))
}

func TestApplyEditedConfigDisablesExistingTimersForDisabledJobs(t *testing.T) {
	unitDir := t.TempDir()
	targetUID := uint32(1000)

	disabled := false
	job := config.Job{
		ID:      "job-disabled",
		When:    config.ScheduleList{"@daily"},
		Run:     "echo disabled",
		Enabled: &disabled,
	}
	rendered, err := systemd.RenderJobUnits(targetUID, job)
	if err != nil {
		t.Fatalf("RenderJobUnits() error = %v", err)
	}

	if err := os.WriteFile(filepath.Join(unitDir, rendered.ServiceName), []byte(rendered.ServiceContent), 0o644); err != nil {
		t.Fatalf("WriteFile(service) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(unitDir, rendered.TimerName), []byte(rendered.TimerContent), 0o644); err != nil {
		t.Fatalf("WriteFile(timer) error = %v", err)
	}

	fakeExecutor := &recordingExecutor{}
	restore := stubApplyDeps(t, targetUID, unitDir, fakeExecutor)
	defer restore()

	cfg := &config.File{
		Version: 1,
		Jobs:    []config.Job{job},
	}
	report, err := applyEditedConfig(context.Background(), cfg, "")
	if err != nil {
		t.Fatalf("applyEditedConfig() error = %v", err)
	}
	wantReport := applyReport{
		Created:        []string{},
		Modified:       []string{},
		Deleted:        []string{},
		ReloadedDaemon: false,
		DisabledTimers: []string{rendered.TimerName},
		StoppedTimers:  []string{rendered.TimerName},
		EnabledTimers:  nil,
		StartedTimers:  nil,
	}
	if !reflect.DeepEqual(report, wantReport) {
		t.Fatalf("apply report = %#v, want %#v", report, wantReport)
	}

	wantCalls := []string{
		"disable " + rendered.TimerName,
		"stop " + rendered.TimerName,
	}
	if !reflect.DeepEqual(fakeExecutor.calls, wantCalls) {
		t.Fatalf("systemctl calls = %v, want %v", fakeExecutor.calls, wantCalls)
	}
}

func TestDiscoverExistingUnitsTracksManagedMetadata(t *testing.T) {
	unitDir := t.TempDir()

	managedName := "timertab-u1000-managed.timer"
	unmanagedName := "timertab-u1000-unmanaged.timer"
	foreignName := "timertab-u1001-foreign.timer"

	if err := os.WriteFile(filepath.Join(unitDir, managedName), []byte(managedUnitContent(1000, "managed")), 0o644); err != nil {
		t.Fatalf("WriteFile(managed) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(unitDir, unmanagedName), []byte("[Timer]\nOnCalendar=@hourly"), 0o644); err != nil {
		t.Fatalf("WriteFile(unmanaged) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(unitDir, foreignName), []byte(managedUnitContent(1001, "foreign")), 0o644); err != nil {
		t.Fatalf("WriteFile(foreign) error = %v", err)
	}

	got, err := discoverExistingUnits(unitDir, 1000)
	if err != nil {
		t.Fatalf("discoverExistingUnits() error = %v", err)
	}

	want := []reconcile.ExistingUnit{
		{Name: managedName, Content: managedUnitContent(1000, "managed"), Managed: true},
		{Name: unmanagedName, Content: "[Timer]\nOnCalendar=@hourly", Managed: false},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("discoverExistingUnits() = %#v, want %#v", got, want)
	}
}

func stubApplyDeps(t *testing.T, targetUID uint32, unitDir string, executor systemctl.Executor) func() {
	t.Helper()

	originalResolveTargetUID := resolveTargetUID
	originalResolveSystemdUserUnitDir := resolveSystemdUserUnitDir
	originalNewSystemctlExecutor := newSystemctlExecutor
	originalLookupUserByName := lookupUserByName
	originalLookupUserByUID := lookupUserByUID
	originalStatPath := statPath

	resolveTargetUID = func(string) (uint32, error) { return targetUID, nil }
	resolveSystemdUserUnitDir = func(string) (string, error) { return unitDir, nil }
	newSystemctlExecutor = func() systemctl.Executor { return executor }
	lookupUserByName = func(name string) (*user.User, error) {
		return &user.User{Username: name, Uid: strconv.FormatUint(uint64(targetUID), 10)}, nil
	}
	lookupUserByUID = func(uid string) (*user.User, error) {
		return &user.User{Username: "test-user", Uid: uid}, nil
	}
	statPath = func(string) (os.FileInfo, error) {
		return os.Stat(unitDir)
	}

	return func() {
		resolveTargetUID = originalResolveTargetUID
		resolveSystemdUserUnitDir = originalResolveSystemdUserUnitDir
		newSystemctlExecutor = originalNewSystemctlExecutor
		lookupUserByName = originalLookupUserByName
		lookupUserByUID = originalLookupUserByUID
		statPath = originalStatPath
	}
}

func TestLingeringWarningForTargetSkipsRoot(t *testing.T) {
	if warning := lingeringWarningForTarget(0, ""); warning != "" {
		t.Fatalf("lingeringWarningForTarget() = %q, want empty warning for root", warning)
	}
}

func TestLingeringWarningForTargetWarnsWhenLingerFileMissing(t *testing.T) {
	originalLookupUserByName := lookupUserByName
	originalLookupUserByUID := lookupUserByUID
	originalStatPath := statPath
	t.Cleanup(func() {
		lookupUserByName = originalLookupUserByName
		lookupUserByUID = originalLookupUserByUID
		statPath = originalStatPath
	})

	lookupUserByName = func(name string) (*user.User, error) {
		return &user.User{Username: name, Uid: "1000"}, nil
	}
	lookupUserByUID = func(uid string) (*user.User, error) {
		return &user.User{Username: "test-user", Uid: uid}, nil
	}
	statPath = func(string) (os.FileInfo, error) {
		return nil, os.ErrNotExist
	}

	warning := lingeringWarningForTarget(1000, "alice")
	if !strings.Contains(warning, `lingering is not enabled for user "alice"`) {
		t.Fatalf("warning = %q, want lingering warning for alice", warning)
	}
	if !strings.Contains(warning, "loginctl enable-linger alice") {
		t.Fatalf("warning = %q, want enable-linger hint", warning)
	}
}

func managedUnitContent(uid uint32, jobID string) string {
	return strings.Join([]string{
		"# timertab-managed: true",
		"# timertab-uid: " + strconv.FormatUint(uint64(uid), 10),
		"# timertab-job-id: " + jobID,
		"[Unit]",
		"Description=test",
	}, "\n")
}

func assertFileExists(t *testing.T, path string) {
	t.Helper()

	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected file %q to exist: %v", path, err)
	}
}

func assertFileMissing(t *testing.T, path string) {
	t.Helper()

	_, err := os.Stat(path)
	if err == nil {
		t.Fatalf("expected file %q to be missing", path)
	}
	if !os.IsNotExist(err) {
		t.Fatalf("expected os.IsNotExist for %q, got: %v", path, err)
	}
}

type recordingExecutor struct {
	calls []string
}

func (e *recordingExecutor) DaemonReload(_ context.Context) error {
	e.calls = append(e.calls, "daemon-reload")
	return nil
}

func (e *recordingExecutor) EnableTimer(_ context.Context, timerUnit string) error {
	e.calls = append(e.calls, "enable "+timerUnit)
	return nil
}

func (e *recordingExecutor) StartTimer(_ context.Context, timerUnit string) error {
	e.calls = append(e.calls, "start "+timerUnit)
	return nil
}

func (e *recordingExecutor) DisableTimer(_ context.Context, timerUnit string) error {
	e.calls = append(e.calls, "disable "+timerUnit)
	return nil
}

func (e *recordingExecutor) StopTimer(_ context.Context, timerUnit string) error {
	e.calls = append(e.calls, "stop "+timerUnit)
	return nil
}
