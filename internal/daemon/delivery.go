// delivery.go serves the delivery-domain HTTP routes over the daemon's
// own delivery.Store (see daemon.go's Run) - the daemon-native path to
// the same DeliveryView read model and mutations
// internal/mcpserver/tools_startdelivery.go's MCP tools already expose.
// The two surfaces call the same internal/delivery.Store methods; only
// the transport (HTTP JSON vs MCP tool call) and request shape differ.
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
	"github.com/ygrip/punakawan/pkg/protocol"
)

// maxDeliveryWaitSeconds bounds how long a single GET
// /api/v1/deliveries/{id} request will long-poll for a change before
// returning whatever the store currently has - long enough to avoid
// most callers busy-polling, short enough that a request never outlives
// a normal HTTP client timeout by much.
const maxDeliveryWaitSeconds = 30

// deliveryPollInterval is how often handleDeliveryView re-checks the
// store while a wait_seconds request is blocked waiting for LatestSeq
// to advance.
const deliveryPollInterval = 250 * time.Millisecond

// handleListDeliveries serves GET /api/v1/deliveries: every orchestration
// the daemon's delivery.Store knows about, oldest first.
func (t *Transport) handleListDeliveries(w http.ResponseWriter, r *http.Request) {
	list, err := t.delivery.ListOrchestrations(r.Context())
	if err != nil {
		writeDeliveryError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

// handleDeliveryView serves GET /api/v1/deliveries/{orchestrationId}. Two
// optional query params: since_seq (mirrors get_delivery's SinceSeq -
// pass a prior response's latest_seq to see newly_runnable_lane_ids) and
// wait_seconds (0 by default, meaning return immediately). A non-zero
// wait_seconds long-polls: it blocks, re-checking the store every
// deliveryPollInterval, until either LatestSeq has advanced past
// since_seq or wait_seconds elapses, then returns the view either way -
// this is the daemon's push-style update mechanism, reusing
// BuildDeliveryViewSince's own diffing rather than a separate streaming
// subsystem. Callers that want ongoing updates call this in a loop with
// since_seq set to the previous response's latest_seq (see Client's
// WatchDeliveryView/SubscribeDeliveryView).
func (t *Transport) handleDeliveryView(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("orchestrationId")
	sinceSeq, err := queryInt(r, "since_seq", 0)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	waitSeconds, err := queryInt(r, "wait_seconds", 0)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if waitSeconds > maxDeliveryWaitSeconds {
		waitSeconds = maxDeliveryWaitSeconds
	}

	deadline := time.Now().Add(time.Duration(waitSeconds) * time.Second)
	for {
		view, err := t.delivery.BuildDeliveryViewSince(r.Context(), id, sinceSeq)
		if err != nil {
			writeDeliveryError(w, err)
			return
		}
		if waitSeconds == 0 || view.LatestSeq != sinceSeq || !time.Now().Before(deadline) {
			writeJSON(w, http.StatusOK, view)
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
// with. There is no JSON metadata shape here - DeliveryView.Lanes[].
// Evidence already carries that; this route exists only to serve the
// bytes a link built from that metadata points at.
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

// answerDeliveryQuestionRequest is POST
// /api/v1/deliveries/{orchestrationId}/answer-question's body. It mirrors
// mcpserver.AnswerDeliveryQuestionInput's own two cases - set provider
// (resolved-requirement case) or both parent_task_id and project_id
// (ambiguous-routing case) - without importing that package: the HTTP and
// MCP surfaces stay independent even though both drive the same
// delivery.Store methods. orchestration_id is not a field here since it
// comes from the URL path.
type answerDeliveryQuestionRequest struct {
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

func (t *Transport) handleAnswerDeliveryQuestion(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("orchestrationId")
	var body answerDeliveryQuestionRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	switch {
	case body.ParentTaskId != "" && body.ProjectId != "":
		if _, err := t.delivery.RouteParentTask(r.Context(), delivery.NewID(), id, body.ParentTaskId, body.ProjectId); err != nil {
			writeDeliveryError(w, err)
			return
		}
	case body.Provider != "":
		src := delivery.SourceInput{
			Provider: body.Provider, ExternalID: body.ExternalId, URL: body.Url,
			Title: body.Title, Summary: body.Summary,
		}
		if _, err := t.delivery.CaptureRequirement(r.Context(), delivery.NewID(), id, src); err != nil {
			writeDeliveryError(w, err)
			return
		}
		if _, err := t.delivery.ResolveInput(r.Context(), delivery.NewID(), id, body.ExpectedRevision, body.Reference); err != nil {
			writeDeliveryError(w, err)
			return
		}
	default:
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "answer-question requires either provider (resolved-requirement case) or both parent_task_id and project_id (routing case)"})
		return
	}

	t.writeCurrentDeliveryView(w, r, id)
}

// approveProjectDeliveryRequest is POST
// /api/v1/deliveries/{orchestrationId}/approve's body, mirroring
// mcpserver.ApproveProjectDeliveryInput minus orchestration_id (from the
// URL path).
type approveProjectDeliveryRequest struct {
	ManifestId string `json:"manifest_id"`
	ApprovedBy string `json:"approved_by"`
	Reject     bool   `json:"reject,omitempty"`
}

func (t *Transport) handleApproveProjectDelivery(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("orchestrationId")
	var body approveProjectDeliveryRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	var err error
	if body.Reject {
		_, err = t.delivery.RejectManifest(r.Context(), delivery.NewID(), id, body.ManifestId, body.ApprovedBy)
	} else {
		_, err = t.delivery.ApproveManifest(r.Context(), delivery.NewID(), id, body.ManifestId, body.ApprovedBy)
	}
	if err != nil {
		writeDeliveryError(w, err)
		return
	}

	t.writeCurrentDeliveryView(w, r, id)
}

// cancelDeliveryRequest is POST /api/v1/deliveries/{orchestrationId}/cancel's
// body, mirroring mcpserver.CancelDeliveryInput minus orchestration_id
// (from the URL path).
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

	t.writeCurrentDeliveryView(w, r, id)
}

// writeCurrentDeliveryView is the shared "mutate, then respond with the
// refreshed view" tail every mutating delivery route ends with, exactly
// like each MCP tool handler in tools_startdelivery.go does.
func (t *Transport) writeCurrentDeliveryView(w http.ResponseWriter, r *http.Request, orchestrationID string) {
	view, err := t.delivery.BuildDeliveryView(r.Context(), orchestrationID)
	if err != nil {
		writeDeliveryError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, view)
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

// deliveryWatchWaitSeconds is how long WatchDeliveryView asks the daemon
// to hold a request open per call; SubscribeDeliveryView loops this to
// give callers an ongoing stream of updates without either side needing
// a persistent streaming connection.
const deliveryWatchWaitSeconds = 25

// deliveryWatchTimeoutSlack bounds watchHTTP's request timeout beyond
// deliveryWatchWaitSeconds, covering network latency and the daemon's
// own poll-interval overshoot so a slow-but-healthy round trip is never
// mistaken for a hang.
const deliveryWatchTimeoutSlack = 10 * time.Second

// GetDeliveryView fetches orchestrationID's current DeliveryView.
// sinceSeq mirrors delivery.BuildDeliveryViewSince: pass a prior
// response's LatestSeq to populate NewlyRunnableLaneIDs; 0 reports every
// currently runnable lane instead, since there is no prior baseline.
func (c *Client) GetDeliveryView(ctx context.Context, orchestrationID string, sinceSeq int) (*delivery.DeliveryView, error) {
	var view delivery.DeliveryView
	if err := c.doJSON(ctx, c.http, http.MethodGet, deliveryViewPath(orchestrationID, sinceSeq, 0), nil, &view); err != nil {
		return nil, err
	}
	return &view, nil
}

// WatchDeliveryView is GetDeliveryView, except the daemon blocks
// server-side for up to waitSeconds waiting for LatestSeq to advance
// past sinceSeq before responding - the daemon clamps waitSeconds to its
// own maxDeliveryWaitSeconds. It returns whatever the daemon has once its
// wait window elapses, whether or not anything actually changed; callers
// compare the returned LatestSeq to sinceSeq themselves before calling
// again, or use SubscribeDeliveryView to do that in a loop.
func (c *Client) WatchDeliveryView(ctx context.Context, orchestrationID string, sinceSeq, waitSeconds int) (*delivery.DeliveryView, error) {
	ctx, cancel := context.WithTimeout(ctx, time.Duration(waitSeconds)*time.Second+deliveryWatchTimeoutSlack)
	defer cancel()
	var view delivery.DeliveryView
	if err := c.doJSON(ctx, c.watchHTTP, http.MethodGet, deliveryViewPath(orchestrationID, sinceSeq, waitSeconds), nil, &view); err != nil {
		return nil, err
	}
	return &view, nil
}

// SubscribeDeliveryView is the daemon's push-style update mechanism from
// a caller's perspective: it long-polls via WatchDeliveryView in a loop,
// calling onUpdate every time LatestSeq advances past sinceSeq, until ctx
// is cancelled or onUpdate returns false. It never returns an error for a
// cancelled ctx - that is the normal way a caller stops watching.
func (c *Client) SubscribeDeliveryView(ctx context.Context, orchestrationID string, sinceSeq int, onUpdate func(*delivery.DeliveryView) bool) error {
	for {
		view, err := c.WatchDeliveryView(ctx, orchestrationID, sinceSeq, deliveryWatchWaitSeconds)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		if view.LatestSeq != sinceSeq {
			sinceSeq = view.LatestSeq
			if !onUpdate(view) {
				return nil
			}
		}
		if ctx.Err() != nil {
			return nil
		}
	}
}

// ListDeliveries returns every orchestration the daemon's delivery.Store
// knows about, oldest first.
func (c *Client) ListDeliveries(ctx context.Context) ([]*protocol.DeliveryOrchestration, error) {
	var list []*protocol.DeliveryOrchestration
	if err := c.doJSON(ctx, c.http, http.MethodGet, "/api/v1/deliveries", nil, &list); err != nil {
		return nil, err
	}
	return list, nil
}

// AnswerDeliveryQuestionRequest is AnswerDeliveryQuestion's input,
// mirroring mcpserver.AnswerDeliveryQuestionInput's two cases -
// resolved-requirement (set Provider) or ambiguous-routing (set
// ParentTaskId and ProjectId) - minus OrchestrationId, passed as its own
// parameter instead since the HTTP route carries it in the path.
type AnswerDeliveryQuestionRequest struct {
	Reference        string
	ExpectedRevision int

	Provider   string
	ExternalId string
	Url        string
	Title      string
	Summary    string

	ParentTaskId string
	ProjectId    string
}

// AnswerDeliveryQuestion resolves one pending question on orchestrationID
// and returns the refreshed DeliveryView.
func (c *Client) AnswerDeliveryQuestion(ctx context.Context, orchestrationID string, in AnswerDeliveryQuestionRequest) (*delivery.DeliveryView, error) {
	body := answerDeliveryQuestionRequest{
		Reference: in.Reference, ExpectedRevision: in.ExpectedRevision,
		Provider: in.Provider, ExternalId: in.ExternalId, Url: in.Url, Title: in.Title, Summary: in.Summary,
		ParentTaskId: in.ParentTaskId, ProjectId: in.ProjectId,
	}
	var view delivery.DeliveryView
	path := "/api/v1/deliveries/" + url.PathEscape(orchestrationID) + "/answer-question"
	if err := c.doJSON(ctx, c.http, http.MethodPost, path, body, &view); err != nil {
		return nil, err
	}
	return &view, nil
}

// ApproveProjectDeliveryRequest is ApproveProjectDelivery's input,
// mirroring mcpserver.ApproveProjectDeliveryInput minus OrchestrationId.
type ApproveProjectDeliveryRequest struct {
	ManifestId string
	ApprovedBy string
	Reject     bool
}

// ApproveProjectDelivery approves (or, with Reject set, rejects) one
// approval manifest and returns the refreshed DeliveryView.
func (c *Client) ApproveProjectDelivery(ctx context.Context, orchestrationID string, in ApproveProjectDeliveryRequest) (*delivery.DeliveryView, error) {
	body := approveProjectDeliveryRequest{ManifestId: in.ManifestId, ApprovedBy: in.ApprovedBy, Reject: in.Reject}
	var view delivery.DeliveryView
	path := "/api/v1/deliveries/" + url.PathEscape(orchestrationID) + "/approve"
	if err := c.doJSON(ctx, c.http, http.MethodPost, path, body, &view); err != nil {
		return nil, err
	}
	return &view, nil
}

// CancelDeliveryRequest is CancelDelivery's input, mirroring
// mcpserver.CancelDeliveryInput minus OrchestrationId.
type CancelDeliveryRequest struct {
	ExpectedRevision int
	Reason           string
}

// CancelDelivery cancels orchestrationID and returns the refreshed
// DeliveryView.
func (c *Client) CancelDelivery(ctx context.Context, orchestrationID string, in CancelDeliveryRequest) (*delivery.DeliveryView, error) {
	body := cancelDeliveryRequest{ExpectedRevision: in.ExpectedRevision, Reason: in.Reason}
	var view delivery.DeliveryView
	path := "/api/v1/deliveries/" + url.PathEscape(orchestrationID) + "/cancel"
	if err := c.doJSON(ctx, c.http, http.MethodPost, path, body, &view); err != nil {
		return nil, err
	}
	return &view, nil
}

// deliveryViewPath builds GET /api/v1/deliveries/{orchestrationId}'s
// path plus query string, omitting since_seq/wait_seconds when zero so a
// plain GetDeliveryView call looks like the simplest possible request.
func deliveryViewPath(orchestrationID string, sinceSeq, waitSeconds int) string {
	q := url.Values{}
	if sinceSeq != 0 {
		q.Set("since_seq", strconv.Itoa(sinceSeq))
	}
	if waitSeconds != 0 {
		q.Set("wait_seconds", strconv.Itoa(waitSeconds))
	}
	p := "/api/v1/deliveries/" + url.PathEscape(orchestrationID)
	if len(q) > 0 {
		p += "?" + q.Encode()
	}
	return p
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
