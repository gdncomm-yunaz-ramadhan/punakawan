package api

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
)

// newID returns a random, URL-safe identifier of the form
// "<prefix>-<16 hex chars>", used for records the panel creates directly
// (reviews, comments) rather than through the CLI/agent pipeline.
func newID(prefix string) (string, error) {
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("api: generate id: %w", err)
	}
	return fmt.Sprintf("%s-%s", prefix, hex.EncodeToString(buf)), nil
}
