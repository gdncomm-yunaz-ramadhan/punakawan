package knowledge

import (
	"fmt"
	"regexp"

	"github.com/ygrip/punakawan/pkg/protocol"
)

// idPattern matches the stable workspace-level URI scheme from §6.2:
// pkw:<kind>/<workspace>/<local-id>.
var idPattern = regexp.MustCompile(`^pkw:[a-z][a-z-]*/[a-z0-9-]+/.+$`)

// Validate enforces the required provenance fields from §7.3 and the §7.4
// rule that knowledge must not be silently promoted to verified: a record
// claiming validity.state == verified must record who verified it.
func Validate(rec protocol.KnowledgeRecord) error {
	if !idPattern.MatchString(rec.Id) {
		return fmt.Errorf("knowledge: id %q does not match the pkw:<kind>/<workspace>/<local-id> scheme (§6.2)", rec.Id)
	}
	if rec.Type == "" {
		return fmt.Errorf("knowledge: %s: missing type", rec.Id)
	}
	if rec.Title == "" {
		return fmt.Errorf("knowledge: %s: missing title", rec.Id)
	}
	if rec.Source.Provider == "" {
		return fmt.Errorf("knowledge: %s: missing source.provider", rec.Id)
	}
	if rec.Source.RetrievedAt.IsZero() {
		return fmt.Errorf("knowledge: %s: missing source.retrieved_at", rec.Id)
	}
	if rec.Extraction.Method == "" {
		return fmt.Errorf("knowledge: %s: missing extraction.method", rec.Id)
	}
	if rec.Validity.State == "" {
		return fmt.Errorf("knowledge: %s: missing validity.state", rec.Id)
	}
	if rec.Validity.State == protocol.KnowledgeRecordValidityStateVerified && len(rec.Validity.VerifiedBy) == 0 {
		return fmt.Errorf("knowledge: %s: validity.state is verified but validity.verified_by is empty (§7.4: must not silently promote to verified)", rec.Id)
	}
	return nil
}
