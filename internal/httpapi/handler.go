package httpapi

import (
	"context"
	"net/http"
	"time"

	"agent-gateway-mvp/internal/governance"
)

// Store keeps PostgreSQL and transaction details out of the HTTP layer.
type Store interface {
	Ping(context.Context) error
	Create(context.Context, governance.Input, string) (governance.Request, bool, error)
	Get(context.Context, string) (governance.Request, error)
	Approve(context.Context, string) (governance.Request, error)
	Execute(context.Context, string) (governance.Request, error)
}

type handler struct {
	store                  Store
	agentToken, adminToken string
}

func NewHandler(store Store, agentToken, adminToken string) http.Handler {
	h := &handler{store: store, agentToken: agentToken, adminToken: adminToken}
	m := http.NewServeMux()
	m.HandleFunc("GET /healthz", h.health)
	m.HandleFunc("POST /requests", h.authorize(false, h.create))
	m.HandleFunc("GET /requests/{id}", h.authorize(false, h.get))
	m.HandleFunc("POST /requests/{id}/approve", h.authorize(true, h.approve))
	m.HandleFunc("POST /requests/{id}/execute", h.authorize(false, h.execute))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()
		m.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (h *handler) health(w http.ResponseWriter, r *http.Request) {
	if err := h.store.Ping(r.Context()); err != nil {
		respond(w, 503, map[string]string{"error": "database unavailable"})
		return
	}
	respond(w, 200, map[string]string{"status": "ok"})
}
