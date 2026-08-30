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
)

type GitHubPRReview struct {
	ID                  string           `json:"id"`
	Repository          string           `json:"repository"`
	PullRequestNumber   int              `json:"pull_request_number"`
	HeadSHA             string           `json:"head_sha"`
	Findings            []map[string]any `json:"findings"`
	Body                string           `json:"body"`
	Verdict             string           `json:"verdict"`
	Status              string           `json:"status"`
	DeliveryExecutionID string           `json:"delivery_execution_id,omitempty"`
	ExternalReviewID    string           `json:"external_review_id,omitempty"`
	Failure             string           `json:"failure,omitempty"`
	CreatedAt           time.Time        `json:"created_at"`
	UpdatedAt           time.Time        `json:"updated_at"`
}

const githubPRReviewSelect = `
	SELECT id, repository, pull_request_number, head_sha, findings_json, body, verdict, status,
		delivery_execution_id, external_review_id, failure, created_at, updated_at
	FROM github_pr_reviews`

func (s *Store) ProposeGitHubPRReview(ctx context.Context, key, repository string, number int, headSHA string, findings []map[string]any, body, verdict, executionID string) (*GitHubPRReview, error) {
	key = strings.TrimSpace(key)
	repository = strings.TrimSpace(repository)
	headSHA = strings.TrimSpace(headSHA)
	body = strings.TrimSpace(body)
	verdict = strings.TrimSpace(verdict)
	executionID = strings.TrimSpace(executionID)
	if key == "" || repository == "" || number <= 0 || headSHA == "" || body == "" || (verdict != "APPROVE" && verdict != "REQUEST_CHANGES" && verdict != "COMMENT") {
		return nil, fmt.Errorf("delivery: invalid GitHub PR review proposal")
	}
	encoded, err := json.Marshal(findings)
	if err != nil {
		return nil, fmt.Errorf("delivery: encode GitHub PR review findings: %w", err)
	}
	now := time.Now().UTC()
	out := &GitHubPRReview{
		ID:                  newID(),
		Repository:          repository,
		PullRequestNumber:   number,
		HeadSHA:             headSHA,
		Findings:            findings,
		Body:                body,
		Verdict:             verdict,
		Status:              "proposed",
		DeliveryExecutionID: executionID,
		CreatedAt:           now,
		UpdatedAt:           now,
	}
	err = s.db.Write(ctx, key, "propose GitHub PR review "+repository, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `
			INSERT INTO github_pr_reviews (
				id, repository, pull_request_number, head_sha, findings_json, body, verdict, status,
				delivery_execution_id, external_review_id, created_at, updated_at
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, '', ?, ?)`,
			out.ID, out.Repository, out.PullRequestNumber, out.HeadSHA, string(encoded), out.Body, out.Verdict,
			out.Status, out.DeliveryExecutionID, out.CreatedAt.Format(timeLayout), out.UpdatedAt.Format(timeLayout))
		return err
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// GetGitHubPRReview returns the persisted proposal and its terminal submission
// result, if any.
func (s *Store) GetGitHubPRReview(ctx context.Context, id string) (*GitHubPRReview, error) {
	return scanGitHubPRReview(s.db.Reader().QueryRowContext(ctx, githubPRReviewSelect+` WHERE id = ?`, strings.TrimSpace(id)))
}

// ResolveGitHubPRReview records the result of its one external submission.
// A non-empty failure produces the terminal failed state; otherwise the
// adapter's external review id is required for a submitted state.
func (s *Store) ResolveGitHubPRReview(ctx context.Context, key, id, externalReviewID, failure string) (*GitHubPRReview, error) {
	key = strings.TrimSpace(key)
	id = strings.TrimSpace(id)
	externalReviewID = strings.TrimSpace(externalReviewID)
	failure = strings.TrimSpace(failure)
	if key == "" || id == "" || (failure == "" && externalReviewID == "") {
		return nil, fmt.Errorf("delivery: invalid GitHub PR review resolution")
	}
	status := "submitted"
	if failure != "" {
		status = "failed"
	}
	err := s.db.Write(ctx, key, "resolve GitHub PR review "+id, func(tx *sql.Tx) error {
		result, err := tx.ExecContext(ctx, `
			UPDATE github_pr_reviews
			SET status = ?, external_review_id = ?, failure = ?, updated_at = ?
			WHERE id = ? AND status = 'proposed'`,
			status, externalReviewID, failure, time.Now().UTC().Format(timeLayout), id)
		if err != nil {
			return err
		}
		changed, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if changed == 1 {
			return nil
		}
		var currentStatus string
		if err := tx.QueryRowContext(ctx, `SELECT status FROM github_pr_reviews WHERE id = ?`, id).Scan(&currentStatus); err != nil {
			return noRow(err)
		}
		if currentStatus == "submitted" || currentStatus == "failed" {
			return nil
		}
		return ErrInvalidState
	})
	if err != nil && !errors.Is(err, storage.ErrDuplicateWrite) {
		return nil, err
	}
	return s.GetGitHubPRReview(ctx, id)
}

func scanGitHubPRReview(row lifecycleScanner) (*GitHubPRReview, error) {
	var out GitHubPRReview
	var findings, createdAt, updatedAt string
	if err := row.Scan(
		&out.ID, &out.Repository, &out.PullRequestNumber, &out.HeadSHA, &findings, &out.Body, &out.Verdict,
		&out.Status, &out.DeliveryExecutionID, &out.ExternalReviewID, &out.Failure, &createdAt, &updatedAt,
	); err != nil {
		return nil, noRow(err)
	}
	if err := json.Unmarshal([]byte(findings), &out.Findings); err != nil {
		return nil, fmt.Errorf("delivery: decode GitHub PR review %s findings: %w", out.ID, err)
	}
	var err error
	if out.CreatedAt, err = scanTime(createdAt); err != nil {
		return nil, fmt.Errorf("delivery: parse GitHub PR review %s created_at: %w", out.ID, err)
	}
	if out.UpdatedAt, err = scanTime(updatedAt); err != nil {
		return nil, fmt.Errorf("delivery: parse GitHub PR review %s updated_at: %w", out.ID, err)
	}
	return &out, nil
}
