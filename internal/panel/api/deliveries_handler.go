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
		result, err := reader.ListDeliveries(r.Context())
		if err != nil {
			writeError(w, deliveryErrorStatus(err), err)
			return
		}
		writeJSON(w, http.StatusOK, result)
	}
}

// DeliveryDetailHandler serves GET /api/v1/deliveries/{orchestrationId}.
func DeliveryDetailHandler(reader contract.DeliveryReader) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if reader == nil {
			writeError(w, http.StatusServiceUnavailable, errDeliveryUnavailable)
			return
		}
		detail, err := reader.GetDeliveryDetail(r.Context(), r.PathValue("orchestrationId"))
		if err != nil {
			writeError(w, deliveryErrorStatus(err), err)
			return
		}
		writeJSON(w, http.StatusOK, detail)
	}
}

// DeliveryWatchHandler serves
// GET /api/v1/deliveries/{orchestrationId}/watch?since_revision=N&wait_seconds=25,
// forwarding straight to the daemon's own long-poll (an invalid/absent
// since_revision or wait_seconds silently falls back to 0, matching this
// package's existing query-param handling elsewhere, e.g. task_handler.go's
// limit).
func DeliveryWatchHandler(reader contract.DeliveryReader) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if reader == nil {
			writeError(w, http.StatusServiceUnavailable, errDeliveryUnavailable)
			return
		}
		sinceRevision, _ := strconv.Atoi(r.URL.Query().Get("since_revision"))
		waitSeconds, _ := strconv.Atoi(r.URL.Query().Get("wait_seconds"))
		detail, err := reader.WatchDeliveryDetail(r.Context(), r.PathValue("orchestrationId"), sinceRevision, waitSeconds)
		if err != nil {
			writeError(w, deliveryErrorStatus(err), err)
			return
		}
		writeJSON(w, http.StatusOK, detail)
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
		detail, err := reader.CancelDelivery(r.Context(), r.PathValue("orchestrationId"), in)
		if err != nil {
			writeError(w, deliveryErrorStatus(err), err)
			return
		}
		writeJSON(w, http.StatusOK, detail)
	}
}
