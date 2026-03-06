package config

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"golang.org/x/text/language"
	"golang.org/x/text/message"

	"gopkg.in/yaml.v3"
)

var (
	validID           = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)
	validEnv          = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
	cronToken         = regexp.MustCompile(`^\S+$`)
	compiledSchema    *jsonschema.Schema
	compiledSchemaMu  sync.Once
	compiledSchemaErr error
)

var allowedShorthand = map[string]struct{}{
	"@hourly":   {},
	"@daily":    {},
	"@weekly":   {},
	"@monthly":  {},
	"@yearly":   {},
	"@annually": {},
	"@reboot":   {},
}

type SchemaViolation struct {
	Path    string
	Message string
}

type SchemaValidationError struct {
	Violations []SchemaViolation
}

func (e *SchemaValidationError) Error() string {
	if len(e.Violations) == 0 {
		return "schema validation failed"
	}
	if len(e.Violations) == 1 {
		v := e.Violations[0]
		return fmt.Sprintf("schema validation failed at %s: %s", v.Path, v.Message)
	}

	parts := make([]string, 0, len(e.Violations))
	for _, violation := range e.Violations {
		parts = append(parts, fmt.Sprintf("%s: %s", violation.Path, violation.Message))
	}
	return fmt.Sprintf("schema validation failed: %s", strings.Join(parts, "; "))
}

func validateConfigSchema(raw any) error {
	doc, err := toJSONValue(raw)
	if err != nil {
		return fmt.Errorf("schema validation failed: %w", err)
	}

	schema, err := loadCompiledSchema()
	if err != nil {
		return err
	}

	if err := schema.Validate(doc); err != nil {
		var validationErr *jsonschema.ValidationError
		if errors.As(err, &validationErr) {
			return asSchemaValidationError(validationErr)
		}
		return fmt.Errorf("schema validation failed: %w", err)
	}
	return nil
}

func loadCompiledSchema() (*jsonschema.Schema, error) {
	compiledSchemaMu.Do(func() {
		schemaPath, err := resolveSchemaPath()
		if err != nil {
			compiledSchemaErr = fmt.Errorf("load schema/v1.json: %w", err)
			return
		}

		compiler := jsonschema.NewCompiler()
		compiledSchema, compiledSchemaErr = compiler.Compile(schemaPath)
		if compiledSchemaErr != nil {
			compiledSchemaErr = fmt.Errorf("compile schema/v1.json: %w", compiledSchemaErr)
		}
	})
	if compiledSchemaErr != nil {
		return nil, compiledSchemaErr
	}
	return compiledSchema, nil
}

func resolveSchemaPath() (string, error) {
	candidates := make([]string, 0, 2)
	candidates = append(candidates, filepath.Join("schema", "v1.json"))
	if _, sourceFile, _, ok := runtime.Caller(0); ok {
		candidates = append(candidates, filepath.Clean(filepath.Join(filepath.Dir(sourceFile), "..", "..", "schema", "v1.json")))
	}

	checked := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		absPath := candidate
		if !filepath.IsAbs(absPath) {
			var err error
			absPath, err = filepath.Abs(candidate)
			if err != nil {
				continue
			}
		}
		checked = append(checked, absPath)

		info, err := os.Stat(absPath)
		if err == nil && !info.IsDir() {
			return absPath, nil
		}
	}
	return "", fmt.Errorf("file not found; looked in %s", strings.Join(checked, ", "))
}

func toJSONValue(value any) (any, error) {
	switch typed := value.(type) {
	case nil, bool, string,
		int, int8, int16, int32, int64,
		uint, uint8, uint16, uint32, uint64,
		float32, float64:
		return typed, nil
	case map[string]any:
		out := make(map[string]any, len(typed))
		for key, child := range typed {
			converted, err := toJSONValue(child)
			if err != nil {
				return nil, err
			}
			out[key] = converted
		}
		return out, nil
	case map[any]any:
		out := make(map[string]any, len(typed))
		for key, child := range typed {
			keyString, ok := key.(string)
			if !ok {
				return nil, fmt.Errorf("non-string object key %T", key)
			}
			converted, err := toJSONValue(child)
			if err != nil {
				return nil, err
			}
			out[keyString] = converted
		}
		return out, nil
	case []any:
		out := make([]any, len(typed))
		for idx, item := range typed {
			converted, err := toJSONValue(item)
			if err != nil {
				return nil, err
			}
			out[idx] = converted
		}
		return out, nil
	default:
		return typed, nil
	}
}

func asSchemaValidationError(err *jsonschema.ValidationError) error {
	printer := message.NewPrinter(language.English)
	violations := make([]SchemaViolation, 0, 8)
	collectSchemaViolations(err, nil, printer, &violations)

	if len(violations) == 0 {
		violations = append(violations, SchemaViolation{
			Path:    "$",
			Message: err.Error(),
		})
	}

	sort.SliceStable(violations, func(i, j int) bool {
		if violations[i].Path == violations[j].Path {
			return violations[i].Message < violations[j].Message
		}
		return violations[i].Path < violations[j].Path
	})

	unique := make([]SchemaViolation, 0, len(violations))
	seen := make(map[string]struct{}, len(violations))
	for _, violation := range violations {
		key := violation.Path + "\x00" + violation.Message
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		unique = append(unique, violation)
	}

	return &SchemaValidationError{Violations: unique}
}

func collectSchemaViolations(
	err *jsonschema.ValidationError,
	inheritedPath []string,
	printer *message.Printer,
	violations *[]SchemaViolation,
) {
	currentPath := inheritedPath
	if len(err.InstanceLocation) > 0 {
		currentPath = err.InstanceLocation
	}

	if len(err.Causes) == 0 {
		message := err.ErrorKind.LocalizedString(printer)
		if message == "" {
			message = err.Error()
		}
		*violations = append(*violations, SchemaViolation{
			Path:    jsonPathFromTokens(currentPath),
			Message: message,
		})
		return
	}

	for _, cause := range err.Causes {
		collectSchemaViolations(cause, currentPath, printer, violations)
	}
}

func jsonPathFromTokens(tokens []string) string {
	if len(tokens) == 0 {
		return "$"
	}

	var b strings.Builder
	b.WriteByte('$')
	for _, token := range tokens {
		if idx, err := strconv.Atoi(token); err == nil {
			b.WriteString("[")
			b.WriteString(strconv.Itoa(idx))
			b.WriteString("]")
			continue
		}
		if isPathIdentifier(token) {
			b.WriteByte('.')
			b.WriteString(token)
			continue
		}
		b.WriteString("['")
		b.WriteString(strings.ReplaceAll(token, "'", "\\'"))
		b.WriteString("']")
	}
	return b.String()
}

func isPathIdentifier(value string) bool {
	if value == "" {
		return false
	}
	for idx, r := range value {
		if idx == 0 {
			if !(r == '_' || unicode.IsLetter(r)) {
				return false
			}
			continue
		}
		if !(r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r)) {
			return false
		}
	}
	return true
}

func (f *File) Validate() error {
	if f.Version != 1 {
		return fmt.Errorf("version must be 1")
	}
	if f.Jobs == nil {
		return fmt.Errorf("jobs is required")
	}

	seen := make(map[string]struct{}, len(f.Jobs))
	for idx := range f.Jobs {
		job := &f.Jobs[idx]
		if err := validateJob(*job); err != nil {
			return fmt.Errorf("jobs[%d]: %w", idx, err)
		}

		if job.ID != "" {
			if _, exists := seen[job.ID]; exists {
				return fmt.Errorf("jobs[%d]: duplicate id %q", idx, job.ID)
			}
			seen[job.ID] = struct{}{}
		}
	}

	return nil
}

func (f *File) NormalizeIDs() error {
	if err := f.Validate(); err != nil {
		return err
	}

	seen := make(map[string]struct{}, len(f.Jobs))
	for idx := range f.Jobs {
		job := &f.Jobs[idx]
		if job.ID == "" {
			job.ID = nextGeneratedID(*job, seen)
		}
		if _, exists := seen[job.ID]; exists {
			return fmt.Errorf("jobs[%d]: duplicate generated id %q", idx, job.ID)
		}
		seen[job.ID] = struct{}{}
	}

	return nil
}

func validateJob(job Job) error {
	if job.ID != "" && !validID.MatchString(job.ID) {
		return fmt.Errorf("id must match %s", validID.String())
	}
	if strings.TrimSpace(job.Run) == "" {
		return fmt.Errorf("run is required")
	}
	if len(job.When) == 0 {
		return fmt.Errorf("when is required")
	}
	for _, schedule := range job.When {
		if err := validateSchedule(schedule); err != nil {
			return err
		}
	}
	if err := validateEnv(job.Env); err != nil {
		return fmt.Errorf("env: %w", err)
	}
	if err := validateJitter(job.Jitter); err != nil {
		return fmt.Errorf("jitter: %w", err)
	}
	if job.OnSuccess != nil {
		if err := validateHook(*job.OnSuccess); err != nil {
			return fmt.Errorf("on_success: %w", err)
		}
	}
	if job.OnFailure != nil {
		if err := validateHook(*job.OnFailure); err != nil {
			return fmt.Errorf("on_failure: %w", err)
		}
	}
	return nil
}

func validateJitter(value string) error {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}

	duration, err := time.ParseDuration(trimmed)
	if err != nil {
		return fmt.Errorf("must be a valid duration such as \"5m\": %w", err)
	}
	if duration <= 0 {
		return fmt.Errorf("must be greater than 0")
	}

	return nil
}

func validateHook(hook Hook) error {
	if strings.TrimSpace(hook.Command) == "" {
		return fmt.Errorf("command is required")
	}
	if err := validateEnv(hook.Env); err != nil {
		return err
	}
	return nil
}

func validateEnv(values map[string]string) error {
	for key := range values {
		if !validEnv.MatchString(key) {
			return fmt.Errorf("invalid key %q", key)
		}
	}
	return nil
}

func validateSchedule(v string) error {
	value := strings.TrimSpace(v)
	if value == "" {
		return fmt.Errorf("when item cannot be empty")
	}
	if strings.HasPrefix(value, "@") {
		if _, ok := allowedShorthand[value]; !ok {
			return fmt.Errorf("unsupported shorthand %q", value)
		}
		return nil
	}

	parts := strings.Fields(value)
	if len(parts) != 5 {
		return fmt.Errorf("cron expression must have exactly 5 fields")
	}
	for _, part := range parts {
		if !cronToken.MatchString(part) {
			return fmt.Errorf("invalid cron token %q", part)
		}
	}

	return nil
}

func nextGeneratedID(job Job, seen map[string]struct{}) string {
	if slug := slugify(job.Name); slug != "" {
		if _, exists := seen[slug]; !exists {
			return slug
		}
		for n := 2; n < 10000; n++ {
			candidate := fmt.Sprintf("%s-%d", slug, n)
			if _, exists := seen[candidate]; !exists {
				return candidate
			}
		}
	}

	candidate := "job-" + jobDigest(job)
	if _, exists := seen[candidate]; !exists {
		return candidate
	}

	for n := 2; n < 10000; n++ {
		withSuffix := fmt.Sprintf("%s-%d", candidate, n)
		if _, exists := seen[withSuffix]; !exists {
			return withSuffix
		}
	}

	return candidate
}

func slugify(input string) string {
	if strings.TrimSpace(input) == "" {
		return ""
	}
	var b strings.Builder
	lastDash := false
	for _, r := range strings.ToLower(input) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			lastDash = false
		case r == '-', r == '_', r == '.', r == ' ':
			if !lastDash && b.Len() > 0 {
				b.WriteByte('-')
				lastDash = true
			}
		}
	}

	out := strings.Trim(b.String(), "-")
	if out == "" {
		return ""
	}
	if len(out) > 64 {
		out = strings.TrimRight(out[:64], "-")
	}
	if !validID.MatchString(out) {
		return ""
	}
	return out
}

func jobDigest(job Job) string {
	copyJob := job
	copyJob.ID = ""

	if len(copyJob.When) > 1 {
		sorted := append([]string(nil), copyJob.When...)
		sort.Strings(sorted)
		copyJob.When = sorted
	}

	data, err := yaml.Marshal(copyJob)
	if err != nil {
		return "000000000000"
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])[:12]
}
