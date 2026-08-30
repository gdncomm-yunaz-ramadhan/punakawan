package clienthooks

import (
	"strings"

	"github.com/ygrip/punakawan/internal/telemetry"
)

// mainSourceID is the source_id every top-level turn's own usage snapshot
// (as opposed to a named subagent's) is recorded under.
const mainSourceID = "main"

// sourceIDFor returns agentID as the snapshot source id when a lifecycle
// event fired inside a subagent, or mainSourceID for the top-level turn.
func sourceIDFor(agentID string) string {
	if strings.TrimSpace(agentID) == "" {
		return mainSourceID
	}
	return strings.TrimSpace(agentID)
}

// stopIDFor derives a deterministic Finalize stop id from a session's own
// identity, so a client that (incorrectly) fires SessionEnd twice for the
// same external session still finalizes it exactly once - Finalize's own
// idempotency is keyed by this exact stop id.
func stopIDFor(clientKind, externalSessionID string) string {
	return clientKind + ":" + externalSessionID + ":end"
}

// Action names which telemetry.Store method a Mapped event should drive.
type Action string

const (
	// ActionBegin means call telemetry.Store.Begin with Mapped.Begin.
	ActionBegin Action = "begin"
	// ActionSnapshot means call telemetry.Store.IngestSnapshot with
	// Mapped.Snapshot.
	ActionSnapshot Action = "snapshot"
	// ActionFinalize means call telemetry.Store.Finalize with
	// Mapped.Finalize.
	ActionFinalize Action = "finalize"
	// ActionIgnore means this event, though recognized, carries no
	// telemetry action (e.g. a lifecycle event this package does not track
	// usage against).
	ActionIgnore Action = "ignore"
)

// Mapped is one lifecycle hook event's mapping onto a telemetry action.
// Exactly one of Begin/Snapshot/Finalize is non-nil, matching Action;
// ExternalSessionID/ClientKind are always populated on Begin, and every
// request's SessionID (Snapshot/Finalize) or DeliveryID/ExecutionID
// (Begin) that this package cannot itself know is left empty - the
// caller, which owns the session marker naming the current delivery and
// the already-resolved punakawan session id, fills those in before
// calling the Store.
type Mapped struct {
	Action   Action
	Begin    *telemetry.BeginRequest
	Snapshot *telemetry.SnapshotRequest
	Finalize *telemetry.FinalizeRequest
	// ExternalSessionID is always populated (even for ActionIgnore/errors
	// where possible), independent of Action: it is the client's own
	// session id from the hook payload, which a caller needs to resolve
	// which telemetry.AgentSession a Snapshot/Finalize request belongs to
	// (telemetry.Store.GetSessionByExternalID), since that is a different
	// id than the one a Begin request would mint.
	ExternalSessionID string
}

// ignored is the Mapped value naming ActionIgnore, for an event this
// package recognizes but has nothing to record for.
func ignoredFor(externalSessionID string) Mapped {
	return Mapped{Action: ActionIgnore, ExternalSessionID: externalSessionID}
}
