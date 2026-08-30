// Package deliverysource implements contract.DeliveryReader over a
// *daemon.Client connection, following this codebase's reader-per-subsystem
// convention (internal/panel/sources): a thin wrapper around an
// already-existing store - here, the daemon's own delivery.Store and
// internal/deliveryprojection.Projector, reached over its authenticated
// loopback transport rather than opened directly, since internal/daemon.
// Daemon is the only process allowed to do that.
package deliverysource

import (
	"context"

	"github.com/ygrip/punakawan/internal/daemon"
	"github.com/ygrip/punakawan/internal/deliveryprojection"
)

// Source implements contract.DeliveryReader by forwarding every call
// verbatim to Client - the daemon's HTTP surface already exposes exactly
// the shape a Panel handler needs, so there is nothing to translate here.
type Source struct {
	Client *daemon.Client
}

// ListDeliveries returns every delivery's compact summary.
func (s *Source) ListDeliveries(ctx context.Context) (daemon.ListDeliveriesResult, error) {
	return s.Client.ListDeliveries(ctx)
}

// GetDeliveryDetail returns orchestrationID's current DeliveryDetail
// immediately.
func (s *Source) GetDeliveryDetail(ctx context.Context, orchestrationID string) (*deliveryprojection.DeliveryDetail, error) {
	return s.Client.GetDeliveryDetail(ctx, orchestrationID)
}

// WatchDeliveryDetail long-polls the daemon for up to waitSeconds waiting
// for ProjectionRevision to advance past sinceRevision.
func (s *Source) WatchDeliveryDetail(ctx context.Context, orchestrationID string, sinceRevision, waitSeconds int) (*deliveryprojection.DeliveryDetail, error) {
	return s.Client.WatchDeliveryDetail(ctx, orchestrationID, sinceRevision, waitSeconds)
}

// CancelDelivery cancels orchestrationID.
func (s *Source) CancelDelivery(ctx context.Context, orchestrationID string, in daemon.CancelDeliveryRequest) (*deliveryprojection.DeliveryDetail, error) {
	return s.Client.CancelDelivery(ctx, orchestrationID, in)
}

// GetDeliveryEvidence fetches one lane-scoped evidence artifact's raw
// bytes and media type by id.
func (s *Source) GetDeliveryEvidence(ctx context.Context, orchestrationID, evidenceID string) ([]byte, string, error) {
	return s.Client.GetDeliveryEvidence(ctx, orchestrationID, evidenceID)
}
