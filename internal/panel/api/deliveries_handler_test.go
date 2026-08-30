package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ygrip/punakawan/internal/daemon"
	"github.com/ygrip/punakawan/internal/deliveryprojection"
)

// fakeDeliveryReader is a scriptable contract.DeliveryReader stand-in: each
// method returns whatever the test pre-loaded, and records the last input
// it was called with so a test can assert the handler translated the HTTP
// request correctly.
type fakeDeliveryReader struct {
	list      daemon.ListDeliveriesResult
	detail    *deliveryprojection.DeliveryDetail
	err       error
	lastID    string
	lastSince int
	lastWait  int
	lastCanc  daemon.CancelDeliveryRequest

	evidenceData []byte
	evidenceType string
	lastEvidence string
}

func (f *fakeDeliveryReader) ListDeliveries(ctx context.Context) (daemon.ListDeliveriesResult, error) {
	return f.list, f.err
}

func (f *fakeDeliveryReader) GetDeliveryDetail(ctx context.Context, orchestrationID string) (*deliveryprojection.DeliveryDetail, error) {
	f.lastID = orchestrationID
	return f.detail, f.err
}

func (f *fakeDeliveryReader) WatchDeliveryDetail(ctx context.Context, orchestrationID string, sinceRevision, waitSeconds int) (*deliveryprojection.DeliveryDetail, error) {
	f.lastID, f.lastSince, f.lastWait = orchestrationID, sinceRevision, waitSeconds
	return f.detail, f.err
}

func (f *fakeDeliveryReader) CancelDelivery(ctx context.Context, orchestrationID string, in daemon.CancelDeliveryRequest) (*deliveryprojection.DeliveryDetail, error) {
	f.lastID, f.lastCanc = orchestrationID, in
	return f.detail, f.err
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
	reader := &fakeDeliveryReader{list: daemon.ListDeliveriesResult{
		Items: []deliveryprojection.DeliverySummary{
			{ID: "orc-1", Status: deliveryprojection.StatusActive},
			{ID: "orc-2", Status: deliveryprojection.StatusPending},
		},
		SnapshotRevision: 3,
	}}
	rec := doDelivery(http.MethodGet, "/api/v1/deliveries", "", "", ListDeliveriesHandler(reader))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	var body daemon.ListDeliveriesResult
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Items) != 2 || body.SnapshotRevision != 3 {
		t.Fatalf("body = %+v, want 2 items and snapshot_revision 3", body)
	}
}

func TestListDeliveriesHandlerNilReaderIs503(t *testing.T) {
	rec := doDelivery(http.MethodGet, "/api/v1/deliveries", "", "", ListDeliveriesHandler(nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
}

func TestDeliveryDetailHandlerReturnsDetail(t *testing.T) {
	reader := &fakeDeliveryReader{detail: &deliveryprojection.DeliveryDetail{DeliverySummary: deliveryprojection.DeliverySummary{ID: "orc-1", ProjectionRevision: 7}}}
	rec := doDelivery(http.MethodGet, "/api/v1/deliveries/orc-1", "orc-1", "", DeliveryDetailHandler(reader))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if reader.lastID != "orc-1" {
		t.Fatalf("reader called with %q, want orc-1", reader.lastID)
	}
	var got deliveryprojection.DeliveryDetail
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.ProjectionRevision != 7 {
		t.Fatalf("ProjectionRevision = %d, want 7", got.ProjectionRevision)
	}
}

func TestDeliveryDetailHandlerMapsNotFoundStatus(t *testing.T) {
	reader := &fakeDeliveryReader{err: &daemon.StatusError{Status: http.StatusNotFound, Message: "no such orchestration"}}
	rec := doDelivery(http.MethodGet, "/api/v1/deliveries/orc-missing", "orc-missing", "", DeliveryDetailHandler(reader))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestDeliveryDetailHandlerMapsConflictStatus(t *testing.T) {
	reader := &fakeDeliveryReader{err: &daemon.StatusError{Status: http.StatusConflict, Message: "revision conflict"}}
	rec := doDelivery(http.MethodGet, "/api/v1/deliveries/orc-1", "orc-1", "", DeliveryDetailHandler(reader))
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409", rec.Code)
	}
}

func TestDeliveryDetailHandlerUnstructuredErrorIs500(t *testing.T) {
	reader := &fakeDeliveryReader{err: context.DeadlineExceeded}
	rec := doDelivery(http.MethodGet, "/api/v1/deliveries/orc-1", "orc-1", "", DeliveryDetailHandler(reader))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
}

func TestDeliveryWatchHandlerPassesThroughSinceRevisionAndWaitSeconds(t *testing.T) {
	reader := &fakeDeliveryReader{detail: &deliveryprojection.DeliveryDetail{DeliverySummary: deliveryprojection.DeliverySummary{ID: "orc-1", ProjectionRevision: 9}}}
	rec := doDelivery(http.MethodGet, "/api/v1/deliveries/orc-1/watch?since_revision=3&wait_seconds=5", "orc-1", "", DeliveryWatchHandler(reader))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if reader.lastID != "orc-1" || reader.lastSince != 3 || reader.lastWait != 5 {
		t.Fatalf("reader called with (%q, %d, %d), want (orc-1, 3, 5)", reader.lastID, reader.lastSince, reader.lastWait)
	}
}

func TestDeliveryWatchHandlerNilReaderIs503(t *testing.T) {
	rec := doDelivery(http.MethodGet, "/api/v1/deliveries/orc-1/watch", "orc-1", "", DeliveryWatchHandler(nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
}

func TestCancelDeliveryHandler(t *testing.T) {
	reader := &fakeDeliveryReader{detail: &deliveryprojection.DeliveryDetail{}}
	body := `{"expected_revision":4,"reason":"no longer needed"}`
	rec := doDelivery(http.MethodPost, "/api/v1/deliveries/orc-1/cancel", "orc-1", body, CancelDeliveryHandler(reader))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if reader.lastCanc.ExpectedRevision != 4 || reader.lastCanc.Reason != "no longer needed" {
		t.Fatalf("request = %+v, want expected_revision=4 reason set", reader.lastCanc)
	}
}

func TestCancelDeliveryHandlerInvalidBodyIs400(t *testing.T) {
	rec := doDelivery(http.MethodPost, "/api/v1/deliveries/orc-1/cancel", "orc-1", "not-json", CancelDeliveryHandler(&fakeDeliveryReader{}))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
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
