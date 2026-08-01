package engines

import (
	"custom_erp/db"
	"database/sql"
	"fmt"
	"strconv"
	"strings"
	"sync"
)

// Stage 28.1: a generic, module-by-module configuration framework. Every
// operationally-meaningful value that used to be a hardcoded Go constant
// (loyalty point expiry, reservation TTL, OTP validity, ...) is declared once
// here as a SettingDefinition - its module, label, type and default - so ONE
// admin Settings screen can render and edit any of them, and the engine that
// consumes it reads the live value instead of a literal. Mirrors the report
// registry (report_registry.go): register once, drive generically. Adding a
// setting from here on is one RegisterSetting call (settings_definitions.go)
// plus a GetSetting* read at the consuming site - no new endpoint or UI code.
//
// Storage is per-tenant (the system_settings table, one row per overridden
// key). An unset key falls back to its registered Default, so an empty table
// reproduces exactly the pre-Stage-28 hardcoded behavior - nothing changes
// until an admin edits a value.

// Setting value types - how a setting is validated and rendered.
const (
	SettingTypeInt    = "int"
	SettingTypeFloat  = "float"
	SettingTypeBool   = "bool"
	SettingTypeString = "string"
	SettingTypeSelect = "select"
)

// SettingOption is one choice for a select-type setting.
type SettingOption struct {
	Value string `json:"value"`
	Label string `json:"label"`
}

// SettingDefinition declares one configurable setting. Min/Max apply to the
// numeric types (int and float) only; Options to select settings only.
//
// Min/Max are float64 so one pair serves both numeric types - an int setting
// simply carries whole-number bounds. Stage 30.7 uses them as the guardrail
// on platform-safety settings (API page-size cap, per-tenant concurrency,
// batch sizes): those are configurable rather than hardcoded, but bounded so
// a mistyped value can't destabilize the server.
type SettingDefinition struct {
	Key         string          `json:"key"`
	Module      string          `json:"module"`
	Label       string          `json:"label"`
	Description string          `json:"description"`
	Type        string          `json:"type"`
	Default     string          `json:"default"`
	Unit        string          `json:"unit,omitempty"`
	Options     []SettingOption `json:"options,omitempty"`
	Min         *float64        `json:"min,omitempty"`
	Max         *float64        `json:"max,omitempty"`
}

var settingsRegistry = map[string]SettingDefinition{}
var settingsRegistryOrder []string

// RegisterSetting adds one definition to the registry. Called only from
// settings_definitions.go's init() - a duplicate key is a build-time
// programmer error, so it panics rather than silently overwriting (same
// posture as RegisterReport).
func RegisterSetting(def SettingDefinition) {
	if _, exists := settingsRegistry[def.Key]; exists {
		panic(fmt.Sprintf("setting %q already registered", def.Key))
	}
	settingsRegistry[def.Key] = def
	settingsRegistryOrder = append(settingsRegistryOrder, def.Key)
}

// ListSettingDefinitions returns every registered setting in registration
// order (the admin UI groups them by Module).
func ListSettingDefinitions() []SettingDefinition {
	out := make([]SettingDefinition, 0, len(settingsRegistryOrder))
	for _, k := range settingsRegistryOrder {
		out = append(out, settingsRegistry[k])
	}
	return out
}

// --- value resolution (in-process cache -> DB override -> registered default) ---

var settingsCache = map[string]string{}
var settingsCacheMu sync.RWMutex

func settingsCacheKey(schema, key string) string { return schema + "\x00" + key }

// rawSettingForSchema is the schema-scoped primitive - some consumers (a
// per-tenant background worker) hold a schema, not a tenantID, the same split
// insertLoyaltyLedgerEntryInSchema uses. Returns the stored override, or the
// registered default when unset. Cached in-process; SetSetting invalidates on
// write.
func rawSettingForSchema(schema, key string) string {
	def := settingsRegistry[key] // zero value (Default "") if the key is unregistered
	ck := settingsCacheKey(schema, key)
	settingsCacheMu.RLock()
	if v, hit := settingsCache[ck]; hit {
		settingsCacheMu.RUnlock()
		return v
	}
	settingsCacheMu.RUnlock()

	var value string
	err := db.DB.QueryRow(fmt.Sprintf("SELECT value FROM %s.system_settings WHERE key = $1", schema), key).Scan(&value)
	if err == sql.ErrNoRows {
		value = def.Default
	} else if err != nil {
		// Transient read failure (or the table absent in an older tenant
		// schema): fall back to the default but do NOT cache, so a later
		// healthy read still picks up a real override.
		return def.Default
	}
	settingsCacheMu.Lock()
	settingsCache[ck] = value
	settingsCacheMu.Unlock()
	return value
}

func rawSetting(tenantID, key string) string {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return settingsRegistry[key].Default
	}
	return rawSettingForSchema(schema, key)
}

// GetSettingString returns the effective string value for a setting.
func GetSettingString(tenantID, key string) string { return rawSetting(tenantID, key) }

// GetSettingInt returns the effective int value, falling back to the
// registered default (then 0) if a stored value is somehow non-numeric. Never
// panics - a config read must never crash a request path.
func GetSettingInt(tenantID, key string) int {
	return parseSettingInt(rawSetting(tenantID, key), key)
}

// GetSettingIntForSchema is the schema-scoped variant of GetSettingInt, for
// schema-holding callers such as per-tenant background workers.
func GetSettingIntForSchema(schema, key string) int {
	return parseSettingInt(rawSettingForSchema(schema, key), key)
}

func parseSettingInt(raw, key string) int {
	if n, err := strconv.Atoi(strings.TrimSpace(raw)); err == nil {
		return n
	}
	if n, err := strconv.Atoi(strings.TrimSpace(settingsRegistry[key].Default)); err == nil {
		return n
	}
	return 0
}

// GetSettingFloat returns the effective float value, falling back to the
// registered default (then 0) if a stored value is somehow non-numeric. Same
// never-panic posture as GetSettingInt - a config read must never crash a
// request path.
func GetSettingFloat(tenantID, key string) float64 {
	return parseSettingFloat(rawSetting(tenantID, key), key)
}

// GetSettingFloatForSchema is the schema-scoped variant of GetSettingFloat.
func GetSettingFloatForSchema(schema, key string) float64 {
	return parseSettingFloat(rawSettingForSchema(schema, key), key)
}

func parseSettingFloat(raw, key string) float64 {
	if f, err := strconv.ParseFloat(strings.TrimSpace(raw), 64); err == nil {
		return f
	}
	if f, err := strconv.ParseFloat(strings.TrimSpace(settingsRegistry[key].Default), 64); err == nil {
		return f
	}
	return 0
}

// GetSettingBool returns the effective bool value ("true"/"false").
func GetSettingBool(tenantID, key string) bool {
	return strings.EqualFold(strings.TrimSpace(rawSetting(tenantID, key)), "true")
}

// GetSettingBoolForSchema is the schema-scoped variant of GetSettingBool.
func GetSettingBoolForSchema(schema, key string) bool {
	return strings.EqualFold(strings.TrimSpace(rawSettingForSchema(schema, key)), "true")
}

// GetSettingStringForSchema is the schema-scoped variant of GetSettingString.
func GetSettingStringForSchema(schema, key string) string {
	return rawSettingForSchema(schema, key)
}

// SettingIsOverridden reports whether an admin has explicitly set this key
// for the tenant, as opposed to it still sitting at its registered default.
//
// This exists for the handful of settings that also have a deployment-level
// environment-variable override (session token TTL / JWT_EXPIRY_HOURS). The
// precedence has to be: an explicit admin edit wins, otherwise the env var,
// otherwise the registered default. Without this check the env var would
// silently beat whatever the admin typed on the Configuration screen - a
// control that looks wired but does nothing, which is worse than a constant.
func SettingIsOverridden(tenantID, key string) bool {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return false
	}
	var exists bool
	if err := db.DB.QueryRow(fmt.Sprintf(
		"SELECT EXISTS(SELECT 1 FROM %s.system_settings WHERE key = $1)", schema), key).Scan(&exists); err != nil {
		return false
	}
	return exists
}

// SettingWithValue is a definition plus the tenant's current effective value -
// the shape the admin Settings GET returns.
type SettingWithValue struct {
	SettingDefinition
	Value string `json:"value"`
}

// ListSettingsWithValues returns every registered setting with the tenant's
// current effective value, in registration order.
func ListSettingsWithValues(tenantID string) []SettingWithValue {
	schema, err := db.GetTenantSchema(tenantID)
	defs := ListSettingDefinitions()
	out := make([]SettingWithValue, 0, len(defs))
	for _, d := range defs {
		val := d.Default
		if err == nil {
			val = rawSettingForSchema(schema, d.Key)
		}
		out = append(out, SettingWithValue{SettingDefinition: d, Value: val})
	}
	return out
}

// SetSetting validates value against key's definition and persists it for the
// tenant, invalidating the cache. Returns an error whose message is safe to
// show the user (the handler surfaces it verbatim via writeAPIErrorGeneric).
func SetSetting(tenantID, key, value, updatedBy string) error {
	def, ok := settingsRegistry[key]
	if !ok {
		return fmt.Errorf("unknown setting %q", key)
	}
	if err := validateSettingValue(def, value); err != nil {
		return err
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	if _, err := db.DB.Exec(fmt.Sprintf(`
		INSERT INTO %s.system_settings (key, value, updated_at, updated_by)
		VALUES ($1, $2, now(), $3)
		ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, updated_at = now(), updated_by = EXCLUDED.updated_by`, schema),
		key, value, updatedBy); err != nil {
		return err
	}
	settingsCacheMu.Lock()
	delete(settingsCache, settingsCacheKey(schema, key))
	settingsCacheMu.Unlock()
	return nil
}

// ValidateSetting checks a value against a registered setting's definition
// without persisting it - lets a batch update validate every key before
// writing any, so one bad value never leaves a half-applied batch.
func ValidateSetting(key, value string) error {
	def, ok := settingsRegistry[key]
	if !ok {
		return fmt.Errorf("unknown setting %q", key)
	}
	return validateSettingValue(def, value)
}

func validateSettingValue(def SettingDefinition, value string) error {
	switch def.Type {
	case SettingTypeInt:
		n, err := strconv.Atoi(strings.TrimSpace(value))
		if err != nil {
			return fmt.Errorf("%s must be a whole number", def.Label)
		}
		return checkSettingBounds(def, float64(n))
	case SettingTypeFloat:
		f, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
		if err != nil {
			return fmt.Errorf("%s must be a number", def.Label)
		}
		return checkSettingBounds(def, f)
	case SettingTypeBool:
		v := strings.ToLower(strings.TrimSpace(value))
		if v != "true" && v != "false" {
			return fmt.Errorf("%s must be true or false", def.Label)
		}
	case SettingTypeSelect:
		for _, o := range def.Options {
			if o.Value == value {
				return nil
			}
		}
		return fmt.Errorf("%q is not a valid option for %s", value, def.Label)
	case SettingTypeString:
		// free text - no constraint beyond being present
	default:
		return fmt.Errorf("setting %q has an unknown type %q", def.Key, def.Type)
	}
	return nil
}

// checkSettingBounds enforces a numeric setting's registered Min/Max. This is
// the guardrail that makes platform-safety settings safe to expose: the value
// is admin-editable rather than hardcoded, but can't be set somewhere that
// would destabilize the server.
func checkSettingBounds(def SettingDefinition, n float64) error {
	if def.Min != nil && n < *def.Min {
		return fmt.Errorf("%s must be at least %s", def.Label, formatSettingNumber(*def.Min))
	}
	if def.Max != nil && n > *def.Max {
		return fmt.Errorf("%s must be at most %s", def.Label, formatSettingNumber(*def.Max))
	}
	return nil
}

// formatSettingNumber renders a bound the way an admin wrote it - "10" not
// "10.000000", "2.5" stays "2.5".
func formatSettingNumber(f float64) string {
	return strconv.FormatFloat(f, 'f', -1, 64)
}
