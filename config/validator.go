// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package config

import (
	"fmt"
	"net/url"
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/netresearch/go-cron"
)

// ValidationError represents a configuration validation error
type ValidationError struct {
	Field   string
	Value   any
	Message string
}

func (e ValidationError) Error() string {
	return fmt.Sprintf("config validation error for field '%s': %s (value: %v)",
		e.Field, e.Message, e.Value)
}

// ValidationErrors represents multiple validation errors
type ValidationErrors []ValidationError

func (e ValidationErrors) Error() string {
	messages := make([]string, 0, len(e))
	for _, err := range e {
		messages = append(messages, err.Error())
	}
	return strings.Join(messages, "; ")
}

// Validator provides configuration validation
type Validator struct {
	errors ValidationErrors
}

// NewValidator creates a new configuration validator
func NewValidator() *Validator {
	return &Validator{
		errors: make(ValidationErrors, 0),
	}
}

// AddError adds a validation error
func (v *Validator) AddError(field string, value any, message string) {
	v.errors = append(v.errors, ValidationError{
		Field:   field,
		Value:   value,
		Message: message,
	})
}

// HasErrors returns true if there are validation errors
func (v *Validator) HasErrors() bool {
	return len(v.errors) > 0
}

// Errors returns all validation errors
func (v *Validator) Errors() ValidationErrors {
	return v.errors
}

// ValidateRequired validates that a field is not empty
func (v *Validator) ValidateRequired(field, value string) {
	if strings.TrimSpace(value) == "" {
		v.AddError(field, value, "is required")
	}
}

// ValidateMinLength validates minimum string length
func (v *Validator) ValidateMinLength(field string, value string, minLength int) {
	if len(value) < minLength {
		v.AddError(field, value, fmt.Sprintf("must be at least %d characters", minLength))
	}
}

// ValidateMaxLength validates maximum string length
func (v *Validator) ValidateMaxLength(field string, value string, maxLength int) {
	if len(value) > maxLength {
		v.AddError(field, value, fmt.Sprintf("must be at most %d characters", maxLength))
	}
}

// ValidateRange validates that a number is within range
func (v *Validator) ValidateRange(field string, value int, minVal, maxVal int) {
	if value < minVal || value > maxVal {
		v.AddError(field, value, fmt.Sprintf("must be between %d and %d", minVal, maxVal))
	}
}

// ValidatePositive validates that a number is positive
func (v *Validator) ValidatePositive(field string, value int) {
	if value <= 0 {
		v.AddError(field, value, "must be positive")
	}
}

// ValidateURL validates that a string is a valid URL
func (v *Validator) ValidateURL(field, value string) {
	if value == "" {
		return
	}

	u, err := url.Parse(value)
	if err != nil || u.Scheme == "" || u.Host == "" {
		v.AddError(field, value, "must be a valid URL")
	}
}

// ValidateEmail validates that a string is a valid email
func (v *Validator) ValidateEmail(field, value string) {
	if value == "" {
		return
	}

	emailRegex := regexp.MustCompile(`^[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}$`)
	if !emailRegex.MatchString(value) {
		v.AddError(field, value, "must be a valid email address")
	}
}

// ValidateCronExpression validates a cron expression using go-cron's parser.
// This handles all formats: descriptors (@daily), @every intervals, standard
// cron expressions with optional seconds, month/day names, and wraparound ranges.
func (v *Validator) ValidateCronExpression(field, value string) {
	if value == "" {
		return
	}

	// Allow ofelia's triggered-only schedule keywords. These are duplicated
	// in core/schedule_keywords.go but config can't depend on core (cycle).
	if value == scheduleTriggered || value == scheduleManual || value == scheduleNone {
		return
	}

	parseOpts := cron.SecondOptional | cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor
	if err := cron.ValidateSpec(value, parseOpts); err != nil {
		v.AddError(field, value, fmt.Sprintf("invalid cron expression: %v", err))
	}
}

// ValidateEnum validates that a value is in a list of allowed values
func (v *Validator) ValidateEnum(field string, value string, allowed []string) {
	if value == "" {
		return
	}

	if slices.Contains(allowed, value) {
		return
	}

	v.AddError(field, value, fmt.Sprintf("must be one of: %s", strings.Join(allowed, ", ")))
}

// ValidatePath validates that a path exists or can be created
func (v *Validator) ValidatePath(field, value string) {
	if value == "" {
		return
	}

	// Basic path validation - just check for invalid characters
	if strings.ContainsAny(value, "\x00") {
		v.AddError(field, value, "contains invalid characters")
	}
}

// Validator2 validates a whole configuration object by reflection, rather
// than field by field. Validate walks the exported fields of the struct it
// was given (recursing into nested structs), derives each field's config key
// from its gcfg/mapstructure tag, and reports the collected problems through
// a freshly built [Validator]. Only string, int/int64 and slice fields are
// inspected; other kinds are skipped. Construct with NewConfigValidator.
type Validator2 struct {
	config    any
	sanitizer *Sanitizer
}

// NewConfigValidator creates a configuration validator
func NewConfigValidator(config any) *Validator2 {
	return &Validator2{
		config:    config,
		sanitizer: NewSanitizer(),
	}
}

// Validate performs validation on the configuration
func (cv *Validator2) Validate() error {
	v := NewValidator()

	// Validate the configuration using reflection to check struct tags and values
	cv.validateStruct(v, cv.config, "")

	if v.HasErrors() {
		return v.Errors()
	}

	return nil
}

// validateStruct recursively validates struct fields based on tags
func (cv *Validator2) validateStruct(v *Validator, obj any, path string) {
	cv.validateStructIn(v, obj, path, false)
}

// validateStructIn is validateStruct with the job flag, which travels down
// through nested and squashed structs so an embedded job field is still
// recognized as being inside a job.
func (cv *Validator2) validateStructIn(v *Validator, obj any, path string, inJob bool) {
	val, ok := derefToStruct(obj)
	if !ok {
		return
	}

	typ := val.Type()
	for fieldType := range typ.Fields() {
		// Skip unexported fields
		if !fieldType.IsExported() {
			continue
		}

		field := val.FieldByIndex(fieldType.Index)
		mapstructureTag := fieldType.Tag.Get("mapstructure")
		defaultTag := fieldType.Tag.Get("default")
		fieldPath := resolveFieldPath(path, fieldType.Name, fieldType.Tag.Get("gcfg"), mapstructureTag)

		// Handle nested structs
		if field.Kind() == reflect.Struct {
			squashed := mapstructureTag == ",squash"
			if !squashed {
				cv.validateStructIn(v, field.Interface(), fieldPath, inJob)
				continue
			}
			// A squashed struct contributes its fields at the parent's level,
			// so it is walked with the parent's path. Only inside a job:
			// every job type embeds core.<Kind>Job and, through it, BareJob,
			// which is where schedule and command live — skipping squashed
			// structs is why a job's schedule was never checked. Outside a job
			// the old behavior stands, so the global section is untouched by
			// this change.
			if inJob {
				cv.validateStructIn(v, field.Interface(), path, inJob)
			}
			continue
		}

		// Job sections are maps keyed by the name the user wrote; walk them so
		// a job's fields are validated at all. Skipping maps meant no job was
		// ever reachable by the validator.
		if field.Kind() == reflect.Map {
			cv.validateJobMap(v, field, fieldPath)
			continue
		}

		// Validate based on field type and value
		cv.validateField(v, field, fieldCtx{parent: val, path: fieldPath, defaultTag: defaultTag, inJob: inJob})
	}
}

// derefToStruct dereferences a pointer (if any) and returns the underlying
// struct Value. It returns (zero, false) if obj is nil, a nil pointer, or
// not a struct.
func derefToStruct(obj any) (reflect.Value, bool) {
	val := reflect.ValueOf(obj)
	if val.Kind() == reflect.Pointer {
		if val.IsNil() {
			return reflect.Value{}, false
		}
		val = val.Elem()
	}
	if val.Kind() != reflect.Struct {
		return reflect.Value{}, false
	}
	return val, true
}

// resolveFieldPath returns the canonical config key for a struct field.
// It prefers the gcfg tag, then mapstructure (unless it is a squash
// directive), and falls back to the Go field name. It also prepends
// the parent path when non-empty.
func resolveFieldPath(parentPath, fieldName, gcfgTag, mapstructureTag string) string {
	fieldPath := fieldName
	if parentPath != "" {
		fieldPath = parentPath + "." + fieldName
	}

	// Use gcfg or mapstructure tag as field name if available
	if gcfgTag != "" && gcfgTag != "-" {
		return gcfgTag
	}
	if mapstructureTag != "" && mapstructureTag != "-" && mapstructureTag != ",squash" {
		return mapstructureTag
	}
	return fieldPath
}

// fieldCtx carries what a field needs beyond its own value: the struct it
// belongs to (so a requirement can be conditional on a sibling), its config
// key, its default tag, and whether it sits inside a job section.
type fieldCtx struct {
	parent     reflect.Value
	path       string
	defaultTag string
	// inJob suppresses the "a field with no default is required" rule. That
	// rule is a heuristic tuned to the global section; applied to a job it
	// would demand nearly every key a job can carry. What a job genuinely
	// needs is stated in jobRequirements and checked separately.
	inJob bool
}

// validateField validates individual fields based on their type and tags
func (cv *Validator2) validateField(v *Validator, field reflect.Value, ctx fieldCtx) {
	switch field.Kind() {
	case reflect.String:
		cv.validateStringField(v, field, ctx)
	case reflect.Int, reflect.Int64:
		cv.validateIntField(v, field, ctx.path)
	case reflect.Slice:
		cv.validateSliceField(v, field, ctx.path)
	case reflect.Invalid, reflect.Bool, reflect.Int8, reflect.Int16, reflect.Int32,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr,
		reflect.Float32, reflect.Float64, reflect.Complex64, reflect.Complex128,
		reflect.Array, reflect.Chan, reflect.Func, reflect.Interface, reflect.Map,
		reflect.Pointer, reflect.Struct, reflect.UnsafePointer:
		// These types are not currently validated or are handled elsewhere (e.g., structs)
		// No validation needed for these field types in this context
	default:
		// Handle any future or unexpected types gracefully
		// No validation performed for unrecognized types
	}
}

// validateStringField validates string type fields
func (cv *Validator2) validateStringField(v *Validator, field reflect.Value, ctx fieldCtx) {
	str := field.String()

	// Skip validation for fields with defaults when they're empty
	if ctx.defaultTag != "" && str == "" {
		return
	}

	// Check for required fields
	if ctx.defaultTag == "" && str == "" && !ctx.inJob &&
		!cv.isOptionalField(ctx.path) && cv.gateIsOpen(ctx.parent, ctx.path) {
		v.ValidateRequired(ctx.path, str)
	}

	// Validate specific string fields
	if str != "" {
		cv.validateSpecificStringField(v, ctx.path, str)
	}
}

// fieldKey returns the config key a path ends in, dropping any section
// qualifier the path carries for reporting.
func fieldKey(path string) string {
	if i := strings.LastIndex(path, "."); i >= 0 {
		return strings.ToLower(path[i+1:])
	}
	return strings.ToLower(path)
}

// validateSpecificStringField validates specific string field formats
func (cv *Validator2) validateSpecificStringField(v *Validator, path string, str string) {
	// First perform general security validation
	if !cv.performSecurityValidation(v, path, str) {
		return // Stop validation if security check fails
	}

	// Match on the last segment of the path. Inside a job the path is
	// qualified with its section ("job-local \"backup\".schedule") so the error
	// says which job is at fault, while the key that selects the check is the
	// bare field name. Without this the schedule of a job was never checked:
	// the qualified path matched no case and fell through silently.
	//
	// The case strings are user-facing INI key names; Go struct tags
	// (gcfg:"…" / mapstructure:"…") on Config require them as literals there
	// too, so extracting constants only relocates the duplication.
	switch fieldKey(path) {
	case keySchedule, "cron":
		cv.validateCronField(v, path, str)
	case "email-to", "email-from": //nolint:goconst // see comment above switch
		cv.validateEmailField(v, path, str)
	case "web-address", "pprof-address":
		cv.validateAddressField(v, path, str)
	case "log-level": //nolint:goconst // see comment above switch
		cv.validateLogLevelField(v, path, str)
	case keyCommand, "cmd":
		cv.validateCommandField(v, path, str)
	case keyImage:
		cv.validateImageField(v, path, str)
	case "save-folder", "working_dir":
		cv.validatePathField(v, path, str)
	// NOTE: save-mode/save-folder-mode (like save-folder and the other keys in
	// this switch) only reach here when reflected from a non-squashed config.
	// validateStruct skips ",squash"-embedded structs, so for the production
	// Config — where SaveConfig is squash-embedded — these are not validated at
	// load time. The authoritative guard is the runtime resolver
	// (SaveConfig.GetSaveFileMode/GetSaveFolderMode), which fails the save with a
	// clear error. This wiring is defense-in-depth, kept consistent with the
	// sibling cases.
	case "save-mode", "save-folder-mode":
		cv.validateFileModeField(v, path, str)
	}
}

// performSecurityValidation performs general security validation for all string fields
func (cv *Validator2) performSecurityValidation(v *Validator, path string, str string) bool {
	if cv.sanitizer == nil {
		return true
	}

	// General string sanitization for all fields
	if _, err := cv.sanitizer.SanitizeString(str, 1024); err != nil {
		v.AddError(path, str, fmt.Sprintf("input validation failed: %v", err))
		return false
	}
	return true
}

// validateCronField validates cron expression fields
func (cv *Validator2) validateCronField(v *Validator, path string, str string) {
	v.ValidateCronExpression(path, str)
	if cv.sanitizer != nil {
		if err := cv.sanitizer.ValidateCronExpression(str); err != nil {
			v.AddError(path, str, fmt.Sprintf("cron validation failed: %v", err))
		}
	}
}

// validateEmailField validates email fields
func (cv *Validator2) validateEmailField(v *Validator, path string, str string) {
	v.ValidateEmail(path, str)
	if cv.sanitizer != nil {
		if err := cv.sanitizer.ValidateEmailList(str); err != nil {
			v.AddError(path, str, fmt.Sprintf("email validation failed: %v", err))
		}
	}
}

// validateAddressField validates address fields
func (cv *Validator2) validateAddressField(v *Validator, path string, str string) {
	if !cv.isValidAddress(str) {
		v.AddError(path, str, "invalid address format")
	}
}

// validateLogLevelField validates log level fields
func (cv *Validator2) validateLogLevelField(v *Validator, path string, str string) {
	if !cv.isValidLogLevel(str) {
		v.AddError(path, str, "invalid log level (use: debug, info, warn, error)")
	}
}

// validateCommandField validates command fields
func (cv *Validator2) validateCommandField(v *Validator, path string, str string) {
	if cv.sanitizer != nil {
		if err := cv.sanitizer.ValidateCommand(str); err != nil {
			v.AddError(path, str, fmt.Sprintf("command validation failed: %v", err))
		}
	}
}

// validateImageField validates Docker image fields
func (cv *Validator2) validateImageField(v *Validator, path string, str string) {
	if cv.sanitizer != nil {
		if err := cv.sanitizer.ValidateDockerImage(str); err != nil {
			v.AddError(path, str, fmt.Sprintf("Docker image validation failed: %v", err))
		}
	}
}

// validatePathField validates path fields
func (cv *Validator2) validatePathField(v *Validator, path string, str string) {
	if cv.sanitizer != nil {
		if err := cv.sanitizer.ValidatePath(str, ""); err != nil {
			v.AddError(path, str, fmt.Sprintf("path validation failed: %v", err))
		}
	}
}

// validateFileModeField validates octal file/directory mode fields
// (save-mode, save-folder-mode). Mirrors middlewares.parseFileMode: accepts an
// optional 0o/0O prefix, interprets the remainder as octal, and rejects values
// outside the permission bits (0000-0777). The parser lives in the middlewares
// package too, but config cannot import it (core->config import cycle), so this
// stays a self-contained check.
func (cv *Validator2) validateFileModeField(v *Validator, path, str string) {
	if strings.TrimSpace(str) == "" {
		return // empty resolves to the secure default downstream
	}
	trimmed := strings.TrimPrefix(strings.TrimPrefix(strings.TrimSpace(str), "0o"), "0O")
	mode, err := strconv.ParseUint(trimmed, 8, 32)
	if err != nil {
		v.AddError(path, str, "invalid octal file mode (e.g. 0644)")
		return
	}
	if mode > 0o777 {
		v.AddError(path, str, "file mode out of range: only permission bits 0000-0777 are allowed")
	}
}

// validateIntField validates integer type fields
func (cv *Validator2) validateIntField(v *Validator, field reflect.Value, path string) {
	val := field.Int()

	// Validate port numbers
	if strings.Contains(path, "port") && val > 0 {
		v.ValidateRange(path, int(val), 1, 65535)
	}

	// Validate positive values for counts/sizes
	if (strings.Contains(path, "max") || strings.Contains(path, "size")) && val < 0 {
		v.AddError(path, val, "must be non-negative")
	}
}

// validateSliceField validates slice type fields
func (cv *Validator2) validateSliceField(v *Validator, field reflect.Value, path string) {
	// Validate slice fields (e.g., dependencies)
	// Dependencies validation would need access to all job names - deferred to runtime
}

// isOptionalField checks if a field is optional (can be empty)
func (cv *Validator2) isOptionalField(path string) bool {
	optionalFields := []string{
		"smtp-user", "smtp-password", "email-to", "email-from",
		"slack-webhook", "save-folder", "save-mode", "save-folder-mode",
		"restore-history", "restore-history-max-age",
		"container", "service", "image", "user", "network",
		"environment", "secrets", "volumes", "working_dir",
		"log-level", // Has default value "info"
		"env-file", "env-from",
	}

	for _, field := range optionalFields {
		if strings.Contains(path, field) {
			return true
		}
	}
	return false
}

// requiredWhen names, for a field that only means anything alongside a
// feature, the sibling boolean that switches that feature on.
//
// Without this, a field carrying no `default` tag is required unconditionally,
// which demanded web-UI credentials from every config that enabled strict
// validation — including the ones with no web UI at all. That made strict
// validation impractical to adopt, and an operator who cannot adopt it does
// not get the checks it exists to provide.
// #nosec G101 -- these are INI key names the validator matches on, not values
var requiredWhen = map[string]string{
	"web-password-hash": "web-auth-enabled",
	"web-secret-key":    "web-auth-enabled",
}

// gateIsOpen reports whether a conditionally-required field is currently
// required, i.e. whether the sibling flag that governs it is set. Fields with
// no entry in requiredWhen are always required and answer true.
//
// A gate that cannot be found answers true as well: an unresolvable gate means
// the mapping and the config have drifted apart, and demanding the field is
// the safe direction — it surfaces, where silently dropping the check would
// not.
func (cv *Validator2) gateIsOpen(parent reflect.Value, path string) bool {
	gate, conditional := requiredWhen[path]
	if !conditional {
		return true
	}
	if !parent.IsValid() || parent.Kind() != reflect.Struct {
		return true
	}

	typ := parent.Type()
	for fieldType := range typ.Fields() {
		if !fieldType.IsExported() || fieldType.Type.Kind() != reflect.Bool {
			continue
		}
		if fieldType.Tag.Get("gcfg") != gate && fieldType.Tag.Get("mapstructure") != gate {
			continue
		}
		return parent.FieldByIndex(fieldType.Index).Bool()
	}
	return true
}

// isValidAddress checks if an address string is valid
func (cv *Validator2) isValidAddress(addr string) bool {
	// Allow formats like ":8080", "localhost:8080", "127.0.0.1:8080"
	if addr == "" {
		return false
	}

	// Simple validation - must contain colon for port
	if !strings.Contains(addr, ":") {
		return false
	}

	parts := strings.Split(addr, ":")
	if len(parts) != 2 {
		return false
	}

	// Port must be numeric
	_, err := strconv.Atoi(parts[1])
	return err == nil
}

// isValidLogLevel checks if a log level is valid
func (cv *Validator2) isValidLogLevel(level string) bool {
	validLevels := []string{
		logLevelDebug, logLevelTrace, logLevelInfo, logLevelNotice,
		logLevelWarn, logLevelWarning, logLevelError, logLevelFatal,
		logLevelPanic, logLevelCritical,
	}
	level = strings.ToLower(level)
	return slices.Contains(validLevels, level)
}

// INI keys named in more than one place here: the switch that picks a format
// check, and the table of what each job kind needs. They were literals in both
// until the table arrived, at which point the same key existed twice with
// nothing tying the two together.
const (
	keySchedule  = "schedule"
	keyCommand   = "command"
	keyImage     = "image"
	keyContainer = "container"
)

// jobRequirement is one thing a job of a given kind must carry. Several field
// names mean "at least one of these", which is how job-run accepts either an
// image to start or an existing container to reuse.
type jobRequirement struct {
	fields []string
	// why is appended to the error so the message says what the field is for
	// rather than only that it is missing.
	why string
}

// jobRequirements states what each job section needs in order to run.
//
// The entries are taken from what the runtime already demands, not invented
// here: core.RunJob.Validate returns ErrImageOrContainer, core.RunServiceJob
// .Validate returns ErrImageRequired, ExecJob execs by container id and
// command, LocalJob runs a command, and ComposeJob shells out with a file and
// a service. Checking them here moves those failures from "the job silently
// never runs" to "validate says so".
var jobRequirements = map[string][]jobRequirement{
	"job-exec": {
		{fields: []string{keyContainer}, why: "the container to exec in"},
		{fields: []string{keyCommand}, why: "the command to run"},
	},
	"job-run": {
		{fields: []string{keyImage, keyContainer}, why: "an image to start or an existing container to reuse"},
	},
	"job-local": {
		{fields: []string{keyCommand}, why: "the command to run"},
	},
	"job-service-run": {
		{fields: []string{keyImage}, why: "the image to create the swarm service from"},
	},
	"job-compose": {
		{fields: []string{"file"}, why: "the compose file"},
		{fields: []string{"service"}, why: "the service to run"},
	},
}

// scheduleRequirement applies to every job kind: without a schedule there is
// nothing to register the job under.
var scheduleRequirement = jobRequirement{fields: []string{keySchedule}, why: "when to run"}

// validateJobMap walks the jobs of one section. Each entry is validated like
// any other struct — so formats are checked — and then against the
// requirements for its kind.
func (cv *Validator2) validateJobMap(v *Validator, m reflect.Value, section string) {
	if m.Kind() != reflect.Map || m.IsNil() {
		return
	}

	reqs, known := jobRequirements[section]
	for _, key := range m.MapKeys() {
		entry := m.MapIndex(key)
		val, ok := derefToStruct(entry.Interface())
		if !ok {
			continue
		}

		// Field-level checks (formats, ranges) for everything the job carries.
		// The path carries the section and the job name so an error names the
		// job the user wrote rather than a bare key.
		cv.validateStructIn(v, entry.Interface(), fmt.Sprintf("%s %q", section, key.String()), true)

		if !known {
			continue
		}
		jobName := key.String()
		for _, req := range append([]jobRequirement{scheduleRequirement}, reqs...) {
			cv.checkJobRequirement(v, val, section, jobName, req)
		}
	}
}

// checkJobRequirement reports a requirement that no field satisfies.
func (cv *Validator2) checkJobRequirement(
	v *Validator, job reflect.Value, section, jobName string, req jobRequirement,
) {
	for _, name := range req.fields {
		if strings.TrimSpace(fieldValueByKey(job, name)) != "" {
			return
		}
	}

	v.AddError(
		fmt.Sprintf("%s %q: %s", section, jobName, strings.Join(req.fields, " or ")),
		"",
		fmt.Sprintf("is required (%s)", req.why),
	)
}

// fieldValueByKey returns the string value of the field carrying the given
// config key, searching embedded structs because a job's schedule and command
// live on the BareJob it embeds rather than on the job type itself.
func fieldValueByKey(val reflect.Value, key string) string {
	if val.Kind() != reflect.Struct {
		return ""
	}

	typ := val.Type()
	for fieldType := range typ.Fields() {
		field := val.FieldByIndex(fieldType.Index)

		// Embedded structs are descended into even when the embedded type is
		// unexported: the job types embed exported ones today, but the values
		// are only read here, never handed out, so there is no reason for the
		// lookup to depend on that staying true.
		if fieldType.Anonymous && field.Kind() == reflect.Struct {
			if found := fieldValueByKey(field, key); found != "" {
				return found
			}
			continue
		}

		if !fieldType.IsExported() {
			continue
		}
		if resolveFieldPath("", fieldType.Name, fieldType.Tag.Get("gcfg"), fieldType.Tag.Get("mapstructure")) != key &&
			!strings.EqualFold(fieldType.Name, key) {
			continue
		}
		if field.Kind() == reflect.String {
			return field.String()
		}
	}
	return ""
}
