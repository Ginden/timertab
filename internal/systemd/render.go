package systemd

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"unicode"

	"github.com/ginden/timertab/internal/config"
)

const (
	managedMarkerLine = "# timertab-managed: true"
	uidMarkerPrefix   = "# timertab-uid: "
	jobIDMarkerPrefix = "# timertab-job-id: "
)

type RenderedUnits struct {
	BaseName       string
	ServiceName    string
	TimerName      string
	ServiceContent string
	TimerContent   string
}

func IsManagedUnitContentForUID(content string, targetUID uint32) bool {
	var (
		hasManagedMarker bool
		hasUIDMarker     bool
		hasJobIDMarker   bool
	)

	targetUIDString := fmt.Sprintf("%d", targetUID)
	for _, rawLine := range strings.Split(content, "\n") {
		line := strings.TrimSpace(rawLine)
		switch {
		case line == managedMarkerLine:
			hasManagedMarker = true
		case strings.HasPrefix(line, uidMarkerPrefix):
			uidValue := strings.TrimSpace(strings.TrimPrefix(line, uidMarkerPrefix))
			hasUIDMarker = uidValue == targetUIDString
		case strings.HasPrefix(line, jobIDMarkerPrefix):
			jobIDValue := strings.TrimSpace(strings.TrimPrefix(line, jobIDMarkerPrefix))
			hasJobIDMarker = jobIDValue != ""
		}
	}

	return hasManagedMarker && hasUIDMarker && hasJobIDMarker
}

func RenderJobUnits(targetUID uint32, job config.Job) (RenderedUnits, error) {
	if strings.TrimSpace(job.ID) == "" {
		return RenderedUnits{}, fmt.Errorf("job id is required")
	}

	baseName := buildUnitBaseName(targetUID, job.ID)
	serviceName := baseName + ".service"
	timerName := baseName + ".timer"

	timerDirectives, err := config.CompileTimerDirectives(job.When)
	if err != nil {
		return RenderedUnits{}, err
	}

	units := RenderedUnits{
		BaseName:       baseName,
		ServiceName:    serviceName,
		TimerName:      timerName,
		ServiceContent: renderServiceContent(targetUID, job, serviceName),
		TimerContent:   renderTimerContent(targetUID, job, serviceName, timerDirectives),
	}

	return units, nil
}

func buildUnitBaseName(targetUID uint32, jobID string) string {
	component := sanitizeUnitComponent(jobID)
	return fmt.Sprintf("timertab-u%d-%s-%s", targetUID, component, shortStableHash(jobID))
}

func renderServiceContent(targetUID uint32, job config.Job, serviceName string) string {
	var b strings.Builder

	writeManagedMarkers(&b, targetUID, job.ID)
	b.WriteString("[Unit]\n")
	b.WriteString("Description=")
	b.WriteString(systemdQuoted("timertab job " + job.ID))
	b.WriteString("\n\n")

	b.WriteString("[Service]\n")
	b.WriteString("Type=oneshot\n")

	if job.Cwd != "" {
		b.WriteString("WorkingDirectory=")
		b.WriteString(systemdQuoted(job.Cwd))
		b.WriteString("\n")
	}

	appendEnvironmentLines(&b, job.Env)
	appendServiceLimits(&b, job.Limits)
	appendRawDirectives(&b, job.Systemd, false)

	b.WriteString("ExecStart=/bin/sh -lc ")
	b.WriteString(systemdQuoted(job.Run))
	b.WriteString("\n")

	b.WriteString("ExecStopPost=/bin/sh -lc ")
	b.WriteString(systemdQuoted(hookDispatchScript(serviceName, job)))
	b.WriteString("\n")

	return b.String()
}

func renderTimerContent(targetUID uint32, job config.Job, serviceName string, timerDirectives []string) string {
	var b strings.Builder

	writeManagedMarkers(&b, targetUID, job.ID)
	b.WriteString("[Unit]\n")
	b.WriteString("Description=")
	b.WriteString(systemdQuoted("timertab timer " + job.ID))
	b.WriteString("\n\n")

	b.WriteString("[Timer]\n")
	b.WriteString("Unit=")
	b.WriteString(serviceName)
	b.WriteString("\n")
	if job.Persistent != nil && *job.Persistent {
		b.WriteString("Persistent=true\n")
	}
	if strings.TrimSpace(job.Jitter) != "" {
		b.WriteString("RandomizedDelaySec=")
		b.WriteString(strings.TrimSpace(job.Jitter))
		b.WriteString("\n")
	}
	appendRawDirectives(&b, job.Systemd, true)
	for _, directive := range timerDirectives {
		b.WriteString(directive)
		b.WriteString("\n")
	}
	b.WriteString("\n")

	b.WriteString("[Install]\n")
	b.WriteString("WantedBy=timers.target\n")

	return b.String()
}

func writeManagedMarkers(b *strings.Builder, targetUID uint32, jobID string) {
	b.WriteString(managedMarkerLine)
	b.WriteString("\n")
	b.WriteString(uidMarkerPrefix)
	b.WriteString(fmt.Sprintf("%d", targetUID))
	b.WriteString("\n")
	b.WriteString(jobIDMarkerPrefix)
	b.WriteString(jobID)
	b.WriteString("\n")
}

func appendEnvironmentLines(b *strings.Builder, env map[string]string) {
	keys := sortedKeys(env)
	for _, key := range keys {
		b.WriteString("Environment=")
		b.WriteString(systemdQuoted(key + "=" + env[key]))
		b.WriteString("\n")
	}
}

func appendServiceLimits(b *strings.Builder, limits *config.Limits) {
	if limits == nil {
		return
	}

	if trimmed := strings.TrimSpace(limits.MemoryMax); trimmed != "" {
		b.WriteString("MemoryMax=")
		b.WriteString(trimmed)
		b.WriteString("\n")
	}
	if trimmed := strings.TrimSpace(limits.CPUQuota); trimmed != "" {
		b.WriteString("CPUQuota=")
		b.WriteString(trimmed)
		b.WriteString("\n")
	}
	if limits.IOWeight != nil {
		b.WriteString("IOWeight=")
		b.WriteString(fmt.Sprintf("%d", *limits.IOWeight))
		b.WriteString("\n")
	}
}

func appendRawDirectives(b *strings.Builder, overrides *config.Systemd, timer bool) {
	if overrides == nil {
		return
	}

	var directives []config.SystemdDirective
	if timer {
		directives = overrides.Timer.Directives()
	} else {
		directives = overrides.Service.Directives()
	}
	if len(directives) == 0 {
		return
	}

	for _, directive := range directives {
		b.WriteString(directive.Name)
		b.WriteString("=")
		b.WriteString(directive.Value)
		b.WriteString("\n")
	}
}

func sortedKeys(values map[string]string) []string {
	if len(values) == 0 {
		return nil
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func hookDispatchScript(serviceName string, job config.Job) string {
	successCommand := hookCommand(job.OnSuccess)
	failureCommand := hookCommand(job.OnFailure)

	parts := []string{
		"TIMERTAB_JOB_ID=" + shellQuoted(job.ID),
		"TIMERTAB_UNIT=" + shellQuoted(serviceName),
		"export TIMERTAB_JOB_ID TIMERTAB_UNIT SERVICE_RESULT EXIT_CODE EXIT_STATUS",
		`if [ "${SERVICE_RESULT:-}" = "success" ]; then ` + successCommand + "; fi",
		`if [ "${SERVICE_RESULT:-}" != "success" ]; then ` + failureCommand + "; fi",
	}

	return strings.Join(parts, "; ")
}

func hookCommand(hook *config.Hook) string {
	if hook == nil {
		return ":"
	}
	if strings.TrimSpace(hook.Command) == "" {
		return ":"
	}

	prefix := hookEnvPrefix(hook.Env)
	if prefix != "" {
		prefix += " "
	}

	return prefix + "/bin/sh -lc " + shellQuoted(hook.Command)
}

func hookEnvPrefix(env map[string]string) string {
	keys := sortedKeys(env)
	if len(keys) == 0 {
		return ""
	}

	assignments := make([]string, 0, len(keys))
	for _, key := range keys {
		assignments = append(assignments, key+"="+shellQuoted(env[key]))
	}

	return strings.Join(assignments, " ")
}

func shortStableHash(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])[:10]
}

func sanitizeUnitComponent(value string) string {
	trimmed := strings.TrimSpace(strings.ToLower(value))
	if trimmed == "" {
		return "job"
	}

	var b strings.Builder
	b.Grow(len(trimmed))
	lastDash := false
	for _, r := range trimmed {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
			lastDash = false
		case r >= '0' && r <= '9':
			b.WriteRune(r)
			lastDash = false
		case r == '.' || r == '_' || r == '-':
			b.WriteRune(r)
			lastDash = false
		case unicode.IsSpace(r):
			if !lastDash {
				b.WriteByte('-')
				lastDash = true
			}
		default:
			if !lastDash {
				b.WriteByte('-')
				lastDash = true
			}
		}
	}

	out := strings.Trim(b.String(), "-")
	if out == "" {
		return "job"
	}

	return out
}

func shellQuoted(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'"'"'`) + "'"
}

func systemdQuoted(value string) string {
	replacer := strings.NewReplacer(
		`\`, `\\`,
		`"`, `\"`,
		"\n", `\n`,
		"\r", `\r`,
		"\t", `\t`,
	)
	return `"` + replacer.Replace(value) + `"`
}
