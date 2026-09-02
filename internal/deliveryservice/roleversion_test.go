package deliveryservice

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/ygrip/punakawan/internal/agent"
	"github.com/ygrip/punakawan/internal/delivery"
	"github.com/ygrip/punakawan/internal/plan"
	"github.com/ygrip/punakawan/internal/storage"
	"github.com/ygrip/punakawan/internal/telemetry"
)

// fakeAgentRegistry is a minimal agent.AgentRegistry stub for tests that
// only need Get to resolve a fixed set of role versions, without
// depending on the real embedded prompts/*/agent.yaml manifests.
type fakeAgentRegistry struct {
	versions map[string]string
}

func (r fakeAgentRegistry) List() []agent.RoleSpec { return nil }

func (r fakeAgentRegistry) Get(id string) (agent.RoleSpec, error) {
	v, ok := r.versions[id]
	if !ok {
		return agent.RoleSpec{}, fmt.Errorf("unknown role %q", id)
	}
	return agent.RoleSpec{ID: id, Version: v}, nil
}

func (r fakeAgentRegistry) Reload() error { return nil }

func newServiceWithTelemetryAndAgents(t *testing.T) (*Service, *telemetry.Store) {
	t.Helper()
	db, err := storage.Open(context.Background(), filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("storage.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	tstore := telemetry.NewStore(db)
	reg := fakeAgentRegistry{versions: map[string]string{"bagong": "1"}}
	return New(delivery.NewStore(db), plan.NewStore(db), WithTelemetryStore(tstore), WithAgentRegistry(reg)), tstore
}

func TestBeginTelemetrySessionResolvesRoleVersion(t *testing.T) {
	svc, tstore := newServiceWithTelemetryAndAgents(t)
	req := jiraRequest("tenant-a", "ABC-123", "bagong")
	req.Session.Provider = "claude-code"
	req.Session.ExternalSessionID = "claude-sess-role"
	result := mustStart(t, svc, req)

	if result.TelemetrySession == nil {
		t.Fatal("TelemetrySession = nil, want a begun telemetry session")
	}
	if result.TelemetrySession.RoleVersion != "1" {
		t.Fatalf("TelemetrySession.RoleVersion = %q, want %q", result.TelemetrySession.RoleVersion, "1")
	}

	// Round-trip through GetSession, per the acceptance criteria.
	fetched, err := tstore.GetSession(context.Background(), result.TelemetrySession.ID)
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if fetched.RoleVersion != "1" {
		t.Fatalf("GetSession(...).RoleVersion = %q, want %q", fetched.RoleVersion, "1")
	}
}

func TestBeginTelemetrySessionUnknownParticipantLeavesRoleVersionEmpty(t *testing.T) {
	svc, tstore := newServiceWithTelemetryAndAgents(t)
	req := jiraRequest("tenant-a", "ABC-999", "not-a-real-role")
	req.Session.Provider = "claude-code"
	req.Session.ExternalSessionID = "claude-sess-unknown"
	result := mustStart(t, svc, req)

	if result.TelemetrySession == nil {
		t.Fatal("TelemetrySession = nil, want a begun telemetry session")
	}
	if result.TelemetrySession.RoleVersion != "" {
		t.Fatalf("TelemetrySession.RoleVersion = %q, want empty for an unknown participant", result.TelemetrySession.RoleVersion)
	}

	fetched, err := tstore.GetSession(context.Background(), result.TelemetrySession.ID)
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if fetched.RoleVersion != "" {
		t.Fatalf("GetSession(...).RoleVersion = %q, want empty", fetched.RoleVersion)
	}
}
