package delivery

import "github.com/oklog/ulid/v2"

// newID returns a filesystem-safe, lexicographically sortable ULID
// (Crockford base32, 26 chars, no path separators). ulid.Make() draws
// from a package-level monotonic entropy source guarded by its own
// mutex, so concurrent calls are safe and same-millisecond IDs stay
// ordered.
func newID() string {
	return ulid.Make().String()
}
