package delivery

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ygrip/punakawan/internal/storage"
	"github.com/ygrip/punakawan/pkg/protocol"
)

// SourceKind distinguishes a Jira-sourced delivery lifetime (reused by
// canonical Jira key until cancelled) from an ad-hoc one (always fresh).
type SourceKind string

const (
	SourceKindJira  SourceKind = "jira"
	SourceKindAdhoc SourceKind = "adhoc"
)

// SourceIdentity names the exact source StartOrResolveExecution resolves a
// lifetime/execution against. Provider and Tenant are only meaningful for
// SourceKindJira; Key must be empty for SourceKindAdhoc.
type SourceIdentity struct {
	Kind     SourceKind
	Provider string
	Tenant   string
	Key      string
}

// DeliveryLifetime is the lifetime internal record for one delivery
// source - a Jira issue (reused by canonical key until cancelled) or an
// ad-hoc source (always its own lifetime). Its executions may finish and
// continue, but its identity never changes.
type DeliveryLifetime struct {
	ID             string    `json:"id"`
	SourceKind     string    `json:"source_kind"`
	SourceProvider string    `json:"source_provider,omitempty"`
	SourceTenant   string    `json:"source_tenant,omitempty"`
	SourceKey      string    `json:"source_key,omitempty"`
	JiraIssueKey   string    `json:"jira_issue_key,omitempty"`
	Status         string    `json:"status"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

// ResolveJiraDeliveryOptions is ResolveJiraDelivery's compatibility input,
// mirroring OrchestrationOptions plus the initial Jira snapshot fields.
type ResolveJiraDeliveryOptions struct {
	Title                string
	Description          string
	WorkflowDefinitionID string
	SnapshotTitle        string
	SnapshotBody         string
	PlanID               string
	PlanRevision         int
}

// ResolvedJiraDelivery is ResolveJiraDelivery's compatibility output.
type ResolvedJiraDelivery struct {
	Case      *DeliveryLifetime  `json:"case"`
	Execution *DeliveryExecution `json:"execution"`
	Created   bool               `json:"created"`
}

// ResolveJiraDelivery resolves the exact normalized Jira key to one lifetime
// case in the single, global (tenant-less) namespace every caller of this
// method has always shared. It is a thin compatibility wrapper over
// StartOrResolveExecution kept for existing single-tenant Jira callers;
// new code should call StartOrResolveExecution (or deliveryservice.Service)
// directly with an explicit tenant.
func (s *Store) ResolveJiraDelivery(ctx context.Context, idempotencyKey, jiraIssueKey string, opts ResolveJiraDeliveryOptions) (*ResolvedJiraDelivery, error) {
	_, issueKey, err := canonicalJiraSource(jiraIssueKey)
	if err != nil {
		return nil, err
	}
	resolved, err := s.StartOrResolveExecution(ctx, idempotencyKey, SourceIdentity{Kind: SourceKindJira, Provider: "jira", Key: issueKey}, OrchestrationOptions{
		Title: opts.Title, Description: opts.Description, WorkflowDefinitionID: opts.WorkflowDefinitionID,
		SnapshotTitle: opts.SnapshotTitle, SnapshotBody: opts.SnapshotBody,
		PlanID: opts.PlanID, PlanRevision: opts.PlanRevision,
	})
	if err != nil {
		return nil, fmt.Errorf("delivery: resolve Jira delivery: %w", err)
	}
	return &ResolvedJiraDelivery{Case: resolved.Lifetime, Execution: resolved.Execution, Created: resolved.CreatedExecution}, nil
}

// ResolvedExecution is StartOrResolveExecution's result: the exact lifetime
// and execution the call resolved to, plus which of the two (if any) it
// actually created.
type ResolvedExecution struct {
	Lifetime         *DeliveryLifetime  `json:"lifetime"`
	Execution        *DeliveryExecution `json:"execution"`
	CreatedLifetime  bool               `json:"created_lifetime"`
	CreatedExecution bool               `json:"created_execution"`
}

// rowQuerier is satisfied by both *sql.DB (reader pool) and *sql.Tx,
// letting the lookups below run either mid-transaction or standalone.
type rowQuerier interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

const caseColumns = `id, source_kind, source_provider, source_tenant, source_key, jira_issue_key, status, created_at, updated_at`

func getActiveJiraLifetime(ctx context.Context, q rowQuerier, tenant, canonicalKey string) (*DeliveryLifetime, error) {
	return scanCase(q.QueryRowContext(ctx, `SELECT `+caseColumns+` FROM delivery_cases WHERE source_kind = 'jira' AND source_provider = 'jira' AND source_tenant = ? AND source_key = ? AND status = 'active'`, tenant, canonicalKey))
}

func getLifetimeByID(ctx context.Context, q rowQuerier, id string) (*DeliveryLifetime, error) {
	return scanCase(q.QueryRowContext(ctx, `SELECT `+caseColumns+` FROM delivery_cases WHERE id = ?`, id))
}

// StartOrResolveExecution is the one provider-neutral entry point for
// starting or resuming a delivery lifetime and execution, inside a single
// storage.DB.Write transaction:
//
//	jira + matching active lifetime + active execution   -> return both unchanged
//	jira + matching active lifetime + terminal execution -> create ordinal + 1
//	jira + only cancelled lifetime (or none at all)      -> create a new lifetime and ordinal 1
//	adhoc                                                 -> always create a new lifetime and ordinal 1
func (s *Store) StartOrResolveExecution(ctx context.Context, idempotencyKey string, source SourceIdentity, opts OrchestrationOptions) (*ResolvedExecution, error) {
	if (opts.PlanID == "") != (opts.PlanRevision == 0) || opts.PlanRevision < 0 {
		return nil, fmt.Errorf("delivery: plan_id and positive plan_revision must be supplied together")
	}
	if opts.WorkflowDefinitionID != "" {
		if s.workflowDefinitions == nil {
			return nil, fmt.Errorf("delivery: workflow_definition_id %q given but no workflow definition resolver is configured", opts.WorkflowDefinitionID)
		}
		if err := s.workflowDefinitions.ValidateEnabled(ctx, opts.WorkflowDefinitionID); err != nil {
			return nil, fmt.Errorf("delivery: attach workflow definition %q: %w", opts.WorkflowDefinitionID, err)
		}
	}

	var issueKey, tenant, canonicalKey string
	switch source.Kind {
	case SourceKindJira:
		issueKey = strings.ToUpper(strings.TrimSpace(source.Key))
		if !jiraKeyPattern.MatchString(issueKey) {
			return nil, fmt.Errorf("delivery: invalid Jira issue key %q", source.Key)
		}
		tenant = strings.TrimSpace(source.Tenant)
		canonicalKey = "jira:" + issueKey
	case SourceKindAdhoc:
		if strings.TrimSpace(source.Key) != "" {
			return nil, fmt.Errorf("delivery: ad-hoc source must not carry a key")
		}
	default:
		return nil, fmt.Errorf("delivery: unknown source kind %q", source.Kind)
	}

	snapshotTitle := strings.TrimSpace(opts.SnapshotTitle)
	if snapshotTitle == "" {
		snapshotTitle = issueKey
	}

	// An ad-hoc start always creates a new lifetime, and has no secondary
	// lookup key to recover it by after a duplicate-idempotency-key retry
	// (unlike Jira's canonical tenant+key), so its lifetime/execution/
	// orchestration ids are derived deterministically from idempotencyKey
	// up front - the same pattern StartDeliveryWithOptions already uses for
	// its own orchestration id.
	var adhocLifetimeID, adhocExecutionID, adhocOrchestrationID string
	if source.Kind == SourceKindAdhoc {
		if idempotencyKey == "" {
			adhocLifetimeID, adhocExecutionID, adhocOrchestrationID = newID(), newID(), newID()
		} else {
			adhocLifetimeID = contentDigest(idempotencyKey, "lifetime")[:26]
			adhocExecutionID = contentDigest(idempotencyKey, "execution")[:26]
			adhocOrchestrationID = contentDigest(idempotencyKey, "orchestration")[:26]
		}
	}

	createdLifetime, createdExecution := false, false
	now := time.Now().UTC()

	err := s.db.Write(ctx, idempotencyKey, "start or resolve delivery execution", func(tx *sql.Tx) error {
		// Ad-hoc always creates a new lifetime, so it never runs the active-
		// lifetime lookup below; lifetimeID starts empty for it exactly like
		// the "no active Jira lifetime" case, and both fall into the same
		// create-a-new-lifetime branch further down.
		lifetimeID := ""
		if source.Kind == SourceKindJira {
			existing, err := getActiveJiraLifetime(ctx, tx, tenant, canonicalKey)
			switch {
			case err == nil:
				lifetimeID = existing.ID
			case errors.Is(err, ErrNotFound):
				lifetimeID = ""
			default:
				return err
			}
		}

		if lifetimeID != "" {
			var last DeliveryExecution
			err := scanExecution(tx.QueryRowContext(ctx, `SELECT id, case_id, orchestration_id, ordinal, status, session_id, started_at, ended_at FROM delivery_executions WHERE case_id = ? ORDER BY ordinal DESC LIMIT 1`, lifetimeID), &last)
			if err != nil && !errors.Is(err, ErrNotFound) {
				return err
			}
			if err == nil {
				events, err := loadEventsTx(ctx, tx, last.OrchestrationID)
				if err != nil {
					return err
				}
				orch, err := reduceOrchestration(last.OrchestrationID, events)
				if err != nil {
					return err
				}
				if !isTerminal(orch.Status) {
					// jira + matching active lifetime + active execution: unchanged.
					return nil
				}
				if _, err := tx.ExecContext(ctx, `UPDATE delivery_executions SET status = ?, ended_at = COALESCE(ended_at, ?) WHERE id = ?`, string(orch.Status), now.Format(timeLayout), last.ID); err != nil {
					return err
				}
			}
			// jira + matching active lifetime + terminal (or missing) execution:
			// fall through to create the next ordinal under this same lifetime.
		} else {
			lifetimeID = newID()
			var sourceProvider, sourceTenant, sourceKeyValue, jiraIssueKeyValue any
			if source.Kind == SourceKindJira {
				sourceProvider, sourceTenant, sourceKeyValue, jiraIssueKeyValue = "jira", tenant, canonicalKey, issueKey
			} else {
				lifetimeID = adhocLifetimeID
				sourceProvider, sourceTenant, sourceKeyValue, jiraIssueKeyValue = "", "", nil, nil
			}
			if _, err := tx.ExecContext(ctx, `INSERT INTO delivery_cases (id, source_kind, source_provider, source_tenant, source_key, jira_issue_key, status, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, 'active', ?, ?)`,
				lifetimeID, string(source.Kind), sourceProvider, sourceTenant, sourceKeyValue, jiraIssueKeyValue, now.Format(timeLayout), now.Format(timeLayout),
			); err != nil {
				return err
			}
			createdLifetime = true
		}

		var ordinal int
		if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(ordinal), 0) + 1 FROM delivery_executions WHERE case_id = ?`, lifetimeID).Scan(&ordinal); err != nil {
			return err
		}
		executionID, orchestrationID := newID(), newID()
		if source.Kind == SourceKindAdhoc {
			executionID, orchestrationID = adhocExecutionID, adhocOrchestrationID
		}
		payloadMap := map[string]any{"unresolved_inputs": []protocol.DeliveryOrchestrationUnresolvedInputsElem{}}
		if title := strings.TrimSpace(opts.Title); title != "" {
			payloadMap["title"] = title
		}
		if description := strings.TrimSpace(opts.Description); description != "" {
			payloadMap["description"] = description
		}
		if opts.WorkflowDefinitionID != "" {
			payloadMap["workflow_definition_id"] = opts.WorkflowDefinitionID
		}
		if opts.PlanID != "" {
			payloadMap["plan_id"] = opts.PlanID
			payloadMap["plan_revision"] = opts.PlanRevision
		}
		payload, err := json.Marshal(payloadMap)
		if err != nil {
			return err
		}
		if err := insertEvent(ctx, tx, eventRow{ID: newID(), OrchestrationID: orchestrationID, IdempotencyKey: idempotencyKey, Type: string(protocol.DeliveryEventTypeOrchestrationCreated), Payload: string(payload), Sequence: 0, OccurredAt: now}); err != nil {
			return err
		}
		sequence := 1
		if source.Kind == SourceKindJira {
			sourceID := newID()
			sourcePayload, err := json.Marshal(map[string]any{"provider": "jira", "external_id": issueKey, "canonical_key": canonicalKey, "content_hash": contentHash(SourceInput{Provider: "jira", ExternalID: issueKey, Title: snapshotTitle, Summary: opts.SnapshotBody}), "title": snapshotTitle, "summary": opts.SnapshotBody})
			if err != nil {
				return err
			}
			if err := insertEvent(ctx, tx, eventRow{ID: newID(), OrchestrationID: orchestrationID, EntityID: &sourceID, IdempotencyKey: idempotencyKey, Type: string(protocol.DeliveryEventTypeRequirementCaptured), Payload: string(sourcePayload), Sequence: sequence, OccurredAt: now}); err != nil {
				return err
			}
			sequence++
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO delivery_executions (id, case_id, orchestration_id, ordinal, status, started_at) VALUES (?, ?, ?, ?, 'active', ?)`, executionID, lifetimeID, orchestrationID, ordinal, now.Format(timeLayout)); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE delivery_cases SET status = 'active', updated_at = ? WHERE id = ?`, now.Format(timeLayout), lifetimeID); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO delivery_projection_versions (orchestration_id, revision, updated_at) VALUES (?, 1, ?)`, orchestrationID, now.Format(timeLayout)); err != nil {
			return err
		}
		if source.Kind == SourceKindJira {
			var snapshotVersion int
			if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(version), 0) + 1 FROM jira_source_snapshots WHERE case_id = ?`, lifetimeID).Scan(&snapshotVersion); err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, `INSERT INTO jira_source_snapshots (id, idempotency_key, case_id, execution_id, session_id, jira_issue_key, version, title, body, content_hash, captured_at) VALUES (?, ?, ?, ?, '', ?, ?, ?, ?, ?, ?)`, newID(), idempotencyKey+":initial-snapshot", lifetimeID, executionID, issueKey, snapshotVersion, snapshotTitle, opts.SnapshotBody, contentHash(SourceInput{Provider: "jira", ExternalID: issueKey, Title: snapshotTitle, Summary: opts.SnapshotBody}), now.Format(timeLayout)); err != nil {
				return err
			}
		}
		createdExecution = true
		return nil
	})
	if err != nil && !errors.Is(err, storage.ErrDuplicateWrite) {
		return nil, fmt.Errorf("delivery: start or resolve delivery execution: %w", err)
	}

	var lifetime *DeliveryLifetime
	if source.Kind == SourceKindJira {
		lifetime, err = getActiveJiraLifetime(ctx, s.db.Reader(), tenant, canonicalKey)
	} else {
		lifetime, err = getLifetimeByID(ctx, s.db.Reader(), adhocLifetimeID)
	}
	if err != nil {
		return nil, err
	}
	execution, err := s.GetExecutionByCase(ctx, lifetime.ID)
	if err != nil {
		return nil, err
	}
	return &ResolvedExecution{Lifetime: lifetime, Execution: execution, CreatedLifetime: createdLifetime, CreatedExecution: createdExecution}, nil
}

func canonicalJiraSource(issueKey string) (string, string, error) {
	key := strings.ToUpper(strings.TrimSpace(issueKey))
	if !jiraKeyPattern.MatchString(key) {
		return "", "", fmt.Errorf("delivery: invalid Jira issue key %q", issueKey)
	}
	return "jira:" + key, key, nil
}

// GetDeliveryCaseByJira returns the exact global (tenant-less) lifetime for
// jiraIssueKey - its active lifetime if one exists, otherwise its most
// recently created (typically cancelled) one.
func (s *Store) GetDeliveryCaseByJira(ctx context.Context, jiraIssueKey string) (*DeliveryLifetime, error) {
	key, _, err := canonicalJiraSource(jiraIssueKey)
	if err != nil {
		return nil, err
	}
	return scanCase(s.db.Reader().QueryRowContext(ctx, `SELECT `+caseColumns+` FROM delivery_cases WHERE source_kind = 'jira' AND source_key = ? ORDER BY (status = 'active') DESC, created_at DESC LIMIT 1`, key))
}

func scanCase(row lifecycleScanner) (*DeliveryLifetime, error) {
	var v DeliveryLifetime
	var sourceKey, jiraIssueKey sql.NullString
	var created, updated string
	if err := row.Scan(&v.ID, &v.SourceKind, &v.SourceProvider, &v.SourceTenant, &sourceKey, &jiraIssueKey, &v.Status, &created, &updated); err != nil {
		return nil, noRow(err)
	}
	if sourceKey.Valid {
		v.SourceKey = sourceKey.String
	}
	if jiraIssueKey.Valid {
		v.JiraIssueKey = jiraIssueKey.String
	}
	var err error
	if v.CreatedAt, err = scanTime(created); err != nil {
		return nil, err
	}
	if v.UpdatedAt, err = scanTime(updated); err != nil {
		return nil, err
	}
	return &v, nil
}
