// Package session implements the Punakawan Panel's authenticated
// mutation session, per punakawan-artifact-review-plan-mutation-plan-v2.md
// §15: an ephemeral, in-memory, single-process session secret; a
// one-time bootstrap token exchanged for a SameSite=Strict, HttpOnly
// session cookie; and a CSRF token required on every mutating request.
// Nothing here is persisted - a process restart invalidates every
// session, per §15's "session invalidated when the panel stops."
package session

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"sync"
	"time"
)

// bootstrapTokenTTL is deliberately short: the bootstrap token only needs
// to survive the moment between the panel process starting and the
// browser's first request, per §15's "one-time bootstrap token."
const bootstrapTokenTTL = 2 * time.Minute

// sessionTTL is §15's "short session lifetime" - long enough for a normal
// review session, short enough that a stale cookie left in a browser
// profile stops working on its own.
const sessionTTL = 12 * time.Hour

// ErrInvalidBootstrapToken is returned by ExchangeBootstrapToken when the
// token is unknown, already used, or expired.
var ErrInvalidBootstrapToken = errors.New("session: invalid or expired bootstrap token")

type entry struct {
	expiresAt time.Time
	csrfToken string
}

// Manager holds every live bootstrap token and session for one panel
// process. The zero value is not usable; construct via NewManager.
type Manager struct {
	mu        sync.Mutex
	bootstrap map[string]time.Time
	sessions  map[string]entry
}

// NewManager returns an empty Manager.
func NewManager() *Manager {
	return &Manager{
		bootstrap: make(map[string]time.Time),
		sessions:  make(map[string]entry),
	}
}

func randomToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// IssueBootstrapToken mints a single-use token for the panel's initial
// browser open.
func (m *Manager) IssueBootstrapToken() (string, error) {
	tok, err := randomToken()
	if err != nil {
		return "", err
	}
	m.mu.Lock()
	m.bootstrap[tok] = time.Now().Add(bootstrapTokenTTL)
	m.mu.Unlock()
	return tok, nil
}

// ExchangeBootstrapToken consumes token - it is removed whether or not
// this call succeeds, so a token can never be exchanged twice - and, if it
// was valid and unexpired, mints a new session plus its CSRF token.
func (m *Manager) ExchangeBootstrapToken(token string) (sessionID, csrfToken string, expiresAt time.Time, err error) {
	m.mu.Lock()
	expiry, ok := m.bootstrap[token]
	delete(m.bootstrap, token)
	m.mu.Unlock()
	if !ok || time.Now().After(expiry) {
		return "", "", time.Time{}, ErrInvalidBootstrapToken
	}

	sessionID, err = randomToken()
	if err != nil {
		return "", "", time.Time{}, err
	}
	csrfToken, err = randomToken()
	if err != nil {
		return "", "", time.Time{}, err
	}
	expiresAt = time.Now().Add(sessionTTL)

	m.mu.Lock()
	m.sessions[sessionID] = entry{expiresAt: expiresAt, csrfToken: csrfToken}
	m.mu.Unlock()
	return sessionID, csrfToken, expiresAt, nil
}

// ValidSession reports whether sessionID names a live, unexpired session.
func (m *Manager) ValidSession(sessionID string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	e, ok := m.sessions[sessionID]
	return ok && time.Now().Before(e.expiresAt)
}

// ValidCSRF reports whether csrfToken matches the token issued alongside
// sessionID at exchange time.
func (m *Manager) ValidCSRF(sessionID, csrfToken string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	e, ok := m.sessions[sessionID]
	if !ok || time.Now().After(e.expiresAt) {
		return false
	}
	return csrfToken != "" && csrfToken == e.csrfToken
}

// InvalidateAll drops every live session and unused bootstrap token, per
// §15's "session invalidated when the panel stops."
func (m *Manager) InvalidateAll() {
	m.mu.Lock()
	m.bootstrap = make(map[string]time.Time)
	m.sessions = make(map[string]entry)
	m.mu.Unlock()
}
