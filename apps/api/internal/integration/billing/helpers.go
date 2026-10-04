package billing

import (
	"github.com/gofrs/uuid"
)

// asStringAny extracts a string from an any (JSON-decoded value).
// Returns "" if v is nil or not a string.
func asStringAny(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

// asBool extracts a bool from an any.
func asBool(v any) bool {
	if b, ok := v.(bool); ok {
		return b
	}
	return false
}

// uuidFromString parses a UUID, returning uuid.Nil on parse error (caller
// must check the err from the store path — this is just a defensive helper).
func uuidFromString(s string) (uuid.UUID, error) {
	return uuid.FromString(s)
}