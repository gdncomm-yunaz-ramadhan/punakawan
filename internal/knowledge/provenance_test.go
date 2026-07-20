package knowledge

import (
	"testing"
	"time"

	"github.com/ygrip/punakawan/pkg/protocol"
)

func validRecord() protocol.KnowledgeRecord {
	return protocol.KnowledgeRecord{
		Id:     "pkw:req/fixture/REQ-1",
		Type:   protocol.KnowledgeRecordTypeRequirement,
		Status: "active",
		Title:  "Refund an approved order",
		Source: protocol.KnowledgeRecordSource{
			Provider:    "jira",
			RetrievedAt: time.Now().UTC(),
		},
		Extraction: protocol.KnowledgeRecordExtraction{
			Method: protocol.KnowledgeRecordExtractionMethodModelAssisted,
		},
		Validity: protocol.KnowledgeRecordValidity{
			State: protocol.KnowledgeRecordValidityStateInferred,
		},
	}
}

func TestValidateAcceptsCompleteRecord(t *testing.T) {
	if err := Validate(validRecord()); err != nil {
		t.Fatalf("expected valid record to pass, got %v", err)
	}
}

func TestValidateRejectsBadID(t *testing.T) {
	rec := validRecord()
	rec.Id = "not-a-pkw-uri"
	if err := Validate(rec); err == nil {
		t.Fatal("expected error for malformed id")
	}
}

func TestValidateRequiresProvenanceFields(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*protocol.KnowledgeRecord)
	}{
		{"missing type", func(r *protocol.KnowledgeRecord) { r.Type = "" }},
		{"missing title", func(r *protocol.KnowledgeRecord) { r.Title = "" }},
		{"missing source.provider", func(r *protocol.KnowledgeRecord) { r.Source.Provider = "" }},
		{"missing source.retrieved_at", func(r *protocol.KnowledgeRecord) { r.Source.RetrievedAt = time.Time{} }},
		{"missing extraction.method", func(r *protocol.KnowledgeRecord) { r.Extraction.Method = "" }},
		{"missing validity.state", func(r *protocol.KnowledgeRecord) { r.Validity.State = "" }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rec := validRecord()
			c.mutate(&rec)
			if err := Validate(rec); err == nil {
				t.Fatalf("expected error for %s", c.name)
			}
		})
	}
}

func TestValidateRejectsVerifiedWithoutVerifiedBy(t *testing.T) {
	rec := validRecord()
	rec.Validity.State = protocol.KnowledgeRecordValidityStateVerified
	rec.Validity.VerifiedBy = nil
	if err := Validate(rec); err == nil {
		t.Fatal("expected error for verified record with no verified_by")
	}
}

func TestValidateAcceptsVerifiedWithVerifiedBy(t *testing.T) {
	rec := validRecord()
	rec.Validity.State = protocol.KnowledgeRecordValidityStateVerified
	rec.Validity.VerifiedBy = []string{"gareng"}
	if err := Validate(rec); err != nil {
		t.Fatalf("expected valid record to pass, got %v", err)
	}
}
