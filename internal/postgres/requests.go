package postgres

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"

	"agent-gateway-mvp/internal/governance"
)

const columns = "id, agent_id, user_id, action, service, status, result"

func scan(row *sql.Row) (governance.Request, error) {
	var v governance.Request
	err := row.Scan(&v.ID, &v.AgentID, &v.UserID, &v.Action, &v.Service, &v.Status, &v.Result)
	if errors.Is(err, sql.ErrNoRows) {
		return v, governance.ErrNotFound
	}
	return v, err
}

func (s *Store) Create(ctx context.Context, input governance.Input, key string) (governance.Request, bool, error) {
	var zero governance.Request
	if input.Action != governance.ActionReadLogs && input.Action != governance.ActionRestart {
		return zero, false, governance.ErrActionNotAllowed
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return zero, false, err
	}
	defer tx.Rollback()
	user, ok, err := allowed(ctx, tx, input.Action, input.Service)
	if err != nil {
		return zero, false, err
	}
	if !ok {
		return zero, false, governance.ErrOutsideScope
	}
	status := governance.StatusApproved
	if input.Action == governance.ActionRestart {
		status = governance.StatusPending
	}
	res, err := tx.ExecContext(ctx, `INSERT INTO requests
		(id, agent_id, user_id, idempotency_key, action, service, status)
		VALUES ($1, 'demo-agent', $2, $3, $4, $5, $6)
		ON CONFLICT (agent_id, idempotency_key) DO NOTHING`, rand.Text(), user, key, input.Action, input.Service, status)
	if err != nil {
		return zero, false, err
	}
	v, err := scan(tx.QueryRowContext(ctx, "SELECT "+columns+" FROM requests WHERE agent_id = 'demo-agent' AND idempotency_key = $1", key))
	if err != nil {
		return zero, false, err
	}
	if v.Action != input.Action || v.Service != input.Service || v.UserID != user {
		return zero, false, governance.ErrKeyConflict
	}
	n, err := res.RowsAffected()
	if err != nil {
		return zero, false, err
	}
	if n == 1 {
		if err := record(ctx, tx, v.ID, "requested", "demo-agent"); err != nil {
			return zero, false, err
		}
	}
	if err := tx.Commit(); err != nil {
		return zero, false, err
	}
	return v, n == 1, nil
}

func (s *Store) Get(ctx context.Context, id string) (governance.Request, error) {
	return scan(s.db.QueryRowContext(ctx, "SELECT "+columns+" FROM requests WHERE id = $1 AND agent_id = 'demo-agent'", id))
}

func (s *Store) Approve(ctx context.Context, id string) (governance.Request, error) {
	return s.transition(ctx, id, true)
}

func (s *Store) Execute(ctx context.Context, id string) (governance.Request, error) {
	return s.transition(ctx, id, false)
}

func (s *Store) transition(ctx context.Context, id string, approval bool) (governance.Request, error) {
	var zero governance.Request
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return zero, err
	}
	defer tx.Rollback()
	v, err := scan(tx.QueryRowContext(ctx, "SELECT "+columns+" FROM requests WHERE id = $1 AND agent_id = 'demo-agent' FOR UPDATE", id))
	if err != nil {
		return zero, err
	}
	user, ok, err := allowed(ctx, tx, v.Action, v.Service)
	if err != nil {
		return zero, err
	}
	if !ok || user != v.UserID {
		return zero, governance.ErrDelegationChanged
	}
	if approval && v.Status == governance.StatusPending {
		v.Status = governance.StatusApproved
		err = record(ctx, tx, v.ID, "approved", "demo-admin")
	} else if !approval && v.Status == governance.StatusPending {
		return zero, governance.ErrApprovalRequired
	} else if !approval && v.Status == governance.StatusApproved {
		v.Result, err = executeTool(ctx, tx, v.Action, v.Service)
		if err != nil {
			return zero, err
		}
		v.Status = governance.StatusSucceeded
		err = record(ctx, tx, v.ID, "executed", "demo-agent")
	}
	if err != nil {
		return zero, err
	}
	_, err = tx.ExecContext(ctx, "UPDATE requests SET status = $2, result = $3::jsonb WHERE id = $1", v.ID, v.Status, string(v.Result))
	if err != nil {
		return zero, err
	}
	if err := tx.Commit(); err != nil {
		return zero, err
	}
	return v, nil
}
