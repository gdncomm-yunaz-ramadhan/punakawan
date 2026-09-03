// Package delivery is the canonical multi-project delivery control plane:
// project registry, orchestrations, and delivery lanes, persisted through
// the SQLite storage kernel (internal/storage).
// Orchestration and lane state is never written directly — it is derived
// by replaying an append-only, idempotent event log (see reduce.go).
package delivery

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ygrip/punakawan/internal/deliveryhooks"
	"github.com/ygrip/punakawan/internal/storage"
	"github.com/ygrip/punakawan/pkg/protocol"
)

const timeLayout = time.RFC3339Nano

// Store is the control plane's persistence boundary. It never falls
// back to an ambient working-directory project: every call is scoped by
// an explicit id.
type Store struct {
	db *storage.DB
	// workflowDefinitions is nil unless WithWorkflowDefinitionResolver is
	// passed to NewStore. It is an injected interface rather than a
	// direct dependency on internal/workflowdef's concrete store because
	// that store owns an entirely separate persistence lifecycle (one
	// YAML file per definition id) than this package's own event log, so
	// there is no shared lifecycle to couple to directly.
	workflowDefinitions WorkflowDefinitionResolver
	// hooks is nil unless WithHooks is passed to NewStore, in which case
	// every dispatch call below through it is a no-op (see
	// deliveryhooks.Dispatcher.Dispatch), so every existing Store caller
	// that never configures hooks is unaffected.
	hooks *deliveryhooks.Dispatcher
	// requireJiraWorkLogs gates completion of Jira-backed lanes until at
	// least one explicit task-bound worklog has been recorded. It is opt-in
	// so existing non-Jira delivery callers remain unchanged.
	requireJiraWorkLogs bool
}

// NewStore wraps an opened storage kernel database. opts is variadic so
// every existing call site keeps compiling unchanged; pass
// WithWorkflowDefinitionResolver to enable workflow_definition_id
// attachment and the role-stage gate's definition-aware behavior.
func NewStore(db *storage.DB, opts ...StoreOption) *Store {
	s := &Store{db: db}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// NewID mints a filesystem-safe ULID for a new project, orchestration,
// or lane. Callers generate ids up front so creation calls are
// idempotent on retry (the same id plus the same idempotency key is a
// no-op, not a duplicate).
func NewID() string { return newID() }

// RegisterProject adds a project to the registry. A duplicate slug
// fails; a duplicate idempotencyKey is harmless and returns the
// already-registered project.
func (s *Store) RegisterProject(ctx context.Context, idempotencyKey, id, slug, repositoryURL, defaultBranch string) (*protocol.DeliveryProject, error) {
	now := time.Now().UTC()
	err := s.db.Write(ctx, idempotencyKey, "register project "+slug, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx,
			`INSERT INTO delivery_projects (id, slug, repository_url, repository_identity, default_branch, status, registered_at, revision) VALUES (?, ?, ?, ?, ?, 'active', ?, 0)`,
			id, slug, repositoryURL, RepositoryIdentity(repositoryURL), defaultBranch, now.Format(timeLayout),
		)
		return err
	})
	if errors.Is(err, storage.ErrDuplicateWrite) {
		return s.GetProject(ctx, id)
	}
	if err != nil {
		return nil, fmt.Errorf("delivery: register project %s: %w", slug, err)
	}
	return s.GetProject(ctx, id)
}

// UpsertProject creates a project or updates the repository configuration of
// the existing project with the same slug. Existing ids and registration
// timestamps remain stable across updates.
func (s *Store) UpsertProject(ctx context.Context, idempotencyKey, id, slug, repositoryURL, defaultBranch string) (*protocol.DeliveryProject, error) {
	now := time.Now().UTC()
	err := s.db.Write(ctx, idempotencyKey, "upsert project "+slug, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `
			INSERT INTO delivery_projects (id, slug, repository_url, repository_identity, default_branch, status, registered_at, revision)
			VALUES (?, ?, ?, ?, ?, 'active', ?, 0)
			ON CONFLICT(slug) DO UPDATE SET
				repository_url = excluded.repository_url,
				repository_identity = excluded.repository_identity,
				default_branch = excluded.default_branch,
				status = 'active',
				revision = delivery_projects.revision + 1`,
			id, slug, repositoryURL, RepositoryIdentity(repositoryURL), defaultBranch, now.Format(timeLayout),
		)
		return err
	})
	if errors.Is(err, storage.ErrDuplicateWrite) {
		return s.GetProjectBySlug(ctx, slug)
	}
	if err != nil {
		return nil, fmt.Errorf("delivery: upsert project %s: %w", slug, err)
	}
	return s.GetProjectBySlug(ctx, slug)
}

// MergeProjectMetadata merges partial into projectID's stored metadata,
// field by field: only the fields partial actually sets (its zero-value
// pointer/slice fields are omitted by its own JSON encoding) are written,
// so a caller that only knows one fact - e.g. a PostToolUse hook that
// detected just the package manager from go.mod - never clobbers a fact a
// different call already recorded. Returns the project with its metadata
// as stored after the merge.
func (s *Store) MergeProjectMetadata(ctx context.Context, idempotencyKey, projectID string, partial protocol.DeliveryProjectMetadata) (*protocol.DeliveryProject, error) {
	partialJSON, err := json.Marshal(partial)
	if err != nil {
		return nil, fmt.Errorf("delivery: encode metadata for project %s: %w", projectID, err)
	}
	var partialFields map[string]json.RawMessage
	if err := json.Unmarshal(partialJSON, &partialFields); err != nil {
		return nil, fmt.Errorf("delivery: decode metadata fields for project %s: %w", projectID, err)
	}
	if len(partialFields) == 0 {
		return s.GetProject(ctx, projectID)
	}

	writeErr := s.db.Write(ctx, idempotencyKey, "merge project metadata "+projectID, func(tx *sql.Tx) error {
		var existing sql.NullString
		if err := tx.QueryRowContext(ctx, `SELECT metadata FROM delivery_projects WHERE id = ?`, projectID).Scan(&existing); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ErrNotFound
			}
			return err
		}
		merged := map[string]json.RawMessage{}
		if existing.Valid && existing.String != "" {
			if err := json.Unmarshal([]byte(existing.String), &merged); err != nil {
				return fmt.Errorf("delivery: decode existing metadata for project %s: %w", projectID, err)
			}
		}
		for k, v := range partialFields {
			merged[k] = v
		}
		encoded, err := json.Marshal(merged)
		if err != nil {
			return fmt.Errorf("delivery: encode merged metadata for project %s: %w", projectID, err)
		}
		_, err = tx.ExecContext(ctx, `UPDATE delivery_projects SET metadata = ?, revision = revision + 1 WHERE id = ?`, string(encoded), projectID)
		return err
	})
	if writeErr != nil && !errors.Is(writeErr, storage.ErrDuplicateWrite) {
		if errors.Is(writeErr, ErrNotFound) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("delivery: merge project metadata %s: %w", projectID, writeErr)
	}
	return s.GetProject(ctx, projectID)
}

// GetProjectBySlug returns the project registered under slug.
func (s *Store) GetProjectBySlug(ctx context.Context, slug string) (*protocol.DeliveryProject, error) {
	row := s.db.Reader().QueryRowContext(ctx,
		`SELECT id, slug, repository_url, default_branch, status, registered_at, revision, metadata FROM delivery_projects WHERE slug = ?`, slug)
	return scanProject(row, slug)
}

// FindProjectsByRepositoryURL returns at most two projects matching a Git
// remote. Two rows are enough for callers to distinguish the normal zero/one
// result from an ambiguous normalized identity without reading the registry.
func (s *Store) FindProjectsByRepositoryURL(ctx context.Context, repositoryURL string) ([]protocol.DeliveryProject, error) {
	identity := RepositoryIdentity(repositoryURL)
	if identity == "" {
		return []protocol.DeliveryProject{}, nil
	}
	variants := repositoryURLVariants(repositoryURL)
	args := make([]any, 0, len(variants)+1)
	args = append(args, identity)
	for _, variant := range variants {
		args = append(args, variant)
	}
	return s.queryProjects(ctx, `
		SELECT id, slug, repository_url, default_branch, status, registered_at, revision, metadata
		FROM delivery_projects
		WHERE repository_identity = ? OR repository_url IN (`+sqlPlaceholders(len(variants))+`)
		ORDER BY slug
		LIMIT 2`, args...)
}

// FindProjectsBySlugs returns explicitly selected projects ordered by slug.
func (s *Store) FindProjectsBySlugs(ctx context.Context, slugs []string) ([]protocol.DeliveryProject, error) {
	if len(slugs) == 0 {
		return []protocol.DeliveryProject{}, nil
	}
	args := make([]any, len(slugs))
	for i, slug := range slugs {
		args[i] = slug
	}
	return s.queryProjects(ctx, `
		SELECT id, slug, repository_url, default_branch, status, registered_at, revision, metadata
		FROM delivery_projects
		WHERE slug IN (`+sqlPlaceholders(len(slugs))+`)
		ORDER BY slug`, args...)
}

// SearchProjects finds explicitly requested project names or repository URLs.
// limit is supplied by the MCP boundary and therefore always positive and
// bounded before it reaches this store.
func (s *Store) SearchProjects(ctx context.Context, query string, limit int) ([]protocol.DeliveryProject, error) {
	pattern := "%" + escapeLike(query) + "%"
	return s.queryProjects(ctx, `
		SELECT id, slug, repository_url, default_branch, status, registered_at, revision, metadata
		FROM delivery_projects
		WHERE slug LIKE ? ESCAPE '\' COLLATE NOCASE
		   OR repository_url LIKE ? ESCAPE '\' COLLATE NOCASE
		ORDER BY slug
		LIMIT ?`, pattern, pattern, limit)
}

func (s *Store) queryProjects(ctx context.Context, query string, args ...any) ([]protocol.DeliveryProject, error) {
	rows, err := s.db.Reader().QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("delivery: query projects: %w", err)
	}
	defer rows.Close()
	projects := make([]protocol.DeliveryProject, 0)
	for rows.Next() {
		project, err := scanProject(rows, "")
		if err != nil {
			return nil, err
		}
		projects = append(projects, *project)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("delivery: query projects: %w", err)
	}
	return projects, nil
}

func sqlPlaceholders(count int) string {
	return strings.TrimSuffix(strings.Repeat("?,", count), ",")
}

func escapeLike(value string) string {
	value = strings.ReplaceAll(value, `\`, `\\`)
	value = strings.ReplaceAll(value, `%`, `\%`)
	return strings.ReplaceAll(value, `_`, `\_`)
}

// GetProject fails closed (ErrNotFound) for an unknown project id.
func (s *Store) GetProject(ctx context.Context, id string) (*protocol.DeliveryProject, error) {
	row := s.db.Reader().QueryRowContext(ctx,
		`SELECT id, slug, repository_url, default_branch, status, registered_at, revision, metadata FROM delivery_projects WHERE id = ?`, id)
	return scanProject(row, id)
}

type projectScanner interface {
	Scan(dest ...any) error
}

func scanProject(row projectScanner, identity string) (*protocol.DeliveryProject, error) {
	var p protocol.DeliveryProject
	var defaultBranch, registeredAt string
	var metadata sql.NullString
	if err := row.Scan(&p.Id, &p.Slug, &p.RepositoryUrl, &defaultBranch, &p.Status, &registeredAt, &p.Revision, &metadata); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("delivery: get project %s: %w", identity, err)
	}
	if defaultBranch != "" {
		p.DefaultBranch = &defaultBranch
	}
	t, err := time.Parse(timeLayout, registeredAt)
	if err != nil {
		return nil, fmt.Errorf("delivery: parse registered_at for project %s: %w", identity, err)
	}
	p.RegisteredAt = t
	if metadata.Valid && metadata.String != "" {
		var m protocol.DeliveryProjectMetadata
		if err := json.Unmarshal([]byte(metadata.String), &m); err != nil {
			return nil, fmt.Errorf("delivery: decode metadata for project %s: %w", identity, err)
		}
		p.Metadata = &m
	}
	return &p, nil
}

// OrchestrationOptions carries the attributes a new orchestration may
// optionally be created with, so there is one options value per creation
// call rather than a growing tail of positional parameters.
// WorkflowDefinitionID is fixed for the life of the run; Title and
// Description are merely the values the run starts out with, and
// UpdateOrchestrationDetails can change either afterwards.
type OrchestrationOptions struct {
	// WorkflowDefinitionID, when set, must name an existing, enabled
	// workflow definition. Once attached, every lane's role-stage gate
	// consults that definition's Roles map instead of always requiring
	// all four stages.
	WorkflowDefinitionID string
	// Title is a short human-readable summary of what the run delivers,
	// as written by whoever started it. Left empty, the orchestration
	// simply carries no title and consumers derive a readable label from
	// its requirement references instead.
	Title string
	// Description is longer prose about what the run is for and why it
	// exists. Left empty, the orchestration carries none and nothing
	// substitutes for it - a derived title is traceable back to a
	// requirement the caller supplied, whereas derived prose would be
	// invention.
	Description string
	// PlanID and PlanRevision identify the immutable high-level plan that
	// coordinates work across every project in the delivery.
	PlanID       string
	PlanRevision int
	// SnapshotTitle and SnapshotBody are StartOrResolveExecution's initial
	// Jira source snapshot content, used only when the source is Jira.
	SnapshotTitle string
	SnapshotBody  string
}

// OrchestrationDetails names the descriptive attributes of an
// orchestration that stay editable for its whole life. Every field is a
// pointer so the three cases stay distinct: nil leaves the current value
// alone, a pointer to a non-empty string sets it, and a pointer to an
// empty string clears it back to absent.
type OrchestrationDetails struct {
	Title       *string
	Description *string
	// PlanRecordID is deprecated: the id of the knowledge record holding
	// the run's final plan, as returned by the old submit_final_plan
	// write path. This package does not resolve it: the knowledge store
	// has an entirely separate persistence lifecycle from this event
	// log, the same reason workflow definitions are reached through an
	// injected resolver rather than a direct dependency. New deliveries
	// should set PlanID/PlanRevision instead (§4.4 - plans now live in
	// internal/plan, not as knowledge records); this field is kept only
	// so pre-existing references remain settable/readable.
	PlanRecordID *string
	// PlanID and PlanRevision together name the exact internal/plan
	// revision this run is built from. Like PlanRecordID, this package
	// does not resolve them - internal/plan has its own persistence
	// lifecycle, reached the same way the knowledge store is.
	PlanID       *string
	PlanRevision *int
	// SessionID is the id of the workflow run driving the delivery - the
	// identifier a session is known by everywhere else in the system.
	SessionID *string
}

// set reports whether details asks for any change at all.
func (d OrchestrationDetails) set() bool {
	return d.Title != nil || d.Description != nil || d.PlanRecordID != nil ||
		d.PlanID != nil || d.PlanRevision != nil || d.SessionID != nil
}

// payload renders details as an orchestration.details_updated payload
// carrying a key for each field the caller actually supplied, so the
// reducer can tell "leave this alone" from "clear this".
func (d OrchestrationDetails) payload() map[string]interface{} {
	out := map[string]interface{}{}
	for key, value := range map[string]*string{
		"title": d.Title, "description": d.Description,
		"plan_record_id": d.PlanRecordID, "plan_id": d.PlanID, "session_id": d.SessionID,
	} {
		if value != nil {
			out[key] = strings.TrimSpace(*value)
		}
	}
	if d.PlanRevision != nil {
		out["plan_revision"] = *d.PlanRevision
	}
	return out
}

// CreateOrchestration appends the orchestration.created event that
// establishes id. A duplicate idempotencyKey is harmless and returns
// the already-created orchestration. It is a thin wrapper over
// CreateOrchestrationWithOptions with no options set, so every existing
// caller of this exact signature keeps compiling and behaving exactly as
// before.
func (s *Store) CreateOrchestration(ctx context.Context, idempotencyKey, id string, inputs []protocol.DeliveryOrchestrationUnresolvedInputsElem) (*protocol.DeliveryOrchestration, error) {
	return s.CreateOrchestrationWithOptions(ctx, idempotencyKey, id, inputs, OrchestrationOptions{})
}

// CreateOrchestrationWithOptions is CreateOrchestration plus the
// optional attributes in opts. A zero opts behaves identically to
// CreateOrchestration. A non-empty WorkflowDefinitionID is validated
// (must exist and be enabled) via the configured
// WorkflowDefinitionResolver before anything is written - with none
// configured, attaching a definition is rejected outright rather than
// silently accepted and later ignored by the gate.
func (s *Store) CreateOrchestrationWithOptions(ctx context.Context, idempotencyKey, id string, inputs []protocol.DeliveryOrchestrationUnresolvedInputsElem, opts OrchestrationOptions) (*protocol.DeliveryOrchestration, error) {
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
	if inputs == nil {
		inputs = []protocol.DeliveryOrchestrationUnresolvedInputsElem{}
	}
	payloadMap := map[string]interface{}{"unresolved_inputs": inputs}
	if opts.WorkflowDefinitionID != "" {
		payloadMap["workflow_definition_id"] = opts.WorkflowDefinitionID
	}
	if title := strings.TrimSpace(opts.Title); title != "" {
		payloadMap["title"] = title
	}
	if description := strings.TrimSpace(opts.Description); description != "" {
		payloadMap["description"] = description
	}
	if opts.PlanID != "" {
		payloadMap["plan_id"] = opts.PlanID
		payloadMap["plan_revision"] = opts.PlanRevision
	}
	payload, err := json.Marshal(payloadMap)
	if err != nil {
		return nil, fmt.Errorf("delivery: encode orchestration.created payload: %w", err)
	}
	now := time.Now().UTC()
	err = s.db.Write(ctx, idempotencyKey, "create orchestration "+id, func(tx *sql.Tx) error {
		return insertEvent(ctx, tx, eventRow{
			ID: newID(), OrchestrationID: id, IdempotencyKey: idempotencyKey,
			Type: string(protocol.DeliveryEventTypeOrchestrationCreated), Payload: string(payload),
			Sequence: 0, OccurredAt: now,
		})
	})
	if errors.Is(err, storage.ErrDuplicateWrite) {
		return s.GetOrchestration(ctx, id)
	}
	if err != nil {
		return nil, fmt.Errorf("delivery: create orchestration %s: %w", id, err)
	}
	orch, err := s.GetOrchestration(ctx, id)
	if err != nil {
		return nil, err
	}
	// Dispatched only on the fresh-create path above, never on the
	// duplicate-idempotency-key retry: a retried StartDelivery call must
	// not re-announce "delivery started" for a delivery that already
	// started.
	s.hooks.Dispatch(ctx, deliveryhooks.Event{
		Type: deliveryhooks.EventDeliveryStarted, DeliveryID: id, Revision: orch.Revision,
		Title: derefOrEmpty(orch.Title), Projects: orch.ProjectIds,
		Summary: "delivery started",
	})
	return orch, nil
}

// GetOrchestration fails closed (ErrNotFound) for an unknown id.
func (s *Store) GetOrchestration(ctx context.Context, id string) (*protocol.DeliveryOrchestration, error) {
	events, err := loadEvents(ctx, s.db.Reader(), id)
	if err != nil {
		return nil, err
	}
	return reduceOrchestration(id, events)
}

// ListOrchestrations returns every orchestration this store knows about,
// oldest first. There is exactly one orchestration.created event per
// orchestration (always at sequence 0), so selecting that event type
// alone already gives one row per id - no DISTINCT needed.
func (s *Store) ListOrchestrations(ctx context.Context) ([]*protocol.DeliveryOrchestration, error) {
	rows, err := s.db.Reader().QueryContext(ctx,
		`SELECT orchestration_id FROM delivery_events WHERE type = ? ORDER BY occurred_at`,
		string(protocol.DeliveryEventTypeOrchestrationCreated),
	)
	if err != nil {
		return nil, fmt.Errorf("delivery: list orchestrations: %w", err)
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("delivery: scan orchestration id: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("delivery: list orchestrations: %w", err)
	}

	out := make([]*protocol.DeliveryOrchestration, 0, len(ids))
	for _, id := range ids {
		orch, err := s.GetOrchestration(ctx, id)
		if err != nil {
			return nil, err
		}
		out = append(out, orch)
	}
	return out, nil
}

// CancelOrchestration appends orchestration.cancelled after checking
// expectedRevision against the current derived revision and that the
// orchestration is not already terminal.
func (s *Store) CancelOrchestration(ctx context.Context, idempotencyKey, id string, expectedRevision int) (*protocol.DeliveryOrchestration, error) {
	err := s.db.Write(ctx, idempotencyKey, "cancel orchestration "+id, func(tx *sql.Tx) error {
		events, err := loadEventsTx(ctx, tx, id)
		if err != nil {
			return err
		}
		current, err := reduceOrchestration(id, events)
		if err != nil {
			return err
		}
		if current.Revision != expectedRevision {
			return ErrRevisionConflict
		}
		if isTerminal(current.Status) {
			return ErrInvalidState
		}
		now := time.Now().UTC()
		if err := insertEvent(ctx, tx, eventRow{
			ID: newID(), OrchestrationID: id, IdempotencyKey: idempotencyKey,
			Type: string(protocol.DeliveryEventTypeOrchestrationCancelled), Payload: "{}",
			Sequence: len(events), OccurredAt: now,
		}); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO delivery_projection_versions (orchestration_id, revision, updated_at) VALUES (?, 1, ?) ON CONFLICT(orchestration_id) DO UPDATE SET revision = delivery_projection_versions.revision + 1, updated_at = excluded.updated_at`, id, now.Format(timeLayout)); err != nil {
			return err
		}
		var caseID string
		err = tx.QueryRowContext(ctx, `SELECT case_id FROM delivery_executions WHERE orchestration_id = ?`, id).Scan(&caseID)
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `UPDATE delivery_cases SET status = 'cancelled', updated_at = ? WHERE id = ?`, now.Format(timeLayout), caseID)
		return err
	})
	if !errors.Is(err, storage.ErrDuplicateWrite) && err != nil {
		return nil, err
	}
	orch, getErr := s.GetOrchestration(ctx, id)
	if getErr != nil {
		return nil, getErr
	}
	// Cancellation is reported as delivery.failed rather than
	// delivery.completed since it means the delivery did not reach a
	// successful outcome; CompleteOrchestration dispatches
	// delivery.completed for the successful one. Only the fresh-cancel
	// path dispatches here, never the duplicate-idempotency-key retry,
	// for the same reason
	// CreateOrchestrationWithOptions only dispatches on its own fresh
	// path.
	if err == nil {
		s.hooks.Dispatch(ctx, deliveryhooks.Event{
			Type: deliveryhooks.EventDeliveryFailed, DeliveryID: id, Revision: orch.Revision,
			Title: derefOrEmpty(orch.Title), Projects: orch.ProjectIds,
			PlanID: derefOrEmpty(orch.PlanId), PlanRevision: derefOrZero(orch.PlanRevision),
			PullRequests: s.pullRequestURLs(ctx, id),
			Summary:      "delivery cancelled",
		})
	}
	return orch, nil
}

// RegisterInput appends input.registered, adding one more not-yet-routed
// requirement reference to the orchestration.
func (s *Store) RegisterInput(ctx context.Context, idempotencyKey, orchestrationID string, expectedRevision int, input protocol.DeliveryOrchestrationUnresolvedInputsElem) (*protocol.DeliveryOrchestration, error) {
	payload := map[string]interface{}{"reference": input.Reference}
	if input.Note != nil {
		payload["note"] = *input.Note
	}
	return s.appendOrchestrationEvent(ctx, idempotencyKey, orchestrationID, expectedRevision, protocol.DeliveryEventTypeInputRegistered, payload)
}

// ResolveInput appends input.resolved, removing a requirement reference
// once routing has assigned it to a project.
func (s *Store) ResolveInput(ctx context.Context, idempotencyKey, orchestrationID string, expectedRevision int, reference string) (*protocol.DeliveryOrchestration, error) {
	return s.appendOrchestrationEvent(ctx, idempotencyKey, orchestrationID, expectedRevision, protocol.DeliveryEventTypeInputResolved, map[string]interface{}{"reference": reference})
}

// UpdateOrchestrationDetails appends orchestration.details_updated,
// changing whichever of title, description, plan reference, and session
// id the caller supplied and leaving the rest alone. A call that asks
// for no change at all is rejected rather than recorded: an event that
// changes nothing would still bump the revision and invalidate every
// concurrent caller's expected_revision for no reason.
func (s *Store) UpdateOrchestrationDetails(ctx context.Context, idempotencyKey, orchestrationID string, expectedRevision int, details OrchestrationDetails) (*protocol.DeliveryOrchestration, error) {
	if !details.set() {
		return nil, fmt.Errorf("delivery: update orchestration %s: no field to update was supplied", orchestrationID)
	}

	// planWasUnset is captured from the pre-write state read inside the
	// same write transaction below (not from a separate read before/after
	// it), so a retried call that hits the duplicate-idempotency-key path
	// never sees a stale "already set" view of a plan that only this same
	// retried call itself would have set.
	var planWasUnset bool
	fresh := false
	payload := details.payload()
	encoded, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("delivery: encode %s payload: %w", protocol.DeliveryEventTypeOrchestrationDetailsUpdated, err)
	}
	writeErr := s.db.Write(ctx, idempotencyKey, string(protocol.DeliveryEventTypeOrchestrationDetailsUpdated)+" "+orchestrationID, func(tx *sql.Tx) error {
		events, err := loadEventsTx(ctx, tx, orchestrationID)
		if err != nil {
			return err
		}
		current, err := reduceOrchestration(orchestrationID, events)
		if err != nil {
			return err
		}
		if current.Revision != expectedRevision {
			return ErrRevisionConflict
		}
		if isTerminal(current.Status) {
			return ErrInvalidState
		}
		planWasUnset = current.PlanId == nil || *current.PlanId == ""
		fresh = true
		return insertEvent(ctx, tx, eventRow{
			ID: newID(), OrchestrationID: orchestrationID, IdempotencyKey: idempotencyKey,
			Type: string(protocol.DeliveryEventTypeOrchestrationDetailsUpdated), Payload: string(encoded),
			Sequence: len(events), OccurredAt: time.Now().UTC(),
		})
	})
	if writeErr != nil && !errors.Is(writeErr, storage.ErrDuplicateWrite) {
		return nil, writeErr
	}
	updated, err := s.GetOrchestration(ctx, orchestrationID)
	if err != nil {
		return nil, err
	}
	if fresh && (details.PlanID != nil || details.PlanRevision != nil) {
		eventType := deliveryhooks.EventPlanRevised
		if planWasUnset {
			eventType = deliveryhooks.EventPlanCreated
		}
		s.hooks.Dispatch(ctx, deliveryhooks.Event{
			Type: eventType, DeliveryID: orchestrationID, Revision: updated.Revision,
			Title: derefOrEmpty(updated.Title), Projects: updated.ProjectIds,
			PlanID: derefOrEmpty(updated.PlanId), PlanRevision: derefOrZero(updated.PlanRevision),
			Summary: "orchestration plan reference updated",
		})
	}
	return updated, nil
}

// AttachProject appends project.attached, recording that this run
// involves an already-registered project. The project must exist and be
// active, the same bar CreateLane holds a lane's project to, so a run
// never claims to involve a project nothing could route work to.
// Attaching a project that is already attached changes nothing and
// appends no event, so a retry is harmless.
func (s *Store) AttachProject(ctx context.Context, idempotencyKey, orchestrationID string, expectedRevision int, projectID string) (*protocol.DeliveryOrchestration, error) {
	return s.appendProjectEvent(ctx, idempotencyKey, orchestrationID, expectedRevision, protocol.DeliveryEventTypeProjectAttached, projectID,
		func(tx *sql.Tx, orch *protocol.DeliveryOrchestration, lanes map[string]*protocol.DeliveryLane) (bool, error) {
			if indexOfString(orch.ProjectIds, projectID) >= 0 {
				return false, nil
			}
			var status string
			if err := tx.QueryRowContext(ctx, `SELECT status FROM delivery_projects WHERE id = ?`, projectID).Scan(&status); err != nil {
				if errors.Is(err, sql.ErrNoRows) {
					return false, ErrNotFound
				}
				return false, err
			}
			if status != string(protocol.DeliveryProjectStatusActive) {
				return false, ErrProjectInactive
			}
			return true, nil
		})
}

// DetachProject appends project.detached, withdrawing the statement that
// this run involves projectID.
//
// Detaching never touches lanes. A lane's project scope is fixed at its
// creation and every later call is checked against it, so silently
// reassigning or deleting a lane here would break the one invariant the
// whole package leans on - and orphaning it would leave work running
// against a project the run claims not to involve. So a project with any
// lane still short of a terminal status (accepted or failed) cannot be
// detached at all: ErrProjectHasActiveLanes says so, and the caller
// finishes or fails those lanes first.
//
// Lanes that already reached a terminal status do not block detaching,
// and they keep their project_id afterwards - the run's history still
// says the work happened there. A consumer therefore still sees that
// project in a delivery view, marked as no longer attached rather than
// erased.
//
// Unlike attaching, detaching does not require the project to still be
// registered and active: a project disabled after it was attached is
// exactly the one somebody most wants to withdraw.
func (s *Store) DetachProject(ctx context.Context, idempotencyKey, orchestrationID string, expectedRevision int, projectID string) (*protocol.DeliveryOrchestration, error) {
	return s.appendProjectEvent(ctx, idempotencyKey, orchestrationID, expectedRevision, protocol.DeliveryEventTypeProjectDetached, projectID,
		func(tx *sql.Tx, orch *protocol.DeliveryOrchestration, lanes map[string]*protocol.DeliveryLane) (bool, error) {
			if indexOfString(orch.ProjectIds, projectID) < 0 {
				return false, ErrNotFound
			}
			for _, lane := range lanes {
				if lane.ProjectId == projectID && !isLaneTerminal(lane.Status) {
					return false, ErrProjectHasActiveLanes
				}
			}
			return true, nil
		})
}

// appendProjectEvent is appendOrchestrationEvent for the two
// project-membership events, which need to inspect the run's lanes and
// its already-attached set inside the same transaction they append from.
// decide reports whether the event is still worth appending (false with
// a nil error means the request was already satisfied, which is a
// success, not a no-op to complain about).
func (s *Store) appendProjectEvent(
	ctx context.Context,
	idempotencyKey, orchestrationID string,
	expectedRevision int,
	eventType protocol.DeliveryEventType,
	projectID string,
	decide func(*sql.Tx, *protocol.DeliveryOrchestration, map[string]*protocol.DeliveryLane) (bool, error),
) (*protocol.DeliveryOrchestration, error) {
	if projectID == "" {
		return nil, fmt.Errorf("delivery: %s on %s: project_id is required", eventType, orchestrationID)
	}
	payload, err := json.Marshal(map[string]interface{}{"project_id": projectID})
	if err != nil {
		return nil, fmt.Errorf("delivery: encode %s payload: %w", eventType, err)
	}
	err = s.db.Write(ctx, idempotencyKey, string(eventType)+" "+orchestrationID, func(tx *sql.Tx) error {
		events, err := loadEventsTx(ctx, tx, orchestrationID)
		if err != nil {
			return err
		}
		current, err := reduceOrchestration(orchestrationID, events)
		if err != nil {
			return err
		}
		if current.Revision != expectedRevision {
			return ErrRevisionConflict
		}
		if isTerminal(current.Status) {
			return ErrInvalidState
		}

		lanes, err := allLanes(orchestrationID, events)
		if err != nil {
			return err
		}
		proceed, err := decide(tx, current, lanes)
		if err != nil || !proceed {
			return err
		}
		return insertEvent(ctx, tx, eventRow{
			ID: newID(), OrchestrationID: orchestrationID, IdempotencyKey: idempotencyKey,
			Type: string(eventType), Payload: string(payload),
			Sequence: len(events), OccurredAt: time.Now().UTC(),
		})
	})
	if errors.Is(err, storage.ErrDuplicateWrite) || err == nil {
		return s.GetOrchestration(ctx, orchestrationID)
	}
	return nil, err
}

func (s *Store) appendOrchestrationEvent(ctx context.Context, idempotencyKey, orchestrationID string, expectedRevision int, eventType protocol.DeliveryEventType, payload map[string]interface{}) (*protocol.DeliveryOrchestration, error) {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("delivery: encode %s payload: %w", eventType, err)
	}
	err = s.db.Write(ctx, idempotencyKey, string(eventType)+" "+orchestrationID, func(tx *sql.Tx) error {
		events, err := loadEventsTx(ctx, tx, orchestrationID)
		if err != nil {
			return err
		}
		current, err := reduceOrchestration(orchestrationID, events)
		if err != nil {
			return err
		}
		if current.Revision != expectedRevision {
			return ErrRevisionConflict
		}
		if isTerminal(current.Status) {
			return ErrInvalidState
		}
		return insertEvent(ctx, tx, eventRow{
			ID: newID(), OrchestrationID: orchestrationID, IdempotencyKey: idempotencyKey,
			Type: string(eventType), Payload: string(encoded),
			Sequence: len(events), OccurredAt: time.Now().UTC(),
		})
	})
	if errors.Is(err, storage.ErrDuplicateWrite) || err == nil {
		return s.GetOrchestration(ctx, orchestrationID)
	}
	return nil, err
}

// CreateLane appends lane.created after verifying the orchestration is
// open and the project exists and is active. If parentTaskID is
// non-empty (a lane may legitimately be created before a task is
// assigned to it, per DeliveryLane's own parent_task_id field), the
// task must actually exist in this orchestration and, if it is
// already routed, must be routed to this same projectID
// (ErrScopeMismatch otherwise) - a lane's project scope must always
// agree with its own task's routing, never silently diverge from it.
// A lane's project scope is fixed at creation and checked on every
// later call against it. It is a thin wrapper over CreateLaneWithOptions
// with no options set, so every existing caller of this exact signature
// keeps compiling and behaving exactly as before.
func (s *Store) CreateLane(ctx context.Context, idempotencyKey, id, orchestrationID, projectID, parentTaskID string) (*protocol.DeliveryLane, error) {
	return s.CreateLaneWithOptions(ctx, idempotencyKey, id, orchestrationID, projectID, parentTaskID, LaneOptions{})
}

// LaneOptions carries the attributes a new lane may optionally be
// created with, following OrchestrationOptions' shape so a later
// addition does not grow CreateLane's parameter list again.
type LaneOptions struct {
	// SessionID names the workflow run that decided this lane should
	// exist. It is recorded once, at creation, and never amended: it
	// answers "which session opened this", not "who is working it now",
	// which the lane's lease already answers. Left empty, the lane
	// simply names no session, as every lane recorded before this field
	// existed does.
	SessionID string
}

// CreateLaneWithOptions is CreateLane plus the optional creation
// attributes in opts. A zero opts behaves identically to CreateLane.
func (s *Store) CreateLaneWithOptions(ctx context.Context, idempotencyKey, id, orchestrationID, projectID, parentTaskID string, opts LaneOptions) (*protocol.DeliveryLane, error) {
	err := s.db.Write(ctx, idempotencyKey, "create lane "+id, func(tx *sql.Tx) error {
		events, err := loadEventsTx(ctx, tx, orchestrationID)
		if err != nil {
			return err
		}
		orch, err := reduceOrchestration(orchestrationID, events)
		if err != nil {
			return err
		}
		if isTerminal(orch.Status) {
			return ErrInvalidState
		}

		if parentTaskID != "" {
			task, err := reduceParentTask(orchestrationID, parentTaskID, events)
			if err != nil {
				return err
			}
			if task.ProjectId != nil && *task.ProjectId != projectID {
				return ErrScopeMismatch
			}
		}

		var status string
		if err := tx.QueryRowContext(ctx, `SELECT status FROM delivery_projects WHERE id = ?`, projectID).Scan(&status); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ErrNotFound
			}
			return err
		}
		if status != string(protocol.DeliveryProjectStatusActive) {
			return ErrProjectInactive
		}

		payloadMap := map[string]interface{}{"project_id": projectID, "parent_task_id": parentTaskID}
		if sessionID := strings.TrimSpace(opts.SessionID); sessionID != "" {
			payloadMap["session_id"] = sessionID
		}
		payload, err := json.Marshal(payloadMap)
		if err != nil {
			return err
		}
		laneID := id
		return insertEvent(ctx, tx, eventRow{
			ID: newID(), OrchestrationID: orchestrationID, EntityID: &laneID, IdempotencyKey: idempotencyKey,
			Type: string(protocol.DeliveryEventTypeLaneCreated), Payload: string(payload),
			Sequence: len(events), OccurredAt: time.Now().UTC(),
		})
	})
	if errors.Is(err, storage.ErrDuplicateWrite) {
		return s.GetLane(ctx, orchestrationID, id)
	}
	if err != nil {
		return nil, fmt.Errorf("delivery: create lane %s: %w", id, err)
	}
	return s.GetLane(ctx, orchestrationID, id)
}

// GetLane fails closed (ErrNotFound) when laneID does not exist within
// orchestrationID's own event log — a lane id from a different
// orchestration is never visible through this call.
func (s *Store) GetLane(ctx context.Context, orchestrationID, laneID string) (*protocol.DeliveryLane, error) {
	events, err := loadEvents(ctx, s.db.Reader(), orchestrationID)
	if err != nil {
		return nil, err
	}
	return reduceLane(orchestrationID, laneID, events)
}

// ListLanes returns every lane orchestrationID has ever created,
// mirroring ListGraph's shape for tasks/edges - a caller resolving
// "every lane belonging to project X" has no other exported way to
// enumerate lanes across an orchestration, since a lane's own
// project_id is fixed at creation independent of whether it is routed
// through a parent task.
func (s *Store) ListLanes(ctx context.Context, orchestrationID string) ([]*protocol.DeliveryLane, error) {
	events, err := loadEvents(ctx, s.db.Reader(), orchestrationID)
	if err != nil {
		return nil, err
	}
	laneMap, err := allLanes(orchestrationID, events)
	if err != nil {
		return nil, err
	}
	lanes := make([]*protocol.DeliveryLane, 0, len(laneMap))
	for _, l := range laneMap {
		lanes = append(lanes, l)
	}
	return lanes, nil
}

// UpdateLaneStatus appends lane.status_changed after checking
// expectedRevision against the lane's current derived revision.
func (s *Store) UpdateLaneStatus(ctx context.Context, idempotencyKey, orchestrationID, laneID string, expectedRevision int, status protocol.DeliveryLaneStatus) (*protocol.DeliveryLane, error) {
	err := s.db.Write(ctx, idempotencyKey, "update lane "+laneID, func(tx *sql.Tx) error {
		events, err := loadEventsTx(ctx, tx, orchestrationID)
		if err != nil {
			return err
		}
		current, err := reduceLane(orchestrationID, laneID, events)
		if err != nil {
			return err
		}
		if current.Revision != expectedRevision {
			return ErrRevisionConflict
		}
		payload, err := json.Marshal(map[string]interface{}{"status": string(status)})
		if err != nil {
			return err
		}
		return insertEvent(ctx, tx, eventRow{
			ID: newID(), OrchestrationID: orchestrationID, EntityID: &laneID, IdempotencyKey: idempotencyKey,
			Type: string(protocol.DeliveryEventTypeLaneStatusChanged), Payload: string(payload),
			Sequence: len(events), OccurredAt: time.Now().UTC(),
		})
	})
	if errors.Is(err, storage.ErrDuplicateWrite) {
		return s.GetLane(ctx, orchestrationID, laneID)
	}
	if err != nil {
		return nil, err
	}
	return s.GetLane(ctx, orchestrationID, laneID)
}
