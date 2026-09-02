package agent

import "testing"

type fakeChecker struct {
	known map[string]bool
}

func (c fakeChecker) Has(name string) bool { return c.known[name] }

func TestValidateDuplicateID(t *testing.T) {
	specs := []RoleSpec{
		{ID: "bagong"},
		{ID: "bagong"},
	}
	if err := Validate(specs, nil, nil); err == nil {
		t.Fatalf("Validate: got nil error, want duplicate-id error")
	}
}

func TestValidateUnknownOutputSchema(t *testing.T) {
	specs := []RoleSpec{
		{ID: "bagong", OutputSchemaID: "not_a_real_schema"},
	}
	checker := fakeChecker{known: map[string]bool{"bagong_review": true}}
	if err := Validate(specs, checker, nil); err == nil {
		t.Fatalf("Validate: got nil error, want unknown-output-schema error")
	}
}

func TestValidateUnknownToolName(t *testing.T) {
	specs := []RoleSpec{
		{ID: "gareng", ToolPolicy: ToolPolicy{AllowedTools: []string{"plan_get", "not_a_real_tool"}}},
	}
	checker := fakeChecker{known: map[string]bool{"plan_get": true}}
	if err := Validate(specs, nil, checker); err == nil {
		t.Fatalf("Validate: got nil error, want unknown-tool error")
	}
}

func TestValidateUnknownDeniedToolName(t *testing.T) {
	specs := []RoleSpec{
		{ID: "gareng", ToolPolicy: ToolPolicy{DeniedTools: []string{"not_a_real_tool"}}},
	}
	checker := fakeChecker{known: map[string]bool{"plan_get": true}}
	if err := Validate(specs, nil, checker); err == nil {
		t.Fatalf("Validate: got nil error, want unknown-denied-tool error")
	}
}

func TestValidateNilCheckersSkipChecks(t *testing.T) {
	specs := []RoleSpec{
		{ID: "bagong", OutputSchemaID: "whatever", ToolPolicy: ToolPolicy{AllowedTools: []string{"whatever_tool"}}},
	}
	if err := Validate(specs, nil, nil); err != nil {
		t.Fatalf("Validate with nil checkers: %v, want nil", err)
	}
}

func TestValidateValidSpecsPass(t *testing.T) {
	specs := []RoleSpec{
		{ID: "bagong", OutputSchemaID: "bagong_review", ToolPolicy: ToolPolicy{AllowedTools: []string{"plan_get"}}},
		{ID: "gareng", OutputSchemaID: "gareng_review", ToolPolicy: ToolPolicy{AllowedTools: []string{"plan_get"}}},
	}
	schemaChecker := fakeChecker{known: map[string]bool{"bagong_review": true, "gareng_review": true}}
	capChecker := fakeChecker{known: map[string]bool{"plan_get": true}}
	if err := Validate(specs, schemaChecker, capChecker); err != nil {
		t.Fatalf("Validate valid specs: %v, want nil", err)
	}
}

func TestKnowledgeSchemaCheckerAgainstRealSchema(t *testing.T) {
	checker, err := NewKnowledgeSchemaChecker()
	if err != nil {
		t.Fatalf("NewKnowledgeSchemaChecker: %v", err)
	}
	for _, id := range []string{"gareng_review", "petruk_plan", "bagong_review", "final_plan"} {
		if !checker.Has(id) {
			t.Errorf("checker.Has(%q) = false, want true", id)
		}
	}
	if checker.Has("not_a_real_schema_key") {
		t.Errorf("checker.Has(not_a_real_schema_key) = true, want false")
	}
}
