package api

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/ygrip/punakawan/internal/dossier"
	"github.com/ygrip/punakawan/pkg/protocol"
)

// fakeDossierReader delegates to the real internal/dossier store rooted at a
// temp dir, so blocking-findings and self-verification errors are the genuine
// ones the handlers must map.
type fakeDossierReader struct{ root string }

func (f *fakeDossierReader) ListDossiers(ctx context.Context, projectID string) ([]protocol.ChangeDossier, error) {
	ids, err := dossier.List(f.root)
	if err != nil {
		return nil, err
	}
	out := make([]protocol.ChangeDossier, 0, len(ids))
	for _, id := range ids {
		loaded, err := dossier.Get(f.root, id)
		if err != nil {
			return nil, err
		}
		out = append(out, loaded.Dossier)
	}
	return out, nil
}

func (f *fakeDossierReader) CreateDossier(ctx context.Context, projectID string, d protocol.ChangeDossier) (protocol.ChangeDossier, error) {
	return dossier.Create(f.root, d)
}

func (f *fakeDossierReader) GetDossier(ctx context.Context, projectID, id string) (dossier.Loaded, error) {
	return dossier.Get(f.root, id)
}

func (f *fakeDossierReader) AddDossierClaim(ctx context.Context, projectID, id string, claim protocol.DossierClaim) (protocol.DossierClaim, error) {
	return dossier.AddClaim(f.root, id, claim)
}

func (f *fakeDossierReader) VerifyDossierClaim(ctx context.Context, projectID, id, claimID, byRole, note string) (protocol.DossierClaim, error) {
	return dossier.VerifyClaim(f.root, id, claimID, byRole, note)
}

func (f *fakeDossierReader) DisputeDossierClaim(ctx context.Context, projectID, id, claimID, byRole, note string) (protocol.DossierClaim, error) {
	return dossier.DisputeClaim(f.root, id, claimID, byRole, note)
}

func (f *fakeDossierReader) AddDossierEvidence(ctx context.Context, projectID, id string, ev protocol.DossierEvidence) (protocol.DossierEvidence, error) {
	return dossier.AddEvidence(f.root, id, ev)
}

func (f *fakeDossierReader) FinalizeDossier(ctx context.Context, projectID, id string) error {
	return dossier.Finalize(f.root, id)
}

func (f *fakeDossierReader) ExportDossierMarkdown(ctx context.Context, projectID, id string) (string, error) {
	return dossier.ExportMarkdown(f.root, id)
}

func (f *fakeDossierReader) ExportDossierJSON(ctx context.Context, projectID, id string) ([]byte, error) {
	return dossier.ExportJSON(f.root, id)
}

// createDossierViaHandler POSTs a new dossier and returns its assigned id.
func createDossierViaHandler(t *testing.T, reader *fakeDossierReader) string {
	t.Helper()
	rec := doReq(t, http.MethodPost, "/api/v1/projects/proj-a/dossiers",
		`{"title":"Change X","project_id":"proj","objective":{"statement":"do the change"}}`,
		map[string]string{"projectId": "proj-a"}, DossierCreateHandler(reader))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status = %d, want 201: %s", rec.Code, rec.Body.String())
	}
	var out struct {
		Dossier protocol.ChangeDossier `json:"dossier"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode create: %v", err)
	}
	if out.Dossier.Id == "" {
		t.Fatalf("server did not assign a dossier id")
	}
	return out.Dossier.Id
}

// addClaimViaHandler POSTs a claim produced by producerRole and returns its id.
func addClaimViaHandler(t *testing.T, reader *fakeDossierReader, dossierID, producerRole string) string {
	t.Helper()
	body := `{"type":"tests_pass","statement":"the tests pass","producer":{"role":"` + producerRole + `"}}`
	rec := doReq(t, http.MethodPost, "/api/v1/projects/proj-a/dossiers/"+dossierID+"/claims", body,
		map[string]string{"projectId": "proj-a", "id": dossierID}, DossierAddClaimHandler(reader))
	if rec.Code != http.StatusCreated {
		t.Fatalf("add claim status = %d, want 201: %s", rec.Code, rec.Body.String())
	}
	var out struct {
		Claim protocol.DossierClaim `json:"claim"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode claim: %v", err)
	}
	if out.Claim.Id == "" {
		t.Fatalf("server did not assign a claim id")
	}
	return out.Claim.Id
}

func TestDossierCreateAndGet(t *testing.T) {
	reader := &fakeDossierReader{root: t.TempDir()}
	id := createDossierViaHandler(t, reader)

	rec := doReq(t, http.MethodGet, "/api/v1/projects/proj-a/dossiers/"+id, "",
		map[string]string{"projectId": "proj-a", "id": id}, DossierGetHandler(reader))
	if rec.Code != http.StatusOK {
		t.Fatalf("get status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	var out struct {
		Dossier  protocol.ChangeDossier     `json:"dossier"`
		Claims   []protocol.DossierClaim    `json:"claims"`
		Evidence []protocol.DossierEvidence `json:"evidence"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out.Dossier.Id != id {
		t.Fatalf("dossier id = %q, want %q", out.Dossier.Id, id)
	}
	if out.Dossier.Status != protocol.ChangeDossierStatusDraft {
		t.Fatalf("status = %q, want draft", out.Dossier.Status)
	}
}

func TestDossierSelfVerification409(t *testing.T) {
	reader := &fakeDossierReader{root: t.TempDir()}
	id := createDossierViaHandler(t, reader)
	claimID := addClaimViaHandler(t, reader, id, "petruk")

	// Petruk produced the claim, so Petruk verifying it is self-verification.
	rec := doReq(t, http.MethodPost, "/api/v1/projects/proj-a/dossiers/"+id+"/claims/"+claimID+"/verify",
		`{"by_role":"petruk","note":"looks good"}`,
		map[string]string{"projectId": "proj-a", "id": id, "claimId": claimID}, DossierVerifyClaimHandler(reader))
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409: %s", rec.Code, rec.Body.String())
	}
	var body map[string]string
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if body["code"] != "self_verification" {
		t.Fatalf("code = %q, want self_verification", body["code"])
	}
}

func TestDossierFinalizeBlocked409(t *testing.T) {
	reader := &fakeDossierReader{root: t.TempDir()}
	id := createDossierViaHandler(t, reader)
	claimID := addClaimViaHandler(t, reader, id, "petruk")

	// Gareng disputes Petruk's claim: a disputed claim is a blocking finding.
	rec := doReq(t, http.MethodPost, "/api/v1/projects/proj-a/dossiers/"+id+"/claims/"+claimID+"/dispute",
		`{"by_role":"gareng","note":"tests are flaky"}`,
		map[string]string{"projectId": "proj-a", "id": id, "claimId": claimID}, DossierDisputeClaimHandler(reader))
	if rec.Code != http.StatusOK {
		t.Fatalf("dispute status = %d, want 200: %s", rec.Code, rec.Body.String())
	}

	rec = doReq(t, http.MethodPost, "/api/v1/projects/proj-a/dossiers/"+id+"/finalize", "",
		map[string]string{"projectId": "proj-a", "id": id}, DossierFinalizeHandler(reader))
	if rec.Code != http.StatusConflict {
		t.Fatalf("finalize status = %d, want 409: %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Code     string   `json:"code"`
		Blockers []string `json:"blockers"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Code != "blocking_findings" {
		t.Fatalf("code = %q, want blocking_findings", body.Code)
	}
	if len(body.Blockers) == 0 {
		t.Fatalf("blockers empty, want the disputed-claim reason")
	}
}
