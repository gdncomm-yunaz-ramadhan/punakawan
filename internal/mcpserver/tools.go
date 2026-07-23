package mcpserver

import (
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ygrip/punakawan/internal/app"
)

// approvalGateNote is appended to every tool description whose handler can
// trigger a write-approval gate (punokawan-7wv: gate mechanics were only
// documented in the server's Instructions blob, not on the specific tool
// that hits the gate). Kept short and shared rather than repeating
// call_adapter_operation's full explanation on each one.
const approvalGateNote = " Writes elicit one human approval for the whole run (see call_adapter_operation); unsupported clients must show the user Approve/Deny and call respond_to_adapter_approval."

// registerTools adds the data-operation tools defined in §28.4, plus
// create_workflow_run: §28.4 lists get_workflow_state/advance_workflow but
// not a way to start a run in the first place, and the server cannot
// function without one, so this is a necessary addition beyond the plan's
// literal tool list rather than an unstated one.
func registerTools(server *mcp.Server, a *app.App) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "build_context_dossier",
		Description: "Assemble the §9.1 context dossier from workspace, git, and durable knowledge state. No reasoning is performed.",
	}, buildContextDossierHandler(a))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "request_capsule",
		Description: "Build and persist an immutable, digested ContextCapsule for one Gareng/Petruk/Bagong invocation (architecture-enhancement-plan.md §6). Rejects requirement_ids/knowledge_ids whose record type is another role's output (e.g. bagong cannot cite a petruk-plan record) and allowed_tools entries a role must not have (e.g. bagong cannot be granted write_file). Set retrieval_query to also run Semar's automatic knowledge-retrieval pipeline (§11/§6.4, AEP-M7): search_knowledge's full ranking against that query, filtered to what this role may receive and to token_budget, added alongside any explicit knowledge_ids with each item's match explanation recorded as its reason. Call this before submit_gareng_review/submit_petruk_plan/submit_bagong_review, which require the returned id as capsule_id.",
	}, requestCapsuleHandler(a))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "submit_gareng_review",
		Description: "Validate and persist a Gareng feasibility/risk review (§8.2) as durable knowledge. Requires capsule_id from a prior request_capsule call for role gareng.",
	}, submitGarengReviewHandler(a))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "submit_petruk_plan",
		Description: "Validate and persist a Petruk implementation-planning output (§8.3) as durable knowledge. Requires capsule_id from a prior request_capsule call for role petruk.",
	}, submitPetrukPlanHandler(a))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "submit_semar_synthesis",
		Description: "Validate and persist Semar's consolidated clarification (§8.1/§9.2) or final plan (§9.3) as durable knowledge. Exactly one of synthesis or final_plan must be set.",
	}, submitSemarSynthesisHandler(a))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "submit_bagong_review",
		Description: "Validate and persist a Bagong independent final review (§8.4) as durable knowledge. Requires capsule_id from a prior request_capsule call for role bagong.",
	}, submitBagongReviewHandler(a))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "create_workflow_run",
		Description: "Start a new workflow run in state \"created\" (§18.1).",
	}, createWorkflowRunHandler(a))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_workflow_state",
		Description: "Read a workflow run's current state and checkpoint history (§18.1).",
	}, getWorkflowStateHandler(a))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "advance_workflow",
		Description: "Transition a workflow run to a new state, appending a checkpoint (§18.1). Valid next_state values: created, context-building, awaiting-clarification, planning, awaiting-approval, executing, reviewing, blocked, completed, failed, cancelled. Only §9's transition graph is accepted from the current state (e.g. created cannot jump straight to completed); blocked/failed/cancelled are reachable from any non-terminal state. Call get_workflow_state first if the valid next states from the current one aren't obvious.",
	}, advanceWorkflowHandler(a))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "ingest_jira_requirement",
		Description: "Fetch a Jira issue and create (or refresh) its requirement knowledge record, so the requirement_id build_task_context and submit_task_graph both hard-require actually exists. Call this before either of those for any requirement_id not already ingested. Read-only against Jira; no approval needed.",
	}, ingestJiraRequirementHandler(a))

	// Milestone 6: Plan-to-Beads and Petruk execution (§10, §11).
	mcp.AddTool(server, &mcp.Tool{
		Name:        "submit_task_graph",
		Description: "Batch-create TaskContracts and wire their dependency edges into Beads (§10.1-§10.4). The calling role does the decomposition; this tool only creates and wires the result. Each item's requirement_id must already exist as a knowledge record - call ingest_jira_requirement first for any Jira-sourced requirement not yet ingested.",
	}, submitTaskGraphHandler(a))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "list_ready_tasks",
		Description: "List Beads issues with no active blockers (§9's 'Petruk executes ready task'). Read-only.",
	}, listReadyTasksHandler(a))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "claim_ready_task",
		Description: "Atomically claim the first ready Beads issue matching the filters (§11.3's 'claim task' step). Mutates issue state.",
	}, claimReadyTaskHandler(a))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "build_task_context",
		Description: "Assemble the fresh, bounded per-task execution context (§11.2) and write it as this task's task.yaml evidence (§17.2). Read-only against the knowledge store. requirement_id must already exist as a knowledge record - call ingest_jira_requirement first for any Jira-sourced requirement not yet ingested. Resuming the same task_id (e.g. impl -> tests -> review): task_scope, task_acceptance_criteria, task_definition_of_done, task_expected_files_or_components, affected_symbols_and_files, and required_tests each default to the value from that task_id's last call when omitted - pass only the fields that actually changed, not the full payload every time.",
	}, buildTaskContextHandler(a))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "start_task_execution",
		Description: "Create this task's isolated worktree and open its evidence bundle/journal (§11.1 steps 1-4). Requires a prior approved worktree-creation request: this is a human-run CLI step, not another MCP tool - ask the user to run `punakawan worktree approve <repo-id> <task-id>` in their own terminal, then retry this call.",
	}, startTaskExecutionHandler(a))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "finish_task_execution",
		Description: "Record this task's final status and remove its isolated worktree (§11.1 step 10).",
	}, finishTaskExecutionHandler(a))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "write_file",
		Description: "Write one file within a task's worktree, policy-checked and confined to the worktree root (§15.4, §3.1). Use instead of writing to disk directly.",
	}, writeFileHandler(a))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "bulk_create_files",
		Description: "Create several files within a task's worktree in one call, with the same checks as write_file, best-effort per file.",
	}, bulkCreateFilesHandler(a))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "check_diff",
		Description: "Stage and check a task's pending changes against policy and a heuristic secret scan (§15.4), writing diff.patch evidence (§17.2). Must pass before commit_task.",
	}, checkDiffHandler(a))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "run_tests",
		Description: "Run caller-specified compile/test commands through the tool supervisor and record a tests.json evidence report (§11.3, §17.2).",
	}, runTestsHandler(a))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "check_openapi_compatibility",
		Description: "Diff a base and head OpenAPI spec, classify breaking changes, and record api-diff.json evidence (§13.4, §17.2).",
	}, checkOpenAPICompatibilityHandler(a))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "list_task_evidence",
		Description: "List every structured EvidenceRecord check_diff/run_tests/check_openapi_compatibility have recorded for a task (punokawan-s12), so a reviewer can enumerate its evidence without knowing the bundle's file-naming convention.",
	}, listTaskEvidenceHandler(a))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "commit_task",
		Description: "Stage and commit a task's pending changes, refusing to do so unless a prior check_diff passed and the worktree is on a task branch (§15.4).",
	}, commitTaskHandler(a))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "push_task_branch",
		Description: "Push a task's branch to its remote (AEP-M4 §8's 'push branch' step, before create_pr), gated by detected push capability ∩ repository policy ∩ this call's allow_push override. Never force-pushes. Must run before finish_task_execution removes the task's worktree.",
	}, pushTaskBranchHandler(a))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "create_pr",
		Description: "Create a pull request for a pushed task branch (AEP-M4 §8.1). Templates the caller-supplied Summary/Requirements/Changes/Verification/etc. sections into the PR body verbatim - punakawan does not write any of that content itself. If PR creation is not currently possible (no remote, no push access, unsupported provider, no github adapter configured, ...) returns created=false with the specific reason instead of erroring, per §8.1's failure behavior." + approvalGateNote,
	}, createPrHandler(a))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "review_pr",
		Description: "Fetch a PR's metadata, diff files, CI checks, and (optionally) comments (§8.2). REACTIVE - explicit_trigger must be true only when a human explicitly asked to review this specific PR; never call this for a PR being discovered, CI failing, or any other automatic signal. Punakawan does not review anything itself (ADR-0016): use this output to build Gareng/Petruk review capsules via request_capsule, have Bagong verify findings, then call submit_pr_review_findings with Semar's deduplicated result.",
	}, reviewPrHandler(a))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "submit_pr_review_findings",
		Description: "Persist review_pr's final, Semar-deduplicated ReviewFinding[] for a PR (§8.2's 'return final review' step).",
	}, submitPrReviewFindingsHandler(a))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "fetch_unresolved_pr_comments",
		Description: "Fetch a PR's still-open review threads (§8.3). REACTIVE - explicit_trigger must be true only when a human explicitly asked to fix this PR's review comments; never call this for a reviewer commenting, CI failing, or review_pr finishing. Classifying each comment as applicable/already_resolved/stale/conflicting/requires_clarification/major_change_required is the calling agent's judgment, not something this tool determines.",
	}, fetchUnresolvedPrCommentsHandler(a))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "resolve_review_thread",
		Description: "Mark a review thread resolved (§8.3's final, optional write step). Requires allow=true - review threads are never resolved automatically." + approvalGateNote,
	}, resolveReviewThreadHandler(a))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "report_discovered_task",
		Description: "Record newly discovered work found mid-execution as a discovered-from task, labeled for Semar's review (§10.4's discovery rule).",
	}, reportDiscoveredTaskHandler(a))

	// Jira as source of truth: adapter invocation (§5.1-§5.3).
	mcp.AddTool(server, &mcp.Tool{
		Name:        "call_adapter_operation",
		Description: "Invoke a declared adapter operation. Atlassian reads include getJiraIssue, getJiraComments, getJiraRemoteLinks, getJiraEpic, listJiraAttachments, and searchJira. Writes include editJiraIssue, addJiraComment, download/upload/deleteJiraAttachment, estimates, worklogs, and transitions. Writes elicit one human approval for the whole run; unsupported clients must show the user Approve/Deny and then use respond_to_adapter_approval.",
	}, callAdapterOperationHandler(a))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "respond_to_adapter_approval",
		Description: "Record approve or deny only after the human user explicitly chose in the conversation. Never call this tool autonomously or infer consent. After approval, retry the original write; denial blocks the run.",
	}, respondToAdapterApprovalHandler(a))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "request_jira_clarification",
		Description: "Post a pre-rendered clarification comment on a Jira issue and, if a clarification status is configured, transition the issue to it." + approvalGateNote,
	}, requestJiraClarificationHandler(a))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "check_jira_skippable",
		Description: "Check whether a Jira-sourced requirement's current status is in the configured skip-status list, so a caller can exclude it before submitting a task graph.",
	}, checkJiraSkippableHandler(a))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "sync_jira_subtasks",
		Description: "Create Jira subtasks under a parent issue for candidates that don't already exist, deduplicating by normalized summary." + approvalGateNote,
	}, syncJiraSubtasksHandler(a))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "update_jira_task_progress",
		Description: "Update a Jira issue's original estimate (points-derived unless given explicitly), add a worklog entry, and/or post a comment. Each action is optional and one run approval covers all selected writes." + approvalGateNote,
	}, updateJiraTaskProgressHandler(a))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "list_jira_sync_queue",
		Description: "List outbound adapter writes (Jira syncs) that reached the adapter but failed after passing their approval check, recorded for retry (punokawan-nbz). Defaults to pending entries only.",
	}, listJiraSyncQueueHandler(a))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "retry_jira_sync_entry",
		Description: "Replay a list_jira_sync_queue entry's failed write through its original adapter. Marks it resolved on success; on failure it stays queued with an incremented attempt count.",
	}, retryJiraSyncEntryHandler(a))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "submit_jira_assessment",
		Description: "Post a Jira-formatted comment (headings, bullet lists, a table) covering what exists vs. what needs to change, findings, and open questions for stakeholder decision (important ones flagged), then create subtasks with detailed plans. Each task's Jira original/remaining estimate is set to its AI-assisted implementation time; human-manual time and time saved are narrative only. The calling agent does the assessment and decomposition; this tool only renders, writes, and persists the result." + approvalGateNote,
	}, submitJiraAssessmentHandler(a))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "reopen_task",
		Description: "Reopen a closed Beads issue, e.g. when Bagong's independent review finds a blocking regression in already-completed work (§8.4). Pairs with report_discovered_task, which covers the 'create a new task' half of the same acceptance criterion.",
	}, reopenTaskHandler(a))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "search_knowledge",
		Description: "Search the durable knowledge store locally (§11): exact structured identifiers (CVE/GHSA/Sonar rule/Jira key/git hash/...) and aliases outrank BM25F keyword matches, which fall back to fuzzy matching only when keyword search finds nothing. project/repository/module/path only bias ranking (§11.10's scope bonus) - they never filter results out; use types/tags for that. Every result carries an explanation (§11.13) of why it matched. No embeddings, no external model calls: this is a local index over knowledge Punakawan already has, not new reasoning.",
	}, searchKnowledgeHandler(a))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "submit_missing_context_request",
		Description: "Request context a capsule did not include (§6.4). Subagents may request additional context but must not search broadly themselves - this only records the request; it is Semar's (the calling agent's) own next call to search_knowledge, request_capsule, or resolve_missing_context_request that decides what happens to it.",
	}, submitMissingContextRequestHandler(a))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "list_missing_context_requests",
		Description: "List missing-context requests (§6.4), defaulting to pending ones, so Semar can decide each one's resolution.",
	}, listMissingContextRequestsHandler(a))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "resolve_missing_context_request",
		Description: "Record Semar's decision on a missing-context request (§6.4): added_to_revision (requires revised_capsule_id from a prior request_capsule call), rejected, or asked_user. Punakawan does not choose between these - it only persists whichever the calling agent picked.",
	}, resolveMissingContextRequestHandler(a))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "delete_knowledge",
		Description: "Bulk-delete specific knowledge records by id, e.g. ones a search_knowledge call surfaced as stale, superseded, or wrong - so a future search does not keep returning dirty context. Deletes are permanent (no fold-latest, no undo); ids not found in the store are reported separately and are not an error.",
	}, deleteKnowledgeHandler(a))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "reset_project_knowledge",
		Description: "Bulk-delete every knowledge record matching a given project/repository/module scope - use when a whole project's knowledge has gone stale and should be re-ingested from scratch rather than pruned record by record. Requires at least one of project/repository/module (an empty scope would match everything). Defaults to a dry run: returns the matching record ids without deleting anything unless confirm=true.",
	}, resetProjectKnowledgeHandler(a))
}
