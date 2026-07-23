package recipe

import (
	"context"

	"github.com/ygrip/punakawan/pkg/protocol"
)

// FieldMeta is one built-in field's current configuration, the subset of
// packages/adapter-atlassian's getIssueTypeFieldMeta response this package
// actually needs to decide drift. Type is carried for a future caller that
// wants type-level drift too; today's check only ever looks at presence
// (see checkFieldSchemaDrift) since a built-in/system Jira field's schema
// type is fixed platform-wide, not something a project's field
// configuration can retype - only whether the field is configured/visible
// for a project (and thus present in this response at all) actually
// varies per project.
type FieldMeta struct {
	ID   string
	Type string
}

// FieldSchema is a project's current field configuration, as a
// JiraFieldSchemaClient reports it.
type FieldSchema struct {
	// Fields is keyed by the built-in selector field name (e.g.
	// "component", "sprint" - compiler.go's fieldOperators allow-list),
	// not Jira's internal custom field id, since that's what a recipe's
	// selector actually references and what needs checking for drift. A
	// field this map has no entry for is treated as removed from the
	// project's field configuration.
	Fields map[string]FieldMeta
}

// JiraFieldSchemaClient resolves a project's current field configuration,
// mirroring packages/adapter-atlassian/src/operations.ts's
// getIssueTypeFieldMeta operation (params: projectIdOrKey, issueTypeId).
// No concrete Go implementation exists yet - the same honest gap as
// JiraSearchClient/JiraAgileClient (see their doc comments): nothing on
// the Go side calls call_adapter_operation for this today. issueTypeID is
// accepted for parity with the adapter operation's own signature but is
// frequently passed as "" by this package's caller: a retrieval recipe's
// selector has no issue-type concept at all (only project/component are
// captured - see selectorProjectKey), which was task q9r.7.2's own filed
// open context-resolution question, resolved here by reusing exactly the
// scope a recipe's selector already captures rather than inventing a new
// mechanism. A concrete implementation is expected to fall back to a
// project's default/first issue type (or an equivalent non-issue-type-
// scoped field lookup) when issueTypeID is empty - still a strictly more
// accurate check than skipping it entirely.
type JiraFieldSchemaClient interface {
	FieldMeta(ctx context.Context, projectKey, issueTypeID string) (FieldSchema, error)
}

// referencedBuiltinFields returns the deduplicated set of every leaf field
// name rr's selector references. Every entry is necessarily one of
// compiler.go's fieldOperators allow-list, since Compile already rejects
// any selector referencing a field outside it - task q9r.7.2 finding #1's
// "custom-field drift is moot because custom fields aren't compilable in
// the first place" applies here too: there is nothing else this could
// return.
func referencedBuiltinFields(sel protocol.KnowledgeRecordRetrievalRecipeSelector) []string {
	seen := map[string]bool{}
	var out []string
	collect := func(field, _ string, _ interface{}) {
		if field == "" || seen[field] {
			return
		}
		seen[field] = true
		out = append(out, field)
	}
	for _, e := range sel.All {
		walkAllElemLeaves(e, collect)
	}
	for _, e := range sel.Any {
		walkAnyElemLeaves(e, collect)
	}
	return out
}

// selectorProjectKey extracts the literal value of rr's selector's
// equals-operator "project" clause, the project context a field-schema
// drift check runs against - reusing the project/component scoping a
// recipe's selector already captures (task q9r.7.2's own filed open
// question) instead of inventing a separate context-resolution
// mechanism. ok is false if the selector has no such clause to anchor the
// check to (e.g. an "in"-list or resolver-valued project, or no project
// clause at all) - the drift check simply skips in that case rather than
// guessing which project to ask about.
func selectorProjectKey(sel protocol.KnowledgeRecordRetrievalRecipeSelector) (key string, ok bool) {
	find := func(field, operator string, value interface{}) {
		if ok || field != "project" || operator != "equals" {
			return
		}
		m, isMap := value.(map[string]interface{})
		if !isMap {
			return
		}
		lit, hasLit := m["literal"]
		if !hasLit {
			return
		}
		s, isStr := lit.(string)
		if !isStr {
			return
		}
		key, ok = s, true
	}
	for _, e := range sel.All {
		walkAllElemLeaves(e, find)
	}
	for _, e := range sel.Any {
		walkAnyElemLeaves(e, find)
	}
	return key, ok
}
