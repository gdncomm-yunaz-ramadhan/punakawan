package search

import (
	"strings"
	"time"

	"github.com/ygrip/punakawan/pkg/protocol"
)

// IndexedDocument is §11.4's BM25F document model, indexed in Bleve. Paths,
// Symbols, and Identifiers are not authored directly on KnowledgeRecord -
// they are derived at index-build time by running DetectIdentifiers over
// the record's own text fields, so a path or symbol mentioned in a record's
// content becomes matchable without every record having to hand-curate
// those lists.
type IndexedDocument struct {
	Id      string `json:"id"`
	Type    string `json:"type"`
	Title   string `json:"title"`
	Summary string `json:"summary"`
	Content string `json:"content"`

	Aliases []string `json:"aliases"`
	Tags    []string `json:"tags"`

	Project    string `json:"project"`
	Repository string `json:"repository"`
	Module     string `json:"module"`
	Path       string `json:"path"`

	Paths       []string `json:"paths"`
	Symbols     []string `json:"symbols"`
	Identifiers []string `json:"identifiers"`

	TrustLevel string    `json:"trustLevel"`
	UpdatedAt  time.Time `json:"updatedAt"`
}

func stringOrEmpty(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// BuildDocument derives an IndexedDocument from rec, per §11.4's document
// model. updatedAt is Dolt's row-level timestamp (see
// knowledge.Store.AllWithUpdatedAt), not part of rec's own JSON payload.
func BuildDocument(rec protocol.KnowledgeRecord, updatedAt time.Time) IndexedDocument {
	doc := IndexedDocument{
		Id:         rec.Id,
		Type:       string(rec.Type),
		Title:      rec.Title,
		Summary:    stringOrEmpty(rec.Summary),
		Content:    stringOrEmpty(rec.Content),
		Aliases:    append([]string(nil), rec.Aliases...),
		Tags:       append([]string(nil), rec.Tags...),
		TrustLevel: string(rec.Validity.State),
		UpdatedAt:  updatedAt,
	}
	if rec.Scope != nil {
		doc.Project = stringOrEmpty(rec.Scope.Project)
		doc.Repository = stringOrEmpty(rec.Scope.Repository)
		doc.Module = stringOrEmpty(rec.Scope.Module)
		doc.Path = stringOrEmpty(rec.Scope.Path)
	}

	text := strings.Join(append([]string{doc.Title, doc.Summary, doc.Content}, append(doc.Aliases, doc.Tags...)...), " ")
	for _, id := range DetectIdentifiers(text) {
		switch id.Kind {
		case IdentifierKindFilePath, IdentifierKindApiRoute:
			doc.Paths = append(doc.Paths, id.Value)
		case IdentifierKindSymbol, IdentifierKindJavaClassOrMethod:
			doc.Symbols = append(doc.Symbols, id.Value)
		default:
			doc.Identifiers = append(doc.Identifiers, id.Value)
		}
	}
	return doc
}
