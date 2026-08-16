package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/ygrip/punakawan/internal/daemon"
	"github.com/ygrip/punakawan/internal/panel/contract"
)

// errDeliveryUnavailable is returned when this panel instance never
// managed to connect to the daemon at startup (contract.DeliveryReader is
// nil) - a 503, not a 500, since the panel itself is healthy and every
// other endpoint still works.
var errDeliveryUnavailable = errors.New("delivery data is unavailable: the panel could not connect to the daemon")

// deliveryErrorStatus maps a daemon.StatusError (doJSON's error for a
// non-200 daemon response) to the same HTTP status the daemon itself
// answered with - a 404 orchestration lookup or a 409 revision
// conflict/invalid state transition should reach the panel's own caller
// unchanged, not collapse into a generic 500. Any other error (a transport
// failure reaching the daemon at all) is a 500.
func deliveryErrorStatus(err error) int {
	var se *daemon.StatusError
	if errors.As(err, &se) {
		return se.Status
	}
	return http.StatusInternalServerError
}

// ListDeliveriesHandler serves GET /api/v1/deliveries.
func ListDeliveriesHandler(reader contract.DeliveryReader) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if reader == nil {
			writeError(w, http.StatusServiceUnavailable, errDeliveryUnavailable)
			return
		}
		list, err := reader.ListDeliveries(r.Context())
		if err != nil {
			writeError(w, deliveryErrorStatus(err), err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": list})
	}
}

// DeliveryViewHandler serves GET /api/v1/deliveries/{orchestrationId},
// optionally passing ?since_seq= through to the daemon per
// delivery.BuildDeliveryViewSince's diffing (an invalid/absent value
// silently falls back to 0, matching this package's existing query-param
// handling elsewhere, e.g. task_handler.go's limit).
func DeliveryViewHandler(reader contract.DeliveryReader) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if reader == nil {
			writeError(w, http.StatusServiceUnavailable, errDeliveryUnavailable)
			return
		}
		sinceSeq, _ := strconv.Atoi(r.URL.Query().Get("since_seq"))
		view, err := reader.GetDeliveryView(r.Context(), r.PathValue("orchestrationId"), sinceSeq)
		if err != nil {
			writeError(w, deliveryErrorStatus(err), err)
			return
		}
		writeJSON(w, http.StatusOK, view)
	}
}

// DeliveryEvidenceHandler serves
// GET /api/v1/deliveries/{orchestrationId}/evidence/{evidenceId}: the raw
// bytes of one lane-scoped evidence artifact, at whatever media type it
// was recorded with - mirroring EvidencePreviewHandler's binary path
// (evidence_handler.go), since delivery evidence has no text-preview
// shape of its own.
func DeliveryEvidenceHandler(reader contract.DeliveryReader) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if reader == nil {
			writeError(w, http.StatusServiceUnavailable, errDeliveryUnavailable)
			return
		}
		data, mediaType, err := reader.GetDeliveryEvidence(r.Context(), r.PathValue("orchestrationId"), r.PathValue("evidenceId"))
		if err != nil {
			writeError(w, deliveryErrorStatus(err), err)
			return
		}
		w.Header().Set("Content-Type", mediaType)
		w.Header().Set("Content-Length", strconv.Itoa(len(data)))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(data)
	}
}

// answerDeliveryQuestionBody is POST .../answer-question's request body,
// mirroring daemon's own wire shape exactly (the unexported
// answerDeliveryQuestionRequest in internal/daemon/delivery.go): set
// provider for the resolved-requirement case, or both parent_task_id and
// project_id for the ambiguous-routing case.
type answerDeliveryQuestionBody struct {
	Reference        string `json:"reference"`
	ExpectedRevision int    `json:"expected_revision,omitempty"`

	Provider   string `json:"provider,omitempty"`
	ExternalId string `json:"external_id,omitempty"`
	Url        string `json:"url,omitempty"`
	Title      string `json:"title,omitempty"`
	Summary    string `json:"summary,omitempty"`

	ParentTaskId string `json:"parent_task_id,omitempty"`
	ProjectId    string `json:"project_id,omitempty"`
}

// AnswerDeliveryQuestionHandler serves
// POST /api/v1/deliveries/{orchestrationId}/answer-question.
func AnswerDeliveryQuestionHandler(reader contract.DeliveryReader) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if reader == nil {
			writeError(w, http.StatusServiceUnavailable, errDeliveryUnavailable)
			return
		}
		var body answerDeliveryQuestionBody
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		in := daemon.AnswerDeliveryQuestionRequest{
			Reference: body.Reference, ExpectedRevision: body.ExpectedRevision,
			Provider: body.Provider, ExternalId: body.ExternalId, Url: body.Url,
			Title: body.Title, Summary: body.Summary,
			ParentTaskId: body.ParentTaskId, ProjectId: body.ProjectId,
		}
		view, err := reader.AnswerDeliveryQuestion(r.Context(), r.PathValue("orchestrationId"), in)
		if err != nil {
			writeError(w, deliveryErrorStatus(err), err)
			return
		}
		writeJSON(w, http.StatusOK, view)
	}
}

// approveProjectDeliveryBody is POST .../approve's request body, mirroring
// daemon's unexported approveProjectDeliveryRequest.
type approveProjectDeliveryBody struct {
	ManifestId string `json:"manifest_id"`
	ApprovedBy string `json:"approved_by"`
	Reject     bool   `json:"reject,omitempty"`
}

// ApproveProjectDeliveryHandler serves
// POST /api/v1/deliveries/{orchestrationId}/approve.
func ApproveProjectDeliveryHandler(reader contract.DeliveryReader) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if reader == nil {
			writeError(w, http.StatusServiceUnavailable, errDeliveryUnavailable)
			return
		}
		var body approveProjectDeliveryBody
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		in := daemon.ApproveProjectDeliveryRequest{ManifestId: body.ManifestId, ApprovedBy: body.ApprovedBy, Reject: body.Reject}
		view, err := reader.ApproveProjectDelivery(r.Context(), r.PathValue("orchestrationId"), in)
		if err != nil {
			writeError(w, deliveryErrorStatus(err), err)
			return
		}
		writeJSON(w, http.StatusOK, view)
	}
}

// cancelDeliveryBody is POST .../cancel's request body, mirroring daemon's
// unexported cancelDeliveryRequest.
type cancelDeliveryBody struct {
	ExpectedRevision int    `json:"expected_revision"`
	Reason           string `json:"reason,omitempty"`
}

// CancelDeliveryHandler serves POST /api/v1/deliveries/{orchestrationId}/cancel.
func CancelDeliveryHandler(reader contract.DeliveryReader) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if reader == nil {
			writeError(w, http.StatusServiceUnavailable, errDeliveryUnavailable)
			return
		}
		var body cancelDeliveryBody
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		in := daemon.CancelDeliveryRequest{ExpectedRevision: body.ExpectedRevision, Reason: body.Reason}
		view, err := reader.CancelDelivery(r.Context(), r.PathValue("orchestrationId"), in)
		if err != nil {
			writeError(w, deliveryErrorStatus(err), err)
			return
		}
		writeJSON(w, http.StatusOK, view)
	}
}
