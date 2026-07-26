package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ygrip/punakawan/internal/artifact"
	"github.com/ygrip/punakawan/internal/revision"
	"github.com/ygrip/punakawan/pkg/protocol"
)

// twoProjectResolver builds a ProjectStores over two independent
// temp-dir-backed projects ("proj-a"/"proj-b") plus the PlanStores used to
// seed and assert content against each project's tree, so a test can prove
// a mutation lands in one project and leaves the other untouched.
func twoProjectResolver(t *testing.T) (stores *artifact.ProjectStores, rootA, rootB string) {
	t.Helper()
	rootA = t.TempDir()
	rootB = t.TempDir()
	resolve := func(projectID string) (string, error) {
		switch projectID {
		case "proj-a":
			return rootA, nil
		case "proj-b":
			return rootB, nil
		default:
			return "", fmt.Errorf("unknown project %q", projectID)
		}
	}
	return artifact.NewProjectStores(resolve), rootA, rootB
}

func TestProjectArtifactStoresCreateReviewLandsInThatProject(t *testing.T) {
	projects, rootA, rootB := twoProjectResolver(t)
	plansA := &artifact.PlanStore{WorkspaceRoot: rootA}
	ref := seedPlan(t, plansA, "plan-x", "# Plan\n\nBody.\n")

	res := NewProjectArtifactStores(projects, nil, nil, nil, nil)

	body, _ := json.Marshal(createReviewRequest{Title: "Project A review"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/projects/proj-a/artifacts/plan/plan-x/reviews", bytes.NewReader(body))
	req.SetPathValue("projectId", "proj-a")
	req.SetPathValue("type", "plan")
	req.SetPathValue("id", "plan-x")
	rec := httptest.NewRecorder()
	res.CreateReview()(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201: %s", rec.Code, rec.Body)
	}
	var got protocol.ArtifactReview
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	// The project id is stamped as the review's workspace id.
	if got.Metadata.WorkspaceId != "proj-a" {
		t.Fatalf("WorkspaceId = %q, want proj-a", got.Metadata.WorkspaceId)
	}
	if got.Artifact.Version != ref.Version || got.Artifact.RevisionHash != ref.RevisionHash {
		t.Fatalf("Artifact = %+v, want it pinned to %+v", got.Artifact, ref)
	}

	// The review is persisted under project A's tree, and project B is
	// wholly unaffected.
	reviewsA := &artifact.ReviewStore{WorkspaceRoot: rootA}
	if _, err := reviewsA.GetReview(got.Metadata.Id); err != nil {
		t.Fatalf("project A ReviewStore.GetReview: %v (review should live under proj-a)", err)
	}
	reviewsB := &artifact.ReviewStore{WorkspaceRoot: rootB}
	if _, err := reviewsB.GetReview(got.Metadata.Id); err == nil {
		t.Fatal("project B ReviewStore.GetReview succeeded, want the review absent from proj-b")
	}
}

func TestProjectArtifactStoresUnknownProjectIs404(t *testing.T) {
	projects, _, _ := twoProjectResolver(t)
	res := NewProjectArtifactStores(projects, nil, nil, nil, nil)

	body, _ := json.Marshal(createReviewRequest{Title: "x"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/projects/nope/artifacts/plan/plan-x/reviews", bytes.NewReader(body))
	req.SetPathValue("projectId", "nope")
	req.SetPathValue("type", "plan")
	req.SetPathValue("id", "plan-x")
	rec := httptest.NewRecorder()
	res.CreateReview()(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 for an unresolvable project: %s", rec.Code, rec.Body)
	}
}

// TestProjectArtifactStoresAcceptWritesToProjectTree drives the whole
// project-scoped mutation flow - create review, submit, propose, accept -
// entirely through the ProjectArtifactStores variants, and proves the
// accepted new canonical version is written into project A's PlanStore
// while project B never gains the plan at all.
func TestProjectArtifactStoresAcceptWritesToProjectTree(t *testing.T) {
	projects, rootA, rootB := twoProjectResolver(t)
	plansA := &artifact.PlanStore{WorkspaceRoot: rootA}
	seedPlan(t, plansA, "plan-x", proposalBasePlanContent)

	dispatcher := &stubDispatcher{}
	res := NewProjectArtifactStores(projects, nil, nil, func(projectID string) revision.Dispatcher {
		if projectID != "proj-a" {
			t.Fatalf("dispatcher factory called for unexpected project %q", projectID)
		}
		return dispatcher
	}, nil)

	// 1. Create the review under project A.
	crBody, _ := json.Marshal(createReviewRequest{Title: "Add LAN mode"})
	crReq := httptest.NewRequest(http.MethodPost, "/api/v1/projects/proj-a/artifacts/plan/plan-x/reviews", bytes.NewReader(crBody))
	crReq.SetPathValue("projectId", "proj-a")
	crReq.SetPathValue("type", "plan")
	crReq.SetPathValue("id", "plan-x")
	crRec := httptest.NewRecorder()
	res.CreateReview()(crRec, crReq)
	if crRec.Code != http.StatusCreated {
		t.Fatalf("create review status = %d: %s", crRec.Code, crRec.Body)
	}
	var review protocol.ArtifactReview
	if err := json.Unmarshal(crRec.Body.Bytes(), &review); err != nil {
		t.Fatalf("decode review: %v", err)
	}
	reviewID := review.Metadata.Id

	// 2. Submit it (draft -> queued) via the per-project dispatcher.
	subReq := httptest.NewRequest(http.MethodPost, "/api/v1/projects/proj-a/reviews/"+reviewID+"/submit", nil)
	subReq.SetPathValue("projectId", "proj-a")
	subReq.SetPathValue("reviewId", reviewID)
	subRec := httptest.NewRecorder()
	res.Submit()(subRec, subReq)
	if subRec.Code != http.StatusCreated {
		t.Fatalf("submit status = %d: %s", subRec.Code, subRec.Body)
	}
	if dispatcher.calls != 1 {
		t.Fatalf("dispatcher.calls = %d, want 1 (project-scoped submit must use the per-project dispatcher)", dispatcher.calls)
	}

	// 3. Post a proposal (queued -> proposal_ready).
	propBody, _ := json.Marshal(createProposalRequest{Content: proposalRevisedPlanContent, ChangeSummary: "LAN mode"})
	propReq := httptest.NewRequest(http.MethodPost, "/api/v1/projects/proj-a/reviews/"+reviewID+"/proposals", bytes.NewReader(propBody))
	propReq.SetPathValue("projectId", "proj-a")
	propReq.SetPathValue("reviewId", reviewID)
	propRec := httptest.NewRecorder()
	res.CreateProposal()(propRec, propReq)
	if propRec.Code != http.StatusCreated {
		t.Fatalf("create proposal status = %d: %s", propRec.Code, propRec.Body)
	}

	// 4. Accept attempt 1 -> writes version 2 into project A's PlanStore.
	accReq := httptest.NewRequest(http.MethodPost, "/api/v1/projects/proj-a/reviews/"+reviewID+"/proposals/1/accept", nil)
	accReq.SetPathValue("projectId", "proj-a")
	accReq.SetPathValue("reviewId", reviewID)
	accReq.SetPathValue("proposalId", "1")
	accRec := httptest.NewRecorder()
	res.AcceptProposal()(accRec, accReq)
	if accRec.Code != http.StatusOK {
		t.Fatalf("accept status = %d: %s", accRec.Code, accRec.Body)
	}

	// Project A now has version 2 with the revised content.
	curA, err := plansA.Current("plan-x")
	if err != nil {
		t.Fatalf("project A Current: %v", err)
	}
	if curA.Version != 2 {
		t.Fatalf("project A current version = %d, want 2", curA.Version)
	}
	contentA, _, err := plansA.Version("plan-x", curA.Version)
	if err != nil {
		t.Fatalf("project A Version: %v", err)
	}
	if string(contentA) != proposalRevisedPlanContent {
		t.Fatalf("project A version 2 content = %q, want the revised plan", string(contentA))
	}

	// Project B never gained the plan at all.
	plansB := &artifact.PlanStore{WorkspaceRoot: rootB}
	if _, err := plansB.Current("plan-x"); err == nil {
		t.Fatal("project B has plan-x, want it absent (mutation must not leak across projects)")
	}
}

func TestProjectArtifactStoresSubmitRequiresDispatcher(t *testing.T) {
	projects, rootA, _ := twoProjectResolver(t)
	plansA := &artifact.PlanStore{WorkspaceRoot: rootA}
	reviewsA := &artifact.ReviewStore{WorkspaceRoot: rootA}
	ref := seedPlan(t, plansA, "plan-x", proposalBasePlanContent)
	if err := reviewsA.PutReview(protocol.ArtifactReview{
		Metadata: protocol.ArtifactReviewMetadata{Id: "review-1", WorkspaceId: "proj-a", Status: protocol.ArtifactReviewMetadataStatusDraft},
		Artifact: protocol.ArtifactReviewArtifact{Type: protocol.ArtifactReviewArtifactTypePlan, Id: "plan-x", Version: ref.Version, RevisionHash: ref.RevisionHash},
		Review:   protocol.ArtifactReviewReview{Title: "x"},
	}); err != nil {
		t.Fatalf("PutReview: %v", err)
	}

	// nil dispatcher factory: submit degrades to 500, but resolution of a
	// valid project still happens first (so this is a 500, not a 404).
	res := NewProjectArtifactStores(projects, nil, nil, nil, nil)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/projects/proj-a/reviews/review-1/submit", nil)
	req.SetPathValue("projectId", "proj-a")
	req.SetPathValue("reviewId", "review-1")
	rec := httptest.NewRecorder()
	res.Submit()(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500 when no dispatcher is configured: %s", rec.Code, rec.Body)
	}
}
