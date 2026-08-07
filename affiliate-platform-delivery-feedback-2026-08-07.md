# Punakawan Delivery Feedback: Affiliate Platform

Date: 2026-08-07  
Reviewer: Codex, acting as the connected reasoning agent  
Repository under delivery: `gdncomm/affiliate-platform`  
Punakawan repository reviewed: `punokawan`

## Executive summary

Punakawan is valuable and directionally correct. It materially improved Jira safety, traceability, structured assessment, worklog discipline, and review documentation during the delivery of TRF-21973 and TRF-21749. The strongest parts were the normalized Jira interface, durable run-scoped approvals, task/evidence records, and the requirement that a review state its uncertainty instead of inventing evidence.

However, in this real delivery, Punakawan was not able to complete the advertised end-to-end path by itself. The implementation reached production-quality pull requests only because the connected agent manually created Git worktrees, ran Git operations directly, audited diffs outside Punakawan, reauthenticated GitHub CLI, created PRs through a fallback, repaired workflow state after the fact, and worked around mutable evidence files. The largest blockers were not reasoning quality; they were missing or unreliable execution capabilities at system boundaries.

My honest assessment is:

- Punakawan is already useful as a **governance, persistence, and Jira orchestration layer**.
- It is not yet dependable as the sole **end-to-end delivery runtime** for a private repository.
- The current happy path is too sensitive to adapter configuration, hard-coded executable paths, workspace layout, test environment state, and manually synchronized lifecycle records.
- The product should fail early when those capabilities are unavailable. It should not discover them after assessment, implementation, and branch creation.

Overall score for this delivery: **3/5**. The workflow discipline was strong; execution reliability and efficiency were not.

## Scope and evidence

The feedback comes from one complete delivery involving two parent Jira issues returned by the requested JQL:

1. **TRF-21973** — prevent stale-empty caching and improve BRS recommendation diagnostics.
2. **TRF-21749** — make direct and indirect commission-rule settings editable, including clearing an existing indirect rule.

The delivery produced:

- `feature/TRF-21973` and PR #350.
- `feature/TRF-21749` and PR #351.
- Jira assessments, reused Jira subtasks, estimates, comments, and worklogs.
- Punakawan requirements, task contexts, evidence bundles, dossiers, Bagong reviews, and recorded outcomes.
- 196 passing command-module tests for TRF-21973.
- 195 passing command-module tests for TRF-21749.
- Follow-up fixes for a stale integration-test expectation and a Sonar ternary finding.

The review therefore covers the original implementation, CI feedback, Jira writes, Git/PR publication, and follow-up correction loops.

## What went well

### 1. Jira reads were compact and useful

The Atlassian adapter normalized Jira issues into the fields needed for implementation: key, summary, description, status, issue type, subtasks, labels, links, priority, and source metadata. This avoided carrying a large raw REST or ADF payload in the model context.

The requested JQL returned exactly two parent issues, and both were correctly identified as active work. `check_jira_skippable` correctly reported that neither should be skipped.

This is one of Punakawan's clearest strengths: it turns Jira into usable planning context instead of an opaque API response.

### 2. Existing Jira subtasks were reused instead of duplicated

`submit_jira_assessment` and subtask inspection allowed the delivery to reuse:

- TRF-21974, TRF-21975, TRF-21976.
- TRF-21750, TRF-21751, TRF-21752.

No duplicate subtasks were created. The work was logged against the existing development, testing, and review subtasks, matching Punakawan's explicit worklog guidance.

This behavior is important in real Jira projects, where duplicate decomposition quickly becomes noise.

### 3. Run-scoped approval was safe and durable

External writes were blocked until the user explicitly approved both adapter runs. After approval, the same run authorization covered Jira comments and worklogs without repeatedly asking for confirmation.

The approval survived pauses and follow-up work. That is a good balance: strict before the first write, low-friction afterward.

The approval system also made the boundary clear: Punakawan could reason and prepare freely, but external mutation remained human-authorized.

### 4. High-level Jira write tools worked reliably

`update_jira_task_progress` successfully:

- Added worklogs.
- Posted concise implementation/test/review comments.
- Kept estimates and transitions unchanged when they were not requested.

`add_jira_comment` successfully added PR summaries and links to the parent issues.

The structured results were easy to verify: `comment_posted`, `worklog_added`, `estimate_updated`, and `transitioned`.

### 5. Task context and role capsules improved review discipline

`build_task_context` forced the agent to restate:

- Scope.
- Acceptance criteria.
- Expected files and symbols.
- Required tests.
- Definition of done.

`request_capsule` made the Bagong review constraints explicit. In particular, it prohibited writes and required direct evidence rather than Petruk's summary.

That separation was useful. Even though the same model played both roles, the process reduced the chance that the reviewer would simply repeat the implementation narrative.

### 6. Bagong encouraged honest uncertainty

The Bagong workflow explicitly required the review to state that Playwright Milestone 7 evidence was unavailable. It also required the TRF-21749 review to distinguish:

- Command-level tests that passed.
- Integration-test source that compiled.
- The integration test body that could not run locally because the Spring context required GCP Application Default Credentials.

That prevented fabricated E2E or integration claims.

The review validator also rejected an attempted review summary that did not explicitly say that no actionable problems were found. The exact phrase requirement is brittle, but the underlying goal—forcing a clear conclusion—is good.

### 7. Test evidence and work outcomes created a durable trail

`run_tests`, `list_task_evidence`, dossiers, Bagong reviews, and `record_work_outcome` created a reconstructable record of:

- Commands run.
- Exit codes.
- Test counts.
- Deviations.
- Commits.
- Branches.
- PR URLs.
- Known environment gaps.

The outcome records were especially useful because they allowed the agent to record the actual path instead of pretending the canonical path succeeded.

### 8. Deviations were first-class

The final outcomes honestly recorded deviations such as:

- Manual isolated Git worktrees.
- Manual diff audit.
- GitHub CLI fallback.
- Local GCP ADC limitation.
- External Jenkins rather than GitHub Actions.

This is the correct product philosophy. Real delivery systems need an honest deviation record more than they need a cosmetically perfect workflow graph.

## What went wrong

## A. Punakawan/system failures

### 1. The GitHub PR capability was unavailable

`create_pr` failed for both branches with:

> authentication unavailable: no github adapter configured: adapters: unknown adapter id "github"

This was discovered only after the branches had been implemented, tested, committed, and pushed.

The connected GitHub app also returned 404 for the private repository, and the local `gh` token was expired. The agent ultimately had to:

1. Attempt Punakawan PR creation.
2. Attempt the GitHub app.
3. Attempt browser fallback.
4. Start GitHub device authentication.
5. Ask the user to approve the device flow from a phone.
6. Verify the `gdncomm-yunaz-ramadhan` identity.
7. Create the PRs with `gh`.

For a product that advertises Jira-to-PR delivery, this is a critical missing preflight.

Expected behavior:

- Validate the GitHub adapter, repository access, account identity, remote, and PR permission before implementation starts.
- If unavailable, stop early with one actionable error.
- Expose a supported fallback contract rather than forcing the agent to discover authentication surfaces interactively.

### 2. Git/worktree execution was not usable through Punakawan

`start_task_execution` rejected the repository/worktree arrangement. The agent had to create isolated worktrees manually:

- `.worktrees/TRF-21973`.
- `.worktrees/TRF-21749`.

`check_diff` then failed because Punakawan was configured to use a nonexistent executable:

`/Users/yunaz.ramadhan/.signals/bin/git`

As a result:

- No canonical `diff.patch` was produced.
- No canonical `api-diff.json` was produced.
- Diff checking had to use direct `git diff --check` and manual review.
- Punakawan commit/push helpers could not be trusted for the delivery.

Expected behavior:

- Resolve `git` from the current process `PATH` or from a validated project toolchain.
- Run a startup preflight that verifies every configured executable.
- Treat linked Git worktrees as first-class repositories.
- Accept a task-specific worktree path and validate its relationship to the primary repository.
- Never rely on a stale absolute path without a clear remediation command.

### 3. A run retrieved the wrong requirement

The TRF-21973 feature run's prepared context included:

`pkw:req/affiliate-platform/TRF-21850`

instead of TRF-21973.

A later fresh `build_task_context` call pulled the correct TRF-21973 parent requirement, so implementation continued safely. However, the run's persisted context snapshot remained wrong and appeared again in the recorded outcome.

This is a serious provenance problem. An exact Jira delivery run must never silently attach a different requirement because it happens to rank well in retrieval.

Expected behavior:

- Exact requirement IDs supplied by the caller must be pinned, not searched.
- Retrieval should supplement the pinned requirement, never replace it.
- If the run objective/task key and retrieved requirement key disagree, fail closed.
- The outcome should reference the final verified context, not the original incorrect snapshot.

### 4. Evidence files were mutable

Repeated `run_tests` calls wrote to the same path:

`.punakawan/evidence/<run>/<task>/tests.json`

`list_task_evidence` returned multiple evidence records with different content hashes pointing to that same path. A dossier record for TRF-21973 also retained an earlier caller-supplied hash after the file had been overwritten. The agent had to calculate a new hash and add a second “final” evidence record.

This weakens the evidence model:

- A historical record no longer necessarily points to its historical bytes.
- A caller-supplied SHA can be stale.
- A dossier may look cryptographically grounded while referencing mutable content.

Expected behavior:

- Store every test invocation as an immutable artifact, for example `tests/<timestamp-or-id>.json`.
- Compute SHA-256 server-side.
- Reject a supplied hash that does not match the bytes.
- Keep a stable “latest” pointer separately if needed.
- Make dossier evidence content-addressed.

### 5. Evidence paths are not portable

An ad hoc run ID such as:

`pkw:run/affiliate-platform/adhoc-1786084693329696000`

was embedded directly into filesystem paths. The colon is invalid in Windows filenames and the slashes create unintended directory nesting.

Expected behavior:

- Use a sanitized filesystem slug or a hash for directories.
- Keep the canonical run ID inside metadata, not in a raw path component.

### 6. Workflow state required manual replay after completion

Substantial work occurred while runs remained in `created`. After implementation, testing, Jira logging, review, push, and PR creation, the agent had to manually advance each run through:

1. `context-building`
2. `planning`
3. `awaiting-approval`
4. `executing`
5. `reviewing`
6. `completed`

Attempting to move directly from `created` to `completed` was rejected.

This made the state machine descriptive only after the fact. It did not accurately reflect the live workflow.

Expected behavior:

- Relevant tool calls should transition or checkpoint the run automatically.
- Provide a single `finalize_workflow` operation that validates evidence and fills safe missing transitions.
- If manual transition is required, `get_next_workflow_step` should be the normal control surface and should be concise.
- A recorded successful outcome plus an approved Bagong review should be sufficient to complete an ad hoc run.

### 7. Tool descriptions consumed excessive context

Every Punakawan tool description repeated a very long common preamble explaining:

- The reasoning boundary.
- Workflow versus adapter approvals.
- Jira assessment rules.
- Bagong review rules.
- RTK guidance.

Discovering a handful of deferred tool schemas produced tens of thousands of tokens and truncated output. The supposedly token-efficient workflow therefore spent a large amount of context on duplicated tool documentation.

Expected behavior:

- Put shared instructions in one MCP resource, server instruction, or prompt.
- Keep each tool description focused on its unique contract.
- Return schema-only discovery when requested.
- Allow the client to fetch role/workflow guidance once and cache its revision.

### 8. `run_tests` returned too much raw output

Some `run_tests` results returned more than 100,000 characters of Maven output, including repeated stack traces and Spring condition reports. Tool output was truncated, and the agent had to run additional searches to recover the actual root cause.

This contradicts the stated RTK/token-efficiency goal.

Expected behavior:

- Return a compact summary by default: command, exit code, duration, module summary, test counts, and extracted failure causes.
- Persist full stdout/stderr as artifacts.
- Include a bounded tail and direct artifact reference.
- Deduplicate repeated retry failures.
- Extract the first causal exception separately from secondary Spring/JUnit noise.

### 9. Capability checks happened too late

The following were discovered only during execution:

- GitHub adapter missing.
- Private-repo GitHub app access missing.
- Expired `gh` authentication.
- Invalid configured Git path.
- Worktree policy incompatibility.
- GCP ADC missing.
- MongoDB unavailable locally.
- Mockito/Byte Buddy attachment blocked in one sandboxed run.

Expected behavior:

Introduce a delivery preflight that reports:

- Adapter readiness.
- Repository/remote access.
- Git executable.
- Worktree support.
- PR account and permission.
- Required test profiles and external services.
- Whether commands can attach agents or spawn subprocesses.

The run should classify each item as required, optional, or expected-to-defer-to-CI.

### 10. Beads bootstrap caused an unwanted base-branch commit

To satisfy Punakawan task tracking, `bd init` was run in the affiliate-platform repository. It automatically created a local commit on `release/2026-08-05`:

`f7e1987863 bd init: initialize beads issue tracking`

The feature branches were deliberately based on `origin/release/2026-08-05`, so the metadata commit did not enter either PR. Nevertheless, the base checkout was left ahead by one local commit with Punakawan/Beads metadata.

This was partly an agent/operator mistake: initialization should not have been allowed to mutate the release branch. Punakawan should still make this safer.

Expected behavior:

- Detect whether Beads is initialized without mutating the repository.
- Require explicit approval before initialization or automatic commits.
- Initialize metadata in a dedicated management branch/worktree or outside the code branch.
- Never create a commit on a protected/base branch as an implicit workflow step.

### 11. Review “independence” is procedural, not model-independent

Bagong required fresh context and direct evidence, which is useful. But the same connected model implemented the change and authored the review. Punakawan itself does not reason.

Therefore “independent review” currently means:

- A separate prompt.
- A separate capsule.
- A fresh evidence read.
- A distinct persisted review record.

It does not mean:

- A different model.
- A different agent process.
- A different person.

The documentation should state this distinction prominently. If stronger independence is desired, Punakawan should optionally require a different agent identity/model/session for Bagong.

### 12. Semantic review validation relied on wording

A Bagong review with no findings was rejected because `honest_summary` did not contain an explicit phrase such as “no actionable problems.”

The intention is reasonable, but phrase-level validation is brittle and language-dependent.

Expected behavior:

- Add an explicit structured conclusion such as `actionable_findings: false`.
- Validate consistency between the conclusion and findings arrays.
- Treat prose as explanation, not as a machine-state signal.

## B. Mistakes by the connected agent

These should not be blamed entirely on Punakawan.

### 1. I did not update every existing integration expectation initially

The TRF-21749 production change correctly cleared indirect attribution, and the new tests covered clearing. However, an existing integration test still expected the old indirect rule in the response and history's new value. Jenkins found the mismatch.

I should have searched all existing tests and fixtures for `indirectAttributionRuleCommission` before declaring the change complete.

Punakawan could help by generating an impacted-test search checklist, but the miss was mine.

### 2. I briefly proposed the wrong compatibility interpretation

After Jenkins failed, I initially interpreted an omitted indirect field as “preserve existing value.” The user corrected this: for this requirement, missing indirect values on save are expected to clear the rule.

The Jira text said the setting must be editable, but it did not explicitly define omitted/null/empty semantics. I should have requested or recorded that semantic decision earlier instead of inferring compatibility behavior from an old test.

Punakawan could improve this through acceptance-criteria gap detection:

- What does omitted mean?
- What does explicit null mean?
- What does an empty object mean?
- Is update behavior PATCH-like or replacement-like?

### 3. I used a ternary that violated the project's Sonar convention

The implementation used a ternary inside `setIndirectAttributionRuleCommission`. Sonar required an `if/else`. The user also clarified a future convention: avoid ternaries and fully qualified names in implementation code.

I should have inspected the project's quality profile and conventions before writing code. This convention was later persisted in Beads memory.

Punakawan could prevent this class of follow-up by retrieving:

- Sonar rules.
- Checkstyle/PMD rules.
- Repository coding conventions.
- Prior accepted review findings.

and including them in the Petruk/Bagong context.

### 4. I initialized Beads in the main checkout

The unwanted local `bd init` commit was caused by my workflow decision. A safer agent should use a dedicated metadata worktree or ask before initializing.

The product should add guardrails, but the immediate operator responsibility is clear.

### 5. The first review was too optimistic

Bagong approved TRF-21749 with a documented integration gap because the full command suite passed and the application integration test could not start locally. That approval did not catch:

- The stale existing integration expectation.
- The later Sonar ternary finding.

The review truthfully reported its evidence, but “approved” may have sounded stronger than the evidence justified. A better verdict would distinguish:

- Logic approved.
- Application integration pending CI.
- Quality gate pending CI.

## Blockers and what should be provided

## Critical blockers

| Blocker | Impact | What Punakawan/platform should provide |
|---|---|---|
| No configured GitHub adapter | Cannot create PRs through Punakawan | A supported GitHub adapter with private-repo auth, account identity, PR create/read/update, and capability preflight |
| Invalid hard-coded Git path | Cannot generate canonical diff or use task Git workflow | PATH-based executable discovery, configurable validated toolchains, and a startup diagnostic |
| Worktree policy incompatibility | Cannot start/commit tasks in isolated branches | First-class linked-worktree support and explicit repository/worktree parameters |
| Wrong requirement retrieved | Provenance can attach work to an unrelated requirement | Exact requirement pinning and mismatch fail-closed validation |
| Mutable evidence artifacts | Historical hashes can point at overwritten bytes | Immutable per-invocation artifacts with server-computed hashes |
| Missing test environment contract | Expensive tests fail before reaching the test body | Project test profiles, dependency/credential preflight, and explicit “delegate to CI” policy |
| Implicit base-branch mutation from Beads bootstrap | Pollutes protected checkout and risks accidental push | Non-mutating detection, explicit approval, and isolated metadata initialization |

## Project inputs that should be available before implementation

Punakawan should expect or help the user configure a project delivery profile containing:

1. **Repository**
   - Canonical repository ID and private-repo URL.
   - Base branch.
   - Branch naming policy.
   - Worktree root and whether linked worktrees are allowed.
   - Protected branches that must never receive automatic commits.

2. **GitHub**
   - Adapter installed/enabled.
   - Required account identity.
   - Repository permission check.
   - Draft versus ready PR policy.
   - Required labels, reviewers, teams, and PR template.

3. **Jira**
   - Parent versus subtask worklog policy.
   - Status-transition policy.
   - Estimate policy.
   - Required comment format.

4. **Toolchain**
   - Valid `git`, `rtk`, Maven/Gradle, Java, and language-runtime paths.
   - Whether subprocess/JVM agent attachment is allowed.
   - Network requirements.

5. **Testing**
   - Tests runnable locally without credentials.
   - Tests requiring GCP ADC, databases, Kafka, or other services.
   - Safe local mocks/test profiles.
   - Tests that must be delegated to Jenkins/CI.
   - Expected retry policy.

6. **Quality rules**
   - Sonar profile or relevant rules.
   - Formatting/checkstyle/PMD configuration.
   - Repository conventions such as avoiding ternaries and FQNs.
   - Security/logging restrictions.

7. **Requirement semantics**
   - PATCH versus replacement behavior.
   - Null/empty/omitted field semantics.
   - Compatibility expectations.
   - API/status/error contracts.

## Inefficiencies observed

### 1. Too many overlapping records

The same delivery facts were manually repeated across:

- Jira assessment.
- Jira subtasks.
- Beads tasks.
- Punakawan task graph.
- Task context.
- Test evidence.
- Change dossier.
- Bagong review.
- Work outcome.
- PR body.
- Jira completion comment.

Each record has value, but manual synchronization is expensive and creates drift risk.

Improvement:

- Treat evidence and task facts as canonical structured records.
- Generate the PR body, Jira summary, dossier, and outcome views from those records.
- Avoid asking the agent to restate test counts, branches, and risks repeatedly.

### 2. Workflow ceremony was larger than the implementation

For two modest tickets, the agent called many orchestration tools before and after code changes. The state machine was then replayed manually.

Improvement:

- Offer a compact “standard feature delivery” workflow that bundles safe steps.
- Keep advanced role/dossier controls available but optional.
- Auto-create capsules/evidence links when the standard workflow is selected.

### 3. Schema discovery was expensive

Deferred tool discovery returned huge descriptions because every tool repeated the common Punakawan instruction block.

Improvement:

- One shared instruction resource.
- Short per-tool descriptions.
- A schema-only discovery endpoint.

### 4. Test failure diagnosis required redundant calls

Large Maven output was truncated, so the agent had to search Surefire reports separately to find:

- Missing GCP Application Default Credentials.
- Mongo connection refusal.
- Mockito/Byte Buddy self-attach failure.

Improvement:

- Parse Surefire/JUnit XML automatically.
- Extract the causal chain.
- Store raw logs without returning them by default.

### 5. GitHub failure was discovered at the end

The delivery spent time preparing a Punakawan PR payload that could never be submitted.

Improvement:

- Preflight PR capability before coding.
- Cache the result for the run.

### 6. Worklog distribution was manual

The agent had to list subtasks, choose hours per dev/test/review subtask, call six writes, and later add follow-up time.

Improvement:

- Let the assessment/task graph define worklog allocation.
- Add a reviewed “log actual work for completed tasks” operation.
- Show the exact proposed allocation at approval time.

### 7. CI is external but not integrated

Jenkins appeared to GitHub as an external check. Punakawan and the GitHub Actions workflow could report the URL but not read structured Jenkins results.

Improvement:

- Add a pluggable CI adapter interface.
- At minimum, model external checks as pending/pass/fail with a link and user-supplied diagnostic evidence.
- Do not label Jenkins inspection as GitHub Actions capability.

## Prioritized recommendations

## P0 — required for dependable end-to-end delivery

1. **Add a run preflight**
   - Verify Jira adapter.
   - Verify GitHub adapter and account.
   - Verify repository access and base branch.
   - Verify Git executable and worktree compatibility.
   - Verify required test environment capabilities.
   - Fail before implementation if a required capability is missing.

2. **Pin exact requirements**
   - An explicit requirement ID must never be replaced by ranked retrieval.
   - Add mismatch detection between Jira key, task ID, and context records.

3. **Make evidence immutable**
   - Unique artifact per command invocation.
   - Server-computed hashes.
   - Content-addressed evidence.
   - No multiple historical records pointing to mutable bytes.

4. **Make repository bootstrap non-mutating**
   - Never auto-commit on a base/protected branch.
   - Require approval for Beads initialization.
   - Support an external or dedicated metadata workspace.

5. **Support linked worktrees end to end**
   - Context, tests, diff, commit, push, and PR must operate on the same explicit worktree.

## P1 — major reliability and efficiency improvements

1. **Compact test results**
   - Return summaries and causal failures.
   - Persist full output.

2. **Automate workflow lifecycle**
   - Tool calls should advance states.
   - Add one validated finalize operation.

3. **Create a project delivery profile**
   - Repository, GitHub, Jira, test, and quality policy in one versioned configuration.

4. **Generate downstream narratives**
   - PR bodies, Jira comments, and outcomes should render from canonical evidence.

5. **Model verification confidence**
   - Separate logic review, integration verification, quality-gate verification, and E2E verification.

6. **Ingest quality conventions**
   - Sonar/checkstyle rules and team conventions should be part of task context.

## P2 — maturity improvements

1. Optional cross-agent/model Bagong review.
2. Pluggable external CI adapters.
3. Better approval UI for clients without MCP elicitation forms.
4. Structured worklog allocation.
5. Filesystem-safe run identifiers.
6. Structured review conclusion instead of phrase matching.

## Suggested acceptance tests for Punakawan itself

The following product tests would have caught most issues from this delivery:

1. **Cold repository with no Beads**
   - Start on a protected release branch.
   - Confirm Punakawan does not create or commit metadata without approval.

2. **Private GitHub repository without adapter**
   - Confirm preflight fails before planning/implementation.
   - Error must name the missing adapter and setup action.

3. **Exact Jira requirement**
   - Pin TRF-21973 while a similar TRF-21850 record exists.
   - Confirm no run context can select TRF-21850 as the parent.

4. **Linked worktree delivery**
   - Create a feature worktree.
   - Run context, tests, diff, commit, push, and PR entirely through Punakawan.

5. **Repeated test runs**
   - Run the same task tests three times.
   - Confirm three immutable artifact paths and hashes remain valid.

6. **Missing GCP ADC**
   - Confirm preflight classifies the integration test as unavailable locally.
   - Confirm the run does not spend minutes starting the full context before reporting the known blocker.

7. **Large Maven failure**
   - Confirm the MCP response stays bounded.
   - Confirm the first causal exception is extracted.
   - Confirm full output is available as an artifact.

8. **Project quality convention**
   - Supply a “no ternary in implementation code” rule.
   - Confirm Petruk includes it in the plan and Bagong checks it before PR creation.

## Proposed north-star workflow

A dependable standard delivery should look like this:

1. Resolve exact Jira issue.
2. Run capability preflight.
3. Present missing requirements or blockers.
4. Obtain one approval for external writes.
5. Assess and reuse/create tasks.
6. Create or attach an isolated feature worktree.
7. Implement with project quality rules in context.
8. Run available local tests; classify unavailable tests for CI.
9. Produce immutable diff/test/API evidence.
10. Run a review with explicit confidence levels.
11. Commit and push through the same worktree.
12. Create the PR using the preflighted account.
13. Log work and render Jira/PR summaries from canonical evidence.
14. Finalize the workflow automatically.

The connected agent should only need to intervene for reasoning, code decisions, and genuinely missing user choices—not to repair infrastructure plumbing.

## Final assessment

Punakawan's core idea is strong. The most difficult parts of agentic software delivery are not generating code; they are keeping context correct, preserving evidence, controlling external writes, and leaving a trustworthy trail. Punakawan clearly understands that problem.

The current implementation already adds real value in Jira normalization, approval gating, durable assessment, worklogs, dossiers, and explicit uncertainty. Those pieces should be preserved.

The next investment should focus less on adding more workflow ceremony and more on making the existing golden path reliable:

- Preflight every external capability.
- Pin exact requirements.
- Make evidence immutable.
- Treat Git worktrees as first-class.
- Keep the base branch untouched.
- Make tool responses compact.
- Make workflow state follow actual actions automatically.

Once those boundaries are dependable, Punakawan can credibly deliver on its Jira-to-PR promise. Until then, it is best described as a strong orchestration and governance layer that still requires an experienced connected agent to complete and repair the execution path.
