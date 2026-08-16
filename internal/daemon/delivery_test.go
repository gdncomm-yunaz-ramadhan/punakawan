package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/ygrip/punakawan/internal/delivery"
	"github.com/ygrip/punakawan/internal/storage"
	"github.com/ygrip/punakawan/pkg/protocol"
)

// newTestDeliveryStore opens a throwaway storage kernel database and
// wraps it in a delivery.Store, mirroring internal/delivery's own
// newTestDB/newTestStore test helpers - this package cannot reuse those
// directly since they are unexported in a different package.
func newTestDeliveryStore(t *testing.T) *delivery.Store {
	t.Helper()
	db, err := storage.Open(context.Background(), filepath.Join(t.TempDir(), "delivery.db"))
	if err != nil {
		t.Fatalf("storage.Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return delivery.NewStore(db)
}

// deliveryTestServer bundles a running Transport together with the same
// delivery.Store it serves, so a test can seed fixtures directly through
// the store and then exercise them over HTTP.
type deliveryTestServer struct {
	tr    *Transport
	store *delivery.Store
	token string
}

func newDeliveryTestServer(t *testing.T) *deliveryTestServer {
	t.Helper()
	store := newTestDeliveryStore(t)
	token := "secret-token"
	tr, err := NewTransport("127.0.0.1", "0", token, nil, store)
	if err != nil {
		t.Fatalf("NewTransport: %v", err)
	}
	go tr.Serve()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		tr.Shutdown(ctx)
	})
	return &deliveryTestServer{tr: tr, store: store, token: token}
}

// request issues one authenticated HTTP request against the test
// transport without touching *testing.T, so it is also safe to call from
// a goroutine other than the one running the test (t.Fatalf is not).
func (s *deliveryTestServer) request(method, path string, body any) (*http.Response, error) {
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		reader = bytes.NewReader(encoded)
	}
	req, err := http.NewRequest(method, "http://"+s.tr.Addr()+path, reader)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+s.token)
	return http.DefaultClient.Do(req)
}

func (s *deliveryTestServer) do(t *testing.T, method, path string, body any) *http.Response {
	t.Helper()
	resp, err := s.request(method, path, body)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	return resp
}

// newApprovableFixture builds one orchestration with a project, a routed
// parent task, and a pending approval manifest - the minimum needed to
// exercise the approve/reject route.
func newApprovableFixture(t *testing.T, store *delivery.Store) (orchestrationID, manifestID string) {
	t.Helper()
	ctx := context.Background()
	orch, err := store.CreateOrchestration(ctx, "orch-"+delivery.NewID(), delivery.NewID(), nil)
	if err != nil {
		t.Fatalf("CreateOrchestration: %v", err)
	}
	project, err := store.RegisterProject(ctx, "project-"+delivery.NewID(), delivery.NewID(), "svc-"+delivery.NewID(), "https://example.test/svc.git", "main")
	if err != nil {
		t.Fatalf("RegisterProject: %v", err)
	}
	source, err := store.CaptureRequirement(ctx, delivery.NewID(), orch.Id, delivery.SourceInput{Provider: "freetext", Title: "T", Summary: "S"})
	if err != nil {
		t.Fatalf("CaptureRequirement: %v", err)
	}
	taskID := delivery.NewID()
	if _, err := store.CreateParentTask(ctx, delivery.NewID(), taskID, orch.Id, "Task", []string{source.Id}); err != nil {
		t.Fatalf("CreateParentTask: %v", err)
	}
	if _, err := store.RouteParentTask(ctx, delivery.NewID(), orch.Id, taskID, project.Id); err != nil {
		t.Fatalf("RouteParentTask: %v", err)
	}
	manifestID = delivery.NewID()
	if _, err := store.CreateApprovalManifest(ctx, delivery.NewID(), manifestID, orch.Id, project.Id, []string{taskID}, delivery.ManifestPlan{}); err != nil {
		t.Fatalf("CreateApprovalManifest: %v", err)
	}
	return orch.Id, manifestID
}

func TestHandleListDeliveriesReturnsEveryOrchestration(t *testing.T) {
	s := newDeliveryTestServer(t)
	ctx := context.Background()
	first, err := s.store.CreateOrchestration(ctx, "first", delivery.NewID(), nil)
	if err != nil {
		t.Fatalf("CreateOrchestration: %v", err)
	}
	time.Sleep(2 * time.Millisecond)
	second, err := s.store.CreateOrchestration(ctx, "second", delivery.NewID(), nil)
	if err != nil {
		t.Fatalf("CreateOrchestration: %v", err)
	}

	resp := s.do(t, http.MethodGet, "/api/v1/deliveries", nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var list []*protocol.DeliveryOrchestration
	if err := json.NewDecoder(resp.Body).Decode(&list); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(list) != 2 || list[0].Id != first.Id || list[1].Id != second.Id {
		t.Fatalf("unexpected list: %+v", list)
	}
}

func TestHandleDeliveryViewNotFound(t *testing.T) {
	s := newDeliveryTestServer(t)
	resp := s.do(t, http.MethodGet, "/api/v1/deliveries/unknown", nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", resp.StatusCode)
	}
}

func TestHandleDeliveryViewReturnsCurrentState(t *testing.T) {
	s := newDeliveryTestServer(t)
	ctx := context.Background()
	orch, err := s.store.CreateOrchestration(ctx, "orch", delivery.NewID(), nil)
	if err != nil {
		t.Fatalf("CreateOrchestration: %v", err)
	}

	resp := s.do(t, http.MethodGet, "/api/v1/deliveries/"+orch.Id, nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var view delivery.DeliveryView
	if err := json.NewDecoder(resp.Body).Decode(&view); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if view.Orchestration.Id != orch.Id {
		t.Fatalf("unexpected orchestration in view: %+v", view.Orchestration)
	}
}

func TestHandleDeliveryViewWaitSecondsBlocksUntilTimeout(t *testing.T) {
	s := newDeliveryTestServer(t)
	ctx := context.Background()
	orch, err := s.store.CreateOrchestration(ctx, "orch", delivery.NewID(), nil)
	if err != nil {
		t.Fatalf("CreateOrchestration: %v", err)
	}

	start := time.Now()
	resp := s.do(t, http.MethodGet, fmt.Sprintf("/api/v1/deliveries/%s?since_seq=0&wait_seconds=1", orch.Id), nil)
	elapsed := time.Since(start)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if elapsed < 900*time.Millisecond {
		t.Fatalf("expected the handler to block for ~1s with nothing new, only took %s", elapsed)
	}
}

type watchResult struct {
	elapsed time.Duration
	status  int
	err     error
}

func TestHandleDeliveryViewWaitSecondsReturnsEarlyOnChange(t *testing.T) {
	s := newDeliveryTestServer(t)
	ctx := context.Background()
	orch, err := s.store.CreateOrchestration(ctx, "orch", delivery.NewID(), nil)
	if err != nil {
		t.Fatalf("CreateOrchestration: %v", err)
	}

	results := make(chan watchResult, 1)
	go func() {
		start := time.Now()
		resp, err := s.request(http.MethodGet, fmt.Sprintf("/api/v1/deliveries/%s?since_seq=0&wait_seconds=5", orch.Id), nil)
		if err != nil {
			results <- watchResult{err: err}
			return
		}
		resp.Body.Close()
		results <- watchResult{elapsed: time.Since(start), status: resp.StatusCode}
	}()

	time.Sleep(300 * time.Millisecond)
	if _, err := s.store.RegisterInput(ctx, "reg", orch.Id, orch.Revision, protocol.DeliveryOrchestrationUnresolvedInputsElem{Reference: "R"}); err != nil {
		t.Fatalf("RegisterInput: %v", err)
	}

	select {
	case res := <-results:
		if res.err != nil {
			t.Fatalf("watch request: %v", res.err)
		}
		if res.status != http.StatusOK {
			t.Fatalf("expected 200, got %d", res.status)
		}
		if res.elapsed >= 5*time.Second {
			t.Fatalf("expected the handler to return before the full wait window, took %s", res.elapsed)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("handler did not return promptly after the update")
	}
}

func TestHandleAnswerDeliveryQuestionResolvedRequirement(t *testing.T) {
	s := newDeliveryTestServer(t)
	ctx := context.Background()
	orch, err := s.store.CreateOrchestration(ctx, "orch", delivery.NewID(), []protocol.DeliveryOrchestrationUnresolvedInputsElem{{Reference: "REQ-1"}})
	if err != nil {
		t.Fatalf("CreateOrchestration: %v", err)
	}

	body := answerDeliveryQuestionRequest{
		Reference: "REQ-1", ExpectedRevision: orch.Revision,
		Provider: "freetext", Title: "T", Summary: "S",
	}
	resp := s.do(t, http.MethodPost, "/api/v1/deliveries/"+orch.Id+"/answer-question", body)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var view delivery.DeliveryView
	if err := json.NewDecoder(resp.Body).Decode(&view); err != nil {
		t.Fatalf("decode: %v", err)
	}
	for _, q := range view.PendingQuestions {
		if q == "REQ-1" {
			t.Fatalf("expected REQ-1 to be resolved, still pending: %+v", view.PendingQuestions)
		}
	}
}

func TestHandleAnswerDeliveryQuestionRequiresProviderOrRouting(t *testing.T) {
	s := newDeliveryTestServer(t)
	ctx := context.Background()
	orch, err := s.store.CreateOrchestration(ctx, "orch", delivery.NewID(), nil)
	if err != nil {
		t.Fatalf("CreateOrchestration: %v", err)
	}

	resp := s.do(t, http.MethodPost, "/api/v1/deliveries/"+orch.Id+"/answer-question", answerDeliveryQuestionRequest{Reference: "x"})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestHandleApproveProjectDeliveryApproves(t *testing.T) {
	s := newDeliveryTestServer(t)
	orchID, manifestID := newApprovableFixture(t, s.store)

	body := approveProjectDeliveryRequest{ManifestId: manifestID, ApprovedBy: "human-1"}
	resp := s.do(t, http.MethodPost, "/api/v1/deliveries/"+orchID+"/approve", body)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var view delivery.DeliveryView
	if err := json.NewDecoder(resp.Body).Decode(&view); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(view.PendingApprovals) != 0 {
		t.Fatalf("expected manifest no longer pending, got %+v", view.PendingApprovals)
	}
}

func TestHandleApproveProjectDeliveryRejects(t *testing.T) {
	s := newDeliveryTestServer(t)
	orchID, manifestID := newApprovableFixture(t, s.store)

	body := approveProjectDeliveryRequest{ManifestId: manifestID, ApprovedBy: "human-1", Reject: true}
	resp := s.do(t, http.MethodPost, "/api/v1/deliveries/"+orchID+"/approve", body)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var view delivery.DeliveryView
	if err := json.NewDecoder(resp.Body).Decode(&view); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(view.PendingApprovals) != 0 {
		t.Fatalf("expected manifest no longer pending, got %+v", view.PendingApprovals)
	}
}

func TestHandleCancelDelivery(t *testing.T) {
	s := newDeliveryTestServer(t)
	ctx := context.Background()
	orch, err := s.store.CreateOrchestration(ctx, "orch", delivery.NewID(), nil)
	if err != nil {
		t.Fatalf("CreateOrchestration: %v", err)
	}

	body := cancelDeliveryRequest{ExpectedRevision: orch.Revision}
	resp := s.do(t, http.MethodPost, "/api/v1/deliveries/"+orch.Id+"/cancel", body)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var view delivery.DeliveryView
	if err := json.NewDecoder(resp.Body).Decode(&view); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if view.Orchestration.Status != protocol.DeliveryOrchestrationStatusCancelled {
		t.Fatalf("expected cancelled status, got %s", view.Orchestration.Status)
	}
}

func TestHandleCancelDeliveryRevisionConflict(t *testing.T) {
	s := newDeliveryTestServer(t)
	ctx := context.Background()
	orch, err := s.store.CreateOrchestration(ctx, "orch", delivery.NewID(), nil)
	if err != nil {
		t.Fatalf("CreateOrchestration: %v", err)
	}

	body := cancelDeliveryRequest{ExpectedRevision: orch.Revision + 1}
	resp := s.do(t, http.MethodPost, "/api/v1/deliveries/"+orch.Id+"/cancel", body)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("expected 409, got %d", resp.StatusCode)
	}
}

func TestHandleDeliveryEvidenceServesRawBytes(t *testing.T) {
	s := newDeliveryTestServer(t)
	ctx := context.Background()
	orch, err := s.store.CreateOrchestration(ctx, "orch", delivery.NewID(), nil)
	if err != nil {
		t.Fatalf("CreateOrchestration: %v", err)
	}
	project, err := s.store.RegisterProject(ctx, "project", delivery.NewID(), "svc", "https://example.test/svc.git", "main")
	if err != nil {
		t.Fatalf("RegisterProject: %v", err)
	}
	source, err := s.store.CaptureRequirement(ctx, delivery.NewID(), orch.Id, delivery.SourceInput{Provider: "freetext", Title: "T"})
	if err != nil {
		t.Fatalf("CaptureRequirement: %v", err)
	}
	taskID := delivery.NewID()
	if _, err := s.store.CreateParentTask(ctx, delivery.NewID(), taskID, orch.Id, "Task", []string{source.Id}); err != nil {
		t.Fatalf("CreateParentTask: %v", err)
	}
	if _, err := s.store.RouteParentTask(ctx, delivery.NewID(), orch.Id, taskID, project.Id); err != nil {
		t.Fatalf("RouteParentTask: %v", err)
	}
	laneID := delivery.NewID()
	if _, err := s.store.CreateLane(ctx, delivery.NewID(), laneID, orch.Id, project.Id, taskID); err != nil {
		t.Fatalf("CreateLane: %v", err)
	}
	hash, err := s.store.PutArtifact(ctx, []byte("evidence bytes"), "text/plain")
	if err != nil {
		t.Fatalf("PutArtifact: %v", err)
	}
	artifact, err := s.store.RecordArtifact(ctx, delivery.NewID(), delivery.NewID(), delivery.ArtifactRef{
		OrchestrationID: orch.Id, ProjectID: project.Id, LaneID: laneID, Kind: protocol.EvidenceArtifactKindCommand,
	}, hash)
	if err != nil {
		t.Fatalf("RecordArtifact: %v", err)
	}

	resp := s.do(t, http.MethodGet, "/api/v1/deliveries/"+orch.Id+"/evidence/"+artifact.Id, nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if got := resp.Header.Get("Content-Type"); got != "text/plain" {
		t.Fatalf("Content-Type = %q, want text/plain", got)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if string(data) != "evidence bytes" {
		t.Fatalf("body = %q, want %q", data, "evidence bytes")
	}
}

// TestHandleDeliveryEvidenceMismatchedOrchestrationIs404 covers the
// scoping check handleDeliveryEvidence does beyond GetArtifactRecord's
// own id lookup: an evidence id that exists, but under a different
// orchestration than the URL names, must 404 rather than leak across
// orchestrations.
func TestHandleDeliveryEvidenceMismatchedOrchestrationIs404(t *testing.T) {
	s := newDeliveryTestServer(t)
	ctx := context.Background()
	orch, err := s.store.CreateOrchestration(ctx, "orch", delivery.NewID(), nil)
	if err != nil {
		t.Fatalf("CreateOrchestration: %v", err)
	}
	other, err := s.store.CreateOrchestration(ctx, "other", delivery.NewID(), nil)
	if err != nil {
		t.Fatalf("CreateOrchestration(other): %v", err)
	}
	project, err := s.store.RegisterProject(ctx, "project", delivery.NewID(), "svc", "https://example.test/svc.git", "main")
	if err != nil {
		t.Fatalf("RegisterProject: %v", err)
	}
	hash, err := s.store.PutArtifact(ctx, []byte("data"), "text/plain")
	if err != nil {
		t.Fatalf("PutArtifact: %v", err)
	}
	artifact, err := s.store.RecordArtifact(ctx, delivery.NewID(), delivery.NewID(), delivery.ArtifactRef{
		OrchestrationID: other.Id, ProjectID: project.Id, Kind: protocol.EvidenceArtifactKindCommand,
	}, hash)
	if err != nil {
		t.Fatalf("RecordArtifact: %v", err)
	}

	resp := s.do(t, http.MethodGet, "/api/v1/deliveries/"+orch.Id+"/evidence/"+artifact.Id, nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", resp.StatusCode)
	}
}

func TestHandleDeliveryEvidenceUnknownIdIs404(t *testing.T) {
	s := newDeliveryTestServer(t)
	ctx := context.Background()
	orch, err := s.store.CreateOrchestration(ctx, "orch", delivery.NewID(), nil)
	if err != nil {
		t.Fatalf("CreateOrchestration: %v", err)
	}
	resp := s.do(t, http.MethodGet, "/api/v1/deliveries/"+orch.Id+"/evidence/unknown", nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", resp.StatusCode)
	}
}

// --- Client round-trip tests -----------------------------------------

func TestClientDeliveryRoundTrip(t *testing.T) {
	paths := testPaths(t)
	d, err := Run(context.Background(), "127.0.0.1", "0", paths)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	go d.Serve()
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		d.Shutdown(ctx)
	}()

	client, err := Discover(paths)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}

	store := delivery.NewStore(d.DB())
	ctx := context.Background()
	orch, err := store.CreateOrchestration(ctx, "orch", delivery.NewID(), []protocol.DeliveryOrchestrationUnresolvedInputsElem{{Reference: "REQ-1"}})
	if err != nil {
		t.Fatalf("CreateOrchestration: %v", err)
	}

	list, err := client.ListDeliveries(ctx)
	if err != nil {
		t.Fatalf("ListDeliveries: %v", err)
	}
	if len(list) != 1 || list[0].Id != orch.Id {
		t.Fatalf("unexpected list: %+v", list)
	}

	view, err := client.GetDeliveryView(ctx, orch.Id, 0)
	if err != nil {
		t.Fatalf("GetDeliveryView: %v", err)
	}
	if view.Orchestration.Id != orch.Id {
		t.Fatalf("unexpected view: %+v", view.Orchestration)
	}

	view, err = client.AnswerDeliveryQuestion(ctx, orch.Id, AnswerDeliveryQuestionRequest{
		Reference: "REQ-1", ExpectedRevision: orch.Revision,
		Provider: "freetext", Title: "T", Summary: "S",
	})
	if err != nil {
		t.Fatalf("AnswerDeliveryQuestion: %v", err)
	}
	if len(view.PendingQuestions) != 0 {
		t.Fatalf("expected question resolved, got %+v", view.PendingQuestions)
	}

	view, err = client.CancelDelivery(ctx, orch.Id, CancelDeliveryRequest{ExpectedRevision: view.Orchestration.Revision})
	if err != nil {
		t.Fatalf("CancelDelivery: %v", err)
	}
	if view.Orchestration.Status != protocol.DeliveryOrchestrationStatusCancelled {
		t.Fatalf("expected cancelled, got %s", view.Orchestration.Status)
	}
}

// TestClientGetDeliveryEvidenceRoundTrip covers Client.GetDeliveryEvidence,
// the one non-JSON daemon route: it must return the artifact's raw bytes
// and media type, not a decoded struct.
func TestClientGetDeliveryEvidenceRoundTrip(t *testing.T) {
	paths := testPaths(t)
	d, err := Run(context.Background(), "127.0.0.1", "0", paths)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	go d.Serve()
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		d.Shutdown(ctx)
	}()

	client, err := Discover(paths)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}

	store := delivery.NewStore(d.DB())
	ctx := context.Background()
	orch, err := store.CreateOrchestration(ctx, "orch", delivery.NewID(), nil)
	if err != nil {
		t.Fatalf("CreateOrchestration: %v", err)
	}
	project, err := store.RegisterProject(ctx, "project", delivery.NewID(), "svc", "https://example.test/svc.git", "main")
	if err != nil {
		t.Fatalf("RegisterProject: %v", err)
	}
	hash, err := store.PutArtifact(ctx, []byte("evidence bytes"), "text/plain")
	if err != nil {
		t.Fatalf("PutArtifact: %v", err)
	}
	artifact, err := store.RecordArtifact(ctx, delivery.NewID(), delivery.NewID(), delivery.ArtifactRef{
		OrchestrationID: orch.Id, ProjectID: project.Id, Kind: protocol.EvidenceArtifactKindCommand,
	}, hash)
	if err != nil {
		t.Fatalf("RecordArtifact: %v", err)
	}

	data, mediaType, err := client.GetDeliveryEvidence(ctx, orch.Id, artifact.Id)
	if err != nil {
		t.Fatalf("GetDeliveryEvidence: %v", err)
	}
	if string(data) != "evidence bytes" {
		t.Fatalf("data = %q, want %q", data, "evidence bytes")
	}
	if mediaType != "text/plain" {
		t.Fatalf("mediaType = %q, want text/plain", mediaType)
	}

	if _, _, err := client.GetDeliveryEvidence(ctx, orch.Id, "unknown"); err == nil {
		t.Fatalf("expected error for unknown evidence id")
	}
}

// TestClientMethodErrorIsStatusError proves doJSON's non-200 path surfaces
// a *StatusError carrying the daemon's own status code and error message,
// not just an opaque formatted string - a caller (e.g. a Panel HTTP
// handler) needs the status to react the same way the daemon's own
// writeDeliveryError already distinguishes 404 vs. 409 vs. 500.
func TestClientMethodErrorIsStatusError(t *testing.T) {
	paths := testPaths(t)
	d, err := Run(context.Background(), "127.0.0.1", "0", paths)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	go d.Serve()
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		d.Shutdown(ctx)
	}()

	client, err := Discover(paths)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}

	_, err = client.GetDeliveryView(context.Background(), "no-such-orchestration", 0)
	if err == nil {
		t.Fatal("expected an error for an unknown orchestration id")
	}
	var statusErr *StatusError
	if !errors.As(err, &statusErr) {
		t.Fatalf("error = %v (%T), want *StatusError", err, err)
	}
	if statusErr.Status != http.StatusNotFound {
		t.Fatalf("Status = %d, want 404", statusErr.Status)
	}
	if statusErr.Message == "" {
		t.Fatal("expected a non-empty Message")
	}
}

func TestClientApproveProjectDeliveryRoundTrip(t *testing.T) {
	paths := testPaths(t)
	d, err := Run(context.Background(), "127.0.0.1", "0", paths)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	go d.Serve()
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		d.Shutdown(ctx)
	}()

	client, err := Discover(paths)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	store := delivery.NewStore(d.DB())
	orchID, manifestID := newApprovableFixture(t, store)

	view, err := client.ApproveProjectDelivery(context.Background(), orchID, ApproveProjectDeliveryRequest{ManifestId: manifestID, ApprovedBy: "human-1"})
	if err != nil {
		t.Fatalf("ApproveProjectDelivery: %v", err)
	}
	if len(view.PendingApprovals) != 0 {
		t.Fatalf("expected manifest resolved, got %+v", view.PendingApprovals)
	}
}

func TestClientSubscribeDeliveryViewObservesUpdate(t *testing.T) {
	paths := testPaths(t)
	d, err := Run(context.Background(), "127.0.0.1", "0", paths)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	go d.Serve()
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		d.Shutdown(ctx)
	}()

	client, err := Discover(paths)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	store := delivery.NewStore(d.DB())
	orch, err := store.CreateOrchestration(context.Background(), "orch", delivery.NewID(), nil)
	if err != nil {
		t.Fatalf("CreateOrchestration: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	updates := make(chan int, 1)
	go client.SubscribeDeliveryView(ctx, orch.Id, 0, func(v *delivery.DeliveryView) bool {
		updates <- v.LatestSeq
		return false
	})

	time.Sleep(300 * time.Millisecond)
	if _, err := store.RegisterInput(context.Background(), "reg", orch.Id, orch.Revision, protocol.DeliveryOrchestrationUnresolvedInputsElem{Reference: "R"}); err != nil {
		t.Fatalf("RegisterInput: %v", err)
	}

	select {
	case seq := <-updates:
		if seq <= 0 {
			t.Fatalf("expected latest_seq > 0 after the update, got %d", seq)
		}
	case <-time.After(4 * time.Second):
		t.Fatal("SubscribeDeliveryView did not observe the update in time")
	}
}
