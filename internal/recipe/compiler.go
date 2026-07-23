package recipe

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/ygrip/punakawan/pkg/protocol"
)

// CompiledQuery is §5's CompiledQuery: the evidence-ready, reproducible
// result of compiling a retrieval recipe's selector against a Jira
// provider. JQL is never built by string-concatenating user text - every
// clause's value goes through resolveClauseValue and formatJQLValue,
// which quote and escape it.
type CompiledQuery struct {
	JQL     string
	OrderBy string
	Fields  []string
	// Warnings records every approximation taken during compilation (for
	// example, an unconfigured Agile client forcing a futureSprints()
	// fallback) so a caller can surface them rather than silently trust a
	// guess.
	Warnings []string
	// Explanations is one human-readable line per compiled clause, in
	// selector order, per §5's "Punakawan can explain each condition."
	Explanations []string
}

// Board and Sprint are the minimal Jira Agile metadata the built-in
// jira.board_for_project/jira.next_sprint resolvers need.
type Board struct {
	ID   string
	Name string
}

type Sprint struct {
	ID    string
	Name  string
	State string // "active", "future", or "closed"
	// StartDate is nil when Jira hasn't scheduled the sprint yet; such a
	// sprint still sorts after any sprint with a known start date.
	StartDate *time.Time
}

// JiraAgileClient resolves board/sprint metadata for the built-in
// resolvers. No concrete implementation is wired yet: packages/
// adapter-atlassian has no board- or sprint-listing operation today, so
// connecting this to the real Jira Agile REST API is deferred to
// whichever later phase adds live execution (§15's RecipeExecutor) - an
// honest gap, not an oversight. A nil Agile client is valid: both
// resolvers degrade to an explicitly labeled futureSprints()
// approximation with a warning instead of failing outright.
type JiraAgileClient interface {
	BoardsForProject(ctx context.Context, projectKey string) ([]Board, error)
	Sprints(ctx context.Context, boardID string) ([]Sprint, error)
}

// ClarificationNeededError is returned when a resolver's answer is not
// unique and picking one anyway would be a guess dressed up as a
// compiled query, per Phase 2's exit criterion: strict "next" must
// resolve to one sprint ID or ask for clarification.
type ClarificationNeededError struct {
	Field   string
	Reason  string
	Options []string
}

func (e *ClarificationNeededError) Error() string {
	return fmt.Sprintf("recipe: compile: %s needs clarification: %s", e.Field, e.Reason)
}

// rawJQL marks a resolved value as an already-valid JQL fragment (a
// builtin function call such as futureSprints()) that must be inserted
// verbatim rather than quoted as a string literal.
type rawJQL string

// ResolverFunc resolves one `resolver:`-shaped selector value. warnings
// is shared with the whole Compile call so a resolver can append an
// approximation notice without owning its own reporting channel.
type ResolverFunc func(ctx context.Context, args map[string]interface{}, warnings *[]string) (interface{}, error)

// Compiler implements §5/§15's QueryCompiler/RecipeCompiler for the Jira
// provider: it turns a retrieval recipe's structured selector into a
// CompiledQuery, resolving any `resolver:`-shaped dynamic values along
// the way.
type Compiler struct {
	Agile     JiraAgileClient
	Resolvers map[string]ResolverFunc
}

// NewCompiler builds a Compiler with the built-in jira.board_for_project
// and jira.next_sprint resolvers registered. agile may be nil; see
// JiraAgileClient's doc comment for the resulting fallback behavior.
func NewCompiler(agile JiraAgileClient) *Compiler {
	c := &Compiler{Agile: agile}
	c.Resolvers = map[string]ResolverFunc{
		"jira.board_for_project": c.resolveBoardForProject,
		"jira.next_sprint":       c.resolveNextSprint,
	}
	return c
}

// fieldOperators is a deliberately minimal allow-list covering the
// fields this plan's own Jira example exercises plus the handful of
// other fields a next-sprint-style issue search commonly needs. It is
// not a full catalog of every real Jira field - a provider needing a
// field outside this list is a scope extension, not a silent one.
var fieldOperators = map[string]map[string]bool{
	"project":   {"equals": true, "not_equals": true, "in": true, "not_in": true},
	"component": {"equals": true, "not_equals": true, "in": true, "not_in": true, "contains": true},
	"summary":   {"contains": true, "phrase_contains": true},
	"sprint":    {"equals": true, "not_equals": true, "in": true, "not_in": true},
	"status":    {"equals": true, "not_equals": true, "in": true, "not_in": true},
	"assignee":  {"equals": true, "not_equals": true},
	"priority":  {"equals": true, "not_equals": true, "in": true, "not_in": true},
	"key":       {"equals": true, "in": true},
}

var jqlOperators = map[string]string{
	"equals":          "=",
	"not_equals":      "!=",
	"contains":        "~",
	"phrase_contains": "~",
	"in":              "in",
	"not_in":          "not in",
	"greater_than":    ">",
	"less_than":       "<",
}

var orderingFields = map[string]bool{
	"rank":     true,
	"created":  true,
	"updated":  true,
	"priority": true,
	"key":      true,
}

// Compile turns rr's selector/ordering/output into a CompiledQuery.
// bindings supplies values for rr.Inputs; Compile only checks that every
// required input has a binding present - the schema's selector value
// oneOf (literal or resolver) has no input-reference shape yet, so
// threading bindings into clause values themselves is not implemented
// here and is left for a later phase to define, rather than inventing an
// undocumented convention now.
func (c *Compiler) Compile(ctx context.Context, rr *protocol.KnowledgeRecordRetrievalRecipe, bindings map[string]interface{}) (CompiledQuery, error) {
	if rr == nil {
		return CompiledQuery{}, fmt.Errorf("recipe: compile: recipe has no retrieval_recipe body")
	}
	if err := c.checkRequiredInputs(rr, bindings); err != nil {
		return CompiledQuery{}, err
	}

	var warnings []string
	var clauses []string
	var explanations []string

	for _, elem := range rr.Selector.All {
		frag, expl, err := c.compileAllElem(ctx, elem, &warnings)
		if err != nil {
			return CompiledQuery{}, err
		}
		clauses = append(clauses, frag)
		explanations = append(explanations, expl...)
	}

	var anyFrags []string
	for _, elem := range rr.Selector.Any {
		frag, expl, err := c.compileAnyElem(ctx, elem, &warnings)
		if err != nil {
			return CompiledQuery{}, err
		}
		anyFrags = append(anyFrags, frag)
		explanations = append(explanations, expl...)
	}
	if len(anyFrags) > 0 {
		clauses = append(clauses, "("+strings.Join(anyFrags, " OR ")+")")
	}
	if len(clauses) == 0 {
		return CompiledQuery{}, fmt.Errorf("recipe: compile: selector has no clauses")
	}

	orderBy, err := compileOrdering(rr.Ordering)
	if err != nil {
		return CompiledQuery{}, err
	}

	return CompiledQuery{
		JQL:          strings.Join(clauses, " AND "),
		OrderBy:      orderBy,
		Fields:       rr.Output.Fields,
		Warnings:     warnings,
		Explanations: explanations,
	}, nil
}

func (c *Compiler) checkRequiredInputs(rr *protocol.KnowledgeRecordRetrievalRecipe, bindings map[string]interface{}) error {
	for _, in := range rr.Inputs {
		if in.Required == nil || !*in.Required {
			continue
		}
		if _, ok := bindings[in.Name]; !ok {
			return fmt.Errorf("recipe: compile: required input %q has no binding", in.Name)
		}
	}
	return nil
}

func compileOrdering(ordering []protocol.KnowledgeRecordRetrievalRecipeOrderingElem) (string, error) {
	if len(ordering) == 0 {
		return "", nil
	}
	parts := make([]string, 0, len(ordering))
	for _, o := range ordering {
		if !orderingFields[o.Field] {
			return "", fmt.Errorf("recipe: compile: unsupported ordering field %q", o.Field)
		}
		dir := "ASC"
		if o.Direction == protocol.KnowledgeRecordRetrievalRecipeOrderingElemDirectionDescending {
			dir = "DESC"
		}
		parts = append(parts, fmt.Sprintf("%s %s", o.Field, dir))
	}
	return strings.Join(parts, ", "), nil
}

// opString extracts a level-specific *Operator pointer's underlying
// string, or "" if the clause left it unset. Every generated Operator
// type shares the "equals"/"not_equals"/... value set (duplicated per
// nesting level rather than $ref'd, per the schema's own doc comment),
// so one generic function covers all of them.
func opString[T ~string](p *T) string {
	if p == nil {
		return ""
	}
	return string(*p)
}

func (c *Compiler) compileAllElem(ctx context.Context, e protocol.KnowledgeRecordRetrievalRecipeSelectorAllElem, warnings *[]string) (string, []string, error) {
	if e.Field != nil {
		frag, expl, err := c.compileLeaf(ctx, *e.Field, opString(e.Operator), e.Value, warnings)
		if err != nil {
			return "", nil, err
		}
		return frag, []string{expl}, nil
	}

	var parts []string
	var explanations []string
	if len(e.All) > 0 {
		var sub []string
		for _, leaf := range e.All {
			if leaf.Field == nil {
				return "", nil, fmt.Errorf("recipe: compile: nested all clause is missing a field")
			}
			frag, expl, err := c.compileLeaf(ctx, *leaf.Field, opString(leaf.Operator), leaf.Value, warnings)
			if err != nil {
				return "", nil, err
			}
			sub = append(sub, frag)
			explanations = append(explanations, expl)
		}
		parts = append(parts, "("+strings.Join(sub, " AND ")+")")
	}
	if len(e.Any) > 0 {
		var sub []string
		for _, leaf := range e.Any {
			if leaf.Field == nil {
				return "", nil, fmt.Errorf("recipe: compile: nested any clause is missing a field")
			}
			frag, expl, err := c.compileLeaf(ctx, *leaf.Field, opString(leaf.Operator), leaf.Value, warnings)
			if err != nil {
				return "", nil, err
			}
			sub = append(sub, frag)
			explanations = append(explanations, expl)
		}
		parts = append(parts, "("+strings.Join(sub, " OR ")+")")
	}
	if len(parts) == 0 {
		return "", nil, fmt.Errorf("recipe: compile: selector clause has neither a field nor a nested all/any group")
	}
	return strings.Join(parts, " AND "), explanations, nil
}

func (c *Compiler) compileAnyElem(ctx context.Context, e protocol.KnowledgeRecordRetrievalRecipeSelectorAnyElem, warnings *[]string) (string, []string, error) {
	if e.Field != nil {
		frag, expl, err := c.compileLeaf(ctx, *e.Field, opString(e.Operator), e.Value, warnings)
		if err != nil {
			return "", nil, err
		}
		return frag, []string{expl}, nil
	}

	var parts []string
	var explanations []string
	if len(e.All) > 0 {
		var sub []string
		for _, leaf := range e.All {
			if leaf.Field == nil {
				return "", nil, fmt.Errorf("recipe: compile: nested all clause is missing a field")
			}
			frag, expl, err := c.compileLeaf(ctx, *leaf.Field, opString(leaf.Operator), leaf.Value, warnings)
			if err != nil {
				return "", nil, err
			}
			sub = append(sub, frag)
			explanations = append(explanations, expl)
		}
		parts = append(parts, "("+strings.Join(sub, " AND ")+")")
	}
	if len(e.Any) > 0 {
		var sub []string
		for _, leaf := range e.Any {
			if leaf.Field == nil {
				return "", nil, fmt.Errorf("recipe: compile: nested any clause is missing a field")
			}
			frag, expl, err := c.compileLeaf(ctx, *leaf.Field, opString(leaf.Operator), leaf.Value, warnings)
			if err != nil {
				return "", nil, err
			}
			sub = append(sub, frag)
			explanations = append(explanations, expl)
		}
		parts = append(parts, "("+strings.Join(sub, " OR ")+")")
	}
	if len(parts) == 0 {
		return "", nil, fmt.Errorf("recipe: compile: selector clause has neither a field nor a nested all/any group")
	}
	return strings.Join(parts, " AND "), explanations, nil
}

func (c *Compiler) compileLeaf(ctx context.Context, field, operator string, value interface{}, warnings *[]string) (string, string, error) {
	ops, ok := fieldOperators[field]
	if !ok {
		return "", "", fmt.Errorf("recipe: compile: unsupported selector field %q", field)
	}
	if operator == "" {
		return "", "", fmt.Errorf("recipe: compile: clause for field %q is missing an operator", field)
	}
	if !ops[operator] {
		return "", "", fmt.Errorf("recipe: compile: operator %q is not supported for field %q", operator, field)
	}
	jqlOp, ok := jqlOperators[operator]
	if !ok {
		return "", "", fmt.Errorf("recipe: compile: no JQL mapping for operator %q", operator)
	}

	resolved, resolverName, err := c.resolveClauseValue(ctx, value, warnings)
	if err != nil {
		return "", "", err
	}

	var valueText string
	switch operator {
	case "in", "not_in":
		valueText, err = formatJQLList(resolved)
	case "phrase_contains":
		s, ok := resolved.(string)
		if !ok {
			return "", "", fmt.Errorf("recipe: compile: field %q operator phrase_contains requires a string value, got %T", field, resolved)
		}
		valueText = quoteJQL(`"` + s + `"`)
	default:
		valueText, err = formatJQLValue(resolved)
	}
	if err != nil {
		return "", "", err
	}

	fragment := fmt.Sprintf("%s %s %s", field, jqlOp, valueText)
	explanation := fmt.Sprintf("%s %s %v", field, operator, resolved)
	if resolverName != "" {
		explanation += fmt.Sprintf(" (resolved via %s)", resolverName)
	}
	return fragment, explanation, nil
}

// resolveClauseValue resolves a top-level selector `value` field, which
// the schema requires to be either {literal: ...} or {resolver: ...,
// arguments: ...} (§5's oneOf). It returns the resolver name used, or ""
// for a literal, so callers can annotate their explanation.
func (c *Compiler) resolveClauseValue(ctx context.Context, value interface{}, warnings *[]string) (interface{}, string, error) {
	m, ok := value.(map[string]interface{})
	if !ok {
		return nil, "", fmt.Errorf("recipe: compile: clause value must be an object with a literal or resolver key, got %#v", value)
	}
	if lit, has := m["literal"]; has {
		return lit, "", nil
	}
	name, has := m["resolver"].(string)
	if !has {
		return nil, "", fmt.Errorf("recipe: compile: clause value has neither a literal nor a resolver key: %#v", value)
	}
	resolved, err := c.callResolver(ctx, name, m, warnings)
	if err != nil {
		return nil, "", err
	}
	return resolved, name, nil
}

// resolveArgument resolves one entry of a resolver call's `arguments`
// object. Unlike a top-level clause value, an argument may be a bare
// literal (the schema's `arguments` is a freeform object, not the
// value oneOf) or itself a nested {resolver: ..., arguments: ...} call,
// per the plan's jira.next_sprint(board: jira.board_for_project(...))
// example.
func (c *Compiler) resolveArgument(ctx context.Context, v interface{}, warnings *[]string) (interface{}, error) {
	m, ok := v.(map[string]interface{})
	if !ok {
		return v, nil
	}
	name, has := m["resolver"].(string)
	if !has {
		return v, nil
	}
	return c.callResolver(ctx, name, m, warnings)
}

func (c *Compiler) callResolver(ctx context.Context, name string, m map[string]interface{}, warnings *[]string) (interface{}, error) {
	fn, ok := c.Resolvers[name]
	if !ok {
		return nil, fmt.Errorf("recipe: compile: unknown resolver %q", name)
	}
	argsRaw, _ := m["arguments"].(map[string]interface{})
	args := make(map[string]interface{}, len(argsRaw))
	for k, raw := range argsRaw {
		resolvedArg, err := c.resolveArgument(ctx, raw, warnings)
		if err != nil {
			return nil, fmt.Errorf("recipe: compile: resolver %q argument %q: %w", name, k, err)
		}
		args[k] = resolvedArg
	}
	resolved, err := fn(ctx, args, warnings)
	if err != nil {
		return nil, fmt.Errorf("recipe: compile: resolver %q: %w", name, err)
	}
	return resolved, nil
}

func (c *Compiler) resolveBoardForProject(ctx context.Context, args map[string]interface{}, warnings *[]string) (interface{}, error) {
	projectKey, _ := args["project_key"].(string)
	if projectKey == "" {
		return nil, fmt.Errorf("jira.board_for_project: project_key argument is required")
	}
	if c.Agile == nil {
		return nil, fmt.Errorf("jira.board_for_project: no Jira Agile client configured")
	}
	boards, err := c.Agile.BoardsForProject(ctx, projectKey)
	if err != nil {
		return nil, err
	}
	if len(boards) == 0 {
		return nil, fmt.Errorf("jira.board_for_project: no board found for project %q", projectKey)
	}
	if len(boards) > 1 {
		options := make([]string, len(boards))
		for i, b := range boards {
			options[i] = fmt.Sprintf("%s (%s)", b.Name, b.ID)
		}
		return nil, &ClarificationNeededError{
			Field:   "board",
			Reason:  fmt.Sprintf("project %q has %d boards", projectKey, len(boards)),
			Options: options,
		}
	}
	return boards[0].ID, nil
}

func (c *Compiler) resolveNextSprint(ctx context.Context, args map[string]interface{}, warnings *[]string) (interface{}, error) {
	boardID, _ := args["board"].(string)
	if boardID == "" {
		return nil, fmt.Errorf("jira.next_sprint: board argument is required")
	}
	if c.Agile == nil {
		*warnings = append(*warnings, "jira.next_sprint: no Jira Agile client configured; falling back to futureSprints() approximation")
		return rawJQL("futureSprints()"), nil
	}

	sprints, err := c.Agile.Sprints(ctx, boardID)
	if err != nil {
		return nil, err
	}
	var candidates []Sprint
	for _, s := range sprints {
		if s.State == "active" || s.State == "future" {
			candidates = append(candidates, s)
		}
	}
	if len(candidates) == 0 {
		*warnings = append(*warnings, fmt.Sprintf("jira.next_sprint: board %q has no active or future sprint; falling back to futureSprints() approximation", boardID))
		return rawJQL("futureSprints()"), nil
	}

	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].StartDate == nil {
			return false
		}
		if candidates[j].StartDate == nil {
			return true
		}
		return candidates[i].StartDate.Before(*candidates[j].StartDate)
	})
	if len(candidates) > 1 &&
		candidates[0].StartDate != nil && candidates[1].StartDate != nil &&
		candidates[0].StartDate.Equal(*candidates[1].StartDate) {
		return nil, &ClarificationNeededError{
			Field:   "sprint",
			Reason:  fmt.Sprintf("board %q has multiple sprints starting %s", boardID, candidates[0].StartDate.Format(time.RFC3339)),
			Options: []string{candidates[0].Name, candidates[1].Name},
		}
	}
	return candidates[0].ID, nil
}

func formatJQLValue(v interface{}) (string, error) {
	switch t := v.(type) {
	case rawJQL:
		return string(t), nil
	case string:
		return quoteJQL(t), nil
	case bool:
		return strconv.FormatBool(t), nil
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64), nil
	case int:
		return strconv.Itoa(t), nil
	case nil:
		return "", fmt.Errorf("recipe: compile: value resolved to nil")
	default:
		return "", fmt.Errorf("recipe: compile: unsupported resolved value type %T", v)
	}
}

func formatJQLList(v interface{}) (string, error) {
	var items []interface{}
	switch t := v.(type) {
	case []interface{}:
		items = t
	case []string:
		for _, s := range t {
			items = append(items, s)
		}
	default:
		items = []interface{}{v}
	}
	if len(items) == 0 {
		return "", fmt.Errorf("recipe: compile: in/not_in value resolved to an empty list")
	}
	parts := make([]string, len(items))
	for i, item := range items {
		s, err := formatJQLValue(item)
		if err != nil {
			return "", err
		}
		parts[i] = s
	}
	return "(" + strings.Join(parts, ", ") + ")", nil
}

// quoteJQL escapes s for safe use as a JQL string literal (backslash and
// double-quote), matching packages/adapter-atlassian's quoteJql helper.
// This is the mechanism behind §5's "never concatenate unescaped user
// text directly into JQL."
func quoteJQL(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		if r == '"' || r == '\\' {
			b.WriteByte('\\')
		}
		b.WriteRune(r)
	}
	b.WriteByte('"')
	return b.String()
}
