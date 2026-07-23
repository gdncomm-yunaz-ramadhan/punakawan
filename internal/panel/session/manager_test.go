package session

import "testing"

func TestExchangeBootstrapTokenGrantsASessionAndCSRFToken(t *testing.T) {
	m := NewManager()
	tok, err := m.IssueBootstrapToken()
	if err != nil {
		t.Fatalf("IssueBootstrapToken: %v", err)
	}

	sessionID, csrfToken, _, err := m.ExchangeBootstrapToken(tok)
	if err != nil {
		t.Fatalf("ExchangeBootstrapToken: %v", err)
	}
	if !m.ValidSession(sessionID) {
		t.Fatal("ValidSession = false, want true right after exchange")
	}
	if !m.ValidCSRF(sessionID, csrfToken) {
		t.Fatal("ValidCSRF = false, want true for the token just issued")
	}
}

func TestExchangeBootstrapTokenIsSingleUse(t *testing.T) {
	m := NewManager()
	tok, err := m.IssueBootstrapToken()
	if err != nil {
		t.Fatalf("IssueBootstrapToken: %v", err)
	}
	if _, _, _, err := m.ExchangeBootstrapToken(tok); err != nil {
		t.Fatalf("first exchange: %v", err)
	}
	if _, _, _, err := m.ExchangeBootstrapToken(tok); err != ErrInvalidBootstrapToken {
		t.Fatalf("second exchange err = %v, want ErrInvalidBootstrapToken", err)
	}
}

func TestExchangeBootstrapTokenRejectsUnknownToken(t *testing.T) {
	m := NewManager()
	if _, _, _, err := m.ExchangeBootstrapToken("never-issued"); err != ErrInvalidBootstrapToken {
		t.Fatalf("err = %v, want ErrInvalidBootstrapToken", err)
	}
}

func TestValidSessionRejectsUnknownSession(t *testing.T) {
	m := NewManager()
	if m.ValidSession("no-such-session") {
		t.Fatal("ValidSession = true for a session that was never issued")
	}
}

func TestValidCSRFRejectsWrongToken(t *testing.T) {
	m := NewManager()
	tok, _ := m.IssueBootstrapToken()
	sessionID, _, _, err := m.ExchangeBootstrapToken(tok)
	if err != nil {
		t.Fatalf("ExchangeBootstrapToken: %v", err)
	}
	if m.ValidCSRF(sessionID, "wrong-token") {
		t.Fatal("ValidCSRF = true for a mismatched token")
	}
	if m.ValidCSRF(sessionID, "") {
		t.Fatal("ValidCSRF = true for an empty token")
	}
}

func TestInvalidateAllDropsLiveSessions(t *testing.T) {
	m := NewManager()
	tok, _ := m.IssueBootstrapToken()
	sessionID, _, _, err := m.ExchangeBootstrapToken(tok)
	if err != nil {
		t.Fatalf("ExchangeBootstrapToken: %v", err)
	}

	m.InvalidateAll()
	if m.ValidSession(sessionID) {
		t.Fatal("ValidSession = true after InvalidateAll")
	}
}
