package httpapi_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"

	"agent-gateway-mvp/internal/governance"
	"agent-gateway-mvp/internal/httpapi"
	"agent-gateway-mvp/internal/postgres"
)

func TestGatewayAcrossInstances(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set TEST_DATABASE_URL to run PostgreSQL integration tests")
	}
	root, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	name := "gateway_test_" + strings.ToLower(rand.Text())
	if _, err := root.Exec("CREATE SCHEMA " + name); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if _, err := root.Exec("DROP SCHEMA " + name + " CASCADE"); err != nil {
			t.Error(err)
		}
	}()
	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	q.Set("search_path", name)
	u.RawQuery = q.Encode()
	db, err := sql.Open("postgres", u.String())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db2, err := sql.Open("postgres", u.String())
	if err != nil {
		t.Fatal(err)
	}
	defer db2.Close()
	if err := postgres.Initialize(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	agentToken, adminToken := "test-agent-token-123", "test-admin-token-123"
	sa := httptest.NewServer(httpapi.NewHandler(postgres.New(db), agentToken, adminToken))
	sb := httptest.NewServer(httpapi.NewHandler(postgres.New(db2), agentToken, adminToken))
	defer sa.Close()
	defer sb.Close()

	call := func(base, method, path, token, key, body string, want int) governance.Request {
		t.Helper()
		req, err := http.NewRequest(method, base+path, bytes.NewBufferString(body))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Idempotency-Key", key)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		data, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatal(err)
		}
		if resp.StatusCode != want {
			t.Fatalf("%s %s: got %d, want %d: %s", method, path, resp.StatusCode, want, data)
		}
		var v governance.Request
		if want < 300 {
			if err := json.Unmarshal(data, &v); err != nil {
				t.Fatal(err)
			}
		}
		return v
	}

	restart := `{"action":"service.restart","service":"payments"}`
	read := `{"action":"logs.read","service":"payments"}`
	call(sa.URL, "POST", "/requests", "bad", "bad-token", restart, 401)
	call(sa.URL, "POST", "/requests", agentToken, "bad-scope", `{"action":"logs.read","service":"other"}`, 403)
	call(sa.URL, "POST", "/requests", agentToken, "bad-action", `{"action":"service.delete","service":"payments"}`, 403)
	call(sa.URL, "POST", "/requests", agentToken, "spoof", `{"action":"logs.read","service":"payments","user_id":"admin"}`, 400)
	call(sa.URL, "POST", "/requests", agentToken, "bad-json", read+read, 400)
	call(sa.URL, "POST", "/requests", agentToken, "", restart, 400)
	v := call(sa.URL, "POST", "/requests", agentToken, "restart-1", restart, 201)
	if v.Status != "pending" || v.UserID != "demo-user" || v.AgentID != "demo-agent" {
		t.Fatalf("unexpected request identity/state: %+v", v)
	}
	duplicate := call(sb.URL, "POST", "/requests", agentToken, "restart-1", restart, 200)
	if duplicate.ID != v.ID {
		t.Fatal("retry created a second request")
	}
	call(sb.URL, "POST", "/requests", agentToken, "restart-1", read, 409)
	path := "/requests/" + v.ID
	call(sb.URL, "POST", path+"/execute", agentToken, "", "", 409)
	call(sa.URL, "POST", path+"/approve", agentToken, "", "", 401)
	call(sa.URL, "POST", path+"/execute", adminToken, "", "", 401)
	call(sb.URL, "POST", path+"/approve", adminToken, "", "", 200)
	call(sa.URL, "POST", path+"/approve", adminToken, "", "", 200)

	var wg sync.WaitGroup
	for i := range 16 {
		wg.Go(func() {
			base := sa.URL
			if i%2 == 1 {
				base = sb.URL
			}
			done := call(base, "POST", path+"/execute", agentToken, "", "", 200)
			var result struct {
				Count int  `json:"restart_count"`
				Mock  bool `json:"simulated"`
			}
			if err := json.Unmarshal(done.Result, &result); err != nil || done.Status != "succeeded" || result.Count != 1 || !result.Mock {
				t.Errorf("unexpected result: %+v", done)
			}
		})
	}
	wg.Wait()
	var count int
	if err := db.QueryRow("SELECT restart_count FROM demo_services WHERE name = 'payments'").Scan(&count); err != nil || count != 1 {
		t.Fatalf("restart count %d, error %v", count, err)
	}
	for _, event := range []string{"requested", "approved", "executed"} {
		if err := db.QueryRow("SELECT count(*) FROM audit WHERE request_id = $1 AND event = $2", v.ID, event).Scan(&count); err != nil || count != 1 {
			t.Fatalf("%s audit count %d, error %v", event, count, err)
		}
	}
	call(sb.URL, "GET", path, agentToken, "", "", 200)
	call(sa.URL, "GET", "/requests/missing", agentToken, "", "", 404)
	logs := call(sa.URL, "POST", "/requests", agentToken, "read-1", read, 201)
	call(sb.URL, "POST", "/requests/"+logs.ID+"/execute", agentToken, "", "", 200)

	pending := call(sa.URL, "POST", "/requests", agentToken, "restart-2", restart, 201)
	call(sb.URL, "POST", "/requests/"+pending.ID+"/approve", adminToken, "", "", 200)
	for i, mutation := range []string{
		"active = false", "expires_at = now() - interval '1 second'",
		"actions = ARRAY['logs.read']", "user_id = 'someone-else'",
	} {
		if _, err := db.Exec("UPDATE delegations SET " + mutation); err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			if err := postgres.Initialize(context.Background(), db2); err != nil {
				t.Fatal(err)
			}
		}
		call(sb.URL, "POST", "/requests/"+pending.ID+"/execute", agentToken, "", "", 403)
		if i != 3 {
			call(sa.URL, "POST", "/requests", agentToken, fmt.Sprintf("revoked-%d", i), restart, 403)
		}
		if _, err := db.Exec("UPDATE delegations SET active = true, expires_at = now() + interval '1 hour', actions = ARRAY['logs.read','service.restart'], user_id = 'demo-user'"); err != nil {
			t.Fatal(err)
		}
	}
	if err := postgres.Initialize(context.Background(), db2); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	call(sa.URL, "POST", "/requests/"+pending.ID+"/execute", agentToken, "", "", 503)
}
