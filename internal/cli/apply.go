package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/ginden/timertab/internal/config"
	"github.com/ginden/timertab/internal/progress"
	"github.com/ginden/timertab/internal/reconcile"
	"github.com/ginden/timertab/internal/systemctl"
	"github.com/ginden/timertab/internal/systemd"
)

var (
	resolveCurrentUID     = config.ResolveCurrentUID
	resolveSystemdUnitDir = config.ResolveSystemdUnitDirForUID
	renderJobUnits        = systemd.RenderJobUnits
	newSystemctlExecutor  = func(targetUID uint32) systemctl.Executor { return systemctl.NewCommandExecutorForUID(targetUID) }
	lookupUserByUID       = user.LookupId
	statPath              = os.Stat
)

type applyDesiredState struct {
	units             []reconcile.DesiredUnit
	enabledTimers     []string
	disabledTimers    []string
	timersToSkipStart map[string]struct{}
}

type applyReport struct {
	Created        []string
	Modified       []string
	Deleted        []string
	ReloadedDaemon bool
	DisabledTimers []string
	StoppedTimers  []string
	EnabledTimers  []string
	StartedTimers  []string
	Warnings       []string
	DaemonLabel    string
}

type timerRuntimeState struct {
	Missing       bool
	UnitFileState string
	ActiveState   string
}

func applyEditedConfig(ctx context.Context, cfg *config.File) (applyReport, error) {
	if cfg == nil {
		return applyReport{}, fmt.Errorf("config is required")
	}

	targetUID, err := resolveCurrentUID()
	if err != nil {
		return applyReport{}, err
	}
	instanceID := cfg.EffectiveInstanceID()

	unitDir, err := resolveSystemdUnitDir(targetUID)
	if err != nil {
		return applyReport{}, err
	}

	progress.PrintfLevel(ctx, 2, "timertab: rendering desired units")
	desiredState, err := buildDesiredState(targetUID, instanceID, cfg.Jobs)
	if err != nil {
		return applyReport{}, err
	}

	progress.PrintfLevel(ctx, 2, "timertab: scanning existing systemd units in %s", unitDir)
	existing, err := discoverExistingUnits(unitDir, targetUID, instanceID)
	if err != nil {
		return applyReport{}, err
	}

	scope := systemctl.ScopeForUID(targetUID)
	executor := newSystemctlExecutor(targetUID)
	mutator := &filesystemMutator{
		unitDir:  unitDir,
		executor: executor,
	}

	plan, err := reconcile.Apply(ctx, reconcile.ApplyRequest{
		TargetUID:  targetUID,
		InstanceID: instanceID,
		Desired:    desiredState.units,
		Existing:   existing,
	}, mutator)
	if err != nil {
		return applyReport{}, fmt.Errorf("reconcile apply: %w", err)
	}

	timerStates, err := discoverTimerRuntimeStates(ctx, scope, plan)
	if err != nil {
		return applyReport{}, err
	}

	systemctlPlan := buildSystemctlPlan(desiredState, plan, timerStates)
	progress.PrintfLevel(ctx, 2, "timertab: applying systemd manager operations")
	if err := systemctl.RunPlan(ctx, executor, systemctlPlan); err != nil {
		return applyReport{}, err
	}

	report, err := buildApplyReport(targetUID, unitDir, plan, systemctlPlan)
	if err != nil {
		return applyReport{}, err
	}
	if warning := lingeringWarningForCurrentUser(targetUID); warning != "" {
		report.Warnings = append(report.Warnings, warning)
	}

	return report, nil
}

func previewEditedConfig(_ context.Context, cfg *config.File) (applyReport, error) {
	if cfg == nil {
		return applyReport{}, fmt.Errorf("config is required")
	}

	targetUID, err := resolveCurrentUID()
	if err != nil {
		return applyReport{}, err
	}
	instanceID := cfg.EffectiveInstanceID()

	unitDir, err := resolveSystemdUnitDir(targetUID)
	if err != nil {
		return applyReport{}, err
	}

	desiredState, err := buildDesiredState(targetUID, instanceID, cfg.Jobs)
	if err != nil {
		return applyReport{}, err
	}

	existing, err := discoverExistingUnits(unitDir, targetUID, instanceID)
	if err != nil {
		return applyReport{}, err
	}

	plan, err := reconcile.BuildPlan(targetUID, instanceID, desiredState.units, existing)
	if err != nil {
		return applyReport{}, fmt.Errorf("reconcile build plan: %w", err)
	}

	report := applyReport{
		Created:  make([]string, 0, len(plan.Create)),
		Modified: make([]string, 0, len(plan.Update)),
		Deleted:  make([]string, 0, len(plan.Remove)),
	}
	for _, unit := range plan.Create {
		report.Created = append(report.Created, filepath.Join(unitDir, unit.Name))
	}
	for _, unit := range plan.Update {
		report.Modified = append(report.Modified, filepath.Join(unitDir, unit.Name))
	}
	for _, unitName := range plan.Remove {
		report.Deleted = append(report.Deleted, filepath.Join(unitDir, unitName))
	}

	return report, nil
}

func buildDesiredState(targetUID uint32, instanceID string, jobs []config.Job) (applyDesiredState, error) {
	state := applyDesiredState{
		units:             make([]reconcile.DesiredUnit, 0, len(jobs)*2),
		enabledTimers:     make([]string, 0, len(jobs)),
		disabledTimers:    make([]string, 0, len(jobs)),
		timersToSkipStart: make(map[string]struct{}),
	}

	for idx, job := range jobs {
		rendered, err := renderJobUnits(targetUID, instanceID, job)
		if err != nil {
			return applyDesiredState{}, fmt.Errorf("render units for jobs[%d] %q: %w", idx, job.ID, err)
		}

		state.units = append(state.units,
			reconcile.DesiredUnit{Name: rendered.ServiceName, Content: rendered.ServiceContent},
			reconcile.DesiredUnit{Name: rendered.TimerName, Content: rendered.TimerContent},
		)

		if job.IsEnabled() {
			state.enabledTimers = append(state.enabledTimers, rendered.TimerName)
			if isRebootOnlyTimer(job) {
				state.timersToSkipStart[rendered.TimerName] = struct{}{}
			}
		} else {
			state.disabledTimers = append(state.disabledTimers, rendered.TimerName)
		}
	}

	// Normalize order before planning so dry-runs, tests, and auto-generated
	// diffs stay stable regardless of input job ordering.
	sort.Slice(state.units, func(i, j int) bool {
		return state.units[i].Name < state.units[j].Name
	})
	state.enabledTimers = sortedUniqueStrings(state.enabledTimers)
	state.disabledTimers = sortedUniqueStrings(state.disabledTimers)

	return state, nil
}

func discoverExistingUnits(unitDir string, targetUID uint32, instanceID string) ([]reconcile.ExistingUnit, error) {
	entries, err := os.ReadDir(unitDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("read unit directory %q: %w", unitDir, err)
	}

	existing := make([]reconcile.ExistingUnit, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		name := entry.Name()
		if !reconcile.IsManagedUnitForUID(targetUID, instanceID, name) {
			continue
		}

		path := filepath.Join(unitDir, name)
		contentBytes, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read existing unit %q: %w", path, err)
		}
		content := string(contentBytes)

		// Name prefix narrows the search space; embedded markers decide whether the
		// file is actually ours and therefore safe to mutate or prune.
		existing = append(existing, reconcile.ExistingUnit{
			Name:    name,
			Content: content,
			Managed: systemd.IsManagedUnitContentForUID(content, targetUID, instanceID),
		})
	}

	sort.Slice(existing, func(i, j int) bool {
		return existing[i].Name < existing[j].Name
	})

	return existing, nil
}

func discoverTimerRuntimeStates(ctx context.Context, scope systemctl.Scope, plan reconcile.Plan) (map[string]timerRuntimeState, error) {
	timerNames := make([]string, 0, len(plan.Keep)+len(plan.Update))
	for _, unit := range plan.Update {
		if strings.HasSuffix(unit.Name, ".timer") {
			timerNames = append(timerNames, unit.Name)
		}
	}
	for _, unitName := range plan.Keep {
		if strings.HasSuffix(unitName, ".timer") {
			timerNames = append(timerNames, unitName)
		}
	}

	timerNames = sortedUniqueStrings(timerNames)
	states := make(map[string]timerRuntimeState, len(timerNames))
	for _, timerName := range timerNames {
		props, missing, err := showUnitProperties(ctx, scope, timerName, "UnitFileState", "ActiveState")
		if err != nil {
			return nil, fmt.Errorf("inspect timer state for %q: %w", timerName, err)
		}

		state := timerRuntimeState{Missing: missing}
		if !missing {
			state.UnitFileState = strings.TrimSpace(props["UnitFileState"])
			state.ActiveState = strings.TrimSpace(props["ActiveState"])
		}
		states[timerName] = state
	}

	return states, nil
}

func buildSystemctlPlan(state applyDesiredState, plan reconcile.Plan, timerStates map[string]timerRuntimeState) systemctl.Plan {
	enabledTimers := make(map[string]struct{}, len(state.enabledTimers))
	for _, timerName := range state.enabledTimers {
		enabledTimers[timerName] = struct{}{}
	}

	disabledTimers := make(map[string]struct{}, len(state.disabledTimers))
	for _, timerName := range state.disabledTimers {
		disabledTimers[timerName] = struct{}{}
	}

	toDisable := make([]string, 0, len(state.disabledTimers))
	toEnable := make([]string, 0, len(state.enabledTimers))
	toStart := make([]string, 0, len(state.enabledTimers))
	for _, unit := range plan.Create {
		if !strings.HasSuffix(unit.Name, ".timer") {
			continue
		}
		if _, enabled := enabledTimers[unit.Name]; enabled {
			toEnable = append(toEnable, unit.Name)
			if timerShouldStartOnApply(state, unit.Name) {
				toStart = append(toStart, unit.Name)
			}
		}
	}
	for _, unit := range plan.Update {
		if !strings.HasSuffix(unit.Name, ".timer") {
			continue
		}

		if _, enabled := enabledTimers[unit.Name]; enabled {
			toEnable = append(toEnable, unit.Name)
			if timerShouldStartOnApply(state, unit.Name) {
				toStart = append(toStart, unit.Name)
			}
			continue
		}

		if _, disabled := disabledTimers[unit.Name]; disabled && timerNeedsDisableStop(timerStates[unit.Name]) {
			toDisable = append(toDisable, unit.Name)
		}
	}
	for _, unitName := range plan.Keep {
		if !strings.HasSuffix(unitName, ".timer") {
			continue
		}

		if _, enabled := enabledTimers[unitName]; enabled {
			if timerNeedsEnable(timerStates[unitName]) {
				toEnable = append(toEnable, unitName)
			}
			if timerShouldStartOnApply(state, unitName) && timerNeedsStart(timerStates[unitName]) {
				toStart = append(toStart, unitName)
			}
			continue
		}

		if _, disabled := disabledTimers[unitName]; disabled && timerNeedsDisableStop(timerStates[unitName]) {
			toDisable = append(toDisable, unitName)
		}
	}

	return systemctl.Plan{
		TimersToDisable: sortedUniqueStrings(toDisable),
		TimersToEnable:  sortedUniqueStrings(toEnable),
		TimersToStart:   sortedUniqueStrings(toStart),
		ReloadDaemon:    len(plan.Create) > 0 || len(plan.Update) > 0 || len(plan.Remove) > 0,
	}
}

func timerNeedsEnable(state timerRuntimeState) bool {
	if state.Missing {
		return true
	}
	return !isEnabledTimerUnitFileState(state.UnitFileState)
}

func timerNeedsStart(state timerRuntimeState) bool {
	if state.Missing {
		return true
	}
	return !isStartedTimerActiveState(state.ActiveState)
}

func timerNeedsDisableStop(state timerRuntimeState) bool {
	if state.Missing {
		return false
	}
	return isEnabledTimerUnitFileState(state.UnitFileState) || isStartedTimerActiveState(state.ActiveState)
}

func timerShouldStartOnApply(state applyDesiredState, timerName string) bool {
	_, skip := state.timersToSkipStart[timerName]
	return !skip
}

func isRebootOnlyTimer(job config.Job) bool {
	if !isRebootOnlySchedule(job.When) {
		return false
	}
	if job.Systemd == nil || job.Systemd.Timer == nil {
		return true
	}
	for _, directive := range job.Systemd.Timer.Directives() {
		name := strings.TrimSpace(directive.Name)
		if isTimerTriggerDirective(name) && !strings.EqualFold(name, "OnBootSec") {
			return false
		}
	}
	return true
}

func isRebootOnlySchedule(when config.ScheduleList) bool {
	if len(when) == 0 {
		return false
	}
	for _, schedule := range when {
		if strings.TrimSpace(schedule) != "@reboot" {
			return false
		}
	}
	return true
}

func isTimerTriggerDirective(name string) bool {
	switch strings.TrimSpace(name) {
	case "OnActiveSec", "OnBootSec", "OnStartupSec", "OnUnitActiveSec", "OnUnitInactiveSec", "OnCalendar":
		return true
	default:
		return false
	}
}

func isEnabledTimerUnitFileState(state string) bool {
	switch strings.TrimSpace(state) {
	case "enabled", "enabled-runtime", "linked", "linked-runtime", "alias":
		return true
	default:
		return false
	}
}

func isStartedTimerActiveState(state string) bool {
	switch strings.TrimSpace(state) {
	case "active", "activating", "reloading":
		return true
	default:
		return false
	}
}

type filesystemMutator struct {
	unitDir  string
	executor systemctl.Executor
}

func (m *filesystemMutator) WriteUnit(_ context.Context, unit reconcile.DesiredUnit) error {
	path, err := unitFilePath(m.unitDir, unit.Name)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(m.unitDir, 0o755); err != nil {
		return err
	}

	tmp, err := os.CreateTemp(m.unitDir, unit.Name+".*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	cleanupTemp := true
	defer func() {
		if cleanupTemp {
			_ = os.Remove(tmpName)
		}
	}()

	if _, err := tmp.WriteString(unit.Content); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(0o644); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		return err
	}
	cleanupTemp = false

	return nil
}

func (m *filesystemMutator) DisableUnit(ctx context.Context, unitName string) error {
	if !strings.HasSuffix(unitName, ".timer") {
		return nil
	}
	return m.executor.DisableTimer(ctx, unitName)
}

func (m *filesystemMutator) StopUnit(ctx context.Context, unitName string) error {
	if !strings.HasSuffix(unitName, ".timer") {
		return nil
	}
	return m.executor.StopTimer(ctx, unitName)
}

func (m *filesystemMutator) RemoveUnitFile(unitName string) error {
	path, err := unitFilePath(m.unitDir, unitName)
	if err != nil {
		return err
	}

	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func unitFilePath(unitDir, unitName string) (string, error) {
	if unitName != filepath.Base(unitName) {
		return "", fmt.Errorf("invalid unit name %q", unitName)
	}
	return filepath.Join(unitDir, unitName), nil
}

func sortedUniqueStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}

	out := append([]string(nil), values...)
	sort.Strings(out)

	unique := out[:1]
	for _, value := range out[1:] {
		if value == unique[len(unique)-1] {
			continue
		}
		unique = append(unique, value)
	}

	return unique
}

func buildApplyReport(targetUID uint32, unitDir string, plan reconcile.Plan, systemctlPlan systemctl.Plan) (applyReport, error) {
	report := applyReport{
		Created:        make([]string, 0, len(plan.Create)),
		Modified:       make([]string, 0, len(plan.Update)),
		Deleted:        make([]string, 0, len(plan.Remove)),
		DisabledTimers: append([]string(nil), systemctlPlan.TimersToDisable...),
		StoppedTimers:  append([]string(nil), systemctlPlan.TimersToDisable...),
		EnabledTimers:  append([]string(nil), systemctlPlan.TimersToEnable...),
		StartedTimers:  append([]string(nil), systemctlPlan.TimersToStart...),
		ReloadedDaemon: systemctlPlan.ReloadDaemon,
		DaemonLabel:    systemctl.ScopeForUID(targetUID).DaemonLabel(),
	}

	for _, unit := range plan.Create {
		path, err := unitFilePath(unitDir, unit.Name)
		if err != nil {
			return applyReport{}, err
		}
		report.Created = append(report.Created, path)
	}

	for _, unit := range plan.Update {
		path, err := unitFilePath(unitDir, unit.Name)
		if err != nil {
			return applyReport{}, err
		}
		report.Modified = append(report.Modified, path)
	}

	for _, unitName := range plan.Remove {
		path, err := unitFilePath(unitDir, unitName)
		if err != nil {
			return applyReport{}, err
		}
		report.Deleted = append(report.Deleted, path)
	}

	return report, nil
}

func lingeringWarningForCurrentUser(targetUID uint32) string {
	if targetUID == 0 {
		return ""
	}

	username, err := resolveCurrentUsername(targetUID)
	if err != nil || strings.TrimSpace(username) == "" {
		return ""
	}

	lingerPath := filepath.Join("/var/lib/systemd/linger", username)
	if _, err := statPath(lingerPath); err == nil {
		return ""
	} else if !errors.Is(err, os.ErrNotExist) {
		return ""
	}

	return fmt.Sprintf(
		"lingering is not enabled for user %q; timers may not run without an active login session (enable with: loginctl enable-linger %s)",
		username,
		username,
	)
}

func resolveCurrentUsername(targetUID uint32) (string, error) {
	u, err := lookupUserByUID(strconv.FormatUint(uint64(targetUID), 10))
	if err != nil {
		return "", err
	}
	return u.Username, nil
}
