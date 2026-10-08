package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"agent-gateway-mvp/internal/governance"
)

// The mock tool's effect stays in the request transaction for atomic execution.
func executeTool(ctx context.Context, tx *sql.Tx, action, service string) (json.RawMessage, error) {
	var count int
	if action == governance.ActionRestart {
		if err := tx.QueryRowContext(ctx, `UPDATE demo_services SET restart_count = restart_count + 1 WHERE name = $1 RETURNING restart_count`, service).Scan(&count); err != nil {
			return nil, err
		}
		return json.Marshal(map[string]any{"simulated": true, "restart_count": count})
	}
	if err := tx.QueryRowContext(ctx, "SELECT restart_count FROM demo_services WHERE name = $1", service).Scan(&count); err != nil {
		return nil, err
	}
	return json.Marshal(map[string]any{"simulated": true, "logs": []string{fmt.Sprintf("payments healthy; restart_count=%d", count)}})
}
