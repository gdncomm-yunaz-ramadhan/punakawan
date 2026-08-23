package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ygrip/punakawan/internal/daemon"
	"github.com/ygrip/punakawan/internal/delivery"
	"github.com/ygrip/punakawan/pkg/protocol"
)

// fakeDeliveryReader is a scriptable contract.DeliveryReader stand-in: each
// method returns whatever the test pre-loaded, and records the last input
// it was called with so a test can assert the handler translated the HTTP
// request correctly.
type fakeDeliveryReader struct {
	list     []*protocol.DeliveryOrchestration
	view     *delivery.DeliveryView
	err      error
	lastID   string
	lastSeq  int
	lastAns  daemon.AnswerDeliveryQuestionRequest
	lastAppr daemon.ApproveProjectDeliveryRequest
	lastCanc daemon.CancelDeliveryRequest

	evidenceData []byte
	evidenceType string
	lastEvidence string
}

func (f *fakeDeliveryReader) ListDeliveries(ctx context.Context) ([]*protocol.DeliveryOrchestration, error) {
	return f.list, f.err
}

func (f *fakeDeliveryReader) GetDeliveryView(ctx context.Context, orchestrationID string, sinceSeq int) (*delivery.DeliveryView, error) {
	f.lastID, f.lastSeq = orchestrationID, sinceSeq
	return f.view, f.err
}

func (f *fakeDeliveryReader) WatchDeliveryView(ctx context.Context, orchestrationID string, sinceSeq, waitSeconds int) (*delivery.DeliveryView, error) {
	return f.view, f.err
}

func (f *fakeDeliveryReader) AnswerDeliveryQuestion(ctx context.Context, orchestrationID string, in daemon.AnswerDeliveryQuestionRequest) (*delivery.DeliveryView, error) {
	f.lastID, f.lastAns = orchestrationID, in
	return f.view, f.err
}

func (f *fakeDeliveryReader) ApproveProjectDelivery(ctx context.Context, orchestrationID string, in daemon.ApproveProjectDeliveryRequest) (*delivery.DeliveryView, error) {
	f.lastID, f.lastAppr = orchestrationID, in
	return f.view, f.err
}

func (f *fakeDeliveryReader) CancelDelivery(ctx context.Context, orchestrationID string, in daemon.CancelDeliveryRequest) (*delivery.DeliveryView, error) {
	f.lastID, f.lastCanc = orchestrationID, in
	return f.view, f.err
}

func (f *fakeDeliveryReader) GetDeliveryEvidence(ctx context.Context, orchestrationID, evidenceID string) ([]byte, string, error) {
	f.lastID, f.lastEvidence = orchestrationID, evidenceID
	return f.evidenceData, f.evidenceType, f.err
}

func doDelivery(method, target, orchestrationID, body string, h http.HandlerFunc) *httptest.ResponseRecorder {
	var r *http.Request
	if body == "" {
		r = httptest.NewRequest(method, target, nil)
	} else {
		r = httptest.NewRequest(method, target, strings.NewReader(body))
	}
	if orchestrationID != "" {
		r.SetPathValue("orchestrationId", orchestrationID)
	}
	rec := httptest.NewRecorder()
	h(rec, r)
	return rec
}

func TestListDeliveriesHandlerReturnsItems(t *testing.T) {
	reader := &fakeDeliveryReader{list: []*protocol.DeliveryOrchestration{
		{Id: "orc-1", Status: protocol.DeliveryOrchestrationStatusActive},
		{Id: "orc-2", Status: protocol.DeliveryOrchestrationStatusPending},
	}}
	rec := doDelivery(http.MethodGet, "/api/v1/deliveries", "", "", ListDeliveriesHandler(reader))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Items []protocol.DeliveryOrchestration `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Items) != 2 {
		t.Fatalf("items = %+v, want 2", body.Items)
	}
}

func TestListDeliveriesHandlerNilReaderIs503(t *testing.T) {
	rec := doDelivery(http.MethodGet, "/api/v1/deliveries", "", "", ListDeliveriesHandler(nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
}

func TestDeliveryViewHandlerPassesThroughSinceSeq(t *testing.T) {
	reader := &fakeDeliveryReader{view: &delivery.DeliveryView{LatestSeq: 7}}
	rec := doDelivery(http.MethodGet, "/api/v1/deliveries/orc-1?since_seq=3", "orc-1", "", DeliveryViewHandler(reader))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if reader.lastID != "orc-1" || reader.lastSeq != 3 {
		t.Fatalf("reader called with (%q, %d), want (orc-1, 3)", reader.lastID, reader.lastSeq)
	}
	var got delivery.DeliveryView
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.LatestSeq != 7 {
		t.Fatalf("LatestSeq = %d, want 7", got.LatestSeq)
	}
}

func TestDeliveryViewHandlerMapsNotFoundStatus(t *testing.T) {
	reader := &fakeDeliveryReader{err: &daemon.StatusError{Status: http.StatusNotFound, Message: "no such orchestration"}}
	rec := doDelivery(http.MethodGet, "/api/v1/deliveries/orc-missing", "orc-missing", "", DeliveryViewHandler(reader))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestDeliveryViewHandlerMapsConflictStatus(t *testing.T) {
	reader := &fakeDeliveryReader{err: &daemon.StatusError{Status: http.StatusConflict, Message: "revision conflict"}}
	rec := doDelivery(http.MethodGet, "/api/v1/deliveries/orc-1", "orc-1", "", DeliveryViewHandler(reader))
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409", rec.Code)
	}
}

func TestDeliveryViewHandlerUnstructuredErrorIs500(t *testing.T) {
	reader := &fakeDeliveryReader{err: context.DeadlineExceeded}
	rec := doDelivery(http.MethodGet, "/api/v1/deliveries/orc-1", "orc-1", "", DeliveryViewHandler(reader))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
}

func TestAnswerDeliveryQuestionHandlerResolvedRequirement(t *testing.T) {
	reader := &fakeDeliveryReader{view: &delivery.DeliveryView{LatestSeq: 1}}
	body := `{"reference":"ref-1","provider":"jira","external_id":"PROJ-1","expected_revision":2}`
	rec := doDelivery(http.MethodPost, "/api/v1/deliveries/orc-1/answer-question", "orc-1", body, AnswerDeliveryQuestionHandler(reader))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if reader.lastID != "orc-1" {
		t.Fatalf("orchestration id = %q, want orc-1", reader.lastID)
	}
	if reader.lastAns.Provider != "jira" || reader.lastAns.ExternalId != "PROJ-1" || reader.lastAns.ExpectedRevision != 2 {
		t.Fatalf("request = %+v, want provider=jira external_id=PROJ-1 expected_revision=2", reader.lastAns)
	}
}

func TestAnswerDeliveryQuestionHandlerRoutingCase(t *testing.T) {
	reader := &fakeDeliveryReader{view: &delivery.DeliveryView{}}
	body := `{"reference":"ref-1","parent_task_id":"task-1","project_id":"proj-1"}`
	rec := doDelivery(http.MethodPost, "/api/v1/deliveries/orc-1/answer-question", "orc-1", body, AnswerDeliveryQuestionHandler(reader))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if reader.lastAns.ParentTaskId != "task-1" || reader.lastAns.ProjectId != "proj-1" {
		t.Fatalf("request = %+v, want parent_task_id=task-1 project_id=proj-1", reader.lastAns)
	}
}

func TestAnswerDeliveryQuestionHandlerInvalidBodyIs400(t *testing.T) {
	rec := doDelivery(http.MethodPost, "/api/v1/deliveries/orc-1/answer-question", "orc-1", "not-json", AnswerDeliveryQuestionHandler(&fakeDeliveryReader{}))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestApproveProjectDeliveryHandlerApprove(t *testing.T) {
	reader := &fakeDeliveryReader{view: &delivery.DeliveryView{}}
	body := `{"manifest_id":"man-1","approved_by":"alice"}`
	rec := doDelivery(http.MethodPost, "/api/v1/deliveries/orc-1/approve", "orc-1", body, ApproveProjectDeliveryHandler(reader))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if reader.lastAppr.ManifestId != "man-1" || reader.lastAppr.ApprovedBy != "alice" || reader.lastAppr.Reject {
		t.Fatalf("request = %+v, want manifest_id=man-1 approved_by=alice reject=false", reader.lastAppr)
	}
}

func TestApproveProjectDeliveryHandlerReject(t *testing.T) {
	reader := &fakeDeliveryReader{view: &delivery.DeliveryView{}}
	body := `{"manifest_id":"man-1","approved_by":"alice","reject":true}`
	rec := doDelivery(http.MethodPost, "/api/v1/deliveries/orc-1/approve", "orc-1", body, ApproveProjectDeliveryHandler(reader))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if !reader.lastAppr.Reject {
		t.Fatalf("request = %+v, want reject=true", reader.lastAppr)
	}
}

func TestCancelDeliveryHandler(t *testing.T) {
	reader := &fakeDeliveryReader{view: &delivery.DeliveryView{}}
	body := `{"expected_revision":4,"reason":"no longer needed"}`
	rec := doDelivery(http.MethodPost, "/api/v1/deliveries/orc-1/cancel", "orc-1", body, CancelDeliveryHandler(reader))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if reader.lastCanc.ExpectedRevision != 4 || reader.lastCanc.Reason != "no longer needed" {
		t.Fatalf("request = %+v, want expected_revision=4 reason set", reader.lastCanc)
	}
}

func TestCancelDeliveryHandlerNilReaderIs503(t *testing.T) {
	rec := doDelivery(http.MethodPost, "/api/v1/deliveries/orc-1/cancel", "orc-1", `{"expected_revision":1}`, CancelDeliveryHandler(nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
}

func doDeliveryEvidence(orchestrationID, evidenceID string, h http.HandlerFunc) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodGet, "/api/v1/deliveries/"+orchestrationID+"/evidence/"+evidenceID, nil)
	r.SetPathValue("orchestrationId", orchestrationID)
	r.SetPathValue("evidenceId", evidenceID)
	rec := httptest.NewRecorder()
	h(rec, r)
	return rec
}

func TestDeliveryEvidenceHandlerServesRawBytes(t *testing.T) {
	reader := &fakeDeliveryReader{evidenceData: []byte("screenshot bytes"), evidenceType: "image/png"}
	rec := doDeliveryEvidence("orc-1", "ev-1", DeliveryEvidenceHandler(reader))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Content-Type"); got != "image/png" {
		t.Fatalf("Content-Type = %q, want image/png", got)
	}
	if rec.Body.String() != "screenshot bytes" {
		t.Fatalf("body = %q, want screenshot bytes", rec.Body.String())
	}
	if reader.lastID != "orc-1" || reader.lastEvidence != "ev-1" {
		t.Fatalf("reader called with (%q, %q), want (orc-1, ev-1)", reader.lastID, reader.lastEvidence)
	}
}

func TestDeliveryEvidenceHandlerMapsNotFoundStatus(t *testing.T) {
	reader := &fakeDeliveryReader{err: &daemon.StatusError{Status: http.StatusNotFound, Message: "no such evidence"}}
	rec := doDeliveryEvidence("orc-1", "ev-missing", DeliveryEvidenceHandler(reader))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestDeliveryEvidenceHandlerNilReaderIs503(t *testing.T) {
	rec := doDeliveryEvidence("orc-1", "ev-1", DeliveryEvidenceHandler(nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
}
