# Punakawan Architecture Enhancement Plan

## Document Status

- **Status:** Proposed
- **Scope:** Core Punakawan architecture
- **Primary components:** Semar, Gareng, Petruk, Bagong, Git integration, issue tracking, durable knowledge, BM25 search
- **Target outcome:** A reliable multi-agent engineering assistant with isolated context, automatic project capability detection, reactive PR workflows, optional Jira integration, and low-cost local knowledge retrieval
- **Canonical implementation language:** Go for runtime, orchestration infrastructure, command execution, indexing, and durable knowledge services; TypeScript may be used for adapter-level integrations where it provides clear ecosystem advantages
- **Non-goal:** Replacing Git, Jira, Trivy, Sonar, package managers, or source-control providers with custom reimplementations

---

# 1. Executive Summary

This plan enhances Punakawan into a structured multi-agent engineering system with four distinct roles:

- **Semar** acts as the orchestrator and the only agent with broad project and knowledge access.
- **Gareng** independently challenges assumptions, constraints, and risks.
- **Petruk** independently plans and implements approved work.
- **Bagong** starts from a clean context and verifies whether the result is actually supported by requirements and evidence.

The architecture deliberately avoids allowing every agent to read the entire conversation, repository history, and durable knowledge base. Broad context is held by Semar and distributed through small, immutable context capsules. This reduces hallucination, cross-agent contamination, duplicated reasoning, and accidental scope expansion.

Git integration is automatically detected from the active project. Punakawan must inspect the repository, remotes, provider, authentication, current branch, worktree state, and available permissions. There is no manually selected Git mode. The user may still explicitly skip Git-related work or restrict commits, pushes, and pull-request creation.

Three high-level Git workflows are provided:

- `create_pr`, which may be part of the normal implementation workflow.
- `review_pr`, which is reactive and can only run after an explicit user request.
- `fix_pr_review`, which is reactive and can only run after an explicit user request.

Jira is optional. Punakawan must always use **BD** as its internal task graph and execution ledger. Jira, when configured, acts as an external collaboration and reporting adapter. If Jira is missing, inaccessible, or misconfigured, implementation continues using BD.

Durable knowledge is stored locally and retrieved through:

1. Exact identifier matching
2. Alias matching
3. Local BM25F ranking
4. Limited fuzzy matching
5. One-hop relationship expansion

No embedding model, external vector service, or token-consuming vectorization is required. The BM25 index is disposable and rebuildable from canonical durable knowledge.

---

# 2. Goals

## 2.1 Primary Goals

1. Provide deterministic, safe orchestration across multiple specialized agents.
2. Ensure subagents receive only the context required for their task.
3. Make Bagong a genuinely independent verifier with clean context.
4. Automatically detect Git capabilities from the active project.
5. Support normal implementation-to-PR flow when available and allowed.
6. Keep `review_pr` and `fix_pr_review` strictly user-triggered.
7. Use BD as the internal task and progress tracking system.
8. Treat Jira as optional and non-blocking.
9. Store durable project knowledge in a form that is inspectable, diffable, and portable.
10. Provide fast local search using BM25 without embeddings.
11. Make every agent decision traceable to requirements, knowledge, and evidence.
12. Allow all derived indexes to be deleted and rebuilt safely.

## 2.2 Secondary Goals

1. Support monorepos and multiple nested Git repositories.
2. Support local-only repositories without a remote.
3. Preserve unrelated user changes.
4. Produce reproducible context capsules with hashes.
5. Allow project-specific policies without hard-coding provider assumptions.
6. Support future integration with Sonar, Trivy, OSV, Jira, Confluence, GitHub, GitLab, and Bitbucket.
7. Minimize unnecessary LLM context and token usage.
8. Expose clear diagnostic information when retrieval or verification fails.

---

# 3. Non-Goals

The first implementation must not attempt to:

- Build a custom Git implementation.
- Build a custom Jira replacement beyond BD task tracking.
- Build a custom vector database.
- Generate embeddings.
- Implement semantic search using an LLM.
- Automatically fix PR review comments without user instruction.
- Automatically review any discovered PR.
- Automatically force-push branches.
- Modify default branches directly.
- Resolve review threads without explicit permission.
- Allow Bagong to implement fixes.
- Allow subagents to independently browse all durable knowledge.
- Treat scanner findings or reviewer comments as automatically correct.
- Depend on Jira availability to continue work.
- Depend on a remote Git provider to perform local implementation.
- Store agent chain-of-thought as durable knowledge.

---

# 4. Architecture Overview

```text
User
  |
  v
Semar
  |
  +--> Project capability detection
  |
  +--> Durable knowledge retrieval
  |
  +--> BD task graph
  |
  +--> Context capsule creation
  |
  +--> Gareng: independent risk and constraint analysis
  |
  +--> Petruk: independent planning and implementation
  |
  +--> Bagong: clean-context verification
  |
  +--> Git lifecycle
  |      +--> commit
  |      +--> push
  |      +--> create_pr
  |
  +--> Optional Jira synchronization
  |
  +--> Durable knowledge update
```

The core design is intentionally asymmetric:

- Semar is broad and stateful.
- Gareng, Petruk, and Bagong are narrow and isolated.
- Bagong is stricter than the others and cannot inherit implementation reasoning.
- BD is always available internally.
- Git and Jira are discovered capabilities, not required infrastructure.
- Search remains local and deterministic.

---

# 5. Agent Role Design

## 5.1 Semar

### Responsibilities

Semar is the only agent responsible for full task orchestration.

Semar must:

- Interpret the user request.
- Identify the active project and repository boundaries.
- Detect project capabilities.
- Search durable knowledge.
- Resolve existing decisions, constraints, and architecture.
- Create or update BD tasks.
- Decide which workflow phases apply.
- Build context capsules.
- Delegate independent analysis to Gareng and Petruk.
- Reconcile conflicting recommendations.
- Approve the final implementation plan.
- Delegate implementation to Petruk.
- Collect build, test, scan, and diff evidence.
- Build Bagong's clean review capsule.
- Accept, reject, or revise the implementation based on Bagong's verdict.
- Update durable knowledge.
- Decide whether commit, push, and PR creation are available and permitted.
- Synchronize selected updates to Jira when configured.
- Produce the final user-facing result.

### Restrictions

Semar must not:

- Fabricate missing evidence.
- Treat a subagent's claim as verified without supporting evidence.
- Allow a subagent to expand scope without recording the change.
- Automatically trigger `review_pr`.
- Automatically trigger `fix_pr_review`.
- Ignore explicit user instructions to skip Git.
- Expose unrelated durable knowledge to subagents.
- Store hidden reasoning in durable knowledge.

---

## 5.2 Gareng

### Responsibilities

Gareng acts as an independent constraint, risk, and challenge agent.

Gareng should:

- Identify missing or ambiguous requirements.
- Challenge unsupported assumptions.
- Detect architecture conflicts.
- Identify security risks.
- Identify dependency and migration risks.
- Detect backward compatibility concerns.
- Detect operational and observability gaps.
- Assess whether an apparently minor update is operationally safe.
- Detect unnecessary scope expansion.
- Evaluate whether acceptance criteria are testable.
- Return blocking and non-blocking findings.

### Context Rules

Gareng receives:

- Task objective.
- Explicit requirements.
- Acceptance criteria.
- Relevant project constraints.
- Curated durable knowledge.
- Relevant source references.
- Explicitly allowed tools.
- Explicitly forbidden actions.

Gareng does not receive:

- Full conversation history.
- Semar's private reasoning.
- Petruk's conclusions.
- Bagong's verdict.
- Unrelated repository knowledge.
- Full Jira or Confluence access.

### Output Contract

```ts
interface GarengAnalysis {
  summary: string;
  blockers: RiskFinding[];
  warnings: RiskFinding[];
  assumptionsToValidate: AssumptionFinding[];
  missingContextRequests: MissingContextRequest[];
  recommendedAcceptanceCriteria: AcceptanceCriterion[];
  confidence: number;
}
```

---

## 5.3 Petruk

### Responsibilities

Petruk acts as the solution planner and implementer.

Petruk should:

- Independently propose implementation approaches.
- Prefer minimal and maintainable changes.
- Respect explicit scope and constraints.
- Produce a detailed implementation plan.
- Modify only approved files.
- Use existing libraries and integrations when practical.
- Run targeted build and test commands.
- Record all modified files.
- Record unresolved problems.
- Return evidence references rather than unsupported claims.

### Context Rules

Petruk initially receives a clean planning capsule and does not receive Gareng's analysis.

After Semar synthesizes the plan, Petruk may receive an implementation capsule containing:

- Accepted requirements.
- Accepted risk mitigations.
- Approved scope.
- Approved files or modules.
- Relevant durable knowledge.
- Verification commands.
- Forbidden changes.

### Output Contract

```ts
interface PetrukImplementationResult {
  summary: string;
  changedFiles: ChangedFile[];
  commandsExecuted: CommandExecution[];
  buildResults: VerificationResult[];
  testResults: VerificationResult[];
  scanResults: VerificationResult[];
  unresolvedIssues: IssueReference[];
  proposedKnowledgeUpdates: KnowledgeMutation[];
  evidence: EvidenceReference[];
}
```

---

## 5.4 Bagong

### Responsibilities

Bagong is the final independent verifier.

Bagong must start with clean context and determine whether the implementation satisfies the supplied requirements based only on available evidence.

Bagong should:

- Compare requirements with the actual diff.
- Verify acceptance criteria.
- Check build and test results.
- Detect unsupported claims.
- Detect missing verification.
- Detect unrelated changes.
- Check whether security or quality findings remain.
- Check whether durable knowledge updates are supported.
- Return a strict verdict.

### Bagong Must Not Receive

- Full user conversation.
- Semar's reasoning.
- Gareng's reasoning.
- Petruk's narrative explanation.
- Prior agent conclusions.
- Hidden assumptions.
- Unverified claims.

### Bagong Verdict

```ts
type ReviewVerdict =
  | "passed"
  | "passed_with_warnings"
  | "failed"
  | "insufficient_evidence";
```

### Output Contract

```ts
interface BagongReview {
  verdict: ReviewVerdict;
  summary: string;
  satisfiedCriteria: AcceptanceCriterionResult[];
  failedCriteria: AcceptanceCriterionResult[];
  warnings: ReviewFinding[];
  unsupportedClaims: UnsupportedClaim[];
  missingEvidence: MissingEvidence[];
  unrelatedChanges: ChangedFile[];
  confidence: number;
}
```

---

# 6. Context Capsule Design

Every subagent invocation must use a context capsule.

## 6.1 Properties

A context capsule must be:

- Immutable after dispatch.
- Reproducible.
- Hashable.
- Limited to the task.
- Traceable to durable knowledge and evidence.
- Explicit about allowed tools.
- Explicit about forbidden actions.

## 6.2 Schema

```ts
interface ContextCapsule {
  id: string;
  digest: string;
  taskId: string;
  createdAt: string;

  role: "gareng" | "petruk" | "bagong";
  objective: string;

  requirements: RequirementReference[];
  acceptanceCriteria: AcceptanceCriterion[];
  constraints: Constraint[];

  relevantKnowledge: KnowledgeReference[];
  evidence: EvidenceReference[];

  assumptions: Assumption[];
  unresolvedQuestions: UnresolvedQuestion[];

  allowedTools: string[];
  forbiddenActions: string[];

  expectedOutput: OutputContract;
  tokenBudget?: number;
}
```

## 6.3 Digest

The digest should be deterministic:

```text
sha256(
  canonical_json(
    role,
    objective,
    requirements,
    acceptanceCriteria,
    constraints,
    knowledgeReferences,
    evidenceReferences,
    allowedTools,
    forbiddenActions
  )
)
```

The digest must be stored with:

- Agent invocation.
- Agent output.
- Task record.
- Final verification result.

## 6.4 Missing Context Requests

Subagents may request additional context, but may not search broadly themselves.

```ts
interface MissingContextRequest {
  query: string;
  reason: string;
  preferredTypes?: string[];
  blocking: boolean;
}
```

Semar decides whether to:

- Search for the context.
- Add it to a new capsule revision.
- Reject the request as irrelevant.
- Ask the user when necessary.

---

# 7. Project Capability Detection

Semar must begin every project workflow with capability detection.

## 7.1 Detection Scope

```text
Project root
  → Git repository
  → nested repositories
  → remote providers
  → authentication
  → branch state
  → issue providers
  → build systems
  → language ecosystems
  → package managers
  → test frameworks
  → Sonar configuration
  → Trivy availability
  → CI configuration
  → repository policies
```

## 7.2 Git Capability Detection

Punakawan must inspect:

- `.git` directory or Git worktree metadata.
- Repository root.
- Current branch.
- Detached HEAD state.
- Default branch.
- Uncommitted changes.
- Untracked files.
- Existing worktrees.
- Git remotes.
- Remote provider.
- Authentication availability.
- Push permissions.
- Pull-request read permissions.
- Pull-request creation permissions.
- Review-comment permissions.
- Whether the project is inside a larger repository.
- Whether nested repositories exist.

## 7.3 Git Capability Schema

```ts
interface GitCapabilities {
  detected: boolean;
  repositoryRoot?: string;
  isWorktree?: boolean;
  isBareRepository?: boolean;

  currentBranch?: string;
  detachedHead: boolean;
  defaultBranch?: string;

  hasUncommittedChanges: boolean;
  hasUntrackedFiles: boolean;

  remotes: GitRemote[];
  provider?: "github" | "gitlab" | "bitbucket" | "generic";

  capabilities: {
    inspectHistory: boolean;
    createBranch: boolean;
    createWorktree: boolean;
    commit: boolean;
    push: boolean;
    createPullRequest: boolean;
    readPullRequest: boolean;
    commentPullRequest: boolean;
  };

  limitations: string[];
}
```

## 7.4 User Override

Detected capability defines what Punakawan can do.

User instruction defines what Punakawan may do.

```text
Effective behavior =
  detected capabilities
  ∩ repository policy
  ∩ user permission
```

Supported user restrictions:

- Skip Git.
- Do not create a branch.
- Do not commit.
- Commit locally only.
- Do not push.
- Do not create a PR.
- Create a draft PR.
- Create a PR after verification.
- Do not modify the current worktree.
- Preserve existing changes.

## 7.5 Git Execution Policy

```ts
interface GitExecutionPolicy {
  source: "user" | "repository-policy" | "default";

  skipGit: boolean;
  allowBranchCreation: boolean;
  allowWorktreeCreation: boolean;
  allowCommit: boolean;
  allowPush: boolean;
  allowPullRequestCreation: boolean;

  reason?: string;
}
```

---

# 8. Git Workflow Design

## 8.1 `create_pr`

`create_pr` is part of the normal implementation lifecycle and is not reactive.

It may run when:

- Git is detected.
- A remote provider is detected.
- Push access is available.
- PR creation access is available.
- The user has not disabled Git or PR creation.
- The implementation passed required verification.
- Bagong returned `passed` or an accepted `passed_with_warnings`.
- The branch contains only intended changes.
- No prohibited major upgrade was introduced.
- The repository is not in an unsafe state.

### Workflow

```text
Implementation completed
  → collect diff
  → run build
  → run tests
  → run quality and security checks
  → Bagong review
  → update durable knowledge
  → create commit
  → push branch
  → create_pr
```

### Input

```ts
interface CreatePrInput {
  repository: string;
  baseBranch: string;
  headBranch: string;

  title?: string;
  body?: string;
  draft?: boolean;

  taskIds: string[];
  issueReferences?: IssueReference[];
  knowledgeReferences?: KnowledgeReference[];

  reviewers?: string[];
  labels?: string[];
}
```

### Required PR Body Sections

- Summary
- Requirements
- Changes
- Verification
- Security and quality checks
- Known risks
- Deferred work
- BD task references
- Jira references, when available
- Durable knowledge updates

### Failure Behavior

If PR creation is not available, Punakawan must continue with the implementation and report the actual reason:

- No Git repository.
- No remote.
- No push access.
- Unsupported provider.
- Authentication unavailable.
- User disabled PR creation.
- Verification failed.
- Unsafe working tree.
- Bagong rejected the implementation.

---

## 8.2 `review_pr`

`review_pr` is reactive.

It must only run after an explicit user instruction.

### Valid Triggers

- Review PR 42.
- Check this PR.
- Is the open PR safe to merge?
- Review the dependency upgrade PR.

### Invalid Triggers

- A PR was discovered.
- Implementation completed.
- CI failed.
- A reviewer commented.
- A scheduled process found an open PR.
- A PR is associated with the current branch.

### Input

```ts
interface ReviewPrInput {
  repository: string;
  pullRequestNumber: number;

  focus?: Array<
    | "correctness"
    | "security"
    | "performance"
    | "maintainability"
    | "tests"
    | "architecture"
    | "dependencies"
  >;

  includeExistingComments?: boolean;
  compareAgainstKnowledge?: boolean;
}
```

### Workflow

```text
Explicit user trigger
  → fetch PR metadata
  → fetch base and head commits
  → fetch diff
  → fetch CI status
  → fetch existing review comments if requested
  → retrieve related durable knowledge
  → build Gareng review capsule
  → build Petruk review capsule
  → collect independent findings
  → build Bagong verification capsule
  → verify findings against diff and evidence
  → Semar deduplicates and prioritizes
  → return final review
```

### Review Finding Schema

```ts
interface ReviewFinding {
  id: string;
  severity: "blocker" | "major" | "minor" | "suggestion";

  category: string;
  title: string;
  explanation: string;

  file?: string;
  startLine?: number;
  endLine?: number;

  evidence: EvidenceReference[];
  relatedKnowledge: KnowledgeReference[];

  suggestedFix?: string;
  confidence: number;
}
```

---

## 8.3 `fix_pr_review`

`fix_pr_review` is reactive.

It must only run after explicit user instruction.

### Valid Triggers

- Fix the review comments on PR 42.
- Address unresolved review threads.
- Fix comments 3 and 5.
- Apply all minor fixes, but skip the framework upgrade.

### Invalid Triggers

- A reviewer leaves a comment.
- A PR receives changes requested.
- CI fails.
- `review_pr` finishes.
- A scheduled poll finds unresolved threads.

### Workflow

```text
Explicit user trigger
  → fetch current PR head
  → fetch unresolved review comments
  → classify comment applicability
  → map selected comments to BD tasks
  → create isolated worktree
  → build Petruk implementation capsule
  → apply selected fixes
  → run targeted verification
  → run broader verification
  → run Bagong clean-context review
  → report results
  → push only if allowed
  → resolve threads only if allowed
```

### Comment Status

```ts
type ReviewCommentStatus =
  | "applicable"
  | "already_resolved"
  | "stale"
  | "conflicting"
  | "requires_clarification"
  | "major_change_required";
```

### Input

```ts
interface FixPrReviewInput {
  repository: string;
  pullRequestNumber: number;

  reviewFindingIds?: string[];
  reviewCommentIds?: string[];
  excludeFindingIds?: string[];

  allowMinorDependencyUpdates?: boolean;
  allowMajorDependencyUpdates?: boolean;

  pushChanges?: boolean;
  resolveThreads?: boolean;
}
```

### Safety Defaults

- Major dependency updates are not allowed.
- Push requires effective permission.
- Review threads are not resolved automatically.
- Force-push is prohibited.
- Stale comments are not applied.
- Conflicting comments are escalated.
- Unrelated changes are preserved.
- Each fix group must be independently verifiable.

---

# 9. Issue Tracking Architecture

## 9.1 Internal Source of Truth

BD is always the internal execution ledger.

BD stores:

- Task graph.
- Task dependencies.
- Task status.
- Agent assignments.
- Requirement links.
- Evidence links.
- Knowledge links.
- Verification status.
- Git references.
- Jira references.
- Retry history.
- Blockers.
- Decisions.

## 9.2 Jira as Optional Adapter

Jira is optional.

When enabled, Jira may provide:

- Source requirements.
- External issue references.
- User-facing progress.
- Comments.
- Status updates.
- Links to pull requests.
- Links to evidence.

Jira must never be required for:

- Planning.
- Task creation.
- Progress tracking.
- Implementation.
- Verification.
- Git work.
- Knowledge updates.

## 9.3 External Mapping

```ts
interface ExternalIssueMapping {
  internalTaskId: string;

  provider: "jira";
  externalIssueKey: string;
  externalUrl?: string;

  syncDirection:
    | "read_only"
    | "internal_to_external"
    | "bidirectional";

  lastSyncedAt?: string;
  externalRevision?: string;
}
```

## 9.4 Jira Failure Behavior

If Jira is unavailable:

- Continue with BD.
- Record the synchronization error.
- Preserve intended outbound updates.
- Allow retry.
- Do not invent issue keys.
- Do not block implementation.
- Inform the user only when the failure affects their expected workflow.

---

# 10. Durable Knowledge Architecture

## 10.1 Storage Layers

```text
Dolt  = canonical structured knowledge
JSONL = append-only events and evidence metadata
YAML  = portable human-readable exports
BM25  = derived local search index
```

## 10.2 Directory Layout

```text
.punakawan/
├── knowledge/
│   ├── architecture/
│   ├── requirements/
│   ├── decisions/
│   ├── constraints/
│   ├── source-code/
│   ├── dependencies/
│   ├── security/
│   ├── sonar/
│   ├── browser-flows/
│   ├── issues/
│   ├── pull-requests/
│   └── glossary/
├── evidence/
│   ├── builds/
│   ├── tests/
│   ├── scans/
│   ├── pull-requests/
│   ├── commands/
│   └── diffs/
├── events/
│   └── knowledge-events.jsonl
├── portable/
│   └── knowledge.yaml
└── index/
    ├── bm25/
    └── index-manifest.json
```

## 10.3 Canonical Knowledge Types

```ts
type KnowledgeType =
  | "requirement"
  | "architecture"
  | "decision"
  | "constraint"
  | "dependency"
  | "security-finding"
  | "sonar-rule"
  | "browser-flow"
  | "source-symbol"
  | "issue"
  | "pull-request"
  | "test-evidence"
  | "build-evidence"
  | "glossary";
```

## 10.4 Knowledge Item Schema

```ts
interface KnowledgeItem {
  id: string;
  type: KnowledgeType;

  title: string;
  summary: string;
  content: string;

  aliases: string[];
  tags: string[];
  identifiers: string[];

  scope: {
    organization?: string;
    project?: string;
    repository?: string;
    module?: string;
    path?: string;
    symbol?: string;
  };

  trustLevel:
    | "verified"
    | "accepted"
    | "derived"
    | "reported"
    | "unverified";

  sourceId: string;
  sourcePath?: string;
  sourceRevision?: string;

  contentHash: string;

  createdAt: string;
  updatedAt: string;
  validFrom?: string;
  validUntil?: string;

  supersededBy?: string;
}
```

## 10.5 Knowledge Relations

```ts
interface KnowledgeRelation {
  id: string;
  fromKnowledgeId: string;
  toKnowledgeId: string;

  relation:
    | "implements"
    | "depends_on"
    | "introduced_by"
    | "supersedes"
    | "contradicts"
    | "verifies"
    | "violates"
    | "related_to"
    | "derived_from"
    | "affects"
    | "fixed_by"
    | "tracked_by"
    | "reviewed_by"
    | "created_by";

  confidence: number;
  evidenceIds: string[];
}
```

## 10.6 Example Relation Graph

```text
CVE-2026-1234
  introduced_by → org.example:vulnerable-library
  managed_by → company-bom
  affects → payment-service
  fixed_by → company-bom 5.8.2
  verified_by → trivy-scan-20260723
  tracked_by → BD-SEC-14
  implemented_by → PR-42
```

---

# 11. Local Knowledge Search

## 11.1 Search Goals

The search system must:

- Run locally.
- Avoid embeddings.
- Avoid external model calls.
- Avoid token-consuming indexing.
- Support technical identifiers.
- Support natural keyword search.
- Support aliases.
- Support fuzzy typo recovery.
- Support scoped search.
- Support one-hop related knowledge.
- Explain why a result matched.
- Rebuild from canonical knowledge.

## 11.2 Search Pipeline

```text
Query
  → normalize
  → detect structured identifiers
  → exact identifier search
  → alias search
  → BM25F search
  → optional fuzzy fallback
  → scope boost
  → trust boost
  → one-hop relation expansion
  → deduplicate
  → rerank
  → return explainable matches
```

## 11.3 Exact Identifier Recognition

Recognized patterns should include:

- CVE identifiers.
- GHSA identifiers.
- Sonar rule identifiers.
- Jira keys.
- BD task identifiers.
- Git commit hashes.
- Pull-request numbers.
- Maven coordinates.
- npm packages.
- Go modules.
- Rust crates.
- File paths.
- API routes.
- Java classes and methods.
- TypeScript symbols.
- Version strings.

Examples:

```text
CVE-2026-12345
GHSA-abcd-1234-efgh
java:S3776
SETARA-142
BD-SEC-18
org.thymeleaf.extras:thymeleaf-extras-java8time
src/main/java/com/example/PermissionService.java
PermissionService.validateAccess
#42
```

Exact identifier matches must outrank every BM25 result.

---

## 11.4 BM25F Document Model

```ts
interface IndexedKnowledgeDocument {
  id: string;
  type: string;

  title: string;
  summary: string;
  content: string;

  aliases: string[];
  tags: string[];

  project?: string;
  repository?: string;
  module?: string;

  paths: string[];
  symbols: string[];
  identifiers: string[];

  trustLevel: string;
  updatedAt: string;
}
```

## 11.5 Field Weights

```yaml
knowledge:
  search:
    fields:
      identifiers: 12.0
      aliases: 10.0
      symbols: 8.0
      title: 7.0
      paths: 6.0
      tags: 4.0
      summary: 3.0
      content: 1.0
```

## 11.6 Technical Tokenization

The tokenizer must split and preserve:

- Camel case.
- Pascal case.
- Snake case.
- Kebab case.
- Dot-separated package names.
- Colon-separated dependency coordinates.
- Slash-separated paths.
- API routes.
- Version strings.
- CVE and GHSA IDs.
- Sonar rule IDs.
- Git hashes.
- Acronyms.

Example:

```text
BrsProductRecommendationMapper
```

Indexed terms:

```text
BrsProductRecommendationMapper
brsproductrecommendationmapper
Brs
Product
Recommendation
Mapper
brs
product
recommendation
mapper
```

Example:

```text
org.thymeleaf.extras:thymeleaf-extras-java8time
```

Indexed terms:

```text
org.thymeleaf.extras:thymeleaf-extras-java8time
org.thymeleaf.extras
thymeleaf-extras-java8time
thymeleaf
extras
java8time
```

The tokenizer must not aggressively stem technical identifiers.

---

## 11.7 Alias Matching

Aliases are durable knowledge.

Examples:

```text
ASA → Adaptive Setara Assistance
Setara Core → setara-core
quality gate → Sonar Quality Gate
PR → pull request
```

Alias matches receive a strong ranking bonus.

---

## 11.8 Fuzzy Matching

Fuzzy matching is a fallback only.

Run fuzzy matching when:

- Exact matching returns no result.
- BM25 score is below a threshold.
- The token length is sufficient.
- The edit distance is small.
- The candidate is a known identifier, alias, symbol, or package.

Do not use broad fuzzy matching for short tokens.

---

## 11.9 Relation Expansion

Default relation expansion:

- Maximum depth: 1.
- Maximum related items: 10.
- Minimum relation confidence: 0.70.
- Only directly linked, high-confidence items.
- Two-hop expansion requires explicit request.

```yaml
knowledge:
  search:
    related:
      enabled: true
      maxDepth: 1
      maxItems: 10
      minimumConfidence: 0.70
```

---

## 11.10 Ranking

```text
final score =
    BM25F score
  + exact identifier bonus
  + alias bonus
  + scope bonus
  + trust bonus
  + relation bonus
```

Suggested bonuses:

```ts
if (exactIdentifierMatch) score += 100;
if (exactAliasMatch) score += 40;

if (samePathScope) score += 20;
else if (sameModuleScope) score += 12;
else if (sameRepositoryScope) score += 8;
else if (sameProjectScope) score += 5;

if (directlyRelated) score += 8;
if (trustLevel === "verified") score += 5;
if (trustLevel === "accepted") score += 3;
```

Recency should be a limited tie-breaker rather than a dominant factor.

---

## 11.11 Index Technology

Recommended initial implementation:

- Embedded Bleve index.
- Local file-backed index.
- No external service.
- No vector database.
- No embedding model.
- No LLM calls during indexing.
- Incremental updates supported.
- Full rebuild supported.

Canonical knowledge remains in Dolt and JSONL. The Bleve index is disposable.

---

## 11.12 Search API

```ts
interface KnowledgeSearchRequest {
  query: string;

  scope?: {
    project?: string;
    repository?: string;
    module?: string;
    path?: string;
  };

  types?: string[];
  tags?: string[];

  includeRelated?: boolean;
  limit?: number;
}
```

```ts
interface KnowledgeSearchResult {
  id: string;
  title: string;
  summary: string;
  type: string;

  score: number;

  match: {
    kind:
      | "identifier"
      | "alias"
      | "bm25"
      | "fuzzy"
      | "related";

    fields: string[];
    terms: string[];
  };

  relations?: KnowledgeRelationReference[];
  source: KnowledgeSourceReference;
}
```

## 11.13 Search Explanation

Punakawan should expose:

```text
Matched because:
- Exact alias: "S3776"
- Same repository: setara-core
- Type: sonar-rule
- Verified by policy snapshot
- Directly related to PaymentService
```

This is important for debugging retrieval and context selection.

---

# 12. Knowledge Tooling

Semar receives broad access to:

```text
knowledge.search
knowledge.get
knowledge.related
knowledge.trace
knowledge.explain_match
knowledge.store
knowledge.update
knowledge.supersede
knowledge.link
knowledge.refresh_index
knowledge.verify_freshness
```

Subagents do not receive broad search access.

## 12.1 `knowledge.search`

Search exact identifiers, aliases, BM25 terms, and optionally related items.

## 12.2 `knowledge.get`

Retrieve one canonical knowledge item and its current revision.

## 12.3 `knowledge.related`

Retrieve bounded one-hop relationships.

## 12.4 `knowledge.trace`

Show:

- Original source.
- Revisions.
- Derived items.
- Evidence.
- Related tasks.
- Related PRs.

## 12.5 `knowledge.explain_match`

Explain ranking and matching.

## 12.6 `knowledge.store`

Create new canonical knowledge with source, trust, and scope.

## 12.7 `knowledge.update`

Update existing knowledge with revision history.

## 12.8 `knowledge.supersede`

Mark prior knowledge as superseded without deleting historical records.

## 12.9 `knowledge.link`

Create explicit typed relations.

## 12.10 `knowledge.refresh_index`

Incrementally update or rebuild BM25 index.

## 12.11 `knowledge.verify_freshness`

Compare source revision, content hash, and validity period.

---

# 13. Workflow State Machine

## 13.1 Standard Implementation Workflow

```text
INITIALIZED
  ↓
PROJECT_DETECTION
  ↓
CAPABILITY_DETECTION
  ↓
KNOWLEDGE_RETRIEVAL
  ↓
REQUIREMENT_NORMALIZATION
  ↓
INDEPENDENT_ANALYSIS
  ├── GARENG_ANALYSIS
  └── PETRUK_PLANNING
  ↓
PLAN_SYNTHESIS
  ↓
BD_TASK_GRAPH_CREATED
  ↓
IMPLEMENTATION
  ↓
VERIFICATION
  ↓
BAGONG_REVIEW
  ↓
KNOWLEDGE_UPDATE
  ↓
GIT_PHASE
  ├── skipped
  ├── local commit
  ├── pushed branch
  └── create_pr
  ↓
OPTIONAL_JIRA_SYNC
  ↓
COMPLETED
```

## 13.2 Reactive PR Review Workflow

```text
USER_TRIGGER_REVIEW_PR
  ↓
PR_CONTEXT_FETCH
  ↓
KNOWLEDGE_RETRIEVAL
  ↓
GARENG_REVIEW
  ↓
PETRUK_REVIEW
  ↓
BAGONG_FINDING_VERIFICATION
  ↓
SEMAR_SYNTHESIS
  ↓
REVIEW_RESULT
```

## 13.3 Reactive PR Fix Workflow

```text
USER_TRIGGER_FIX_PR_REVIEW
  ↓
REVIEW_COMMENT_FETCH
  ↓
COMMENT_CLASSIFICATION
  ↓
BD_TASK_CREATION
  ↓
ISOLATED_WORKTREE
  ↓
PETRUK_IMPLEMENTATION
  ↓
VERIFICATION
  ↓
BAGONG_REVIEW
  ↓
OPTIONAL_PUSH
  ↓
OPTIONAL_THREAD_RESOLUTION
  ↓
COMPLETED
```

---

# 14. Repository and Package Structure

```text
punakawan/
├── cmd/
│   └── punakawan/
├── internal/
│   ├── agents/
│   │   ├── semar/
│   │   ├── gareng/
│   │   ├── petruk/
│   │   └── bagong/
│   ├── orchestration/
│   │   ├── workflow/
│   │   ├── capsule/
│   │   ├── policy/
│   │   └── evidence/
│   ├── capability/
│   │   ├── project/
│   │   ├── git/
│   │   ├── build/
│   │   ├── issue/
│   │   └── provider/
│   ├── git/
│   │   ├── local/
│   │   ├── provider/
│   │   │   ├── github/
│   │   │   ├── gitlab/
│   │   │   └── bitbucket/
│   │   ├── review/
│   │   └── pullrequest/
│   ├── issues/
│   │   ├── bd/
│   │   ├── jira/
│   │   └── sync/
│   ├── knowledge/
│   │   ├── model/
│   │   ├── store/
│   │   │   ├── dolt/
│   │   │   ├── jsonl/
│   │   │   └── yaml/
│   │   ├── search/
│   │   │   ├── exact/
│   │   │   ├── alias/
│   │   │   ├── bm25/
│   │   │   ├── fuzzy/
│   │   │   └── relation/
│   │   ├── index/
│   │   └── migration/
│   ├── tools/
│   ├── config/
│   └── telemetry/
├── adapters/
│   ├── github/
│   ├── gitlab/
│   ├── bitbucket/
│   ├── jira/
│   ├── sonar/
│   └── trivy/
├── schemas/
│   ├── context-capsule.schema.json
│   ├── knowledge-item.schema.json
│   ├── knowledge-relation.schema.json
│   ├── git-capabilities.schema.json
│   └── review-result.schema.json
├── migrations/
├── testdata/
└── docs/
    ├── architecture/
    ├── adr/
    └── workflows/
```

---

# 15. Configuration

Configuration should define policies, not Git modes.

```yaml
punakawan:
  agents:
    semar:
      broadKnowledgeAccess: true

    gareng:
      executionMode: isolated-subagent
      inheritConversation: false
      broadKnowledgeAccess: false
      contextRequestsThroughSemar: true

    petruk:
      executionMode: isolated-subagent
      inheritConversation: false
      broadKnowledgeAccess: false
      contextRequestsThroughSemar: true

    bagong:
      executionMode: clean-subagent
      inheritConversation: false
      inheritAgentReasoning: false
      broadKnowledgeAccess: false
      evidenceOnly: true

  issues:
    internal:
      provider: bd
      enabled: true

    external:
      provider: jira
      enabled: false
      writesRequireApproval: true
      syncDirection: internal_to_external

  git:
    autoDetect: true

    implementation:
      preferIsolatedWorktree: true
      preserveUnrelatedChanges: true
      createTaskBranch: true

    commit:
      enabled: true
      groupByTask: true

    pullRequest:
      createAfterSuccessfulImplementation: true
      draftByDefault: true

    reactiveOperations:
      reviewPrRequiresUserTrigger: true
      fixPrReviewRequiresUserTrigger: true

    safety:
      allowForcePush: false
      allowDestructiveReset: false
      allowDefaultBranchModification: false

  knowledge:
    canonicalStore: dolt
    eventStore: jsonl
    portableFormat: yaml

    search:
      engine: bleve
      exactMatching: true
      aliases: true
      bm25: true
      fuzzyFallback: true
      embeddings: false

      related:
        enabled: true
        maxDepth: 1
        maxItems: 10
        minimumConfidence: 0.70

      fields:
        identifiers: 12.0
        aliases: 10.0
        symbols: 8.0
        title: 7.0
        paths: 6.0
        tags: 4.0
        summary: 3.0
        content: 1.0
```

---

# 16. Implementation Phases

## Phase 0: Baseline and Migration Assessment

### Tasks

- Inspect current Punakawan repository structure.
- Identify existing agent implementations.
- Identify existing Git tooling.
- Identify current BD usage.
- Identify current Jira coupling.
- Identify current knowledge storage.
- Identify existing Dolt or JSONL usage.
- Identify current search implementation.
- Identify existing tool schemas.
- Document backward compatibility constraints.
- Create migration ADRs.

### Deliverables

- Current-state architecture map.
- Migration risk register.
- Component dependency graph.
- Initial ADR set.
- Backward compatibility plan.

### Acceptance Criteria

- Existing components are mapped.
- Existing public APIs are listed.
- Breaking changes are identified.
- Migration path is documented.
- No implementation begins without known data migration requirements.

---

## Phase 1: Core Domain Models

### Tasks

Implement canonical schemas for:

- Agent role.
- Context capsule.
- Knowledge item.
- Knowledge relation.
- Evidence reference.
- Requirement.
- Acceptance criterion.
- Git capability.
- Git execution policy.
- BD task.
- External issue mapping.
- PR review.
- PR fix result.
- Bagong verdict.

### Deliverables

- Go domain models.
- JSON schemas.
- Serialization tests.
- Canonical JSON utility.
- Digest implementation.

### Acceptance Criteria

- Context capsule digest is deterministic.
- Equivalent input produces identical digest.
- Schema validation rejects invalid roles and verdicts.
- Knowledge relations require valid source and target IDs.
- Evidence references cannot be anonymous.

---

## Phase 2: Agent Isolation Runtime

### Tasks

- Implement subagent execution boundaries.
- Disable inherited conversation for Gareng, Petruk, and Bagong.
- Add allowed-tool enforcement.
- Add forbidden-action enforcement.
- Add context capsule injection.
- Add missing-context request handling.
- Add invocation and result persistence.
- Add token budget enforcement.
- Add agent timeout and cancellation.
- Add retry policy.

### Deliverables

- Agent executor.
- Capsule validator.
- Tool policy middleware.
- Invocation event log.
- Missing-context request workflow.

### Acceptance Criteria

- Gareng cannot access broad knowledge directly.
- Petruk cannot access unapproved files through orchestration tools.
- Bagong cannot access prior agent outputs.
- Every invocation records capsule digest.
- Rejected tool calls are logged.
- Missing-context requests return to Semar.

---

## Phase 3: Semar Orchestrator

### Tasks

- Implement task intake.
- Implement project detection.
- Implement capability detection pipeline.
- Implement requirement normalization.
- Implement knowledge retrieval orchestration.
- Implement independent Gareng and Petruk dispatch.
- Implement plan synthesis.
- Implement BD task graph creation.
- Implement verification collection.
- Implement Bagong dispatch.
- Implement final decision handling.
- Implement knowledge update proposal.
- Implement Git phase decision.
- Implement Jira synchronization decision.

### Acceptance Criteria

- Semar can run a complete task without Jira.
- Semar can run a complete task without Git.
- Semar can build separate Gareng and Petruk capsules.
- Semar can reject unsupported subagent claims.
- Semar does not trigger reactive PR tools automatically.

---

## Phase 4: BD Internal Task Graph

### Tasks

- Implement BD task persistence.
- Implement task dependencies.
- Implement task states.
- Implement task-to-requirement links.
- Implement task-to-evidence links.
- Implement task-to-knowledge links.
- Implement task-to-Git references.
- Implement task-to-Jira mappings.
- Implement task status transitions.
- Implement retry and blocker tracking.

### Suggested Task States

```text
pending
ready
in_progress
blocked
verification_failed
review_failed
completed
cancelled
```

### Acceptance Criteria

- Every implementation task has a BD ID.
- Tasks remain usable without Jira.
- Evidence can be attached to a task.
- A PR can reference multiple BD tasks.
- Task state transitions are validated.
- Jira sync failures do not change BD truth.

---

## Phase 5: Jira Adapter Decoupling

### Tasks

- Extract Jira-specific behavior from orchestration.
- Implement Jira capability detection.
- Implement read-only import.
- Implement optional outbound sync.
- Implement sync queue.
- Implement conflict detection.
- Implement retry.
- Implement approval gating for writes.
- Implement mapping persistence.

### Acceptance Criteria

- Disabling Jira does not affect task execution.
- Jira read failure does not fail the workflow.
- Outbound sync can be retried.
- External issue keys are never fabricated.
- BD remains canonical.

---

## Phase 6: Git Capability Detection

### Tasks

- Detect Git repository.
- Detect repository root.
- Detect nested repositories.
- Detect current branch.
- Detect detached HEAD.
- Detect default branch.
- Detect remotes.
- Detect provider.
- Detect local working tree state.
- Detect existing worktrees.
- Detect authentication.
- Detect push permission.
- Detect PR read and write capabilities.
- Detect repository policy files.
- Implement user override parsing.

### Acceptance Criteria

- Local repository without remote is detected correctly.
- GitHub, GitLab, and Bitbucket remotes are recognized.
- Detached HEAD is reported.
- Uncommitted changes are reported.
- Explicit "skip Git" overrides all capabilities.
- Missing authentication does not block local changes.

---

## Phase 7: Safe Git Workspace

### Tasks

- Implement isolated worktree creation.
- Implement safe task branch naming.
- Preserve unrelated changes.
- Prevent default-branch modification.
- Prevent destructive reset.
- Prevent force-push.
- Capture before-and-after Git state.
- Implement cleanup.
- Implement rollback.
- Handle nested repositories.

### Acceptance Criteria

- Existing user changes are preserved.
- Default branch is never directly modified.
- Failed implementation can be rolled back.
- Generated branch contains only intended changes.
- Worktree cleanup is safe after failures.

---

## Phase 8: `create_pr`

### Tasks

- Implement provider-neutral PR request model.
- Implement GitHub adapter first.
- Add title generation from verified task data.
- Add body generation from requirements, changes, verification, risks, and knowledge.
- Add draft policy.
- Add reviewer and label support.
- Add failure reporting.
- Add PR-to-BD linking.
- Add optional Jira link update.

### Acceptance Criteria

- `create_pr` can run after verified implementation.
- PR body is evidence-backed.
- User Git restrictions are respected.
- No PR is created when verification fails.
- No PR is created when Bagong fails.
- PR link is attached to BD tasks.
- Missing provider support produces a local result, not workflow failure.

---

## Phase 9: `review_pr`

### Tasks

- Implement explicit trigger validation.
- Fetch PR metadata.
- Fetch base and head commits.
- Fetch diff.
- Fetch CI status.
- Fetch existing comments optionally.
- Retrieve related durable knowledge.
- Create independent Gareng and Petruk review capsules.
- Create Bagong finding-verification capsule.
- Deduplicate findings.
- Rank findings.
- Produce structured verdict.

### Acceptance Criteria

- Tool cannot run without explicit user trigger.
- Findings reference file and line where possible.
- Findings include evidence.
- Duplicate findings are merged.
- Unsupported findings are removed or marked low confidence.
- Review includes tested and untested areas.
- Review result records reviewed commit SHA.

---

## Phase 10: `fix_pr_review`

### Tasks

- Implement explicit trigger validation.
- Fetch unresolved comments.
- Detect stale comments.
- Detect already-resolved comments.
- Detect conflicts.
- Detect major-change requirements.
- Create BD tasks for selected fixes.
- Use isolated worktree.
- Apply fixes through Petruk.
- Verify each fix group.
- Run Bagong.
- Push only when permitted.
- Resolve threads only when permitted.

### Acceptance Criteria

- Tool cannot run automatically.
- Stale comments are not implemented.
- Major changes are not applied unless explicitly allowed.
- Failed fixes are rolled back.
- Successful fixes are linked to comments and BD tasks.
- Threads remain unresolved unless explicitly permitted.
- Force-push is never used.

---

## Phase 11: Durable Knowledge Store

### Tasks

- Define Dolt schema.
- Implement knowledge CRUD.
- Implement revision history.
- Implement source tracking.
- Implement content hashing.
- Implement supersession.
- Implement relations.
- Implement JSONL event output.
- Implement YAML export.
- Implement source freshness checks.

### Acceptance Criteria

- Knowledge items are versioned.
- Updates preserve prior revisions.
- Superseded items remain traceable.
- Every item has source and trust level.
- JSONL events are append-only.
- YAML export can reconstruct essential knowledge.
- Search index can be deleted without data loss.

---

## Phase 12: Technical Tokenizer

### Tasks

- Implement camel-case splitting.
- Implement snake-case splitting.
- Implement kebab-case splitting.
- Implement path splitting.
- Preserve original token.
- Parse Maven coordinates.
- Parse npm package names.
- Parse CVE and GHSA IDs.
- Parse Sonar IDs.
- Parse Jira and BD IDs.
- Parse symbols.
- Parse versions.
- Add language-aware stopword rules.
- Avoid aggressive stemming.

### Acceptance Criteria

- Technical identifiers remain searchable exactly.
- Class names can be found using split words.
- Maven coordinates can be found by group, artifact, or full coordinate.
- Path search works with partial segments.
- Short technical acronyms are preserved.
- Tokenization is deterministic.

---

## Phase 13: BM25 Index

### Tasks

- Integrate Bleve.
- Define field mappings.
- Define BM25 field weights.
- Implement incremental indexing.
- Implement deletion.
- Implement full rebuild.
- Implement index manifest.
- Implement corruption recovery.
- Implement index versioning.
- Implement scoped filters.

### Acceptance Criteria

- Index requires no model call.
- Index build consumes no LLM tokens.
- Index can be rebuilt from canonical knowledge.
- Exact identifiers outrank content matches.
- Scope filters work.
- Index corruption can be recovered by rebuild.
- Incremental update changes only affected documents.

---

## Phase 14: Search Pipeline

### Tasks

- Implement query normalization.
- Implement exact identifier detection.
- Implement alias resolution.
- Implement BM25F search.
- Implement low-confidence fuzzy fallback.
- Implement scope boost.
- Implement trust boost.
- Implement one-hop relation expansion.
- Implement deduplication.
- Implement explain-match output.
- Implement result limits.

### Acceptance Criteria

- Searching `java:S3776` returns the Sonar rule first.
- Searching a typo can return a correct package when confidence is sufficient.
- Searching a class word returns related class knowledge.
- Related items are bounded.
- Search results explain why they matched.
- No embedding model is required.
- No external service is required.

---

## Phase 15: Semar Knowledge Retrieval

### Tasks

- Extract identifiers from the user task.
- Generate deterministic keyword queries.
- Search exact identifiers first.
- Run BM25 search.
- Expand direct relations.
- Rerank by task scope.
- Select context within token budget.
- Build knowledge references.
- Record why each item was selected.
- Exclude irrelevant knowledge.

### Acceptance Criteria

- Context capsules remain bounded.
- Semar can explain why knowledge was included.
- Duplicate knowledge is removed.
- Superseded knowledge is excluded by default.
- Current repository and module are prioritized.
- Subagents do not receive broad search tools.

---

## Phase 16: Bagong Evidence Review

### Tasks

- Define evidence-only review capsule.
- Include requirements.
- Include acceptance criteria.
- Include actual diffs.
- Include build and test outputs.
- Include scan outputs.
- Include relevant knowledge references.
- Exclude implementation narrative.
- Implement strict verdict rules.
- Implement insufficient-evidence handling.

### Acceptance Criteria

- Bagong can fail unsupported claims.
- Bagong can return insufficient evidence.
- Bagong cannot modify files.
- Bagong cannot retrieve broad knowledge.
- Bagong verdict is tied to capsule digest.
- Bagong flags unrelated changes.

---

## Phase 17: Observability and Audit

### Tasks

- Add structured workflow events.
- Add agent invocation events.
- Add capsule digests.
- Add search diagnostics.
- Add Git capability logs.
- Add Jira sync logs.
- Add BD state transition logs.
- Add PR workflow logs.
- Add verification summaries.
- Redact secrets.
- Add local debug mode.

### Acceptance Criteria

- Every task can be reconstructed.
- Search decisions can be inspected.
- Agent inputs and outputs are linked by digest.
- Secret values are not logged.
- Git and Jira failures are distinguishable.
- Reactive tools show explicit trigger source.

---

# 17. Testing Strategy

## 17.1 Unit Tests

Cover:

- Context digest determinism.
- Capability detection parsing.
- Remote provider detection.
- User Git override logic.
- Task state transitions.
- Jira mapping.
- Knowledge hashing.
- Relation validation.
- Tokenizer behavior.
- Exact identifier detection.
- BM25 ranking.
- Scope boosting.
- Trust boosting.
- Fuzzy fallback.
- Relation expansion limits.
- Review comment classification.

## 17.2 Integration Tests

Cover:

- Local Git repository without remote.
- GitHub remote with read-only access.
- GitHub remote with write access.
- Detached HEAD.
- Dirty working tree.
- Nested repository.
- Jira unavailable.
- Jira read-only.
- Jira write failure.
- BM25 index rebuild.
- Corrupted index recovery.
- Complete implementation without Git.
- Complete implementation with local commit.
- Complete implementation with PR.
- Reactive PR review.
- Reactive PR review fix.

## 17.3 Agent Isolation Tests

Verify:

- Gareng cannot read full conversation.
- Petruk cannot read Gareng output before synthesis.
- Bagong cannot read prior agent outputs.
- Forbidden tools are blocked.
- Missing context is routed to Semar.
- Capsule digest changes when context changes.
- Identical capsule produces identical digest.

## 17.4 Golden Retrieval Tests

Create a fixed knowledge corpus and assert:

- Exact CVE lookup.
- Sonar rule lookup.
- Maven coordinate lookup.
- Partial class-name lookup.
- File-path lookup.
- Alias lookup.
- Typo fallback.
- Scope-aware ranking.
- Superseded knowledge exclusion.
- One-hop relation expansion.

## 17.5 End-to-End Scenarios

### Scenario A: No Git, No Jira

- User asks for implementation.
- Semar detects no Git.
- BD tracks progress.
- Petruk modifies files.
- Bagong verifies.
- Knowledge updates.
- No commit or PR.
- Workflow succeeds.

### Scenario B: Local Git, No Remote

- Git detected.
- Local branch and worktree created.
- Implementation verified.
- Local commit created.
- PR skipped because no remote.
- Workflow succeeds.

### Scenario C: GitHub with PR Support

- Git and remote detected.
- Worktree and branch created.
- Implementation verified.
- Bagong passes.
- Commit and push succeed.
- `create_pr` runs.
- BD receives PR link.

### Scenario D: Explicit Skip Git

- Git detected.
- User says skip Git.
- No branch, commit, push, or PR.
- Local file modifications proceed.
- Workflow succeeds.

### Scenario E: PR Review

- Open PR exists.
- No action occurs automatically.
- User asks to review.
- `review_pr` runs.
- Findings are evidence-backed.

### Scenario F: Fix Review Comments

- Review comments exist.
- No automatic fix occurs.
- User explicitly requests selected fixes.
- Stale comments are excluded.
- Minor fixes are applied.
- Major upgrade is reported, not applied.

### Scenario G: Jira Failure

- Jira configured.
- Sync fails.
- BD remains updated.
- Implementation and PR continue.
- Jira retry is recorded.

### Scenario H: Knowledge Search

- Query includes CVE and class name.
- Exact CVE match ranks first.
- Related dependency and affected module are added.
- Class knowledge is retrieved by BM25.
- No embedding call occurs.

---

# 18. Security and Safety Requirements

- Never log tokens or credentials.
- Never expose Git provider tokens to subagents.
- Never allow force-push by default.
- Never modify the default branch directly.
- Never resolve PR threads automatically.
- Never execute review fixes without explicit user instruction.
- Never treat Jira as canonical.
- Never allow Bagong to modify code.
- Never allow broad knowledge search from isolated agents.
- Never persist hidden reasoning.
- Always preserve unrelated user changes.
- Always verify branch ownership before push.
- Always record reviewed commit SHA.
- Always verify that comments apply to current code.
- Always sanitize command arguments.
- Always restrict shell execution to approved working directories.

---

# 19. Performance Requirements

## 19.1 Search

Target:

- Exact identifier lookup: under 20 ms for typical project corpus.
- BM25 query: under 100 ms for typical project corpus.
- One-hop relation expansion: under 50 ms.
- Index rebuild: proportional to corpus size and fully local.
- No model call during indexing.
- No model call during retrieval.
- No token cost for vectorization.

## 19.2 Agent Context

- Context capsule should prioritize summaries and references.
- Large documents should be chunked.
- Duplicate knowledge should be removed.
- Irrelevant history should not be included.
- Bagong capsule should remain evidence-focused.
- Broad source documents should be loaded only when explicitly needed.

---

# 20. Migration Strategy

## 20.1 Existing Git Configuration

If existing versions contain explicit Git modes:

- Continue parsing them temporarily.
- Translate them into user or project policy.
- Emit a deprecation warning.
- Remove mode-based logic from orchestration.
- Delete the old configuration in a later major release.

## 20.2 Existing Jira-First Tasks

- Import current Jira-linked tasks into BD.
- Preserve Jira keys as external mappings.
- Make BD task IDs canonical internally.
- Continue external synchronization.

## 20.3 Existing Knowledge

- Import existing JSONL and YAML knowledge.
- Generate content hashes.
- Assign trust levels.
- Create source references.
- Build initial BM25 index.
- Mark ambiguous items as unverified.
- Preserve original content.

## 20.4 Existing Agent Context

- Replace shared conversation context with context capsules.
- Add compatibility wrapper for older agent invocations.
- Record missing source references.
- Disable direct broad retrieval for subagents.

---

# 21. Architecture Decision Records

Create these ADRs:

1. **ADR-001: Semar owns broad context and orchestration**
2. **ADR-002: Gareng and Petruk operate independently**
3. **ADR-003: Bagong starts with clean evidence-only context**
4. **ADR-004: Subagents receive immutable context capsules**
5. **ADR-005: Git capabilities are detected automatically**
6. **ADR-006: User Git restrictions override detected capabilities**
7. **ADR-007: `create_pr` belongs to normal implementation flow**
8. **ADR-008: `review_pr` is explicitly user-triggered**
9. **ADR-009: `fix_pr_review` is explicitly user-triggered**
10. **ADR-010: BD is the internal task source of truth**
11. **ADR-011: Jira is an optional external adapter**
12. **ADR-012: Dolt and JSONL are canonical knowledge stores**
13. **ADR-013: BM25 is the default knowledge retrieval engine**
14. **ADR-014: Embeddings are not required**
15. **ADR-015: Search indexes are disposable and rebuildable**
16. **ADR-016: Exact technical identifiers outrank text similarity**
17. **ADR-017: Relation expansion is bounded**
18. **ADR-018: Unsupported claims fail verification**
19. **ADR-019: Agent reasoning is not stored as project knowledge**
20. **ADR-020: Git and Jira failures must not erase completed local work**

---

# 22. Milestone Plan

## Milestone 1: Core Models and Isolation

Includes:

- Domain models.
- Context capsules.
- Agent role enforcement.
- Bagong clean context.
- Tool permissions.
- Invocation audit.

Exit criteria:

- Isolated agents operate using capsule-only context.
- Bagong cannot access prior reasoning.

## Milestone 2: BD and Optional Jira

Includes:

- BD task graph.
- Jira adapter separation.
- External mappings.
- Sync queue.

Exit criteria:

- Full workflow succeeds without Jira.
- Jira failures are non-blocking.

## Milestone 3: Git Capability Detection

Includes:

- Repository detection.
- Remote and provider detection.
- Authentication and permission detection.
- User override.
- Safe worktrees.

Exit criteria:

- Git behavior is derived automatically.
- Explicit skip Git is respected.

## Milestone 4: Git Workflows

Includes:

- `create_pr`.
- `review_pr`.
- `fix_pr_review`.
- Review comment classification.
- Provider-neutral interfaces.

Exit criteria:

- PR creation works after verified implementation.
- Review and fix workflows require explicit user trigger.

## Milestone 5: Durable Knowledge Core

Includes:

- Dolt schema.
- JSONL events.
- YAML export.
- Relations.
- Revision history.

Exit criteria:

- Knowledge is durable, versioned, and source-backed.

## Milestone 6: Local BM25 Search

Includes:

- Technical tokenizer.
- Exact matching.
- Alias matching.
- BM25F.
- Fuzzy fallback.
- Relation expansion.
- Explain-match.

Exit criteria:

- Knowledge retrieval works without embeddings or model calls.

## Milestone 7: Full Orchestration

Includes:

- Semar end-to-end workflow.
- Independent Gareng and Petruk analysis.
- Bagong verification.
- Git and Jira decision handling.
- Knowledge updates.

Exit criteria:

- Complete implementation workflow runs across all supported capability combinations.

## Milestone 8: Hardening

Includes:

- Security review.
- Performance optimization.
- Migration tools.
- Failure recovery.
- Extensive end-to-end tests.
- Documentation.

Exit criteria:

- Production-ready release candidate.

---

# 23. Definition of Done

The enhancement is complete when:

- Semar is the only agent with broad context and search access.
- Gareng and Petruk receive independent, limited context.
- Bagong always starts clean.
- Every subagent invocation uses a hashed context capsule.
- Git is automatically detected.
- User Git restrictions override automatic detection.
- `create_pr` works as part of normal implementation.
- `review_pr` only runs after explicit user request.
- `fix_pr_review` only runs after explicit user request.
- BD works as the internal task graph.
- Jira is optional and non-blocking.
- Durable knowledge is versioned and source-backed.
- Search uses exact matching, aliases, local BM25F, limited fuzzy fallback, and bounded relations.
- No embedding model is required.
- No token cost is incurred for knowledge vectorization.
- Search results explain why they matched.
- Search index can be deleted and rebuilt.
- Bagong can reject unsupported claims.
- Failed verification prevents PR creation.
- Unrelated user changes are preserved.
- Force-push and default-branch modification remain prohibited.
- End-to-end tests cover no-Git, local-Git, remote-Git, no-Jira, Jira-failure, PR review, and PR review fix scenarios.

---

# 24. Recommended First Implementation Order

The safest sequence is:

1. Domain models and context capsule hashing.
2. Agent isolation runtime.
3. Bagong clean-context enforcement.
4. BD internal task graph.
5. Jira decoupling.
6. Git capability detection.
7. Safe worktree and branch handling.
8. Durable knowledge schema.
9. Technical tokenizer.
10. Exact and alias search.
11. BM25F index.
12. Bounded relation expansion.
13. Semar retrieval and capsule construction.
14. `create_pr`.
15. `review_pr`.
16. `fix_pr_review`.
17. End-to-end orchestration.
18. Migration utilities.
19. Hardening and documentation.

This order deliberately establishes isolation, task truth, and durable knowledge before adding increasingly clever Git behavior. Otherwise the system may become very efficient at opening pull requests for work it misunderstood, which is an achievement best left to ordinary CI bots.
