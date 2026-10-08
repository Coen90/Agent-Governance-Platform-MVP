package httpapi

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"

	"agent-gateway-mvp/internal/governance"
)

func handleError(w http.ResponseWriter, err error) bool {
	if err == nil {
		return false
	}
	code := http.StatusServiceUnavailable
	message := "storage unavailable; retry with the same idempotency key"
	switch {
	case errors.Is(err, governance.ErrNotFound):
		code, message = http.StatusNotFound, governance.ErrNotFound.Error()
	case errors.Is(err, governance.ErrActionNotAllowed):
		code, message = http.StatusForbidden, governance.ErrActionNotAllowed.Error()
	case errors.Is(err, governance.ErrOutsideScope):
		code, message = http.StatusForbidden, governance.ErrOutsideScope.Error()
	case errors.Is(err, governance.ErrDelegationChanged):
		code, message = http.StatusForbidden, governance.ErrDelegationChanged.Error()
	case errors.Is(err, governance.ErrKeyConflict):
		code, message = http.StatusConflict, governance.ErrKeyConflict.Error()
	case errors.Is(err, governance.ErrApprovalRequired):
		code, message = http.StatusConflict, governance.ErrApprovalRequired.Error()
	default:
		log.Printf("database operation failed: %v", err)
	}
	respond(w, code, map[string]string{"error": message})
	return true
}

func respond(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}
