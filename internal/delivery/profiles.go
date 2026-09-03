package delivery

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/ygrip/punakawan/internal/storage"
	"github.com/ygrip/punakawan/pkg/protocol"
)

// ProfileInput is the settable content of a ProjectDeliveryProfile.
// Explicit repository configuration a caller supplies here takes
// precedence over any global detected/learned default; merging those
// is the caller's job before calling SetDeliveryProfile.
type ProfileInput struct {
	LocalPath            string
	CanonicalRemote      string
	BaseBranch           string
	Provider             string
	BuildCommand         string
	TestCommand          string
	RequiredExecutables  []string
	RequiredServices     []string
	QualityRules         []string
	CIAdapter            string
	VerificationGates    []string
	MaxConcurrentWorkers int
}

// SetDeliveryProfile creates or wholesale-replaces the one profile for
// projectID, incrementing its revision. A profile is plain configuration
// state, not event-sourced: there is exactly one current profile per
// project, not a history of profile revisions to replay.
func (s *Store) SetDeliveryProfile(ctx context.Context, idempotencyKey, id, projectID string, in ProfileInput) (*protocol.ProjectDeliveryProfile, error) {
	if in.BaseBranch == "" {
		return nil, fmt.Errorf("delivery: profile requires base_branch")
	}
	execs, err := json.Marshal(nonNil(in.RequiredExecutables))
	if err != nil {
		return nil, err
	}
	services, err := json.Marshal(nonNil(in.RequiredServices))
	if err != nil {
		return nil, err
	}
	quality, err := json.Marshal(nonNil(in.QualityRules))
	if err != nil {
		return nil, err
	}
	gates, err := json.Marshal(nonNil(in.VerificationGates))
	if err != nil {
		return nil, err
	}

	err = s.db.Write(ctx, idempotencyKey, "set profile for project "+projectID, func(tx *sql.Tx) error {
		var status string
		if err := tx.QueryRowContext(ctx, `SELECT status FROM delivery_projects WHERE id = ?`, projectID).Scan(&status); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ErrNotFound
			}
			return err
		}
		_, err := tx.ExecContext(ctx, `
			INSERT INTO delivery_profiles (id, project_id, local_path, canonical_remote, base_branch, provider, build_command, test_command, required_executables, required_services, quality_rules, ci_adapter, verification_gates, max_concurrent_workers, revision)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 0)
			ON CONFLICT(project_id) DO UPDATE SET
				local_path = excluded.local_path, canonical_remote = excluded.canonical_remote, base_branch = excluded.base_branch,
				provider = excluded.provider, build_command = excluded.build_command, test_command = excluded.test_command,
				required_executables = excluded.required_executables, required_services = excluded.required_services,
				quality_rules = excluded.quality_rules, ci_adapter = excluded.ci_adapter, verification_gates = excluded.verification_gates,
				max_concurrent_workers = excluded.max_concurrent_workers, revision = revision + 1`,
			id, projectID, in.LocalPath, in.CanonicalRemote, in.BaseBranch, in.Provider, in.BuildCommand, in.TestCommand,
			string(execs), string(services), string(quality), in.CIAdapter, string(gates), nullableInt(in.MaxConcurrentWorkers),
		)
		return err
	})
	if err != nil && !errors.Is(err, storage.ErrDuplicateWrite) {
		return nil, err
	}
	return s.GetDeliveryProfile(ctx, projectID)
}

// GetDeliveryProfile fails closed (ErrNotFound) when projectID has no
// profile yet.
func (s *Store) GetDeliveryProfile(ctx context.Context, projectID string) (*protocol.ProjectDeliveryProfile, error) {
	row := s.db.Reader().QueryRowContext(ctx, `
		SELECT id, project_id, local_path, canonical_remote, base_branch, provider, build_command, test_command,
		       required_executables, required_services, quality_rules, ci_adapter, verification_gates, max_concurrent_workers, revision
		FROM delivery_profiles WHERE project_id = ?`, projectID)

	var p protocol.ProjectDeliveryProfile
	var localPath, remote, provider, build, test, execs, services, quality, ci, gates string
	var maxWorkers sql.NullInt64
	if err := row.Scan(&p.Id, &p.ProjectId, &localPath, &remote, &p.BaseBranch, &provider, &build, &test,
		&execs, &services, &quality, &ci, &gates, &maxWorkers, &p.Revision); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("delivery: get profile for project %s: %w", projectID, err)
	}
	if localPath != "" {
		p.LocalPath = &localPath
	}
	if remote != "" {
		p.CanonicalRemote = &remote
	}
	if provider != "" {
		v := protocol.ProjectDeliveryProfileProvider(provider)
		p.Provider = &v
	}
	if build != "" {
		p.BuildCommand = &build
	}
	if test != "" {
		p.TestCommand = &test
	}
	if ci != "" {
		p.CiAdapter = &ci
	}
	if err := json.Unmarshal([]byte(execs), &p.RequiredExecutables); err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(services), &p.RequiredServices); err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(quality), &p.QualityRules); err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(gates), &p.VerificationGates); err != nil {
		return nil, err
	}
	if maxWorkers.Valid {
		v := int(maxWorkers.Int64)
		p.MaxConcurrentWorkers = &v
	}
	return &p, nil
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

func nullableInt(v int) interface{} {
	if v <= 0 {
		return nil
	}
	return v
}

// RememberProjectCheckout records where this project is checked out,
// leaving every other setting on its profile as it was.
//
// SetDeliveryProfile replaces a profile wholesale, which is right for a
// caller stating a project's whole delivery configuration and wrong for
// one that has just learned a path. Nothing wrote local_path at all before
// this - it was a field with no writer - which is why a delivery could
// only ever work against the directory its MCP server happened to be
// started in.
func (s *Store) RememberProjectCheckout(ctx context.Context, idempotencyKey, projectID, localPath, canonicalRemote, baseBranch string) error {
	in := ProfileInput{LocalPath: localPath, CanonicalRemote: canonicalRemote, BaseBranch: baseBranch}
	id := stableProfileID(projectID)
	existing, err := s.GetDeliveryProfile(ctx, projectID)
	switch {
	case err == nil:
		id = existing.Id
		in = profileInputFrom(existing)
		in.LocalPath = localPath
		if canonicalRemote != "" {
			in.CanonicalRemote = canonicalRemote
		}
		if in.BaseBranch == "" {
			in.BaseBranch = baseBranch
		}
	case errors.Is(err, ErrNotFound):
	default:
		return err
	}
	if in.BaseBranch == "" {
		return fmt.Errorf("delivery: remember checkout for project %s: no base branch is known for it", projectID)
	}
	if _, err := s.SetDeliveryProfile(ctx, idempotencyKey, id, projectID, in); err != nil {
		return err
	}
	return nil
}

// stableProfileID derives the id of a project's one profile, so a caller
// that has never seen the row does not have to invent one.
func stableProfileID(projectID string) string {
	return "profile-" + projectID
}

// profileInputFrom is the settable half of an existing profile, so a
// caller changing one field keeps the rest.
func profileInputFrom(p *protocol.ProjectDeliveryProfile) ProfileInput {
	in := ProfileInput{
		BaseBranch:          p.BaseBranch,
		RequiredExecutables: p.RequiredExecutables,
		RequiredServices:    p.RequiredServices,
		QualityRules:        p.QualityRules,
		VerificationGates:   p.VerificationGates,
	}
	if p.LocalPath != nil {
		in.LocalPath = *p.LocalPath
	}
	if p.CanonicalRemote != nil {
		in.CanonicalRemote = *p.CanonicalRemote
	}
	if p.Provider != nil {
		in.Provider = string(*p.Provider)
	}
	if p.BuildCommand != nil {
		in.BuildCommand = *p.BuildCommand
	}
	if p.TestCommand != nil {
		in.TestCommand = *p.TestCommand
	}
	if p.CiAdapter != nil {
		in.CIAdapter = *p.CiAdapter
	}
	if p.MaxConcurrentWorkers != nil {
		in.MaxConcurrentWorkers = *p.MaxConcurrentWorkers
	}
	return in
}
