package recipe

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/ygrip/punakawan/pkg/protocol"
)

func literalValue(v interface{}) interface{} {
	return map[string]interface{}{"literal": v}
}

func resolverValue(name string, args map[string]interface{}) interface{} {
	return map[string]interface{}{"resolver": name, "arguments": args}
}

func strField(s string) *string { return &s }

func allEquals(field string, value interface{}) protocol.KnowledgeRecordRetrievalRecipeSelectorAllElem {
	op := protocol.KnowledgeRecordRetrievalRecipeSelectorAllElemOperatorEquals
	return protocol.KnowledgeRecordRetrievalRecipeSelectorAllElem{Field: strField(field), Operator: &op, Value: value}
}

func baseRecipe(selector protocol.KnowledgeRecordRetrievalRecipeSelector) *protocol.KnowledgeRecordRetrievalRecipe {
	return &protocol.KnowledgeRecordRetrievalRecipe{
		Capability: "jira.issue.search",
		Intent:     "project.next-sprint.issues",
		Provider:   "jira",
		Resource:   "issue",
		Operation:  "search",
		ReadOnly:   true,
		Selector:   selector,
		Output: protocol.KnowledgeRecordRetrievalRecipeOutput{
			EntityType:    "jira_issue",
			IdentityField: "key",
			Fields:        []string{"key", "summary"},
		},
	}
}

func TestCompileSimpleLiteralClause(t *testing.T) {
	rr := baseRecipe(protocol.KnowledgeRecordRetrievalRecipeSelector{
		All: []protocol.KnowledgeRecordRetrievalRecipeSelectorAllElem{
			allEquals("project", literalValue("TRF")),
		},
	})

	c := NewCompiler(nil)
	got, err := c.Compile(context.Background(), rr, nil)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if got.JQL != `project = "TRF"` {
		t.Fatalf("JQL = %q, want project = \"TRF\"", got.JQL)
	}
	if len(got.Explanations) != 1 {
		t.Fatalf("Explanations = %v, want 1 entry", got.Explanations)
	}
}

func TestCompileEscapesUserSuppliedValue(t *testing.T) {
	malicious := `TRF" OR project = "OTHER`
	rr := baseRecipe(protocol.KnowledgeRecordRetrievalRecipeSelector{
		All: []protocol.KnowledgeRecordRetrievalRecipeSelectorAllElem{
			allEquals("project", literalValue(malicious)),
		},
	})

	c := NewCompiler(nil)
	got, err := c.Compile(context.Background(), rr, nil)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	want := `project = "TRF\" OR project = \"OTHER"`
	if got.JQL != want {
		t.Fatalf("JQL = %q, want %q (value must stay quoted/escaped, never concatenated raw)", got.JQL, want)
	}
}

func TestCompileNestedAnyGroupInsideAllClause(t *testing.T) {
	anyOp := protocol.KnowledgeRecordRetrievalRecipeSelectorAllElemAnyElemOperatorPhraseContains
	rr := baseRecipe(protocol.KnowledgeRecordRetrievalRecipeSelector{
		All: []protocol.KnowledgeRecordRetrievalRecipeSelectorAllElem{
			allEquals("project", literalValue("TRF")),
			{
				Any: []protocol.KnowledgeRecordRetrievalRecipeSelectorAllElemAnyElem{
					{Field: strField("summary"), Operator: &anyOp, Value: literalValue("AFFILIATE PLATFORM")},
				},
			},
		},
	})

	c := NewCompiler(nil)
	got, err := c.Compile(context.Background(), rr, nil)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	want := `project = "TRF" AND (summary ~ "\"AFFILIATE PLATFORM\"")`
	if got.JQL != want {
		t.Fatalf("JQL = %q, want %q", got.JQL, want)
	}
}

func TestCompileOrderingAndOutputFields(t *testing.T) {
	rr := baseRecipe(protocol.KnowledgeRecordRetrievalRecipeSelector{
		All: []protocol.KnowledgeRecordRetrievalRecipeSelectorAllElem{allEquals("project", literalValue("TRF"))},
	})
	rr.Ordering = []protocol.KnowledgeRecordRetrievalRecipeOrderingElem{
		{Field: "rank", Direction: protocol.KnowledgeRecordRetrievalRecipeOrderingElemDirectionAscending},
	}

	c := NewCompiler(nil)
	got, err := c.Compile(context.Background(), rr, nil)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if got.OrderBy != "rank ASC" {
		t.Fatalf("OrderBy = %q, want %q", got.OrderBy, "rank ASC")
	}
	if len(got.Fields) != 2 || got.Fields[0] != "key" {
		t.Fatalf("Fields = %v, want [key summary]", got.Fields)
	}
}

func TestCompileRejectsUnsupportedField(t *testing.T) {
	rr := baseRecipe(protocol.KnowledgeRecordRetrievalRecipeSelector{
		All: []protocol.KnowledgeRecordRetrievalRecipeSelectorAllElem{allEquals("not_a_real_field", literalValue("x"))},
	})

	c := NewCompiler(nil)
	if _, err := c.Compile(context.Background(), rr, nil); err == nil {
		t.Fatal("Compile: want error for an unsupported field, got nil")
	}
}

func TestCompileRejectsOperatorNotValidForField(t *testing.T) {
	op := protocol.KnowledgeRecordRetrievalRecipeSelectorAllElemOperatorGreaterThan
	rr := baseRecipe(protocol.KnowledgeRecordRetrievalRecipeSelector{
		All: []protocol.KnowledgeRecordRetrievalRecipeSelectorAllElem{
			{Field: strField("project"), Operator: &op, Value: literalValue("TRF")},
		},
	})

	c := NewCompiler(nil)
	if _, err := c.Compile(context.Background(), rr, nil); err == nil {
		t.Fatal("Compile: want error for greater_than on project, got nil")
	}
}

func TestCompileRequiresLiteralOrResolverValueShape(t *testing.T) {
	op := protocol.KnowledgeRecordRetrievalRecipeSelectorAllElemOperatorEquals
	rr := baseRecipe(protocol.KnowledgeRecordRetrievalRecipeSelector{
		All: []protocol.KnowledgeRecordRetrievalRecipeSelectorAllElem{
			{Field: strField("project"), Operator: &op, Value: "TRF"},
		},
	})

	c := NewCompiler(nil)
	if _, err := c.Compile(context.Background(), rr, nil); err == nil {
		t.Fatal("Compile: want error for a bare (unwrapped) clause value, got nil")
	}
}

func TestCompileRequiredInputWithoutBindingFails(t *testing.T) {
	rr := baseRecipe(protocol.KnowledgeRecordRetrievalRecipeSelector{
		All: []protocol.KnowledgeRecordRetrievalRecipeSelectorAllElem{allEquals("project", literalValue("TRF"))},
	})
	required := true
	rr.Inputs = []protocol.KnowledgeRecordRetrievalRecipeInputsElem{
		{Name: "sprint_selector", Type: "sprint_selector", Required: &required},
	}

	c := NewCompiler(nil)
	if _, err := c.Compile(context.Background(), rr, nil); err == nil {
		t.Fatal("Compile: want error for a missing required input binding, got nil")
	}
	if _, err := c.Compile(context.Background(), rr, map[string]interface{}{"sprint_selector": "next"}); err != nil {
		t.Fatalf("Compile with binding present: %v", err)
	}
}

// fakeAgile is a JiraAgileClient test double.
type fakeAgile struct {
	boards     map[string][]Board
	sprints    map[string][]Sprint
	boardsErr  error
	sprintsErr error
}

func (f *fakeAgile) BoardsForProject(ctx context.Context, projectKey string) ([]Board, error) {
	if f.boardsErr != nil {
		return nil, f.boardsErr
	}
	return f.boards[projectKey], nil
}

func (f *fakeAgile) Sprints(ctx context.Context, boardID string) ([]Sprint, error) {
	if f.sprintsErr != nil {
		return nil, f.sprintsErr
	}
	return f.sprints[boardID], nil
}

func TestCompileResolvesNestedBoardAndSprintResolvers(t *testing.T) {
	start := time.Date(2026, 8, 3, 0, 0, 0, 0, time.UTC)
	agile := &fakeAgile{
		boards: map[string][]Board{"TRF": {{ID: "board-1", Name: "TRF board"}}},
		sprints: map[string][]Sprint{
			"board-1": {
				{ID: "sprint-9", Name: "Sprint 9", State: "closed"},
				{ID: "sprint-10", Name: "Sprint 10", State: "active", StartDate: &start},
				{ID: "sprint-11", Name: "Sprint 11", State: "future", StartDate: timePtr(start.AddDate(0, 0, 14))},
			},
		},
	}

	op := protocol.KnowledgeRecordRetrievalRecipeSelectorAllElemOperatorEquals
	rr := baseRecipe(protocol.KnowledgeRecordRetrievalRecipeSelector{
		All: []protocol.KnowledgeRecordRetrievalRecipeSelectorAllElem{
			{
				Field:    strField("sprint"),
				Operator: &op,
				Value: resolverValue("jira.next_sprint", map[string]interface{}{
					"board": resolverValue("jira.board_for_project", map[string]interface{}{"project_key": "TRF"}),
				}),
			},
		},
	})

	c := NewCompiler(agile)
	got, err := c.Compile(context.Background(), rr, nil)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	want := `sprint = "sprint-10"`
	if got.JQL != want {
		t.Fatalf("JQL = %q, want %q", got.JQL, want)
	}
	if len(got.Warnings) != 0 {
		t.Fatalf("Warnings = %v, want none when the Agile client resolves a unique sprint", got.Warnings)
	}
	if !strings.Contains(got.Explanations[0], "resolved via jira.next_sprint") {
		t.Fatalf("Explanations[0] = %q, want a mention of jira.next_sprint", got.Explanations[0])
	}
}

func TestCompileFallsBackToFutureSprintsWithoutAgileClient(t *testing.T) {
	op := protocol.KnowledgeRecordRetrievalRecipeSelectorAllElemOperatorEquals
	rr := baseRecipe(protocol.KnowledgeRecordRetrievalRecipeSelector{
		All: []protocol.KnowledgeRecordRetrievalRecipeSelectorAllElem{
			{
				Field:    strField("sprint"),
				Operator: &op,
				Value:    resolverValue("jira.next_sprint", map[string]interface{}{"board": "board-1"}),
			},
		},
	})

	c := NewCompiler(nil)
	got, err := c.Compile(context.Background(), rr, nil)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if got.JQL != "sprint = futureSprints()" {
		t.Fatalf("JQL = %q, want the unquoted futureSprints() builtin", got.JQL)
	}
	if len(got.Warnings) != 1 {
		t.Fatalf("Warnings = %v, want exactly one approximation warning", got.Warnings)
	}
}

func TestCompileAsksForClarificationOnAmbiguousBoard(t *testing.T) {
	agile := &fakeAgile{
		boards: map[string][]Board{"TRF": {
			{ID: "board-1", Name: "TRF board"},
			{ID: "board-2", Name: "TRF secondary board"},
		}},
	}

	op := protocol.KnowledgeRecordRetrievalRecipeSelectorAllElemOperatorEquals
	rr := baseRecipe(protocol.KnowledgeRecordRetrievalRecipeSelector{
		All: []protocol.KnowledgeRecordRetrievalRecipeSelectorAllElem{
			{
				Field:    strField("sprint"),
				Operator: &op,
				Value: resolverValue("jira.next_sprint", map[string]interface{}{
					"board": resolverValue("jira.board_for_project", map[string]interface{}{"project_key": "TRF"}),
				}),
			},
		},
	})

	c := NewCompiler(agile)
	_, err := c.Compile(context.Background(), rr, nil)
	if err == nil {
		t.Fatal("Compile: want a ClarificationNeededError for an ambiguous board, got nil")
	}
	var clarify *ClarificationNeededError
	if !errors.As(err, &clarify) {
		t.Fatalf("Compile err = %v, want it to wrap *ClarificationNeededError", err)
	}
	if clarify.Field != "board" {
		t.Fatalf("clarify.Field = %q, want board", clarify.Field)
	}
}

func TestCompileAsksForClarificationOnTiedNextSprint(t *testing.T) {
	start := time.Date(2026, 8, 3, 0, 0, 0, 0, time.UTC)
	agile := &fakeAgile{
		sprints: map[string][]Sprint{
			"board-1": {
				{ID: "sprint-10", Name: "Sprint 10", State: "future", StartDate: &start},
				{ID: "sprint-10b", Name: "Sprint 10 (parallel)", State: "future", StartDate: &start},
			},
		},
	}

	op := protocol.KnowledgeRecordRetrievalRecipeSelectorAllElemOperatorEquals
	rr := baseRecipe(protocol.KnowledgeRecordRetrievalRecipeSelector{
		All: []protocol.KnowledgeRecordRetrievalRecipeSelectorAllElem{
			{
				Field:    strField("sprint"),
				Operator: &op,
				Value:    resolverValue("jira.next_sprint", map[string]interface{}{"board": "board-1"}),
			},
		},
	})

	c := NewCompiler(agile)
	_, err := c.Compile(context.Background(), rr, nil)
	var clarify *ClarificationNeededError
	if !errors.As(err, &clarify) {
		t.Fatalf("Compile err = %v, want it to wrap *ClarificationNeededError", err)
	}
	if clarify.Field != "sprint" {
		t.Fatalf("clarify.Field = %q, want sprint", clarify.Field)
	}
}

func TestCompileInOperatorFormatsList(t *testing.T) {
	op := protocol.KnowledgeRecordRetrievalRecipeSelectorAllElemOperatorIn
	rr := baseRecipe(protocol.KnowledgeRecordRetrievalRecipeSelector{
		All: []protocol.KnowledgeRecordRetrievalRecipeSelectorAllElem{
			{
				Field:    strField("status"),
				Operator: &op,
				Value:    resolverValue("test.status_list", nil),
			},
		},
	})

	c := NewCompiler(nil)
	c.Resolvers["test.status_list"] = func(ctx context.Context, args map[string]interface{}, warnings *[]string) (interface{}, error) {
		return []interface{}{"Open", "In Progress"}, nil
	}

	got, err := c.Compile(context.Background(), rr, nil)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	want := `status in ("Open", "In Progress")`
	if got.JQL != want {
		t.Fatalf("JQL = %q, want %q", got.JQL, want)
	}
}

func timePtr(t time.Time) *time.Time { return &t }
