// Package revision implements §8's "Automatic Retrigger Flow": dispatching
// a durable BD parent+child task graph for one artifact revision request,
// per punakawan-artifact-review-plan-mutation-plan-v2.md §16.
//
// Only the dispatch step lives here - actually revising the artifact
// (§9's Agent Revision Contract, §11's validation) is Punakawan's own
// agent loop picking up the created BD tasks via the existing
// bd ready/claim flow, the same mechanism every other Punakawan task
// already uses. This package's job ends at "a durable, idempotent run
// exists for a human or agent to pick up," matching this session's
// established honest-gap pattern (e.g. internal/recipe's JiraAgileClient)
// rather than fabricating an in-process revision engine.
package revision

import (
	"context"
)

// RunReference identifies the durable run a Dispatch call created (or, for
// a repeated call with the same request, already existed for).
type RunReference struct {
	// RunID is the run's own identifier - equal to ParentTaskID in this
	// implementation, since the BD parent task IS the durable run record
	// (§16: "the parent remains blocked at Await user acceptance until
	// the user acts").
	RunID string
	// ParentTaskID is the BD id of "Revise <artifact> from review
	// <review-id>".
	ParentTaskID string
}

// Dispatcher is §8's RevisionWorkflowDispatcher: it turns one immutable
// ArtifactRevisionRequest into a durable, resumable unit of work. A
// second Dispatch call carrying the same request.Metadata.Id (the
// idempotency key, computed by the caller per §8's "review ID + base
// revision hash + comment snapshot hash + submission sequence") must
// return the existing run rather than create a second one.
type Dispatcher interface {
	Dispatch(ctx context.Context, request Request) (RunReference, error)
}

// Request is the subset of an ArtifactRevisionRequest plus its own review
// a Dispatcher needs to build §9's Agent Revision Contract and §16's BD
// task titles - kept separate from protocol.ArtifactRevisionRequest so
// this package does not need to import protocol.ArtifactReview just to
// read a title/instruction.
type Request struct {
	RequestID         string
	ReviewID          string
	ArtifactType      string
	ArtifactID        string
	BaseVersion       int
	BaseRevisionHash  string
	ReviewTitle       string
	ReviewInstruction string
	CommentCount      int
}
