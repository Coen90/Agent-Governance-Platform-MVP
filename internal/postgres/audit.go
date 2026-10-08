package postgres

import (
	"context"
	"database/sql"
)

func record(ctx context.Context, tx *sql.Tx, id, event, actor string) error {
	_, err := tx.ExecContext(ctx, "INSERT INTO audit (request_id, event, actor) VALUES ($1, $2, $3)", id, event, actor)
	return err
}
