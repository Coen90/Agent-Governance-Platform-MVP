CREATE TABLE IF NOT EXISTS delegations (
  agent_id text PRIMARY KEY,
  user_id text NOT NULL,
  service text NOT NULL,
  actions text[] NOT NULL,
  active boolean NOT NULL DEFAULT true,
  expires_at timestamptz NOT NULL
);

CREATE TABLE IF NOT EXISTS requests (
  id text PRIMARY KEY,
  agent_id text NOT NULL REFERENCES delegations(agent_id),
  user_id text NOT NULL,
  idempotency_key text NOT NULL,
  action text NOT NULL CHECK (action IN ('logs.read', 'service.restart')),
  service text NOT NULL,
  status text NOT NULL CHECK (status IN ('pending', 'approved', 'succeeded')),
  result jsonb NOT NULL DEFAULT 'null',
  created_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (agent_id, idempotency_key)
);

CREATE TABLE IF NOT EXISTS audit (
  id bigserial PRIMARY KEY,
  request_id text NOT NULL REFERENCES requests(id),
  event text NOT NULL,
  actor text NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now()
);

-- A mock target system. Restarting changes this counter, not a real service.
CREATE TABLE IF NOT EXISTS demo_services (
  name text PRIMARY KEY,
  restart_count integer NOT NULL DEFAULT 0
);

INSERT INTO delegations (agent_id, user_id, service, actions, expires_at)
VALUES ('demo-agent', 'demo-user', 'payments',
        ARRAY['logs.read', 'service.restart'], now() + interval '24 hours')
ON CONFLICT DO NOTHING;
INSERT INTO demo_services (name) VALUES ('payments') ON CONFLICT DO NOTHING;
