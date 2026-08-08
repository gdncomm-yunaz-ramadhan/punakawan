package daemon

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"
)

func startTestTransport(t *testing.T, token string, ready func() error) *Transport {
	t.Helper()
	tr, err := NewTransport("127.0.0.1", "0", token, ready)
	if err != nil {
		t.Fatalf("NewTransport: %v", err)
	}
	go tr.Serve()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		tr.Shutdown(ctx)
	})
	return tr
}

func TestHealthzUnauthenticated(t *testing.T) {
	tr := startTestTransport(t, "secret-token", nil)
	resp, err := http.Get("http://" + tr.Addr() + "/healthz")
	if err != nil {
		t.Fatalf("GET /healthz: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var body map[string]string
	json.NewDecoder(resp.Body).Decode(&body)
	if body["status"] != "alive" {
		t.Fatalf("unexpected body: %+v", body)
	}
}

func TestReadyzRequiresToken(t *testing.T) {
	tr := startTestTransport(t, "secret-token", nil)

	resp, err := http.Get("http://" + tr.Addr() + "/readyz")
	if err != nil {
		t.Fatalf("GET /readyz without token: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 without token, got %d", resp.StatusCode)
	}

	req, _ := http.NewRequest(http.MethodGet, "http://"+tr.Addr()+"/readyz", nil)
	req.Header.Set("Authorization", "Bearer wrong-token")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /readyz with wrong token: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 with wrong token, got %d", resp.StatusCode)
	}

	req.Header.Set("Authorization", "Bearer secret-token")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /readyz with correct token: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 with correct token, got %d", resp.StatusCode)
	}
}

func TestReadyzReflectsReadyFunc(t *testing.T) {
	tr := startTestTransport(t, "secret-token", func() error { return errUnready })

	req, _ := http.NewRequest(http.MethodGet, "http://"+tr.Addr()+"/readyz", nil)
	req.Header.Set("Authorization", "Bearer secret-token")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /readyz: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 when not ready, got %d", resp.StatusCode)
	}
}

func TestUnexpectedHostRejected(t *testing.T) {
	tr := startTestTransport(t, "secret-token", nil)
	req, _ := http.NewRequest(http.MethodGet, "http://"+tr.Addr()+"/healthz", nil)
	req.Host = "evil.example.com"
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /healthz with forged Host: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403 for unexpected Host, got %d", resp.StatusCode)
	}
}

var errUnready = &notReadyError{}

type notReadyError struct{}

func (*notReadyError) Error() string { return "storage not open yet" }
