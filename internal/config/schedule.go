package config

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

type ScheduleList []string

var shorthandOnCalendar = map[string]string{
	"@hourly":   "*-*-* *:00:00",
	"@daily":    "*-*-* 00:00:00",
	"@weekly":   "Mon *-*-* 00:00:00",
	"@monthly":  "*-*-01 00:00:00",
	"@yearly":   "*-01-01 00:00:00",
	"@annually": "*-01-01 00:00:00",
}

var cronMonthNames = map[string]int{
	"jan":       1,
	"january":   1,
	"feb":       2,
	"february":  2,
	"mar":       3,
	"march":     3,
	"apr":       4,
	"april":     4,
	"may":       5,
	"jun":       6,
	"june":      6,
	"jul":       7,
	"july":      7,
	"aug":       8,
	"august":    8,
	"sep":       9,
	"sept":      9,
	"september": 9,
	"oct":       10,
	"october":   10,
	"nov":       11,
	"november":  11,
	"dec":       12,
	"december":  12,
}

var cronWeekdayNames = map[string]int{
	"sun":       0,
	"sunday":    0,
	"mon":       1,
	"monday":    1,
	"tue":       2,
	"tues":      2,
	"tuesday":   2,
	"wed":       3,
	"wednesday": 3,
	"thu":       4,
	"thur":      4,
	"thurs":     4,
	"thursday":  4,
	"fri":       5,
	"friday":    5,
	"sat":       6,
	"saturday":  6,
}

var systemdWeekdayNames = []string{"Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"}

type cronFieldOptions struct {
	min   int
	max   int
	names map[string]int
}

func (s *ScheduleList) UnmarshalYAML(node *yaml.Node) error {
	switch node.Kind {
	case yaml.ScalarNode:
		if node.Value == "" {
			return fmt.Errorf("when cannot be empty")
		}
		*s = []string{node.Value}
		return nil
	case yaml.SequenceNode:
		if len(node.Content) == 0 {
			return fmt.Errorf("when list cannot be empty")
		}
		out := make([]string, 0, len(node.Content))
		for _, item := range node.Content {
			if item.Kind != yaml.ScalarNode || item.Value == "" {
				return fmt.Errorf("when list items must be non-empty strings")
			}
			out = append(out, item.Value)
		}
		*s = out
		return nil
	default:
		return fmt.Errorf("when must be a string or list of strings")
	}
}

func (s ScheduleList) MarshalYAML() (any, error) {
	if len(s) == 1 {
		return s[0], nil
	}
	return []string(s), nil
}

func CompileTimerDirectives(when ScheduleList) ([]string, error) {
	return CompileTimerDirectivesInLocation(when, "")
}

func CompileTimerDirectivesInLocation(when ScheduleList, tz string) ([]string, error) {
	if len(when) == 0 {
		return nil, fmt.Errorf("when is required")
	}

	out := make([]string, 0, len(when))
	for idx, schedule := range when {
		directives, err := compileScheduleToTimerDirectives(schedule)
		if err != nil {
			return nil, fmt.Errorf("when[%d] %q: %w", idx, schedule, err)
		}
		out = append(out, appendCalendarTimezone(directives, tz)...)
	}

	return out, nil
}

func appendCalendarTimezone(directives []string, tz string) []string {
	trimmedTZ := strings.TrimSpace(tz)
	if trimmedTZ == "" {
		return directives
	}

	out := make([]string, len(directives))
	for idx, directive := range directives {
		if strings.HasPrefix(directive, "OnCalendar=") {
			out[idx] = directive + " " + trimmedTZ
			continue
		}
		out[idx] = directive
	}
	return out
}

func compileScheduleToTimerDirectives(value string) ([]string, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil, fmt.Errorf("when item cannot be empty")
	}

	if strings.HasPrefix(trimmed, "@") {
		if trimmed == "@reboot" {
			return []string{"OnBootSec=0"}, nil
		}

		calendar, ok := shorthandOnCalendar[trimmed]
		if !ok {
			return nil, fmt.Errorf("unsupported shorthand %q", trimmed)
		}
		return []string{"OnCalendar=" + calendar}, nil
	}

	calendars, err := compileCronExpression(trimmed)
	if err != nil {
		return nil, err
	}

	directives := make([]string, 0, len(calendars))
	for _, calendar := range calendars {
		directives = append(directives, "OnCalendar="+calendar)
	}

	return directives, nil
}

func compileCronExpression(value string) ([]string, error) {
	parts := strings.Fields(value)
	if len(parts) != 5 {
		return nil, fmt.Errorf("cron expression must have exactly 5 fields")
	}

	minuteValues, err := parseCronField(parts[0], cronFieldOptions{min: 0, max: 59})
	if err != nil {
		return nil, fmt.Errorf("invalid cron minute field %q: %w", parts[0], err)
	}
	hourValues, err := parseCronField(parts[1], cronFieldOptions{min: 0, max: 23})
	if err != nil {
		return nil, fmt.Errorf("invalid cron hour field %q: %w", parts[1], err)
	}
	dayOfMonthValues, err := parseCronField(parts[2], cronFieldOptions{min: 1, max: 31})
	if err != nil {
		return nil, fmt.Errorf("invalid cron day-of-month field %q: %w", parts[2], err)
	}
	monthValues, err := parseCronField(parts[3], cronFieldOptions{min: 1, max: 12, names: cronMonthNames})
	if err != nil {
		return nil, fmt.Errorf("invalid cron month field %q: %w", parts[3], err)
	}
	weekdayValues, err := parseCronWeekdayField(parts[4])
	if err != nil {
		return nil, fmt.Errorf("invalid cron weekday field %q: %w", parts[4], err)
	}

	minuteSpec := formatNumericField(minuteValues, 0, 59, 2)
	hourSpec := formatNumericField(hourValues, 0, 23, 2)
	dayOfMonthSpec := formatNumericField(dayOfMonthValues, 1, 31, 2)
	monthSpec := formatNumericField(monthValues, 1, 12, 2)
	weekdaySpec := formatWeekdayField(weekdayValues)
	timeSpec := fmt.Sprintf("%s:%s:00", hourSpec, minuteSpec)

	dayOfMonthWildcard := cronDayFieldStartsWithStar(parts[2])
	weekdayWildcard := cronDayFieldStartsWithStar(parts[4])

	domCalendar := fmt.Sprintf("*-%s-%s %s", monthSpec, dayOfMonthSpec, timeSpec)
	dowCalendar := fmt.Sprintf("%s *-%s-* %s", weekdaySpec, monthSpec, timeSpec)
	combinedDayCalendar := fmt.Sprintf("%s *-%s-%s %s", weekdaySpec, monthSpec, dayOfMonthSpec, timeSpec)
	anyDayCalendar := fmt.Sprintf("*-%s-* %s", monthSpec, timeSpec)

	switch {
	case dayOfMonthWildcard && weekdayWildcard:
		return []string{anyDayCalendar}, nil
	case dayOfMonthWildcard && len(dayOfMonthValues) == 31:
		return []string{dowCalendar}, nil
	case weekdayWildcard && len(weekdayValues) == 7:
		return []string{domCalendar}, nil
	case dayOfMonthWildcard || weekdayWildcard:
		return []string{combinedDayCalendar}, nil
	default:
		// Cron semantics for day-of-month and weekday are OR, not AND.
		return []string{domCalendar, dowCalendar}, nil
	}
}

func cronDayFieldStartsWithStar(field string) bool {
	return strings.HasPrefix(strings.TrimSpace(field), "*")
}

func parseCronField(value string, opts cronFieldOptions) ([]int, error) {
	parts := strings.Split(value, ",")
	set := make(map[int]struct{}, opts.max-opts.min+1)

	for _, part := range parts {
		expanded, err := expandCronPart(part, opts.min, opts.max, func(token string) (int, error) {
			return parseCronToken(token, opts.min, opts.max, opts.names)
		})
		if err != nil {
			return nil, err
		}
		for _, v := range expanded {
			set[v] = struct{}{}
		}
	}

	return sortedSet(set), nil
}

func parseCronWeekdayField(value string) ([]int, error) {
	parts := strings.Split(value, ",")
	set := make(map[int]struct{}, 7)

	for _, part := range parts {
		expanded, err := expandCronPart(part, 0, 7, parseCronWeekdayToken)
		if err != nil {
			return nil, err
		}
		for _, v := range expanded {
			if v == 7 {
				v = 0
			}
			set[v] = struct{}{}
		}
	}

	return sortedSet(set), nil
}

func expandCronPart(part string, min, max int, resolveToken func(string) (int, error)) ([]int, error) {
	if part == "" {
		return nil, fmt.Errorf("empty token")
	}

	base := part
	step := 1
	if strings.Contains(part, "/") {
		if strings.Count(part, "/") != 1 {
			return nil, fmt.Errorf("invalid step token %q", part)
		}
		stepParts := strings.SplitN(part, "/", 2)
		base = stepParts[0]
		v, err := strconv.Atoi(stepParts[1])
		if err != nil || v <= 0 {
			return nil, fmt.Errorf("invalid step %q", stepParts[1])
		}
		step = v
	}

	start := min
	end := max

	switch {
	case base == "*":
	case strings.Contains(base, "-"):
		if strings.Count(base, "-") != 1 {
			return nil, fmt.Errorf("invalid range token %q", base)
		}
		rangeParts := strings.SplitN(base, "-", 2)
		rangeStart, err := resolveToken(rangeParts[0])
		if err != nil {
			return nil, err
		}
		rangeEnd, err := resolveToken(rangeParts[1])
		if err != nil {
			return nil, err
		}
		if rangeStart > rangeEnd {
			return nil, fmt.Errorf("range start must be <= end")
		}
		start = rangeStart
		end = rangeEnd
	default:
		value, err := resolveToken(base)
		if err != nil {
			return nil, err
		}
		start = value
		if step == 1 {
			end = value
		}
	}

	out := make([]int, 0, end-start+1)
	for v := start; v <= end; v += step {
		out = append(out, v)
	}

	return out, nil
}

func parseCronToken(token string, min, max int, names map[string]int) (int, error) {
	trimmed := strings.TrimSpace(token)
	if trimmed == "" {
		return 0, fmt.Errorf("empty token")
	}
	if names != nil {
		if v, ok := names[strings.ToLower(trimmed)]; ok {
			return v, nil
		}
	}
	v, err := strconv.Atoi(trimmed)
	if err != nil {
		return 0, fmt.Errorf("invalid value %q", token)
	}
	if v < min || v > max {
		return 0, fmt.Errorf("value %d out of bounds %d..%d", v, min, max)
	}
	return v, nil
}

func parseCronWeekdayToken(token string) (int, error) {
	trimmed := strings.TrimSpace(token)
	if trimmed == "" {
		return 0, fmt.Errorf("empty token")
	}
	if v, ok := cronWeekdayNames[strings.ToLower(trimmed)]; ok {
		return v, nil
	}

	v, err := strconv.Atoi(trimmed)
	if err != nil {
		return 0, fmt.Errorf("invalid value %q", token)
	}
	if v < 0 || v > 7 {
		return 0, fmt.Errorf("value %d out of bounds 0..7", v)
	}
	return v, nil
}

func sortedSet(values map[int]struct{}) []int {
	out := make([]int, 0, len(values))
	for v := range values {
		out = append(out, v)
	}
	sort.Ints(out)
	return out
}

func formatNumericField(values []int, min, max, width int) string {
	if len(values) == max-min+1 {
		return "*"
	}
	formatted := make([]string, 0, len(values))
	for _, v := range values {
		formatted = append(formatted, fmt.Sprintf("%0*d", width, v))
	}
	return strings.Join(formatted, ",")
}

func formatWeekdayField(values []int) string {
	if len(values) == 7 {
		return "*"
	}
	formatted := make([]string, 0, len(values))
	for _, v := range values {
		formatted = append(formatted, systemdWeekdayNames[v])
	}
	return strings.Join(formatted, ",")
}
