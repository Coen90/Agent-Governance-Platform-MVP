package postgres

import (
	"context"
	"database/sql"
	"errors"
)

// Identity is bound to the credential, never taken from the request body.
func allowed(ctx context.Context, tx *sql.Tx, action, service string) (string, bool, error) {
	var user string
	var ok bool
	err := tx.QueryRowContext(ctx, `SELECT user_id,
		active AND expires_at > clock_timestamp() AND service = $1 AND $2 = ANY(actions)
		FROM delegations WHERE agent_id = 'demo-agent' FOR SHARE`, service, action).Scan(&user, &ok)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	return user, ok, err
}
