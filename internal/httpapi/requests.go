package httpapi

import (
	"encoding/json"
	"io"
	"net/http"

	"agent-gateway-mvp/internal/governance"
)

func (h *handler) create(w http.ResponseWriter, r *http.Request) {
	var input governance.Input
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
	d.DisallowUnknownFields()
	if err := d.Decode(&input); err != nil {
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
	v, created, err := h.store.Create(r.Context(), input, key)
	if handleError(w, err) {
		return
	}
	code := http.StatusOK
	if created {
		code = http.StatusCreated
	}
	respond(w, code, v)
}

func (h *handler) get(w http.ResponseWriter, r *http.Request) {
	v, err := h.store.Get(r.Context(), r.PathValue("id"))
	if !handleError(w, err) {
		respond(w, 200, v)
	}
}

func (h *handler) approve(w http.ResponseWriter, r *http.Request) {
	v, err := h.store.Approve(r.Context(), r.PathValue("id"))
	if !handleError(w, err) {
		respond(w, 200, v)
	}
}

func (h *handler) execute(w http.ResponseWriter, r *http.Request) {
	v, err := h.store.Execute(r.Context(), r.PathValue("id"))
	if !handleError(w, err) {
		respond(w, 200, v)
	}
}
