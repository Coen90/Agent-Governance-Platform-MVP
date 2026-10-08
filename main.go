package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	_ "github.com/lib/pq"
)

//go:embed schema.sql
var schema string

type app struct {
	db                     *sql.DB
	agentToken, adminToken string
}

type request struct {
	ID      string          `json:"id"`
	AgentID string          `json:"agent_id"`
	UserID  string          `json:"user_id"`
	Action  string          `json:"action"`
	Service string          `json:"service"`
	Status  string          `json:"status"`
	Result  json.RawMessage `json:"result"`
}

const columns = "id, agent_id, user_id, action, service, status, result"

func scan(row *sql.Row) (request, error) {
	var v request
	err := row.Scan(&v.ID, &v.AgentID, &v.UserID, &v.Action, &v.Service, &v.Status, &v.Result)
	return v, err
}

func main() {
	a := &app{agentToken: os.Getenv("AGENT_TOKEN"), adminToken: os.Getenv("ADMIN_TOKEN")}
	if len(a.agentToken) < 16 || len(a.adminToken) < 16 || a.agentToken == a.adminToken {
		log.Fatal("set different AGENT_TOKEN and ADMIN_TOKEN values (at least 16 characters)")
	}
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		log.Fatal("DATABASE_URL is required")
	}
	var err error
	a.db, err = sql.Open("postgres", dsn)
	if err != nil {
		log.Fatal("invalid database configuration")
	}
	defer a.db.Close()
	a.db.SetMaxOpenConns(10)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	err = initialize(ctx, a.db)
	cancel()
	if err != nil {
		log.Fatal("database initialization failed: ", err)
	}
	addr := os.Getenv("LISTEN_ADDR")
	if addr == "" {
		addr = "127.0.0.1:8080"
	}
	server := &http.Server{Addr: addr, Handler: a.handler(), ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout: 10 * time.Second, WriteTimeout: 15 * time.Second, IdleTimeout: 60 * time.Second}
	log.Printf("agent gateway listening on %s", addr)
	log.Fatal(server.ListenAndServe())
}

func initialize(ctx context.Context, db *sql.DB) error {
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

func (a *app) handler() http.Handler {
	m := http.NewServeMux()
	m.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		if err := a.db.PingContext(r.Context()); err != nil {
			respond(w, 503, map[string]string{"error": "database unavailable"})
			return
		}
		respond(w, 200, map[string]string{"status": "ok"})
	})
	m.HandleFunc("POST /requests", a.authorize(false, a.create))
	m.HandleFunc("GET /requests/{id}", a.authorize(false, a.get))
	m.HandleFunc("POST /requests/{id}/approve", a.authorize(true, a.approve))
	m.HandleFunc("POST /requests/{id}/execute", a.authorize(false, a.execute))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()
		m.ServeHTTP(w, r.WithContext(ctx))
	})
}

func matches(got, want string) bool {
	g, w := sha256.Sum256([]byte(got)), sha256.Sum256([]byte(want))
	return subtle.ConstantTimeCompare(g[:], w[:]) == 1
}

func (a *app) authorize(admin bool, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		want := a.agentToken
		if admin {
			want = a.adminToken
		}
		token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !ok || want == "" || !matches(token, want) {
			respond(w, 401, map[string]string{"error": "invalid credentials"})
			return
		}
		next(w, r)
	}
}

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

func (a *app) create(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Action  string `json:"action"`
		Service string `json:"service"`
	}
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
	d.DisallowUnknownFields()
	if err := d.Decode(&body); err != nil {
		respond(w, 400, map[string]string{"error": "expected action and service only"})
		return
	}
	if err := d.Decode(new(any)); err != io.EOF {
		respond(w, 400, map[string]string{"error": "expected one JSON object"})
		return
	}
	key := r.Header.Get("Idempotency-Key")
	if len(key) == 0 || len(key) > 128 {
		respond(w, 400, map[string]string{"error": "Idempotency-Key required (1–128 bytes)"})
		return
	}
	if body.Action != "logs.read" && body.Action != "service.restart" {
		respond(w, 403, map[string]string{"error": "action not allowed"})
		return
	}
	tx, err := a.db.BeginTx(r.Context(), nil)
	if a.dbError(w, err) {
		return
	}
	defer tx.Rollback()
	user, ok, err := allowed(r.Context(), tx, body.Action, body.Service)
	if a.dbError(w, err) {
		return
	}
	if !ok {
		respond(w, 403, map[string]string{"error": "outside active delegation"})
		return
	}
	status := "approved"
	if body.Action == "service.restart" {
		status = "pending"
	}
	res, err := tx.ExecContext(r.Context(), `INSERT INTO requests
		(id, agent_id, user_id, idempotency_key, action, service, status)
		VALUES ($1, 'demo-agent', $2, $3, $4, $5, $6)
		ON CONFLICT (agent_id, idempotency_key) DO NOTHING`, rand.Text(), user, key, body.Action, body.Service, status)
	if a.dbError(w, err) {
		return
	}
	v, err := scan(tx.QueryRowContext(r.Context(), "SELECT "+columns+" FROM requests WHERE agent_id = 'demo-agent' AND idempotency_key = $1", key))
	if a.dbError(w, err) {
		return
	}
	if v.Action != body.Action || v.Service != body.Service || v.UserID != user {
		respond(w, 409, map[string]string{"error": "key already used for a different request"})
		return
	}
	n, err := res.RowsAffected()
	if a.dbError(w, err) {
		return
	}
	if n == 1 && a.dbError(w, record(r.Context(), tx, v.ID, "requested", "demo-agent")) {
		return
	}
	if a.dbError(w, tx.Commit()) {
		return
	}
	code := 200
	if n == 1 {
		code = 201
	}
	respond(w, code, v)
}

func (a *app) get(w http.ResponseWriter, r *http.Request) {
	v, err := scan(a.db.QueryRowContext(r.Context(), "SELECT "+columns+" FROM requests WHERE id = $1 AND agent_id = 'demo-agent'", r.PathValue("id")))
	if a.requestError(w, err) {
		return
	}
	respond(w, 200, v)
}

func (a *app) approve(w http.ResponseWriter, r *http.Request) {
	a.transition(w, r, true)
}

func (a *app) execute(w http.ResponseWriter, r *http.Request) {
	a.transition(w, r, false)
}

func (a *app) transition(w http.ResponseWriter, r *http.Request, approval bool) {
	tx, err := a.db.BeginTx(r.Context(), nil)
	if a.dbError(w, err) {
		return
	}
	defer tx.Rollback()
	v, err := scan(tx.QueryRowContext(r.Context(), "SELECT "+columns+" FROM requests WHERE id = $1 AND agent_id = 'demo-agent' FOR UPDATE", r.PathValue("id")))
	if a.requestError(w, err) {
		return
	}
	user, ok, err := allowed(r.Context(), tx, v.Action, v.Service)
	if a.dbError(w, err) {
		return
	}
	if !ok || user != v.UserID {
		respond(w, 403, map[string]string{"error": "delegation revoked, expired, or changed"})
		return
	}
	if approval && v.Status == "pending" {
		v.Status = "approved"
		err = record(r.Context(), tx, v.ID, "approved", "demo-admin")
	} else if !approval && v.Status == "pending" {
		respond(w, 409, map[string]string{"error": "human approval required"})
		return
	} else if !approval && v.Status == "approved" {
		var count int
		if v.Action == "service.restart" {
			err = tx.QueryRowContext(r.Context(), `UPDATE demo_services SET restart_count = restart_count + 1 WHERE name = $1 RETURNING restart_count`, v.Service).Scan(&count)
			v.Result, _ = json.Marshal(map[string]any{"simulated": true, "restart_count": count})
		} else {
			err = tx.QueryRowContext(r.Context(), "SELECT restart_count FROM demo_services WHERE name = $1", v.Service).Scan(&count)
			v.Result, _ = json.Marshal(map[string]any{"simulated": true, "logs": []string{fmt.Sprintf("payments healthy; restart_count=%d", count)}})
		}
		if a.dbError(w, err) {
			return
		}
		v.Status = "succeeded"
		err = record(r.Context(), tx, v.ID, "executed", "demo-agent")
	}
	if a.dbError(w, err) {
		return
	}
	_, err = tx.ExecContext(r.Context(), "UPDATE requests SET status = $2, result = $3::jsonb WHERE id = $1", v.ID, v.Status, string(v.Result))
	if a.dbError(w, err) || a.dbError(w, tx.Commit()) {
		return
	}
	respond(w, 200, v)
}

func record(ctx context.Context, tx *sql.Tx, id, event, actor string) error {
	_, err := tx.ExecContext(ctx, "INSERT INTO audit (request_id, event, actor) VALUES ($1, $2, $3)", id, event, actor)
	return err
}

func (a *app) requestError(w http.ResponseWriter, err error) bool {
	if errors.Is(err, sql.ErrNoRows) {
		respond(w, 404, map[string]string{"error": "request not found"})
		return true
	}
	return a.dbError(w, err)
}

func (a *app) dbError(w http.ResponseWriter, err error) bool {
	if err == nil {
		return false
	}
	log.Printf("database operation failed: %v", err)
	respond(w, 503, map[string]string{"error": "storage unavailable; retry with the same idempotency key"})
	return true
}

func respond(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}
