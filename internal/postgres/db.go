package postgres

import (
	"context"
	"database/sql"
	_ "embed"

	_ "github.com/lib/pq"
)

//go:embed schema.sql
var schema string

type Store struct {
	db *sql.DB
}

func New(db *sql.DB) *Store {
	return &Store{db: db}
}

func Open(ctx context.Context, dsn string) (*sql.DB, error) {
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(10)
	if err := Initialize(ctx, db); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

func Initialize(ctx context.Context, db *sql.DB) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	// Serialize schema creation when multiple gateways start together.
	if _, err = tx.ExecContext(ctx, "SELECT pg_advisory_xact_lock(8264108)"); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, schema); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) Ping(ctx context.Context) error {
	return s.db.PingContext(ctx)
}
