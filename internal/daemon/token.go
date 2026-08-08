package daemon

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"
)

// tokenBytes is the random token length in bytes (256 bits) before hex
// encoding.
const tokenBytes = 32

// LoadOrCreateToken returns the per-install bearer token clients must
// present to reach any authenticated daemon endpoint, minting a fresh
// one on first use. An existing token file with permissions looser than
// owner-only is refused rather than trusted (AC4: "unsafe token
// permissions fail closed") - a token any local user could read would
// let them impersonate an authorized client.
func LoadOrCreateToken(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err == nil {
		if err := checkTokenFilePermissions(path); err != nil {
			return "", err
		}
		token := strings.TrimSpace(string(data))
		if token == "" {
			return "", fmt.Errorf("daemon: token file %s is empty", path)
		}
		return token, nil
	}
	if !errors.Is(err, fs.ErrNotExist) {
		return "", fmt.Errorf("daemon: read token file %s: %w", path, err)
	}

	buf := make([]byte, tokenBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("daemon: generate token: %w", err)
	}
	token := hex.EncodeToString(buf)
	if err := os.WriteFile(path, []byte(token), 0o600); err != nil {
		return "", fmt.Errorf("daemon: write token file %s: %w", path, err)
	}
	return token, nil
}
