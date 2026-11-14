import time

INVOICE = {"kind": "tool.call", "tool": "get_invoice", "server": "finance-mcp", "args": {"invoice_id": "INV-000123"}}


def test_admin_api_requires_key(secured):
    assert secured.http.get(secured.gateway + "/v1/agents").status_code == 401
    assert secured.http.get(secured.gateway + "/v1/agents", headers={"Authorization": "Bearer wrong"}).status_code == 401
    assert secured.http.get(secured.gateway + "/v1/agents", headers=secured.admin()).status_code == 200


def test_health_is_open(secured):
    assert secured.http.get(secured.gateway + "/v1/health").status_code == 200


def test_decide_requires_agent_identity(secured):
    r = secured.http.post(secured.gateway + "/v1/decide", json={"agent_id": "finance-agent", "kind": "tool.call", "tool": "get_invoice"})
    assert r.status_code in (401, 403)


def test_agent_key_works_and_wrong_key_fails(secured):
    assert secured.agent_key.startswith("rgk_")
    assert secured.decide(INVOICE)["verdict"] == "allow"
    h = {"X-Rungated-Agent": "finance-agent", "X-Rungated-Key": "rgk_wrong"}
    r = secured.http.post(secured.gateway + "/v1/decide", json={"agent_id": "finance-agent", "kind": "tool.call", "tool": "get_invoice"}, headers=h)
    assert r.status_code in (401, 403)


def issue_token(s, session):
    r = s.http.post(s.gateway + "/v1/agents/finance-agent/token", headers={"X-Rungated-Agent": "finance-agent", "X-Rungated-Key": s.agent_key}, json={"session_id": session, "ttl": 300})
    assert r.status_code == 200, r.text
    tok = r.json().get("token")
    assert tok and tok.startswith("rgt1.")
    return tok


def test_agent_token_flow(secured):
    tok = issue_token(secured, "tok-1")
    r = secured.http.post(secured.gateway + "/v1/decide", headers={"X-Rungated-Token": tok}, json=dict(INVOICE, agent_id="finance-agent"))
    assert r.status_code == 200 and r.json()["verdict"] == "allow"


def test_token_revoked_by_containment(secured):
    tok = issue_token(secured, "tok-2")
    assert secured.http.post(secured.gateway + "/v1/contain", headers=secured.admin(), json={"agent_id": "finance-agent", "reason": "e2e"}).status_code in (200, 201)
    r = secured.http.post(secured.gateway + "/v1/decide", headers={"X-Rungated-Token": tok}, json=dict(INVOICE, agent_id="finance-agent"))
    assert r.status_code != 200 or r.json()["verdict"] != "allow"


def test_gateway_restart_keeps_containment_and_audit(secured):
    assert secured.http.post(secured.gateway + "/v1/contain", headers=secured.admin(), json={"agent_id": "finance-agent", "reason": "restart"}).status_code in (200, 201)
    time.sleep(0.5)
    secured.stop_gateway()
    secured.start_gateway()
    c = secured.http.get(secured.gateway + "/v1/containments", headers=secured.admin()).json()["containments"]
    assert any(x["agent_id"] == "finance-agent" and not x.get("released_at") for x in c)
    assert secured.http.get(secured.gateway + "/v1/audit/verify", headers=secured.admin()).json()["ok"] is True
