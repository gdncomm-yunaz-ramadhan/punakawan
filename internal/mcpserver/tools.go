package mcpserver

import (
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ygrip/punakawan/internal/app"
	"github.com/ygrip/punakawan/internal/capability"
)

// addTool registers one MCP tool on server and, in the same call, records its
// name in the capability registry, plus (via reg.record) its description and
// a closure that can re-register it later - what find_tool uses to reveal a
// tool the default facade hid. Routing every registration through this
// wrapper is what makes the registry the single source of truth for "which
// capabilities exist" (agent-context plan §4.3): the set can no longer drift
// from what the server actually exposes, because they are populated by the
// same statements. reg may be nil (the wrapper then behaves like mcp.AddTool).
func addTool[In, Out any](server *mcp.Server, reg *toolIndex, tool *mcp.Tool, h mcp.ToolHandlerFor[In, Out]) {
	mcp.AddTool(server, tool, h)
	if reg != nil {
		reg.Add(capability.Descriptor{Name: tool.Name, Source: capability.SourceMCP})
		reg.record(tool.Name, tool.Description, func() { mcp.AddTool(server, tool, h) })
	}
}

// approvalGateNote is appended to every tool description whose handler can
// trigger a write-approval gate - gate mechanics were only documented in
// the server's Instructions blob, not on the specific tool that hits the
// gate. Kept short and shared rather than repeating
// call_adapter_operation's full explanation on each one.
const approvalGateNote = " Writes elicit one human approval for the whole run (see call_adapter_operation); unsupported clients must show the user Approve/Deny and call respond_to_adapter_approval."

// plainLanguageStyleNote is appended to every tool's free-text authored-
// content field (Jira ticket summaries/descriptions/comments, git commit
// messages) so agent-written output reads the same everywhere: plain and
// immediately understandable, regardless of which tool produced it. The
// wayang role names (Semar/Gareng/Petruk/Bagong) are an internal convenience
// and never a tone to write in - see prompts/shared/communication.md.
const plainLanguageStyleNote = " Style: clear, concise, plain language - short sentences, everyday words, no jargon, no filler, no hype, no theatrical or mystical phrasing. State what happened or what's needed and why it matters, nothing more."

// registerTools adds the data-operation tools defined in §28.4, plus
// create_workflow_run: §28.4 lists get_workflow_state/advance_workflow but
// not a way to start a run in the first place, and the server cannot
// function without one, so this is a necessary addition beyond the plan's
// literal tool list rather than an unstated one.
func registerTools(server *mcp.Server, a *app.App, reg *toolIndex) {
	addTool(server, reg, &mcp.Tool{
		Name:        "build_context_dossier",
		Description: "Assemble the context dossier from workspace, git, and durable knowledge state. No reasoning is performed.",
	}, buildContextDossierHandler(a))

	addTool(server, reg, &mcp.Tool{
		Name:        "create_knowledge_record",
		Description: "Create a durable project knowledge record directly, without first creating a workflow run, capsule, dossier, or existing target. Use this for ad-hoc decisions, facts, assumptions, risks, evidence, conventions, and other reusable context. Defaults to model-assisted/inferred provenance; set validity_state deliberately when the source supports a stronger or different claim. Structured role outputs and retrieval recipes must use their dedicated tools.",
	}, createKnowledgeRecordHandler(a))

	addTool(server, reg, &mcp.Tool{
		Name:        "set_project_metadata",
		Description: "Create or update one project metadata entry (mirrors the panel's metadata editor). Omit value to attempt best-effort auto-detection for known keys (currently: test.command) before falling back to an error the caller can turn into an explicit ask. Use this to close a prepare_work_context 'metadata' MissingItem gap without a human editing project.yaml by hand.",
	}, setProjectMetadataHandler(a))

	addTool(server, reg, &mcp.Tool{
		Name:        "submit_final_plan",
		Description: "Validate and persist Semar's final implementation plan into the Plan domain (internal/plan), refused while blocking contradictions remain open. For a lane's Synthesis stage instead, use submit_lane_review with role semar. For general plan CRUD (multiple steps, project_ids, revisions), use plan_save/plan_get instead.",
	}, submitFinalPlanHandler(a))

	addTool(server, reg, &mcp.Tool{
		Name:        "plan_save",
		Description: "Save a new revision of a first-class Plan. Reuse an existing plan's id to append a clarifying revision on top of it (immutable - the prior revision is never changed), or a fresh id to start a new plan lineage.",
	}, planSaveHandler(a))

	addTool(server, reg, &mcp.Tool{
		Name:        "plan_get",
		Description: "Fetch a Plan by id, optionally an exact past revision. Omit revision for the plan's current (highest) revision.",
	}, planGetHandler(a))

	// internal/planexec: plan-native step execution tracking, for a
	// project that wants to track execution against a Plan's own steps.
	addTool(server, reg, &mcp.Tool{
		Name:        "plan_step_ready",
		Description: "List a plan's steps that are ready to claim: not yet claimed or completed, and every step they depend on (PlanStep.depends_on) is already completed. Reads only Plan/execution state. Read-only.",
	}, planStepReadyHandler(a))

	addTool(server, reg, &mcp.Tool{
		Name:        "plan_step_claim",
		Description: "Claim one plan step's execution, recording who claimed it. Fails if the step is not ready (a dependency is not yet completed) or is already claimed by someone else.",
	}, planStepClaimHandler(a))

	addTool(server, reg, &mcp.Tool{
		Name:        "plan_step_complete",
		Description: "Mark one plan step's execution done, so any other step that depends on it can become ready. A project tracking execution against a Plan's own steps needs this explicit call because nothing else closes that record automatically.",
	}, planStepCompleteHandler(a))

	addTool(server, reg, &mcp.Tool{
		Name:        "plan_step_reopen",
		Description: "Reopen a previously completed plan step's execution, recording why (e.g. a review found a regression in already-completed work).",
	}, planStepReopenHandler(a))

	addTool(server, reg, &mcp.Tool{
		Name:        "create_workflow_run",
		Description: "Start a new workflow run in state \"created\".",
	}, createWorkflowRunHandler(a))

	addTool(server, reg, &mcp.Tool{
		Name:        "save_workflow_definition",
		Description: "Create or update a reusable workflow definition (mirrors the panel's editor), skipping propose_project_learning's review gate. Validated against the panel's capability rules; common Jira-create spellings normalize to create_jira_issue. Updating an id requires its current revision (optimistic locking); a new id ignores any revision. Prior versions are snapshotted, never overwritten. Set judgment (with a rationale) when it's the agent's own judgment, not a direct instruction, driving the capture - logged as an accepted, deduplicated proposal, an audit trail not a gate.",
	}, saveWorkflowDefinitionHandler(a, reg))

	addTool(server, reg, &mcp.Tool{
		Name:        "prepare_work_context",
		Description: "Before substantial project work, call this once to compose bounded project context for a run: resolves the workflow (by id or capability/intent selector), validates inputs, resolves required and priority-selected project metadata, and - given a retrieval_query - retrieves scoped knowledge filtered by lifecycle validity so disputed/stale/superseded records never appear as accepted guidance. Returns run_id, an immutable context digest, selected metadata/knowledge with reasons, and any missing context. Pass run_id to run-scoped calls; resume by reusing it.",
	}, prepareWorkContextHandler(a))

	addTool(server, reg, &mcp.Tool{
		Name:        "knowledge_record_candidate",
		Description: "Persist a candidate durable learning, e.g. delivery evidence worth remembering once accepted. Named 'candidate', not 'create': this is evidence offered for storage, not an assertion of canonical truth. Currently always written to the local project knowledge store. Defaults to model-assisted/inferred provenance.",
	}, knowledgeRecordCandidateHandler(a))

	addTool(server, reg, &mcp.Tool{
		Name:        "get_knowledge_records",
		Description: "Batch-read complete typed knowledge records by id, e.g. to expand the ids prepare_work_context or search_knowledge returned into full records in one call. Ids not found are reported in not_found rather than erroring the whole batch. project_id (ADR-0020) selects which project's knowledge store to read from and defaults to the calling project; name another project's id only to deliberately cross-project read (requires it share this project's hub).",
	}, getKnowledgeRecordsHandler(a))

	addTool(server, reg, &mcp.Tool{
		Name:        "get_workflow_state",
		Description: "Read a workflow run's current state and checkpoint history.",
	}, getWorkflowStateHandler(a))

	addTool(server, reg, &mcp.Tool{
		Name:        "advance_workflow",
		Description: "Transition a workflow run to a new state, appending a checkpoint. Valid next_state values: created, context-building, awaiting-clarification, planning, awaiting-approval, executing, reviewing, blocked, completed, failed, cancelled. Only the defined transition graph is accepted from the current state; blocked/failed/cancelled are reachable from any non-terminal state. A context-aware run cannot enter completed without a recorded outcome (record_work_outcome) and, if definition-backed, all steps completed. Call get_workflow_state first if valid next states aren't obvious.",
	}, advanceWorkflowHandler(a))

	addTool(server, reg, &mcp.Tool{
		Name:        "get_next_workflow_step",
		Description: "For a definition-backed run, list the steps that are ready to execute now and the ones still blocked (with the reason: unmet dependency, or a capability the workflow does not allow / that is not registered). A disallowed capability surfaces as blocked here, before you execute it. An ad hoc run has no steps and says so.",
	}, getNextWorkflowStepHandler(a))

	addTool(server, reg, &mcp.Tool{
		Name:        "complete_workflow_step",
		Description: "Mark one workflow step done, attaching evidence_ids and/or a deviation_reason (one is required), then unlock any dependent steps whose inputs are now satisfied. Rejects completing a step whose capability the workflow does not allow. Records a run-scoped capability event for the run's trace.",
	}, completeWorkflowStepHandler(a))

	addTool(server, reg, &mcp.Tool{
		Name:        "propose_project_learning",
		Description: "Open (or reinforce) a reviewed learning proposal for a workflow, project_metadata, knowledge, or convention improvement. Supply target_id and the proposed candidate content; it becomes a proposal in the artifact-review flow - never a direct canonical write. A human accepts/rejects it in the panel. An equivalent pending proposal absorbs your evidence_ids/source_run_ids and increments support_count instead of duplicating. Proposals must reference structured outcome/evidence, not mined chat. Acceptance writes a new immutable revision; for workflows, acceptance never auto-activates it.",
	}, proposeProjectLearningHandler(a))

	addTool(server, reg, &mcp.Tool{
		Name:        "record_work_outcome",
		Description: "Persist the structured result of a context-aware run before completing it: status (success|partial|failed), a concise summary, evidence ids, output refs, any workflow deviations, missing/stale context encountered, and reusable observations classified for a later learning proposal (workflow|metadata|knowledge|contradiction|workflow-revision). An observation is a traceable input to a proposal, NOT canonical knowledge. A context-aware run cannot be advanced to completed until this is recorded.",
	}, recordWorkOutcomeHandler(a))

	addTool(server, reg, &mcp.Tool{
		Name:        "ingest_jira_requirement",
		Description: "Fetch a Jira issue and create (or refresh) its requirement knowledge record, so the requirement_id build_task_context hard-requires actually exists. Call this before that for any requirement_id not already ingested. Read-only against Jira; no approval needed.",
	}, ingestJiraRequirementHandler(a))

	addTool(server, reg, &mcp.Tool{
		Name:        "build_task_context",
		Description: "Assemble the fresh, bounded per-task execution context and write it as this task's task.yaml evidence. Read-only against the knowledge store. requirement_id must already exist as a knowledge record - call ingest_jira_requirement first for any not-yet-ingested Jira requirement. Resuming the same task_id (impl -> tests -> review): task_scope, acceptance_criteria, definition_of_done, expected_files_or_components, affected_symbols_and_files, and required_tests default to the last call's value when omitted - pass only what changed.",
	}, buildTaskContextHandler(a))

	addTool(server, reg, &mcp.Tool{
		Name:        "start_task_execution",
		Description: "Create this task's isolated worktree and open its evidence bundle/journal. Requires a prior approved worktree-creation request: this is a human-run CLI step, not another MCP tool - ask the user to run `punakawan worktree approve <repo-id> <task-id>` in their own terminal, then retry this call.",
	}, startTaskExecutionHandler(a))

	addTool(server, reg, &mcp.Tool{
		Name:        "finish_task_execution",
		Description: "Record this task's final status and remove its isolated worktree.",
	}, finishTaskExecutionHandler(a))

	addTool(server, reg, &mcp.Tool{
		Name:        "write_files",
		Description: "Write one or many files inside a task's worktree, each policy-checked and confined to the worktree root, creating parent directories and overwriting any existing file. Use this instead of writing to disk directly whenever a task is producing or editing code. Requires start_task_execution to have created that task's worktree first. Best-effort: each file reports its own error and one failure never aborts the rest.",
	}, writeFilesHandler(a))

	addTool(server, reg, &mcp.Tool{
		Name:        "check_diff",
		Description: "Stage and check a task's pending changes against policy and a heuristic secret scan, writing diff.patch evidence. Must pass before commit_task.",
	}, checkDiffHandler(a))

	addTool(server, reg, &mcp.Tool{
		Name:        "run_tests",
		Description: "Run caller-specified compile/test commands through the tool supervisor and record a tests.json evidence report.",
	}, runTestsHandler(a))

	addTool(server, reg, &mcp.Tool{
		Name:        "check_openapi_compatibility",
		Description: "Diff a base and head OpenAPI spec, classify breaking changes, and record api-diff.json evidence.",
	}, checkOpenAPICompatibilityHandler(a))

	addTool(server, reg, &mcp.Tool{
		Name:        "list_task_evidence",
		Description: "List every structured EvidenceRecord check_diff/run_tests/check_openapi_compatibility have recorded for a task, so a reviewer can enumerate its evidence without knowing the bundle's file-naming convention.",
	}, listTaskEvidenceHandler(a))

	addTool(server, reg, &mcp.Tool{
		Name:        "commit_task",
		Description: "Stage and commit a task's pending changes; refused unless a prior check_diff passed and the worktree is on a task branch. Write the message in Conventional Commits form: imperative subject <=72 chars, a body only when the why isn't obvious, referencing the concrete source touched." + plainLanguageStyleNote,
	}, commitTaskHandler(a))

	addTool(server, reg, &mcp.Tool{
		Name:        "push_task_branch",
		Description: "Push a task's branch to its remote, before create_pr, gated by detected push capability ∩ repository policy ∩ this call's allow_push override. Never force-pushes. Must run before finish_task_execution removes the task's worktree.",
	}, pushTaskBranchHandler(a))

	addTool(server, reg, &mcp.Tool{
		Name:        "create_pr",
		Description: "Create a pull request for a pushed task branch. Templates the caller's Summary/Requirements/Changes/Verification sections into the PR body verbatim - write them concise: lead with the key change and impact, terse bullets, reference concrete files/symbols touched. If creation isn't possible (no remote/access/adapter) returns created=false with the reason." + approvalGateNote,
	}, createPrHandler(a))

	addTool(server, reg, &mcp.Tool{
		Name:        "review_pr",
		Description: "Fetch a PR's metadata, diff files, CI checks, and (optionally) comments. REACTIVE - explicit_trigger must be true only when a human explicitly asked to review this specific PR; never call this for a PR being discovered, CI failing, or any other automatic signal. Punakawan does not review anything itself (ADR-0016): have Gareng/Petruk review and Bagong verify findings, then call submit_pr_review_findings with Semar's deduplicated result.",
	}, reviewPrHandler(a))

	addTool(server, reg, &mcp.Tool{
		Name:        "submit_pr_review_findings",
		Description: "Persist review_pr's final, Semar-deduplicated ReviewFinding[] for a PR.",
	}, submitPrReviewFindingsHandler(a))

	addTool(server, reg, &mcp.Tool{
		Name:        "fetch_unresolved_pr_comments",
		Description: "Fetch a PR's still-open review threads. REACTIVE - explicit_trigger must be true only when a human explicitly asked to fix this PR's review comments; never call this for a reviewer commenting, CI failing, or review_pr finishing. Classifying each comment as applicable/already_resolved/stale/conflicting/requires_clarification/major_change_required is the calling agent's judgment, not something this tool determines.",
	}, fetchUnresolvedPrCommentsHandler(a))

	addTool(server, reg, &mcp.Tool{
		Name:        "resolve_review_thread",
		Description: "Mark a review thread resolved. Requires allow=true - review threads are never resolved automatically." + approvalGateNote,
	}, resolveReviewThreadHandler(a))

	// Jira as source of truth: adapter invocation (§5.1-§5.3).
	addTool(server, reg, &mcp.Tool{
		Name:        "call_adapter_operation",
		Description: "Invoke a declared adapter operation. Atlassian reads include getJiraIssue, getJiraComments, getJiraRemoteLinks, getJiraEpic, listJiraAttachments, listJiraBoards, listJiraSprints, and searchJira. Writes include editJiraIssue, addJiraComment, download/upload/deleteJiraAttachment, estimates, worklogs, and transitions. Writes elicit one human approval for the whole run; unsupported clients must show the user Approve/Deny and then use respond_to_adapter_approval.",
	}, callAdapterOperationHandler(a))

	addTool(server, reg, &mcp.Tool{
		Name:        "respond_to_adapter_approval",
		Description: "Record approve or deny only after the human user explicitly chose in the conversation. Never call this tool autonomously or infer consent. After approval, retry the original write; denial blocks the run.",
	}, respondToAdapterApprovalHandler(a))

	addTool(server, reg, &mcp.Tool{
		Name:        "list_pending_approvals",
		Description: "Read-only re-check of this project's durable approval queue (optionally filtered by run_id). Approvals persist per run_id across sessions, so call this to see what is still pending before proceeding - especially in a loop, or after a session stopped/resumed - instead of blindly retrying a write. Returns each pending approval's id, run, operation, and target so you can surface Approve/Deny to the user and then call respond_to_adapter_approval.",
	}, listPendingApprovalsHandler(a))

	addTool(server, reg, &mcp.Tool{
		Name:        "request_jira_clarification",
		Description: "Post a pre-rendered clarification comment on a Jira issue and, if a clarification status is configured, transition the issue to it. Body: Markdown, converted to ADF." + plainLanguageStyleNote + approvalGateNote,
	}, requestJiraClarificationHandler(a))

	addTool(server, reg, &mcp.Tool{
		Name:        "check_jira_skippable",
		Description: "Check whether a Jira-sourced requirement's current status is in the configured skip-status list, so a caller can exclude it before submitting a task graph.",
	}, checkJiraSkippableHandler(a))

	addTool(server, reg, &mcp.Tool{
		Name:        "create_jira_issue",
		Description: "Create a Jira issue with project key, issue type name, and summary. Returns key/status/URL. issue_type_name is free-text (per-site) - use sync_jira_subtasks for subtasks under one parent." + plainLanguageStyleNote + approvalGateNote,
	}, createJiraIssueHandler(a))

	addTool(server, reg, &mcp.Tool{
		Name:        "sync_jira_subtasks",
		Description: "Create Jira subtasks under a parent issue for candidates that don't already exist, deduplicating by normalized summary." + plainLanguageStyleNote + approvalGateNote,
	}, syncJiraSubtasksHandler(a))

	addTool(server, reg, &mcp.Tool{
		Name:        "update_jira_task_progress",
		Description: "Update a Jira issue's estimate, worklog, and/or comment - each optional, one approval covers all. Log against the subtask, not the parent, for decomposed issues." + plainLanguageStyleNote + approvalGateNote,
	}, updateJiraTaskProgressHandler(a))

	// Native Jira convenience tools: common ops that previously
	// needed a raw call_adapter_operation passthrough. Each is a thin,
	// approval-gated wrapper over the same atlassian adapter operation layer as
	// the tools above; run_id is optional (lightweight one-off mode).
	addTool(server, reg, &mcp.Tool{
		Name:        "jira_search_user",
		Description: "Look up Jira Cloud users by display name or email and return their accountId(s), so a name/email can be resolved to the accountId that jira_assign_issue (and Jira writes generally) require. Read-only: no approval needed. run_id is optional for one-off use.",
	}, jiraSearchUserHandler(a))

	addTool(server, reg, &mcp.Tool{
		Name:        "jira_link_issues",
		Description: "Create an issue link between two Jira issues (e.g. Blocks or Relates). inward_issue/outward_issue map onto Jira's inward/outward sides, so direction follows the link type. run_id is optional for one-off use." + approvalGateNote,
	}, jiraLinkIssuesHandler(a))

	addTool(server, reg, &mcp.Tool{
		Name:        "jira_set_story_points",
		Description: "Set an issue's Story Points custom field. Defaults to customfield_10016 (the common Jira Cloud default); pass story_points_field_id to override per project/board (discover the real id via atlassian.getIssueTypeFieldMeta). run_id is optional for one-off use." + approvalGateNote,
	}, jiraSetStoryPointsHandler(a))

	addTool(server, reg, &mcp.Tool{
		Name:        "jira_assign_issue",
		Description: "Assign a Jira issue to a user by accountId (resolve a name/email with jira_search_user first). run_id is optional for one-off use." + approvalGateNote,
	}, jiraAssignIssueHandler(a))

	addTool(server, reg, &mcp.Tool{
		Name:        "jira_find_sprint",
		Description: "Find Jira Agile sprints scoped to a board_id or project_key (a project_key is resolved to that project's scrum board(s) first via atlassian.listJiraBoards), optionally filtered by state (active/future/closed) and a case-insensitive name substring - so a caller can resolve a sprint id without already knowing it, instead of falling back to a raw JQL 'board in (X) and sprint in openSprints()' workaround. Returns each match's id, name, state, and board_id. Read-only: no approval needed. run_id is optional for one-off use.",
	}, jiraFindSprintHandler(a))

	addTool(server, reg, &mcp.Tool{
		Name:        "get_jira_issue",
		Description: "Read one Jira issue's subtasks, links, and/or comments - pick sections via include (default subtasks and links, which share one fetch). Call it to find the right subtask key before update_jira_task_progress on a decomposed issue, to see what blocks or relates to a ticket, or to read the discussion. Comment bodies are plain text from ADF, paged via start_at/max_results (default 20, max 100). Read-only: needs no approval. run_id is optional.",
	}, getJiraIssueHandler(a))

	addTool(server, reg, &mcp.Tool{
		Name:        "add_jira_comment",
		Description: "Post a standalone Jira comment (flat, not threaded - reply by referencing the earlier comment). Body: Markdown, converted to ADF; not old wiki markup." + plainLanguageStyleNote + approvalGateNote,
	}, addJiraCommentHandler(a))

	addTool(server, reg, &mcp.Tool{
		Name:        "list_jira_sync_queue",
		Description: "List outbound adapter writes (Jira syncs) that reached the adapter but failed after passing their approval check, recorded for retry. Defaults to pending entries only.",
	}, listJiraSyncQueueHandler(a))

	addTool(server, reg, &mcp.Tool{
		Name:        "retry_jira_sync_entry",
		Description: "Replay a list_jira_sync_queue entry's failed write through its original adapter. Marks it resolved on success; on failure it stays queued with an incremented attempt count.",
	}, retryJiraSyncEntryHandler(a))

	addTool(server, reg, &mcp.Tool{
		Name:        "submit_jira_assessment",
		Description: "Comment body format: Markdown (converts to ADF; not old wiki markup). Posts a Jira comment covering current state vs. needed changes, findings, and open questions, then creates subtasks with detailed plans. Each subtask's estimate is the AI-assisted implementation time. The agent does the assessment; this tool renders and persists it." + approvalGateNote,
	}, submitJiraAssessmentHandler(a))

	addTool(server, reg, &mcp.Tool{
		Name:        "search_knowledge",
		Description: "Search the durable knowledge store locally: exact structured identifiers (CVE/GHSA/Sonar rule/Jira key/git hash) and aliases outrank BM25F keyword matches, which fall back to fuzzy matching only when keyword search finds nothing. project/repository/module/path only bias ranking, never filter - use types/tags for that. Every result explains why it matched. No embeddings or model calls: a local index over knowledge already stored. project_id selects which project's store to search; another project's id searches it via a lower-fidelity scan instead.",
	}, searchKnowledgeHandler(a))

	addTool(server, reg, &mcp.Tool{
		Name:        "submit_missing_context_request",
		Description: "Request context a prior submission did not include. Subagents may request additional context but must not search broadly themselves - this only records the request; it is Semar's (the calling agent's) own next call to search_knowledge or resolve_missing_context_request that decides what happens to it.",
	}, submitMissingContextRequestHandler(a))

	addTool(server, reg, &mcp.Tool{
		Name:        "list_missing_context_requests",
		Description: "List missing-context requests, defaulting to pending ones, so Semar can decide each one's resolution.",
	}, listMissingContextRequestsHandler(a))

	addTool(server, reg, &mcp.Tool{
		Name:        "resolve_missing_context_request",
		Description: "Record Semar's decision on a missing-context request: added_to_revision (requires a revised_capsule_id identifying the revised context), rejected, or asked_user. Punakawan does not choose between these - it only persists whichever the calling agent picked.",
	}, resolveMissingContextRequestHandler(a))

	addTool(server, reg, &mcp.Tool{
		Name:        "find_prune_candidates",
		Description: "List knowledge records with the signal needed to judge whether they're obsolete: validity_state, superseded_by, source age, and relation_count (how many other records reference this one - the closest proxy for 'still relied on'). No validity_state is required or assumed eligible for pruning; every state is included unless filtered. Filters/paginates like the underlying store; min_age_days applies within the fetched page, not globally - use next_cursor to keep scanning. Read-only: pass ids to delete_knowledge to remove them.",
	}, findPruneCandidatesHandler(a))

	addTool(server, reg, &mcp.Tool{
		Name:        "delete_knowledge",
		Description: "Bulk-delete specific knowledge records by id, e.g. ones find_prune_candidates or search_knowledge flagged as stale, superseded, or wrong. Any validity_state may be deleted; naming an id is the deliberate act. Not undoable through this tool: the knowledge store is a plain SQLite table with no version history, so a delete is permanent - commit_hash is only an opaque audit-log identifier for this operation, not a revertable snapshot. commit_hash is empty when every id was not_found.",
	}, deleteKnowledgeHandler(a))

	// Contradiction Ledger (Gareng, §16-22). Deterministic, no reasoning.
	addTool(server, reg, &mcp.Tool{
		Name:        "submit_contradiction",
		Description: "Record a detected contradiction - a disagreement between sources about one subject. Deduplicates deterministically by normalized subject.key: if a contradiction already exists for the same subject the existing record is returned (deduplicated=true) rather than a duplicate created. New records start at status detected and block by default only when severity is critical. Gated to role Gareng's contradictions capability.",
	}, submitContradictionHandler(a))

	addTool(server, reg, &mcp.Tool{
		Name:        "list_contradictions",
		Description: "List contradictions in the ledger. With no status filter it returns only still-open ones (detected/triaged/needs_clarification/resolution_proposed); pass status to filter to exactly one lifecycle state. Read-only.",
	}, listContradictionsHandler(a))

	addTool(server, reg, &mcp.Tool{
		Name:        "resolve_contradiction",
		Description: "Record a contradiction's confirmed resolution statement and who confirmed it, advancing it to resolved. Only valid from resolution_proposed. Gated to role Gareng's contradictions capability.",
	}, resolveContradictionHandler(a))

	// Cross-Repository Impact Graph (Gareng, §23-31). Deterministic query, no reasoning.
	addTool(server, reg, &mcp.Tool{
		Name:        "analyze_impact",
		Description: "Answer \"if subject_id changes, what else is affected?\" by a cycle-safe traversal of the impact graph, returning direct/transitive impact plus affected repositories, tests, deployments, owners, missing coverage, and any related contradictions. depth defaults to 3; set refresh=true to reconcile the structural graph from the workspace first. Read-only against durable state.",
	}, analyzeImpactHandler(a))

	addTool(server, reg, &mcp.Tool{
		Name:        "record_impact_edge",
		Description: "Record a discovered dependency edge (from->to of a given type and confidence) into the impact graph. Idempotent by (from,to,type): re-recording an edge supersedes the prior one. Gated to role Gareng's cross_repository_impact capability.",
	}, recordImpactEdgeHandler(a))

	addTool(server, reg, &mcp.Tool{
		Name:        "verify_impact_coverage",
		Description: "Bagong's coverage check: traverse the impact graph from subject_id and report whether every reachable symbol/operation is tested and whether any reachable edge is in dispute. Returns covered=true only when nothing is missing coverage and nothing is disputed, plus the affected repositories, missing-coverage nodes, and related contradictions. Gated to role Bagong's cross_repository_verification capability.",
	}, verifyImpactCoverageHandler(a))

	// Delivery scheduling: pull-based lease lifecycle over a multi-project
	// orchestration's dependency graph. A connected agent lists what it
	// could work on, claims one lane, keeps the lease alive with periodic
	// heartbeats while it does the actual work (role stages happen here,
	// in the calling agent, not in this server), then completes or
	// rejects it.
	addTool(server, reg, &mcp.Tool{
		Name:        "list_runnable_lanes",
		Description: "List every lane in an orchestration that has no unresolved predecessor and is not already leased. Read-only, but also refreshes which lanes are blocked versus runnable from the current dependency graph before listing.",
	}, listRunnableLanesHandler(a))

	addTool(server, reg, &mcp.Tool{
		Name:        "claim_lane",
		Description: "Claim a runnable lane, granting the calling worker an exclusive, time-limited lease. Fails if the lane is not currently runnable, if its revision has moved since it was listed (someone else changed it), or if its project already has another lane leased or running.",
	}, claimLaneHandler(a))

	addTool(server, reg, &mcp.Tool{
		Name:        "create_worktree",
		Description: "Create (or resume) a held lane's own isolated git worktree and branch, forked from its project's configured base branch. Never touches the project's main checkout.",
	}, createWorktreeHandler(a))

	addTool(server, reg, &mcp.Tool{
		Name:        "heartbeat_lease",
		Description: "Renew a held lease before it expires, proving the worker is still alive. Fails if lease_token does not match the lane's current lease (it was reclaimed after expiring, or never matched to begin with).",
	}, heartbeatLeaseHandler(a))

	addTool(server, reg, &mcp.Tool{
		Name:        "complete_lease",
		Description: "Report a held lease's work as done, moving the lane to review for the next stage to decide accepted or failed.",
	}, completeLeaseHandler(a))

	addTool(server, reg, &mcp.Tool{
		Name:        "reject_lease",
		Description: "Decline a held lease (e.g. a precondition no longer holds), returning the lane to runnable so it can be retried.",
	}, rejectLeaseHandler(a))

	addTool(server, reg, &mcp.Tool{
		Name:        "report_discovered_dependency",
		Description: "Report a dependency discovered mid-execution rather than known upfront: records it as a new edge and re-syncs the frontier, pausing only the lanes actually affected while unrelated work continues.",
	}, reportDiscoveredDependencyHandler(a))

	addTool(server, reg, &mcp.Tool{
		Name:        "run_in_lane",
		Description: "Run one command scoped strictly to a held lease's own worktree - the only execution surface this delivery domain exposes, so a worker can never read, write, or execute anything outside its own lane. Requires the lane's current lease token.",
	}, runInLaneHandler(a))

	addTool(server, reg, &mcp.Tool{
		Name:        "build_lane_context",
		Description: "Assemble a lane's bounded context: its pinned requirement sources, project delivery profile, and exact base commit, plus a digest identifying this exact combination. Fails closed if any pinned reference no longer resolves.",
	}, buildLaneContextHandler(a))

	addTool(server, reg, &mcp.Tool{
		Name:        "submit_lane_review",
		Description: "Record one role stage for a held lane's current attempt: role picks semar, gareng, petruk, or bagong, and the payload field of that name carries the content. Call it after reasoning as that role, once you hold the lane's lease. Stages run in that order, each requiring the one before it; petruk is refused while gareng's review still has blocking findings; recording a stage clears every later stage. After bagong, complete_lease.",
	}, submitLaneReviewHandler(a))

	// Publish, repair, and merge-readiness: turning a verified lane into a
	// published pull request, a bounded repair loop for a lane whose review
	// or CI came back lacking, and a read-only merge-readiness check.
	// Nothing here ever merges or closes a pull request.
	addTool(server, reg, &mcp.Tool{
		Name:        "publish_pr",
		Description: "Publish a held lane's pull request. Idempotent per lane: once a lane already has a published pull request, this returns it unchanged instead of opening a second one, even after a retry with a new call. Requires the lane's current lease token. This is a GitHub adapter write and requires that run's adapter-write approval to already exist (see call_adapter_operation/respond_to_adapter_approval); it fails with an actionable message rather than eliciting one itself. Never merges or closes anything - only opens.",
	}, publishPrHandler(a))

	addTool(server, reg, &mcp.Tool{
		Name:        "record_verification_dimension",
		Description: "Record one verification dimension's status (logic, unit, integration, quality, e2e, or ci) for a held lane's current attempt, with optional evidence_id and summary. The latest recording for a dimension wins.",
	}, recordVerificationDimensionHandler(a))

	addTool(server, reg, &mcp.Tool{
		Name:        "record_ci_check",
		Description: "Report one CI check's latest status for a held lane's current attempt. The ci verification dimension is derived from every required check's latest reported status unless it has been explicitly recorded via record_verification_dimension instead.",
	}, recordCiCheckHandler(a))

	addTool(server, reg, &mcp.Tool{
		Name:        "submit_review_conclusion",
		Description: "Record a reviewer's conclusion (approved, changes_requested, or blocked) for a held lane's current attempt. Rejected unless the reviewer's session is independent from implementer_session_id, or independence_override_reason is explicitly given.",
	}, submitReviewConclusionHandler(a))

	addTool(server, reg, &mcp.Tool{
		Name:        "start_repair_cycle",
		Description: "Start another repair cycle for a lane whose review or CI came back lacking, kicking it back to runnable for rework. After a fixed number of repair cycles for the same attempt, this escalates the lane for a human to look at instead of starting another - reported back as escalated=true, not as a tool-call error, since it is an expected outcome.",
	}, startRepairCycleHandler(a))

	addTool(server, reg, &mcp.Tool{
		Name:        "get_verification_matrix",
		Description: "Read a lane's current verification matrix: exactly one entry per fixed dimension (logic, unit, integration, quality, e2e, ci), defaulting to pending when nothing has been recorded or derived for it yet. Read-only.",
	}, getVerificationMatrixHandler(a))

	addTool(server, reg, &mcp.Tool{
		Name:        "check_merge_readiness",
		Description: "Check whether a lane is ready to merge against its project's required verification gates and latest review conclusion, reporting which gates are still failing if not. Read-only; never merges or closes anything.",
	}, checkMergeReadinessHandler(a))

	addTool(server, reg, &mcp.Tool{
		Name:        "request_project_approval",
		Description: "Ask whether a project's lanes in this orchestration are all ready to merge, and if so, run preflight and create the one approval manifest that project needs before delivery proceeds. Reports which lanes/gates are still blocking instead of creating a manifest if any lane is not yet ready. Re-calling with the same set of ready parent tasks returns the already-created manifest rather than making a second one.",
	}, requestProjectApprovalHandler(a))

	// Delivery facades: five higher-level tools over the granular delivery
	// primitives above, for a caller that wants "start this delivery and
	// tell me what's going on" without first learning the whole
	// orchestration/lane/task/manifest model.
	addTool(server, reg, &mcp.Tool{
		Name:        "start_delivery",
		Description: "Bootstrap one new delivery orchestration from a batch of requirement references (Jira keys, GitHub owner/repo#number references, URLs, or free text) and return its id plus current status. A reference this call cannot confidently classify becomes a pending question rather than failing the whole call. Pass projects to decompose it in the same call: each task becomes a parent task and a lane, so the returned view shows real lanes. Omitting projects leaves an inert shell nothing can run.",
	}, startDeliveryHandler(a))

	addTool(server, reg, &mcp.Tool{
		Name:        "get_delivery",
		Description: "Read one delivery orchestration's current status: every lane grouped by project, still-blocked lanes, pending approvals, pending questions, and one sentence naming the single most useful next step. Read-only. This is also how a reconnecting caller resumes - the domain is event-sourced, so current state is always derived fresh, and passing a prior response's latest_seq as since_seq reports exactly which lanes became runnable while you were away.",
	}, getDeliveryHandler(a))

	addTool(server, reg, &mcp.Tool{
		Name:        "update_delivery",
		Description: "Edit what a delivery orchestration says about itself after it was started: title, description, the plan record it follows, the session driving it, and which projects it involves. Every field is optional and only what you pass changes. Pass expected_revision from get_delivery so an edit against a stale view conflicts. Detaching a project whose lanes are still unfinished is refused; lanes are never deleted or reassigned.",
	}, updateDeliveryHandler(a))

	addTool(server, reg, &mcp.Tool{
		Name:        "answer_delivery_question",
		Description: "Answer one pending delivery question, either by supplying a now-known requirement's real content (provider/external_id/url/title/summary) or by routing an ambiguously-scoped parent task to a project (parent_task_id/project_id). Returns the orchestration's refreshed status.",
	}, answerDeliveryQuestionHandler(a))

	addTool(server, reg, &mcp.Tool{
		Name:        "approve_project_delivery",
		Description: "Approve (or, with reject set, reject) one project's pending approval manifest for a delivery orchestration. Returns the orchestration's refreshed status.",
	}, approveProjectDeliveryHandler(a))

	addTool(server, reg, &mcp.Tool{
		Name:        "cancel_delivery",
		Description: "Cancel a delivery orchestration that has not already reached a terminal status. Returns the orchestration's refreshed status.",
	}, cancelDeliveryHandler(a))

	addTool(server, reg, &mcp.Tool{
		Name:        "invoke_workflow_definition",
		Description: "Invoke a saved workflow definition by id. A definition with a non-empty roles map is delivery-shaped: this starts a delivery orchestration (inputs must include a references array, matching start_delivery) and returns its orchestration id, fetchable via get_delivery. Every other definition is invoked through the legacy step-execution run engine and returns a workflow run id instead.",
	}, invokeWorkflowDefinitionHandler(a))

	addTool(server, reg, &mcp.Tool{
		Name:        "register_project",
		Description: "Register a target project (repository URL and default branch) in the delivery registry, so orchestrations have somewhere to route tasks and create lanes against. A duplicate slug fails.",
	}, registerProjectHandler(a))

	addTool(server, reg, &mcp.Tool{
		Name:        "create_parent_task",
		Description: "Group one or more already-captured requirement sources into a new, unrouted parent task for a delivery orchestration. The caller decides how a requirement is broken down; this only records that decision. Returns the created task and the orchestration's refreshed status.",
	}, createParentTaskHandler(a))

	addTool(server, reg, &mcp.Tool{
		Name:        "create_lane",
		Description: "Create a delivery lane scoped to a project and, optionally, a parent task already routed to that same project. Returns the created lane and the orchestration's refreshed status.",
	}, createLaneHandler(a))

	addTool(server, reg, &mcp.Tool{
		Name:        "add_dependency_edge",
		Description: "Record an up-front, explicitly-authored dependency between two parent tasks in a delivery orchestration - the caller stating a dependency it already knows about, before either task's lane has been leased. Rejects unknown task ids and any edge that would create a cycle. Returns the created edge and the orchestration's refreshed status.",
	}, addDependencyEdgeHandler(a))
}
