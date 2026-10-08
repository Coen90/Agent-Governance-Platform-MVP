# Agent Governance Platform MVP

**에이전트가 사용자를 대신해 허용된 도구만 실행하도록 하는 작은 Go API입니다.**

결제 서비스(`payments`) 로그 조회와 관리자 승인 후 재시작, 두 가지 흐름만 구현했습니다.
로그와 재시작은 모두 모의 도구입니다. 실제 인프라를 조회하거나 재시작하지 않습니다.

## 실행

Docker Desktop을 실행한 뒤:

```sh
docker compose up --build -d
python3 demo.py
```

데모는 A 서버에 요청 → 승인 전 실행 차단 → B 서버에서 승인 → 두 서버에 동시 실행 12회 → 같은 키로 재요청 순서로 진행됩니다.

```text
                   ┌─ Go gateway A :8081 ─┐
Agent → LB :8080 ───┤                     ├─ PostgreSQL
                   └─ Go gateway B :8082 ─┘
```

`docker compose down`으로 종료합니다. DB는 볼륨에 보존됩니다.

## 아주 작은 권한 모델

- `AGENT_TOKEN`은 `demo-agent`에 연결되고, DB의 위임 정보는 사용자를 `demo-user`로 식별합니다.
- 클라이언트는 `action`, `service`만 보냅니다. 사용자나 에이전트 신원을 직접 지정할 수 없습니다.
- 위임 범위는 `payments`의 `logs.read`, `service.restart`이며, 처음 실행할 때 24시간 만료로 생성됩니다.
- `logs.read`는 바로 실행 가능하고, `service.restart`는 `ADMIN_TOKEN`으로 승인해야 합니다.
- 승인과 실행 시 위임의 활성 상태, 만료, 행동 범위, 사용자 관계를 다시 검사합니다.
- 요청 내용은 생성 후 바뀌지 않으므로 승인은 특정 행동과 대상에 연결됩니다.

정책은 Go와 DB의 작은 허용 목록으로 구현했습니다. OPA, OAuth, 에이전트 등록 화면, 실제 시스템 연결은 후속 작업입니다.

## API

| 메서드 | 경로 | 인증 | 동작 |
|---|---|---|---|
| POST | `/requests` | 에이전트 | 요청 생성. `Idempotency-Key` 필수 |
| GET | `/requests/{id}` | 에이전트 | 요청 상태와 결과 조회 |
| POST | `/requests/{id}/approve` | 관리자 | 요청 승인 |
| POST | `/requests/{id}/execute` | 에이전트 | 승인과 위임 확인 후 실행 |
| GET | `/healthz` | 없음 | DB 연결 확인 |

```sh
curl -s http://localhost:8080/requests \
  -H 'Authorization: Bearer local-demo-agent-token' \
  -H 'Idempotency-Key: log-example-1' \
  -H 'Content-Type: application/json' \
  -d '{"action":"logs.read","service":"payments"}'
```

응답의 `id`를 `/requests/{id}/execute`에 넣어 같은 에이전트 토큰으로 POST하면 모의 로그를 받습니다.
재시작 요청은 `pending → approved → succeeded`, 조회는 `approved → succeeded` 순서입니다.
같은 에이전트가 같은 키와 내용으로 재요청하면 기존 요청을 반환하고, 다른 내용을 보내면 HTTP 409입니다.

## 공유 상태와 감사

게이트웨이는 요청 상태를 메모리에 보관하지 않습니다. 요청별 DB 행 잠금과 트랜잭션으로 승인·실행·감사 기록을 처리합니다.
모의 재시작 카운터와 실행 결과도 같은 트랜잭션에 포함되므로, 두 서버에서 동시에 실행하거나 재시도해도 한 번만 증가합니다.
실제 외부 API를 연결할 때는 이 보장이 외부 호출에 그대로 적용되지 않습니다. 대상의 멱등성 지원과 불명확한 실행 결과 처리부터 추가해야 합니다.

감사 기록 확인:

```sh
docker compose exec db psql -U gateway -d gateway -c \
  'SELECT r.user_id, r.agent_id, r.action, r.service, a.event, a.actor, a.created_at FROM audit a JOIN requests r ON r.id = a.request_id ORDER BY a.id;'
```

위임 즉시 중단:

```sh
docker compose exec db psql -U gateway -d gateway -c \
  "UPDATE delegations SET active = false WHERE agent_id = 'demo-agent';"
```

중단 전에 이미 커밋된 실행은 취소되지 않습니다. 서버를 재시작해도 위임을 다시 활성화하거나 만료를 연장하지 않습니다.

## 검증

통합 테스트는 별도의 임시 스키마를 생성하고 종료 시 그 스키마만 삭제합니다.
PostgreSQL URL을 제공하면 두 인스턴스의 승인 공유, 동시 실행, 인증, 권한 범위, 위임 중단·만료, DB 실패 시 실행 차단을 검사합니다.
환경변수가 없으면 통합 테스트는 건너뜁니다.

```sh
TEST_DATABASE_URL='postgres://user:password@localhost:5432/testdb?sslmode=disable' go test -race -v ./...
go vet ./...
```

Compose의 DB는 호스트 포트를 공개하지 않습니다. 테스트용 DB가 필요하면:

```sh
docker run -d --name agent-gateway-mvp-test \
  -e POSTGRES_PASSWORD=test-password -e POSTGRES_DB=gateway_test \
  -p 127.0.0.1:15432:5432 postgres:17-alpine
# DB가 준비된 뒤 실행
TEST_DATABASE_URL='postgres://postgres:test-password@localhost:15432/gateway_test?sslmode=disable' go test -race -v ./...
docker stop agent-gateway-mvp-test
docker rm agent-gateway-mvp-test
```

Compose의 고정 토큰과 DB 비밀번호는 로컬 데모용입니다. 포트는 localhost에만 열립니다.
이 MVP는 사용자 한 명, 에이전트 한 개, 관리자 한 명만 다루며 감사 기록은 일반 DB 테이블입니다.
