"""Run after `docker compose up --build -d`. No Python dependencies needed."""
import json
import uuid
from concurrent.futures import ThreadPoolExecutor
from urllib.error import HTTPError
from urllib.request import Request, urlopen


def call(port, path, *, admin=False, body=None, key=None):
    token = "local-demo-admin-token" if admin else "local-demo-agent-token"
    headers = {"Authorization": f"Bearer {token}", "Content-Type": "application/json"}
    if key:
        headers["Idempotency-Key"] = key
    req = Request(f"http://127.0.0.1:{port}{path}",
                  data=json.dumps(body).encode() if body else b"",
                  headers=headers, method="POST")
    with urlopen(req, timeout=15) as resp:
        return json.load(resp)


if __name__ == "__main__":
    key = str(uuid.uuid4())
    body = {"action": "service.restart", "service": "payments"}
    req = call(8081, "/requests", body=body, key=key)
    print("1. A 서버에서 재시작 요청:", json.dumps(req, ensure_ascii=False))
    path = f"/requests/{req['id']}"
    try:
        call(8082, path + "/execute")
        raise AssertionError("승인 없이 실행됨")
    except HTTPError as err:
        assert err.code == 409
        print("2. 승인 전 실행 차단: HTTP 409")
    call(8082, path + "/approve", admin=True)
    print("3. B 서버에서 관리자 승인 완료")
    with ThreadPoolExecutor(max_workers=12) as pool:
        results = list(pool.map(lambda i: call(8081 + i % 2, path + "/execute"), range(12)))
    assert all(r["status"] == "succeeded" for r in results)
    assert all(r["result"] == results[0]["result"] for r in results)
    print("4. A/B에 동시 실행 12회 → 모두 같은 결과:", results[0]["result"])
    retry = call(8080, "/requests", body=body, key=key)
    assert retry["id"] == req["id"]
    print("5. 로드밸런서를 통한 재요청 → 동일 요청 ID")
