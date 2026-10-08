package governance

import "errors"

var (
	ErrNotFound          = errors.New("request not found")
	ErrActionNotAllowed  = errors.New("action not allowed")
	ErrOutsideScope      = errors.New("outside active delegation")
	ErrDelegationChanged = errors.New("delegation revoked, expired, or changed")
	ErrKeyConflict       = errors.New("key already used for a different request")
	ErrApprovalRequired  = errors.New("human approval required")
)
