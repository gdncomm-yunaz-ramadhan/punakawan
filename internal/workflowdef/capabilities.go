package workflowdef

// CapabilitySet is the set of capability identifiers a workflow definition is
// allowed to reference. There is no runtime capability registry to enumerate
// today (punokawan-dzt context): invokable capabilities come from two
// sources — the hardcoded MCP tool names registered in
// internal/mcpserver/tools.go, and the per-adapter manifest operations. This
// type unions both into a single membership check for Validate.
type CapabilitySet struct {
	names map[string]struct{}
}

// NewCapabilitySet builds a CapabilitySet from the known MCP tool names and
// the (dynamic) adapter operation identifiers. Duplicates across the two
// sources are harmless — membership is by set.
func NewCapabilitySet(mcpNames []string, adapterOps []string) CapabilitySet {
	names := make(map[string]struct{}, len(mcpNames)+len(adapterOps))
	for _, n := range mcpNames {
		names[n] = struct{}{}
	}
	for _, op := range adapterOps {
		names[op] = struct{}{}
	}
	return CapabilitySet{names: names}
}

// Has reports whether name is a registered capability in this set.
func (c CapabilitySet) Has(name string) bool {
	_, ok := c.names[name]
	return ok
}

// Len reports how many distinct capabilities the set contains.
func (c CapabilitySet) Len() int { return len(c.names) }

// KnownMCPCapabilities returns the hardcoded list of MCP tool names that a
// workflow step may reference as a capability.
//
// IMPORTANT: this slice is a hand-maintained mirror of the tool names
// registered in internal/mcpserver/tools.go (registerTools). There is no
// runtime registry that enumerates them, so this list MUST be kept in sync by
// hand whenever a tool is added to or removed from that file. The plan's
// example schema also uses higher-level capability identifiers (e.g.
// "knowledge.search", "jira.issue.search") that are NOT MCP tool names; those
// come from adapter manifests and are supplied to NewCapabilitySet as
// adapterOps rather than living here.
func KnownMCPCapabilities() []string {
	return []string{
		"build_context_dossier",
		"request_capsule",
		"submit_gareng_review",
		"submit_petruk_plan",
		"submit_semar_synthesis",
		"submit_bagong_review",
		"create_workflow_run",
		"get_workflow_state",
		"advance_workflow",
		"ingest_jira_requirement",
		"submit_task_graph",
		"list_ready_tasks",
		"claim_ready_task",
		"build_task_context",
		"start_task_execution",
		"finish_task_execution",
		"write_file",
		"bulk_create_files",
		"check_diff",
		"run_tests",
		"check_openapi_compatibility",
		"list_task_evidence",
		"commit_task",
		"push_task_branch",
		"create_pr",
		"review_pr",
		"submit_pr_review_findings",
		"fetch_unresolved_pr_comments",
		"resolve_review_thread",
		"report_discovered_task",
		"call_adapter_operation",
		"respond_to_adapter_approval",
		"request_jira_clarification",
		"check_jira_skippable",
		"sync_jira_subtasks",
		"update_jira_task_progress",
		"list_jira_sync_queue",
		"retry_jira_sync_entry",
		"submit_jira_assessment",
		"reopen_task",
		"search_knowledge",
		"submit_missing_context_request",
		"list_missing_context_requests",
		"resolve_missing_context_request",
		"delete_knowledge",
		"reset_project_knowledge",
	}
}
