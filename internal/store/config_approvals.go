package store

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/magnify-labs/otel-magnify/pkg/models"
)

// CreateOrUpdateConfigApprovalRequest creates a new pending approval request, or updates the
// existing pending request for the same workload and target group.
func (d *DB) CreateOrUpdateConfigApprovalRequest(req models.ConfigApprovalRequest) (models.ConfigApprovalRequest, error) {
	now := time.Now().UTC()
	if req.ID == "" {
		if existing, err := d.getPendingConfigApprovalRequest(req.WorkloadID, req.TargetGroup); err == nil && existing != nil {
			req.ID = existing.ID
		} else if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return models.ConfigApprovalRequest{}, err
		}
	}
	if req.ID == "" {
		id, err := newConfigApprovalID()
		if err != nil {
			return models.ConfigApprovalRequest{}, err
		}
		req.ID = id
	}
	if req.Status == "" {
		req.Status = models.ConfigApprovalStatusPending
	}
	if req.CreatedAt.IsZero() {
		req.CreatedAt = now
	}
	req.UpdatedAt = now

	_, err := d.Exec(`
		INSERT INTO workload_config_approvals (
			id, workload_id, draft_yaml, target_group, target_env, requester, request_comment,
			approver, approval_comment, status, approved_by, approved_at, push_comment,
			prod_target, prod_confirmation, prod_double_confirmed, break_glass, break_glass_reason,
			config_hash, created_at, updated_at, pushed_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			draft_yaml = excluded.draft_yaml,
			target_env = excluded.target_env,
			requester = excluded.requester,
			request_comment = excluded.request_comment,
			prod_target = excluded.prod_target,
			prod_confirmation = excluded.prod_confirmation,
			updated_at = excluded.updated_at
	`, req.ID, req.WorkloadID, req.DraftYAML, req.TargetGroup, req.TargetEnv, req.Requester, req.RequestComment,
		req.Approver, req.ApprovalComment, req.Status, req.ApprovedBy, req.ApprovedAt, req.PushComment,
		req.ProdTarget, req.ProdConfirmation, req.ProdDoubleConfirmed, req.BreakGlass, req.BreakGlassReason,
		req.ConfigHash, req.CreatedAt, req.UpdatedAt, req.PushedAt)
	if err != nil {
		return models.ConfigApprovalRequest{}, err
	}
	return d.GetConfigApprovalRequest(req.ID)
}

// GetConfigApprovalRequest returns a config approval request by ID.
func (d *DB) GetConfigApprovalRequest(id string) (models.ConfigApprovalRequest, error) {
	return d.getConfigApprovalRequest(`
		SELECT id, workload_id, draft_yaml, target_group, target_env, requester, request_comment,
		       approver, approval_comment, status, approved_by, approved_at, push_comment,
		       prod_target, prod_confirmation, prod_double_confirmed, break_glass, break_glass_reason,
		       config_hash, created_at, updated_at, pushed_at
		FROM workload_config_approvals WHERE id = ?`, id)
}

// ListConfigApprovalRequests returns all config approval requests for a workload, newest first.
func (d *DB) ListConfigApprovalRequests(workloadID string) ([]models.ConfigApprovalRequest, error) {
	rows, err := d.Query(`
		SELECT id, workload_id, draft_yaml, target_group, target_env, requester, request_comment,
		       approver, approval_comment, status, approved_by, approved_at, push_comment,
		       prod_target, prod_confirmation, prod_double_confirmed, break_glass, break_glass_reason,
		       config_hash, created_at, updated_at, pushed_at
		FROM workload_config_approvals WHERE workload_id = ? ORDER BY updated_at DESC`, workloadID)
	if err != nil {
		return nil, err
	}
	//nolint:errcheck // deferred cleanup of read-only result rows
	defer rows.Close()
	var out []models.ConfigApprovalRequest
	for rows.Next() {
		req, err := scanConfigApprovalRequest(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, req)
	}
	return out, rows.Err()
}

// ApproveConfigApprovalRequest moves a pending approval request into the approved state.
func (d *DB) ApproveConfigApprovalRequest(id, approvedBy, comment string, approvedAt time.Time) (models.ConfigApprovalRequest, error) {
	approvedAt = approvedAt.UTC()
	res, err := d.Exec(`
		UPDATE workload_config_approvals
		SET status = ?, approver = ?, approval_comment = ?, approved_by = ?, approved_at = ?, updated_at = ?
		WHERE id = ? AND status = ?`, models.ConfigApprovalStatusApproved, approvedBy, comment, approvedBy, approvedAt, approvedAt, id, models.ConfigApprovalStatusPending)
	if err != nil {
		return models.ConfigApprovalRequest{}, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return models.ConfigApprovalRequest{}, sql.ErrNoRows
	}
	return d.GetConfigApprovalRequest(id)
}

// MarkConfigApprovalRequestPushed records the push outcome for an approved or break-glass request.
func (d *DB) MarkConfigApprovalRequestPushed(id, configHash, pushComment string, prodDoubleConfirmed, breakGlass bool, breakGlassReason string, pushedAt time.Time) (models.ConfigApprovalRequest, error) {
	pushedAt = pushedAt.UTC()
	var reason *string
	if breakGlassReason != "" {
		reason = &breakGlassReason
	}
	res, err := d.Exec(`
		UPDATE workload_config_approvals
		SET status = ?, push_comment = ?, prod_double_confirmed = ?, break_glass = ?, break_glass_reason = ?, config_hash = ?, pushed_at = ?, updated_at = ?
		WHERE id = ? AND status IN (?, ?)`, models.ConfigApprovalStatusPushed, pushComment, prodDoubleConfirmed, breakGlass, reason, configHash, pushedAt, pushedAt, id, models.ConfigApprovalStatusApproved, models.ConfigApprovalStatusPending)
	if err != nil {
		return models.ConfigApprovalRequest{}, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return models.ConfigApprovalRequest{}, sql.ErrNoRows
	}
	return d.GetConfigApprovalRequest(id)
}

func (d *DB) getPendingConfigApprovalRequest(workloadID, targetGroup string) (*models.ConfigApprovalRequest, error) {
	req, err := d.getConfigApprovalRequest(`
		SELECT id, workload_id, draft_yaml, target_group, target_env, requester, request_comment,
		       approver, approval_comment, status, approved_by, approved_at, push_comment,
		       prod_target, prod_confirmation, prod_double_confirmed, break_glass, break_glass_reason,
		       config_hash, created_at, updated_at, pushed_at
		FROM workload_config_approvals WHERE workload_id = ? AND target_group = ? AND status = ?`, workloadID, targetGroup, models.ConfigApprovalStatusPending)
	if err != nil {
		return nil, err
	}
	return &req, nil
}

func (d *DB) getConfigApprovalRequest(query string, args ...any) (models.ConfigApprovalRequest, error) {
	return scanConfigApprovalRequest(d.QueryRow(query, args...))
}

type configApprovalScanner interface{ Scan(dest ...any) error }

func scanConfigApprovalRequest(row configApprovalScanner) (models.ConfigApprovalRequest, error) {
	var req models.ConfigApprovalRequest
	if err := row.Scan(&req.ID, &req.WorkloadID, &req.DraftYAML, &req.TargetGroup, &req.TargetEnv, &req.Requester, &req.RequestComment,
		&req.Approver, &req.ApprovalComment, &req.Status, &req.ApprovedBy, &req.ApprovedAt, &req.PushComment,
		&req.ProdTarget, &req.ProdConfirmation, &req.ProdDoubleConfirmed, &req.BreakGlass, &req.BreakGlassReason,
		&req.ConfigHash, &req.CreatedAt, &req.UpdatedAt, &req.PushedAt); err != nil {
		return req, err
	}
	return req, nil
}

func newConfigApprovalID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("generate config approval id: %w", err)
	}
	return "car_" + hex.EncodeToString(b[:]), nil
}
