package clienthooks

import (
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"
	"strings"

	"github.com/ygrip/punakawan/internal/telemetry"
)

// mainSourceID is the source_id every top-level turn's own usage snapshot
// (as opposed to a named subagent's) is recorded under.
const mainSourceID = "main"

// sourceIDForTranscript keys a snapshot by the transcript it summarizes.
//
// The source id used to be the client's agent id, on the assumption that
// a subagent event summarized the subagent's own transcript. It does not:
// every branch here summarizes whatever path the hook payload carried,
// and a client that passes the session transcript to a subagent event -
// which Claude Code does - then stored the whole session's totals a
// second time under a different source id. Since totals are summed across
// sources, one session with two tool-use agent ids reported three times
// its real usage.
//
// Keying by the transcript makes the id follow the thing being counted:
// every event over one file collapses onto one row, and a genuinely
// separate subagent transcript (Codex supplies one) still earns its own.
// The session's own transcript keeps the name "main" - Claude Code names
// it after the session - so existing rows stay addressable.
func sourceIDForTranscript(externalSessionID, path string) string {
	clean := strings.TrimSpace(path)
	if clean == "" {
		return mainSourceID
	}
	clean = filepath.Clean(clean)
	base := strings.TrimSuffix(filepath.Base(clean), filepath.Ext(clean))
	if session := strings.TrimSpace(externalSessionID); session != "" && strings.EqualFold(base, session) {
		return mainSourceID
	}
	sum := sha256.Sum256([]byte(clean))
	return "t" + hex.EncodeToString(sum[:8])
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
