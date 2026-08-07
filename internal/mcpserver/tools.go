package mcpserver

import (
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ygrip/punakawan/internal/app"
	"github.com/ygrip/punakawan/internal/capability"
)

// addTool registers one MCP tool on server and, in the same call, records its
// name in the capability registry. Routing every registration through this
// wrapper is what makes the registry the single source of truth for "which
// capabilities exist" (agent-context plan §4.3): the set can no longer drift
// from what the server actually exposes, because they are populated by the
// same statements. reg may be nil (the wrapper then behaves like mcp.AddTool).
func addTool[In, Out any](server *mcp.Server, reg *capability.Registry, tool *mcp.Tool, h mcp.ToolHandlerFor[In, Out]) {
	mcp.AddTool(server, tool, h)
	if reg != nil {
		reg.Add(capability.Descriptor{Name: tool.Name, Source: capability.SourceMCP})
	}
}

// approvalGateNote is appended to every tool description whose handler can
// trigger a write-approval gate (punokawan-7wv: gate mechanics were only
// documented in the server's Instructions blob, not on the specific tool
// that hits the gate). Kept short and shared rather than repeating
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
func registerTools(server *mcp.Server, a *app.App, reg *capability.Registry) {
	addTool(server, reg, &mcp.Tool{
		Name:        "build_context_dossier",
		Description: "Assemble the §9.1 context dossier from workspace, git, and durable knowledge state. No reasoning is performed.",
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
		Name:        "request_capsule",
		Description: "Build and persist an immutable, digested ContextCapsule for one Gareng/Petruk/Bagong invocation (architecture-enhancement-plan.md §6). Rejects requirement_ids/knowledge_ids whose record type is another role's output (e.g. bagong cannot cite a petruk-plan record) and allowed_tools entries a role must not have (e.g. bagong cannot be granted write_file). Set retrieval_query to also run Semar's automatic knowledge-retrieval pipeline (§11/§6.4, AEP-M7): search_knowledge's full ranking against that query, filtered to what this role may receive and to token_budget, added alongside any explicit knowledge_ids with each item's match explanation recorded as its reason. Call this before submit_gareng_review/submit_petruk_plan/submit_bagong_review, which require the returned id as capsule_id.",
	}, requestCapsuleHandler(a))

	addTool(server, reg, &mcp.Tool{
		Name:        "submit_gareng_review",
		Description: "Validate and persist a Gareng feasibility/risk review (§8.2) as durable knowledge. Requires capsule_id from a prior request_capsule call for role gareng.",
	}, submitGarengReviewHandler(a))

	addTool(server, reg, &mcp.Tool{
		Name:        "submit_petruk_plan",
		Description: "Validate and persist a Petruk implementation-planning output (§8.3) as durable knowledge. Requires capsule_id from a prior request_capsule call for role petruk.",
	}, submitPetrukPlanHandler(a))

	addTool(server, reg, &mcp.Tool{
		Name:        "submit_semar_synthesis",
		Description: "Validate and persist Semar's consolidated clarification (§8.1/§9.2) or final plan (§9.3) as durable knowledge. Exactly one of synthesis or final_plan must be set.",
	}, submitSemarSynthesisHandler(a))

	addTool(server, reg, &mcp.Tool{
		Name:        "submit_bagong_review",
		Description: "Validate and persist a Bagong independent final review (§8.4) as durable knowledge. Requires capsule_id from a prior request_capsule call for role bagong. Enforces the mandatory senior-maintainer review rubric as a hard constraint (see the bagong prompt): the submission is REJECTED unless requirement_coverage (verification performed) and uncertainties (questions/assumptions and remaining unverified risks) are both populated, findings are non-blank, and a no-findings review states so explicitly in honest_summary.",
	}, submitBagongReviewHandler(a))

	addTool(server, reg, &mcp.Tool{
		Name:        "create_workflow_run",
		Description: "Start a new workflow run in state \"created\" (§18.1).",
	}, createWorkflowRunHandler(a))

	addTool(server, reg, &mcp.Tool{
		Name:        "save_workflow_definition",
		Description: "Create or update a reusable workflow definition (mirrors the panel's workflow editor) - use this to turn an explicitly user-dictated flow, or a pattern the calling agent judges worth capturing, straight into a versioned, selector-resolvable definition without waiting on propose_project_learning's panel-review gate. Validated against the same capability rules the panel enforces (unknown/command-like capabilities are rejected); common Jira-create spellings createJiraIssue and atlassian.createJiraIssue are normalized to the canonical create_jira_issue tool. Updating an existing id requires the current revision (optimistic locking, like project metadata); a brand-new id ignores whatever revision is passed. The prior version is always snapshotted, never overwritten - see the panel's workflow history for any revision. Set judgment (with a required rationale) when the agent's own judgment - not a direct user instruction - is why this pattern is being captured: this records a fingerprinted, deduplicated learning proposal alongside the save (support_count increments if the same step pattern is captured again), already marked accepted since the save already happened - it is an audit trail, not an additional gate.",
	}, saveWorkflowDefinitionHandler(a, reg))

	addTool(server, reg, &mcp.Tool{
		Name:        "prepare_work_context",
		Description: "Before substantial project work, call this once to compose the bounded project context for a run (agent-context plan §4.4): it resolves the workflow (by explicit workflow_id or an exact capability/intent selector; ad hoc when neither matches), validates and defaults inputs, resolves required project metadata, selects optional metadata by priority, and — when a retrieval_query is given — retrieves scoped knowledge filtered by lifecycle validity so disputed/stale/superseded/draft records never appear as accepted guidance (inferred goes to a separate caution list). Returns the run_id, the immutable context digest, the selected metadata and knowledge (each with a selection reason and content hash), and any missing required context (which puts the run in awaiting-clarification). Deterministic: the same inputs and store revisions produce the same digest. Pass the returned run_id to run-scoped calls; resume/refresh by passing an existing run_id.",
	}, prepareWorkContextHandler(a))

	addTool(server, reg, &mcp.Tool{
		Name:        "get_knowledge_records",
		Description: "Batch-read complete typed knowledge records by id (agent-context plan §5.2), e.g. to expand the ids prepare_work_context or search_knowledge returned into full records in one call. Ids not found are reported in not_found rather than erroring the whole batch. project_id (ADR-0020) selects which project's knowledge store to read from and defaults to the calling project; name another project's id only to deliberately cross-project read (requires it share this project's hub).",
	}, getKnowledgeRecordsHandler(a))

	addTool(server, reg, &mcp.Tool{
		Name:        "get_workflow_state",
		Description: "Read a workflow run's current state and checkpoint history (§18.1).",
	}, getWorkflowStateHandler(a))

	addTool(server, reg, &mcp.Tool{
		Name:        "advance_workflow",
		Description: "Transition a workflow run to a new state, appending a checkpoint (§18.1). Valid next_state values: created, context-building, awaiting-clarification, planning, awaiting-approval, executing, reviewing, blocked, completed, failed, cancelled. Only §9's transition graph is accepted from the current state (e.g. created cannot jump straight to completed); blocked/failed/cancelled are reachable from any non-terminal state. A context-aware run (one created via prepare_work_context or a definition) cannot enter completed without a recorded outcome (record_work_outcome) and, if definition-backed, all steps completed. Call get_workflow_state first if the valid next states from the current one aren't obvious.",
	}, advanceWorkflowHandler(a))

	addTool(server, reg, &mcp.Tool{
		Name:        "get_next_workflow_step",
		Description: "For a definition-backed run, list the steps that are ready to execute now and the ones still blocked (with the reason: unmet dependency, or a capability the workflow does not allow / that is not registered). A disallowed capability surfaces as blocked here, before you execute it (agent-context plan §5.3). An ad hoc run has no steps and says so.",
	}, getNextWorkflowStepHandler(a))

	addTool(server, reg, &mcp.Tool{
		Name:        "complete_workflow_step",
		Description: "Mark one workflow step done, attaching evidence_ids and/or a deviation_reason (one is required), then unlock any dependent steps whose inputs are now satisfied (agent-context plan §5.3). Rejects completing a step whose capability the workflow does not allow. Records a run-scoped capability event for the run's trace.",
	}, completeWorkflowStepHandler(a))

	addTool(server, reg, &mcp.Tool{
		Name:        "propose_project_learning",
		Description: "Open (or reinforce) a reviewed learning proposal for a workflow, project_metadata, or knowledge improvement (agent-context plan §6.2/§6.3). Supply the target_id and the proposed candidate content; it becomes a proposal in the existing artifact-review flow - NEVER a direct canonical write. A human accepts/rejects it in the panel. Deterministic dedup (plan §6.4): an equivalent pending proposal absorbs your evidence_ids/source_run_ids and increments support_count instead of opening a duplicate. Proposals must reference the structured outcome/evidence, not mined chat. Acceptance writes a new immutable revision; for workflows, acceptance never enables the new revision (activation is separate).",
	}, proposeProjectLearningHandler(a))

	addTool(server, reg, &mcp.Tool{
		Name:        "record_work_outcome",
		Description: "Persist the structured result of a context-aware run before completing it (agent-context plan §6.1): status (success|partial|failed), a concise summary, evidence ids, output refs, any workflow deviations, missing/stale context encountered, and reusable observations classified for a later learning proposal (workflow|metadata|knowledge|contradiction|workflow-revision). An observation is a traceable input to a proposal, NOT canonical knowledge. A context-aware run cannot be advanced to completed until this is recorded.",
	}, recordWorkOutcomeHandler(a))

	addTool(server, reg, &mcp.Tool{
		Name:        "ingest_jira_requirement",
		Description: "Fetch a Jira issue and create (or refresh) its requirement knowledge record, so the requirement_id build_task_context and submit_task_graph both hard-require actually exists. Call this before either of those for any requirement_id not already ingested. Read-only against Jira; no approval needed.",
	}, ingestJiraRequirementHandler(a))

	// Milestone 6: Plan-to-Beads and Petruk execution (§10, §11).
	addTool(server, reg, &mcp.Tool{
		Name:        "submit_task_graph",
		Description: "Batch-create TaskContracts and wire their dependency edges into Beads (§10.1-§10.4). The calling role does the decomposition; this tool only creates and wires the result. Each item's requirement_id must already exist as a knowledge record - call ingest_jira_requirement first for any Jira-sourced requirement not yet ingested.",
	}, submitTaskGraphHandler(a))

	addTool(server, reg, &mcp.Tool{
		Name:        "list_ready_tasks",
		Description: "List Beads issues with no active blockers (§9's 'Petruk executes ready task'). Read-only. Returns at most `limit` issues (default 50).",
	}, listReadyTasksHandler(a))

	addTool(server, reg, &mcp.Tool{
		Name:        "claim_ready_task",
		Description: "Atomically claim the first ready Beads issue matching the filters (§11.3's 'claim task' step). Mutates issue state, returning the single claimed issue. The optional assignee filters which candidate issues are considered; it is NOT the claimer - bd assigns the claimed issue to the invoking bd user itself.",
	}, claimReadyTaskHandler(a))

	addTool(server, reg, &mcp.Tool{
		Name:        "build_task_context",
		Description: "Assemble the fresh, bounded per-task execution context (§11.2) and write it as this task's task.yaml evidence (§17.2). Read-only against the knowledge store. requirement_id must already exist as a knowledge record - call ingest_jira_requirement first for any Jira-sourced requirement not yet ingested. Resuming the same task_id (e.g. impl -> tests -> review): task_scope, task_acceptance_criteria, task_definition_of_done, task_expected_files_or_components, affected_symbols_and_files, and required_tests each default to the value from that task_id's last call when omitted - pass only the fields that actually changed, not the full payload every time.",
	}, buildTaskContextHandler(a))

	addTool(server, reg, &mcp.Tool{
		Name:        "start_task_execution",
		Description: "Create this task's isolated worktree and open its evidence bundle/journal (§11.1 steps 1-4). Requires a prior approved worktree-creation request: this is a human-run CLI step, not another MCP tool - ask the user to run `punakawan worktree approve <repo-id> <task-id>` in their own terminal, then retry this call.",
	}, startTaskExecutionHandler(a))

	addTool(server, reg, &mcp.Tool{
		Name:        "finish_task_execution",
		Description: "Record this task's final status and remove its isolated worktree (§11.1 step 10).",
	}, finishTaskExecutionHandler(a))

	addTool(server, reg, &mcp.Tool{
		Name:        "write_file",
		Description: "Write one file within a task's worktree, policy-checked and confined to the worktree root (§15.4, §3.1). Use instead of writing to disk directly.",
	}, writeFileHandler(a))

	addTool(server, reg, &mcp.Tool{
		Name:        "bulk_create_files",
		Description: "Create several files within a task's worktree in one call, with the same checks as write_file, best-effort per file.",
	}, bulkCreateFilesHandler(a))

	addTool(server, reg, &mcp.Tool{
		Name:        "check_diff",
		Description: "Stage and check a task's pending changes against policy and a heuristic secret scan (§15.4), writing diff.patch evidence (§17.2). Must pass before commit_task.",
	}, checkDiffHandler(a))

	addTool(server, reg, &mcp.Tool{
		Name:        "run_tests",
		Description: "Run caller-specified compile/test commands through the tool supervisor and record a tests.json evidence report (§11.3, §17.2).",
	}, runTestsHandler(a))

	addTool(server, reg, &mcp.Tool{
		Name:        "check_openapi_compatibility",
		Description: "Diff a base and head OpenAPI spec, classify breaking changes, and record api-diff.json evidence (§13.4, §17.2).",
	}, checkOpenAPICompatibilityHandler(a))

	addTool(server, reg, &mcp.Tool{
		Name:        "list_task_evidence",
		Description: "List every structured EvidenceRecord check_diff/run_tests/check_openapi_compatibility have recorded for a task (punokawan-s12), so a reviewer can enumerate its evidence without knowing the bundle's file-naming convention.",
	}, listTaskEvidenceHandler(a))

	addTool(server, reg, &mcp.Tool{
		Name:        "commit_task",
		Description: "Stage and commit a task's pending changes, refusing to do so unless a prior check_diff passed and the worktree is on a task branch (§15.4). Write the message in Conventional Commits form: imperative subject <=72 chars, a body only when the why is not obvious (reason + impact, not a diff restatement), referencing the concrete source touched and leading with what matters most." + plainLanguageStyleNote,
	}, commitTaskHandler(a))

	addTool(server, reg, &mcp.Tool{
		Name:        "push_task_branch",
		Description: "Push a task's branch to its remote (AEP-M4 §8's 'push branch' step, before create_pr), gated by detected push capability ∩ repository policy ∩ this call's allow_push override. Never force-pushes. Must run before finish_task_execution removes the task's worktree.",
	}, pushTaskBranchHandler(a))

	addTool(server, reg, &mcp.Tool{
		Name:        "create_pr",
		Description: "Create a pull request for a pushed task branch (AEP-M4 §8.1). Templates the caller-supplied Summary/Requirements/Changes/Verification/etc. sections into the PR body verbatim - punakawan does not write any of that content itself, so write them concise, clear, and easy to scan: lead with the most important change and its impact, use terse bullets, and reference the concrete source touched (path/to/file, symbol, endpoint) with added/changed/removed - no filler. If PR creation is not currently possible (no remote, no push access, unsupported provider, no github adapter configured, ...) returns created=false with the specific reason instead of erroring, per §8.1's failure behavior." + approvalGateNote,
	}, createPrHandler(a))

	addTool(server, reg, &mcp.Tool{
		Name:        "review_pr",
		Description: "Fetch a PR's metadata, diff files, CI checks, and (optionally) comments (§8.2). REACTIVE - explicit_trigger must be true only when a human explicitly asked to review this specific PR; never call this for a PR being discovered, CI failing, or any other automatic signal. Punakawan does not review anything itself (ADR-0016): use this output to build Gareng/Petruk review capsules via request_capsule, have Bagong verify findings, then call submit_pr_review_findings with Semar's deduplicated result.",
	}, reviewPrHandler(a))

	addTool(server, reg, &mcp.Tool{
		Name:        "submit_pr_review_findings",
		Description: "Persist review_pr's final, Semar-deduplicated ReviewFinding[] for a PR (§8.2's 'return final review' step).",
	}, submitPrReviewFindingsHandler(a))

	addTool(server, reg, &mcp.Tool{
		Name:        "fetch_unresolved_pr_comments",
		Description: "Fetch a PR's still-open review threads (§8.3). REACTIVE - explicit_trigger must be true only when a human explicitly asked to fix this PR's review comments; never call this for a reviewer commenting, CI failing, or review_pr finishing. Classifying each comment as applicable/already_resolved/stale/conflicting/requires_clarification/major_change_required is the calling agent's judgment, not something this tool determines.",
	}, fetchUnresolvedPrCommentsHandler(a))

	addTool(server, reg, &mcp.Tool{
		Name:        "resolve_review_thread",
		Description: "Mark a review thread resolved (§8.3's final, optional write step). Requires allow=true - review threads are never resolved automatically." + approvalGateNote,
	}, resolveReviewThreadHandler(a))

	addTool(server, reg, &mcp.Tool{
		Name:        "report_discovered_task",
		Description: "Record newly discovered work found mid-execution as a discovered-from task, labeled for Semar's review (§10.4's discovery rule).",
	}, reportDiscoveredTaskHandler(a))

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
		Description: "Comment body format: Markdown, confirmed working (converted to ADF; NOT old wiki markup). Post a pre-rendered clarification comment on a Jira issue and, if a clarification status is configured, transition the issue to it." + plainLanguageStyleNote + approvalGateNote,
	}, requestJiraClarificationHandler(a))

	addTool(server, reg, &mcp.Tool{
		Name:        "check_jira_skippable",
		Description: "Check whether a Jira-sourced requirement's current status is in the configured skip-status list, so a caller can exclude it before submitting a task graph.",
	}, checkJiraSkippableHandler(a))

	addTool(server, reg, &mcp.Tool{
		Name:        "create_jira_issue",
		Description: "Create a new Jira issue (bug, task, or any other issue type the project supports) with a project key, issue type name, and summary; description and parent_key are optional. Returns the new issue's key, status, and URL. issue_type_name is a free-text name, not a fixed enum - it is per-site/per-project, e.g. 'Bug' or 'Task'; discover the real names via call_adapter_operation atlassian.getIssueTypeFieldMeta if unsure. For creating several subtasks under one parent with dedup against existing children, use sync_jira_subtasks instead. run_id is optional for one-off use." + plainLanguageStyleNote + approvalGateNote,
	}, createJiraIssueHandler(a))

	addTool(server, reg, &mcp.Tool{
		Name:        "sync_jira_subtasks",
		Description: "Create Jira subtasks under a parent issue for candidates that don't already exist, deduplicating by normalized summary." + plainLanguageStyleNote + approvalGateNote,
	}, syncJiraSubtasksHandler(a))

	addTool(server, reg, &mcp.Tool{
		Name:        "update_jira_task_progress",
		Description: "Comment body format: Markdown, confirmed working (converted to ADF; do NOT use old Jira wiki markup like h3. or {{code}} - it renders literally). Update a Jira issue's original estimate (points-derived unless given explicitly), add a worklog entry, and/or post a comment. Each action is optional and one run approval covers all selected writes. For a decomposed issue, log against the specific subtask, NOT the parent - use list_jira_subtasks first to get the right issue_id_or_key." + plainLanguageStyleNote + approvalGateNote,
	}, updateJiraTaskProgressHandler(a))

	// Native Jira convenience tools (punokawan-t6y): common ops that previously
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
		Name:        "list_jira_subtasks",
		Description: "List a parent Jira issue's existing subtasks (children) as {key, summary, status}, plus the parent's own key/summary/status. Read-only: no approval needed. Call this before update_jira_task_progress on a decomposed issue to pick the correct subtask key to log work/estimate against, instead of logging on the parent. run_id is optional for one-off use.",
	}, listJiraSubtasksHandler(a))

	addTool(server, reg, &mcp.Tool{
		Name:        "list_jira_linked_issues",
		Description: "List a Jira issue's linked issues (Blocks/Relates/Duplicates/etc.) as {direction, relationship, key, summary, status, issue_type}. Read-only: no approval needed. run_id is optional for one-off use.",
	}, listJiraLinkedIssuesHandler(a))

	addTool(server, reg, &mcp.Tool{
		Name:        "list_jira_comments",
		Description: "List a Jira issue's comments (newest-ordered) as {id, author, body, created, updated}; body is plain text extracted from ADF, not raw ADF, to stay concise. Paged via start_at/max_results (default 20, max 100); Total tells you if more exist. Read-only: no approval needed. To post a comment or reply (comments are flat, not threaded), use add_jira_comment. run_id is optional for one-off use.",
	}, listJiraCommentsHandler(a))

	addTool(server, reg, &mcp.Tool{
		Name:        "add_jira_comment",
		Description: "Post a standalone comment on a Jira issue, and how you reply too (comments are a flat list, not threaded, so a reply is a new comment referencing the earlier one). This is the bare-comment primitive; request_jira_clarification also posts a comment plus a transition, and update_jira_task_progress bundles a comment with estimate/worklog - reach for those only when you want their extra effects. Body is Markdown, converted to ADF; do NOT use old wiki markup. run_id is optional for one-off use." + plainLanguageStyleNote + approvalGateNote,
	}, addJiraCommentHandler(a))

	addTool(server, reg, &mcp.Tool{
		Name:        "list_jira_sync_queue",
		Description: "List outbound adapter writes (Jira syncs) that reached the adapter but failed after passing their approval check, recorded for retry (punokawan-nbz). Defaults to pending entries only.",
	}, listJiraSyncQueueHandler(a))

	addTool(server, reg, &mcp.Tool{
		Name:        "retry_jira_sync_entry",
		Description: "Replay a list_jira_sync_queue entry's failed write through its original adapter. Marks it resolved on success; on failure it stays queued with an incremented attempt count.",
	}, retryJiraSyncEntryHandler(a))

	addTool(server, reg, &mcp.Tool{
		Name:        "submit_jira_assessment",
		Description: "Comment body format: Markdown, confirmed working (converted to ADF; NOT old wiki markup - h3./{{code}} render literally). Post a Jira-formatted comment (headings, bullet lists, a table) covering what exists vs. what needs to change, findings, and open questions for stakeholder decision (important ones flagged), then create subtasks with detailed plans. Each task's Jira original/remaining estimate is set to its AI-assisted implementation time; human-manual time and time saved are narrative only. The calling agent does the assessment and decomposition; this tool only renders, writes, and persists the result." + approvalGateNote,
	}, submitJiraAssessmentHandler(a))

	addTool(server, reg, &mcp.Tool{
		Name:        "reopen_task",
		Description: "Reopen a closed Beads issue, e.g. when Bagong's independent review finds a blocking regression in already-completed work (§8.4). Pairs with report_discovered_task, which covers the 'create a new task' half of the same acceptance criterion.",
	}, reopenTaskHandler(a))

	addTool(server, reg, &mcp.Tool{
		Name:        "search_knowledge",
		Description: "Search the durable knowledge store locally (§11): exact structured identifiers (CVE/GHSA/Sonar rule/Jira key/git hash/...) and aliases outrank BM25F keyword matches, which fall back to fuzzy matching only when keyword search finds nothing. project/repository/module/path only bias ranking (§11.10's scope bonus) - they never filter results out; use types/tags for that. Every result carries an explanation (§11.13) of why it matched. No embeddings, no external model calls: this is a local index over knowledge Punakawan already has, not new reasoning. project_id (ADR-0020, distinct from the ranking-bonus project field above) selects which project's knowledge store to search and defaults to the calling project; naming another project's id deliberately searches it instead, via a lower-fidelity substring scan rather than the ranked BM25 index (which cannot span projects), and only works when that project shares this one's hub.",
	}, searchKnowledgeHandler(a))

	addTool(server, reg, &mcp.Tool{
		Name:        "submit_missing_context_request",
		Description: "Request context a capsule did not include (§6.4). Subagents may request additional context but must not search broadly themselves - this only records the request; it is Semar's (the calling agent's) own next call to search_knowledge, request_capsule, or resolve_missing_context_request that decides what happens to it.",
	}, submitMissingContextRequestHandler(a))

	addTool(server, reg, &mcp.Tool{
		Name:        "list_missing_context_requests",
		Description: "List missing-context requests (§6.4), defaulting to pending ones, so Semar can decide each one's resolution.",
	}, listMissingContextRequestsHandler(a))

	addTool(server, reg, &mcp.Tool{
		Name:        "resolve_missing_context_request",
		Description: "Record Semar's decision on a missing-context request (§6.4): added_to_revision (requires revised_capsule_id from a prior request_capsule call), rejected, or asked_user. Punakawan does not choose between these - it only persists whichever the calling agent picked.",
	}, resolveMissingContextRequestHandler(a))

	addTool(server, reg, &mcp.Tool{
		Name:        "find_prune_candidates",
		Description: "List knowledge records with the signal needed to judge whether they're obsolete: validity_state, superseded_by, source age (source.retrieved_at), and relation_count (how many other records reference this one - the closest real proxy for 'still relied on', since no access/usage telemetry exists). No validity_state is required or assumed eligible for pruning - every state is included unless filtered, and the returned signals are advisory only. Filters/paginates like the underlying store (type/status/validity_state/repository/source/limit/cursor); min_age_days is applied within the fetched page, not globally - use next_cursor to keep scanning. Read-only: pass candidate ids to delete_knowledge to actually remove them.",
	}, findPruneCandidatesHandler(a))

	addTool(server, reg, &mcp.Tool{
		Name:        "delete_knowledge",
		Description: "Bulk-delete specific knowledge records by id, e.g. ones find_prune_candidates or search_knowledge surfaced as stale, superseded, or wrong - so a future search does not keep returning dirty context. Any validity_state may be deleted; naming an id is itself the deliberate act. Not undoable through this tool, but not unrecoverable either: a successful delete is immediately committed to the project's Dolt knowledge store and commit_hash is returned - the deleted records remain fully readable via `SELECT ... FROM knowledge_records AS OF '<commit_hash>~1'` (no checkout needed), or fully restorable with `dolt checkout <commit_hash>~1 -- knowledge_records` in that store's directory. commit_hash is empty when every id was not_found (nothing was actually deleted).",
	}, deleteKnowledgeHandler(a))

	addTool(server, reg, &mcp.Tool{
		Name:        "reset_project_knowledge",
		Description: "Bulk-delete every knowledge record matching a given project/repository/module scope - use when a whole project's knowledge has gone stale and should be re-ingested from scratch rather than pruned record by record. Requires at least one of project/repository/module (an empty scope would match everything). Defaults to a dry run: returns the matching record ids without deleting anything unless confirm=true. A confirmed delete is immediately committed to the project's Dolt knowledge store and commit_hash is returned - see delete_knowledge's description for how to read or restore the pre-delete state from it.",
	}, resetProjectKnowledgeHandler(a))

	// Contradiction Ledger (Gareng, §16-22). Deterministic, no reasoning.
	addTool(server, reg, &mcp.Tool{
		Name:        "submit_contradiction",
		Description: "Record a detected contradiction - a disagreement between sources about one subject (§21/CONTRA-007). Deduplicates deterministically by normalized subject.key (§20): if a contradiction already exists for the same subject the existing record is returned (deduplicated=true) rather than a duplicate created. New records start at status detected and block by default only when severity is critical. Gated to role Gareng's contradictions capability.",
	}, submitContradictionHandler(a))

	addTool(server, reg, &mcp.Tool{
		Name:        "list_contradictions",
		Description: "List contradictions in the ledger. With no status filter it returns only still-open ones (detected/triaged/needs_clarification/resolution_proposed); pass status to filter to exactly one lifecycle state. Read-only.",
	}, listContradictionsHandler(a))

	addTool(server, reg, &mcp.Tool{
		Name:        "resolve_contradiction",
		Description: "Record a contradiction's confirmed resolution statement and who confirmed it, advancing it to resolved (§18). Only valid from resolution_proposed. Gated to role Gareng's contradictions capability.",
	}, resolveContradictionHandler(a))

	// Cross-Repository Impact Graph (Gareng, §23-31). Deterministic query, no reasoning.
	addTool(server, reg, &mcp.Tool{
		Name:        "analyze_impact",
		Description: "Answer \"if subject_id changes, what else is affected?\" by a cycle-safe traversal of the impact graph (§26/§29), returning direct/transitive impact plus affected repositories, tests, deployments, owners, missing coverage, and any related contradictions. depth defaults to 3; set refresh=true to reconcile the structural graph from the workspace first. Read-only against durable state.",
	}, analyzeImpactHandler(a))

	addTool(server, reg, &mcp.Tool{
		Name:        "record_impact_edge",
		Description: "Record a discovered dependency edge (from->to of a given type and confidence) into the impact graph (§29/IMPACT-012). Idempotent by (from,to,type): re-recording an edge supersedes the prior one. Gated to role Gareng's cross_repository_impact capability.",
	}, recordImpactEdgeHandler(a))

	addTool(server, reg, &mcp.Tool{
		Name:        "verify_impact_coverage",
		Description: "Bagong's coverage check (§30/IMPACT-014): traverse the impact graph from subject_id and report whether every reachable symbol/operation is tested and whether any reachable edge is in dispute. Returns covered=true only when nothing is missing coverage and nothing is disputed, plus the affected repositories, missing-coverage nodes, and related contradictions. Gated to role Bagong's cross_repository_verification capability.",
	}, verifyImpactCoverageHandler(a))

	// Change Dossier (§32-39): the durable, versioned proof artifact for a change.
	addTool(server, reg, &mcp.Tool{
		Name:        "create_change_dossier",
		Description: "Create a new Change Dossier (§37) - the durable proof artifact tracking a change from objective to completion. Starts at status draft. Gated to role Semar's change_dossier capability.",
	}, createChangeDossierHandler(a))

	addTool(server, reg, &mcp.Tool{
		Name:        "add_dossier_claim",
		Description: "Attach a claim (producer role, type, statement, optional evidence ids) to a dossier's append-only claim log (§34). Status defaults to claimed. Gated to role Semar's change_dossier capability.",
	}, addDossierClaimHandler(a))

	addTool(server, reg, &mcp.Tool{
		Name:        "verify_dossier_claim",
		Description: "Record an independent verification of a dossier claim (§34). Rejected if by_role equals the claim's producer role - a role cannot verify its own claim. Sets the claim to verified.",
	}, verifyDossierClaimHandler(a))

	addTool(server, reg, &mcp.Tool{
		Name:        "dispute_dossier_claim",
		Description: "Record an independent dispute of a dossier claim (§34), setting it to disputed - a blocking finding for finalize_dossier. Rejected if by_role equals the claim's producer role.",
	}, disputeDossierClaimHandler(a))

	addTool(server, reg, &mcp.Tool{
		Name:        "add_dossier_evidence",
		Description: "Attach an evidence record (type, artifacts, source, result) to a dossier (§35). The caller supplies any artifact sha256; the store does not compute it. Gated to role Semar's change_dossier capability.",
	}, addDossierEvidenceHandler(a))

	addTool(server, reg, &mcp.Tool{
		Name:        "set_dossier_contradictions",
		Description: "Set a dossier's resolved/unresolved contradiction sets (§34). Unresolved contradictions are blocking findings that prevent finalize_dossier. Gated to role Semar's change_dossier capability.",
	}, setDossierContradictionsHandler(a))

	addTool(server, reg, &mcp.Tool{
		Name:        "set_dossier_impact",
		Description: "Set a dossier's impact section (§33): affected repositories, deliberately excluded repositories (each with a reason), and areas with missing coverage. Rendered in the markdown/PR-summary exports. Gated to role Semar's change_dossier capability.",
	}, setDossierImpactHandler(a))

	addTool(server, reg, &mcp.Tool{
		Name:        "finalize_dossier",
		Description: "Complete a dossier, but only when it is at verified status and free of blocking findings (§36): no unresolved contradictions, no missing plan items or unapproved deviations, and no disputed/rejected claims. Returns a clear error listing every blocker when it cannot complete. Gated to role Semar's change_dossier capability.",
	}, finalizeDossierHandler(a))

	addTool(server, reg, &mcp.Tool{
		Name:        "export_dossier",
		Description: "Render a dossier as human-readable markdown (format=md, default) or deterministic JSON (format=json), including the §38 summary indicators. Read-only.",
	}, exportDossierHandler(a))

	// Handoff Capsule (§40-43): compact, resumable snapshots of in-progress work.
	addTool(server, reg, &mcp.Tool{
		Name:        "create_handoff_capsule",
		Description: "Create a Handoff Capsule (§40) - a compact, resumable snapshot of in-progress work that references plan/task/dossier/contradictions/evidence by id rather than copying them, so work can continue across sessions, clients, and people without the transcript. Stamps the current role-config revision for resume-time validation. Gated to role Semar's handoff_capsule capability.",
	}, createHandoffCapsuleHandler(a))

	addTool(server, reg, &mcp.Tool{
		Name:        "get_handoff_capsule",
		Description: "Read a handoff capsule by id (§41). A capsule that was never written is returned as an empty capsule carrying just the id. Read-only.",
	}, getHandoffCapsuleHandler(a))

	addTool(server, reg, &mcp.Tool{
		Name:        "validate_handoff_capsule",
		Description: "Classify whether a capsule can be resumed (§42): resumable, refresh_required (inputs moved but still resumable after reloading the listed items), blocked (a referenced dependency is gone), invalid (repository state diverged), or superseded (must not resume silently). Read-only.",
	}, validateHandoffCapsuleHandler(a))

	addTool(server, reg, &mcp.Tool{
		Name:        "resume_from_handoff",
		Description: "Validate a capsule and, if resumable or refresh_required, return only the smallest necessary verified context (§43) - objective, current phase/task and next action, accepted plan ref, open contradictions, unresolved risks - plus any required refresh steps. Errors when superseded, blocked, or invalid, explaining why.",
	}, resumeFromHandoffHandler(a))
}
