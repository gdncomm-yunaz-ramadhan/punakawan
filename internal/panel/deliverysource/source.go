// Package deliverysource implements contract.DeliveryReader over a
// *daemon.Client connection, following this codebase's reader-per-subsystem
// convention (internal/panel/sources): a thin wrapper around an
// already-existing store - here, the daemon's own delivery.Store, reached
// over its authenticated loopback transport rather than opened directly,
// since internal/daemon.Daemon is the only process allowed to do that.
package deliverysource

import (
	"context"

	"github.com/ygrip/punakawan/internal/daemon"
	"github.com/ygrip/punakawan/internal/delivery"
	"github.com/ygrip/punakawan/pkg/protocol"
)

// Source implements contract.DeliveryReader by forwarding every call
// verbatim to Client - the daemon's HTTP surface already exposes exactly
// the shape a Panel handler needs, so there is nothing to translate here.
type Source struct {
	Client *daemon.Client
}

// ListDeliveries returns every orchestration the daemon's delivery.Store
// knows about, oldest first.
func (s *Source) ListDeliveries(ctx context.Context) ([]*protocol.DeliveryOrchestration, error) {
	return s.Client.ListDeliveries(ctx)
}

// GetDeliveryView returns orchestrationID's current view immediately.
func (s *Source) GetDeliveryView(ctx context.Context, orchestrationID string, sinceSeq int) (*delivery.DeliveryView, error) {
	return s.Client.GetDeliveryView(ctx, orchestrationID, sinceSeq)
}

// WatchDeliveryView long-polls the daemon for up to waitSeconds waiting for
// LatestSeq to advance past sinceSeq.
func (s *Source) WatchDeliveryView(ctx context.Context, orchestrationID string, sinceSeq, waitSeconds int) (*delivery.DeliveryView, error) {
	return s.Client.WatchDeliveryView(ctx, orchestrationID, sinceSeq, waitSeconds)
}

// AnswerDeliveryQuestion resolves one pending question on orchestrationID.
func (s *Source) AnswerDeliveryQuestion(ctx context.Context, orchestrationID string, in daemon.AnswerDeliveryQuestionRequest) (*delivery.DeliveryView, error) {
	return s.Client.AnswerDeliveryQuestion(ctx, orchestrationID, in)
}

// ApproveProjectDelivery approves (or rejects) one approval manifest.
func (s *Source) ApproveProjectDelivery(ctx context.Context, orchestrationID string, in daemon.ApproveProjectDeliveryRequest) (*delivery.DeliveryView, error) {
	return s.Client.ApproveProjectDelivery(ctx, orchestrationID, in)
}

// CancelDelivery cancels orchestrationID.
func (s *Source) CancelDelivery(ctx context.Context, orchestrationID string, in daemon.CancelDeliveryRequest) (*delivery.DeliveryView, error) {
	return s.Client.CancelDelivery(ctx, orchestrationID, in)
}

// GetDeliveryEvidence fetches one lane-scoped evidence artifact's raw
// bytes and media type by id.
func (s *Source) GetDeliveryEvidence(ctx context.Context, orchestrationID, evidenceID string) ([]byte, string, error) {
	return s.Client.GetDeliveryEvidence(ctx, orchestrationID, evidenceID)
}
