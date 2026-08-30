// delivery.go serves the delivery-domain HTTP routes over the daemon's
// own delivery.Store and internal/deliveryprojection.Projector (see
// daemon.go's Run) - the daemon-native path to the one panel-facing
// delivery projection every consumer (panel, CLI) reaches through
// Client below. List and detail always report the same
// delivery_projection_versions revision, and neither route ever exposes
// scheduler-internal concepts (lanes, blocked counts, pending questions,
// a lane-derived next action).
package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/ygrip/punakawan/internal/delivery"
	"github.com/ygrip/punakawan/internal/deliveryprojection"
)

// maxDeliveryWaitSeconds bounds how long a single watch request will
// long-poll for a change before returning whatever the projector
// currently has - long enough to avoid most callers busy-polling, short
// enough that a request never outlives a normal HTTP client timeout by
// much.
const maxDeliveryWaitSeconds = 30

// deliveryPollInterval is how often handleDeliveryWatch re-checks the
// projector while a wait_seconds request is blocked waiting for
// ProjectionRevision to advance.
const deliveryPollInterval = 250 * time.Millisecond

// handleListDeliveries serves GET /api/v1/deliveries: every delivery's
// compact summary, plus the highest projection revision among them so a
// caller has something to compare a later list response against.
func (t *Transport) handleListDeliveries(w http.ResponseWriter, r *http.Request) {
	items, err := t.projector.ListSummaries(r.Context(), deliveryprojection.ListFilter{})
	if err != nil {
		writeDeliveryError(w, err)
		return
	}
	snapshotRevision := 0
	for _, item := range items {
		if item.ProjectionRevision > snapshotRevision {
			snapshotRevision = item.ProjectionRevision
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "snapshot_revision": snapshotRevision})
}

// handleDeliveryDetail serves GET /api/v1/deliveries/{orchestrationId}:
// orchestrationId's current DeliveryDetail, returned immediately.
func (t *Transport) handleDeliveryDetail(w http.ResponseWriter, r *http.Request) {
	detail, err := t.projector.GetDetail(r.Context(), r.PathValue("orchestrationId"))
	if err != nil {
		writeDeliveryError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, detail)
}

// handleDeliveryWatch serves
// GET /api/v1/deliveries/{orchestrationId}/watch?since_revision=N&wait_seconds=25:
// it long-polls, re-checking the projector every deliveryPollInterval,
// until either ProjectionRevision has advanced past since_revision or
// wait_seconds elapses, then returns the current DeliveryDetail either
// way - the daemon's push-style update mechanism, reusing GetDetail's own
// projection_revision rather than a separate streaming subsystem.
// Callers that want ongoing updates call this in a loop with
// since_revision set to the previous response's projection_revision (see
// Client's WatchDeliveryDetail/SubscribeDeliveryDetail).
func (t *Transport) handleDeliveryWatch(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("orchestrationId")
	sinceRevision, err := queryInt(r, "since_revision", 0)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	waitSeconds, err := queryInt(r, "wait_seconds", deliveryWatchWaitSeconds)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if waitSeconds > maxDeliveryWaitSeconds {
		waitSeconds = maxDeliveryWaitSeconds
	}

	deadline := time.Now().Add(time.Duration(waitSeconds) * time.Second)
	for {
		detail, err := t.projector.GetDetail(r.Context(), id)
		if err != nil {
			writeDeliveryError(w, err)
			return
		}
		if waitSeconds == 0 || detail.ProjectionRevision != sinceRevision || !time.Now().Before(deadline) {
			writeJSON(w, http.StatusOK, detail)
			return
		}
		select {
		case <-r.Context().Done():
			return
		case <-time.After(deliveryPollInterval):
		}
	}
}

// handleDeliveryEvidence serves
// GET /api/v1/deliveries/{orchestrationId}/evidence/{evidenceId}: the raw
// bytes of one lane-scoped evidence artifact recorded via
// delivery.Store.RecordArtifact, at whatever media type it was stored
// with. There is no JSON metadata shape here - DeliveryDetail's Activity
// timeline and Sessions already describe the delivery's own history; this
// route exists only to serve the bytes a link built from that metadata
// points at.
func (t *Transport) handleDeliveryEvidence(w http.ResponseWriter, r *http.Request) {
	orchestrationID := r.PathValue("orchestrationId")
	rec, err := t.delivery.GetArtifactRecord(r.Context(), r.PathValue("evidenceId"))
	if err != nil {
		writeDeliveryError(w, err)
		return
	}
	if rec.OrchestrationId != orchestrationID {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "delivery: evidence not found for this orchestration"})
		return
	}
	data, err := t.delivery.GetArtifact(rec.ContentHash)
	if err != nil {
		writeDeliveryError(w, err)
		return
	}
	w.Header().Set("Content-Type", rec.MediaType)
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

// cancelDeliveryRequest is POST /api/v1/deliveries/{orchestrationId}/cancel's
// body, mirroring mcpserver.CancelDeliveryInput minus orchestration_id
// (from the URL path). expected_revision is the orchestration's own
// event-log revision (DeliveryDetail.orchestration_revision), the same
// optimistic-concurrency counter cancel/complete have always checked -
// not projection_revision, which is a different, projection-only counter.
type cancelDeliveryRequest struct {
	ExpectedRevision int    `json:"expected_revision"`
	Reason           string `json:"reason,omitempty"`
}

func (t *Transport) handleCancelDelivery(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("orchestrationId")
	var body cancelDeliveryRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	if _, err := t.delivery.CancelOrchestration(r.Context(), delivery.NewID(), id, body.ExpectedRevision); err != nil {
		writeDeliveryError(w, err)
		return
	}

	detail, err := t.projector.GetDetail(r.Context(), id)
	if err != nil {
		writeDeliveryError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, detail)
}

// writeDeliveryError maps an internal/delivery sentinel error to the HTTP
// status a caller should react to: a missing id is 404, a conflicting
// expected-revision or invalid state transition is 409 (the caller's own
// view is stale - it should refetch, not blindly retry the same
// request), everything else is 500.
func writeDeliveryError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, delivery.ErrNotFound):
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
	case errors.Is(err, delivery.ErrRevisionConflict), errors.Is(err, delivery.ErrInvalidState):
		writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
	default:
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}
}

// queryInt parses an optional non-negative integer query parameter,
// returning def when it is absent.
func queryInt(r *http.Request, name string, def int) (int, error) {
	raw := r.URL.Query().Get(name)
	if raw == "" {
		return def, nil
	}
	v, err := strconv.Atoi(raw)
	if err != nil || v < 0 {
		return 0, errors.New("daemon: invalid " + name + " query parameter")
	}
	return v, nil
}

// --- Client methods -------------------------------------------------
//
// Everything below is the Client-side counterpart of this file's HTTP
// routes: Panel and any other daemon caller should reach delivery data
// through these instead of hand-building requests to the paths above.

// deliveryWatchWaitSeconds is how long WatchDeliveryDetail asks the
// daemon to hold a request open per call; SubscribeDeliveryDetail loops
// this to give callers an ongoing stream of updates without either side
// needing a persistent streaming connection.
const deliveryWatchWaitSeconds = 25

// deliveryWatchTimeoutSlack bounds watchHTTP's request timeout beyond
// deliveryWatchWaitSeconds, covering network latency and the daemon's
// own poll-interval overshoot so a slow-but-healthy round trip is never
// mistaken for a hang.
const deliveryWatchTimeoutSlack = 10 * time.Second

// ListDeliveriesResult is ListDeliveries' result: every delivery's
// compact summary, plus the highest projection revision among them.
type ListDeliveriesResult struct {
	Items            []deliveryprojection.DeliverySummary `json:"items"`
	SnapshotRevision int                                  `json:"snapshot_revision"`
}

// ListDeliveries returns every delivery's compact summary.
func (c *Client) ListDeliveries(ctx context.Context) (ListDeliveriesResult, error) {
	var out ListDeliveriesResult
	if err := c.doJSON(ctx, c.http, http.MethodGet, "/api/v1/deliveries", nil, &out); err != nil {
		return ListDeliveriesResult{}, err
	}
	return out, nil
}

// GetDeliveryDetail fetches orchestrationID's current DeliveryDetail
// immediately.
func (c *Client) GetDeliveryDetail(ctx context.Context, orchestrationID string) (*deliveryprojection.DeliveryDetail, error) {
	var detail deliveryprojection.DeliveryDetail
	if err := c.doJSON(ctx, c.http, http.MethodGet, "/api/v1/deliveries/"+url.PathEscape(orchestrationID), nil, &detail); err != nil {
		return nil, err
	}
	return &detail, nil
}

// WatchDeliveryDetail is GetDeliveryDetail, except the daemon blocks
// server-side for up to waitSeconds waiting for ProjectionRevision to
// advance past sinceRevision before responding - the daemon clamps
// waitSeconds to its own maxDeliveryWaitSeconds. It returns whatever the
// daemon has once its wait window elapses, whether or not anything
// actually changed; callers compare the returned ProjectionRevision to
// sinceRevision themselves before calling again, or use
// SubscribeDeliveryDetail to do that in a loop.
func (c *Client) WatchDeliveryDetail(ctx context.Context, orchestrationID string, sinceRevision, waitSeconds int) (*deliveryprojection.DeliveryDetail, error) {
	ctx, cancel := context.WithTimeout(ctx, time.Duration(waitSeconds)*time.Second+deliveryWatchTimeoutSlack)
	defer cancel()
	q := url.Values{}
	q.Set("since_revision", strconv.Itoa(sinceRevision))
	q.Set("wait_seconds", strconv.Itoa(waitSeconds))
	path := "/api/v1/deliveries/" + url.PathEscape(orchestrationID) + "/watch?" + q.Encode()
	var detail deliveryprojection.DeliveryDetail
	if err := c.doJSON(ctx, c.watchHTTP, http.MethodGet, path, nil, &detail); err != nil {
		return nil, err
	}
	return &detail, nil
}

// SubscribeDeliveryDetail is the daemon's push-style update mechanism
// from a caller's perspective: it long-polls via WatchDeliveryDetail in a
// loop, calling onUpdate every time ProjectionRevision advances past
// sinceRevision, until ctx is cancelled or onUpdate returns false. It
// never returns an error for a cancelled ctx - that is the normal way a
// caller stops watching.
func (c *Client) SubscribeDeliveryDetail(ctx context.Context, orchestrationID string, sinceRevision int, onUpdate func(*deliveryprojection.DeliveryDetail) bool) error {
	for {
		detail, err := c.WatchDeliveryDetail(ctx, orchestrationID, sinceRevision, deliveryWatchWaitSeconds)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		if detail.ProjectionRevision != sinceRevision {
			sinceRevision = detail.ProjectionRevision
			if !onUpdate(detail) {
				return nil
			}
		}
		if ctx.Err() != nil {
			return nil
		}
	}
}

// CancelDeliveryRequest is CancelDelivery's input, mirroring
// mcpserver.CancelDeliveryInput minus OrchestrationId.
type CancelDeliveryRequest struct {
	ExpectedRevision int
	Reason           string
}

// CancelDelivery cancels orchestrationID and returns the refreshed
// DeliveryDetail.
func (c *Client) CancelDelivery(ctx context.Context, orchestrationID string, in CancelDeliveryRequest) (*deliveryprojection.DeliveryDetail, error) {
	body := cancelDeliveryRequest{ExpectedRevision: in.ExpectedRevision, Reason: in.Reason}
	var detail deliveryprojection.DeliveryDetail
	path := "/api/v1/deliveries/" + url.PathEscape(orchestrationID) + "/cancel"
	if err := c.doJSON(ctx, c.http, http.MethodPost, path, body, &detail); err != nil {
		return nil, err
	}
	return &detail, nil
}

// doRequest issues one authenticated request against the daemon: body
// (if non-nil) is marshaled as the JSON request payload. A non-200
// response becomes an error carrying the response's own JSON error
// detail, so a caller sees the same message the HTTP handler produced
// rather than just a bare status; on that path the response body is
// already closed. On success, the caller owns the still-open response
// and must close its body.
func (c *Client) doRequest(ctx context.Context, hc *http.Client, method, path string, body any) (*http.Response, error) {
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		reader = bytes.NewReader(encoded)
	}
	req, err := http.NewRequestWithContext(ctx, method, "http://"+c.addr+path, reader)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := hc.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		msg, _ := io.ReadAll(resp.Body)
		message := string(bytes.TrimSpace(msg))
		var errBody struct {
			Error string `json:"error"`
		}
		if json.Unmarshal(msg, &errBody) == nil && errBody.Error != "" {
			message = errBody.Error
		}
		return nil, &StatusError{Method: method, Path: path, Status: resp.StatusCode, Message: message}
	}
	return resp, nil
}

// doJSON is doRequest plus decoding a 200 response's body into out (if
// non-nil).
func (c *Client) doJSON(ctx context.Context, hc *http.Client, method, path string, body, out any) error {
	resp, err := c.doRequest(ctx, hc, method, path, body)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if out == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// GetDeliveryEvidence fetches one lane-scoped evidence artifact's raw
// bytes and media type by id, scoped to orchestrationID (a mismatched
// orchestration/evidence pair 404s, same as an unknown evidenceID).
func (c *Client) GetDeliveryEvidence(ctx context.Context, orchestrationID, evidenceID string) ([]byte, string, error) {
	path := "/api/v1/deliveries/" + url.PathEscape(orchestrationID) + "/evidence/" + url.PathEscape(evidenceID)
	resp, err := c.doRequest(ctx, c.http, http.MethodGet, path, nil)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", err
	}
	return data, resp.Header.Get("Content-Type"), nil
}

// StatusError is doJSON's error for a non-200 daemon response: Status is
// the daemon's own HTTP status code (already mapped by writeDeliveryError
// et al. to mean the same thing an equivalent in-process call's sentinel
// error would - 404 not found, 409 revision conflict/invalid state), and
// Message is the response body's "error" field when present. A caller that
// needs to react to a specific status (e.g. a Panel HTTP handler forwarding
// the same status to its own response instead of collapsing every daemon
// failure to 500) should errors.As into this rather than parsing Error()'s
// text.
type StatusError struct {
	Method  string
	Path    string
	Status  int
	Message string
}

func (e *StatusError) Error() string {
	return fmt.Sprintf("daemon: %s %s: %d: %s", e.Method, e.Path, e.Status, e.Message)
}
