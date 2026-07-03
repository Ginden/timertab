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
		Run:  config.ShellCommand("echo keep"),
	}
	rendered, err := systemd.RenderJobUnits(targetUID, config.DefaultInstanceID, keepJob)
	if err != nil {
		t.Fatalf("RenderJobUnits() error = %v", err)
	}

	staleTimer := "timertab-u1000-stale-a.timer"
	staleService := "timertab-u1000-stale-a.service"
	if err := os.WriteFile(filepath.Join(unitDir, staleTimer), []byte(managedUnitContent(targetUID, config.DefaultInstanceID, "stale-a")), 0o644); err != nil {
		t.Fatalf("WriteFile(stale timer) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(unitDir, staleService), []byte(managedUnitContent(targetUID, config.DefaultInstanceID, "stale-a")), 0o644); err != nil {
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
	report, err := applyEditedConfig(context.Background(), cfg)
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
		DaemonLabel:    systemctl.UserScope.DaemonLabel(),
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

func TestApplyEditedConfigEnablesRebootTimerWithoutStartingOnCreate(t *testing.T) {
	unitDir := t.TempDir()
	targetUID := uint32(1000)

	job := config.Job{
		ID:   "job-reboot",
		When: config.ScheduleList{"@reboot"},
		Run:  config.ShellCommand("echo reboot"),
	}
	rendered, err := systemd.RenderJobUnits(targetUID, config.DefaultInstanceID, job)
	if err != nil {
		t.Fatalf("RenderJobUnits() error = %v", err)
	}

	fakeExecutor := &recordingExecutor{}
	restore := stubApplyDeps(t, targetUID, unitDir, fakeExecutor)
	defer restore()

	cfg := &config.File{
		Version: 1,
		Jobs:    []config.Job{job},
	}
	report, err := applyEditedConfig(context.Background(), cfg)
	if err != nil {
		t.Fatalf("applyEditedConfig() error = %v", err)
	}

	wantReport := applyReport{
		Created: []string{
			filepath.Join(unitDir, rendered.ServiceName),
			filepath.Join(unitDir, rendered.TimerName),
		},
		Modified:       []string{},
		Deleted:        []string{},
		ReloadedDaemon: true,
		DisabledTimers: nil,
		StoppedTimers:  nil,
		EnabledTimers:  []string{rendered.TimerName},
		StartedTimers:  nil,
		DaemonLabel:    systemctl.UserScope.DaemonLabel(),
	}
	if !reflect.DeepEqual(report, wantReport) {
		t.Fatalf("apply report = %#v, want %#v", report, wantReport)
	}

	wantCalls := []string{
		"daemon-reload",
		"enable " + rendered.TimerName,
	}
	if !reflect.DeepEqual(fakeExecutor.calls, wantCalls) {
		t.Fatalf("systemctl calls = %v, want %v", fakeExecutor.calls, wantCalls)
	}

	assertFileExists(t, filepath.Join(unitDir, rendered.ServiceName))
	assertFileExists(t, filepath.Join(unitDir, rendered.TimerName))
}

func TestApplyEditedConfigDisablesExistingTimersForDisabledJobs(t *testing.T) {
	unitDir := t.TempDir()
	targetUID := uint32(1000)

	disabled := false
	job := config.Job{
		ID:      "job-disabled",
		When:    config.ScheduleList{"@daily"},
		Run:     config.ShellCommand("echo disabled"),
		Enabled: &disabled,
	}
	rendered, err := systemd.RenderJobUnits(targetUID, config.DefaultInstanceID, job)
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
	originalRunSystemctlShow := runSystemctlShow
	t.Cleanup(func() {
		runSystemctlShow = originalRunSystemctlShow
	})
	runSystemctlShow = func(_ context.Context, args ...string) (string, string, error) {
		want := []string{"--user", "show", rendered.TimerName, "--property=UnitFileState", "--property=ActiveState"}
		if !reflect.DeepEqual(args, want) {
			t.Fatalf("runSystemctlShow args = %v, want %v", args, want)
		}
		return "UnitFileState=enabled\nActiveState=active\n", "", nil
	}

	cfg := &config.File{
		Version: 1,
		Jobs:    []config.Job{job},
	}
	report, err := applyEditedConfig(context.Background(), cfg)
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
		DaemonLabel:    systemctl.UserScope.DaemonLabel(),
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

func TestApplyEditedConfigSkipsUnchangedEnabledTimersAlreadyRunning(t *testing.T) {
	unitDir := t.TempDir()
	targetUID := uint32(1000)

	job := config.Job{
		ID:   "job-enabled",
		When: config.ScheduleList{"@daily"},
		Run:  config.ShellCommand("echo enabled"),
	}
	rendered, err := systemd.RenderJobUnits(targetUID, config.DefaultInstanceID, job)
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
	originalRunSystemctlShow := runSystemctlShow
	t.Cleanup(func() {
		runSystemctlShow = originalRunSystemctlShow
	})
	runSystemctlShow = func(_ context.Context, args ...string) (string, string, error) {
		want := []string{"--user", "show", rendered.TimerName, "--property=UnitFileState", "--property=ActiveState"}
		if !reflect.DeepEqual(args, want) {
			t.Fatalf("runSystemctlShow args = %v, want %v", args, want)
		}
		return "UnitFileState=enabled\nActiveState=active\n", "", nil
	}

	cfg := &config.File{
		Version: 1,
		Jobs:    []config.Job{job},
	}
	report, err := applyEditedConfig(context.Background(), cfg)
	if err != nil {
		t.Fatalf("applyEditedConfig() error = %v", err)
	}

	wantReport := applyReport{
		Created:        []string{},
		Modified:       []string{},
		Deleted:        []string{},
		ReloadedDaemon: false,
		DisabledTimers: nil,
		StoppedTimers:  nil,
		EnabledTimers:  nil,
		StartedTimers:  nil,
		DaemonLabel:    systemctl.UserScope.DaemonLabel(),
	}
	if !reflect.DeepEqual(report, wantReport) {
		t.Fatalf("apply report = %#v, want %#v", report, wantReport)
	}
	if len(fakeExecutor.calls) != 0 {
		t.Fatalf("systemctl calls = %v, want none", fakeExecutor.calls)
	}
}

func TestApplyEditedConfigSkipsDisabledTimersAlreadyStopped(t *testing.T) {
	unitDir := t.TempDir()
	targetUID := uint32(1000)

	disabled := false
	job := config.Job{
		ID:      "job-disabled",
		When:    config.ScheduleList{"@daily"},
		Run:     config.ShellCommand("echo disabled"),
		Enabled: &disabled,
	}
	rendered, err := systemd.RenderJobUnits(targetUID, config.DefaultInstanceID, job)
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
	originalRunSystemctlShow := runSystemctlShow
	t.Cleanup(func() {
		runSystemctlShow = originalRunSystemctlShow
	})
	runSystemctlShow = func(_ context.Context, args ...string) (string, string, error) {
		want := []string{"--user", "show", rendered.TimerName, "--property=UnitFileState", "--property=ActiveState"}
		if !reflect.DeepEqual(args, want) {
			t.Fatalf("runSystemctlShow args = %v, want %v", args, want)
		}
		return "UnitFileState=disabled\nActiveState=inactive\n", "", nil
	}

	cfg := &config.File{
		Version: 1,
		Jobs:    []config.Job{job},
	}
	report, err := applyEditedConfig(context.Background(), cfg)
	if err != nil {
		t.Fatalf("applyEditedConfig() error = %v", err)
	}

	wantReport := applyReport{
		Created:        []string{},
		Modified:       []string{},
		Deleted:        []string{},
		ReloadedDaemon: false,
		DisabledTimers: nil,
		StoppedTimers:  nil,
		EnabledTimers:  nil,
		StartedTimers:  nil,
		DaemonLabel:    systemctl.UserScope.DaemonLabel(),
	}
	if !reflect.DeepEqual(report, wantReport) {
		t.Fatalf("apply report = %#v, want %#v", report, wantReport)
	}
	if len(fakeExecutor.calls) != 0 {
		t.Fatalf("systemctl calls = %v, want none", fakeExecutor.calls)
	}
}

func TestApplyEditedConfigStartsExistingTimerWhenRuntimeStateDrifted(t *testing.T) {
	unitDir := t.TempDir()
	targetUID := uint32(1000)

	job := config.Job{
		ID:   "job-enabled",
		When: config.ScheduleList{"@daily"},
		Run:  config.ShellCommand("echo enabled"),
	}
	rendered, err := systemd.RenderJobUnits(targetUID, config.DefaultInstanceID, job)
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
	originalRunSystemctlShow := runSystemctlShow
	t.Cleanup(func() {
		runSystemctlShow = originalRunSystemctlShow
	})
	runSystemctlShow = func(_ context.Context, args ...string) (string, string, error) {
		want := []string{"--user", "show", rendered.TimerName, "--property=UnitFileState", "--property=ActiveState"}
		if !reflect.DeepEqual(args, want) {
			t.Fatalf("runSystemctlShow args = %v, want %v", args, want)
		}
		return "UnitFileState=disabled\nActiveState=inactive\n", "", nil
	}

	cfg := &config.File{
		Version: 1,
		Jobs:    []config.Job{job},
	}
	report, err := applyEditedConfig(context.Background(), cfg)
	if err != nil {
		t.Fatalf("applyEditedConfig() error = %v", err)
	}

	wantReport := applyReport{
		Created:        []string{},
		Modified:       []string{},
		Deleted:        []string{},
		ReloadedDaemon: false,
		DisabledTimers: nil,
		StoppedTimers:  nil,
		EnabledTimers:  []string{rendered.TimerName},
		StartedTimers:  []string{rendered.TimerName},
		DaemonLabel:    systemctl.UserScope.DaemonLabel(),
	}
	if !reflect.DeepEqual(report, wantReport) {
		t.Fatalf("apply report = %#v, want %#v", report, wantReport)
	}

	wantCalls := []string{
		"enable " + rendered.TimerName,
		"start " + rendered.TimerName,
	}
	if !reflect.DeepEqual(fakeExecutor.calls, wantCalls) {
		t.Fatalf("systemctl calls = %v, want %v", fakeExecutor.calls, wantCalls)
	}
}

func TestApplyEditedConfigReloadsDaemonAfterPruningWithoutEnabledTimers(t *testing.T) {
	unitDir := t.TempDir()
	targetUID := uint32(1000)

	staleTimer := "timertab-u1000-stale.timer"
	staleService := "timertab-u1000-stale.service"
	if err := os.WriteFile(filepath.Join(unitDir, staleTimer), []byte(managedUnitContent(targetUID, config.DefaultInstanceID, "stale")), 0o644); err != nil {
		t.Fatalf("WriteFile(stale timer) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(unitDir, staleService), []byte(managedUnitContent(targetUID, config.DefaultInstanceID, "stale")), 0o644); err != nil {
		t.Fatalf("WriteFile(stale service) error = %v", err)
	}

	fakeExecutor := &recordingExecutor{}
	restore := stubApplyDeps(t, targetUID, unitDir, fakeExecutor)
	defer restore()

	cfg := &config.File{
		Version: 1,
		Jobs:    []config.Job{},
	}
	report, err := applyEditedConfig(context.Background(), cfg)
	if err != nil {
		t.Fatalf("applyEditedConfig() error = %v", err)
	}

	wantReport := applyReport{
		Created:  []string{},
		Modified: []string{},
		Deleted: []string{
			filepath.Join(unitDir, staleService),
			filepath.Join(unitDir, staleTimer),
		},
		ReloadedDaemon: true,
		DisabledTimers: nil,
		StoppedTimers:  nil,
		EnabledTimers:  nil,
		StartedTimers:  nil,
		DaemonLabel:    systemctl.UserScope.DaemonLabel(),
	}
	if !reflect.DeepEqual(report, wantReport) {
		t.Fatalf("apply report = %#v, want %#v", report, wantReport)
	}

	wantCalls := []string{
		"disable " + staleTimer,
		"stop " + staleTimer,
		"daemon-reload",
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

	if err := os.WriteFile(filepath.Join(unitDir, managedName), []byte(managedUnitContent(1000, config.DefaultInstanceID, "managed")), 0o644); err != nil {
		t.Fatalf("WriteFile(managed) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(unitDir, unmanagedName), []byte("[Timer]\nOnCalendar=@hourly"), 0o644); err != nil {
		t.Fatalf("WriteFile(unmanaged) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(unitDir, foreignName), []byte(managedUnitContent(1001, config.DefaultInstanceID, "foreign")), 0o644); err != nil {
		t.Fatalf("WriteFile(foreign) error = %v", err)
	}

	got, err := discoverExistingUnits(unitDir, 1000, config.DefaultInstanceID)
	if err != nil {
		t.Fatalf("discoverExistingUnits() error = %v", err)
	}

	want := []reconcile.ExistingUnit{
		{Name: managedName, Content: managedUnitContent(1000, config.DefaultInstanceID, "managed"), Managed: true},
		{Name: unmanagedName, Content: "[Timer]\nOnCalendar=@hourly", Managed: false},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("discoverExistingUnits() = %#v, want %#v", got, want)
	}
}

func TestDiscoverExistingUnitsTreatsLegacyDefaultInstanceUnitsAsManaged(t *testing.T) {
	unitDir := t.TempDir()

	legacyManagedName := "timertab-u1000-legacy.timer"
	legacyManagedContent := strings.Join([]string{
		"# timertab-managed: true",
		"# timertab-uid: 1000",
		"# timertab-job-id: legacy",
		"[Timer]",
		"OnCalendar=@hourly",
	}, "\n")

	if err := os.WriteFile(filepath.Join(unitDir, legacyManagedName), []byte(legacyManagedContent), 0o644); err != nil {
		t.Fatalf("WriteFile(legacy managed) error = %v", err)
	}

	got, err := discoverExistingUnits(unitDir, 1000, config.DefaultInstanceID)
	if err != nil {
		t.Fatalf("discoverExistingUnits() error = %v", err)
	}

	want := []reconcile.ExistingUnit{
		{Name: legacyManagedName, Content: legacyManagedContent, Managed: true},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("discoverExistingUnits() = %#v, want %#v", got, want)
	}
}

func stubApplyDeps(t *testing.T, targetUID uint32, unitDir string, executor systemctl.Executor) func() {
	t.Helper()

	originalResolveCurrentUID := resolveCurrentUID
	originalResolveSystemdUnitDir := resolveSystemdUnitDir
	originalNewSystemctlExecutor := newSystemctlExecutor
	originalLookupUserByUID := lookupUserByUID
	originalStatPath := statPath

	resolveCurrentUID = func() (uint32, error) { return targetUID, nil }
	resolveSystemdUnitDir = func(gotUID uint32) (string, error) {
		if gotUID != targetUID {
			t.Fatalf("resolveSystemdUnitDir() uid = %d, want %d", gotUID, targetUID)
		}
		return unitDir, nil
	}
	newSystemctlExecutor = func(gotUID uint32) systemctl.Executor {
		if gotUID != targetUID {
			t.Fatalf("newSystemctlExecutor() uid = %d, want %d", gotUID, targetUID)
		}
		return executor
	}
	lookupUserByUID = func(uid string) (*user.User, error) {
		return &user.User{Username: "test-user", Uid: uid}, nil
	}
	statPath = func(string) (os.FileInfo, error) {
		return os.Stat(unitDir)
	}

	return func() {
		resolveCurrentUID = originalResolveCurrentUID
		resolveSystemdUnitDir = originalResolveSystemdUnitDir
		newSystemctlExecutor = originalNewSystemctlExecutor
		lookupUserByUID = originalLookupUserByUID
		statPath = originalStatPath
	}
}

func TestLingeringWarningForCurrentUserSkipsRoot(t *testing.T) {
	if warning := lingeringWarningForCurrentUser(0); warning != "" {
		t.Fatalf("lingeringWarningForCurrentUser() = %q, want empty warning for root", warning)
	}
}

func TestLingeringWarningForCurrentUserWarnsWhenLingerFileMissing(t *testing.T) {
	originalLookupUserByUID := lookupUserByUID
	originalStatPath := statPath
	t.Cleanup(func() {
		lookupUserByUID = originalLookupUserByUID
		statPath = originalStatPath
	})

	lookupUserByUID = func(uid string) (*user.User, error) {
		return &user.User{Username: "alice", Uid: uid}, nil
	}
	statPath = func(string) (os.FileInfo, error) {
		return nil, os.ErrNotExist
	}

	warning := lingeringWarningForCurrentUser(1000)
	if !strings.Contains(warning, `lingering is not enabled for user "alice"`) {
		t.Fatalf("warning = %q, want lingering warning for alice", warning)
	}
	if !strings.Contains(warning, "loginctl enable-linger alice") {
		t.Fatalf("warning = %q, want enable-linger hint", warning)
	}
}

func managedUnitContent(uid uint32, instanceID, jobID string) string {
	return strings.Join([]string{
		"# timertab-managed: true",
		"# timertab-uid: " + strconv.FormatUint(uint64(uid), 10),
		"# timertab-instance-id: " + instanceID,
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

func (e *recordingExecutor) StartService(_ context.Context, serviceUnit string) error {
	e.calls = append(e.calls, "start-service "+serviceUnit)
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
