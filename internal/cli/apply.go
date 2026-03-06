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
	"github.com/ginden/timertab/internal/reconcile"
	"github.com/ginden/timertab/internal/systemctl"
	"github.com/ginden/timertab/internal/systemd"
)

var (
	resolveTargetUID          = config.ResolveTargetUID
	resolveSystemdUserUnitDir = config.ResolveSystemdUserUnitDir
	renderJobUnits            = systemd.RenderJobUnits
	newSystemctlExecutor      = func() systemctl.Executor { return systemctl.NewCommandExecutor() }
	lookupUserByName          = user.Lookup
	lookupUserByUID           = user.LookupId
	statPath                  = os.Stat
)

type applyDesiredState struct {
	units          []reconcile.DesiredUnit
	enabledTimers  []string
	disabledTimers []string
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
}

func applyEditedConfig(ctx context.Context, cfg *config.File, targetUser string) (applyReport, error) {
	if cfg == nil {
		return applyReport{}, fmt.Errorf("config is required")
	}

	targetUID, err := resolveTargetUID(targetUser)
	if err != nil {
		return applyReport{}, err
	}

	unitDir, err := resolveSystemdUserUnitDir(targetUser)
	if err != nil {
		return applyReport{}, err
	}

	desiredState, err := buildDesiredState(targetUID, cfg.Jobs)
	if err != nil {
		return applyReport{}, err
	}

	existing, err := discoverExistingUnits(unitDir, targetUID)
	if err != nil {
		return applyReport{}, err
	}

	executor := newSystemctlExecutor()
	mutator := &filesystemMutator{
		unitDir:  unitDir,
		executor: executor,
	}

	plan, err := reconcile.Apply(ctx, reconcile.ApplyRequest{
		TargetUID: targetUID,
		Desired:   desiredState.units,
		Existing:  existing,
	}, mutator)
	if err != nil {
		return applyReport{}, fmt.Errorf("reconcile apply: %w", err)
	}

	systemctlPlan := buildSystemctlPlan(desiredState, existing, plan)
	if err := systemctl.RunPlan(ctx, executor, systemctlPlan); err != nil {
		return applyReport{}, err
	}

	report, err := buildApplyReport(unitDir, plan, systemctlPlan)
	if err != nil {
		return applyReport{}, err
	}
	if warning := lingeringWarningForTarget(targetUID, targetUser); warning != "" {
		report.Warnings = append(report.Warnings, warning)
	}

	return report, nil
}

func buildDesiredState(targetUID uint32, jobs []config.Job) (applyDesiredState, error) {
	state := applyDesiredState{
		units:          make([]reconcile.DesiredUnit, 0, len(jobs)*2),
		enabledTimers:  make([]string, 0, len(jobs)),
		disabledTimers: make([]string, 0, len(jobs)),
	}

	for idx, job := range jobs {
		rendered, err := renderJobUnits(targetUID, job)
		if err != nil {
			return applyDesiredState{}, fmt.Errorf("render units for jobs[%d] %q: %w", idx, job.ID, err)
		}

		state.units = append(state.units,
			reconcile.DesiredUnit{Name: rendered.ServiceName, Content: rendered.ServiceContent},
			reconcile.DesiredUnit{Name: rendered.TimerName, Content: rendered.TimerContent},
		)

		if job.IsEnabled() {
			state.enabledTimers = append(state.enabledTimers, rendered.TimerName)
		} else {
			state.disabledTimers = append(state.disabledTimers, rendered.TimerName)
		}
	}

	sort.Slice(state.units, func(i, j int) bool {
		return state.units[i].Name < state.units[j].Name
	})
	state.enabledTimers = sortedUniqueStrings(state.enabledTimers)
	state.disabledTimers = sortedUniqueStrings(state.disabledTimers)

	return state, nil
}

func discoverExistingUnits(unitDir string, targetUID uint32) ([]reconcile.ExistingUnit, error) {
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
		if !reconcile.IsManagedUnitForUID(targetUID, name) {
			continue
		}

		path := filepath.Join(unitDir, name)
		contentBytes, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read existing unit %q: %w", path, err)
		}
		content := string(contentBytes)

		existing = append(existing, reconcile.ExistingUnit{
			Name:    name,
			Content: content,
			Managed: systemd.IsManagedUnitContentForUID(content, targetUID),
		})
	}

	sort.Slice(existing, func(i, j int) bool {
		return existing[i].Name < existing[j].Name
	})

	return existing, nil
}

func buildSystemctlPlan(state applyDesiredState, existing []reconcile.ExistingUnit, plan reconcile.Plan) systemctl.Plan {
	existingManagedTimers := make(map[string]struct{}, len(existing))
	for _, unit := range existing {
		if !unit.Managed || !strings.HasSuffix(unit.Name, ".timer") {
			continue
		}
		existingManagedTimers[unit.Name] = struct{}{}
	}

	removedTimers := make(map[string]struct{}, len(plan.Remove))
	for _, unitName := range plan.Remove {
		if strings.HasSuffix(unitName, ".timer") {
			removedTimers[unitName] = struct{}{}
		}
	}

	toDisable := make([]string, 0, len(state.disabledTimers))
	for _, timerName := range state.disabledTimers {
		if _, removed := removedTimers[timerName]; removed {
			continue
		}
		if _, exists := existingManagedTimers[timerName]; exists {
			toDisable = append(toDisable, timerName)
		}
	}

	return systemctl.Plan{
		TimersToDisable: sortedUniqueStrings(toDisable),
		TimersToEnable:  sortedUniqueStrings(state.enabledTimers),
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

func buildApplyReport(unitDir string, plan reconcile.Plan, systemctlPlan systemctl.Plan) (applyReport, error) {
	report := applyReport{
		Created:        make([]string, 0, len(plan.Create)),
		Modified:       make([]string, 0, len(plan.Update)),
		Deleted:        make([]string, 0, len(plan.Remove)),
		DisabledTimers: append([]string(nil), systemctlPlan.TimersToDisable...),
		StoppedTimers:  append([]string(nil), systemctlPlan.TimersToDisable...),
		EnabledTimers:  append([]string(nil), systemctlPlan.TimersToEnable...),
		StartedTimers:  append([]string(nil), systemctlPlan.TimersToEnable...),
		ReloadedDaemon: len(systemctlPlan.TimersToEnable) > 0,
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

func lingeringWarningForTarget(targetUID uint32, targetUser string) string {
	if targetUID == 0 {
		return ""
	}

	username, err := resolveTargetUsername(targetUID, targetUser)
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
		"warning: lingering is not enabled for user %q; timers may not run without an active login session (enable with: loginctl enable-linger %s)",
		username,
		username,
	)
}

func resolveTargetUsername(targetUID uint32, targetUser string) (string, error) {
	normalizedTargetUser := strings.TrimSpace(targetUser)
	if normalizedTargetUser != "" {
		u, err := lookupUserByName(normalizedTargetUser)
		if err != nil {
			return "", err
		}
		return u.Username, nil
	}

	u, err := lookupUserByUID(strconv.FormatUint(uint64(targetUID), 10))
	if err != nil {
		return "", err
	}
	return u.Username, nil
}
