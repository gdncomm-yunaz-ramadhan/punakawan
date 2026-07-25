package project

import (
	"errors"
	"fmt"
	"reflect"
	"regexp"
	"strings"
)

// Exported error kinds. Callers (the HTTP handler) map these to machine
// codes and status codes with errors.Is, so their identity - not their
// message - is the contract.
var (
	// ErrRevisionConflict signals an optimistic-locking failure: the base
	// revision a mutation was made against no longer matches the project's
	// current revision. The HTTP layer answers 409.
	ErrRevisionConflict = errors.New("project: revision conflict")
	// ErrDuplicateKey signals an attempt to add a key that already exists
	// (case-insensitively). Machine code "duplicate_key", HTTP 400.
	ErrDuplicateKey = errors.New("project: duplicate metadata key")
	// ErrSecretRejected signals a key that looks like it names a secret.
	// Machine code "secret_rejected", HTTP 400.
	ErrSecretRejected = errors.New("project: secret-like metadata rejected")
	// ErrInvalidValue signals a value whose type is not one of the permitted
	// kinds. Machine code "invalid_value", HTTP 400.
	ErrInvalidValue = errors.New("project: invalid metadata value")
	// ErrMissingField signals a required field (key, description, value) that
	// is empty or absent. Machine code "missing_field", HTTP 400.
	ErrMissingField = errors.New("project: missing required field")
	// ErrKeyNotFound signals a mutation targeting a key that does not exist.
	// HTTP 404.
	ErrKeyNotFound = errors.New("project: metadata key not found")
)

// secretKeyPattern matches keys that name credentials, per §4.1's "secret
// values are forbidden in normal metadata": secrets belong in the
// environment or secure configuration, never in the readable, versioned,
// audited project.yaml.
var secretKeyPattern = regexp.MustCompile(`(?i)(secret|token|password|passwd|api[_-]?key|credential)`)

// validateEntry enforces §4.1's metadata rules on a single entry: key,
// description, and value are all required; the key must not look like a
// secret; and the value must be one of the permitted kinds. Uniqueness is
// enforced by the caller (Project.AddMetadata), which alone has the full set
// to compare against.
func validateEntry(e MetadataEntry) error {
	if strings.TrimSpace(e.Key) == "" {
		return fmt.Errorf("project: metadata key is required: %w", ErrMissingField)
	}
	if secretKeyPattern.MatchString(e.Key) {
		return fmt.Errorf("project: key %q names a secret; store credentials in the environment, not metadata: %w", e.Key, ErrSecretRejected)
	}
	if strings.TrimSpace(e.Description) == "" {
		return fmt.Errorf("project: description is required for key %q: %w", e.Key, ErrMissingField)
	}
	if e.Value == nil {
		return fmt.Errorf("project: value is required for key %q: %w", e.Key, ErrMissingField)
	}
	if err := validateValue(e.Value); err != nil {
		return err
	}
	return nil
}

// validateValue accepts string, number (any int/uint/float kind), bool,
// []string, and structured map/slice values, and rejects everything else
// (functions, channels, and other unmarshalable kinds), per §4.1's value
// rules.
func validateValue(v any) error {
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.String, reflect.Bool,
		reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float32, reflect.Float64:
		return nil
	case reflect.Slice, reflect.Array:
		// Every element must itself be a permitted kind (covers []string,
		// []any of scalars, and lists of structured maps).
		for i := 0; i < rv.Len(); i++ {
			if err := validateValue(rv.Index(i).Interface()); err != nil {
				return err
			}
		}
		return nil
	case reflect.Map:
		for _, key := range rv.MapKeys() {
			if err := validateValue(rv.MapIndex(key).Interface()); err != nil {
				return err
			}
		}
		return nil
	case reflect.Interface, reflect.Ptr:
		if rv.IsNil() {
			return fmt.Errorf("project: nil value is not permitted: %w", ErrInvalidValue)
		}
		return validateValue(rv.Elem().Interface())
	default:
		return fmt.Errorf("project: value of kind %s is not permitted (allowed: string, number, bool, list, structured map): %w", rv.Kind(), ErrInvalidValue)
	}
}
