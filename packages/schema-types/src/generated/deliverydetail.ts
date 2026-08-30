/* eslint-disable */
/**
 * Code generated from protocol/*.schema.json. DO NOT EDIT.
 * Regenerate with `pnpm --filter @punakawan/schema-types generate`.
 */

/**
 * One delivery's full panel-detail read model: every DeliverySummary field, plus the full plan, per-project plans, requirement sources, provider-specific detail, sessions, provider writes, and one merged activity timeline. Carries no scheduler-internal concepts (lanes, blocked counts, pending questions, a lane-derived next action, the deprecated plan_record_id).
 */
export interface DeliveryDetail {
  id: string;
  title: string;
  status: "pending" | "active" | "completed" | "cancelled";
  source?: {
    kind: "jira" | "adhoc";
    key?: string;
    title?: string;
    status?: string;
  };
  projects: {
    id: string;
    slug: string;
  }[];
  plan?: {
    id: string;
    revision: number;
    objective: string;
    status?: string;
  };
  workflow?: {
    id: string;
    name: string;
  };
  progress?: {
    percent?: number;
    summary: string;
    reported_at: string;
  };
  session?: {
    participant?: string;
    provider?: string;
    model?: string;
    status: string;
    started_at: string;
    stopped_at?: string;
  };
  usage: {
    input_tokens: number;
    output_tokens: number;
    cache_tokens: number;
    tool_calls: number;
    elapsed_ms: number;
    estimated_costs: {
      [k: string]: number;
    };
    pricing_complete: boolean;
  };
  updated_at: string;
  cancellable: boolean;
  projection_revision: number;
  description?: string;
  /**
   * The underlying orchestration's own event-log revision - the value cancel/complete's optimistic concurrency check compares an expected_revision against. A different counter than projection_revision, which this projection uses for list/detail consistency and watch polling instead.
   */
  orchestration_revision: number;
  plan_detail?: Plan;
  project_plans?: {
    project_id: string;
    project_slug: string;
    plan: Plan1;
    head_revision: number;
  }[];
  requirement_sources?: {
    id: string;
    orchestration_id: string;
    provider: "jira" | "confluence" | "github" | "url" | "freetext";
    external_id?: string;
    canonical_key: string;
    content_hash: string;
    title: string;
    summary?: string;
    parent_source_id?: string;
    captured_at: string;
    revision: number;
  }[];
  /**
   * Present only for a Jira-sourced delivery.
   */
  jira?: {
    issue_key: string;
    parent_status?: string;
    touched_items: {
      parent_task_id: string;
      jira_issue_key: string;
      touch_count: number;
      first_touched_at?: string;
      last_touched_at?: string;
    }[];
    transitions: {
      from_status?: string;
      to_status: string;
      /**
       * The write's own outbox status, not the Jira issue's status.
       */
      status: string;
      occurred_at: string;
    }[];
    worklogs: {
      id: string;
      orchestration_id: string;
      case_id?: string;
      execution_id?: string;
      lane_id: string;
      parent_task_id?: string;
      session_id?: string;
      jira_issue_key: string;
      started_at: string;
      duration_seconds: number;
      summary: string;
      sync_status: "pending" | "synced" | "failed";
      jira_worklog_id?: string;
      synced_at?: string;
      created_at: string;
    }[];
    write_health: WriteHealth;
  };
  /**
   * Present only when the delivery has proposed at least one GitHub PR review.
   */
  github?: {
    repository?: string;
    pull_request_number?: number;
    head_sha?: string;
    reviews: {
      id: string;
      repository: string;
      pull_request_number: number;
      head_sha: string;
      findings: {
        [k: string]: unknown;
      }[];
      body: string;
      verdict: "APPROVE" | "REQUEST_CHANGES" | "COMMENT";
      status: "proposed" | "approved" | "submitted" | "failed";
      delivery_execution_id?: string;
      external_review_id?: string;
      failure?: string;
      created_at: string;
      updated_at: string;
    }[];
    write_health: WriteHealth;
  };
  sessions?: {
    id: string;
    case_id: string;
    execution_id: string;
    orchestration_id: string;
    resumed_from_id?: string;
    participant: string;
    status: string;
    started_at: string;
    ended_at?: string;
    worktree_path?: string;
    provider?: string;
    checkpoints: {
      id: string;
      case_id: string;
      execution_id: string;
      session_id: string;
      sequence: number;
      summary: string;
      progress_percent?: number;
      handoff_to?: string;
      created_at: string;
    }[];
  }[];
  provider_writes?: {
    id: string;
    adapter: string;
    operation: string;
    target_key: string;
    status: "pending" | "claimed" | "retrying" | "succeeded" | "failed" | "cancelled" | "reconciling";
    attempt_count: number;
    last_error?: string;
    created_at: string;
    updated_at: string;
  }[];
  /**
   * One merged domain/provider/session timeline, oldest first.
   */
  activity: {
    kind: string;
    summary: string;
    occurred_at: string;
  }[];
}
/**
 * The delivery's full high-level plan content, exactly the revision named by plan.revision above.
 */
export interface Plan {
  id: string;
  project_ids?: string[];
  revision?: number;
  objective: string;
  steps?: {
    id?: string;
    objective: string;
    target_project_id?: string;
    target_repo_id?: string;
    expected_outcome: string;
    acceptance_criteria?: string[];
    verification_method?: string;
    depends_on?: string[];
    unresolved_blocking_question?: string;
  }[];
  acceptance_criteria?: string[];
  verification?: string;
  assumptions?: string[];
  unresolved_questions?: string[];
  created_by?: string;
  created_at?: string;
  status?: string;
  previous_revision?: number;
  reason_for_change?: string;
  legacy_markdown?: string;
  requirements?: string[];
  non_goals?: string[];
  architecture_decision?: string;
  data_model_impact?: string;
  api_impact?: string;
  repository_impact_map?: {
    [k: string]: string;
  };
  implementation_sequence?: string[];
  unit_test_plan?: string[];
  integration_test_plan?: string[];
  e2e_plan?: string[];
  migration_plan?: string[];
  rollback_plan?: string[];
  observability_plan?: string[];
  documentation_plan?: string[];
  deployment_changes?: string[];
  security_considerations?: string[];
  compatibility_considerations?: string[];
  verification_criteria?: string[];
  risks_and_mitigations?: string[];
}
export interface Plan1 {
  id: string;
  project_ids?: string[];
  revision?: number;
  objective: string;
  steps?: {
    id?: string;
    objective: string;
    target_project_id?: string;
    target_repo_id?: string;
    expected_outcome: string;
    acceptance_criteria?: string[];
    verification_method?: string;
    depends_on?: string[];
    unresolved_blocking_question?: string;
  }[];
  acceptance_criteria?: string[];
  verification?: string;
  assumptions?: string[];
  unresolved_questions?: string[];
  created_by?: string;
  created_at?: string;
  status?: string;
  previous_revision?: number;
  reason_for_change?: string;
  legacy_markdown?: string;
  requirements?: string[];
  non_goals?: string[];
  architecture_decision?: string;
  data_model_impact?: string;
  api_impact?: string;
  repository_impact_map?: {
    [k: string]: string;
  };
  implementation_sequence?: string[];
  unit_test_plan?: string[];
  integration_test_plan?: string[];
  e2e_plan?: string[];
  migration_plan?: string[];
  rollback_plan?: string[];
  observability_plan?: string[];
  documentation_plan?: string[];
  deployment_changes?: string[];
  security_considerations?: string[];
  compatibility_considerations?: string[];
  verification_criteria?: string[];
  risks_and_mitigations?: string[];
}
export interface WriteHealth {
  pending: number;
  retrying: number;
  reconciling: number;
  failed: number;
  succeeded: number;
  cancelled: number;
}
