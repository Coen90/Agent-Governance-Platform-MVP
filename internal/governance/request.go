package governance

import "encoding/json"

const (
	ActionReadLogs  = "logs.read"
	ActionRestart   = "service.restart"
	StatusPending   = "pending"
	StatusApproved  = "approved"
	StatusSucceeded = "succeeded"
)

type Input struct {
	Action  string `json:"action"`
	Service string `json:"service"`
}

type Request struct {
	ID      string          `json:"id"`
	AgentID string          `json:"agent_id"`
	UserID  string          `json:"user_id"`
	Action  string          `json:"action"`
	Service string          `json:"service"`
	Status  string          `json:"status"`
	Result  json.RawMessage `json:"result"`
}
